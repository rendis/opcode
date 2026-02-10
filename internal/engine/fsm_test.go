package engine

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
)

// mockAppender records appended events for assertions.
type mockAppender struct {
	mu     sync.Mutex
	events []*store.Event
}

func (m *mockAppender) AppendEvent(_ context.Context, event *store.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *mockAppender) Events() []*store.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]*store.Event, len(m.events))
	copy(cp, m.events)
	return cp
}

// failAppender always returns an error.
type failAppender struct{}

func (f *failAppender) AppendEvent(_ context.Context, _ *store.Event) error {
	return errors.New("store unavailable")
}

// --- WorkflowFSM Tests ---

func TestWorkflowFSM_ValidTransitions(t *testing.T) {
	app := &mockAppender{}
	fsm := NewWorkflowFSM(app)
	ctx := context.Background()
	wfID := "wf-1"

	// pending -> active
	require.NoError(t, fsm.Transition(ctx, wfID, schema.WorkflowStatusPending, schema.WorkflowStatusActive))
	// active -> suspended
	require.NoError(t, fsm.Transition(ctx, wfID, schema.WorkflowStatusActive, schema.WorkflowStatusSuspended))
	// suspended -> active (resume)
	require.NoError(t, fsm.Transition(ctx, wfID, schema.WorkflowStatusSuspended, schema.WorkflowStatusActive))
	// active -> completed
	require.NoError(t, fsm.Transition(ctx, wfID, schema.WorkflowStatusActive, schema.WorkflowStatusCompleted))

	events := app.Events()
	assert.Len(t, events, 4)
	assert.Equal(t, schema.EventWorkflowStarted, events[0].Type)
	assert.Equal(t, schema.EventWorkflowSuspended, events[1].Type)
	assert.Equal(t, schema.EventWorkflowStarted, events[2].Type) // resumed = started again
	assert.Equal(t, schema.EventWorkflowCompleted, events[3].Type)
}

func TestWorkflowFSM_InvalidTransition(t *testing.T) {
	app := &mockAppender{}
	fsm := NewWorkflowFSM(app)
	ctx := context.Background()

	err := fsm.Transition(ctx, "wf-1", schema.WorkflowStatusPending, schema.WorkflowStatusCompleted)
	require.Error(t, err)

	opcErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeInvalidTransition, opcErr.Code)
	assert.Contains(t, opcErr.Message, "pending")
	assert.Contains(t, opcErr.Message, "completed")

	// No events should have been emitted
	assert.Empty(t, app.Events())
}

func TestWorkflowFSM_TerminalStatesRejectTransitions(t *testing.T) {
	app := &mockAppender{}
	fsm := NewWorkflowFSM(app)
	ctx := context.Background()

	for _, terminal := range []schema.WorkflowStatus{
		schema.WorkflowStatusCompleted,
		schema.WorkflowStatusFailed,
		schema.WorkflowStatusCancelled,
	} {
		err := fsm.Transition(ctx, "wf-1", terminal, schema.WorkflowStatusActive)
		require.Error(t, err, "should not transition from terminal state %s", terminal)
	}
}

func TestWorkflowFSM_EventEmitFailure(t *testing.T) {
	fsm := NewWorkflowFSM(&failAppender{})
	ctx := context.Background()

	err := fsm.Transition(ctx, "wf-1", schema.WorkflowStatusPending, schema.WorkflowStatusActive)
	require.Error(t, err)

	opcErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeStore, opcErr.Code)
}

func TestWorkflowFSM_BeforeHook(t *testing.T) {
	app := &mockAppender{}
	fsm := NewWorkflowFSM(app)
	ctx := context.Background()

	var hookCalled bool
	fsm.OnBefore(schema.WorkflowStatusPending, schema.WorkflowStatusActive, func(from, to string) error {
		hookCalled = true
		assert.Equal(t, "pending", from)
		assert.Equal(t, "active", to)
		return nil
	})

	require.NoError(t, fsm.Transition(ctx, "wf-1", schema.WorkflowStatusPending, schema.WorkflowStatusActive))
	assert.True(t, hookCalled)
}

func TestWorkflowFSM_BeforeHookError(t *testing.T) {
	app := &mockAppender{}
	fsm := NewWorkflowFSM(app)
	ctx := context.Background()

	fsm.OnBefore(schema.WorkflowStatusPending, schema.WorkflowStatusActive, func(from, to string) error {
		return errors.New("hook failed")
	})

	err := fsm.Transition(ctx, "wf-1", schema.WorkflowStatusPending, schema.WorkflowStatusActive)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hook failed")
	// Event should NOT have been emitted since before hook failed.
	assert.Empty(t, app.Events())
}

