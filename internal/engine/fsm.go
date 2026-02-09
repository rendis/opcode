package engine

import (
	"context"
	"sync"

	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
)

// TransitionHook is called before or after a state transition.
type TransitionHook func(from, to string) error

// EventAppender is satisfied by the Store and EventLog; used by FSMs to emit events on transitions.
type EventAppender interface {
	AppendEvent(ctx context.Context, event *store.Event) error
}

// --- Workflow FSM ---

type workflowHookKey struct {
	from, to schema.WorkflowStatus
}

// WorkflowFSM manages workflow lifecycle state transitions.
type WorkflowFSM struct {
	mu       sync.Mutex
	appender EventAppender
	before   map[workflowHookKey][]TransitionHook
	after    map[workflowHookKey][]TransitionHook
}

// NewWorkflowFSM creates a new WorkflowFSM that emits events via the given appender.
func NewWorkflowFSM(appender EventAppender) *WorkflowFSM {
	return &WorkflowFSM{
		appender: appender,
		before:   make(map[workflowHookKey][]TransitionHook),
		after:    make(map[workflowHookKey][]TransitionHook),
	}
}

// OnBefore registers a hook called before a workflow transition.
func (f *WorkflowFSM) OnBefore(from, to schema.WorkflowStatus, hook TransitionHook) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := workflowHookKey{from, to}
	f.before[key] = append(f.before[key], hook)
}

// OnAfter registers a hook called after a workflow transition.
func (f *WorkflowFSM) OnAfter(from, to schema.WorkflowStatus, hook TransitionHook) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := workflowHookKey{from, to}
	f.after[key] = append(f.after[key], hook)
}

// Transition validates and executes a workflow state transition.
// It emits the corresponding event via the appender.
// The caller (Executor) is responsible for persisting the new state to the store.
func (f *WorkflowFSM) Transition(ctx context.Context, workflowID string, from, to schema.WorkflowStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !isValidWorkflowTransition(from, to) {
		return schema.NewErrorf(schema.ErrCodeInvalidTransition,
			"invalid workflow transition: %s -> %s", from, to).
			WithDetails(map[string]any{"workflow_id": workflowID, "from": string(from), "to": string(to)})
	}

	key := workflowHookKey{from, to}

	// Run before hooks.
	for _, hook := range f.before[key] {
		if err := hook(string(from), string(to)); err != nil {
			return err
		}
	}

	// Emit the corresponding event.
	eventType := workflowEventType(to)
	if eventType != "" {
		event := &store.Event{
			WorkflowID: workflowID,
			Type:       eventType,
		}
		if err := f.appender.AppendEvent(ctx, event); err != nil {
			return schema.NewErrorf(schema.ErrCodeStore, "emit workflow event: %s", err.Error()).WithCause(err)
		}
	}

	// Run after hooks.
	for _, hook := range f.after[key] {
		if err := hook(string(from), string(to)); err != nil {
			return err
		}
	}

	return nil
}

func isValidWorkflowTransition(from, to schema.WorkflowStatus) bool {
	allowed, ok := ValidWorkflowTransitions[from]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == to {
			return true
		}
	}
	return false
}

func workflowEventType(to schema.WorkflowStatus) string {
	switch to {
	case schema.WorkflowStatusActive:
		return schema.EventWorkflowStarted
	case schema.WorkflowStatusCompleted:
		return schema.EventWorkflowCompleted
	case schema.WorkflowStatusFailed:
		return schema.EventWorkflowFailed
	case schema.WorkflowStatusCancelled:
		return schema.EventWorkflowCancelled
	case schema.WorkflowStatusSuspended:
		return schema.EventWorkflowSuspended
	default:
		return ""
	}
}

// --- Step FSM ---

type stepHookKey struct {
	from, to schema.StepStatus
}

// StepFSM manages step lifecycle state transitions.
type StepFSM struct {
	mu       sync.Mutex
	appender EventAppender
	before   map[stepHookKey][]TransitionHook
	after    map[stepHookKey][]TransitionHook
}

// NewStepFSM creates a new StepFSM that emits events via the given appender.
func NewStepFSM(appender EventAppender) *StepFSM {
	return &StepFSM{
		appender: appender,
		before:   make(map[stepHookKey][]TransitionHook),
		after:    make(map[stepHookKey][]TransitionHook),
	}
}

// OnBefore registers a hook called before a step transition.
func (f *StepFSM) OnBefore(from, to schema.StepStatus, hook TransitionHook) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := stepHookKey{from, to}
	f.before[key] = append(f.before[key], hook)
}

// OnAfter registers a hook called after a step transition.
func (f *StepFSM) OnAfter(from, to schema.StepStatus, hook TransitionHook) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := stepHookKey{from, to}
	f.after[key] = append(f.after[key], hook)
}

