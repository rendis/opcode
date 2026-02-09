package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rendis/opcode/internal/actions"
	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock implementations ---

// mockStore is a minimal in-memory Store for testing.
type mockStore struct {
	mu          sync.Mutex
	workflows   map[string]*store.Workflow
	stepStates  map[string]map[string]*store.StepState // wf_id -> step_id -> state
	events      []*store.Event
	decisions   []*store.PendingDecision
	wfContexts  map[string]*store.WorkflowContext
}

func newMockStore() *mockStore {
	return &mockStore{
		workflows:  make(map[string]*store.Workflow),
		stepStates: make(map[string]map[string]*store.StepState),
		wfContexts: make(map[string]*store.WorkflowContext),
	}
}

func (m *mockStore) CreateWorkflow(_ context.Context, wf *store.Workflow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workflows[wf.ID] = wf
	return nil
}

func (m *mockStore) GetWorkflow(_ context.Context, id string) (*store.Workflow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wf, ok := m.workflows[id]
	if !ok {
		return nil, nil
	}
	// Return a copy to avoid mutation.
	cp := *wf
	return &cp, nil
}

func (m *mockStore) UpdateWorkflow(_ context.Context, id string, update store.WorkflowUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	wf, ok := m.workflows[id]
	if !ok {
		return fmt.Errorf("workflow not found: %s", id)
	}
	if update.Status != nil {
		wf.Status = *update.Status
	}
	if update.StartedAt != nil {
		wf.StartedAt = update.StartedAt
	}
	if update.CompletedAt != nil {
		wf.CompletedAt = update.CompletedAt
	}
	if update.Output != nil {
		wf.Output = update.Output
	}
	if update.Error != nil {
		wf.Error = update.Error
	}
	return nil
}

func (m *mockStore) ListWorkflows(_ context.Context, _ store.WorkflowFilter) ([]*store.Workflow, error) {
	return nil, nil
}

func (m *mockStore) DeleteWorkflow(_ context.Context, _ string) error { return nil }

func (m *mockStore) AppendEvent(_ context.Context, event *store.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	event.Sequence = int64(len(m.events) + 1)
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	m.events = append(m.events, event)
	return nil
}

func (m *mockStore) GetEvents(_ context.Context, workflowID string, since int64) ([]*store.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*store.Event
	for _, e := range m.events {
		if e.WorkflowID == workflowID && e.Sequence > since {
			result = append(result, e)
		}
	}
	return result, nil
}

func (m *mockStore) GetEventsByType(_ context.Context, eventType string, filter store.EventFilter) ([]*store.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*store.Event
	for _, e := range m.events {
		if e.Type == eventType {
			if filter.WorkflowID != "" && e.WorkflowID != filter.WorkflowID {
				continue
			}
			result = append(result, e)
		}
	}
	return result, nil
}

func (m *mockStore) UpsertStepState(_ context.Context, state *store.StepState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stepStates[state.WorkflowID] == nil {
		m.stepStates[state.WorkflowID] = make(map[string]*store.StepState)
	}
	m.stepStates[state.WorkflowID][state.StepID] = state
	return nil
}

func (m *mockStore) GetStepState(_ context.Context, workflowID, stepID string) (*store.StepState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ss, ok := m.stepStates[workflowID][stepID]; ok {
		return ss, nil
	}
	return nil, nil
}

func (m *mockStore) ListStepStates(_ context.Context, workflowID string) ([]*store.StepState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*store.StepState
	for _, ss := range m.stepStates[workflowID] {
		result = append(result, ss)
	}
	return result, nil
}

func (m *mockStore) UpsertWorkflowContext(_ context.Context, wfCtx *store.WorkflowContext) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wfContexts[wfCtx.WorkflowID] = wfCtx
	return nil
}

func (m *mockStore) GetWorkflowContext(_ context.Context, workflowID string) (*store.WorkflowContext, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.wfContexts[workflowID], nil
}

func (m *mockStore) CreateDecision(_ context.Context, dec *store.PendingDecision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decisions = append(m.decisions, dec)
	return nil
}

func (m *mockStore) ResolveDecision(_ context.Context, id string, res *store.Resolution) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.decisions {
		if d.ID == id {
			resJSON, _ := json.Marshal(res)
			d.Resolution = resJSON
			d.Status = "resolved"
			now := time.Now().UTC()
			d.ResolvedAt = &now
			return nil
		}
	}
	return fmt.Errorf("decision not found: %s", id)
}

func (m *mockStore) ListPendingDecisions(_ context.Context, filter store.DecisionFilter) ([]*store.PendingDecision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*store.PendingDecision
	for _, d := range m.decisions {
		if filter.WorkflowID != "" && d.WorkflowID != filter.WorkflowID {
			continue
		}
		if filter.Status != "" && d.Status != filter.Status {
			continue
		}
		result = append(result, d)
	}
	return result, nil
}

