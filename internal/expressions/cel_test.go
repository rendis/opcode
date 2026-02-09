package expressions

import (
	"context"
	"sync"
	"testing"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCELEngine(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)
	assert.NotNil(t, e)
	assert.Equal(t, "cel", e.Name())
}

// --- Interface compliance ---

func TestCELEngine_ImplementsEngine(t *testing.T) {
	var _ Engine = (*CELEngine)(nil)
}

// --- Basic evaluation ---

func TestCEL_BooleanLiteral(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	out, err := e.Evaluate(context.Background(), "true", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, true, out)
}

func TestCEL_IntegerArithmetic(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	out, err := e.Evaluate(context.Background(), "1 + 2", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, int64(3), out)
}

func TestCEL_StringConcatenation(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	out, err := e.Evaluate(context.Background(), `"hello" + " " + "world"`, map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "hello world", out)
}

// --- Step conditions ---

func TestCEL_StepCondition_InputsAccess(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	data := map[string]any{
		"inputs": map[string]any{
			"enabled": true,
			"count":   int64(5),
		},
	}

	t.Run("boolean field", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `inputs.enabled == true`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("numeric comparison", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `inputs.count > 3`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("numeric comparison false", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `inputs.count > 10`, data)
		require.NoError(t, err)
		assert.Equal(t, false, out)
	})
}

func TestCEL_StepCondition_StepsAccess(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	data := map[string]any{
		"steps": map[string]any{
			"fetch": map[string]any{
				"output": map[string]any{
					"status": int64(200),
					"body":   "ok",
				},
			},
		},
	}

	t.Run("nested field access", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `steps.fetch.output.status == 200`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("string field", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `steps.fetch.output.body == "ok"`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})
}

func TestCEL_StepCondition_WorkflowAccess(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	data := map[string]any{
		"workflow": map[string]any{
			"run_id": "abc-123",
		},
	}

	out, err := e.Evaluate(context.Background(), `workflow.run_id == "abc-123"`, data)
	require.NoError(t, err)
	assert.Equal(t, true, out)
}

func TestCEL_StepCondition_ContextAccess(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	data := map[string]any{
		"context": map[string]any{
			"intent": "order_processing",
		},
	}

	out, err := e.Evaluate(context.Background(), `context.intent == "order_processing"`, data)
	require.NoError(t, err)
	assert.Equal(t, true, out)
}

// --- Condition.if routing ---

func TestCEL_IfRouting(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	data := map[string]any{
		"inputs": map[string]any{
			"priority": "high",
		},
	}

	t.Run("condition true", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `inputs.priority == "high"`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("condition false", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `inputs.priority == "low"`, data)
		require.NoError(t, err)
		assert.Equal(t, false, out)
	})
}

// --- Condition.switch with multiple branches ---

func TestCEL_SwitchRouting(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	data := map[string]any{
		"inputs": map[string]any{
			"action": "create",
		},
	}

	// Switch expressions return a string value used to select a branch.
	out, err := e.Evaluate(context.Background(), `inputs.action`, data)
	require.NoError(t, err)
	assert.Equal(t, "create", out)
}

func TestCEL_SwitchWithTernary(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	data := map[string]any{
		"steps": map[string]any{
			"check": map[string]any{
				"output": map[string]any{
					"score": int64(85),
				},
			},
		},
	}

	expr := `steps.check.output.score >= 90 ? "excellent" : steps.check.output.score >= 70 ? "good" : "needs_work"`
	out, err := e.Evaluate(context.Background(), expr, data)
	require.NoError(t, err)
	assert.Equal(t, "good", out)
}

// --- Logical operators ---

func TestCEL_LogicalOperators(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	data := map[string]any{
		"inputs": map[string]any{
			"age":      int64(25),
			"verified": true,
		},
	}

	t.Run("AND", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `inputs.age >= 18 && inputs.verified`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("OR", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `inputs.age < 18 || inputs.verified`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("NOT", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `!inputs.verified`, data)
		require.NoError(t, err)
		assert.Equal(t, false, out)
	})
}

