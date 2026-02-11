package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rendis/opcode/internal/actions"
	"github.com/rendis/opcode/internal/expressions"
	"github.com/rendis/opcode/internal/reasoning"
	"github.com/rendis/opcode/internal/secrets"
	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
)

// Executor is the central workflow execution coordinator.
type Executor interface {
	// Run starts a new workflow from a persisted Workflow record.
	Run(ctx context.Context, wf *store.Workflow, params map[string]any) (*ExecutionResult, error)

	// Resume continues a suspended or interrupted workflow from its last checkpoint.
	// Replays events to rebuild state, then continues from first pending step.
	// Reasoning nodes are NEVER replayed — stored decisions are injected.
	Resume(ctx context.Context, workflowID string) (*ExecutionResult, error)

	// Signal delivers an agent message to a suspended workflow.
	Signal(ctx context.Context, workflowID string, signal schema.Signal) error

	// Extend mutates the DAG of a running workflow in-flight.
	Extend(ctx context.Context, workflowID string, mutation schema.DAGMutation) error

	// Cancel terminates a workflow with a reason, cascading to active steps.
	Cancel(ctx context.Context, workflowID string, reason string) error

	// Status returns the current state of a workflow.
	Status(ctx context.Context, workflowID string) (*WorkflowStatus, error)
}

// ExecutionResult is returned by Run and Resume with the workflow outcome.
type ExecutionResult struct {
	WorkflowID  string                 `json:"workflow_id"`
	Status      schema.WorkflowStatus  `json:"status"`
	Output      json.RawMessage        `json:"output,omitempty"`
	Error       *schema.OpcodeError    `json:"error,omitempty"`
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Steps       map[string]*StepResult `json:"steps,omitempty"`
}

// StepResult summarizes the outcome of a single step.
type StepResult struct {
	StepID     string            `json:"step_id"`
	Status     schema.StepStatus `json:"status"`
	Output     json.RawMessage   `json:"output,omitempty"`
	Error      *schema.OpcodeError `json:"error,omitempty"`
	DurationMs int64             `json:"duration_ms,omitempty"`
}

// WorkflowStatus is a snapshot of a workflow's current state for querying.
type WorkflowStatus struct {
	WorkflowID       string                      `json:"workflow_id"`
	Status           schema.WorkflowStatus       `json:"status"`
	Steps            map[string]*store.StepState  `json:"steps,omitempty"`
	PendingDecisions []*store.PendingDecision     `json:"pending_decisions,omitempty"`
	Context          *store.WorkflowContext       `json:"context,omitempty"`
	Events           []*store.Event               `json:"events,omitempty"`
}

// EventLogger abstracts the event log operations needed by the executor.
// Satisfied by *store.EventLog and test mocks.
type EventLogger interface {
	EventAppender
	GetEvents(ctx context.Context, workflowID string, since int64) ([]*store.Event, error)
	GetEventsByType(ctx context.Context, eventType string, filter store.EventFilter) ([]*store.Event, error)
	ReplayEvents(ctx context.Context, workflowID string) (map[string]*store.StepState, error)
}

// DefaultPoolSize is the default worker pool concurrency.
const DefaultPoolSize = 10

// DefaultOnTimeout is the default behavior when a workflow-level timeout fires.
const DefaultOnTimeout = "fail"

// ExecutorConfig holds configuration for the executor.
type ExecutorConfig struct {
	PoolSize       int                  // max concurrent step goroutines
	CircuitBreaker *CircuitBreakerConfig // circuit breaker config (nil = defaults)
}

// executorImpl is the concrete Executor implementation.
type executorImpl struct {
	store        store.Store
	eventLog     EventLogger
	wfFSM        *WorkflowFSM
	stepFSM      *StepFSM
	actions      actions.ActionRegistry
	pool         *WorkerPool
	config       ExecutorConfig
	circuitBkr   *CircuitBreakerRegistry
	interpolator *expressions.Interpolator
	celEngine    *expressions.CELEngine

	// mu guards running map.
	mu      sync.Mutex
	running map[string]*workflowRun
}

// workflowRun tracks a single in-flight workflow execution.
type workflowRun struct {
	workflowID string
	agentID    string
	dag        *DAG
	stepStates map[string]*store.StepState
	cancel     context.CancelFunc
	signals    chan schema.Signal
	mu         sync.Mutex // guards stepStates
}

// NewExecutor creates a new Executor with the given dependencies.
// vault is optional (nil = secrets interpolation disabled).
func NewExecutor(s store.Store, el EventLogger, registry actions.ActionRegistry, cfg ExecutorConfig, vault ...secrets.Vault) Executor {
	if cfg.PoolSize <= 0 {
		cfg.PoolSize = DefaultPoolSize
	}

	wfFSM := NewWorkflowFSM(el)
	stepFSM := NewStepFSM(el)
	pool := NewWorkerPool(cfg.PoolSize)

	cbConfig := DefaultCircuitBreakerConfig()
	if cfg.CircuitBreaker != nil {
		cbConfig = *cfg.CircuitBreaker
	}

	var v secrets.Vault
	if len(vault) > 0 && vault[0] != nil {
		v = vault[0]
	}

	// CEL engine is optional — flow control steps check nil before use.
	celEngine, _ := expressions.NewCELEngine()

	return &executorImpl{
		store:        s,
		eventLog:     el,
		wfFSM:        wfFSM,
		stepFSM:      stepFSM,
		actions:      registry,
		pool:         pool,
		config:       cfg,
		circuitBkr:   NewCircuitBreakerRegistry(cbConfig),
		interpolator: expressions.NewInterpolator(v),
		celEngine:    celEngine,
		running:      make(map[string]*workflowRun),
	}
}

