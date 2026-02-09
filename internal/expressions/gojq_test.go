package expressions

import (
	"context"
	"sync"
	"testing"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGoJQEngine(t *testing.T) {
	e := NewGoJQEngine()
	assert.NotNil(t, e)
	assert.Equal(t, "jq", e.Name())
}

// --- Interface compliance ---

func TestGoJQEngine_ImplementsEngine(t *testing.T) {
	var _ Engine = (*GoJQEngine)(nil)
}

// --- Basic evaluation ---

func TestGoJQ_Identity(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{"name": "opcode"}

	out, err := e.Evaluate(context.Background(), ".", data)
	require.NoError(t, err)

	m, ok := out.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "opcode", m["name"])
}

func TestGoJQ_SelectField(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{"name": "opcode", "version": "1.0"}

	out, err := e.Evaluate(context.Background(), ".name", data)
	require.NoError(t, err)
	assert.Equal(t, "opcode", out)
}

func TestGoJQ_NestedField(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{
		"config": map[string]any{
			"retries": 3.0,
		},
	}

	out, err := e.Evaluate(context.Background(), ".config.retries", data)
	require.NoError(t, err)
	assert.Equal(t, 3.0, out)
}

func TestGoJQ_NullResult(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{"name": "opcode"}

	out, err := e.Evaluate(context.Background(), ".missing", data)
	require.NoError(t, err)
	assert.Nil(t, out)
}

// --- Select/filter/map operations ---

func TestGoJQ_ArraySelect(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{
		"items": []any{
			map[string]any{"name": "a", "active": true},
			map[string]any{"name": "b", "active": false},
			map[string]any{"name": "c", "active": true},
		},
	}

	out, err := e.Evaluate(context.Background(), `[.items[] | select(.active)]`, data)
	require.NoError(t, err)

	arr, ok := out.([]any)
	require.True(t, ok)
	assert.Len(t, arr, 2)
}

func TestGoJQ_ArrayFilter(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{
		"numbers": []any{1.0, 2.0, 3.0, 4.0, 5.0},
	}

	out, err := e.Evaluate(context.Background(), `[.numbers[] | select(. > 3)]`, data)
	require.NoError(t, err)

	arr, ok := out.([]any)
	require.True(t, ok)
	assert.Equal(t, []any{4.0, 5.0}, arr)
}

func TestGoJQ_ArrayMap(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{
		"names": []any{"alice", "bob"},
	}

	out, err := e.Evaluate(context.Background(), `[.names[] | ascii_upcase]`, data)
	require.NoError(t, err)

	arr, ok := out.([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"ALICE", "BOB"}, arr)
}

func TestGoJQ_ObjectConstruction(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{
		"user": map[string]any{
			"first_name": "Alice",
			"last_name":  "Smith",
			"age":        30.0,
		},
	}

	out, err := e.Evaluate(context.Background(), `{name: (.user.first_name + " " + .user.last_name), age: .user.age}`, data)
	require.NoError(t, err)

	m, ok := out.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Alice Smith", m["name"])
	assert.Equal(t, 30.0, m["age"])
}

// --- Aggregation ---

func TestGoJQ_AggregationAdd(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{
		"values": []any{1.0, 2.0, 3.0, 4.0},
	}

	out, err := e.Evaluate(context.Background(), `.values | add`, data)
	require.NoError(t, err)
	assert.Equal(t, 10.0, out)
}

func TestGoJQ_AggregationLength(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{
		"items": []any{"a", "b", "c"},
	}

	out, err := e.Evaluate(context.Background(), `.items | length`, data)
	require.NoError(t, err)
	assert.Equal(t, 3, out)
}

func TestGoJQ_AggregationGroupBy(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{
		"items": []any{
			map[string]any{"type": "fruit", "name": "apple"},
			map[string]any{"type": "veggie", "name": "carrot"},
			map[string]any{"type": "fruit", "name": "banana"},
		},
	}

	out, err := e.Evaluate(context.Background(), `[.items | group_by(.type)[] | {type: .[0].type, count: length}]`, data)
	require.NoError(t, err)

	arr, ok := out.([]any)
	require.True(t, ok)
	assert.Len(t, arr, 2)
}

func TestGoJQ_AggregationMinMax(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{
		"values": []any{3.0, 1.0, 4.0, 1.0, 5.0},
	}

	t.Run("min", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `.values | min`, data)
		require.NoError(t, err)
		assert.Equal(t, 1.0, out)
	})

	t.Run("max", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `.values | max`, data)
		require.NoError(t, err)
		assert.Equal(t, 5.0, out)
	})
}

