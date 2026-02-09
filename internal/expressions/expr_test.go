package expressions

import (
	"context"
	"sync"
	"testing"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewExprEngine(t *testing.T) {
	e := NewExprEngine()
	assert.NotNil(t, e)
	assert.Equal(t, "expr", e.Name())
}

// --- Interface compliance ---

func TestExprEngine_ImplementsEngine(t *testing.T) {
	var _ Engine = (*ExprEngine)(nil)
}

// --- Basic evaluation ---

func TestExpr_IntegerLiteral(t *testing.T) {
	e := NewExprEngine()

	out, err := e.Evaluate(context.Background(), "42", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, 42, out)
}

func TestExpr_StringLiteral(t *testing.T) {
	e := NewExprEngine()

	out, err := e.Evaluate(context.Background(), `"hello"`, map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "hello", out)
}

func TestExpr_BooleanLiteral(t *testing.T) {
	e := NewExprEngine()

	out, err := e.Evaluate(context.Background(), "true", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, true, out)
}

func TestExpr_Arithmetic(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{"a": 10, "b": 3}

	t.Run("addition", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), "a + b", data)
		require.NoError(t, err)
		assert.Equal(t, 13, out)
	})

	t.Run("multiplication", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), "a * b", data)
		require.NoError(t, err)
		assert.Equal(t, 30, out)
	})

	t.Run("modulo", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), "a % b", data)
		require.NoError(t, err)
		assert.Equal(t, 1, out)
	})
}

// --- Custom variables injection ---

func TestExpr_VariableInjection(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"name":    "opcode",
		"version": 2,
		"enabled": true,
	}

	t.Run("string variable", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), "name", data)
		require.NoError(t, err)
		assert.Equal(t, "opcode", out)
	})

	t.Run("integer variable", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), "version", data)
		require.NoError(t, err)
		assert.Equal(t, 2, out)
	})

	t.Run("boolean variable", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), "enabled", data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})
}

func TestExpr_NestedVariableAccess(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"steps": map[string]any{
			"fetch": map[string]any{
				"output": map[string]any{
					"status": 200,
					"body":   "ok",
				},
			},
		},
	}

	out, err := e.Evaluate(context.Background(), `steps.fetch.output.status == 200`, data)
	require.NoError(t, err)
	assert.Equal(t, true, out)
}

func TestExpr_WorkflowVariables(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"steps": map[string]any{
			"validate": map[string]any{
				"output": map[string]any{"passed": true},
			},
		},
		"inputs": map[string]any{
			"threshold": 0.8,
		},
		"workflow": map[string]any{
			"run_id": "abc-123",
		},
		"context": map[string]any{
			"intent": "order_processing",
		},
	}

	t.Run("steps access", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `steps.validate.output.passed`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("inputs access", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `inputs.threshold`, data)
		require.NoError(t, err)
		assert.Equal(t, 0.8, out)
	})

	t.Run("workflow access", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `workflow.run_id`, data)
		require.NoError(t, err)
		assert.Equal(t, "abc-123", out)
	})
}

// --- Let bindings ---

func TestExpr_LetBindings(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"price":    100.0,
		"quantity": 5,
		"tax_rate": 0.1,
	}

	t.Run("simple let", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(),
			`let subtotal = price * quantity; subtotal`, data)
		require.NoError(t, err)
		assert.Equal(t, 500.0, out)
	})

	t.Run("chained let", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(),
			`let subtotal = price * quantity; let tax = subtotal * tax_rate; subtotal + tax`, data)
		require.NoError(t, err)
		assert.Equal(t, 550.0, out)
	})

	t.Run("let with condition", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(),
			`let total = price * quantity; total > 400 ? "bulk" : "standard"`, data)
		require.NoError(t, err)
		assert.Equal(t, "bulk", out)
	})
}

// --- Array operations ---

func TestExpr_ArrayFilter(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"items": []any{
			map[string]any{"name": "a", "active": true},
			map[string]any{"name": "b", "active": false},
			map[string]any{"name": "c", "active": true},
		},
	}

	out, err := e.Evaluate(context.Background(), `filter(items, {.active})`, data)
	require.NoError(t, err)

	arr, ok := out.([]any)
	require.True(t, ok)
	assert.Len(t, arr, 2)
}