// Run starts a new workflow execution.
func (e *executorImpl) Run(ctx context.Context, wf *store.Workflow, params map[string]any) (*ExecutionResult, error) {
	if wf == nil {
		return nil, schema.NewError(schema.ErrCodeValidation, "workflow is nil")
	}

	// Parse the DAG from the workflow definition.
	dag, err := ParseDAG(&wf.Definition)
	if err != nil {
		return nil, err
	}

	// Transition workflow: pending → active.
	if err := e.wfFSM.Transition(ctx, wf.ID, wf.Status, schema.WorkflowStatusActive); err != nil {
		return nil, err
	}

	// Persist the status change and start time.
	now := time.Now().UTC()
	activeStatus := schema.WorkflowStatusActive
	if err := e.store.UpdateWorkflow(ctx, wf.ID, store.WorkflowUpdate{
		Status:    &activeStatus,
		StartedAt: &now,
	}); err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeStore, "update workflow status: %s", err.Error()).WithCause(err)
	}

	// Initialize step states as pending.
	stepStates := make(map[string]*store.StepState, len(dag.Steps))
	for id := range dag.Steps {
		ss := &store.StepState{
			WorkflowID: wf.ID,
			StepID:     id,
			Status:     schema.StepStatusPending,
		}
		stepStates[id] = ss
		if err := e.store.UpsertStepState(ctx, ss); err != nil {
			return nil, schema.NewErrorf(schema.ErrCodeStore, "init step state %s: %s", id, err.Error()).WithCause(err)
		}
	}

	// Apply workflow-level timeout if specified.
	execCtx, execCancel := context.WithCancel(ctx)
	var timeoutBehavior string
	if wf.Definition.Timeout != "" {
		dur, parseErr := time.ParseDuration(wf.Definition.Timeout)
		if parseErr != nil {
			execCancel()
			return nil, schema.NewErrorf(schema.ErrCodeValidation, "invalid workflow timeout %q: %s", wf.Definition.Timeout, parseErr.Error())
		}
		timeoutBehavior = wf.Definition.OnTimeout
		if timeoutBehavior == "" {
			timeoutBehavior = DefaultOnTimeout
		}
		execCtx, execCancel = context.WithTimeout(ctx, dur)
	}

	// Register the run.
	run := &workflowRun{
		workflowID: wf.ID,
		agentID:    wf.AgentID,
		dag:        dag,
		stepStates: stepStates,
		cancel:     execCancel,
		signals:    make(chan schema.Signal, 16),
	}
	e.mu.Lock()
	e.running[wf.ID] = run
	e.mu.Unlock()

	// Execute the DAG.
	result := e.executeDAG(execCtx, run, wf.ID, params, timeoutBehavior)

	// Cleanup.
	execCancel()
	e.mu.Lock()
	delete(e.running, wf.ID)
	e.mu.Unlock()

	return result, nil
}

// Resume continues a suspended or interrupted workflow.
func (e *executorImpl) Resume(ctx context.Context, workflowID string) (*ExecutionResult, error) {
	// Load workflow from store.
	wf, err := e.store.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeStore, "load workflow: %s", err.Error()).WithCause(err)
	}
	if wf == nil {
		return nil, schema.NewError(schema.ErrCodeNotFound, "workflow not found: "+workflowID)
	}

	// Only suspended or active workflows can be resumed.
	if wf.Status != schema.WorkflowStatusSuspended && wf.Status != schema.WorkflowStatusActive {
		return nil, schema.NewErrorf(schema.ErrCodeConflict,
			"cannot resume workflow in status %s", wf.Status)
	}

	// Parse the DAG.
	dag, err := ParseDAG(&wf.Definition)
	if err != nil {
		return nil, err
	}

	// Replay events to rebuild step states.
	stepStates, err := e.eventLog.ReplayEvents(ctx, workflowID)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeStore, "replay events: %s", err.Error()).WithCause(err)
	}

	// Ensure all DAG steps have a state entry.
	for id := range dag.Steps {
		if _, ok := stepStates[id]; !ok {
			stepStates[id] = &store.StepState{
				WorkflowID: workflowID,
				StepID:     id,
				Status:     schema.StepStatusPending,
			}
		}
	}

	// Resolve any expired decisions before resuming.
	if err := e.resolveExpiredDecisions(ctx, workflowID, stepStates); err != nil {
		// Decision timed out without fallback — fail the workflow.
		now := time.Now().UTC()
		failedStatus := schema.WorkflowStatusFailed
		_ = e.store.UpdateWorkflow(ctx, workflowID, store.WorkflowUpdate{
			Status:      &failedStatus,
			CompletedAt: &now,
		})
		_ = e.wfFSM.Transition(ctx, workflowID, wf.Status, schema.WorkflowStatusFailed)
		var opErr *schema.OpcodeError
		if !errors.As(err, &opErr) {
			opErr = schema.NewErrorf(schema.ErrCodeTimeout, "decision timeout: %s", err.Error())
		}
		return &ExecutionResult{
			WorkflowID:  workflowID,
			Status:      schema.WorkflowStatusFailed,
			Error:       opErr,
			CompletedAt: &now,
			Steps:       make(map[string]*StepResult),
		}, nil
	}

	// If suspended, transition back to active.
	if wf.Status == schema.WorkflowStatusSuspended {
		if err := e.wfFSM.Transition(ctx, workflowID, wf.Status, schema.WorkflowStatusActive); err != nil {
			return nil, err
		}
		activeStatus := schema.WorkflowStatusActive
		if err := e.store.UpdateWorkflow(ctx, workflowID, store.WorkflowUpdate{Status: &activeStatus}); err != nil {
			return nil, schema.NewErrorf(schema.ErrCodeStore, "update workflow status: %s", err.Error()).WithCause(err)
		}
		// Emit resume event.
		if err := e.eventLog.AppendEvent(ctx, &store.Event{
			WorkflowID: workflowID,
			Type:       schema.EventWorkflowResumed,
		}); err != nil {
			return nil, schema.NewErrorf(schema.ErrCodeStore, "emit resume event: %s", err.Error()).WithCause(err)
		}
	}

	// Apply remaining workflow timeout if any.
	execCtx, execCancel := context.WithCancel(ctx)
	var timeoutBehavior string
	if wf.Definition.Timeout != "" {
		dur, parseErr := time.ParseDuration(wf.Definition.Timeout)
		if parseErr == nil {
			// Subtract elapsed time since workflow started.
			elapsed := time.Duration(0)
			if wf.StartedAt != nil {
				elapsed = time.Since(*wf.StartedAt)
			}
			remaining := dur - elapsed
			if remaining <= 0 {
				execCancel()
				return nil, schema.NewError(schema.ErrCodeTimeout, "workflow timeout already expired")
			}
			timeoutBehavior = wf.Definition.OnTimeout
			if timeoutBehavior == "" {
				timeoutBehavior = DefaultOnTimeout
			}
			execCtx, execCancel = context.WithTimeout(ctx, remaining)
		}
	}

	run := &workflowRun{
		workflowID: workflowID,
		agentID:    wf.AgentID,
		dag:        dag,
		stepStates: stepStates,
		cancel:     execCancel,
		signals:    make(chan schema.Signal, 16),
	}
	e.mu.Lock()
	e.running[workflowID] = run
	e.mu.Unlock()

	result := e.executeDAG(execCtx, run, workflowID, wf.InputParams, timeoutBehavior)

	execCancel()
	e.mu.Lock()
	delete(e.running, workflowID)
	e.mu.Unlock()

	return result, nil
}

