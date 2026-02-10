package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/internal/streaming"
	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock store for workflow.context tests ---

type wfMockStore struct {
	store.Store // embed to satisfy interface; only override what we need
	ctx         *store.WorkflowContext
	upserted    *store.WorkflowContext
	getErr      error
	upsertErr   error
}

func (m *wfMockStore) GetWorkflowContext(_ context.Context, _ string) (*store.WorkflowContext, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.ctx, nil
}

func (m *wfMockStore) UpsertWorkflowContext(_ context.Context, wfCtx *store.WorkflowContext) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	m.upserted = wfCtx
	return nil
}

// --- Tests ---

func TestWorkflowRun(t *testing.T) {
	called := false
	expectedOutput := json.RawMessage(`{"child":"result"}`)

	deps := WorkflowActionDeps{
		RunSubWorkflow: func(_ context.Context, templateName, version string, params map[string]any, parentID, agentID string) (json.RawMessage, error) {
			called = true
			assert.Equal(t, "my-template", templateName)
			assert.Equal(t, "v2", version)
			assert.Equal(t, map[string]any{"key": "val"}, params)
			assert.Equal(t, "parent-wf", parentID)
			assert.Equal(t, "agent-1", agentID)
			return expectedOutput, nil
		},
	}

	action := &workflowRunAction{deps: deps}
	assert.Equal(t, "workflow.run", action.Name())

	out, err := action.Execute(context.Background(), ActionInput{
		Params: map[string]any{
			"template_name": "my-template",
			"version":       "v2",
			"params":        map[string]any{"key": "val"},
		},
		Context: map[string]any{
			"workflow_id": "parent-wf",
			"agent_id":    "agent-1",
		},
	})

	require.NoError(t, err)
	assert.True(t, called)
	assert.JSONEq(t, `{"child":"result"}`, string(out.Data))
}

func TestWorkflowRunValidation(t *testing.T) {
	action := &workflowRunAction{}
	err := action.Validate(map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "template_name")

	err = action.Validate(map[string]any{"template_name": "ok"})
	assert.NoError(t, err)
}

