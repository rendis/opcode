package expressions

import "context"

// Engine evaluates expressions within workflow steps.
// Three implementations: CEL (conditions), GoJQ (transforms), Expr (logic).
type Engine interface {
	Name() string
	Evaluate(ctx context.Context, expression string, data map[string]any) (any, error)
}