func (m *mockStore) RegisterAgent(_ context.Context, _ *store.Agent) error    { return nil }
func (m *mockStore) GetAgent(_ context.Context, _ string) (*store.Agent, error) { return nil, nil }
func (m *mockStore) UpdateAgentSeen(_ context.Context, _ string) error         { return nil }
func (m *mockStore) StoreSecret(_ context.Context, _ string, _ []byte) error   { return nil }
func (m *mockStore) GetSecret(_ context.Context, _ string) ([]byte, error)     { return nil, nil }
func (m *mockStore) DeleteSecret(_ context.Context, _ string) error            { return nil }
func (m *mockStore) StoreTemplate(_ context.Context, _ *store.WorkflowTemplate) error { return nil }
func (m *mockStore) GetTemplate(_ context.Context, _, _ string) (*store.WorkflowTemplate, error) {
	return nil, nil
}
func (m *mockStore) ListTemplates(_ context.Context, _ store.TemplateFilter) ([]*store.WorkflowTemplate, error) {
	return nil, nil
}
func (m *mockStore) Migrate(_ context.Context) error { return nil }
func (m *mockStore) Vacuum(_ context.Context) error  { return nil }
func (m *mockStore) Close() error                    { return nil }

// mockEventLog wraps mockStore to satisfy EventAppender via AppendEvent.
type mockEventLog struct {
	store *mockStore
}

func (m *mockEventLog) AppendEvent(ctx context.Context, event *store.Event) error {
	return m.store.AppendEvent(ctx, event)
}

func (m *mockEventLog) GetEvents(ctx context.Context, workflowID string, since int64) ([]*store.Event, error) {
	return m.store.GetEvents(ctx, workflowID, since)
}

func (m *mockEventLog) GetEventsByType(ctx context.Context, eventType string, filter store.EventFilter) ([]*store.Event, error) {
	return m.store.GetEventsByType(ctx, eventType, filter)
}

func (m *mockEventLog) ReplayEvents(ctx context.Context, workflowID string) (map[string]*store.StepState, error) {
	events, err := m.store.GetEvents(ctx, workflowID, 0)
	if err != nil {
		return nil, err
	}
	states := make(map[string]*store.StepState)
	for _, e := range events {
		if e.StepID == "" {
			continue
		}
		ss, ok := states[e.StepID]
		if !ok {
			ss = &store.StepState{
				WorkflowID: workflowID,
				StepID:     e.StepID,
				Status:     schema.StepStatusPending,
			}
			states[e.StepID] = ss
		}
		switch e.Type {
		case schema.EventStepStarted:
			ss.Status = schema.StepStatusRunning
		case schema.EventStepCompleted:
			ss.Status = schema.StepStatusCompleted
			ss.Output = e.Payload
		case schema.EventStepFailed:
			ss.Status = schema.StepStatusFailed
		case schema.EventStepSkipped:
			ss.Status = schema.StepStatusSkipped
		case schema.EventStepRetrying:
			ss.Status = schema.StepStatusRetrying
			ss.RetryCount++
		case schema.EventDecisionRequested:
			ss.Status = schema.StepStatusSuspended
		}
	}
	return states, nil
}

// mockAction implements actions.Action for testing.
type mockAction struct {
	name    string
	execFn  func(ctx context.Context, input actions.ActionInput) (*actions.ActionOutput, error)
}

func (a *mockAction) Name() string                        { return a.name }
func (a *mockAction) Schema() actions.ActionSchema         { return actions.ActionSchema{} }
func (a *mockAction) Validate(_ map[string]any) error      { return nil }
func (a *mockAction) Execute(ctx context.Context, input actions.ActionInput) (*actions.ActionOutput, error) {
	if a.execFn != nil {
		return a.execFn(ctx, input)
	}
	return &actions.ActionOutput{Data: json.RawMessage(`{"ok":true}`)}, nil
}

// mockRegistry implements actions.ActionRegistry.
type mockRegistry struct {
	mu      sync.Mutex
	actions map[string]actions.Action
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{actions: make(map[string]actions.Action)}
}

func (r *mockRegistry) Register(action actions.Action) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions[action.Name()] = action
	return nil
}

func (r *mockRegistry) Get(name string) (actions.Action, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.actions[name]
	if !ok {
		return nil, fmt.Errorf("action not found: %s", name)
	}
	return a, nil
}

func (r *mockRegistry) List() []actions.ActionInfo {
	return nil
}

// --- Helper to create a test executor ---

type testEnv struct {
	store    *mockStore
	eventLog *mockEventLog
	registry *mockRegistry
	executor Executor
}

func newTestEnv() *testEnv {
	ms := newMockStore()
	mel := &mockEventLog{store: ms}
	reg := newMockRegistry()

	exec := NewExecutor(ms, mel, reg, ExecutorConfig{PoolSize: 4})

	return &testEnv{
		store:    ms,
		eventLog: mel,
		registry: reg,
		executor: exec,
	}
}