func TestWorkflowRunNilRunner(t *testing.T) {
	action := &workflowRunAction{deps: WorkflowActionDeps{}}
	_, err := action.Execute(context.Background(), ActionInput{
		Params: map[string]any{"template_name": "t"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestWorkflowEmit(t *testing.T) {
	hub := streaming.NewMemoryHub()
	ch, cancel, err := hub.Subscribe(context.Background(), streaming.EventFilter{})
	require.NoError(t, err)
	defer cancel()

	action := &workflowEmitAction{deps: WorkflowActionDeps{Hub: hub}}
	assert.Equal(t, "workflow.emit", action.Name())

	out, err := action.Execute(context.Background(), ActionInput{
		Params: map[string]any{
			"event_type": "custom.event",
			"payload":    map[string]any{"data": 42},
		},
		Context: map[string]any{
			"workflow_id": "wf-1",
			"step_id":     "step-1",
		},
	})

	require.NoError(t, err)
	assert.JSONEq(t, `{"emitted":true}`, string(out.Data))

	// Verify event was published
	select {
	case evt := <-ch:
		assert.Equal(t, "wf-1", evt.WorkflowID)
		assert.Equal(t, "step-1", evt.StepID)
		assert.Equal(t, "custom.event", evt.EventType)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestWorkflowEmitValidation(t *testing.T) {
	action := &workflowEmitAction{}
	err := action.Validate(map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "event_type")

	err = action.Validate(map[string]any{"event_type": "ok"})
	assert.NoError(t, err)
}

func TestWorkflowEmitNilHub(t *testing.T) {
	action := &workflowEmitAction{deps: WorkflowActionDeps{}}
	_, err := action.Execute(context.Background(), ActionInput{
		Params: map[string]any{"event_type": "x"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestWorkflowContextRead(t *testing.T) {
	wfCtx := &store.WorkflowContext{
		WorkflowID:     "wf-1",
		AgentID:        "agent-1",
		OriginalIntent: "test intent",
		AgentNotes:     "some notes",
	}
	mockStore := &wfMockStore{ctx: wfCtx}

	action := &workflowContextAction{deps: WorkflowActionDeps{Store: mockStore}}
	assert.Equal(t, "workflow.context", action.Name())

	out, err := action.Execute(context.Background(), ActionInput{
		Params: map[string]any{
			"action":      "read",
			"workflow_id": "wf-1",
		},
	})

	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal(out.Data, &result))
	assert.Equal(t, "wf-1", result["workflow_id"])
	assert.Equal(t, "test intent", result["original_intent"])
}

func TestWorkflowContextUpdate(t *testing.T) {
	mockStore := &wfMockStore{}

	action := &workflowContextAction{deps: WorkflowActionDeps{Store: mockStore}}

	out, err := action.Execute(context.Background(), ActionInput{
		Params: map[string]any{
			"action":      "update",
			"workflow_id": "wf-1",
			"data":        map[string]any{"key": "value"},
			"agent_notes": "updated notes",
		},
		Context: map[string]any{
			"agent_id": "agent-1",
		},
	})

	require.NoError(t, err)
	assert.JSONEq(t, `{"updated":true,"workflow_id":"wf-1"}`, string(out.Data))

	require.NotNil(t, mockStore.upserted)
	assert.Equal(t, "wf-1", mockStore.upserted.WorkflowID)
	assert.Equal(t, "agent-1", mockStore.upserted.AgentID)
	assert.Equal(t, "updated notes", mockStore.upserted.AgentNotes)
	assert.JSONEq(t, `{"key":"value"}`, string(mockStore.upserted.AccumulatedData))
}

func TestWorkflowContextValidation(t *testing.T) {
	action := &workflowContextAction{}

	// Missing action
	err := action.Validate(map[string]any{"workflow_id": "wf-1"})
	assert.Error(t, err)

	// Invalid action
	err = action.Validate(map[string]any{"action": "delete", "workflow_id": "wf-1"})
	assert.Error(t, err)

	// Missing workflow_id
	err = action.Validate(map[string]any{"action": "read"})
	assert.Error(t, err)

	// Valid
	err = action.Validate(map[string]any{"action": "read", "workflow_id": "wf-1"})
	assert.NoError(t, err)
}

func TestWorkflowContextNilStore(t *testing.T) {
	action := &workflowContextAction{deps: WorkflowActionDeps{}}
	_, err := action.Execute(context.Background(), ActionInput{
		Params: map[string]any{"action": "read", "workflow_id": "wf-1"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestWorkflowFail(t *testing.T) {
	action := &workflowFailAction{}
	assert.Equal(t, "workflow.fail", action.Name())

	_, err := action.Execute(context.Background(), ActionInput{
		Params: map[string]any{"reason": "something went wrong"},
	})

	require.Error(t, err)
	var opcErr *schema.OpcodeError
	require.ErrorAs(t, err, &opcErr)
	assert.Equal(t, schema.ErrCodeNonRetryable, opcErr.Code)
	assert.Equal(t, "something went wrong", opcErr.Message)
	assert.False(t, opcErr.IsRetryable())
}

func TestWorkflowFailValidation(t *testing.T) {
	action := &workflowFailAction{}
	err := action.Validate(map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reason")

	err = action.Validate(map[string]any{"reason": "boom"})
	assert.NoError(t, err)
}

func TestWorkflowLog(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	action := &workflowLogAction{deps: WorkflowActionDeps{Logger: logger}}
	assert.Equal(t, "workflow.log", action.Name())

	out, err := action.Execute(context.Background(), ActionInput{
		Params: map[string]any{
			"level":   "info",
			"message": "processing step",
			"data":    map[string]any{"count": 42},
		},
		Context: map[string]any{
			"workflow_id": "wf-1",
			"step_id":     "step-1",
		},
	})

	require.NoError(t, err)
	assert.JSONEq(t, `{"logged":true}`, string(out.Data))

	// Verify the log was written with correct fields
	logged := buf.String()
	assert.Contains(t, logged, "processing step")
	assert.Contains(t, logged, "wf-1")
	assert.Contains(t, logged, "step-1")
}

func TestWorkflowLogLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		t.Run(level, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

			action := &workflowLogAction{deps: WorkflowActionDeps{Logger: logger}}
			out, err := action.Execute(context.Background(), ActionInput{
				Params: map[string]any{
					"level":   level,
					"message": "test " + level,
				},
			})

			require.NoError(t, err)
			assert.JSONEq(t, `{"logged":true}`, string(out.Data))
			assert.Contains(t, buf.String(), "test "+level)
		})
	}
}

func TestWorkflowLogValidation(t *testing.T) {
	action := &workflowLogAction{}
	err := action.Validate(map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "message")

	err = action.Validate(map[string]any{"message": "ok"})
	assert.NoError(t, err)
}

func TestWorkflowLogDefaultLogger(t *testing.T) {
	action := &workflowLogAction{deps: WorkflowActionDeps{}}
	out, err := action.Execute(context.Background(), ActionInput{
		Params: map[string]any{
			"message": "test default logger",
		},
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"logged":true}`, string(out.Data))
}

func TestRegisterWorkflowActions(t *testing.T) {
	reg := NewRegistry()
	hub := streaming.NewMemoryHub()

	deps := WorkflowActionDeps{
		Hub:    hub,
		Logger: slog.Default(),
	}

	err := RegisterWorkflowActions(reg, deps)
	require.NoError(t, err)

	assert.True(t, reg.Has("workflow.run"))
	assert.True(t, reg.Has("workflow.emit"))
	assert.True(t, reg.Has("workflow.context"))
	assert.True(t, reg.Has("workflow.fail"))
	assert.True(t, reg.Has("workflow.log"))
	assert.Equal(t, 5, reg.Count())
}