func TestExpr_ArrayMap(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"items": []any{
			map[string]any{"name": "alice", "score": 85},
			map[string]any{"name": "bob", "score": 92},
		},
	}

	out, err := e.Evaluate(context.Background(), `map(items, {.name})`, data)
	require.NoError(t, err)

	arr, ok := out.([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"alice", "bob"}, arr)
}

func TestExpr_ArrayCount(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"numbers": []any{1, 2, 3, 4, 5},
	}

	out, err := e.Evaluate(context.Background(), `count(numbers, {# > 3})`, data)
	require.NoError(t, err)
	assert.Equal(t, 2, out)
}

func TestExpr_ArrayAny(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"items": []any{
			map[string]any{"status": "pending"},
			map[string]any{"status": "failed"},
			map[string]any{"status": "completed"},
		},
	}

	t.Run("any true", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `any(items, {.status == "failed"})`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("any false", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `any(items, {.status == "cancelled"})`, data)
		require.NoError(t, err)
		assert.Equal(t, false, out)
	})
}

func TestExpr_ArrayAll(t *testing.T) {
	e := NewExprEngine()

	t.Run("all true", func(t *testing.T) {
		data := map[string]any{
			"scores": []any{80, 90, 85, 95},
		}
		out, err := e.Evaluate(context.Background(), `all(scores, {# >= 80})`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("all false", func(t *testing.T) {
		data := map[string]any{
			"scores": []any{80, 70, 85, 95},
		}
		out, err := e.Evaluate(context.Background(), `all(scores, {# >= 80})`, data)
		require.NoError(t, err)
		assert.Equal(t, false, out)
	})
}

func TestExpr_ArraySum(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"orders": []any{
			map[string]any{"amount": 100},
			map[string]any{"amount": 200},
			map[string]any{"amount": 50},
		},
	}

	out, err := e.Evaluate(context.Background(), `sum(orders, {.amount})`, data)
	require.NoError(t, err)
	assert.Equal(t, 350, out)
}

func TestExpr_ArrayMinMax(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"values": []any{3, 1, 4, 1, 5, 9},
	}

	t.Run("min", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `min(values)`, data)
		require.NoError(t, err)
		assert.Equal(t, 1, out)
	})

	t.Run("max", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `max(values)`, data)
		require.NoError(t, err)
		assert.Equal(t, 9, out)
	})
}

func TestExpr_ArraySortBy(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"items": []any{
			map[string]any{"name": "charlie", "age": 30},
			map[string]any{"name": "alice", "age": 25},
			map[string]any{"name": "bob", "age": 28},
		},
	}

	out, err := e.Evaluate(context.Background(), `map(sortBy(items, {.age}), {.name})`, data)
	require.NoError(t, err)

	arr, ok := out.([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"alice", "bob", "charlie"}, arr)
}

func TestExpr_ArrayReduce(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"numbers": []any{1, 2, 3, 4, 5},
	}

	out, err := e.Evaluate(context.Background(), `reduce(numbers, #acc + #, 0)`, data)
	require.NoError(t, err)
	assert.Equal(t, 15, out)
}

func TestExpr_ArrayGroupBy(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"items": []any{
			map[string]any{"type": "fruit", "name": "apple"},
			map[string]any{"type": "veggie", "name": "carrot"},
			map[string]any{"type": "fruit", "name": "banana"},
		},
	}

	out, err := e.Evaluate(context.Background(), `len(groupBy(items, {.type}))`, data)
	require.NoError(t, err)
	assert.Equal(t, 2, out)
}

// --- String operations ---

func TestExpr_StringOperations(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"text":  "Hello World",
		"email": "user@example.com",
	}

	t.Run("contains", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `text contains "World"`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("not contains", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `text not contains "Missing"`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("startsWith", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `text startsWith "Hello"`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("endsWith", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `email endsWith ".com"`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("len", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `len(text)`, data)
		require.NoError(t, err)
		assert.Equal(t, 11, out)
	})

	t.Run("upper", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `upper(text)`, data)
		require.NoError(t, err)
		assert.Equal(t, "HELLO WORLD", out)
	})

	t.Run("lower", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `lower(text)`, data)
		require.NoError(t, err)
		assert.Equal(t, "hello world", out)
	})

	t.Run("trim", func(t *testing.T) {
		data := map[string]any{"s": "  hello  "}
		out, err := e.Evaluate(context.Background(), `trim(s)`, data)
		require.NoError(t, err)
		assert.Equal(t, "hello", out)
	})

	t.Run("split", func(t *testing.T) {
		data := map[string]any{"csv": "a,b,c"}
		out, err := e.Evaluate(context.Background(), `split(csv, ",")`, data)
		require.NoError(t, err)
		arr, ok := out.([]string)
		require.True(t, ok)
		assert.Equal(t, []string{"a", "b", "c"}, arr)
	})
}