func newWorkflow(id string, steps ...schema.StepDefinition) *store.Workflow {
	return &store.Workflow{
		ID:         id,
		Status:     schema.WorkflowStatusPending,
		Definition: schema.WorkflowDefinition{Steps: steps},
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
}

func execActionStep(id, actionName string, deps ...string) schema.StepDefinition {
	return schema.StepDefinition{
		ID:        id,
		Type:      schema.StepTypeAction,
		Action:    actionName,
		DependsOn: deps,
	}
}

// --- Tests ---

func TestExecutor_Run_SingleStep(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "echo"})

	wf := newWorkflow("wf-1", execActionStep("s1", "echo"))
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
	assert.Equal(t, "wf-1", result.WorkflowID)
	assert.NotNil(t, result.CompletedAt)
	assert.Len(t, result.Steps, 1)
	assert.Equal(t, schema.StepStatusCompleted, result.Steps["s1"].Status)
}

func TestExecutor_Run_LinearChain(t *testing.T) {
	te := newTestEnv()

	callOrder := make([]string, 0)
	var mu sync.Mutex

	for _, name := range []string{"step_a", "step_b", "step_c"} {
		n := name
		te.registry.Register(&mockAction{
			name: n,
			execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
				mu.Lock()
				callOrder = append(callOrder, n)
				mu.Unlock()
				return &actions.ActionOutput{Data: json.RawMessage(`{"step":"` + n + `"}`)}, nil
			},
		})
	}

	wf := newWorkflow("wf-2",
		execActionStep("s1", "step_a"),
		execActionStep("s2", "step_b", "s1"),
		execActionStep("s3", "step_c", "s2"),
	)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
	assert.Len(t, result.Steps, 3)

	// Steps should execute in order due to dependencies.
	mu.Lock()
	assert.Equal(t, []string{"step_a", "step_b", "step_c"}, callOrder)
	mu.Unlock()
}

func TestExecutor_Run_ParallelBranches(t *testing.T) {
	te := newTestEnv()

	started := make(chan string, 3)
	done := make(chan struct{})

	for _, name := range []string{"a", "b", "c"} {
		n := name
		te.registry.Register(&mockAction{
			name: n,
			execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
				started <- n
				<-done
				return &actions.ActionOutput{Data: json.RawMessage(`{"ok":true}`)}, nil
			},
		})
	}

	// Three independent steps — should run in parallel.
	wf := newWorkflow("wf-3",
		execActionStep("s1", "a"),
		execActionStep("s2", "b"),
		execActionStep("s3", "c"),
	)
	te.store.CreateWorkflow(context.Background(), wf)

	var result *ExecutionResult
	var runErr error
	runDone := make(chan struct{})
	go func() {
		result, runErr = te.executor.Run(context.Background(), wf, nil)
		close(runDone)
	}()

	// All three should start before any finish.
	names := make(map[string]bool)
	for i := 0; i < 3; i++ {
		select {
		case n := <-started:
			names[n] = true
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for parallel step start")
		}
	}
	assert.True(t, names["a"])
	assert.True(t, names["b"])
	assert.True(t, names["c"])

	close(done) // unblock all steps.
	<-runDone
	require.NoError(t, runErr)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExecutor_Run_StepFailure(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{
		name: "fail_action",
		execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
			return nil, errors.New("action exploded")
		},
	})

	wf := newWorkflow("wf-4", execActionStep("s1", "fail_action"))
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err) // Run itself doesn't error; the result contains the failure.
	assert.Equal(t, schema.WorkflowStatusFailed, result.Status)
	assert.NotNil(t, result.Error)
	assert.Equal(t, schema.StepStatusFailed, result.Steps["s1"].Status)
}

func TestExecutor_Run_StepRetry(t *testing.T) {
	te := newTestEnv()

	callCount := 0
	te.registry.Register(&mockAction{
		name: "flaky",
		execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
			callCount++
			if callCount < 3 {
				return nil, errors.New("transient error")
			}
			return &actions.ActionOutput{Data: json.RawMessage(`{"ok":true}`)}, nil
		},
	})

	wf := newWorkflow("wf-5", schema.StepDefinition{
		ID:     "s1",
		Type:   schema.StepTypeAction,
		Action: "flaky",
		Retry:  &schema.RetryPolicy{Max: 3, Backoff: "none"},
	})
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
	assert.Equal(t, schema.StepStatusCompleted, result.Steps["s1"].Status)
	assert.Equal(t, 3, callCount)
}