func TestGoJQ_AggregationUnique(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{
		"tags": []any{"go", "rust", "go", "python", "rust"},
	}

	out, err := e.Evaluate(context.Background(), `.tags | unique`, data)
	require.NoError(t, err)

	arr, ok := out.([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"go", "python", "rust"}, arr)
}

// --- Multiple outputs ---

func TestGoJQ_MultipleOutputs(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{
		"items": []any{"a", "b", "c"},
	}

	// .items[] without wrapping produces multiple outputs.
	out, err := e.Evaluate(context.Background(), `.items[]`, data)
	require.NoError(t, err)

	arr, ok := out.([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"a", "b", "c"}, arr)
}

func TestGoJQ_EvaluateAll(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{
		"items": []any{"a", "b"},
	}

	results, err := e.EvaluateAll(context.Background(), `.items[]`, data)
	require.NoError(t, err)
	assert.Equal(t, []any{"a", "b"}, results)
}

func TestGoJQ_EvaluateAll_Empty(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{
		"items": []any{},
	}

	results, err := e.EvaluateAll(context.Background(), `.items[]`, data)
	require.NoError(t, err)
	assert.Nil(t, results)
}

// --- Step output transformation (real-world) ---

func TestGoJQ_TransformStepOutput(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{
		"status_code": 200.0,
		"body": map[string]any{
			"users": []any{
				map[string]any{"id": 1.0, "name": "Alice", "role": "admin"},
				map[string]any{"id": 2.0, "name": "Bob", "role": "user"},
				map[string]any{"id": 3.0, "name": "Charlie", "role": "admin"},
			},
		},
	}

	// Extract admin names from API response.
	out, err := e.Evaluate(context.Background(), `[.body.users[] | select(.role == "admin") | .name]`, data)
	require.NoError(t, err)

	arr, ok := out.([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"Alice", "Charlie"}, arr)
}

func TestGoJQ_ReshapeOutput(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{
		"results": []any{
			map[string]any{"key": "a", "value": 1.0},
			map[string]any{"key": "b", "value": 2.0},
		},
	}

	// Convert array of key-value pairs to object.
	out, err := e.Evaluate(context.Background(), `[.results[] | {(.key): .value}] | add`, data)
	require.NoError(t, err)

	m, ok := out.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 1.0, m["a"])
	assert.Equal(t, 2.0, m["b"])
}

// --- Error handling ---

func TestGoJQ_EmptyExpression(t *testing.T) {
	e := NewGoJQEngine()

	_, err := e.Evaluate(context.Background(), "", map[string]any{})
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
	assert.Contains(t, opErr.Message, "empty")
}

func TestGoJQ_ParseError(t *testing.T) {
	e := NewGoJQEngine()

	_, err := e.Evaluate(context.Background(), `.[invalid`, map[string]any{})
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
	assert.Contains(t, opErr.Message, "parse")
	assert.NotNil(t, opErr.Details)
}

func TestGoJQ_RuntimeError(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{"name": "opcode"}

	// Trying to iterate a string as array.
	_, err := e.Evaluate(context.Background(), `.name[]`, data)
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeExecution, opErr.Code)
}

// --- Sandbox: no filesystem/network/env access ---

func TestGoJQ_Sandbox_NoEnvAccess(t *testing.T) {
	e := NewGoJQEngine()

	// $ENV should be empty (sandboxed).
	out, err := e.Evaluate(context.Background(), `$ENV`, map[string]any{})
	require.NoError(t, err)

	// With empty environ loader, $ENV returns an empty object.
	m, ok := out.(map[string]any)
	require.True(t, ok)
	assert.Empty(t, m)
}

func TestGoJQ_Sandbox_NoEnvFunction(t *testing.T) {
	e := NewGoJQEngine()

	// env.HOME should return null with sandboxed environ loader.
	out, err := e.Evaluate(context.Background(), `env.HOME`, map[string]any{})
	require.NoError(t, err)
	assert.Nil(t, out)
}

// --- Program caching ---

func TestGoJQ_Caching(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{"x": 1.0}

	_, err := e.Evaluate(context.Background(), `.x`, data)
	require.NoError(t, err)

	e.mu.RLock()
	cacheLen := len(e.cache)
	e.mu.RUnlock()
	assert.Equal(t, 1, cacheLen)

	_, err = e.Evaluate(context.Background(), `.x`, data)
	require.NoError(t, err)

	e.mu.RLock()
	cacheLen2 := len(e.cache)
	e.mu.RUnlock()
	assert.Equal(t, 1, cacheLen2)
}

// --- Thread safety ---

