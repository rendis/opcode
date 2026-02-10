package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rendis/opcode/internal/engine"
	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock Store ---

type mockStore struct {
	store.Store // embed for unimplemented methods

	workflows  []*store.Workflow
	templates  []*store.WorkflowTemplate
	events     []*store.Event
	agents     map[string]*store.Agent
	storedTpls []*store.WorkflowTemplate

	createWorkflowFn func(ctx context.Context, wf *store.Workflow) error
}

func newMockStore() *mockStore {
	return &mockStore{
		agents: make(map[string]*store.Agent),
	}
}

func (m *mockStore) CreateWorkflow(_ context.Context, wf *store.Workflow) error {
	if m.createWorkflowFn != nil {
		return m.createWorkflowFn(context.Background(), wf)
	}
	m.workflows = append(m.workflows, wf)
	return nil
}

func (m *mockStore) GetWorkflow(_ context.Context, id string) (*store.Workflow, error) {
	for _, wf := range m.workflows {
		if wf.ID == id {
			return wf, nil
		}
	}
	return nil, schema.NewError(schema.ErrCodeNotFound, "workflow not found")
}

func (m *mockStore) ListWorkflows(_ context.Context, filter store.WorkflowFilter) ([]*store.Workflow, error) {
	result := make([]*store.Workflow, 0)
	for _, wf := range m.workflows {
		if filter.Status != nil && wf.Status != *filter.Status {
			continue
		}
		if filter.AgentID != "" && wf.AgentID != filter.AgentID {
			continue
		}
		result = append(result, wf)
	}
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

func (m *mockStore) GetTemplate(_ context.Context, name, version string) (*store.WorkflowTemplate, error) {
	for _, t := range m.templates {
		if t.Name == name && t.Version == version {
			return t, nil
		}
	}
	return nil, schema.NewError(schema.ErrCodeNotFound, "template not found")
}

func (m *mockStore) ListTemplates(_ context.Context, filter store.TemplateFilter) ([]*store.WorkflowTemplate, error) {
	result := make([]*store.WorkflowTemplate, 0)
	for _, t := range m.templates {
		if filter.Name != "" && t.Name != filter.Name {
			continue
		}
		if filter.AgentID != "" && t.AgentID != filter.AgentID {
			continue
		}
		result = append(result, t)
	}
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

func (m *mockStore) StoreTemplate(_ context.Context, tpl *store.WorkflowTemplate) error {
	m.storedTpls = append(m.storedTpls, tpl)
	m.templates = append(m.templates, tpl)
	return nil
}

func (m *mockStore) GetEvents(_ context.Context, workflowID string, _ int64) ([]*store.Event, error) {
	result := make([]*store.Event, 0)
	for _, e := range m.events {
		if workflowID != "" && e.WorkflowID != workflowID {
			continue
		}
		result = append(result, e)
	}
	return result, nil
}

func (m *mockStore) GetEventsByType(_ context.Context, _ string, filter store.EventFilter) ([]*store.Event, error) {
	result := make([]*store.Event, 0)
	for _, e := range m.events {
		if filter.WorkflowID != "" && e.WorkflowID != filter.WorkflowID {
			continue
		}
		if filter.EventType != "" && e.Type != filter.EventType {
			continue
		}
		result = append(result, e)
	}
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

func (m *mockStore) RegisterAgent(_ context.Context, agent *store.Agent) error {
	m.agents[agent.ID] = agent
	return nil
}

func (m *mockStore) GetAgent(_ context.Context, id string) (*store.Agent, error) {
	if a, ok := m.agents[id]; ok {
		return a, nil
	}
	return nil, schema.NewError(schema.ErrCodeNotFound, "agent not found")
}

func (m *mockStore) UpdateAgentSeen(_ context.Context, id string) error {
	if a, ok := m.agents[id]; ok {
		now := time.Now().UTC()
		a.LastSeenAt = &now
	}
	return nil
}

// --- Mock Executor ---

type mockExecutor struct {
	runResult    *engine.ExecutionResult
	runErr       error
	statusResult *engine.WorkflowStatus
	statusErr    error
	signalErr    error
}

func (m *mockExecutor) Run(_ context.Context, _ *store.Workflow, _ map[string]any) (*engine.ExecutionResult, error) {
	return m.runResult, m.runErr
}

func (m *mockExecutor) Resume(_ context.Context, _ string) (*engine.ExecutionResult, error) {
	return nil, nil
}

func (m *mockExecutor) Signal(_ context.Context, _ string, _ schema.Signal) error {
	return m.signalErr
}

func (m *mockExecutor) Extend(_ context.Context, _ string, _ schema.DAGMutation) error {
	return nil
}

func (m *mockExecutor) Cancel(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *mockExecutor) Status(_ context.Context, _ string) (*engine.WorkflowStatus, error) {
	return m.statusResult, m.statusErr
}

// --- Helper ---

func buildRequest(toolName string, args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: args,
		},
	}
}

// --- Tests ---

func TestRunTool(t *testing.T) {
	ms := newMockStore()
	ms.templates = []*store.WorkflowTemplate{
		{
			Name:    "deploy",
			Version: "v1",
			Definition: schema.WorkflowDefinition{
				Steps: []schema.StepDefinition{{ID: "s1", Action: "http.request"}},
			},
		},
	}

	now := time.Now().UTC()
	exec := &mockExecutor{
		runResult: &engine.ExecutionResult{
			WorkflowID: "wf-123",
			Status:     schema.WorkflowStatusCompleted,
			StartedAt:  now,
		},
	}

	s := NewOpcodeServer(OpcodeServerDeps{
		Executor: exec,
		Store:    ms,
	})

	req := buildRequest("opcode.run", map[string]any{
		"template_name": "deploy",
		"agent_id":      "agent-1",
		"params":        map[string]any{"env": "prod"},
	})

	result, err := s.handleRun(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)

	// Verify workflow was created.
	require.Len(t, ms.workflows, 1)
	assert.Equal(t, "deploy", ms.workflows[0].TemplateName)
	assert.Equal(t, "v1", ms.workflows[0].TemplateVersion)

	// Verify agent was registered.
	_, agentFound := ms.agents["agent-1"]
	assert.True(t, agentFound)
}

func TestRunToolLatestVersion(t *testing.T) {
	ms := newMockStore()
	ms.templates = []*store.WorkflowTemplate{
		{Name: "deploy", Version: "v1"},
		{Name: "deploy", Version: "v3"},
		{Name: "deploy", Version: "v2"},
	}

	exec := &mockExecutor{
		runResult: &engine.ExecutionResult{WorkflowID: "wf-1", Status: schema.WorkflowStatusCompleted},
	}

	s := NewOpcodeServer(OpcodeServerDeps{Executor: exec, Store: ms})

	req := buildRequest("opcode.run", map[string]any{
		"template_name": "deploy",
		"agent_id":      "agent-1",
	})

	result, err := s.handleRun(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	// Should have picked v3 (latest).
	require.Len(t, ms.workflows, 1)
	assert.Equal(t, "v3", ms.workflows[0].TemplateVersion)
}

func TestRunToolMissingTemplate(t *testing.T) {
	ms := newMockStore()
	exec := &mockExecutor{}

	s := NewOpcodeServer(OpcodeServerDeps{Executor: exec, Store: ms})

	req := buildRequest("opcode.run", map[string]any{
		"template_name": "nonexistent",
		"agent_id":      "agent-1",
	})

	result, err := s.handleRun(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestRunToolMissingParams(t *testing.T) {
	s := NewOpcodeServer(OpcodeServerDeps{})

	// Missing template_name.
	req := buildRequest("opcode.run", map[string]any{"agent_id": "a"})
	result, err := s.handleRun(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.IsError)

	// Missing agent_id.
	req = buildRequest("opcode.run", map[string]any{"template_name": "x"})
	result, err = s.handleRun(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestStatusTool(t *testing.T) {
	exec := &mockExecutor{
		statusResult: &engine.WorkflowStatus{
			WorkflowID: "wf-123",
			Status:     schema.WorkflowStatusActive,
		},
	}

	s := NewOpcodeServer(OpcodeServerDeps{Executor: exec})

	req := buildRequest("opcode.status", map[string]any{
		"workflow_id": "wf-123",
	})

	result, err := s.handleStatus(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	// Parse result to verify content.
	text := extractText(t, result)
	assert.Contains(t, text, "wf-123")
	assert.Contains(t, text, "active")
}

func TestStatusToolMissingID(t *testing.T) {
	s := NewOpcodeServer(OpcodeServerDeps{})

	req := buildRequest("opcode.status", map[string]any{})
	result, err := s.handleStatus(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestStatusToolNotFound(t *testing.T) {
	exec := &mockExecutor{
		statusErr: schema.NewError(schema.ErrCodeNotFound, "not found"),
	}

	s := NewOpcodeServer(OpcodeServerDeps{Executor: exec})

	req := buildRequest("opcode.status", map[string]any{"workflow_id": "missing"})
	result, err := s.handleStatus(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestSignalTool(t *testing.T) {
	exec := &mockExecutor{}
	s := NewOpcodeServer(OpcodeServerDeps{Executor: exec})

	req := buildRequest("opcode.signal", map[string]any{
		"workflow_id": "wf-123",
		"signal_type": "decision",
		"payload":     map[string]any{"choice": "approve"},
		"step_id":     "step-1",
		"agent_id":    "agent-1",
		"reasoning":   "looks good",
	})

	result, err := s.handleSignal(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	text := extractText(t, result)
	assert.Contains(t, text, "wf-123")
	assert.Contains(t, text, "decision")
}

func TestSignalToolMissingParams(t *testing.T) {
	s := NewOpcodeServer(OpcodeServerDeps{})

	// Missing workflow_id.
	req := buildRequest("opcode.signal", map[string]any{"signal_type": "data", "payload": map[string]any{}})
	result, err := s.handleSignal(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.IsError)

	// Missing signal_type.
	req = buildRequest("opcode.signal", map[string]any{"workflow_id": "x", "payload": map[string]any{}})
	result, err = s.handleSignal(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestSignalToolError(t *testing.T) {
	exec := &mockExecutor{
		signalErr: schema.NewError(schema.ErrCodeSignalFailed, "workflow not suspended"),
	}
	s := NewOpcodeServer(OpcodeServerDeps{Executor: exec})

	req := buildRequest("opcode.signal", map[string]any{
		"workflow_id": "wf-1",
		"signal_type": "cancel",
		"payload":     map[string]any{},
	})

	result, err := s.handleSignal(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestDefineTool(t *testing.T) {
	ms := newMockStore()
	s := NewOpcodeServer(OpcodeServerDeps{Store: ms})

	req := buildRequest("opcode.define", map[string]any{
		"name": "my-workflow",
		"definition": map[string]any{
			"steps": []any{
				map[string]any{"id": "s1", "action": "http.request"},
			},
		},
		"agent_id":    "agent-1",
		"description": "test template",
	})

	result, err := s.handleDefine(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	// Verify template stored.
	require.Len(t, ms.storedTpls, 1)
	assert.Equal(t, "my-workflow", ms.storedTpls[0].Name)
	assert.Equal(t, "v1", ms.storedTpls[0].Version)
	assert.Equal(t, "test template", ms.storedTpls[0].Description)

	text := extractText(t, result)
	assert.Contains(t, text, "my-workflow")
	assert.Contains(t, text, "v1")
}

func TestDefineToolVersionIncrement(t *testing.T) {
	ms := newMockStore()
	ms.templates = []*store.WorkflowTemplate{
		{Name: "deploy", Version: "v1"},
		{Name: "deploy", Version: "v2"},
	}

	s := NewOpcodeServer(OpcodeServerDeps{Store: ms})

	req := buildRequest("opcode.define", map[string]any{
		"name": "deploy",
		"definition": map[string]any{
			"steps": []any{map[string]any{"id": "s1", "action": "noop"}},
		},
		"agent_id": "agent-1",
	})

	result, err := s.handleDefine(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	// Should be v3.
	require.Len(t, ms.storedTpls, 1)
	assert.Equal(t, "v3", ms.storedTpls[0].Version)
}

func TestDefineToolMissingParams(t *testing.T) {
	s := NewOpcodeServer(OpcodeServerDeps{})

	// Missing name.
	req := buildRequest("opcode.define", map[string]any{
		"agent_id":   "a",
		"definition": map[string]any{},
	})
	result, err := s.handleDefine(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.IsError)

	// Missing agent_id.
	req = buildRequest("opcode.define", map[string]any{
		"name":       "x",
		"definition": map[string]any{},
	})
	result, err = s.handleDefine(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestQueryWorkflows(t *testing.T) {
	now := time.Now().UTC()
	ms := newMockStore()
	ms.workflows = []*store.Workflow{
		{ID: "wf-1", Status: schema.WorkflowStatusCompleted, AgentID: "a1", CreatedAt: now},
		{ID: "wf-2", Status: schema.WorkflowStatusActive, AgentID: "a1", CreatedAt: now},
		{ID: "wf-3", Status: schema.WorkflowStatusCompleted, AgentID: "a2", CreatedAt: now},
	}

	s := NewOpcodeServer(OpcodeServerDeps{Store: ms})

	// Query all.
	req := buildRequest("opcode.query", map[string]any{
		"resource": "workflows",
	})
	result, err := s.handleQuery(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var workflows []store.Workflow
	unmarshalResult(t, result, &workflows)
	assert.Len(t, workflows, 3)

	// Query with status filter.
	req = buildRequest("opcode.query", map[string]any{
		"resource": "workflows",
		"filter":   map[string]any{"status": "completed"},
	})
	result, err = s.handleQuery(context.Background(), req)
	require.NoError(t, err)
	unmarshalResult(t, result, &workflows)
	assert.Len(t, workflows, 2)
}

func TestQueryEvents(t *testing.T) {
	now := time.Now().UTC()
	ms := newMockStore()
	ms.events = []*store.Event{
		{ID: 1, WorkflowID: "wf-1", Type: "step_started", Timestamp: now},
		{ID: 2, WorkflowID: "wf-1", Type: "step_completed", Timestamp: now},
		{ID: 3, WorkflowID: "wf-2", Type: "step_started", Timestamp: now},
	}

	s := NewOpcodeServer(OpcodeServerDeps{Store: ms})

	// All events for workflow.
	req := buildRequest("opcode.query", map[string]any{
		"resource": "events",
		"filter":   map[string]any{"workflow_id": "wf-1"},
	})
	result, err := s.handleQuery(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var events []store.Event
	unmarshalResult(t, result, &events)
	assert.Len(t, events, 2)
}

func TestQueryTemplates(t *testing.T) {
	ms := newMockStore()
	ms.templates = []*store.WorkflowTemplate{
		{Name: "deploy", Version: "v1", AgentID: "a1"},
		{Name: "deploy", Version: "v2", AgentID: "a1"},
		{Name: "cleanup", Version: "v1", AgentID: "a2"},
	}

	s := NewOpcodeServer(OpcodeServerDeps{Store: ms})

	// Filter by name.
	req := buildRequest("opcode.query", map[string]any{
		"resource": "templates",
		"filter":   map[string]any{"name": "deploy"},
	})
	result, err := s.handleQuery(context.Background(), req)
	require.NoError(t, err)

	var templates []store.WorkflowTemplate
	unmarshalResult(t, result, &templates)
	assert.Len(t, templates, 2)
}

func TestQueryUnknownResource(t *testing.T) {
	s := NewOpcodeServer(OpcodeServerDeps{})

	req := buildRequest("opcode.query", map[string]any{
		"resource": "invalid",
	})
	result, err := s.handleQuery(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestVersionNum(t *testing.T) {
	assert.Equal(t, 1, versionNum("v1"))
	assert.Equal(t, 42, versionNum("v42"))
	assert.Equal(t, 0, versionNum("invalid"))
	assert.Equal(t, 3, versionNum("3"))
}

// --- Test helpers ---

func extractText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, result.Content)
	return mcp.GetTextFromContent(result.Content[0])
}

func unmarshalResult(t *testing.T, result *mcp.CallToolResult, target any) {
	t.Helper()
	text := extractText(t, result)
	require.NoError(t, json.Unmarshal([]byte(text), target))
}