func TestExecutor_Run_RetryExhausted(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{
		name: "always_fail",
		execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
			return nil, errors.New("permanent error")
		},
	})

	wf := newWorkflow("wf-6", schema.StepDefinition{
		ID:     "s1",
		Type:   schema.StepTypeAction,
		Action: "always_fail",
		Retry:  &schema.RetryPolicy{Max: 2, Backoff: "none"},
	})
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusFailed, result.Status)
	assert.NotNil(t, result.Error)
	assert.Equal(t, schema.ErrCodeRetryExhausted, result.Error.Code)
}

func TestExecutor_Run_StepTimeout(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{
		name: "slow",
		execFn: func(ctx context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
			select {
			case <-time.After(5 * time.Second):
				return &actions.ActionOutput{}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	})

	wf := newWorkflow("wf-7", schema.StepDefinition{
		ID:      "s1",
		Type:    schema.StepTypeAction,
		Action:  "slow",
		Timeout: "100ms",
	})
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusFailed, result.Status)
}

func TestExecutor_Run_WorkflowTimeout_Fail(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{
		name: "slow",
		execFn: func(ctx context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
			select {
			case <-time.After(5 * time.Second):
				return &actions.ActionOutput{}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	})

	wf := newWorkflow("wf-8", execActionStep("s1", "slow"))
	wf.Definition.Timeout = "100ms"
	wf.Definition.OnTimeout = "fail"
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusFailed, result.Status)
	assert.NotNil(t, result.Error)
	assert.Equal(t, schema.ErrCodeTimeout, result.Error.Code)
}

func TestExecutor_Run_WorkflowTimeout_Cancel(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{
		name: "slow",
		execFn: func(ctx context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
			select {
			case <-time.After(5 * time.Second):
				return &actions.ActionOutput{}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	})

	wf := newWorkflow("wf-9", execActionStep("s1", "slow"))
	wf.Definition.Timeout = "100ms"
	wf.Definition.OnTimeout = "cancel"
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCancelled, result.Status)
}

func TestExecutor_Run_WorkflowTimeout_Suspend(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{
		name: "slow",
		execFn: func(ctx context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
			select {
			case <-time.After(5 * time.Second):
				return &actions.ActionOutput{}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	})

	wf := newWorkflow("wf-10", execActionStep("s1", "slow"))
	wf.Definition.Timeout = "100ms"
	wf.Definition.OnTimeout = "suspend"
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusSuspended, result.Status)
}

func TestExecutor_Run_NilWorkflow(t *testing.T) {
	te := newTestEnv()
	_, err := te.executor.Run(context.Background(), nil, nil)
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestExecutor_Run_ActionNotFound(t *testing.T) {
	te := newTestEnv()
	// No actions registered.
	wf := newWorkflow("wf-11", execActionStep("s1", "nonexistent"))
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusFailed, result.Status)
}

func TestExecutor_Run_DiamondDependency(t *testing.T) {
	te := newTestEnv()

	executionLog := make([]string, 0, 4)
	var mu sync.Mutex

	for _, name := range []string{"root", "left", "right", "join"} {
		n := name
		te.registry.Register(&mockAction{
			name: n,
			execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
				mu.Lock()
				executionLog = append(executionLog, n)
				mu.Unlock()
				return &actions.ActionOutput{Data: json.RawMessage(`{"step":"` + n + `"}`)}, nil
			},
		})
	}

	//   root
	//   / \
	// left right
	//   \ /
	//   join
	wf := newWorkflow("wf-12",
		execActionStep("root", "root"),
		execActionStep("left", "left", "root"),
		execActionStep("right", "right", "root"),
		execActionStep("join", "join", "left", "right"),
	)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
	assert.Len(t, result.Steps, 4)

	// Verify ordering constraints.
	mu.Lock()
	rootIdx := sliceIndexOf(executionLog, "root")
	leftIdx := sliceIndexOf(executionLog, "left")
	rightIdx := sliceIndexOf(executionLog, "right")
	joinIdx := sliceIndexOf(executionLog, "join")
	mu.Unlock()

	assert.True(t, rootIdx < leftIdx, "root should run before left")
	assert.True(t, rootIdx < rightIdx, "root should run before right")
	assert.True(t, leftIdx < joinIdx, "left should run before join")
	assert.True(t, rightIdx < joinIdx, "right should run before join")
}

func TestExecutor_Cancel(t *testing.T) {
	te := newTestEnv()

	wf := &store.Workflow{
		ID:     "wf-cancel",
		Status: schema.WorkflowStatusActive,
		Definition: schema.WorkflowDefinition{
			Steps: []schema.StepDefinition{execActionStep("s1", "echo")},
		},
	}
	te.store.CreateWorkflow(context.Background(), wf)
	te.store.UpsertStepState(context.Background(), &store.StepState{
		WorkflowID: wf.ID,
		StepID:     "s1",
		Status:     schema.StepStatusPending,
	})

	err := te.executor.Cancel(context.Background(), "wf-cancel", "user requested")
	require.NoError(t, err)

	// Verify workflow is cancelled.
	updated, _ := te.store.GetWorkflow(context.Background(), "wf-cancel")
	assert.Equal(t, schema.WorkflowStatusCancelled, updated.Status)
}