// Signal delivers a signal to a running/suspended workflow.
func (e *executorImpl) Signal(ctx context.Context, workflowID string, signal schema.Signal) error {
	// Emit signal event.
	payload, _ := json.Marshal(signal)
	if err := e.eventLog.AppendEvent(ctx, &store.Event{
		WorkflowID: workflowID,
		StepID:     signal.StepID,
		Type:       schema.EventSignalReceived,
		Payload:    payload,
	}); err != nil {
		return schema.NewErrorf(schema.ErrCodeStore, "emit signal event: %s", err.Error()).WithCause(err)
	}

	// If the workflow is running, deliver the signal to the channel.
	e.mu.Lock()
	run, ok := e.running[workflowID]
	e.mu.Unlock()

	if ok {
		select {
		case run.signals <- signal:
		default:
			return schema.NewError(schema.ErrCodeSignalFailed, "signal channel full")
		}
		return nil
	}

	// If not running, handle decision signals for suspended workflows.
	if signal.StepID != "" && (signal.Type == schema.SignalDecision || signal.Type == schema.SignalCancel) {
		decisions, err := e.store.ListPendingDecisions(ctx, store.DecisionFilter{
			WorkflowID: workflowID,
			Status:     "pending",
		})
		if err != nil {
			return schema.NewErrorf(schema.ErrCodeStore, "list decisions: %s", err.Error()).WithCause(err)
		}
		for _, d := range decisions {
			if d.StepID != signal.StepID {
				continue
			}

			// Validate multi-agent routing: reject if targeted to a different agent.
			if d.TargetAgentID != "" && signal.AgentID != "" && signal.AgentID != d.TargetAgentID {
				return schema.NewErrorf(schema.ErrCodeValidation,
					"decision targeted to agent %q, signal from %q", d.TargetAgentID, signal.AgentID)
			}

			if signal.Type == schema.SignalCancel {
				if err := e.store.CancelDecision(ctx, d.ID); err != nil {
					return schema.NewErrorf(schema.ErrCodeStore, "cancel decision: %s", err.Error()).WithCause(err)
				}
				return nil
			}

			// SignalDecision: resolve the decision.
			choice, ok := signal.Payload["choice"].(string)
			if !ok || choice == "" {
				return schema.NewError(schema.ErrCodeValidation, "signal payload missing valid 'choice' field")
			}
			// Validate choice against available options.
			var opts []schema.ReasoningOption
			if len(d.Options) > 0 {
				if err := json.Unmarshal(d.Options, &opts); err == nil {
					if err := reasoning.ValidateResolution(opts, choice); err != nil {
						return err
					}
				}
			}
			resolution := &store.Resolution{
				Choice:     choice,
				Reasoning:  signal.Reasoning,
				ResolvedBy: signal.AgentID,
			}
			if data, ok := signal.Payload["data"].(map[string]any); ok {
				resolution.Data = data
			}
			if err := e.store.ResolveDecision(ctx, d.ID, resolution); err != nil {
				return schema.NewErrorf(schema.ErrCodeStore, "resolve decision: %s", err.Error()).WithCause(err)
			}
			// Emit decision resolved event.
			resPayload, _ := json.Marshal(resolution)
			if err := e.eventLog.AppendEvent(ctx, &store.Event{
				WorkflowID: workflowID,
				StepID:     signal.StepID,
				Type:       schema.EventDecisionResolved,
				Payload:    resPayload,
			}); err != nil {
				return schema.NewErrorf(schema.ErrCodeStore, "emit decision resolved: %s", err.Error()).WithCause(err)
			}
			return nil
		}
		return schema.NewErrorf(schema.ErrCodeNotFound, "no pending decision for step %s", signal.StepID)
	}

	return schema.NewError(schema.ErrCodeConflict, "workflow not running")
}

// Extend mutates the DAG of a running workflow.
func (e *executorImpl) Extend(ctx context.Context, workflowID string, mutation schema.DAGMutation) error {
	// Emit mutation event.
	payload, _ := json.Marshal(mutation)
	if err := e.eventLog.AppendEvent(ctx, &store.Event{
		WorkflowID: workflowID,
		Type:       schema.EventDAGMutated,
		Payload:    payload,
	}); err != nil {
		return schema.NewErrorf(schema.ErrCodeStore, "emit dag mutation event: %s", err.Error()).WithCause(err)
	}

	// Variable set is the only mutation supported in Cycle 1.
	if mutation.Action == schema.MutationSetVariable && mutation.Variable != nil {
		varPayload, _ := json.Marshal(mutation.Variable)
		return e.eventLog.AppendEvent(ctx, &store.Event{
			WorkflowID: workflowID,
			Type:       schema.EventVariableSet,
			Payload:    varPayload,
		})
	}

	// DAG structure mutations (insert, replace, remove) are deferred to Cycle 2.
	return schema.NewError(schema.ErrCodeValidation, "DAG structure mutations not yet supported; use set_variable")
}

// Cancel terminates a workflow.
func (e *executorImpl) Cancel(ctx context.Context, workflowID string, reason string) error {
	wf, err := e.store.GetWorkflow(ctx, workflowID)
	if err != nil {
		return schema.NewErrorf(schema.ErrCodeStore, "load workflow: %s", err.Error()).WithCause(err)
	}
	if wf == nil {
		return schema.NewError(schema.ErrCodeNotFound, "workflow not found: "+workflowID)
	}

	// Build current step states.
	stepStates, err := e.store.ListStepStates(ctx, workflowID)
	if err != nil {
		return schema.NewErrorf(schema.ErrCodeStore, "list step states: %s", err.Error()).WithCause(err)
	}
	stateMap := make(map[string]schema.StepStatus, len(stepStates))
	for _, ss := range stepStates {
		stateMap[ss.StepID] = ss.Status
	}

	// Use the FSM cancel cascade.
	if err := CancelWorkflow(ctx, e.wfFSM, e.stepFSM, workflowID, wf.Status, stateMap); err != nil {
		return err
	}

	// Persist cancel status.
	cancelledStatus := schema.WorkflowStatusCancelled
	now := time.Now().UTC()
	errPayload, _ := json.Marshal(map[string]string{"reason": reason})
	if err := e.store.UpdateWorkflow(ctx, workflowID, store.WorkflowUpdate{
		Status:      &cancelledStatus,
		CompletedAt: &now,
		Error:       errPayload,
	}); err != nil {
		return schema.NewErrorf(schema.ErrCodeStore, "update workflow status: %s", err.Error()).WithCause(err)
	}

	// Persist step state changes.
	for _, ss := range stepStates {
		if canSkip(ss.Status) {
			ss.Status = schema.StepStatusSkipped
			if err := e.store.UpsertStepState(ctx, ss); err != nil {
				return schema.NewErrorf(schema.ErrCodeStore, "update step state %s: %s", ss.StepID, err.Error()).WithCause(err)
			}
		}
	}

	// If the workflow is actively running, cancel its context.
	e.mu.Lock()
	if run, ok := e.running[workflowID]; ok {
		run.cancel()
	}
	e.mu.Unlock()

	return nil
}

