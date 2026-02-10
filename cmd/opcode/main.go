package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/rendis/opcode/internal/actions"
	"github.com/rendis/opcode/internal/engine"
	"github.com/rendis/opcode/internal/isolation"
	"github.com/rendis/opcode/internal/logging"
	"github.com/rendis/opcode/internal/plugins"
	"github.com/rendis/opcode/internal/scheduler"
	"github.com/rendis/opcode/internal/secrets"
	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/internal/streaming"
	"github.com/rendis/opcode/internal/validation"
	"github.com/rendis/opcode/pkg/schema"
	opcmcp "github.com/rendis/opcode/pkg/mcp"
)

func main() {
	// --- Logging ---
	logLevel := envLogLevel("OPCODE_LOG_LEVEL", slog.LevelInfo)
	innerHandler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
	logger := slog.New(logging.NewCorrelationHandler(innerHandler))
	slog.SetDefault(logger)

	// --- Store ---
	dbPath := envString("OPCODE_DB_PATH", "opcode.db")
	libsqlStore, err := store.NewLibSQLStore(dbPath)
	if err != nil {
		logger.Error("failed to open store", slog.String("path", dbPath), slog.Any("error", err))
		os.Exit(1)
	}
	defer libsqlStore.Close()

	if err := libsqlStore.Migrate(context.Background()); err != nil {
		logger.Error("migration failed", slog.Any("error", err))
		os.Exit(1)
	}

	eventLog := store.NewEventLog(libsqlStore)

	// --- Vault ---
	vaultKey := envString("OPCODE_VAULT_KEY", "")
	var vault secrets.Vault
	if vaultKey != "" {
		v, vaultErr := secrets.NewAESVault(libsqlStore, secrets.VaultConfig{
			Passphrase: vaultKey,
			Salt:       []byte("opcode-vault-salt"),
			Iterations: 100_000,
		})
		if vaultErr != nil {
			logger.Error("vault init failed", slog.Any("error", vaultErr))
			os.Exit(1)
		}
		vault = v
		logger.Info("vault initialized")
	} else {
		logger.Warn("OPCODE_VAULT_KEY not set, secret interpolation disabled")
	}

	// --- Action Registry ---
	registry := actions.NewRegistry()
	jsonValidator, err := validation.NewJSONSchemaValidator()
	if err != nil {
		logger.Error("json schema validator init failed", slog.Any("error", err))
		os.Exit(1)
	}

	isolator := isolation.NewFallbackIsolator()

	err = actions.RegisterBuiltins(registry, jsonValidator,
		actions.HTTPConfig{},
		actions.FSConfig{},
		actions.ShellConfig{Isolator: isolator},
	)
	if err != nil {
		logger.Error("action registration failed", slog.Any("error", err))
		os.Exit(1)
	}
	logger.Info("actions registered", slog.Int("count", registry.Count()))

	// --- EventHub ---
	hub := streaming.NewMemoryHub()

	// --- Executor ---
	poolSize := envInt("OPCODE_POOL_SIZE", engine.DefaultPoolSize)
	executor := engine.NewExecutor(libsqlStore, eventLog, registry, engine.ExecutorConfig{
		PoolSize: poolSize,
	}, vault)
	logger.Info("executor initialized", slog.Int("pool_size", poolSize))

	// --- Workflow Actions (late-bind after executor exists) ---
	subRunner := &templateRunner{store: libsqlStore, executor: executor, logger: logger}
	err = actions.RegisterWorkflowActions(registry, actions.WorkflowActionDeps{
		RunSubWorkflow: subRunner.runSubWorkflow,
		Store:          libsqlStore,
		Hub:            hub,
		Logger:         logger,
	})
	if err != nil {
		logger.Error("workflow action registration failed", slog.Any("error", err))
		os.Exit(1)
	}
	logger.Info("workflow actions registered", slog.Int("total_actions", registry.Count()))

	// --- Plugin Manager ---
	pluginMgr := plugins.NewPluginManager(libsqlStore, registry, logger)

	// --- Scheduler ---
	sched := scheduler.NewScheduler(libsqlStore, subRunner, logger)

	// --- MCP Server ---
	mcpServer := opcmcp.NewOpcodeServer(opcmcp.OpcodeServerDeps{
		Executor: executor,
		Store:    libsqlStore,
		Vault:    vault,
		Registry: registry,
		Hub:      hub,
		Logger:   logger,
	})

	// --- Start ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sched.Start(ctx); err != nil {
		logger.Error("scheduler start failed", slog.Any("error", err))
		os.Exit(1)
	}

	// Graceful shutdown on SIGTERM/SIGINT.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		logger.Info("received signal, shutting down", slog.String("signal", sig.String()))
		cancel()
	}()

	logger.Info("opcode server starting", slog.String("transport", "stdio"))
	if err := mcpServer.Serve(ctx); err != nil && ctx.Err() == nil {
		logger.Error("mcp server error", slog.Any("error", err))
	}

	// --- Shutdown ---
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := sched.Stop(); err != nil {
		logger.Warn("scheduler stop error", slog.Any("error", err))
	}
	if err := pluginMgr.StopAll(shutdownCtx); err != nil {
		logger.Warn("plugin manager stop error", slog.Any("error", err))
	}

	logger.Info("opcode shutdown complete")
}

// templateRunner adapts Executor to the scheduler.SubWorkflowRunner interface.
type templateRunner struct {
	store    store.Store
	executor engine.Executor
	logger   *slog.Logger
}

// RunFromTemplate satisfies scheduler.SubWorkflowRunner.
func (r *templateRunner) RunFromTemplate(ctx context.Context, templateName, version string, params map[string]any, agentID string) error {
	_, err := r.runSubWorkflow(ctx, templateName, version, params, "", agentID)
	return err
}

// runSubWorkflow satisfies actions.SubWorkflowRunner (late-bind for workflow.run action).
func (r *templateRunner) runSubWorkflow(ctx context.Context, templateName, version string, params map[string]any, parentID, agentID string) (json.RawMessage, error) {
	tpl, err := r.resolveTemplate(ctx, templateName, version)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	wf := &store.Workflow{
		ID:              uuid.New().String(),
		Name:            tpl.Name,
		TemplateName:    tpl.Name,
		TemplateVersion: tpl.Version,
		Definition:      tpl.Definition,
		Status:          schema.WorkflowStatusPending,
		AgentID:         agentID,
		ParentID:        parentID,
		InputParams:     params,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if createErr := r.store.CreateWorkflow(ctx, wf); createErr != nil {
		return nil, fmt.Errorf("create workflow: %w", createErr)
	}

	result, runErr := r.executor.Run(ctx, wf, params)
	if runErr != nil {
		return nil, runErr
	}
	return result.Output, nil
}

func (r *templateRunner) resolveTemplate(ctx context.Context, name, version string) (*store.WorkflowTemplate, error) {
	if version != "" {
		return r.store.GetTemplate(ctx, name, version)
	}
	templates, err := r.store.ListTemplates(ctx, store.TemplateFilter{Name: name, Limit: 100})
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	if len(templates) == 0 {
		return nil, fmt.Errorf("template %q not found", name)
	}
	best := templates[0]
	for _, t := range templates[1:] {
		if versionNum(t.Version) > versionNum(best.Version) {
			best = t
		}
	}
	return best, nil
}

func versionNum(v string) int {
	v = strings.TrimPrefix(v, "v")
	n, _ := strconv.Atoi(v)
	return n
}

// --- Env helpers ---

func envString(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}

func envLogLevel(key string, defaultVal slog.Level) slog.Level {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	switch strings.ToLower(v) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return defaultVal
	}
}

