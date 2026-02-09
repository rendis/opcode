package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rendis/opcode/pkg/schema"
)

func newTestStore(t *testing.T) *LibSQLStore {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := NewLibSQLStore("file:" + dbPath)
	require.NoError(t, err)
	require.NoError(t, s.Migrate(context.Background()))
	t.Cleanup(func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	})
	return s
}

func seedAgent(t *testing.T, s *LibSQLStore) *Agent {
	t.Helper()
	a := &Agent{
		ID:   uuid.New().String(),
		Name: "test-agent",
		Type: "system",
	}
	require.NoError(t, s.RegisterAgent(context.Background(), a))
	return a
}

// --- Agent Tests ---

func TestRegisterAndGetAgent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a := &Agent{
		ID:       uuid.New().String(),
		Name:     "agent-1",
		Type:     "llm",
		Metadata: json.RawMessage(`{"model":"gpt-4"}`),
	}
	require.NoError(t, s.RegisterAgent(ctx, a))

	got, err := s.GetAgent(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, a.ID, got.ID)
	assert.Equal(t, "agent-1", got.Name)
	assert.Equal(t, "llm", got.Type)
	assert.JSONEq(t, `{"model":"gpt-4"}`, string(got.Metadata))
}

func TestGetAgent_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetAgent(context.Background(), "nonexistent")
	require.Error(t, err)
	opcErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeNotFound, opcErr.Code)
}

func TestUpdateAgentSeen(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := seedAgent(t, s)

	require.NoError(t, s.UpdateAgentSeen(ctx, a.ID))

	got, err := s.GetAgent(ctx, a.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.LastSeenAt)
}

// --- Workflow Tests ---

func TestCreateAndGetWorkflow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	agent := seedAgent(t, s)

	wf := &Workflow{
		ID:      uuid.New().String(),
		Name:    "test-workflow",
		Status:  schema.WorkflowStatusPending,
		AgentID: agent.ID,
		Definition: schema.WorkflowDefinition{
			Steps: []schema.StepDefinition{{ID: "step1", Action: "http.request"}},
		},
		InputParams: map[string]any{"key": "value"},
	}
	require.NoError(t, s.CreateWorkflow(ctx, wf))

	got, err := s.GetWorkflow(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, wf.ID, got.ID)
	assert.Equal(t, "test-workflow", got.Name)
	assert.Equal(t, schema.WorkflowStatusPending, got.Status)
	assert.Equal(t, agent.ID, got.AgentID)
	assert.Len(t, got.Definition.Steps, 1)
	assert.Equal(t, "value", got.InputParams["key"])
}

func TestUpdateWorkflow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	agent := seedAgent(t, s)

	wf := &Workflow{
		ID:      uuid.New().String(),
		Status:  schema.WorkflowStatusPending,
		AgentID: agent.ID,
		Definition: schema.WorkflowDefinition{
			Steps: []schema.StepDefinition{{ID: "s1"}},
		},
	}
	require.NoError(t, s.CreateWorkflow(ctx, wf))

	active := schema.WorkflowStatusActive
	now := time.Now().UTC()
	require.NoError(t, s.UpdateWorkflow(ctx, wf.ID, WorkflowUpdate{
		Status:    &active,
		StartedAt: &now,
	}))

	got, err := s.GetWorkflow(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusActive, got.Status)
	assert.NotNil(t, got.StartedAt)
}

func TestListWorkflows(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	agent := seedAgent(t, s)

	for i := 0; i < 3; i++ {
		wf := &Workflow{
			ID:      uuid.New().String(),
			Status:  schema.WorkflowStatusPending,
			AgentID: agent.ID,
			Definition: schema.WorkflowDefinition{
				Steps: []schema.StepDefinition{{ID: "s1"}},
			},
		}
		require.NoError(t, s.CreateWorkflow(ctx, wf))
	}

	list, err := s.ListWorkflows(ctx, WorkflowFilter{})
	require.NoError(t, err)
	assert.Len(t, list, 3)

	// Filter by status
	pending := schema.WorkflowStatusPending
	list, err = s.ListWorkflows(ctx, WorkflowFilter{Status: &pending, Limit: 2})
	require.NoError(t, err)
	assert.Len(t, list, 2)
}

func TestDeleteWorkflow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	agent := seedAgent(t, s)

	wf := &Workflow{
		ID:      uuid.New().String(),
		Status:  schema.WorkflowStatusPending,
		AgentID: agent.ID,
		Definition: schema.WorkflowDefinition{
			Steps: []schema.StepDefinition{{ID: "s1"}},
		},
	}
	require.NoError(t, s.CreateWorkflow(ctx, wf))
	require.NoError(t, s.DeleteWorkflow(ctx, wf.ID))

	_, err := s.GetWorkflow(ctx, wf.ID)
	require.Error(t, err)
}

