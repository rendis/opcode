package expressions

import (
	"context"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/rendis/opcode/pkg/schema"
)

// ExprEngine implements the Engine interface using expr-lang/expr for complex
// deterministic logic. It supports let bindings, array operations (filter, map,
// count, any, all, sum, min, max), string operations, nil coalescing (??),
// optional chaining (?.), and pipe chaining (|).
// Thread-safe: compiled *vm.Program objects are cached and reused across goroutines.
type ExprEngine struct {
	mu    sync.RWMutex
	cache map[string]*vm.Program
}

// NewExprEngine creates a new Expr expression engine.
func NewExprEngine() *ExprEngine {
	return &ExprEngine{
		cache: make(map[string]*vm.Program),
	}
}

// Name returns the engine identifier.
func (e *ExprEngine) Name() string {
	return "expr"
}

// Evaluate compiles (or retrieves from cache) an Expr expression and evaluates it
// against the provided data. The data map is injected as the expression environment,
// making all keys available as top-level variables.
func (e *ExprEngine) Evaluate(ctx context.Context, expression string, data map[string]any) (any, error) {
	if expression == "" {
		return nil, schema.NewError(schema.ErrCodeValidation, "empty expr expression")
	}

	prg, err := e.getOrCompile(expression, data)
	if err != nil {
		return nil, err
	}

	env := data
	if env == nil {
		env = map[string]any{}
	}

	out, err := vm.Run(prg, env)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution,
			"expr evaluation failed for %q: %s", expression, err.Error()).
			WithCause(err).
			WithDetails(map[string]any{"expression": expression})
	}

	return out, nil
}

// getOrCompile returns a cached compiled program or compiles and caches a new one.
// The data map is used to infer the environment type for compilation.
func (e *ExprEngine) getOrCompile(expression string, data map[string]any) (*vm.Program, error) {
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

	env := data
	if env == nil {
		env = map[string]any{}
	}

	prg, err := expr.Compile(expression,
		expr.Env(env),
		expr.AllowUndefinedVariables(),
	)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeValidation,
			"expr compile error in %q: %s", expression, err.Error()).
			WithCause(err).
			WithDetails(map[string]any{"expression": expression})
	}

	e.cache[expression] = prg
	return prg, nil
}

var _ Engine = (*ExprEngine)(nil)
