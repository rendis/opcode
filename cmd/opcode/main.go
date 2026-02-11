package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"net/http"

	"github.com/google/uuid"
	"github.com/rendis/opcode/internal/actions"
	"github.com/rendis/opcode/internal/engine"
	"github.com/rendis/opcode/internal/isolation"
	"github.com/rendis/opcode/internal/logging"
	"github.com/rendis/opcode/internal/panel"
	"github.com/rendis/opcode/internal/plugins"
	"github.com/rendis/opcode/internal/scheduler"
	"github.com/rendis/opcode/internal/secrets"
	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/internal/streaming"
	"github.com/rendis/opcode/internal/validation"
	opcmcp "github.com/rendis/opcode/pkg/mcp"
	"github.com/rendis/opcode/pkg/schema"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			runInstall(os.Args[2:])
			return
		case "serve":
			// fall through to serve
		}
	}
	runServe()
}

func runServe() {
	cfg := loadConfig()

	// --- Logging ---
	logLevel := parseLogLevel(cfg.LogLevel)
	innerHandler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
	logger := slog.New(logging.NewCorrelationHandler(innerHandler))
	slog.SetDefault(logger)

	// --- Data directory ---
	if dir := filepath.Dir(cfg.DBPath); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot create data directory %s: %v\n", dir, err)
			os.Exit(1)
		}
	}

	// --- Store ---
	libsqlStore, err := store.NewLibSQLStore(cfg.DBPath)
	if err != nil {
		logger.Error("failed to open store", slog.String("path", cfg.DBPath), slog.Any("error", err))
		os.Exit(1)
	}
	defer libsqlStore.Close()

	if err := libsqlStore.Migrate(context.Background()); err != nil {
		logger.Error("migration failed", slog.Any("error", err))
		os.Exit(1)
	}

	eventLog := store.NewEventLog(libsqlStore)

	// --- Vault ---
	vaultKey := os.Getenv("OPCODE_VAULT_KEY")
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
	executor := engine.NewExecutor(libsqlStore, eventLog, registry, engine.ExecutorConfig{
		PoolSize: cfg.PoolSize,
	}, vault)
	logger.Info("executor initialized", slog.Int("pool_size", cfg.PoolSize))

	// --- Session Registry ---
	sessions := opcmcp.NewSessionRegistry()

	// --- MCP Server (before workflow actions — notifier needs MCPServer) ---
	mcpServer := opcmcp.NewOpcodeServer(opcmcp.OpcodeServerDeps{
		Executor: executor,
		Store:    libsqlStore,
		Vault:    vault,
		Registry: registry,
		Hub:      hub,
		Sessions: sessions,
		Logger:   logger,
	})

	// --- Notifier ---
	notifier := opcmcp.NewMCPNotifier(mcpServer.MCPServer(), sessions)

	// --- Workflow Actions (late-bind after executor + MCP server exist) ---
	subRunner := &templateRunner{store: libsqlStore, executor: executor, logger: logger}
	err = actions.RegisterWorkflowActions(registry, actions.WorkflowActionDeps{
		RunSubWorkflow: subRunner.runSubWorkflow,
		Store:          libsqlStore,
		Hub:            hub,
		Notifier:       notifier,
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

	// --- Recovery ---
	recoverOrphanedWorkflows(context.Background(), libsqlStore, eventLog, logger)

	// --- Panel ---
	panelSrv := panel.NewPanelServer(panel.PanelDeps{
		Store:    libsqlStore,
		Executor: executor,
		Hub:      hub,
		Logger:   logger,
	})

	// --- SSE Transport ---
	sseServer := mcpServer.NewSSEServer(cfg.BaseURL)

	// --- Compose HTTP mux: MCP SSE + Panel on same port ---
	mux := http.NewServeMux()
	mux.Handle("/sse", sseServer)
	mux.Handle("/message", sseServer)
	mux.Handle("/", panelSrv.Handler())

	httpSrv := &http.Server{Addr: cfg.ListenAddr, Handler: mux}

	// --- Start ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sched.Start(ctx); err != nil {
		logger.Error("scheduler start failed", slog.Any("error", err))
		os.Exit(1)
	}

	if err := sched.RecoverMissed(ctx); err != nil {
		logger.Warn("scheduler recovery failed", slog.Any("error", err))
	}

	// Graceful shutdown on SIGTERM/SIGINT.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		logger.Info("received signal, shutting down", slog.String("signal", sig.String()))
		cancel()
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = sseServer.Shutdown(shutdownCtx)
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	logger.Info("opcode server starting",
		slog.String("addr", cfg.ListenAddr),
		slog.String("panel", "http://"+cfg.ListenAddr),
	)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		if isAddrInUse(err) {
			port := strings.TrimPrefix(cfg.ListenAddr, ":")
			fmt.Fprintf(os.Stderr, "\nError: port %s is already in use.\n\nAnother opcode instance or a different service is listening on this port.\nTo fix, either:\n  1. Stop the existing process: lsof -ti tcp:%s | xargs kill\n  2. Use a different port: opcode install --listen-addr :4200\n\n", cfg.ListenAddr, port)
		} else {
			logger.Error("server error", slog.Any("error", err))
		}
		os.Exit(1)
	}

	// --- Shutdown ---
	if err := sched.Stop(); err != nil {
		logger.Warn("scheduler stop error", slog.Any("error", err))
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := pluginMgr.StopAll(shutdownCtx); err != nil {
		logger.Warn("plugin manager stop error", slog.Any("error", err))
	}

	logger.Info("opcode shutdown complete")
}

// recoverOrphanedWorkflows transitions any active workflows to suspended on startup.
// This handles the case where the process crashed while workflows were running.
func recoverOrphanedWorkflows(ctx context.Context, s store.Store, el *store.EventLog, logger *slog.Logger) {
	activeStatus := schema.WorkflowStatusActive
	workflows, err := s.ListWorkflows(ctx, store.WorkflowFilter{Status: &activeStatus})
	if err != nil {
		logger.Error("failed to scan orphaned workflows", slog.Any("error", err))
		return
	}
	if len(workflows) == 0 {
		return
	}
	suspendedStatus := schema.WorkflowStatusSuspended
	recovered := 0
	for _, wf := range workflows {
		if err := s.UpdateWorkflow(ctx, wf.ID, store.WorkflowUpdate{Status: &suspendedStatus}); err != nil {
			logger.Error("failed to suspend orphaned workflow",
				slog.String("workflow_id", wf.ID), slog.Any("error", err))
			continue
		}
		_ = el.AppendEvent(ctx, &store.Event{
			WorkflowID: wf.ID,
			Type:       schema.EventWorkflowInterrupted,
			Payload:    json.RawMessage(`{"reason":"process_restart"}`),
		})
		recovered++
	}
	logger.Info("recovered orphaned workflows", slog.Int("count", recovered))
}

func isAddrInUse(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return errors.Is(opErr.Err, syscall.EADDRINUSE)
	}
	return strings.Contains(err.Error(), "address already in use")
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
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