// --- String operations ---

func TestCEL_StringOperations(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	data := map[string]any{
		"inputs": map[string]any{
			"email": "user@example.com",
			"path":  "/api/v2/users",
		},
	}

	t.Run("contains", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `inputs.email.contains("@")`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("startsWith", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `inputs.path.startsWith("/api")`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("endsWith", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `inputs.path.endsWith("/users")`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("size", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `size(inputs.email) > 0`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})
}

// --- List operations ---

func TestCEL_ListOperations(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	data := map[string]any{
		"inputs": map[string]any{
			"tags": []any{"go", "opcode", "workflow"},
		},
	}

	t.Run("in operator", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `"go" in inputs.tags`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("not in", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `!("python" in inputs.tags)`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("size", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `size(inputs.tags) == 3`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})
}

// --- Map operations ---

func TestCEL_MapOperations(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	data := map[string]any{
		"inputs": map[string]any{
			"config": map[string]any{
				"retries": int64(3),
				"verbose": true,
			},
		},
	}

	t.Run("has macro", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `has(inputs.config.retries)`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})

	t.Run("has missing field", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `has(inputs.config.missing)`, data)
		require.NoError(t, err)
		assert.Equal(t, false, out)
	})

	t.Run("index access", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `inputs.config.retries == 3`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})
}

// --- Guard expressions (combining multiple conditions) ---

func TestCEL_GuardExpression(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	data := map[string]any{
		"steps": map[string]any{
			"validate": map[string]any{
				"output": map[string]any{
					"valid": true,
				},
			},
			"auth": map[string]any{
				"output": map[string]any{
					"role": "admin",
				},
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

// --- Error handling ---

func TestCEL_EmptyExpression(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	_, err = e.Evaluate(context.Background(), "", map[string]any{})
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
	assert.Contains(t, opErr.Message, "empty")
}

func TestCEL_CompileError(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	_, err = e.Evaluate(context.Background(), `invalid >>>`, map[string]any{})
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
	assert.Contains(t, opErr.Message, "compile")
	assert.NotNil(t, opErr.Details)
	assert.Contains(t, opErr.Details, "expression")
}

func TestCEL_TypeCheckError(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	// Adding int and string should fail type check.
	_, err = e.Evaluate(context.Background(), `inputs.count + "text"`, map[string]any{
		"inputs": map[string]any{"count": int64(5)},
	})
	// This may be a compile-time or runtime error depending on CEL's type inference with dyn.
	// With dyn types, CEL may defer to runtime. Either way we want an error.
	// With dyn, this compiles but fails at eval.
	require.Error(t, err)
}

func TestCEL_RuntimeError_MissingField(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	data := map[string]any{
		"inputs": map[string]any{},
	}

	_, err = e.Evaluate(context.Background(), `inputs.nonexistent_field > 0`, data)
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeExecution, opErr.Code)
}

func TestCEL_MissingDataKeys_DefaultToEmpty(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	// With empty data, all variables default to empty maps.
	// has() should return false for missing fields.
	out, err := e.Evaluate(context.Background(), `has(inputs.something)`, map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, false, out)
}

// --- Sandbox: no system access ---

func TestCEL_Sandbox_NoSystemAccess(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	// CEL environment only exposes steps/inputs/workflow/context.
	// Attempting to use undefined variables should fail compilation.
	_, err = e.Evaluate(context.Background(), `os.env["HOME"]`, map[string]any{})
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

// --- Program caching ---

func TestCEL_ProgramCaching(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	data := map[string]any{"inputs": map[string]any{"x": int64(1)}}

	// First call compiles and caches.
	out1, err := e.Evaluate(context.Background(), `inputs.x + 1`, data)
	require.NoError(t, err)

	e.mu.RLock()
	cacheLen := len(e.cache)
	e.mu.RUnlock()
	assert.Equal(t, 1, cacheLen, "program should be cached")

	// Second call uses cache.
	out2, err := e.Evaluate(context.Background(), `inputs.x + 1`, data)
	require.NoError(t, err)

	assert.Equal(t, out1, out2)

	e.mu.RLock()
	cacheLen2 := len(e.cache)
	e.mu.RUnlock()
	assert.Equal(t, 1, cacheLen2, "cache size should not change")
}

// --- Thread safety ---

func TestCEL_Concurrent(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make([]error, 100)
	results := make([]any, 100)

	for i := range 100 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			data := map[string]any{
				"inputs": map[string]any{
					"val": int64(idx),
				},
			}
			results[idx], errs[idx] = e.Evaluate(context.Background(), `inputs.val >= 0`, data)
		}(i)
	}
	wg.Wait()

	for i := range 100 {
		assert.NoError(t, errs[i], "goroutine %d should not error", i)
		assert.Equal(t, true, results[i], "goroutine %d should return true", i)
	}
}

func TestCEL_ConcurrentDifferentExpressions(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	expressions := []string{
		`inputs.a == "hello"`,
		`inputs.b > 10`,
		`inputs.c && true`,
		`size(inputs.d) == 2`,
	}

	datasets := []map[string]any{
		{"inputs": map[string]any{"a": "hello"}},
		{"inputs": map[string]any{"b": int64(20)}},
		{"inputs": map[string]any{"c": true}},
		{"inputs": map[string]any{"d": []any{"x", "y"}}},
	}

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			exprIdx := idx % len(expressions)
			out, err := e.Evaluate(context.Background(), expressions[exprIdx], datasets[exprIdx])
			assert.NoError(t, err, "iteration %d expr %d", idx, exprIdx)
			assert.Equal(t, true, out, "iteration %d expr %d", idx, exprIdx)
		}(i)
	}
	wg.Wait()
}

