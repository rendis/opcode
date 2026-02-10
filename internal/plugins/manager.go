package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os/exec"
	"sync"
	"time"

	"github.com/rendis/opcode/internal/actions"
	"github.com/rendis/opcode/internal/store"
)

// PluginConfig describes how to launch and identify a plugin subprocess.
type PluginConfig struct {
	ID      string
	Name    string
	Command string   // MCP server binary path
	Args    []string // CLI arguments
	Env     []string // environment variables
}

// PluginManager manages the lifecycle of MCP plugin subprocesses.
type PluginManager struct {
	store    store.Store
	registry *actions.Registry
	plugins  map[string]*managedPlugin
	mu       sync.RWMutex
	logger   *slog.Logger
}

type managedPlugin struct {
	config     PluginConfig
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	status     string // starting, healthy, unhealthy, crashed, stopped
	errCount   int
	lastErr    string
	cancelFunc context.CancelFunc
}

// NewPluginManager creates a new PluginManager.
func NewPluginManager(s store.Store, registry *actions.Registry, logger *slog.Logger) *PluginManager {
	return &PluginManager{
		store:    s,
		registry: registry,
		plugins:  make(map[string]*managedPlugin),
		logger:   logger,
	}
}

// LoadPlugin starts a plugin subprocess and initializes MCP handshake.
func (pm *PluginManager) LoadPlugin(ctx context.Context, config PluginConfig) error {
	pm.mu.Lock()
	if _, exists := pm.plugins[config.ID]; exists {
		pm.mu.Unlock()
		return fmt.Errorf("plugin %q already loaded", config.ID)
	}
	pm.mu.Unlock()

	pluginCtx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(pluginCtx, config.Command, config.Args...)
	cmd.Env = config.Env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stdout pipe: %w", err)
	}

	mp := &managedPlugin{
		config:     config,
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdout,
		status:     "starting",
		cancelFunc: cancel,
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start plugin %q: %w", config.ID, err)
	}

	// Initialize MCP handshake.
	if err := pm.initializeHandshake(mp); err != nil {
		cancel()
		_ = cmd.Process.Kill()
		return fmt.Errorf("handshake with plugin %q: %w", config.ID, err)
	}

	mp.status = "healthy"

	pm.mu.Lock()
	pm.plugins[config.ID] = mp
	pm.mu.Unlock()

	// Persist plugin record.
	configJSON, _ := json.Marshal(map[string]any{
		"command": config.Command,
		"args":    config.Args,
	})
	_ = pm.store.CreatePlugin(ctx, &store.Plugin{
		ID:     config.ID,
		Name:   config.Name,
		Type:   "mcp",
		Config: configJSON,
		Status: "active",
	})

	// Start health check loop.
	go pm.healthCheckLoop(pluginCtx, mp)

	pm.logger.Info("plugin loaded", slog.String("id", config.ID), slog.String("name", config.Name))
	return nil
}

// initializeHandshake sends the MCP initialize request and reads the response.
func (pm *PluginManager) initializeHandshake(mp *managedPlugin) error {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "opcode",
				"version": "1.0.0",
			},
		},
	}
	return pm.sendRequest(mp, req)
}

func (pm *PluginManager) sendRequest(mp *managedPlugin, req map[string]any) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')
	if _, err := mp.stdin.Write(data); err != nil {
		return fmt.Errorf("write request: %w", err)
	}

	// Read response (with timeout).
	scanner := bufio.NewScanner(mp.stdout)
	done := make(chan bool, 1)
	var response map[string]any

	go func() {
		if scanner.Scan() {
			line := scanner.Bytes()
			_ = json.Unmarshal(line, &response)
			done <- true
		} else {
			done <- false
		}
	}()

	select {
	case ok := <-done:
		if !ok {
			return fmt.Errorf("failed to read response")
		}
		if errField, exists := response["error"]; exists {
			return fmt.Errorf("plugin error: %v", errField)
		}
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("handshake timeout")
	}
}