func TestExpr_StringConcatenation(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"first": "Hello",
		"last":  "World",
	}

	out, err := e.Evaluate(context.Background(), `first + " " + last`, data)
	require.NoError(t, err)
	assert.Equal(t, "Hello World", out)
}

// --- Nil coalescing (??) ---

func TestExpr_NilCoalescing(t *testing.T) {
	e := NewExprEngine()

	t.Run("non-nil value", func(t *testing.T) {
		data := map[string]any{"name": "opcode"}
		out, err := e.Evaluate(context.Background(), `name ?? "default"`, data)
		require.NoError(t, err)
		assert.Equal(t, "opcode", out)
	})

	t.Run("nil value", func(t *testing.T) {
		data := map[string]any{"name": nil}
		out, err := e.Evaluate(context.Background(), `name ?? "default"`, data)
		require.NoError(t, err)
		assert.Equal(t, "default", out)
	})

	t.Run("chained coalescing", func(t *testing.T) {
		data := map[string]any{"a": nil, "b": nil}
		out, err := e.Evaluate(context.Background(), `a ?? b ?? "fallback"`, data)
		require.NoError(t, err)
		assert.Equal(t, "fallback", out)
	})
}

// --- Optional chaining (?.) ---

func TestExpr_OptionalChaining(t *testing.T) {
	e := NewExprEngine()

	t.Run("existing path", func(t *testing.T) {
		data := map[string]any{
			"user": map[string]any{"name": "Alice"},
		}
		out, err := e.Evaluate(context.Background(), `user?.name`, data)
		require.NoError(t, err)
		assert.Equal(t, "Alice", out)
	})

	t.Run("nil intermediate", func(t *testing.T) {
		data := map[string]any{"user": nil}
		out, err := e.Evaluate(context.Background(), `user?.name`, data)
		require.NoError(t, err)
		assert.Nil(t, out)
	})

	t.Run("combined with coalescing", func(t *testing.T) {
		// Provide a typed env so expr can infer fields through optional chain.
		data := map[string]any{
			"user": map[string]any{"name": "Alice"},
		}
		out, err := e.Evaluate(context.Background(), `user?.name ?? "Anonymous"`, data)
		require.NoError(t, err)
		assert.Equal(t, "Alice", out)
	})

	t.Run("coalescing on missing key", func(t *testing.T) {
		data := map[string]any{
			"config": map[string]any{},
		}
		out, err := e.Evaluate(context.Background(), `config.timeout ?? 30`, data)
		require.NoError(t, err)
		assert.Equal(t, 30, out)
	})
}

// --- Pipe chaining ---

func TestExpr_PipeChaining(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"items": []any{
			map[string]any{"name": "alice", "score": 85},
			map[string]any{"name": "bob", "score": 92},
			map[string]any{"name": "charlie", "score": 78},
		},
	}

	t.Run("filter then map", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(),
			`items | filter({.score >= 80}) | map({.name})`, data)
		require.NoError(t, err)

		arr, ok := out.([]any)
		require.True(t, ok)
		assert.Equal(t, []any{"alice", "bob"}, arr)
	})

	t.Run("filter then count", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(),
			`items | filter({.score >= 80}) | len()`, data)
		require.NoError(t, err)
		assert.Equal(t, 2, out)
	})
}

func TestExpr_PipeWithSortBy(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"users": []any{
			map[string]any{"name": "charlie", "age": 30},
			map[string]any{"name": "alice", "age": 25},
			map[string]any{"name": "bob", "age": 28},
		},
	}

	out, err := e.Evaluate(context.Background(),
		`users | sortBy({.age}) | map({.name})`, data)
	require.NoError(t, err)

	arr, ok := out.([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"alice", "bob", "charlie"}, arr)
}

// --- Ternary / conditional ---