// Transition validates and executes a step state transition.
// It emits the corresponding event via the appender.
func (f *StepFSM) Transition(ctx context.Context, workflowID, stepID string, from, to schema.StepStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !isValidStepTransition(from, to) {
		return schema.NewErrorf(schema.ErrCodeInvalidTransition,
			"invalid step transition: %s -> %s", from, to).
			WithStep(stepID).
			WithDetails(map[string]any{"workflow_id": workflowID, "from": string(from), "to": string(to)})
	}

	key := stepHookKey{from, to}

	// Run before hooks.
	for _, hook := range f.before[key] {
		if err := hook(string(from), string(to)); err != nil {
			return err
		}
	}

	// Emit the corresponding event.
	eventType := stepEventType(to)
	if eventType != "" {
		event := &store.Event{
			WorkflowID: workflowID,
			StepID:     stepID,
			Type:       eventType,
		}
		if err := f.appender.AppendEvent(ctx, event); err != nil {
			return schema.NewErrorf(schema.ErrCodeStore, "emit step event: %s", err.Error()).
				WithStep(stepID).WithCause(err)
		}
	}

	// Run after hooks.
	for _, hook := range f.after[key] {
		if err := hook(string(from), string(to)); err != nil {
			return err
		}
	}

	return nil
}

func isValidStepTransition(from, to schema.StepStatus) bool {
	allowed, ok := ValidStepTransitions[from]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == to {
			return true
		}
	}
	return false
}

func stepEventType(to schema.StepStatus) string {
	switch to {
	case schema.StepStatusRunning:
		return schema.EventStepStarted
	case schema.StepStatusCompleted:
		return schema.EventStepCompleted
	case schema.StepStatusFailed:
		return schema.EventStepFailed
	case schema.StepStatusSkipped:
		return schema.EventStepSkipped
	case schema.StepStatusRetrying:
		return schema.EventStepRetrying
	case schema.StepStatusSuspended:
		return schema.EventStepSuspended
	default:
		return ""
	}
}

// --- Cancel Cascade ---

// CancelWorkflow transitions a workflow to cancelled and skips all non-terminal steps.
// stepStates is a map of step_id -> current StepStatus for all known steps.
func CancelWorkflow(ctx context.Context, wfFSM *WorkflowFSM, stepFSM *StepFSM, workflowID string, currentStatus schema.WorkflowStatus, stepStates map[string]schema.StepStatus) error {
	if err := wfFSM.Transition(ctx, workflowID, currentStatus, schema.WorkflowStatusCancelled); err != nil {
		return err
	}

	for stepID, status := range stepStates {
		if isTerminalStep(status) {
			continue
		}
		// For non-terminal steps, find a valid path to skipped.
		if canSkip(status) {
			if err := stepFSM.Transition(ctx, workflowID, stepID, status, schema.StepStatusSkipped); err != nil {
				return err
			}
		}
	}
	return nil
}

func isTerminalStep(s schema.StepStatus) bool {
	return s == schema.StepStatusCompleted || s == schema.StepStatusFailed || s == schema.StepStatusSkipped
}

func canSkip(s schema.StepStatus) bool {
	return isValidStepTransition(s, schema.StepStatusSkipped)
}

// --- Transition tables (kept from scaffold) ---

// ValidWorkflowTransitions defines the allowed state transitions for workflows.
var ValidWorkflowTransitions = map[schema.WorkflowStatus][]schema.WorkflowStatus{
	schema.WorkflowStatusPending:   {schema.WorkflowStatusActive, schema.WorkflowStatusCancelled},
	schema.WorkflowStatusActive:    {schema.WorkflowStatusSuspended, schema.WorkflowStatusCompleted, schema.WorkflowStatusFailed, schema.WorkflowStatusCancelled},
	schema.WorkflowStatusSuspended: {schema.WorkflowStatusActive, schema.WorkflowStatusCancelled, schema.WorkflowStatusFailed},
	schema.WorkflowStatusCompleted: {},
	schema.WorkflowStatusFailed:    {},
	schema.WorkflowStatusCancelled: {},
}

// ValidStepTransitions defines the allowed state transitions for steps.
var ValidStepTransitions = map[schema.StepStatus][]schema.StepStatus{
	schema.StepStatusPending:   {schema.StepStatusScheduled, schema.StepStatusSkipped},
	schema.StepStatusScheduled: {schema.StepStatusRunning, schema.StepStatusSkipped, schema.StepStatusSuspended},
	schema.StepStatusRunning:   {schema.StepStatusCompleted, schema.StepStatusFailed, schema.StepStatusSuspended, schema.StepStatusRetrying},
	schema.StepStatusRetrying:  {schema.StepStatusRunning, schema.StepStatusFailed},
	schema.StepStatusSuspended: {schema.StepStatusRunning, schema.StepStatusFailed, schema.StepStatusSkipped},
	schema.StepStatusCompleted: {},
	schema.StepStatusFailed:    {},
	schema.StepStatusSkipped:   {},
}
