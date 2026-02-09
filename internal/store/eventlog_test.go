package store

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rendis/opcode/pkg/schema"
)

func newTestEventLog(t *testing.T) (*EventLog, *LibSQLStore) {
	t.Helper()
	s := newTestStore(t)
	return NewEventLog(s), s
}

func seedWorkflow(t *testing.T, s *LibSQLStore) (*Agent, *Workflow) {
	t.Helper()
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
	return agent, wf
}

func TestEventLog_AppendEvent_MonotonicSequence(t *testing.T) {
	el, s := newTestEventLog(t)
	ctx := context.Background()
	_, wf := seedWorkflow(t, s)

	for i := 0; i < 5; i++ {
		e := &Event{
			WorkflowID: wf.ID,
			StepID:     "s1",
			Type:       schema.EventStepStarted,
		}
		require.NoError(t, el.AppendEvent(ctx, e))
		assert.Equal(t, int64(i+1), e.Sequence, "sequence should be monotonic")
	}
}

func TestEventLog_GetEvents(t *testing.T) {
	el, s := newTestEventLog(t)
	ctx := context.Background()
	_, wf := seedWorkflow(t, s)

	for _, et := range []string{schema.EventStepStarted, schema.EventStepCompleted, schema.EventStepFailed} {
		require.NoError(t, el.AppendEvent(ctx, &Event{
			WorkflowID: wf.ID, StepID: "s1", Type: et,
		}))
	}

	// Get all
	events, err := el.GetEvents(ctx, wf.ID, 0)
	require.NoError(t, err)
	assert.Len(t, events, 3)

	// Get since sequence 1
	events, err = el.GetEvents(ctx, wf.ID, 1)
	require.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Equal(t, int64(2), events[0].Sequence)
}

func TestEventLog_GetEventsByType(t *testing.T) {
	el, s := newTestEventLog(t)
	ctx := context.Background()
	_, wf := seedWorkflow(t, s)

	require.NoError(t, el.AppendEvent(ctx, &Event{WorkflowID: wf.ID, StepID: "s1", Type: schema.EventStepStarted}))
	require.NoError(t, el.AppendEvent(ctx, &Event{WorkflowID: wf.ID, StepID: "s1", Type: schema.EventStepCompleted}))
	require.NoError(t, el.AppendEvent(ctx, &Event{WorkflowID: wf.ID, StepID: "s2", Type: schema.EventStepStarted}))

	events, err := el.GetEventsByType(ctx, schema.EventStepStarted, EventFilter{WorkflowID: wf.ID})
	require.NoError(t, err)
	assert.Len(t, events, 2)
	for _, e := range events {
		assert.Equal(t, schema.EventStepStarted, e.Type)
	}
}

func TestEventLog_ReplayEvents_FullLifecycle(t *testing.T) {
	el, s := newTestEventLog(t)
	ctx := context.Background()
	_, wf := seedWorkflow(t, s)

	now := time.Now().UTC()

	// s1: started -> completed
	require.NoError(t, el.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s1", Type: schema.EventStepStarted, Timestamp: now,
	}))
	require.NoError(t, el.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s1", Type: schema.EventStepCompleted,
		Payload:   json.RawMessage(`{"result":"ok"}`),
		Timestamp: now.Add(100 * time.Millisecond),
	}))

	// s2: started -> failed
	require.NoError(t, el.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s2", Type: schema.EventStepStarted, Timestamp: now,
	}))
	require.NoError(t, el.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s2", Type: schema.EventStepFailed,
		Payload:   json.RawMessage(`{"error":"timeout"}`),
		Timestamp: now.Add(200 * time.Millisecond),
	}))

	states, err := el.ReplayEvents(ctx, wf.ID)
	require.NoError(t, err)
	require.Len(t, states, 2)

	// s1 should be completed
	assert.Equal(t, schema.StepStatusCompleted, states["s1"].Status)
	assert.NotNil(t, states["s1"].CompletedAt)
	assert.NotNil(t, states["s1"].StartedAt)
	assert.JSONEq(t, `{"result":"ok"}`, string(states["s1"].Output))
	assert.Greater(t, states["s1"].DurationMs, int64(0))

	// s2 should be failed
	assert.Equal(t, schema.StepStatusFailed, states["s2"].Status)
	assert.JSONEq(t, `{"error":"timeout"}`, string(states["s2"].Error))
}

func TestEventLog_ReplayEvents_Skipped(t *testing.T) {
	el, s := newTestEventLog(t)
	ctx := context.Background()
	_, wf := seedWorkflow(t, s)

	require.NoError(t, el.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s1", Type: schema.EventStepSkipped,
	}))

	states, err := el.ReplayEvents(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, schema.StepStatusSkipped, states["s1"].Status)
}

func TestEventLog_ReplayEvents_DecisionSuspend(t *testing.T) {
	el, s := newTestEventLog(t)
	ctx := context.Background()
	_, wf := seedWorkflow(t, s)

	require.NoError(t, el.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s1", Type: schema.EventStepStarted,
	}))
	require.NoError(t, el.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s1", Type: schema.EventDecisionRequested,
	}))

	states, err := el.ReplayEvents(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, schema.StepStatusSuspended, states["s1"].Status)
}