// Status returns the current workflow state snapshot.
func (e *executorImpl) Status(ctx context.Context, workflowID string) (*WorkflowStatus, error) {
	wf, err := e.store.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeStore, "load workflow: %s", err.Error()).WithCause(err)
	}
	if wf == nil {
		return nil, schema.NewError(schema.ErrCodeNotFound, "workflow not found: "+workflowID)
	}

	stepStates, err := e.store.ListStepStates(ctx, workflowID)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeStore, "list step states: %s", err.Error()).WithCause(err)
	}
	stepsMap := make(map[string]*store.StepState, len(stepStates))
	for _, ss := range stepStates {
		stepsMap[ss.StepID] = ss
	}

	decisions, err := e.store.ListPendingDecisions(ctx, store.DecisionFilter{
		WorkflowID: workflowID,
		Status:     "pending",
	})
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeStore, "list decisions: %s", err.Error()).WithCause(err)
	}

	wfCtx, _ := e.store.GetWorkflowContext(ctx, workflowID)

	events, err := e.eventLog.GetEvents(ctx, workflowID, 0)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeStore, "get events: %s", err.Error()).WithCause(err)
	}

	return &WorkflowStatus{
		WorkflowID:       workflowID,
		Status:           wf.Status,
		Steps:            stepsMap,
		PendingDecisions: decisions,
		Context:          wfCtx,
		Events:           events,
	}, nil
}

// --- DAG execution engine ---

// executeDAG walks the DAG level by level, dispatching steps to the worker pool.
func (e *executorImpl) executeDAG(ctx context.Context, run *workflowRun, workflowID string, params map[string]any, timeoutBehavior string) *ExecutionResult {
	startedAt := time.Now().UTC()
	result := &ExecutionResult{
		WorkflowID: workflowID,
		Status:     schema.WorkflowStatusActive,
		StartedAt:  startedAt,
		Steps:      make(map[string]*StepResult),
	}

	var finalErr *schema.OpcodeError

	// Walk levels sequentially. Within each level, dispatch steps in parallel.
	for _, level := range run.dag.Levels {
		if ctx.Err() != nil {
			break
		}

		var wg sync.WaitGroup
		stepErrors := make(chan stepExecError, len(level))

		for _, stepID := range level {
			run.mu.Lock()
			ss := run.stepStates[stepID]
			run.mu.Unlock()

			// Skip already completed/failed/skipped steps (from resume).
			if isTerminalStep(ss.Status) {
				result.Steps[stepID] = &StepResult{
					StepID:     stepID,
					Status:     ss.Status,
					Output:     ss.Output,
					DurationMs: ss.DurationMs,
				}
				continue
			}

			stepDef := run.dag.Steps[stepID]
			wg.Add(1)
			sid := stepID
			sdef := stepDef

			err := e.pool.Submit(ctx, func(stepCtx context.Context) error {
				defer wg.Done()
				execErr := e.executeStep(stepCtx, run, workflowID, sid, sdef, params)
				if execErr != nil {
					stepErrors <- stepExecError{stepID: sid, err: execErr}
				}
				return nil // pool doesn't track step errors
			})
			if err != nil {
				wg.Done()
				// Pool rejected (shutdown or context cancelled).
				stepErrors <- stepExecError{stepID: sid, err: err}
			}
		}

		wg.Wait()
		close(stepErrors)

		// Check for step failures.
		for se := range stepErrors {
			run.mu.Lock()
			ss := run.stepStates[se.stepID]
			run.mu.Unlock()

			// If the step is suspended (reasoning node), that's not a failure.
			if ss != nil && ss.Status == schema.StepStatusSuspended {
				continue
			}

			if finalErr == nil {
				if opErr, ok := se.err.(*schema.OpcodeError); ok {
					finalErr = opErr
				} else {
					finalErr = schema.NewErrorf(schema.ErrCodeStepFailed, "step %s: %s", se.stepID, se.err.Error()).WithStep(se.stepID)
				}
			}
		}

		// If the context is done due to workflow timeout, handle that before
		// treating individual step errors as the final error.
		if ctx.Err() != nil {
			finalErr = nil // Clear step-level errors; timeout takes precedence.
			break
		}

		// If any step failed and there's no on_error handler, abort.
		if finalErr != nil {
			break
		}

		// Check if any step is suspended (reasoning node waiting for decision).
		hasSuspended := false
		for _, stepID := range level {
			run.mu.Lock()
			ss := run.stepStates[stepID]
			run.mu.Unlock()
			if ss != nil && ss.Status == schema.StepStatusSuspended {
				hasSuspended = true
				break
			}
		}
		if hasSuspended {
			// Workflow suspends waiting for agent input.
			result.Status = schema.WorkflowStatusSuspended
			e.finalizeResult(ctx, run, result)
			e.transitionWorkflow(ctx, workflowID, schema.WorkflowStatusActive, schema.WorkflowStatusSuspended)
			return result
		}
	}

	// Handle context errors (timeout or cancellation).
	if ctx.Err() != nil && finalErr == nil {
		if ctx.Err() == context.DeadlineExceeded {
			finalErr = e.handleTimeout(ctx, run, workflowID, timeoutBehavior)
			if timeoutBehavior == "suspend" {
				result.Status = schema.WorkflowStatusSuspended
				e.finalizeResult(ctx, run, result)
				return result
			}
			if timeoutBehavior == "cancel" {
				result.Status = schema.WorkflowStatusCancelled
				e.finalizeResult(ctx, run, result)
				return result
			}
			// default "fail" falls through
		} else {
			finalErr = schema.NewError(schema.ErrCodeCancelled, "workflow cancelled")
		}
	}

	// Determine final workflow status.
	if finalErr != nil {
		result.Status = schema.WorkflowStatusFailed
		result.Error = finalErr
		e.transitionWorkflow(ctx, workflowID, schema.WorkflowStatusActive, schema.WorkflowStatusFailed)
	} else {
		result.Status = schema.WorkflowStatusCompleted
		e.transitionWorkflow(ctx, workflowID, schema.WorkflowStatusActive, schema.WorkflowStatusCompleted)
	}

	now := time.Now().UTC()
	result.CompletedAt = &now
	e.finalizeResult(ctx, run, result)
	e.persistWorkflowEnd(workflowID, result)

	return result
}

type stepExecError struct {
	stepID string
	err    error
}

