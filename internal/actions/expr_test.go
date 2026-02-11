package actions

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rendis/opcode/pkg/schema"
)

func TestExprEvalAction_Name(t *testing.T) {
	a := ExprActions()[0].(*exprEvalAction)
	assert.Equal(t, "expr.eval", a.Name())
}

func TestExprEvalAction_Validate(t *testing.T) {
	a := ExprActions()[0].(*exprEvalAction)

	tests := []struct {
		name    string
		input   map[string]any
		wantErr bool
	}{
		{
			name:    "valid expression",
			input:   map[string]any{"expression": "1 + 1"},
			wantErr: false,
		},
		{
			name:    "empty expression",
			input:   map[string]any{"expression": ""},
			wantErr: true,
		},
		{
			name:    "missing expression",
			input:   map[string]any{},
			wantErr: true,
		},
		{
			name:    "expression not string",
			input:   map[string]any{"expression": 123},
			wantErr: true,
		},
		{
			name: "with optional data",
			input: map[string]any{
				"expression": "count(data)",
				"data":       []int{1, 2, 3},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := a.Validate(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestExprEvalAction_Execute_BasicOperations(t *testing.T) {
	a := ExprActions()[0].(*exprEvalAction)

	tests := []struct {
		name       string
		expression string
		context    map[string]any
		want       any
	}{
		{
			name:       "arithmetic",
			expression: "2 + 3 * 4",
			context:    map[string]any{},
			want:       float64(14),
		},
		{
			name:       "string concatenation",
			expression: `"hello" + " " + "world"`,
			context:    map[string]any{},
			want:       "hello world",
		},
		{
			name:       "boolean logic",
			expression: "true && false",
			context:    map[string]any{},
			want:       false,
		},
		{
			name:       "variable access",
			expression: "inputs.threshold",
			context:    map[string]any{"inputs": map[string]any{"threshold": 0.8}},
			want:       0.8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ActionInput{
				Params:  map[string]any{"expression": tt.expression},
				Context: tt.context,
			}
			output, err := a.Execute(context.Background(), input)
			require.NoError(t, err)

			var result map[string]any
			require.NoError(t, json.Unmarshal(output.Data, &result))
			assert.Equal(t, tt.want, result["result"])
		})
	}
}

func TestExprEvalAction_Execute_FilterOperation(t *testing.T) {
	a := ExprActions()[0].(*exprEvalAction)

	items := []map[string]any{
		{"id": 1, "score": 0.9, "title": "High"},
		{"id": 2, "score": 0.5, "title": "Low"},
		{"id": 3, "score": 0.85, "title": "Good"},
		{"id": 4, "score": 0.3, "title": "Poor"},
	}

	input := ActionInput{
		Params: map[string]any{
			"expression": "filter(items, {.score >= threshold})",
		},
		Context: map[string]any{
			"items":     items,
			"threshold": 0.8,
		},
	}

	output, err := a.Execute(context.Background(), input)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(output.Data, &result))

	filtered, ok := result["result"].([]any)
	require.True(t, ok, "result should be an array")
	assert.Len(t, filtered, 2, "should filter to 2 items with score >= 0.8")
}

func TestExprEvalAction_Execute_MapOperation(t *testing.T) {
	a := ExprActions()[0].(*exprEvalAction)

	items := []map[string]any{
		{"id": 1, "name": "Alice"},
		{"id": 2, "name": "Bob"},
		{"id": 3, "name": "Charlie"},
	}

	input := ActionInput{
		Params: map[string]any{
			"expression": "map(items, {.id})",
		},
		Context: map[string]any{
			"items": items,
		},
	}

	output, err := a.Execute(context.Background(), input)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(output.Data, &result))

	ids, ok := result["result"].([]any)
	require.True(t, ok, "result should be an array")
	assert.Equal(t, []any{float64(1), float64(2), float64(3)}, ids)
}

func TestExprEvalAction_Execute_ArrayOperations(t *testing.T) {
	a := ExprActions()[0].(*exprEvalAction)

	tests := []struct {
		name       string
		expression string
		context    map[string]any
		want       any
	}{
		{
			name:       "count with predicate",
			expression: "count(items, {# > 3})",
			context:    map[string]any{"items": []int{1, 2, 3, 4, 5}},
			want:       float64(2),
		},
		{
			name:       "len for array length",
			expression: "len(items)",
			context:    map[string]any{"items": []int{1, 2, 3, 4, 5}},
			want:       float64(5),
		},
		{
			name:       "sum with mapper",
			expression: "sum(items, {#})",
			context:    map[string]any{"items": []int{1, 2, 3, 4, 5}},
			want:       float64(15),
		},
		{
			name:       "min",
			expression: "min(items)",
			context:    map[string]any{"items": []int{5, 2, 8, 1, 9}},
			want:       float64(1),
		},
		{
			name:       "max",
			expression: "max(items)",
			context:    map[string]any{"items": []int{5, 2, 8, 1, 9}},
			want:       float64(9),
		},
		{
			name:       "any - true",
			expression: "any(items, {# > 5})",
			context:    map[string]any{"items": []int{1, 2, 8, 3}},
			want:       true,
		},
		{
			name:       "any - false",
			expression: "any(items, {# > 10})",
			context:    map[string]any{"items": []int{1, 2, 8, 3}},
			want:       false,
		},
		{
			name:       "all - true",
			expression: "all(items, {# > 0})",
			context:    map[string]any{"items": []int{1, 2, 8, 3}},
			want:       true,
		},
		{
			name:       "all - false",
			expression: "all(items, {# > 5})",
			context:    map[string]any{"items": []int{1, 2, 8, 3}},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ActionInput{
				Params:  map[string]any{"expression": tt.expression},
				Context: tt.context,
			}
			output, err := a.Execute(context.Background(), input)
			require.NoError(t, err)

			var result map[string]any
			require.NoError(t, json.Unmarshal(output.Data, &result))
			assert.Equal(t, tt.want, result["result"])
		})
	}
}

func TestExprEvalAction_Execute_ScopeAccess(t *testing.T) {
	a := ExprActions()[0].(*exprEvalAction)

	input := ActionInput{
		Params: map[string]any{
			"expression": "steps['fetch-data'].output.body.count + inputs.offset",
		},
		Context: map[string]any{
			"steps": map[string]any{
				"fetch-data": map[string]any{
					"output": map[string]any{
						"body": map[string]any{
							"count": 10,
						},
					},
				},
			},
			"inputs": map[string]any{
				"offset": 5,
			},
		},
	}

	output, err := a.Execute(context.Background(), input)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(output.Data, &result))
	assert.Equal(t, float64(15), result["result"])
}