func TestEventLog_ReplayEvents_DecisionResolved(t *testing.T) {
	el, s := newTestEventLog(t)
	ctx := context.Background()
	_, wf := seedWorkflow(t, s)

	require.NoError(t, el.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s1", Type: schema.EventStepStarted,
	}))
	require.NoError(t, el.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s1", Type: schema.EventDecisionRequested,
	}))
	require.NoError(t, el.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s1", Type: schema.EventDecisionResolved,
		Payload: json.RawMessage(`{"choice":"a"}`),
	}))

	states, err := el.ReplayEvents(ctx, wf.ID)
	require.NoError(t, err)
	// Status remains suspended because the decision_resolved event doesn't transition;
	// the executor does that separately.
	assert.Equal(t, schema.StepStatusSuspended, states["s1"].Status)
}

func TestEventLog_ReplayEvents_Retrying(t *testing.T) {
	el, s := newTestEventLog(t)
	ctx := context.Background()
	_, wf := seedWorkflow(t, s)

	require.NoError(t, el.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s1", Type: schema.EventStepStarted,
	}))
	require.NoError(t, el.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s1", Type: schema.EventStepRetrying,
	}))
	require.NoError(t, el.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s1", Type: schema.EventStepStarted,
	}))
	require.NoError(t, el.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s1", Type: schema.EventStepCompleted,
		Payload: json.RawMessage(`{"ok":true}`),
	}))

	states, err := el.ReplayEvents(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, schema.StepStatusCompleted, states["s1"].Status)
	assert.Equal(t, 1, states["s1"].RetryCount)
}

func TestEventLog_ReplayEvents_EmptyWorkflow(t *testing.T) {
	el, s := newTestEventLog(t)
	ctx := context.Background()
	_, wf := seedWorkflow(t, s)

	states, err := el.ReplayEvents(ctx, wf.ID)
	require.NoError(t, err)
	assert.Empty(t, states)
}

func TestEventLog_ReplayEvents_SequenceGap(t *testing.T) {
	el, s := newTestEventLog(t)
	ctx := context.Background()
	_, wf := seedWorkflow(t, s)

	// Manually insert events with a gap using the raw store.
	db := s.DB()
	_, err := db.ExecContext(ctx,
		`INSERT INTO events (workflow_id, step_id, event_type, timestamp, sequence) VALUES (?, 's1', 'step_started', CURRENT_TIMESTAMP, 1)`,
		wf.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO events (workflow_id, step_id, event_type, timestamp, sequence) VALUES (?, 's1', 'step_completed', CURRENT_TIMESTAMP, 3)`,
		wf.ID)
	require.NoError(t, err)

	_, err = el.ReplayEvents(ctx, wf.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sequence gap")
}

func TestEventLog_ConcurrentAppend_DifferentWorkflows(t *testing.T) {
	el, s := newTestEventLog(t)
	ctx := context.Background()

	// Create multiple workflows
	var workflows []*Workflow
	for i := 0; i < 5; i++ {
		_, wf := seedWorkflow(t, s)
		workflows = append(workflows, wf)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 50)

	for _, wf := range workflows {
		wf := wf
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				e := &Event{
					WorkflowID: wf.ID,
					StepID:     "s1",
					Type:       schema.EventStepStarted,
				}
				if err := el.AppendEvent(ctx, e); err != nil {
					errCh <- err
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent append error: %v", err)
	}

	// Verify each workflow has correct sequences 1..10
	for _, wf := range workflows {
		events, err := el.GetEvents(ctx, wf.ID, 0)
		require.NoError(t, err)
		assert.Len(t, events, 10)
		for i, e := range events {
			assert.Equal(t, int64(i+1), e.Sequence)
		}
	}
}

func TestEventLog_WorkflowScopedSequences(t *testing.T) {
	el, s := newTestEventLog(t)
	ctx := context.Background()

	_, wf1 := seedWorkflow(t, s)
	_, wf2 := seedWorkflow(t, s)

	// Append to wf1
	require.NoError(t, el.AppendEvent(ctx, &Event{WorkflowID: wf1.ID, StepID: "s1", Type: schema.EventStepStarted}))
	require.NoError(t, el.AppendEvent(ctx, &Event{WorkflowID: wf1.ID, StepID: "s1", Type: schema.EventStepCompleted}))

	// Append to wf2 — sequence should start at 1, not 3
	e := &Event{WorkflowID: wf2.ID, StepID: "s1", Type: schema.EventStepStarted}
	require.NoError(t, el.AppendEvent(ctx, e))
	assert.Equal(t, int64(1), e.Sequence, "wf2 should have its own sequence starting at 1")
}

func TestEventLog_ImmutableEvents(t *testing.T) {
	el, s := newTestEventLog(t)
	ctx := context.Background()
	_, wf := seedWorkflow(t, s)

	require.NoError(t, el.AppendEvent(ctx, &Event{
		WorkflowID: wf.ID, StepID: "s1", Type: schema.EventStepStarted,
		Payload: json.RawMessage(`{"original":true}`),
	}))

	// Verify we can read it back unchanged
	events, err := el.GetEvents(ctx, wf.ID, 0)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.JSONEq(t, `{"original":true}`, string(events[0].Payload))
}