func TestWorkflowFSM_AfterHook(t *testing.T) {
	app := &mockAppender{}
	fsm := NewWorkflowFSM(app)
	ctx := context.Background()

	var hookCalled bool
	fsm.OnAfter(schema.WorkflowStatusPending, schema.WorkflowStatusActive, func(from, to string) error {
		hookCalled = true
		return nil
	})

	require.NoError(t, fsm.Transition(ctx, "wf-1", schema.WorkflowStatusPending, schema.WorkflowStatusActive))
	assert.True(t, hookCalled)
	// Event should have been emitted before the after hook.
	assert.Len(t, app.Events(), 1)
}

func TestWorkflowFSM_CancelFromMultipleStates(t *testing.T) {
	app := &mockAppender{}
	fsm := NewWorkflowFSM(app)
	ctx := context.Background()

	for _, from := range []schema.WorkflowStatus{
		schema.WorkflowStatusPending,
		schema.WorkflowStatusActive,
		schema.WorkflowStatusSuspended,
	} {
		require.NoError(t, fsm.Transition(ctx, "wf-"+string(from), from, schema.WorkflowStatusCancelled))
	}
	assert.Len(t, app.Events(), 3)
}

// --- StepFSM Tests ---

func TestStepFSM_ValidTransitions(t *testing.T) {
	app := &mockAppender{}
	fsm := NewStepFSM(app)
	ctx := context.Background()
	wfID := "wf-1"

	// pending -> scheduled -> running -> completed
	require.NoError(t, fsm.Transition(ctx, wfID, "s1", schema.StepStatusPending, schema.StepStatusScheduled))
	require.NoError(t, fsm.Transition(ctx, wfID, "s1", schema.StepStatusScheduled, schema.StepStatusRunning))
	require.NoError(t, fsm.Transition(ctx, wfID, "s1", schema.StepStatusRunning, schema.StepStatusCompleted))

	events := app.Events()
	// pending->scheduled has no event type (StepStatusScheduled is not mapped)
	// scheduled->running emits step_started, running->completed emits step_completed
	assert.Len(t, events, 2)
	assert.Equal(t, schema.EventStepStarted, events[0].Type)
	assert.Equal(t, schema.EventStepCompleted, events[1].Type)
	assert.Equal(t, "s1", events[0].StepID)
}

func TestStepFSM_InvalidTransition(t *testing.T) {
	app := &mockAppender{}
	fsm := NewStepFSM(app)
	ctx := context.Background()

	err := fsm.Transition(ctx, "wf-1", "s1", schema.StepStatusPending, schema.StepStatusCompleted)
	require.Error(t, err)

	opcErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeInvalidTransition, opcErr.Code)
	assert.Equal(t, "s1", opcErr.StepID)
}

func TestStepFSM_RetryPath(t *testing.T) {
	app := &mockAppender{}
	fsm := NewStepFSM(app)
	ctx := context.Background()

	// running -> retrying -> running -> completed
	require.NoError(t, fsm.Transition(ctx, "wf-1", "s1", schema.StepStatusRunning, schema.StepStatusRetrying))
	require.NoError(t, fsm.Transition(ctx, "wf-1", "s1", schema.StepStatusRetrying, schema.StepStatusRunning))
	require.NoError(t, fsm.Transition(ctx, "wf-1", "s1", schema.StepStatusRunning, schema.StepStatusCompleted))

	events := app.Events()
	assert.Equal(t, schema.EventStepRetrying, events[0].Type)
	assert.Equal(t, schema.EventStepStarted, events[1].Type)
	assert.Equal(t, schema.EventStepCompleted, events[2].Type)
}

func TestStepFSM_SuspendAndResume(t *testing.T) {
	app := &mockAppender{}
	fsm := NewStepFSM(app)
	ctx := context.Background()

	// running -> suspended -> running -> completed
	require.NoError(t, fsm.Transition(ctx, "wf-1", "s1", schema.StepStatusRunning, schema.StepStatusSuspended))
	require.NoError(t, fsm.Transition(ctx, "wf-1", "s1", schema.StepStatusSuspended, schema.StepStatusRunning))
	require.NoError(t, fsm.Transition(ctx, "wf-1", "s1", schema.StepStatusRunning, schema.StepStatusCompleted))

	events := app.Events()
	assert.Equal(t, schema.EventStepSuspended, events[0].Type)
}

func TestStepFSM_TerminalStatesRejectTransitions(t *testing.T) {
	app := &mockAppender{}
	fsm := NewStepFSM(app)
	ctx := context.Background()

	for _, terminal := range []schema.StepStatus{
		schema.StepStatusCompleted,
		schema.StepStatusFailed,
		schema.StepStatusSkipped,
	} {
		err := fsm.Transition(ctx, "wf-1", "s1", terminal, schema.StepStatusRunning)
		require.Error(t, err, "should not transition from terminal state %s", terminal)
	}
}

