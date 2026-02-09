package expressions

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/rendis/opcode/pkg/schema"
)

// CELEngine implements the Engine interface using Google's Common Expression Language.
// It evaluates step conditions, switch/if routing, and guard expressions.
// Thread-safe: compiled programs are cached and reused across goroutines.
type CELEngine struct {
	env *cel.Env

	mu    sync.RWMutex
	cache map[string]cel.Program
}

// NewCELEngine creates a new CEL expression engine with a sandboxed environment.
// The environment exposes four top-level variables matching InterpolationScope:
//   - steps:    map(string, dyn) — step outputs keyed by step ID
//   - inputs:   map(string, dyn) — workflow input parameters
//   - workflow: map(string, dyn) — workflow metadata (run_id, etc.)
//   - context:  map(string, dyn) — workflow context (intent, etc.)
func NewCELEngine() (*CELEngine, error) {
	mapType := cel.MapType(cel.StringType, cel.DynType)

	env, err := cel.NewEnv(
		cel.Variable("steps", mapType),
		cel.Variable("inputs", mapType),
		cel.Variable("workflow", mapType),
		cel.Variable("context", mapType),
		cel.Variable("iter", mapType),
	)
	if err != nil {
		return nil, fmt.Errorf("create CEL environment: %w", err)
	}

	return &CELEngine{
		env:   env,
		cache: make(map[string]cel.Program),
	}, nil
}

// Name returns the engine identifier.
func (e *CELEngine) Name() string {
	return "cel"
}

// Evaluate compiles (or retrieves from cache) a CEL expression and evaluates it
// against the provided data. The data map should contain keys matching the
// environment variables: steps, inputs, workflow, context.
//
// Returns the evaluation result or an OpcodeError with clear, actionable messages.
func (e *CELEngine) Evaluate(ctx context.Context, expression string, data map[string]any) (any, error) {
	if expression == "" {
		return nil, schema.NewError(schema.ErrCodeValidation, "empty CEL expression")
	}

	prg, err := e.getOrCompile(expression)
	if err != nil {
		return nil, err
	}

	// Build activation with defaults for missing keys to avoid CEL runtime errors.
	activation := buildActivation(data)

	out, _, err := prg.Eval(activation)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution,
			"CEL evaluation failed for %q: %s", expression, err.Error()).
			WithCause(err).
			WithDetails(map[string]any{"expression": expression})
	}

	return out.Value(), nil
}

// getOrCompile returns a cached compiled program or compiles and caches a new one.
func (e *CELEngine) getOrCompile(expression string) (cel.Program, error) {
	e.mu.RLock()
	if prg, ok := e.cache[expression]; ok {
		e.mu.RUnlock()
		return prg, nil
	}
	e.mu.RUnlock()

	e.mu.Lock()
	defer e.mu.Unlock()

	// Double-check after acquiring write lock.
	if prg, ok := e.cache[expression]; ok {
		return prg, nil
	}

	ast, issues := e.env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, schema.NewErrorf(schema.ErrCodeValidation,
			"CEL compile error in %q: %s", expression, issues.Err().Error()).
			WithCause(issues.Err()).
			WithDetails(map[string]any{"expression": expression})
	}

	prg, err := e.env.Program(ast)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeValidation,
			"CEL program error for %q: %s", expression, err.Error()).
			WithCause(err).
			WithDetails(map[string]any{"expression": expression})
	}

	e.cache[expression] = prg
	return prg, nil
}

// buildActivation creates the evaluation activation map from the data.
// Missing keys default to empty maps to prevent CEL runtime nil-ref errors.
func buildActivation(data map[string]any) map[string]any {
	activation := make(map[string]any, 5)

	for _, key := range []string{"steps", "inputs", "workflow", "context", "iter"} {
		if v, ok := data[key]; ok && v != nil {
			activation[key] = v
		} else {
			activation[key] = map[string]any{}
		}
	}

	return activation
}