// --- Return type diversity ---

func TestCEL_ReturnTypes(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	data := map[string]any{
		"inputs": map[string]any{
			"name": "opcode",
			"val":  int64(42),
		},
	}

	t.Run("returns bool", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `true`, data)
		require.NoError(t, err)
		assert.IsType(t, true, out)
	})

	t.Run("returns string", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `inputs.name`, data)
		require.NoError(t, err)
		assert.IsType(t, "", out)
		assert.Equal(t, "opcode", out)
	})

	t.Run("returns int", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `inputs.val`, data)
		require.NoError(t, err)
		assert.Equal(t, int64(42), out)
	})

	t.Run("returns double", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `3.14`, data)
		require.NoError(t, err)
		assert.Equal(t, 3.14, out)
	})
}

// --- Complex real-world patterns ---

func TestCEL_RealWorld_ConditionalStep(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	// Simulate: skip step if previous step returned error status.
	data := map[string]any{
		"steps": map[string]any{
			"api_call": map[string]any{
				"output": map[string]any{
					"status_code": int64(500),
					"retries":     int64(3),
				},
			},
		},
	}

	expr := `steps.api_call.output.status_code >= 200 && steps.api_call.output.status_code < 300`
	out, err := e.Evaluate(context.Background(), expr, data)
	require.NoError(t, err)
	assert.Equal(t, false, out, "500 should not be in 2xx range")
}

func TestCEL_RealWorld_MultiStepGuard(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	data := map[string]any{
		"steps": map[string]any{
			"validate": map[string]any{
				"output": map[string]any{"passed": true},
			},
			"enrich": map[string]any{
				"output": map[string]any{"confidence": 0.95},
			},
		},
		"inputs": map[string]any{
			"threshold": 0.8,
		},
	}

	expr := `steps.validate.output.passed == true && steps.enrich.output.confidence >= inputs.threshold`
	out, err := e.Evaluate(context.Background(), expr, data)
	require.NoError(t, err)
	assert.Equal(t, true, out)
}

// --- Nil data handling ---

func TestCEL_NilData(t *testing.T) {
	e, err := NewCELEngine()
	require.NoError(t, err)

	// nil data should not panic.
	out, err := e.Evaluate(context.Background(), `has(inputs.x)`, nil)
	require.NoError(t, err)
	assert.Equal(t, false, out)
}