func TestExpr_Ternary(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{"status": 200}

	t.Run("true branch", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(),
			`status == 200 ? "ok" : "error"`, data)
		require.NoError(t, err)
		assert.Equal(t, "ok", out)
	})

	t.Run("false branch", func(t *testing.T) {
		data := map[string]any{"status": 500}
		out, err := e.Evaluate(context.Background(),
			`status == 200 ? "ok" : "error"`, data)
		require.NoError(t, err)
		assert.Equal(t, "error", out)
	})
}

// --- Logical operators ---

func TestExpr_LogicalOperators(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"age":      25,
		"verified": true,
	}

	t.Run("AND", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `age >= 18 && verified`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("OR", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `age < 18 || verified`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("NOT", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `!verified`, data)
		require.NoError(t, err)
		assert.Equal(t, false, out)
	})
}

// --- In operator ---

func TestExpr_InOperator(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"role":  "admin",
		"roles": []any{"admin", "editor", "viewer"},
	}

	t.Run("in array", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `role in roles`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("not in array", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `"guest" in roles`, data)
		require.NoError(t, err)
		assert.Equal(t, false, out)
	})

	t.Run("not in operator", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `"guest" not in roles`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})
}

// --- Error handling ---

func TestExpr_EmptyExpression(t *testing.T) {
	e := NewExprEngine()

	_, err := e.Evaluate(context.Background(), "", map[string]any{})
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
	assert.Contains(t, opErr.Message, "empty")
}

func TestExpr_CompileError(t *testing.T) {
	e := NewExprEngine()

	_, err := e.Evaluate(context.Background(), `][invalid`, map[string]any{})
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
	assert.Contains(t, opErr.Message, "compile")
	assert.NotNil(t, opErr.Details)
}

func TestExpr_RuntimeError(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"items": []any{1, 2, 3},
	}

	// Accessing an out-of-bounds index triggers a runtime error.
	_, err := e.Evaluate(context.Background(), `items[100]`, data)
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeExecution, opErr.Code)
}

// --- Sandboxed: no system access ---

func TestExpr_Sandbox_NoEnvAccess(t *testing.T) {
	e := NewExprEngine()

	// Expr does not expose OS environment by default.
	// Undefined variables return nil with AllowUndefinedVariables.
	out, err := e.Evaluate(context.Background(), `HOME`, map[string]any{})
	require.NoError(t, err)
	assert.Nil(t, out)
}

func TestExpr_Sandbox_OnlyInjectedVars(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{"safe": "value"}

	out, err := e.Evaluate(context.Background(), `safe`, data)
	require.NoError(t, err)
	assert.Equal(t, "value", out)

	// Undefined variable returns nil, not system data.
	out, err = e.Evaluate(context.Background(), `dangerous`, data)
	require.NoError(t, err)
	assert.Nil(t, out)
}

// --- Program caching ---

func TestExpr_Caching(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{"x": 1}

	_, err := e.Evaluate(context.Background(), `x + 1`, data)
	require.NoError(t, err)

	e.mu.RLock()
	cacheLen := len(e.cache)
	e.mu.RUnlock()
	assert.Equal(t, 1, cacheLen)

	_, err = e.Evaluate(context.Background(), `x + 1`, data)
	require.NoError(t, err)

	e.mu.RLock()
	cacheLen2 := len(e.cache)
	e.mu.RUnlock()
	assert.Equal(t, 1, cacheLen2, "cache size should not change")
}

func TestExpr_CachingDifferentExpressions(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{"x": 1}

	_, err := e.Evaluate(context.Background(), `x + 1`, data)
	require.NoError(t, err)

	_, err = e.Evaluate(context.Background(), `x * 2`, data)
	require.NoError(t, err)

	e.mu.RLock()
	cacheLen := len(e.cache)
	e.mu.RUnlock()
	assert.Equal(t, 2, cacheLen)
}

// --- Thread safety ---

func TestExpr_Concurrent(t *testing.T) {
	e := NewExprEngine()

	var wg sync.WaitGroup
	errs := make([]error, 100)
	results := make([]any, 100)

	for i := range 100 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			data := map[string]any{"val": idx}
			results[idx], errs[idx] = e.Evaluate(context.Background(), `val >= 0`, data)
		}(i)
	}
	wg.Wait()

	for i := range 100 {
		assert.NoError(t, errs[i], "goroutine %d should not error", i)
		assert.Equal(t, true, results[i], "goroutine %d should return true", i)
	}
}