// executeStep runs a single step: transition states, execute action, handle result.
func (e *executorImpl) executeStep(ctx context.Context, run *workflowRun, workflowID, stepID string, stepDef *schema.StepDefinition, params map[string]any) error {
	// Apply step-level timeout if specified.
	stepCtx := ctx
	var stepCancel context.CancelFunc
	if stepDef.Timeout != "" {
		dur, err := time.ParseDuration(stepDef.Timeout)
		if err == nil && dur > 0 {
			stepCtx, stepCancel = context.WithTimeout(ctx, dur)
			defer stepCancel()
		}
	}

	// Transition: pending → scheduled.
	run.mu.Lock()
	ss := run.stepStates[stepID]
	currentStatus := ss.Status
	run.mu.Unlock()

	if currentStatus == schema.StepStatusPending {
		if err := e.stepFSM.Transition(stepCtx, workflowID, stepID, schema.StepStatusPending, schema.StepStatusScheduled); err != nil {
			return err
		}
		e.updateStepStatus(run, stepID, schema.StepStatusScheduled)
	}

	// Transition: scheduled → running.
	if err := e.stepFSM.Transition(stepCtx, workflowID, stepID, schema.StepStatusScheduled, schema.StepStatusRunning); err != nil {
		return err
	}
	e.updateStepStatus(run, stepID, schema.StepStatusRunning)
	startTime := time.Now().UTC()

	// Persist running state.
	run.mu.Lock()
	run.stepStates[stepID].StartedAt = &startTime
	run.mu.Unlock()
	e.persistStepState(workflowID, stepID, run)

	// Handle step by type.
	var output json.RawMessage
	var execErr error

	switch stepDef.Type {
	case schema.StepTypeReasoning:
		execErr = e.executeReasoningStep(stepCtx, run, workflowID, stepID, stepDef)
		if execErr == nil {
			// Check if suspended or skipped (cancelled decision).
			run.mu.Lock()
			status := run.stepStates[stepID].Status
			run.mu.Unlock()
			if status == schema.StepStatusSuspended || status == schema.StepStatusSkipped {
				return nil
			}
		}

	case schema.StepTypeCondition:
		output, execErr = e.executeConditionStep(stepCtx, run, workflowID, stepID, stepDef, params)

	case schema.StepTypeLoop:
		output, execErr = e.executeLoopStep(stepCtx, run, workflowID, stepID, stepDef, params)

	case schema.StepTypeParallel:
		output, execErr = e.executeParallelStep(stepCtx, run, workflowID, stepID, stepDef, params)

	case schema.StepTypeWait:
		output, execErr = e.executeWaitStep(stepCtx, run, workflowID, stepID, stepDef, params)

	case schema.StepTypeAction, "":
		output, execErr = e.executeActionStep(stepCtx, run, workflowID, stepDef, params)
	}

	// Handle step timeout.
	if stepCtx.Err() == context.DeadlineExceeded && execErr == nil {
		execErr = schema.NewErrorf(schema.ErrCodeTimeout, "step %s timed out", stepID).WithStep(stepID)
	}

	if execErr != nil {
		return e.handleStepFailure(stepCtx, run, workflowID, stepID, stepDef, execErr, params)
	}

	// Transition: running → completed.
	if err := e.stepFSM.Transition(stepCtx, workflowID, stepID, schema.StepStatusRunning, schema.StepStatusCompleted); err != nil {
		return err
	}

	completedAt := time.Now().UTC()
	durationMs := completedAt.Sub(startTime).Milliseconds()

	run.mu.Lock()
	run.stepStates[stepID].Status = schema.StepStatusCompleted
	run.stepStates[stepID].Output = output
	run.stepStates[stepID].CompletedAt = &completedAt
	run.stepStates[stepID].DurationMs = durationMs
	run.mu.Unlock()

	e.persistStepState(workflowID, stepID, run)
	return nil
}

// executeActionStep runs an action-type step via the action registry.
func (e *executorImpl) executeActionStep(ctx context.Context, run *workflowRun, workflowID string, stepDef *schema.StepDefinition, params map[string]any) (json.RawMessage, error) {
	// Check circuit breaker before executing.
	if err := e.circuitBkr.AllowRequest(stepDef.Action); err != nil {
		return nil, err
	}

	action, err := e.actions.Get(stepDef.Action)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "action %q not found: %s", stepDef.Action, err.Error())
	}

	// Build interpolation scope once — used for both ${{}} resolution and action context.
	scope := e.buildInterpolationScope(run, workflowID, params)

	// Interpolate ${{...}} references in step params before execution.
	interpolatedParams := stepDef.Params
	if expressions.HasInterpolation(stepDef.Params) {
		interpolatedParams, err = e.interpolator.Resolve(ctx, stepDef.Params, scope)
		if err != nil {
			return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
				"interpolation failed for step action %q: %s", stepDef.Action, err.Error()).WithCause(err)
		}
	}

	// Build action input from interpolated step params + workflow params.
	stepParams := make(map[string]any)
	if len(interpolatedParams) > 0 {
		if err := json.Unmarshal(interpolatedParams, &stepParams); err != nil {
			return nil, schema.NewErrorf(schema.ErrCodeValidation, "unmarshal step params: %s", err.Error())
		}
	}

	// Build action context: workflow params first, then structured scope keys always win.
	actionCtx := make(map[string]any)
	for k, v := range params {
		actionCtx[k] = v
	}
	actionCtx["steps"] = scope.Steps
	actionCtx["inputs"] = scope.Inputs
	actionCtx["workflow"] = scope.Workflow
	actionCtx["context"] = scope.Context
	actionCtx["workflow_id"] = workflowID
	actionCtx["step_id"] = stepDef.ID
	actionCtx["agent_id"] = run.agentID

	input := actions.ActionInput{
		Params:  stepParams,
		Context: actionCtx,
	}

	out, err := action.Execute(ctx, input)
	if err != nil {
		// Record failure in circuit breaker.
		newState := e.circuitBkr.RecordFailure(stepDef.Action)
		if newState == CircuitOpen {
			// Emit circuit breaker open event (best effort).
			cbPayload, _ := json.Marshal(e.circuitBkr.GetStats(stepDef.Action))
			_ = e.eventLog.AppendEvent(ctx, &store.Event{
				WorkflowID: "", // filled by caller if needed
				Type:       schema.EventCircuitBreakerOpen,
				Payload:    cbPayload,
			})
		}
		return nil, err
	}

	// Record success in circuit breaker.
	e.circuitBkr.RecordSuccess(stepDef.Action)

	if out != nil {
		return out.Data, nil
	}
	return nil, nil
}

