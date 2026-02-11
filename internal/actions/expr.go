package actions

import (
	"context"
	"encoding/json"

	"github.com/rendis/opcode/internal/expressions"
	"github.com/rendis/opcode/pkg/schema"
)

// ExprActions returns all expression evaluation actions.
func ExprActions() []Action {
	return []Action{
		&exprEvalAction{engine: expressions.NewExprEngine()},
	}
}

// --- expr.eval ---

type exprEvalAction struct {
	engine *expressions.ExprEngine
}

func (a *exprEvalAction) Name() string { return "expr.eval" }

func (a *exprEvalAction) Schema() ActionSchema {
	return ActionSchema{
		Description: "Evaluate an Expr expression against the current scope or explicit data",
	}
}

func (a *exprEvalAction) Validate(input map[string]any) error {
	expr, ok := input["expression"].(string)
	if !ok || expr == "" {
		return schema.NewError(schema.ErrCodeValidation, "expr.eval requires non-empty 'expression' string parameter")
	}
	return nil
}

func (a *exprEvalAction) Execute(ctx context.Context, input ActionInput) (*ActionOutput, error) {
	expression, _ := input.Params["expression"].(string)

	// Build evaluation scope.
	scope := make(map[string]any)

	// If explicit data is provided, use it as the primary scope.
	if data, ok := input.Params["data"]; ok {
		scope["data"] = data
	}

	// Merge ActionInput.Context into scope (steps, inputs, workflow, context).
	for k, v := range input.Context {
		scope[k] = v
	}

	result, err := a.engine.Evaluate(ctx, expression, scope)
	if err != nil {
		return nil, err
	}

	// Marshal result to JSON output.
	out, err := json.Marshal(map[string]any{
		"result": result,
	})
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "expr.eval: marshal output: %v", err)
	}

	return &ActionOutput{Data: out}, nil
}