// healthCheckLoop periodically pings the plugin and manages status.
func (pm *PluginManager) healthCheckLoop(ctx context.Context, mp *managedPlugin) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := pm.pingPlugin(mp); err != nil {
				pm.mu.Lock()
				mp.errCount++
				mp.lastErr = err.Error()
				if mp.errCount >= 3 {
					mp.status = "unhealthy"
					pm.logger.Warn("plugin unhealthy",
						slog.String("id", mp.config.ID),
						slog.Int("consecutive_errors", mp.errCount),
					)
					pm.mu.Unlock()
					pm.restartPlugin(ctx, mp)
					return
				}
				pm.mu.Unlock()
				_ = pm.store.UpdatePlugin(ctx, mp.config.ID, "active", mp.lastErr)
			} else {
				pm.mu.Lock()
				mp.errCount = 0
				mp.status = "healthy"
				pm.mu.Unlock()
				_ = pm.store.UpdatePlugin(ctx, mp.config.ID, "active", "")
			}
		}
	}
}

func (pm *PluginManager) pingPlugin(mp *managedPlugin) error {
	if mp.cmd.ProcessState != nil {
		return fmt.Errorf("process exited")
	}
	return nil
}

// restartPlugin attempts to restart a plugin with exponential backoff.
func (pm *PluginManager) restartPlugin(ctx context.Context, mp *managedPlugin) {
	pm.mu.Lock()
	errCount := mp.errCount
	mp.status = "crashed"
	pm.mu.Unlock()

	_ = pm.store.UpdatePlugin(ctx, mp.config.ID, "error", mp.lastErr)

	// Exponential backoff: min(1s * 2^errCount, 60s)
	delay := time.Duration(math.Min(
		float64(time.Second)*math.Pow(2, float64(errCount)),
		float64(60*time.Second),
	))

	pm.logger.Info("restarting plugin",
		slog.String("id", mp.config.ID),
		slog.Duration("backoff", delay),
	)

	select {
	case <-ctx.Done():
		return
	case <-time.After(delay):
	}

	// Clean up old process.
	mp.cancelFunc()
	if mp.cmd.Process != nil {
		_ = mp.cmd.Process.Kill()
	}

	// Re-load.
	pm.mu.Lock()
	delete(pm.plugins, mp.config.ID)
	pm.mu.Unlock()

	if err := pm.LoadPlugin(ctx, mp.config); err != nil {
		pm.logger.Error("failed to restart plugin",
			slog.String("id", mp.config.ID),
			slog.String("error", err.Error()),
		)
	}
}

// DiscoverActions sends a tools/list MCP request and registers discovered actions.
func (pm *PluginManager) DiscoverActions(ctx context.Context, pluginID string) error {
	pm.mu.RLock()
	mp, ok := pm.plugins[pluginID]
	pm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("plugin %q not found", pluginID)
	}

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal tools/list: %w", err)
	}
	data = append(data, '\n')
	if _, err := mp.stdin.Write(data); err != nil {
		return fmt.Errorf("write tools/list: %w", err)
	}

	scanner := bufio.NewScanner(mp.stdout)
	done := make(chan map[string]any, 1)

	go func() {
		if scanner.Scan() {
			var resp map[string]any
			_ = json.Unmarshal(scanner.Bytes(), &resp)
			done <- resp
		} else {
			done <- nil
		}
	}()

	var resp map[string]any
	select {
	case resp = <-done:
		if resp == nil {
			return fmt.Errorf("failed to read tools/list response")
		}
	case <-time.After(10 * time.Second):
		return fmt.Errorf("tools/list timeout")
	case <-ctx.Done():
		return ctx.Err()
	}

	// Parse tools from response.
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return fmt.Errorf("unexpected response format")
	}

	toolsRaw, ok := result["tools"].([]any)
	if !ok {
		return nil // no tools
	}

	var pluginActions []actions.Action
	for _, t := range toolsRaw {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		name, _ := tool["name"].(string)
		desc, _ := tool["description"].(string)
		inputSchema, _ := json.Marshal(tool["inputSchema"])

		pluginActions = append(pluginActions, &mcpToolAction{
			name:        name,
			description: desc,
			inputSchema: inputSchema,
			plugin:      mp,
		})
	}

	if len(pluginActions) > 0 {
		_, err := pm.registry.RegisterPlugin(mp.config.Name, pluginActions)
		if err != nil {
			return fmt.Errorf("register plugin actions: %w", err)
		}
		pm.logger.Info("discovered plugin actions",
			slog.String("id", pluginID),
			slog.Int("count", len(pluginActions)),
		)
	}

	return nil
}