// executeReasoningStep handles reasoning-type steps: creates a pending decision and suspends.
func (e *executorImpl) executeReasoningStep(ctx context.Context, run *workflowRun, workflowID, stepID string, stepDef *schema.StepDefinition) error {
	var cfg schema.ReasoningConfig
	if err := json.Unmarshal(stepDef.Config, &cfg); err != nil {
		return schema.NewErrorf(schema.ErrCodeValidation, "invalid reasoning config: %s", err.Error()).WithStep(stepID)
	}

	// Check if there's already a resolved or cancelled decision (from replay/signal).
	decisions, err := e.store.ListPendingDecisions(ctx, store.DecisionFilter{
		WorkflowID: workflowID,
	})
	if err == nil {
		for _, d := range decisions {
			if d.StepID != stepID {
				continue
			}
			if d.Status == "resolved" && d.Resolution != nil {
				// Decision already resolved — don't re-request. Reasoning nodes NEVER replay.
				return nil
			}
			if d.Status == "cancelled" {
				// Decision was cancelled — skip this step.
				if fsmErr := e.stepFSM.Transition(ctx, workflowID, stepID, schema.StepStatusRunning, schema.StepStatusSkipped); fsmErr != nil {
					return fsmErr
				}
				e.updateStepStatus(run, stepID, schema.StepStatusSkipped)
				e.persistStepState(workflowID, stepID, run)
				return nil
			}
			if d.Status == "pending" {
				// Decision already exists and is pending — suspend without re-creating.
				if fsmErr := e.stepFSM.Transition(ctx, workflowID, stepID, schema.StepStatusRunning, schema.StepStatusSuspended); fsmErr != nil {
					return fsmErr
				}
				e.updateStepStatus(run, stepID, schema.StepStatusSuspended)
				e.persistStepState(workflowID, stepID, run)
				return nil
			}
		}
	}

	// Build rich context for the decision.
	optionsJSON, err := json.Marshal(cfg.Options)
	if err != nil {
		return schema.NewErrorf(schema.ErrCodeValidation, "marshal reasoning options: %s", err.Error()).WithStep(stepID)
	}
	ctxParams := reasoning.ContextParams{Config: cfg}

	// Collect completed step outputs.
	run.mu.Lock()
	outputs := make(map[string]any)
	for sid, ss := range run.stepStates {
		if ss.Status == schema.StepStatusCompleted && len(ss.Output) > 0 {
			var out any
			if err := json.Unmarshal(ss.Output, &out); err == nil {
				outputs[sid] = out
			}
		}
	}
	run.mu.Unlock()
	ctxParams.StepOutputs = outputs

	// Load workflow context (intent, notes, accumulated data).
	if wfCtx, err := e.store.GetWorkflowContext(ctx, workflowID); err == nil && wfCtx != nil {
		ctxParams.WorkflowIntent = wfCtx.OriginalIntent
		ctxParams.AgentNotes = wfCtx.AgentNotes
		if len(wfCtx.AccumulatedData) > 0 {
			var accum map[string]any
			if err := json.Unmarshal(wfCtx.AccumulatedData, &accum); err == nil {
				ctxParams.AccumulatedData = accum
			}
		}
	}

	// Resolve DataInject references via interpolation.
	if len(cfg.DataInject) > 0 {
		scope := e.buildInterpolationScope(run, workflowID, nil)
		resolved := make(map[string]any, len(cfg.DataInject))
		for key, expr := range cfg.DataInject {
			exprJSON, _ := json.Marshal(expr)
			if expressions.HasInterpolation(exprJSON) {
				val, err := e.interpolator.Resolve(ctx, exprJSON, scope)
				if err == nil {
					var v any
					if err := json.Unmarshal(val, &v); err == nil {
						resolved[key] = v
					} else {
						resolved[key] = string(val)
					}
				} else {
					resolved[key] = expr // keep raw on error
				}
			} else {
				// Auto-resolve bare namespace paths (e.g. "steps.fetch.output.body").
				wrapped, _ := json.Marshal(fmt.Sprintf("${{ %s }}", expr))
				val, err := e.interpolator.Resolve(ctx, wrapped, scope)
				if err == nil {
					var v any
					if err := json.Unmarshal(val, &v); err == nil {
						resolved[key] = v
					} else {
						resolved[key] = string(val)
					}
				} else {
					resolved[key] = expr // not a valid path — keep as literal
				}
			}
		}
		ctxParams.ResolvedInjects = resolved
	}

	contextJSON := reasoning.BuildDecisionContext(ctxParams)

	decision := &store.PendingDecision{
		ID:         fmt.Sprintf("%s_%s", workflowID, stepID),
		WorkflowID: workflowID,
		StepID:     stepID,
		Context:    contextJSON,
		Options:    optionsJSON,
		Fallback:   cfg.Fallback,
		Status:     "pending",
		CreatedAt:  time.Now().UTC(),
	}
	if cfg.TargetAgent != "" {
		decision.TargetAgentID = cfg.TargetAgent
	}
	if cfg.Timeout != "" {
		dur, err := time.ParseDuration(cfg.Timeout)
		if err == nil {
			t := time.Now().UTC().Add(dur)
			decision.TimeoutAt = &t
		}
	}

	if err := e.store.CreateDecision(ctx, decision); err != nil {
		return schema.NewErrorf(schema.ErrCodeStore, "create decision: %s", err.Error()).WithStep(stepID).WithCause(err)
	}

	// Emit decision requested event.
	if err := e.eventLog.AppendEvent(ctx, &store.Event{
		WorkflowID: workflowID,
		StepID:     stepID,
		Type:       schema.EventDecisionRequested,
		Payload:    contextJSON,
	}); err != nil {
		return schema.NewErrorf(schema.ErrCodeStore, "emit decision event: %s", err.Error()).WithStep(stepID).WithCause(err)
	}

	// Transition: running → suspended.
	if err := e.stepFSM.Transition(ctx, workflowID, stepID, schema.StepStatusRunning, schema.StepStatusSuspended); err != nil {
		return err
	}
	e.updateStepStatus(run, stepID, schema.StepStatusSuspended)
	e.persistStepState(workflowID, stepID, run)

	return nil
}

// resolveExpiredDecisions checks for pending decisions that have exceeded their timeout.
// For each expired decision: auto-resolve with fallback if set, or return an error.
// Called from Resume() before executing the DAG.
func (e *executorImpl) resolveExpiredDecisions(ctx context.Context, workflowID string, stepStates map[string]*store.StepState) error {
	decisions, err := e.store.ListPendingDecisions(ctx, store.DecisionFilter{
		WorkflowID: workflowID,
		Status:     "pending",
	})
	if err != nil {
		return nil // best-effort; don't block resume
	}

	now := time.Now().UTC()
	for _, d := range decisions {
		if d.TimeoutAt == nil || now.Before(*d.TimeoutAt) {
			continue
		}

		if d.Fallback != "" {
			// Auto-resolve with fallback option.
			resolution := &store.Resolution{
				Choice:     d.Fallback,
				Reasoning:  "timeout_auto_resolved",
				ResolvedBy: "system",
			}
			if err := e.store.ResolveDecision(ctx, d.ID, resolution); err != nil {
				continue
			}
			resPayload, _ := json.Marshal(resolution)
			_ = e.eventLog.AppendEvent(ctx, &store.Event{
				WorkflowID: workflowID,
				StepID:     d.StepID,
				Type:       schema.EventDecisionResolved,
				Payload:    resPayload,
			})
		} else {
			// No fallback — fail the step and return error to abort resume.
			_ = e.store.ResolveDecision(ctx, d.ID, &store.Resolution{
				Choice:     "",
				Reasoning:  "timeout_no_fallback",
				ResolvedBy: "system",
			})
			_ = e.eventLog.AppendEvent(ctx, &store.Event{
				WorkflowID: workflowID,
				StepID:     d.StepID,
				Type:       schema.EventStepFailed,
				Payload:    json.RawMessage(`{"error":"decision timeout: no fallback option configured"}`),
			})
			if ss, ok := stepStates[d.StepID]; ok {
				ss.Status = schema.StepStatusFailed
				ss.Error = json.RawMessage(`"decision timeout: no fallback option configured"`)
			}
			return schema.NewErrorf(schema.ErrCodeTimeout,
				"decision timeout for step %s: no fallback option configured", d.StepID).WithStep(d.StepID)
		}
	}
	return nil
}

