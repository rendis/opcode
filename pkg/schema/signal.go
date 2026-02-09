package schema

// SignalType enumerates the kinds of signals an agent can send.
type SignalType string

const (
	SignalDecision SignalType = "decision"
	SignalData     SignalType = "data"
	SignalCancel   SignalType = "cancel"
	SignalRetry    SignalType = "retry"
	SignalSkip     SignalType = "skip"
)

// Signal is an agent-initiated message to a suspended workflow.
type Signal struct {
	Type      SignalType     `json:"type"`
	StepID    string         `json:"step_id,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	Reasoning string         `json:"reasoning,omitempty"`
}

// DAGMutationAction enumerates the kinds of in-flight DAG mutations.
type DAGMutationAction string

const (
	MutationInsertAfter  DAGMutationAction = "insert_after"
	MutationInsertBefore DAGMutationAction = "insert_before"
	MutationReplace      DAGMutationAction = "replace"
	MutationRemove       DAGMutationAction = "remove"
	MutationSetVariable  DAGMutationAction = "set_variable"
)

// DAGMutation describes an in-flight modification to a running workflow's DAG.
type DAGMutation struct {
	Action     DAGMutationAction `json:"action"`
	TargetStep string            `json:"target_step"`
	Steps      []StepDefinition  `json:"steps,omitempty"`
	Variable   *VariableSet      `json:"variable,omitempty"`
}

// VariableSet injects a variable into a running workflow's context.
type VariableSet struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}