// StopPlugin gracefully stops a plugin subprocess.
func (pm *PluginManager) StopPlugin(ctx context.Context, id string) error {
	pm.mu.Lock()
	mp, ok := pm.plugins[id]
	if !ok {
		pm.mu.Unlock()
		return fmt.Errorf("plugin %q not found", id)
	}
	delete(pm.plugins, id)
	pm.mu.Unlock()

	mp.cancelFunc()

	if mp.cmd.Process != nil {
		// Close stdin to signal shutdown.
		_ = mp.stdin.Close()

		// Wait with timeout for graceful exit.
		done := make(chan error, 1)
		go func() { done <- mp.cmd.Wait() }()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = mp.cmd.Process.Kill()
			<-done
		}
	}

	mp.status = "stopped"
	_ = pm.store.UpdatePlugin(ctx, id, "inactive", "")

	pm.logger.Info("plugin stopped", slog.String("id", id))
	return nil
}

// StopAll stops all managed plugins.
func (pm *PluginManager) StopAll(ctx context.Context) error {
	pm.mu.RLock()
	ids := make([]string, 0, len(pm.plugins))
	for id := range pm.plugins {
		ids = append(ids, id)
	}
	pm.mu.RUnlock()

	var lastErr error
	for _, id := range ids {
		if err := pm.StopPlugin(ctx, id); err != nil {
			lastErr = err
			pm.logger.Error("failed to stop plugin",
				slog.String("id", id),
				slog.String("error", err.Error()),
			)
		}
	}
	return lastErr
}

// Status returns the current status of all managed plugins.
func (pm *PluginManager) Status() map[string]string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[string]string, len(pm.plugins))
	for id, mp := range pm.plugins {
		result[id] = mp.status
	}
	return result
}

// mcpToolAction wraps a discovered MCP tool as an Action.
type mcpToolAction struct {
	name        string
	description string
	inputSchema json.RawMessage
	plugin      *managedPlugin
}

func (a *mcpToolAction) Name() string { return a.name }

func (a *mcpToolAction) Schema() actions.ActionSchema {
	return actions.ActionSchema{
		InputSchema: a.inputSchema,
		Description: a.description,
	}
}

func (a *mcpToolAction) Validate(_ map[string]any) error { return nil }

func (a *mcpToolAction) Execute(ctx context.Context, input actions.ActionInput) (*actions.ActionOutput, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  "tools/call",
		"params": map[string]any{
			"name":      a.name,
			"arguments": input.Params,
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal tools/call: %w", err)
	}
	data = append(data, '\n')
	if _, err := a.plugin.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write tools/call: %w", err)
	}

	scanner := bufio.NewScanner(a.plugin.stdout)
	done := make(chan map[string]any, 1)

	go func() {
		if scanner.Scan() {
			var resp map[string]any
			_ = json.Unmarshal(scanner.Bytes(), &resp)
			done <- resp
		} else {
			done <- nil
		}
	}()

	var resp map[string]any
	select {
	case resp = <-done:
		if resp == nil {
			return nil, fmt.Errorf("failed to read tools/call response")
		}
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("tools/call timeout")
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if errField, exists := resp["error"]; exists {
		errJSON, _ := json.Marshal(errField)
		return nil, fmt.Errorf("plugin tool error: %s", string(errJSON))
	}

	resultJSON, _ := json.Marshal(resp["result"])
	return &actions.ActionOutput{Data: resultJSON}, nil
}
