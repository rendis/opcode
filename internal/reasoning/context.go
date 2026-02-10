package reasoning

import (
	"encoding/json"

	"github.com/rendis/opcode/pkg/schema"
)

// DecisionContext is the rich context stored in PendingDecision.Context.
// It provides the agent with all information needed to make a decision.
type DecisionContext struct {
	PromptContext string            `json:"prompt_context"`
	StepOutputs   map[string]any    `json:"step_outputs,omitempty"`
	WorkflowIntent string           `json:"workflow_intent,omitempty"`
	AgentNotes    string            `json:"agent_notes,omitempty"`
	DataInject    map[string]any    `json:"data_inject,omitempty"`
	AccumulatedData map[string]any  `json:"accumulated_data,omitempty"`
}

// ContextParams holds the inputs needed to build a DecisionContext.
type ContextParams struct {
	Config          schema.ReasoningConfig
	StepOutputs     map[string]any // stepID → parsed output
	WorkflowIntent  string
	AgentNotes      string
	AccumulatedData map[string]any
	ResolvedInjects map[string]any // resolved DataInject values
}

// BuildDecisionContext assembles a rich context for a reasoning node decision.
func BuildDecisionContext(p ContextParams) json.RawMessage {
	dc := DecisionContext{
		PromptContext:   p.Config.PromptContext,
		StepOutputs:     p.StepOutputs,
		WorkflowIntent:  p.WorkflowIntent,
		AgentNotes:      p.AgentNotes,
		AccumulatedData: p.AccumulatedData,
		DataInject:      p.ResolvedInjects,
	}
	data, err := json.Marshal(dc)
	if err != nil {
		// Fallback guaranteed to succeed: only uses string literal key.
		return json.RawMessage(`{"prompt_context":""}`)
	}
	return data
}