func TestStepFSM_Hooks(t *testing.T) {
	app := &mockAppender{}
	fsm := NewStepFSM(app)
	ctx := context.Background()

	var order []string

	fsm.OnBefore(schema.StepStatusPending, schema.StepStatusScheduled, func(from, to string) error {
		order = append(order, "before")
		return nil
	})
	fsm.OnAfter(schema.StepStatusPending, schema.StepStatusScheduled, func(from, to string) error {
		order = append(order, "after")
		return nil
	})

	require.NoError(t, fsm.Transition(ctx, "wf-1", "s1", schema.StepStatusPending, schema.StepStatusScheduled))
	assert.Equal(t, []string{"before", "after"}, order)
}

// --- CancelWorkflow Tests ---

func TestCancelWorkflow_CascadeSkipsNonTerminal(t *testing.T) {
	app := &mockAppender{}
	wfFSM := NewWorkflowFSM(app)
	stepFSM := NewStepFSM(app)
	ctx := context.Background()

	stepStates := map[string]schema.StepStatus{
		"s1": schema.StepStatusCompleted, // terminal — should not be touched
		"s2": schema.StepStatusRunning,   // non-terminal, can skip (cancelled decision)
		"s3": schema.StepStatusPending,   // non-terminal, can skip
		"s4": schema.StepStatusScheduled, // non-terminal, can skip
		"s5": schema.StepStatusSuspended, // non-terminal, can skip
	}

	err := CancelWorkflow(ctx, wfFSM, stepFSM, "wf-1", schema.WorkflowStatusActive, stepStates)
	require.NoError(t, err)

	events := app.Events()
	var eventTypes []string
	for _, e := range events {
		eventTypes = append(eventTypes, e.Type)
	}
	assert.Contains(t, eventTypes, schema.EventWorkflowCancelled)
	// s2 (running -> skipped), s3 (pending -> skipped), s4 (scheduled -> skipped), s5 (suspended -> skipped)
	skipCount := 0
	for _, e := range events {
		if e.Type == schema.EventStepSkipped {
			skipCount++
		}
	}
	assert.Equal(t, 4, skipCount, "should skip s2, s3, s4, s5 (not s1/completed)")
}

func TestCancelWorkflow_FromSuspended(t *testing.T) {
	app := &mockAppender{}
	wfFSM := NewWorkflowFSM(app)
	stepFSM := NewStepFSM(app)
	ctx := context.Background()

	stepStates := map[string]schema.StepStatus{
		"s1": schema.StepStatusSuspended,
	}

	require.NoError(t, CancelWorkflow(ctx, wfFSM, stepFSM, "wf-1", schema.WorkflowStatusSuspended, stepStates))
	events := app.Events()
	assert.Len(t, events, 2) // cancelled + skipped
}

func TestCancelWorkflow_AlreadyTerminal(t *testing.T) {
	app := &mockAppender{}
	wfFSM := NewWorkflowFSM(app)
	stepFSM := NewStepFSM(app)
	ctx := context.Background()

	err := CancelWorkflow(ctx, wfFSM, stepFSM, "wf-1", schema.WorkflowStatusCompleted, nil)
	require.Error(t, err) // completed can't transition to cancelled
}

// --- Thread Safety ---

func TestWorkflowFSM_ConcurrentTransitions(t *testing.T) {
	app := &mockAppender{}
	fsm := NewWorkflowFSM(app)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// All are valid transitions, just testing no data race
			_ = fsm.Transition(ctx, "wf-concurrent", schema.WorkflowStatusPending, schema.WorkflowStatusActive)
		}(i)
	}
	wg.Wait()
	// All transitions should succeed or fail gracefully with no panics
}

func TestStepFSM_ConcurrentTransitions(t *testing.T) {
	app := &mockAppender{}
	fsm := NewStepFSM(app)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = fsm.Transition(ctx, "wf-concurrent", "s1", schema.StepStatusPending, schema.StepStatusScheduled)
		}(i)
	}
	wg.Wait()
}

// --- Transition Table Completeness ---

func TestWorkflowTransitionTable_AllStatusesPresent(t *testing.T) {
	expected := []schema.WorkflowStatus{
		schema.WorkflowStatusPending,
		schema.WorkflowStatusActive,
		schema.WorkflowStatusSuspended,
		schema.WorkflowStatusCompleted,
		schema.WorkflowStatusFailed,
		schema.WorkflowStatusCancelled,
	}
	for _, s := range expected {
		_, ok := ValidWorkflowTransitions[s]
		assert.True(t, ok, "missing workflow status %q in transition table", s)
	}
}

func TestStepTransitionTable_AllStatusesPresent(t *testing.T) {
	expected := []schema.StepStatus{
		schema.StepStatusPending,
		schema.StepStatusScheduled,
		schema.StepStatusRunning,
		schema.StepStatusCompleted,
		schema.StepStatusFailed,
		schema.StepStatusSkipped,
		schema.StepStatusSuspended,
		schema.StepStatusRetrying,
	}
	for _, s := range expected {
		_, ok := ValidStepTransitions[s]
		assert.True(t, ok, "missing step status %q in transition table", s)
	}
}