// --- Event Tests ---

func TestAppendAndGetEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	agent := seedAgent(t, s)

	wf := &Workflow{
		ID:      uuid.New().String(),
		Status:  schema.WorkflowStatusActive,
		AgentID: agent.ID,
		Definition: schema.WorkflowDefinition{
			Steps: []schema.StepDefinition{{ID: "s1"}},
		},
	}
	require.NoError(t, s.CreateWorkflow(ctx, wf))

	// Append 3 events
	for i := 0; i < 3; i++ {
		e := &Event{
			WorkflowID: wf.ID,
			StepID:     "s1",
			Type:       schema.EventStepStarted,
			Payload:    json.RawMessage(`{"attempt":` + string(rune('0'+i)) + `}`),
		}
		require.NoError(t, s.AppendEvent(ctx, e))
		assert.Equal(t, int64(i+1), e.Sequence)
	}

	// Get all events
	events, err := s.GetEvents(ctx, wf.ID, 0)
	require.NoError(t, err)
	assert.Len(t, events, 3)
	assert.Equal(t, int64(1), events[0].Sequence)
	assert.Equal(t, int64(3), events[2].Sequence)

	// Get since sequence 2
	events, err = s.GetEvents(ctx, wf.ID, 2)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, int64(3), events[0].Sequence)
}

func TestGetEventsByType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	agent := seedAgent(t, s)

	wf := &Workflow{
		ID:      uuid.New().String(),
		Status:  schema.WorkflowStatusActive,
		AgentID: agent.ID,
		Definition: schema.WorkflowDefinition{
			Steps: []schema.StepDefinition{{ID: "s1"}},
		},
	}
	require.NoError(t, s.CreateWorkflow(ctx, wf))

	require.NoError(t, s.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s1", Type: schema.EventStepStarted,
	}))
	require.NoError(t, s.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s1", Type: schema.EventStepCompleted,
	}))

	events, err := s.GetEventsByType(ctx, schema.EventStepStarted, EventFilter{WorkflowID: wf.ID})
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, schema.EventStepStarted, events[0].Type)
}

// --- Step State Tests ---

func TestUpsertAndGetStepState(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	agent := seedAgent(t, s)

	wf := &Workflow{
		ID:      uuid.New().String(),
		Status:  schema.WorkflowStatusActive,
		AgentID: agent.ID,
		Definition: schema.WorkflowDefinition{
			Steps: []schema.StepDefinition{{ID: "s1"}},
		},
	}
	require.NoError(t, s.CreateWorkflow(ctx, wf))

	ss := &StepState{
		WorkflowID: wf.ID,
		StepID:     "s1",
		Status:     schema.StepStatusPending,
	}
	require.NoError(t, s.UpsertStepState(ctx, ss))

	got, err := s.GetStepState(ctx, wf.ID, "s1")
	require.NoError(t, err)
	assert.Equal(t, schema.StepStatusPending, got.Status)

	// Update it
	now := time.Now().UTC()
	ss.Status = schema.StepStatusRunning
	ss.StartedAt = &now
	require.NoError(t, s.UpsertStepState(ctx, ss))

	got, err = s.GetStepState(ctx, wf.ID, "s1")
	require.NoError(t, err)
	assert.Equal(t, schema.StepStatusRunning, got.Status)
	assert.NotNil(t, got.StartedAt)
}

func TestListStepStates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	agent := seedAgent(t, s)

	wf := &Workflow{
		ID:      uuid.New().String(),
		Status:  schema.WorkflowStatusActive,
		AgentID: agent.ID,
		Definition: schema.WorkflowDefinition{
			Steps: []schema.StepDefinition{{ID: "s1"}, {ID: "s2"}},
		},
	}
	require.NoError(t, s.CreateWorkflow(ctx, wf))

	require.NoError(t, s.UpsertStepState(ctx, &StepState{WorkflowID: wf.ID, StepID: "s1", Status: schema.StepStatusPending}))
	require.NoError(t, s.UpsertStepState(ctx, &StepState{WorkflowID: wf.ID, StepID: "s2", Status: schema.StepStatusRunning}))

	states, err := s.ListStepStates(ctx, wf.ID)
	require.NoError(t, err)
	assert.Len(t, states, 2)
}

// --- Workflow Context Tests ---

