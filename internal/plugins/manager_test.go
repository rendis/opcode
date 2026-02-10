package plugins

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rendis/opcode/internal/actions"
	"github.com/rendis/opcode/internal/store"
)

// mockPluginStore satisfies store.Store for plugin manager tests.
// Only plugin methods are implemented; others use the embedded interface (panic on use).
type mockPluginStore struct {
	store.Store
	plugins map[string]*store.Plugin
}

func newMockPluginStore() *mockPluginStore {
	return &mockPluginStore{plugins: make(map[string]*store.Plugin)}
}

func (m *mockPluginStore) CreatePlugin(_ context.Context, p *store.Plugin) error {
	m.plugins[p.ID] = p
	return nil
}

func (m *mockPluginStore) GetPlugin(_ context.Context, id string) (*store.Plugin, error) {
	p, ok := m.plugins[id]
	if !ok {
		return nil, nil
	}
	return p, nil
}

func (m *mockPluginStore) UpdatePlugin(_ context.Context, id, status, errMsg string) error {
	p, ok := m.plugins[id]
	if !ok {
		return nil
	}
	p.Status = status
	p.ErrorMessage = errMsg
	return nil
}

func (m *mockPluginStore) ListPlugins(_ context.Context) ([]*store.Plugin, error) {
	var result []*store.Plugin
	for _, p := range m.plugins {
		result = append(result, p)
	}
	return result, nil
}

func TestNewPluginManager(t *testing.T) {
	s := newMockPluginStore()
	reg := actions.NewRegistry()
	logger := slog.Default()

	pm := NewPluginManager(s, reg, logger)
	require.NotNil(t, pm)
	assert.Empty(t, pm.Status())
}

func TestLoadPlugin_InvalidCommand(t *testing.T) {
	s := newMockPluginStore()
	reg := actions.NewRegistry()
	logger := slog.Default()
	pm := NewPluginManager(s, reg, logger)

	err := pm.LoadPlugin(context.Background(), PluginConfig{
		ID:      "test-1",
		Name:    "bad-plugin",
		Command: "/nonexistent/binary/path",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start plugin")
}

func TestLoadPlugin_DuplicateID(t *testing.T) {
	s := newMockPluginStore()
	reg := actions.NewRegistry()
	logger := slog.Default()
	pm := NewPluginManager(s, reg, logger)

	// Manually add a fake plugin to simulate existing.
	pm.mu.Lock()
	pm.plugins["dup-1"] = &managedPlugin{
		config: PluginConfig{ID: "dup-1"},
		status: "healthy",
	}
	pm.mu.Unlock()

	err := pm.LoadPlugin(context.Background(), PluginConfig{
		ID:      "dup-1",
		Name:    "duplicate",
		Command: "echo",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already loaded")
}

func TestStopPlugin_NotFound(t *testing.T) {
	s := newMockPluginStore()
	reg := actions.NewRegistry()
	logger := slog.Default()
	pm := NewPluginManager(s, reg, logger)

	err := pm.StopPlugin(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestPluginStatus(t *testing.T) {
	s := newMockPluginStore()
	reg := actions.NewRegistry()
	logger := slog.Default()
	pm := NewPluginManager(s, reg, logger)

	// Manually add plugins.
	pm.mu.Lock()
	pm.plugins["p1"] = &managedPlugin{
		config: PluginConfig{ID: "p1"},
		status: "healthy",
	}
	pm.plugins["p2"] = &managedPlugin{
		config: PluginConfig{ID: "p2"},
		status: "unhealthy",
	}
	pm.mu.Unlock()

	status := pm.Status()
	assert.Len(t, status, 2)
	assert.Equal(t, "healthy", status["p1"])
	assert.Equal(t, "unhealthy", status["p2"])
}

func TestStopAll_Empty(t *testing.T) {
	s := newMockPluginStore()
	reg := actions.NewRegistry()
	logger := slog.Default()
	pm := NewPluginManager(s, reg, logger)

	err := pm.StopAll(context.Background())
	require.NoError(t, err)
}

func TestDiscoverActions_NotFound(t *testing.T) {
	s := newMockPluginStore()
	reg := actions.NewRegistry()
	logger := slog.Default()
	pm := NewPluginManager(s, reg, logger)

	err := pm.DiscoverActions(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestMcpToolAction_Schema(t *testing.T) {
	action := &mcpToolAction{
		name:        "test-tool",
		description: "A test tool",
		inputSchema: []byte(`{"type":"object"}`),
	}

	assert.Equal(t, "test-tool", action.Name())
	schema := action.Schema()
	assert.Equal(t, "A test tool", schema.Description)
	assert.JSONEq(t, `{"type":"object"}`, string(schema.InputSchema))
}

func TestHealthCheckStatus(t *testing.T) {
	mp := &managedPlugin{
		config: PluginConfig{ID: "health-test"},
		status: "healthy",
	}

	// Simulate consecutive errors.
	mp.errCount = 2
	mp.lastErr = "connection timeout"
	mp.errCount++

	if mp.errCount >= 3 {
		mp.status = "unhealthy"
	}

	assert.Equal(t, "unhealthy", mp.status)
	assert.Equal(t, 3, mp.errCount)
}