func TestExecutor_Cancel_NotFound(t *testing.T) {
	te := newTestEnv()
	err := te.executor.Cancel(context.Background(), "nonexistent", "test")
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeNotFound, opErr.Code)
}

func TestExecutor_Status(t *testing.T) {
	te := newTestEnv()

	wf := &store.Workflow{
		ID:     "wf-status",
		Status: schema.WorkflowStatusActive,
		Definition: schema.WorkflowDefinition{
			Steps: []schema.StepDefinition{execActionStep("s1", "echo")},
		},
	}
	te.store.CreateWorkflow(context.Background(), wf)
	te.store.UpsertStepState(context.Background(), &store.StepState{
		WorkflowID: wf.ID,
		StepID:     "s1",
		Status:     schema.StepStatusRunning,
	})

	status, err := te.executor.Status(context.Background(), "wf-status")
	require.NoError(t, err)
	assert.Equal(t, "wf-status", status.WorkflowID)
	assert.Equal(t, schema.WorkflowStatusActive, status.Status)
	assert.NotNil(t, status.Steps["s1"])
	assert.Equal(t, schema.StepStatusRunning, status.Steps["s1"].Status)
}

func TestExecutor_Status_NotFound(t *testing.T) {
	te := newTestEnv()
	_, err := te.executor.Status(context.Background(), "nonexistent")
	require.Error(t, err)
}

func TestExecutor_Signal_NotRunning(t *testing.T) {
	te := newTestEnv()
	err := te.executor.Signal(context.Background(), "nonexistent", schema.Signal{
		Type: schema.SignalData,
	})
	require.Error(t, err)
}

func TestExecutor_Run_ContextCancellation(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{
		name: "slow",
		execFn: func(ctx context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
			select {
			case <-time.After(10 * time.Second):
				return &actions.ActionOutput{}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	})

	wf := newWorkflow("wf-cancel-ctx", execActionStep("s1", "slow"))
	te.store.CreateWorkflow(context.Background(), wf)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	result, err := te.executor.Run(ctx, wf, nil)
	require.NoError(t, err)
	// Should be failed or cancelled.
	assert.Contains(t, []schema.WorkflowStatus{
		schema.WorkflowStatusFailed,
		schema.WorkflowStatusCancelled,
	}, result.Status)
}

func TestExecutor_Run_ReasoningStep_Suspends(t *testing.T) {
	te := newTestEnv()

	reasoningConfig, _ := json.Marshal(schema.ReasoningConfig{
		PromptContext: "Choose next action",
		Options: []schema.ReasoningOption{
			{ID: "opt_a", Description: "Option A"},
			{ID: "opt_b", Description: "Option B"},
		},
		Timeout:  "1m",
		Fallback: "opt_a",
	})

	wf := newWorkflow("wf-reasoning", schema.StepDefinition{
		ID:     "decide",
		Type:   schema.StepTypeReasoning,
		Config: reasoningConfig,
	})
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusSuspended, result.Status)
	assert.Equal(t, schema.StepStatusSuspended, result.Steps["decide"].Status)

	// Verify a pending decision was created.
	decisions, _ := te.store.ListPendingDecisions(context.Background(), store.DecisionFilter{
		WorkflowID: "wf-reasoning",
		Status:     "pending",
	})
	assert.Len(t, decisions, 1)
	assert.Equal(t, "decide", decisions[0].StepID)
}

func TestExecutor_Run_StepParamsPassedToAction(t *testing.T) {
	te := newTestEnv()

	var receivedParams map[string]any
	var receivedContext map[string]any
	te.registry.Register(&mockAction{
		name: "paramcheck",
		execFn: func(_ context.Context, input actions.ActionInput) (*actions.ActionOutput, error) {
			receivedParams = input.Params
			receivedContext = input.Context
			return &actions.ActionOutput{Data: json.RawMessage(`{"ok":true}`)}, nil
		},
	})

	stepParams := json.RawMessage(`{"key":"step_value"}`)
	wf := newWorkflow("wf-params", schema.StepDefinition{
		ID:     "s1",
		Type:   schema.StepTypeAction,
		Action: "paramcheck",
		Params: stepParams,
	})
	te.store.CreateWorkflow(context.Background(), wf)

	wfParams := map[string]any{"wf_key": "wf_value"}
	result, err := te.executor.Run(context.Background(), wf, wfParams)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
	assert.Equal(t, "step_value", receivedParams["key"])
	assert.Equal(t, "wf_value", receivedContext["wf_key"])
}

