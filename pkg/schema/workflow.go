package schema

import "encoding/json"

// WorkflowDefinition is the JSON-serializable workflow format.
// Agents provide this via opcode.run (inline) or opcode.define (template).
type WorkflowDefinition struct {
	Steps      []StepDefinition       `json:"steps"`
	Inputs     map[string]any         `json:"inputs,omitempty"`
	OnComplete *StepDefinition        `json:"on_complete,omitempty"`
	OnError    *StepDefinition        `json:"on_error,omitempty"`
	Timeout    string                 `json:"timeout,omitempty"`
	OnTimeout  string                 `json:"on_timeout,omitempty"` // fail | suspend | cancel (default: fail)
	Metadata   map[string]any         `json:"metadata,omitempty"`
}

// StepDefinition describes a single step in a workflow.
type StepDefinition struct {
	ID        string          `json:"id"`
	Type      StepType        `json:"type,omitempty"`      // action, condition, reasoning, parallel, loop (default: action)
	Action    string          `json:"action,omitempty"`     // action name (e.g. "http.request", "jq")
	Params    json.RawMessage `json:"params,omitempty"`     // action-specific parameters
	DependsOn []string        `json:"depends_on,omitempty"` // step IDs that must complete first
	Condition string          `json:"condition,omitempty"`  // CEL expression, evaluated before execution
	Retry     *RetryPolicy    `json:"retry,omitempty"`
	Timeout   string          `json:"timeout,omitempty"`    // step-level timeout (e.g. "30s", "5m")
	OnError   *ErrorHandler   `json:"on_error,omitempty"`   // error handling strategy
	Config    json.RawMessage `json:"config,omitempty"`     // type-specific config (reasoning nodes, parallel, loop)
}

// StepType enumerates the kinds of steps in a workflow.
type StepType string

const (
	StepTypeAction    StepType = "action"
	StepTypeCondition StepType = "condition"
	StepTypeReasoning StepType = "reasoning"
	StepTypeParallel  StepType = "parallel"
	StepTypeLoop      StepType = "loop"
)

// RetryPolicy configures retry behavior for a step.
type RetryPolicy struct {
	Max      int    `json:"max"`                 // max retry attempts
	Backoff  string `json:"backoff,omitempty"`    // none | linear | exponential | constant (default: none)
	Delay    string `json:"delay,omitempty"`      // initial delay (e.g. "1s", "500ms")
	MaxDelay string `json:"max_delay,omitempty"`  // cap on backoff delay (e.g. "30s")
}

// ErrorHandler configures what happens when a step fails after retries are exhausted.
type ErrorHandler struct {
	Strategy     ErrorStrategy `json:"strategy"`                // ignore | fail_workflow | fallback_step | retry
	FallbackStep string       `json:"fallback_step,omitempty"` // step ID to jump to (for fallback_step strategy)
}

// ErrorStrategy enumerates error handling strategies for steps.
type ErrorStrategy string

const (
	// ErrorStrategyIgnore marks the step as skipped and continues the workflow.
	ErrorStrategyIgnore ErrorStrategy = "ignore"
	// ErrorStrategyFailWorkflow fails the entire workflow immediately.
	ErrorStrategyFailWorkflow ErrorStrategy = "fail_workflow"
	// ErrorStrategyFallbackStep executes a designated fallback step instead.
	ErrorStrategyFallbackStep ErrorStrategy = "fallback_step"
	// ErrorStrategyRetry defers to the retry policy (default behavior if retry is configured).
	ErrorStrategyRetry ErrorStrategy = "retry"
)

// ReasoningConfig is the config block for reasoning-type steps.
type ReasoningConfig struct {
	PromptContext string            `json:"prompt_context"`
	DataInject    map[string]string `json:"data_inject,omitempty"`
	Options       []ReasoningOption `json:"options"`
	Timeout       string            `json:"timeout,omitempty"`
	Fallback      string            `json:"fallback,omitempty"`
	TargetAgent   string            `json:"target_agent,omitempty"`
}

// ReasoningOption is one choice available at a reasoning node.
type ReasoningOption struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
}

// ParallelConfig is the config block for parallel-type steps.
type ParallelConfig struct {
	Branches [][]StepDefinition `json:"branches"`
	Mode     string             `json:"mode,omitempty"` // all | race (default: all)
}

// LoopConfig is the config block for loop-type steps.
type LoopConfig struct {
	Over      string           `json:"over,omitempty"`      // expression producing iterable
	Condition string           `json:"condition,omitempty"` // while/until condition (CEL)
	Mode      string           `json:"mode,omitempty"`      // for_each | while | until
	Body      []StepDefinition `json:"body"`
	MaxIter   int              `json:"max_iter,omitempty"`
}

// ConditionConfig is the config block for condition-type steps.
type ConditionConfig struct {
	Expression string                    `json:"expression"` // CEL expression
	Branches   map[string][]StepDefinition `json:"branches"` // value → steps
	Default    []StepDefinition           `json:"default,omitempty"`
}