func TestUpsertAndGetWorkflowContext(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	agent := seedAgent(t, s)

	wf := &Workflow{
		ID:      uuid.New().String(),
		Status:  schema.WorkflowStatusActive,
		AgentID: agent.ID,
		Definition: schema.WorkflowDefinition{
			Steps: []schema.StepDefinition{{ID: "s1"}},
		},
	}
	require.NoError(t, s.CreateWorkflow(ctx, wf))

	wfCtx := &WorkflowContext{
		WorkflowID:      wf.ID,
		AgentID:         agent.ID,
		OriginalIntent:  "deploy service",
		DecisionsLog:    json.RawMessage(`[]`),
		AccumulatedData: json.RawMessage(`{"step1":"done"}`),
		AgentNotes:      "proceeding well",
	}
	require.NoError(t, s.UpsertWorkflowContext(ctx, wfCtx))

	got, err := s.GetWorkflowContext(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, "deploy service", got.OriginalIntent)
	assert.Equal(t, "proceeding well", got.AgentNotes)
	assert.JSONEq(t, `{"step1":"done"}`, string(got.AccumulatedData))
}

// --- Pending Decisions Tests ---

func TestCreateAndResolveDecision(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	agent := seedAgent(t, s)

	wf := &Workflow{
		ID:      uuid.New().String(),
		Status:  schema.WorkflowStatusActive,
		AgentID: agent.ID,
		Definition: schema.WorkflowDefinition{
			Steps: []schema.StepDefinition{{ID: "s1"}},
		},
	}
	require.NoError(t, s.CreateWorkflow(ctx, wf))

	dec := &PendingDecision{
		ID:         uuid.New().String(),
		WorkflowID: wf.ID,
		StepID:     "s1",
		AgentID:    agent.ID,
		Context:    json.RawMessage(`{"question":"which path?"}`),
		Options:    json.RawMessage(`["a","b"]`),
		Status:     "pending",
	}
	require.NoError(t, s.CreateDecision(ctx, dec))

	// List pending
	decs, err := s.ListPendingDecisions(ctx, DecisionFilter{WorkflowID: wf.ID, Status: "pending"})
	require.NoError(t, err)
	assert.Len(t, decs, 1)

	// Resolve
	require.NoError(t, s.ResolveDecision(ctx, dec.ID, &Resolution{
		Choice:    "a",
		Reasoning: "seems best",
	}))

	// Verify resolved
	decs, err = s.ListPendingDecisions(ctx, DecisionFilter{WorkflowID: wf.ID, Status: "pending"})
	require.NoError(t, err)
	assert.Len(t, decs, 0)

	decs, err = s.ListPendingDecisions(ctx, DecisionFilter{WorkflowID: wf.ID, Status: "resolved"})
	require.NoError(t, err)
	assert.Len(t, decs, 1)
	assert.Equal(t, "resolved", decs[0].Status)
}

// --- Secrets Tests ---

func TestStoreAndGetSecret(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.StoreSecret(ctx, "api-key", []byte("secret123")))

	val, err := s.GetSecret(ctx, "api-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("secret123"), val)

	// Overwrite
	require.NoError(t, s.StoreSecret(ctx, "api-key", []byte("updated")))
	val, err = s.GetSecret(ctx, "api-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("updated"), val)

	// Delete
	require.NoError(t, s.DeleteSecret(ctx, "api-key"))
	_, err = s.GetSecret(ctx, "api-key")
	require.Error(t, err)
}

// --- Template Tests ---

func TestStoreAndGetTemplate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tpl := &WorkflowTemplate{
		Name:    "deploy",
		Version: "1.0.0",
		Description: "deployment workflow",
		Definition: schema.WorkflowDefinition{
			Steps: []schema.StepDefinition{{ID: "build"}, {ID: "deploy"}},
		},
		AgentID: "system",
	}
	require.NoError(t, s.StoreTemplate(ctx, tpl))

	got, err := s.GetTemplate(ctx, "deploy", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "deploy", got.Name)
	assert.Equal(t, "1.0.0", got.Version)
	assert.Len(t, got.Definition.Steps, 2)
}

func TestListTemplates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, v := range []string{"1.0.0", "2.0.0"} {
		require.NoError(t, s.StoreTemplate(ctx, &WorkflowTemplate{
			Name:    "deploy",
			Version: v,
			Definition: schema.WorkflowDefinition{
				Steps: []schema.StepDefinition{{ID: "s1"}},
			},
			AgentID: "system",
		}))
	}

	list, err := s.ListTemplates(ctx, TemplateFilter{Name: "deploy"})
	require.NoError(t, err)
	assert.Len(t, list, 2)
}

// --- Migration Tests ---

func TestMigrateIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// Migrate was already called in newTestStore; calling again should be a no-op.
	require.NoError(t, s.Migrate(ctx))
}

func TestVacuum(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Vacuum(context.Background()))
}