func TestExecutor_Run_BackoffExponential(t *testing.T) {
	policy := &schema.RetryPolicy{
		Max:     3,
		Backoff: "exponential",
		Delay:   "10ms",
	}

	d0 := ComputeBackoff(policy, 0) // 10ms * 2^0 = 10ms
	d1 := ComputeBackoff(policy, 1) // 10ms * 2^1 = 20ms
	d2 := ComputeBackoff(policy, 2) // 10ms * 2^2 = 40ms

	assert.Equal(t, 10*time.Millisecond, d0)
	assert.Equal(t, 20*time.Millisecond, d1)
	assert.Equal(t, 40*time.Millisecond, d2)
}

func TestExecutor_Run_BackoffLinear(t *testing.T) {
	policy := &schema.RetryPolicy{
		Max:     3,
		Backoff: "linear",
		Delay:   "10ms",
	}

	d0 := ComputeBackoff(policy, 0) // 10ms * 1 = 10ms
	d1 := ComputeBackoff(policy, 1) // 10ms * 2 = 20ms
	d2 := ComputeBackoff(policy, 2) // 10ms * 3 = 30ms

	assert.Equal(t, 10*time.Millisecond, d0)
	assert.Equal(t, 20*time.Millisecond, d1)
	assert.Equal(t, 30*time.Millisecond, d2)
}

func TestExecutor_Extend_SetVariable(t *testing.T) {
	te := newTestEnv()

	wf := &store.Workflow{
		ID:     "wf-extend",
		Status: schema.WorkflowStatusActive,
	}
	te.store.CreateWorkflow(context.Background(), wf)

	err := te.executor.Extend(context.Background(), "wf-extend", schema.DAGMutation{
		Action:   schema.MutationSetVariable,
		Variable: &schema.VariableSet{Key: "env", Value: "production"},
	})
	require.NoError(t, err)

	// Verify events were emitted.
	te.store.mu.Lock()
	eventCount := len(te.store.events)
	te.store.mu.Unlock()
	assert.GreaterOrEqual(t, eventCount, 2) // dag_mutated + variable_set
}

func TestExecutor_Extend_UnsupportedMutation(t *testing.T) {
	te := newTestEnv()

	wf := &store.Workflow{
		ID:     "wf-extend2",
		Status: schema.WorkflowStatusActive,
	}
	te.store.CreateWorkflow(context.Background(), wf)

	err := te.executor.Extend(context.Background(), "wf-extend2", schema.DAGMutation{
		Action:     schema.MutationInsertAfter,
		TargetStep: "s1",
	})
	require.Error(t, err)
}

func TestExecutor_Resume_AfterSuspend(t *testing.T) {
	te := newTestEnv()

	// Register an action for non-reasoning steps.
	te.registry.Register(&mockAction{name: "process"})

	reasoningConfig, _ := json.Marshal(schema.ReasoningConfig{
		PromptContext: "Pick an option",
		Options: []schema.ReasoningOption{
			{ID: "opt_a", Description: "A"},
			{ID: "opt_b", Description: "B"},
		},
	})

	// Workflow: process → decide (reasoning) → process2
	wf := newWorkflow("wf-resume",
		execActionStep("s1", "process"),
		schema.StepDefinition{
			ID:        "decide",
			Type:      schema.StepTypeReasoning,
			Config:    reasoningConfig,
			DependsOn: []string{"s1"},
		},
		execActionStep("s3", "process", "decide"),
	)
	te.store.CreateWorkflow(context.Background(), wf)

	// Run — should suspend at reasoning step.
	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusSuspended, result.Status)
	assert.Equal(t, schema.StepStatusCompleted, result.Steps["s1"].Status)
	assert.Equal(t, schema.StepStatusSuspended, result.Steps["decide"].Status)

	// Resolve the decision via Signal (suspended workflow path).
	err = te.executor.Signal(context.Background(), "wf-resume", schema.Signal{
		Type:   schema.SignalDecision,
		StepID: "decide",
		Payload: map[string]any{
			"choice": "opt_a",
		},
		Reasoning: "A is the better option",
	})
	require.NoError(t, err)

	// Resume the workflow.
	// First update the workflow's status in the store so Resume can load it.
	// The executor's Run already persisted the suspended status.
	resumeResult, err := te.executor.Resume(context.Background(), "wf-resume")
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, resumeResult.Status)
	// s1 should be skipped (already completed), decide should not re-request.
	assert.Equal(t, schema.StepStatusCompleted, resumeResult.Steps["s1"].Status)
}

