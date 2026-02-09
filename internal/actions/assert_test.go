package actions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rendis/opcode/internal/validation"
	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAssertActions(t *testing.T) []Action {
	t.Helper()
	v, err := validation.NewJSONSchemaValidator()
	require.NoError(t, err)
	return AssertActions(v)
}

func findAssertAction(t *testing.T, name string) Action {
	t.Helper()
	for _, a := range newAssertActions(t) {
		if a.Name() == name {
			return a
		}
	}
	t.Fatalf("action %s not found", name)
	return nil
}

func execAssert(t *testing.T, name string, params map[string]any) (map[string]any, error) {
	t.Helper()
	a := findAssertAction(t, name)
	out, err := a.Execute(context.Background(), ActionInput{Params: params})
	if err != nil {
		return nil, err
	}
	var result map[string]any
	require.NoError(t, json.Unmarshal(out.Data, &result))
	return result, nil
}

// --- assert.equals ---

func TestAssertEquals_Match(t *testing.T) {
	result, err := execAssert(t, "assert.equals", map[string]any{
		"expected": "hello",
		"actual":   "hello",
	})
	require.NoError(t, err)
	assert.Equal(t, true, result["pass"])
}

func TestAssertEquals_Mismatch(t *testing.T) {
	_, err := execAssert(t, "assert.equals", map[string]any{
		"expected": "hello",
		"actual":   "world",
	})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeAssertionFailed, opErr.Code)
	assert.Equal(t, "hello", opErr.Details["expected"])
	assert.Equal(t, "world", opErr.Details["actual"])
}

func TestAssertEquals_DeepEqual_Map(t *testing.T) {
	result, err := execAssert(t, "assert.equals", map[string]any{
		"expected": map[string]any{"a": float64(1), "b": map[string]any{"c": "d"}},
		"actual":   map[string]any{"a": float64(1), "b": map[string]any{"c": "d"}},
	})
	require.NoError(t, err)
	assert.Equal(t, true, result["pass"])
}

func TestAssertEquals_DeepEqual_Array(t *testing.T) {
	result, err := execAssert(t, "assert.equals", map[string]any{
		"expected": []any{float64(1), "two", float64(3)},
		"actual":   []any{float64(1), "two", float64(3)},
	})
	require.NoError(t, err)
	assert.Equal(t, true, result["pass"])
}

func TestAssertEquals_NumericTypes(t *testing.T) {
	result, err := execAssert(t, "assert.equals", map[string]any{
		"expected": int(42),
		"actual":   float64(42),
	})
	require.NoError(t, err)
	assert.Equal(t, true, result["pass"])
}

func TestAssertEquals_CustomMessage(t *testing.T) {
	_, err := execAssert(t, "assert.equals", map[string]any{
		"expected": "a",
		"actual":   "b",
		"message":  "custom failure message",
	})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, "custom failure message", opErr.Message)
}

func TestAssertEquals_Validate_MissingExpected(t *testing.T) {
	a := findAssertAction(t, "assert.equals")
	err := a.Validate(map[string]any{"actual": "x"})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

// --- assert.contains ---

func TestAssertContains_StringMatch(t *testing.T) {
	result, err := execAssert(t, "assert.contains", map[string]any{
		"haystack": "hello world",
		"needle":   "world",
	})
	require.NoError(t, err)
	assert.Equal(t, true, result["pass"])
}

func TestAssertContains_StringNoMatch(t *testing.T) {
	_, err := execAssert(t, "assert.contains", map[string]any{
		"haystack": "hello world",
		"needle":   "foo",
	})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeAssertionFailed, opErr.Code)
}

func TestAssertContains_ArrayMatch(t *testing.T) {
	result, err := execAssert(t, "assert.contains", map[string]any{
		"haystack": []any{float64(1), float64(2), float64(3)},
		"needle":   float64(2),
	})
	require.NoError(t, err)
	assert.Equal(t, true, result["pass"])
}

func TestAssertContains_ArrayNoMatch(t *testing.T) {
	_, err := execAssert(t, "assert.contains", map[string]any{
		"haystack": []any{float64(1), float64(2), float64(3)},
		"needle":   float64(5),
	})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeAssertionFailed, opErr.Code)
}

func TestAssertContains_InvalidHaystack(t *testing.T) {
	_, err := execAssert(t, "assert.contains", map[string]any{
		"haystack": float64(42),
		"needle":   "x",
	})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

// --- assert.matches ---

func TestAssertMatches_Match(t *testing.T) {
	result, err := execAssert(t, "assert.matches", map[string]any{
		"value":   "abc123",
		"pattern": `\d+`,
	})
	require.NoError(t, err)
	assert.Equal(t, true, result["pass"])
	assert.Equal(t, "123", result["matches"])
}

func TestAssertMatches_NoMatch(t *testing.T) {
	_, err := execAssert(t, "assert.matches", map[string]any{
		"value":   "abcdef",
		"pattern": `\d+`,
	})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeAssertionFailed, opErr.Code)
}

func TestAssertMatches_InvalidPattern(t *testing.T) {
	_, err := execAssert(t, "assert.matches", map[string]any{
		"value":   "test",
		"pattern": "[invalid",
	})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

// --- assert.schema ---

func TestAssertSchema_Valid(t *testing.T) {
	result, err := execAssert(t, "assert.schema", map[string]any{
		"data": map[string]any{"name": "test", "age": float64(25)},
		"schema": map[string]any{
			"type":     "object",
			"required": []any{"name"},
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"age":  map[string]any{"type": "number"},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, true, result["pass"])
}

func TestAssertSchema_Invalid(t *testing.T) {
	_, err := execAssert(t, "assert.schema", map[string]any{
		"data": map[string]any{"age": "not a number"},
		"schema": map[string]any{
			"type":     "object",
			"required": []any{"name"},
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"age":  map[string]any{"type": "number"},
			},
		},
	})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeAssertionFailed, opErr.Code)
	assert.NotNil(t, opErr.Details)
}

// --- non-retryable ---

func TestAssertionFailed_NonRetryable(t *testing.T) {
	_, err := execAssert(t, "assert.equals", map[string]any{
		"expected": "a",
		"actual":   "b",
	})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeAssertionFailed, opErr.Code)
	assert.False(t, opErr.IsRetryable(), "assertion failures should not be retryable")
}