func TestExprEvalAction_Execute_ExplicitData(t *testing.T) {
	a := ExprActions()[0].(*exprEvalAction)

	items := []map[string]any{
		{"level": "ERROR", "message": "failed"},
		{"level": "INFO", "message": "success"},
		{"level": "ERROR", "message": "timeout"},
	}

	input := ActionInput{
		Params: map[string]any{
			"data":       items,
			"expression": "len(filter(data, {.level == 'ERROR'}))",
		},
		Context: map[string]any{},
	}

	output, err := a.Execute(context.Background(), input)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(output.Data, &result))
	assert.Equal(t, float64(2), result["result"])
}

func TestExprEvalAction_Execute_Errors(t *testing.T) {
	a := ExprActions()[0].(*exprEvalAction)

	tests := []struct {
		name       string
		expression string
		context    map[string]any
		wantErrCode string
	}{
		{
			name:        "invalid syntax",
			expression:  "][invalid",
			context:     map[string]any{},
			wantErrCode: schema.ErrCodeValidation,
		},
		{
			name:        "runtime error - array index",
			expression:  "items[100]",
			context:     map[string]any{"items": []int{1, 2, 3}},
			wantErrCode: schema.ErrCodeExecution,
		},
		{
			name:        "type mismatch at compile",
			expression:  `"string" + 123`,
			context:     map[string]any{},
			wantErrCode: schema.ErrCodeValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ActionInput{
				Params:  map[string]any{"expression": tt.expression},
				Context: tt.context,
			}
			_, err := a.Execute(context.Background(), input)
			require.Error(t, err)

			opcodeErr, ok := err.(*schema.OpcodeError)
			require.True(t, ok, "error should be OpcodeError")
			assert.Equal(t, tt.wantErrCode, opcodeErr.Code)
		})
	}
}

func TestExprEvalAction_Schema(t *testing.T) {
	a := ExprActions()[0].(*exprEvalAction)
	schema := a.Schema()
	assert.NotEmpty(t, schema.Description)
}

func TestExprActions_Factory(t *testing.T) {
	actions := ExprActions()
	require.Len(t, actions, 1)
	assert.Equal(t, "expr.eval", actions[0].Name())
}

func TestExprEvalAction_EngineReusedAcrossCalls(t *testing.T) {
	a := ExprActions()[0].(*exprEvalAction)
	require.NotNil(t, a.engine, "engine should be initialized by factory")

	// Execute the same expression twice — second call should hit the cached compiled program.
	input := ActionInput{
		Params:  map[string]any{"expression": "1 + 1"},
		Context: map[string]any{},
	}

	out1, err := a.Execute(context.Background(), input)
	require.NoError(t, err)

	out2, err := a.Execute(context.Background(), input)
	require.NoError(t, err)

	assert.Equal(t, out1.Data, out2.Data, "same expression should produce same result")
}

func BenchmarkExprEvalAction_CachedEngine(b *testing.B) {
	a := ExprActions()[0]
	input := ActionInput{
		Params: map[string]any{
			"expression": "filter(items, {.score >= 0.8})",
		},
		Context: map[string]any{
			"items": []map[string]any{
				{"score": 0.9}, {"score": 0.5}, {"score": 0.85}, {"score": 0.3},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = a.Execute(context.Background(), input)
	}
}