func TestExecutor_Signal_DecisionInvalidPayload(t *testing.T) {
	te := newTestEnv()

	// Create a workflow with a pending decision.
	wf := &store.Workflow{
		ID:     "wf-sig-invalid",
		Status: schema.WorkflowStatusSuspended,
	}
	te.store.CreateWorkflow(context.Background(), wf)
	te.store.CreateDecision(context.Background(), &store.PendingDecision{
		ID:         "wf-sig-invalid_s1",
		WorkflowID: "wf-sig-invalid",
		StepID:     "s1",
		Status:     "pending",
	})

	// Signal without 'choice' field should fail gracefully.
	err := te.executor.Signal(context.Background(), "wf-sig-invalid", schema.Signal{
		Type:    schema.SignalDecision,
		StepID:  "s1",
		Payload: map[string]any{"wrong_key": "value"},
	})
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestExecutor_Signal_DecisionNilPayload(t *testing.T) {
	te := newTestEnv()

	wf := &store.Workflow{
		ID:     "wf-sig-nil",
		Status: schema.WorkflowStatusSuspended,
	}
	te.store.CreateWorkflow(context.Background(), wf)
	te.store.CreateDecision(context.Background(), &store.PendingDecision{
		ID:         "wf-sig-nil_s1",
		WorkflowID: "wf-sig-nil",
		StepID:     "s1",
		Status:     "pending",
	})

	// Signal with nil payload should fail gracefully.
	err := te.executor.Signal(context.Background(), "wf-sig-nil", schema.Signal{
		Type:   schema.SignalDecision,
		StepID: "s1",
	})
	require.Error(t, err)
}

func TestExecutor_Run_MultipleFailuresInParallel(t *testing.T) {
	te := newTestEnv()

	for _, name := range []string{"fail_a", "fail_b", "fail_c"} {
		n := name
		te.registry.Register(&mockAction{
			name: n,
			execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
				return nil, fmt.Errorf("%s exploded", n)
			},
		})
	}

	wf := newWorkflow("wf-multi-fail",
		execActionStep("s1", "fail_a"),
		execActionStep("s2", "fail_b"),
		execActionStep("s3", "fail_c"),
	)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusFailed, result.Status)
	assert.NotNil(t, result.Error)

	// All steps should have states.
	for _, sid := range []string{"s1", "s2", "s3"} {
		assert.Contains(t, result.Steps, sid)
	}
}

// --- Error Handler Integration Tests ---

func TestExecutor_OnError_Ignore(t *testing.T) {
	te := newTestEnv()

	te.registry.Register(&mockAction{
		name: "fail_action",
		execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
			return nil, errors.New("something broke")
		},
	})
	te.registry.Register(&mockAction{name: "next_action"})

	// Step s1 fails but has on_error: ignore. Step s2 should still run.
	wf := newWorkflow("wf-ignore",
		schema.StepDefinition{
			ID:     "s1",
			Type:   schema.StepTypeAction,
			Action: "fail_action",
			OnError: &schema.ErrorHandler{Strategy: schema.ErrorStrategyIgnore},
		},
		schema.StepDefinition{
			ID:        "s2",
			Type:      schema.StepTypeAction,
			Action:    "next_action",
			DependsOn: []string{"s1"},
		},
	)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
	assert.Equal(t, schema.StepStatusCompleted, result.Steps["s1"].Status) // ignored = completed
	assert.Equal(t, schema.StepStatusCompleted, result.Steps["s2"].Status)
}

func TestExecutor_OnError_FailWorkflow(t *testing.T) {
	te := newTestEnv()

	te.registry.Register(&mockAction{
		name: "fail_action",
		execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
			return nil, errors.New("critical failure")
		},
	})

	wf := newWorkflow("wf-fail-wf",
		schema.StepDefinition{
			ID:     "s1",
			Type:   schema.StepTypeAction,
			Action: "fail_action",
			OnError: &schema.ErrorHandler{Strategy: schema.ErrorStrategyFailWorkflow},
		},
	)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusFailed, result.Status)
	assert.NotNil(t, result.Error)
}

func TestExecutor_OnError_FallbackStep(t *testing.T) {
	te := newTestEnv()

	te.registry.Register(&mockAction{
		name: "fail_action",
		execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
			return nil, errors.New("primary failed")
		},
	})
	te.registry.Register(&mockAction{
		name: "recovery",
		execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
			return &actions.ActionOutput{Data: json.RawMessage(`{"recovered":true}`)}, nil
		},
	})

	wf := newWorkflow("wf-fallback",
		schema.StepDefinition{
			ID:     "s1",
			Type:   schema.StepTypeAction,
			Action: "fail_action",
			OnError: &schema.ErrorHandler{
				Strategy:     schema.ErrorStrategyFallbackStep,
				FallbackStep: "recovery_step",
			},
		},
		schema.StepDefinition{
			ID:     "recovery_step",
			Type:   schema.StepTypeAction,
			Action: "recovery",
		},
	)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	// The original step s1 is failed, but recovery_step runs and workflow completes.
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
	assert.Equal(t, schema.StepStatusFailed, result.Steps["s1"].Status)
	assert.Equal(t, schema.StepStatusCompleted, result.Steps["recovery_step"].Status)
}