// handleStepFailure handles a step that errored, including retry logic, retryable classification,
// circuit breaker awareness, and on_error handler dispatch.
func (e *executorImpl) handleStepFailure(ctx context.Context, run *workflowRun, workflowID, stepID string, stepDef *schema.StepDefinition, execErr error, params map[string]any) error {
	run.mu.Lock()
	ss := run.stepStates[stepID]
	retryCount := ss.RetryCount
	run.mu.Unlock()

	// Check if the error is retryable before attempting retries.
	retryable := IsRetryableError(execErr)

	// Check retry policy — only retry if error is retryable.
	if retryable && stepDef.Retry != nil && retryCount < stepDef.Retry.Max {
		// Log retry attempt event.
		retryPayload, _ := json.Marshal(map[string]any{
			"step_id":      stepID,
			"attempt":      retryCount + 1,
			"max_attempts": stepDef.Retry.Max,
			"error":        execErr.Error(),
			"retryable":    true,
		})
		_ = e.eventLog.AppendEvent(ctx, &store.Event{
			WorkflowID: workflowID,
			StepID:     stepID,
			Type:       schema.EventStepRetryAttempt,
			Payload:    retryPayload,
		})

		// Transition: running → retrying.
		if err := e.stepFSM.Transition(ctx, workflowID, stepID, schema.StepStatusRunning, schema.StepStatusRetrying); err != nil {
			return err
		}

		run.mu.Lock()
		run.stepStates[stepID].Status = schema.StepStatusRetrying
		run.stepStates[stepID].RetryCount++
		run.mu.Unlock()
		e.persistStepState(workflowID, stepID, run)

		// Apply backoff delay using the enhanced ComputeBackoff.
		delay := ComputeBackoff(stepDef.Retry, retryCount)
		if err := WaitForBackoff(ctx, delay); err != nil {
			return err
		}

		// Transition: retrying → running.
		if err := e.stepFSM.Transition(ctx, workflowID, stepID, schema.StepStatusRetrying, schema.StepStatusRunning); err != nil {
			return err
		}
		e.updateStepStatus(run, stepID, schema.StepStatusRunning)

		// Re-execute with fresh step timeout.
		retryCtx := ctx
		if stepDef.Timeout != "" {
			dur, err := time.ParseDuration(stepDef.Timeout)
			if err == nil && dur > 0 {
				var retryCancel context.CancelFunc
				retryCtx, retryCancel = context.WithTimeout(ctx, dur)
				defer retryCancel()
			}
		}

		var output json.RawMessage
		var retryErr error
		switch stepDef.Type {
		case schema.StepTypeAction, "":
			output, retryErr = e.executeActionStep(retryCtx, run, workflowID, stepDef, params)
		default:
			output, retryErr = e.executeActionStep(retryCtx, run, workflowID, stepDef, params)
		}

		if retryErr != nil {
			// Recurse for further retries.
			return e.handleStepFailure(ctx, run, workflowID, stepID, stepDef, retryErr, params)
		}

		// Success after retry.
		if err := e.stepFSM.Transition(ctx, workflowID, stepID, schema.StepStatusRunning, schema.StepStatusCompleted); err != nil {
			return err
		}
		completedAt := time.Now().UTC()
		run.mu.Lock()
		run.stepStates[stepID].Status = schema.StepStatusCompleted
		run.stepStates[stepID].Output = output
		run.stepStates[stepID].CompletedAt = &completedAt
		run.mu.Unlock()
		e.persistStepState(workflowID, stepID, run)
		return nil
	}

	// Retries exhausted or not retryable — consult on_error handler before failing.
	handlerResult, _ := HandleStepError(ctx, e.eventLog, workflowID, stepID, stepDef.OnError, execErr)

	if handlerResult != nil && handlerResult.Handled {
		if handlerResult.FallbackStepID != "" {
			// Execute fallback step: mark original as failed, run fallback.
			e.failStep(ctx, run, workflowID, stepID, execErr)
			return e.executeFallbackStep(ctx, run, workflowID, stepID, handlerResult.FallbackStepID, params)
		}

		if !handlerResult.ShouldFailWorkflow {
			// "ignore" strategy: mark step as completed (error ignored) and continue.
			e.ignoreStepError(ctx, run, workflowID, stepID)
			return nil
		}
		// "fail_workflow" falls through to the normal failure path.
	}

	// Fail the step.
	e.failStep(ctx, run, workflowID, stepID, execErr)

	// Return appropriate error code.
	if stepDef.Retry != nil && retryCount >= stepDef.Retry.Max {
		return schema.NewErrorf(schema.ErrCodeRetryExhausted, "step %s: retries exhausted after %d attempts: %s",
			stepID, retryCount+1, execErr.Error()).WithStep(stepID)
	}
	if !retryable {
		return schema.NewErrorf(schema.ErrCodeNonRetryable, "step %s: non-retryable error: %s",
			stepID, execErr.Error()).WithStep(stepID)
	}

	return schema.NewErrorf(schema.ErrCodeStepFailed, "step %s: %s", stepID, execErr.Error()).WithStep(stepID)
}

// failStep transitions a step to failed state and persists the error.
func (e *executorImpl) failStep(ctx context.Context, run *workflowRun, workflowID, stepID string, execErr error) {
	if err := e.stepFSM.Transition(ctx, workflowID, stepID, schema.StepStatusRunning, schema.StepStatusFailed); err != nil {
		// If transition fails (e.g. already in retrying state), try from retrying.
		_ = e.stepFSM.Transition(ctx, workflowID, stepID, schema.StepStatusRetrying, schema.StepStatusFailed)
	}

	errPayload, _ := json.Marshal(map[string]string{"error": execErr.Error()})
	run.mu.Lock()
	run.stepStates[stepID].Status = schema.StepStatusFailed
	run.stepStates[stepID].Error = errPayload
	run.mu.Unlock()
	e.persistStepState(workflowID, stepID, run)
}

// ignoreStepError marks a step as completed despite the error (used by "ignore" on_error strategy).
func (e *executorImpl) ignoreStepError(ctx context.Context, run *workflowRun, workflowID, stepID string) {
	if err := e.stepFSM.Transition(ctx, workflowID, stepID, schema.StepStatusRunning, schema.StepStatusCompleted); err != nil {
		// Fallback: try from retrying.
		_ = e.stepFSM.Transition(ctx, workflowID, stepID, schema.StepStatusRetrying, schema.StepStatusFailed)
		return
	}

	completedAt := time.Now().UTC()
	run.mu.Lock()
	run.stepStates[stepID].Status = schema.StepStatusCompleted
	run.stepStates[stepID].CompletedAt = &completedAt
	run.mu.Unlock()
	e.persistStepState(workflowID, stepID, run)
}