func TestGoJQ_Concurrent(t *testing.T) {
	e := NewGoJQEngine()

	var wg sync.WaitGroup
	errs := make([]error, 100)
	results := make([]any, 100)

	for i := range 100 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			data := map[string]any{"val": float64(idx)}
			results[idx], errs[idx] = e.Evaluate(context.Background(), `.val + 1`, data)
		}(i)
	}
	wg.Wait()

	for i := range 100 {
		assert.NoError(t, errs[i], "goroutine %d", i)
		assert.Equal(t, float64(i)+1, results[i], "goroutine %d", i)
	}
}

func TestGoJQ_ConcurrentDifferentExpressions(t *testing.T) {
	e := NewGoJQEngine()

	expressions := []string{
		`.name`,
		`.items | length`,
		`[.items[] | select(. > 2)]`,
		`.name | ascii_upcase`,
	}

	datasets := []map[string]any{
		{"name": "test"},
		{"items": []any{1.0, 2.0, 3.0}},
		{"items": []any{1.0, 2.0, 3.0, 4.0}},
		{"name": "hello"},
	}

	expected := []any{
		"test",
		3,
		[]any{3.0, 4.0},
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

// --- Normalize ---

func TestGoJQ_EvaluateNormalized(t *testing.T) {
	e := NewGoJQEngine()
	// int types need normalization for jq.
	data := map[string]any{
		"count": int64(5),
		"items": []any{int(1), int(2), int(3)},
	}

	out, err := e.EvaluateNormalized(context.Background(), `.count + 1`, data)
	require.NoError(t, err)
	assert.Equal(t, 6.0, out)
}

// --- Pipe chains ---

func TestGoJQ_PipeChain(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{
		"data": []any{
			map[string]any{"name": "alice", "score": 85.0},
			map[string]any{"name": "bob", "score": 92.0},
			map[string]any{"name": "charlie", "score": 78.0},
		},
	}

	expr := `[.data[] | select(.score >= 80)] | sort_by(.score) | reverse | .[0].name`
	out, err := e.Evaluate(context.Background(), expr, data)
	require.NoError(t, err)
	assert.Equal(t, "bob", out)
}

// --- String operations ---

func TestGoJQ_StringOperations(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{"text": "hello world"}

	t.Run("split", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `.text | split(" ")`, data)
		require.NoError(t, err)
		arr, ok := out.([]any)
		require.True(t, ok)
		assert.Equal(t, []any{"hello", "world"}, arr)
	})

	t.Run("length", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `.text | length`, data)
		require.NoError(t, err)
		assert.Equal(t, 11, out)
	})

	t.Run("test regex", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `.text | test("hello")`, data)
		require.NoError(t, err)
		assert.Equal(t, true, out)
	})
}

// --- Conditional expressions ---

func TestGoJQ_IfThenElse(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{"status": 200.0}

	out, err := e.Evaluate(context.Background(), `if .status == 200 then "ok" else "error" end`, data)
	require.NoError(t, err)
	assert.Equal(t, "ok", out)
}

// --- Type conversions ---

func TestGoJQ_TypeConversions(t *testing.T) {
	e := NewGoJQEngine()
	data := map[string]any{"num": 42.0, "str": "123"}

	t.Run("to string", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `.num | tostring`, data)
		require.NoError(t, err)
		assert.Equal(t, "42", out)
	})

	t.Run("to number", func(t *testing.T) {
		out, err := e.Evaluate(context.Background(), `.str | tonumber`, data)
		require.NoError(t, err)
		// gojq returns int for whole numbers from tonumber.
		assert.Equal(t, 123, out)
	})
}

// --- Nil data handling ---

func TestGoJQ_NilData(t *testing.T) {
	e := NewGoJQEngine()

	out, err := e.Evaluate(context.Background(), `.`, nil)
	require.NoError(t, err)
	assert.Nil(t, out)
}

// --- normalizeForJQ ---

func TestNormalizeForJQ(t *testing.T) {
	input := map[string]any{
		"int_val":   42,
		"int64_val": int64(100),
		"float_val": 3.14,
		"str_val":   "hello",
		"nested": map[string]any{
			"count": int(5),
		},
		"list": []any{int(1), int(2)},
	}

	result := normalizeForJQ(input).(map[string]any)

	assert.Equal(t, 42.0, result["int_val"])
	assert.Equal(t, 100.0, result["int64_val"])
	assert.Equal(t, 3.14, result["float_val"])
	assert.Equal(t, "hello", result["str_val"])

	nested := result["nested"].(map[string]any)
	assert.Equal(t, 5.0, nested["count"])

	list := result["list"].([]any)
	assert.Equal(t, 1.0, list[0])
	assert.Equal(t, 2.0, list[1])
}