func TestExpr_ConcurrentDifferentExpressions(t *testing.T) {
	e := NewExprEngine()

	expressions := []string{
		`name == "test"`,
		`len(items) > 0`,
		`count(items, {# > 2})`,
		`upper(name)`,
	}

	datasets := []map[string]any{
		{"name": "test"},
		{"items": []any{1, 2, 3}},
		{"items": []any{1, 2, 3, 4}},
		{"name": "hello"},
	}

	expected := []any{
		true,
		true,
		2,
		"HELLO",
	}

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			exprIdx := idx % len(expressions)
			out, err := e.Evaluate(context.Background(), expressions[exprIdx], datasets[exprIdx])
			assert.NoError(t, err, "iteration %d", idx)
			assert.Equal(t, expected[exprIdx], out, "iteration %d expr %d", idx, exprIdx)
		}(i)
	}
	wg.Wait()
}

// --- Return type diversity ---

func TestExpr_ReturnTypes(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"name":  "opcode",
		"count": 42,
		"rate":  3.14,
		"items": []any{"a", "b"},
	}

	t.Run("returns bool", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `true`, data)
		require.NoError(t, err)
		assert.IsType(t, true, out)
	})

	t.Run("returns string", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `name`, data)
		require.NoError(t, err)
		assert.Equal(t, "opcode", out)
	})

	t.Run("returns int", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `count`, data)
		require.NoError(t, err)
		assert.Equal(t, 42, out)
	})

	t.Run("returns float", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `rate`, data)
		require.NoError(t, err)
		assert.Equal(t, 3.14, out)
	})

	t.Run("returns array", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `items`, data)
		require.NoError(t, err)
		arr, ok := out.([]any)
		require.True(t, ok)
		assert.Equal(t, []any{"a", "b"}, arr)
	})

	t.Run("returns map", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `{"key": "value"}`, data)
		require.NoError(t, err)
		m, ok := out.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "value", m["key"])
	})
}

// --- Nil data handling ---

func TestExpr_NilData(t *testing.T) {
	e := NewExprEngine()

	out, err := e.Evaluate(context.Background(), `42`, nil)
	require.NoError(t, err)
	assert.Equal(t, 42, out)
}

// --- Real-world workflow patterns ---

func TestExpr_RealWorld_StepGuard(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"steps": map[string]any{
			"validate": map[string]any{
				"output": map[string]any{"valid": true},
			},
			"auth": map[string]any{
				"output": map[string]any{"role": "admin"},
			},
		},
		"inputs": map[string]any{
			"enabled": true,
		},
	}

	expr := `steps.validate.output.valid && steps.auth.output.role == "admin" && inputs.enabled`
	out, err := e.Evaluate(context.Background(), expr, data)
	require.NoError(t, err)
	assert.Equal(t, true, out)
}

func TestExpr_RealWorld_OrderProcessing(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"order": map[string]any{
			"items": []any{
				map[string]any{"product": "widget", "qty": 5, "price": 10.0},
				map[string]any{"product": "gadget", "qty": 2, "price": 25.0},
				map[string]any{"product": "doohickey", "qty": 1, "price": 100.0},
			},
			"discount": 0.1,
		},
	}

	// Compute order total with discount using let bindings.
	expr := `let subtotal = sum(order.items, {.qty * .price}); let discount = subtotal * order.discount; subtotal - discount`
	out, err := e.Evaluate(context.Background(), expr, data)
	require.NoError(t, err)
	// 5*10 + 2*25 + 1*100 = 200. 200 * 0.1 = 20. 200 - 20 = 180.
	assert.Equal(t, 180.0, out)
}

func TestExpr_RealWorld_DataValidation(t *testing.T) {
	e := NewExprEngine()
	data := map[string]any{
		"records": []any{
			map[string]any{"email": "a@b.com", "age": 25},
			map[string]any{"email": "", "age": 30},
			map[string]any{"email": "c@d.com", "age": 17},
		},
	}

	t.Run("all have valid email", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(),
			`all(records, {len(.email) > 0})`, data)
		require.NoError(t, err)
		assert.Equal(t, false, out)
	})

	t.Run("count adults", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(),
			`count(records, {.age >= 18})`, data)
		require.NoError(t, err)
		assert.Equal(t, 2, out)
	})

	t.Run("filter valid records", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(),
			`filter(records, {len(.email) > 0 && .age >= 18}) | len()`, data)
		require.NoError(t, err)
		assert.Equal(t, 1, out)
	})
}
