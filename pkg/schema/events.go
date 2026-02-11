package schema

// Event type constants for the event sourcing log.
const (
	EventWorkflowStarted   = "workflow_started"
	EventWorkflowCompleted = "workflow_completed"
	EventWorkflowFailed    = "workflow_failed"
	EventWorkflowCancelled = "workflow_cancelled"
	EventWorkflowSuspended = "workflow_suspended"
	EventWorkflowResumed   = "workflow_resumed"
	EventWorkflowTimedOut  = "workflow_timed_out"

	EventStepStarted   = "step_started"
	EventStepCompleted = "step_completed"
	EventStepFailed    = "step_failed"
	EventStepSkipped   = "step_skipped"
	EventStepRetrying  = "step_retrying"
	EventStepSuspended = "step_suspended"

	EventDecisionRequested = "decision_requested"
	EventDecisionResolved  = "decision_resolved"

	EventSignalReceived = "signal_received"
	EventVariableSet    = "variable_set"
	EventDAGMutated     = "dag_mutated"

	EventStepRetryAttempt    = "step_retry_attempt"
	EventErrorHandlerInvoked = "error_handler_invoked"
	EventCircuitBreakerOpen  = "circuit_breaker_open"
	EventCircuitBreakerHalfOpen = "circuit_breaker_half_open"
	EventCircuitBreakerClosed   = "circuit_breaker_closed"
	EventStepFallback        = "step_fallback"
	EventStepIgnored         = "step_ignored"

	EventConditionEvaluated = "condition_evaluated"
	EventLoopIterStarted    = "loop_iter_started"
	EventLoopIterCompleted  = "loop_iter_completed"
	EventLoopCompleted      = "loop_completed"
	EventParallelStarted    = "parallel_started"
	EventParallelCompleted  = "parallel_completed"
	EventWaitStarted        = "wait_started"
	EventWaitCompleted      = "wait_completed"

	EventWorkflowInterrupted = "workflow_interrupted"
)

// WorkflowStatus represents the lifecycle state of a workflow.
type WorkflowStatus string

const (
	WorkflowStatusPending   WorkflowStatus = "pending"
	WorkflowStatusActive    WorkflowStatus = "active"
	WorkflowStatusSuspended WorkflowStatus = "suspended"
	WorkflowStatusCompleted WorkflowStatus = "completed"
	WorkflowStatusFailed    WorkflowStatus = "failed"
	WorkflowStatusCancelled WorkflowStatus = "cancelled"
)

// StepStatus represents the lifecycle state of a step.
type StepStatus string

const (
	StepStatusPending   StepStatus = "pending"
	StepStatusScheduled StepStatus = "scheduled"
	StepStatusRunning   StepStatus = "running"
	StepStatusCompleted StepStatus = "completed"
	StepStatusFailed    StepStatus = "failed"
	StepStatusSkipped   StepStatus = "skipped"
	StepStatusSuspended StepStatus = "suspended"
	StepStatusRetrying  StepStatus = "retrying"
)