func TestExecutor_NonRetryableError_SkipsRetry(t *testing.T) {
	te := newTestEnv()

	callCount := 0
	te.registry.Register(&mockAction{
		name: "validation_fail",
		execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
			callCount++
			return nil, schema.NewError(schema.ErrCodeValidation, "invalid input")
		},
	})

	wf := newWorkflow("wf-noretry", schema.StepDefinition{
		ID:     "s1",
		Type:   schema.StepTypeAction,
		Action: "validation_fail",
		Retry:  &schema.RetryPolicy{Max: 3, Backoff: "none"},
	})
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusFailed, result.Status)
	// Should only be called once — validation errors are not retryable.
	assert.Equal(t, 1, callCount)
	assert.Equal(t, schema.ErrCodeNonRetryable, result.Error.Code)
}

func TestExecutor_RetryWithMaxDelay(t *testing.T) {
	te := newTestEnv()

	callCount := 0
	te.registry.Register(&mockAction{
		name: "flaky_capped",
		execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
			callCount++
			if callCount < 3 {
				return nil, errors.New("transient error")
			}
			return &actions.ActionOutput{Data: json.RawMessage(`{"ok":true}`)}, nil
		},
	})

	wf := newWorkflow("wf-maxdelay", schema.StepDefinition{
		ID:     "s1",
		Type:   schema.StepTypeAction,
		Action: "flaky_capped",
		Retry: &schema.RetryPolicy{
			Max:      5,
			Backoff:  "exponential",
			Delay:    "1ms",
			MaxDelay: "5ms",
		},
	})
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
	assert.Equal(t, 3, callCount)
}

func TestExecutor_RetryEventsLogged(t *testing.T) {
	te := newTestEnv()

	callCount := 0
	te.registry.Register(&mockAction{
		name: "flaky_logged",
		execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
			callCount++
			if callCount < 2 {
				return nil, errors.New("transient")
			}
			return &actions.ActionOutput{Data: json.RawMessage(`{"ok":true}`)}, nil
		},
	})

	wf := newWorkflow("wf-retry-events", schema.StepDefinition{
		ID:     "s1",
		Type:   schema.StepTypeAction,
		Action: "flaky_logged",
		Retry:  &schema.RetryPolicy{Max: 3, Backoff: "none"},
	})
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	// Check that retry attempt events were logged.
	te.store.mu.Lock()
	var retryAttemptEvents int
	for _, e := range te.store.events {
		if e.Type == schema.EventStepRetryAttempt {
			retryAttemptEvents++
		}
	}
	te.store.mu.Unlock()
	assert.GreaterOrEqual(t, retryAttemptEvents, 1)
}

func TestExecutor_CircuitBreaker_BlocksAfterFailures(t *testing.T) {
	cbConfig := CircuitBreakerConfig{
		FailureThreshold: 2,
		Cooldown:         10 * time.Second,
		HalfOpenMax:      1,
	}

	ms := newMockStore()
	mel := &mockEventLog{store: ms}
	reg := newMockRegistry()

	exec := NewExecutor(ms, mel, reg, ExecutorConfig{
		PoolSize:       4,
		CircuitBreaker: &cbConfig,
	})

	reg.Register(&mockAction{
		name: "fragile",
		execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
			return nil, errors.New("fragile action failed")
		},
	})

	// Run first workflow — circuit breaker records failure.
	wf1 := newWorkflow("wf-cb1", execActionStep("s1", "fragile"))
	ms.CreateWorkflow(context.Background(), wf1)
	result1, _ := exec.Run(context.Background(), wf1, nil)
	assert.Equal(t, schema.WorkflowStatusFailed, result1.Status)

	// Run second workflow — another failure, circuit opens.
	wf2 := newWorkflow("wf-cb2", execActionStep("s1", "fragile"))
	ms.CreateWorkflow(context.Background(), wf2)
	result2, _ := exec.Run(context.Background(), wf2, nil)
	assert.Equal(t, schema.WorkflowStatusFailed, result2.Status)

	// Run third workflow — circuit should be open, rejecting immediately.
	wf3 := newWorkflow("wf-cb3", execActionStep("s1", "fragile"))
	ms.CreateWorkflow(context.Background(), wf3)
	result3, _ := exec.Run(context.Background(), wf3, nil)
	assert.Equal(t, schema.WorkflowStatusFailed, result3.Status)
	// The error should be circuit-open related.
	assert.NotNil(t, result3.Error)
}

func TestExecutor_BackoffConstant(t *testing.T) {
	policy := &schema.RetryPolicy{
		Max:     3,
		Backoff: "constant",
		Delay:   "10ms",
	}

	d0 := ComputeBackoff(policy, 0)
	d1 := ComputeBackoff(policy, 1)
	d2 := ComputeBackoff(policy, 2)

	assert.Equal(t, 10*time.Millisecond, d0)
	assert.Equal(t, 10*time.Millisecond, d1)
	assert.Equal(t, 10*time.Millisecond, d2)
}

// --- Utility ---

func sliceIndexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}
