package expressions

import (
	"context"
	"fmt"
	"sync"

	"github.com/itchyny/gojq"
	"github.com/rendis/opcode/pkg/schema"
)

// GoJQEngine implements the Engine interface using GoJQ for JSON data transformation.
// It evaluates jq expressions for filtering, reshaping, and aggregating step outputs.
// Thread-safe: compiled *Code objects are cached and reused across goroutines.
type GoJQEngine struct {
	mu    sync.RWMutex
	cache map[string]*gojq.Code
}

// NewGoJQEngine creates a new GoJQ expression engine.
func NewGoJQEngine() *GoJQEngine {
	return &GoJQEngine{
		cache: make(map[string]*gojq.Code),
	}
}

// Name returns the engine identifier.
func (e *GoJQEngine) Name() string {
	return "jq"
}

// Evaluate compiles (or retrieves from cache) a jq expression and evaluates it
// against the provided data. The data map is used as the input JSON object.
//
// jq expressions can produce multiple outputs. When there is exactly one output,
// it is returned directly. When there are multiple outputs, they are collected
// into a slice and returned as []any.
func (e *GoJQEngine) Evaluate(ctx context.Context, expression string, data map[string]any) (any, error) {
	if expression == "" {
		return nil, schema.NewError(schema.ErrCodeValidation, "empty jq expression")
	}

	code, err := e.getOrCompile(expression)
	if err != nil {
		return nil, err
	}

	iter := code.RunWithContext(ctx, data)

	var results []any
	for {
		val, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := val.(error); isErr {
			return nil, schema.NewErrorf(schema.ErrCodeExecution,
				"jq evaluation failed for %q: %s", expression, err.Error()).
				WithCause(err).
				WithDetails(map[string]any{"expression": expression})
		}
		results = append(results, val)
	}

	switch len(results) {
	case 0:
		return nil, nil
	case 1:
		return results[0], nil
	default:
		return results, nil
	}
}

// getOrCompile returns a cached compiled code or compiles and caches a new one.
func (e *GoJQEngine) getOrCompile(expression string) (*gojq.Code, error) {
	e.mu.RLock()
	if code, ok := e.cache[expression]; ok {
		e.mu.RUnlock()
		return code, nil
	}
	e.mu.RUnlock()

	e.mu.Lock()
	defer e.mu.Unlock()

	// Double-check after acquiring write lock.
	if code, ok := e.cache[expression]; ok {
		return code, nil
	}

	query, err := gojq.Parse(expression)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeValidation,
			"jq parse error in %q: %s", expression, err.Error()).
			WithCause(err).
			WithDetails(map[string]any{"expression": expression})
	}

	code, err := gojq.Compile(query,
		// Sandbox: return empty env to block $ENV and env access.
		gojq.WithEnvironLoader(func() []string { return nil }),
	)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeValidation,
			"jq compile error in %q: %s", expression, err.Error()).
			WithCause(err).
			WithDetails(map[string]any{"expression": expression})
	}

	e.cache[expression] = code
	return code, nil
}

// normalizeForJQ converts Go native types to jq-compatible types.
// jq uses float64 for all numbers. This is useful if callers pass int/int64.
func normalizeForJQ(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, v := range val {
			out[k] = normalizeForJQ(v)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, v := range val {
			out[i] = normalizeForJQ(v)
		}
		return out
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case int32:
		return float64(val)
	case float32:
		return float64(val)
	default:
		return v
	}
}

// EvaluateNormalized is like Evaluate but normalizes integer types to float64
// before evaluation, which matches jq's native number handling.
func (e *GoJQEngine) EvaluateNormalized(ctx context.Context, expression string, data map[string]any) (any, error) {
	normalized, ok := normalizeForJQ(data).(map[string]any)
	if !ok {
		return nil, schema.NewError(schema.ErrCodeValidation, "data must be a JSON object")
	}
	return e.Evaluate(ctx, expression, normalized)
}

// EvaluateAll is like Evaluate but always returns a slice of all outputs,
// even if there is only one or zero results.
func (e *GoJQEngine) EvaluateAll(ctx context.Context, expression string, data map[string]any) ([]any, error) {
	if expression == "" {
		return nil, schema.NewError(schema.ErrCodeValidation, "empty jq expression")
	}

	code, err := e.getOrCompile(expression)
	if err != nil {
		return nil, err
	}

	iter := code.RunWithContext(ctx, data)

	var results []any
	for {
		val, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := val.(error); isErr {
			return nil, schema.NewErrorf(schema.ErrCodeExecution,
				"jq evaluation failed for %q: %s", expression, err.Error()).
				WithCause(err).
				WithDetails(map[string]any{"expression": expression})
		}
		results = append(results, val)
	}

	return results, nil
}

var _ Engine = (*GoJQEngine)(nil)

// Ensure normalizeForJQ handles nil gracefully.
func init() {
	if normalizeForJQ(nil) != nil {
		panic(fmt.Sprintf("normalizeForJQ(nil) returned %v", normalizeForJQ(nil)))
	}
}