// executeFallbackStep runs a fallback step when on_error specifies fallback_step.
// The fallback step must exist in the DAG definition.
func (e *executorImpl) executeFallbackStep(ctx context.Context, run *workflowRun, workflowID, originalStepID, fallbackStepID string, params map[string]any) error {
	fbDef, ok := run.dag.Steps[fallbackStepID]
	if !ok {
		return schema.NewErrorf(schema.ErrCodeNotFound,
			"fallback step %q not found in DAG (referenced by step %s on_error)",
			fallbackStepID, originalStepID).WithStep(originalStepID)
	}

	// Initialize fallback step state if not present.
	run.mu.Lock()
	if _, exists := run.stepStates[fallbackStepID]; !exists {
		run.stepStates[fallbackStepID] = &store.StepState{
			WorkflowID: workflowID,
			StepID:     fallbackStepID,
			Status:     schema.StepStatusPending,
		}
	}
	run.mu.Unlock()

	// Execute the fallback step directly.
	return e.executeStep(ctx, run, workflowID, fallbackStepID, fbDef, params)
}

// handleTimeout processes a workflow-level timeout according to the configured behavior.
func (e *executorImpl) handleTimeout(ctx context.Context, run *workflowRun, workflowID, behavior string) *schema.OpcodeError {
	// Use a fresh context since the original is cancelled.
	bgCtx := context.Background()

	// Emit timeout event.
	_ = e.eventLog.AppendEvent(bgCtx, &store.Event{
		WorkflowID: workflowID,
		Type:       schema.EventWorkflowTimedOut,
	})

	switch behavior {
	case "suspend":
		e.transitionWorkflow(bgCtx, workflowID, schema.WorkflowStatusActive, schema.WorkflowStatusSuspended)
		return nil

	case "cancel":
		run.mu.Lock()
		stateMap := make(map[string]schema.StepStatus, len(run.stepStates))
		for id, ss := range run.stepStates {
			stateMap[id] = ss.Status
		}
		run.mu.Unlock()
		_ = CancelWorkflow(bgCtx, e.wfFSM, e.stepFSM, workflowID, schema.WorkflowStatusActive, stateMap)
		return schema.NewError(schema.ErrCodeCancelled, "workflow cancelled due to timeout")

	default: // "fail"
		return schema.NewError(schema.ErrCodeTimeout, "workflow timed out")
	}
}

// --- Helpers ---

func (e *executorImpl) updateStepStatus(run *workflowRun, stepID string, status schema.StepStatus) {
	run.mu.Lock()
	if ss, ok := run.stepStates[stepID]; ok {
		ss.Status = status
	}
	run.mu.Unlock()
}

func (e *executorImpl) persistStepState(workflowID, stepID string, run *workflowRun) {
	run.mu.Lock()
	ss := run.stepStates[stepID]
	run.mu.Unlock()
	if ss != nil {
		// Best-effort persist — executor continues even if this fails.
		_ = e.store.UpsertStepState(context.Background(), ss)
	}
}

func (e *executorImpl) transitionWorkflow(ctx context.Context, workflowID string, from, to schema.WorkflowStatus) {
	// Use background context if original is cancelled.
	transCtx := ctx
	if ctx.Err() != nil {
		transCtx = context.Background()
	}
	_ = e.wfFSM.Transition(transCtx, workflowID, from, to)
	_ = e.store.UpdateWorkflow(transCtx, workflowID, store.WorkflowUpdate{Status: &to})
}

func (e *executorImpl) persistWorkflowEnd(workflowID string, result *ExecutionResult) {
	update := store.WorkflowUpdate{
		Status:      &result.Status,
		CompletedAt: result.CompletedAt,
	}
	if result.Output != nil {
		update.Output = result.Output
	}
	if result.Error != nil {
		errJSON, _ := json.Marshal(result.Error)
		update.Error = errJSON
	}
	_ = e.store.UpdateWorkflow(context.Background(), workflowID, update)
}

// buildInterpolationScope creates an InterpolationScope from the current run state.
// Step outputs are collected from completed steps, inputs from workflow params.
func (e *executorImpl) buildInterpolationScope(run *workflowRun, workflowID string, params map[string]any) *expressions.InterpolationScope {
	// Collect completed step outputs.
	stepOutputs := make(map[string]any)
	run.mu.Lock()
	for stepID, ss := range run.stepStates {
		if ss.Status == schema.StepStatusCompleted && len(ss.Output) > 0 {
			var out any
			if err := json.Unmarshal(ss.Output, &out); err == nil {
				stepOutputs[stepID] = out
			}
		}
	}
	run.mu.Unlock()

	// Build workflow metadata.
	wfMeta := map[string]any{
		"run_id": workflowID,
	}

	// Build context from store (best-effort).
	ctxData := make(map[string]any)
	wfCtx, err := e.store.GetWorkflowContext(context.Background(), workflowID)
	if err == nil && wfCtx != nil {
		ctxData["intent"] = wfCtx.OriginalIntent
		ctxData["agent_id"] = wfCtx.AgentID
		if len(wfCtx.AccumulatedData) > 0 {
			var accum map[string]any
			if err := json.Unmarshal(wfCtx.AccumulatedData, &accum); err == nil {
				for k, v := range accum {
					ctxData[k] = v
				}
			}
		}
	}

	scope := &expressions.InterpolationScope{
		Steps:    stepOutputs,
		Inputs:   params,
		Workflow: wfMeta,
		Context:  ctxData,
	}

	// Inject loop variables if passed through enriched params.
	if loopVars, ok := params["__loop_vars__"]; ok {
		if lv, ok := loopVars.(*expressions.LoopScope); ok {
			scope.Loop = lv
		}
		// Remove the internal key from the inputs view.
		cleanInputs := make(map[string]any, len(params)-1)
		for k, v := range params {
			if k != "__loop_vars__" {
				cleanInputs[k] = v
			}
		}
		scope.Inputs = cleanInputs
	}

	return scope
}

func (e *executorImpl) finalizeResult(ctx context.Context, run *workflowRun, result *ExecutionResult) {
	run.mu.Lock()
	defer run.mu.Unlock()
	for stepID, ss := range run.stepStates {
		if _, ok := result.Steps[stepID]; ok {
			continue
		}
		sr := &StepResult{
			StepID:     stepID,
			Status:     ss.Status,
			Output:     ss.Output,
			DurationMs: ss.DurationMs,
		}
		if ss.Error != nil {
			sr.Error = schema.NewError(schema.ErrCodeStepFailed, string(ss.Error))
		}
		result.Steps[stepID] = sr
	}
}
