package actions

import (
	"context"
	"encoding/json"
)

// Action is an executable unit of work within a workflow step.
type Action interface {
	Name() string
	Schema() ActionSchema
	Execute(ctx context.Context, input ActionInput) (*ActionOutput, error)
	Validate(input map[string]any) error
}

// ActionRegistry manages the lifecycle and lookup of available actions.
type ActionRegistry interface {
	Register(action Action) error
	Get(name string) (Action, error)
	List() []ActionInfo
}

// ActionSchema describes the input/output contract of an action.
type ActionSchema struct {
	InputSchema  json.RawMessage `json:"input_schema,omitempty"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	Description  string          `json:"description,omitempty"`
}

// ActionInput is the data provided to an action at execution time.
type ActionInput struct {
	Params  map[string]any  `json:"params"`
	Context map[string]any  `json:"context,omitempty"`
}

// ActionOutput is the result of an action execution.
type ActionOutput struct {
	Data json.RawMessage `json:"data,omitempty"`
}

// ActionInfo is a summary of a registered action for listing.
type ActionInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}
