package expressions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock vault ---

type interpMockVault struct {
	secrets map[string][]byte
	err     error
}

func (v *interpMockVault) Resolve(_ context.Context, key string) ([]byte, error) {
	if v.err != nil {
		return nil, v.err
	}
	val, ok := v.secrets[key]
	if !ok {
		return nil, errors.New("secret not found: " + key)
	}
	return val, nil
}

func (v *interpMockVault) Store(_ context.Context, _ string, _ []byte) error { return nil }
func (v *interpMockVault) Delete(_ context.Context, _ string) error          { return nil }

// --- helpers ---

func interpScope(steps map[string]any, inputs, workflow, ctxData map[string]any) *InterpolationScope {
	return &InterpolationScope{
		Steps:    steps,
		Inputs:   inputs,
		Workflow: workflow,
		Context:  ctxData,
	}
}

// --- Resolve tests ---

func TestInterpolator_NoInterpolation(t *testing.T) {
	interp := NewInterpolator(nil)
	raw := json.RawMessage(`{"url":"https://example.com","count":42}`)

	result, err := interp.Resolve(context.Background(), raw, interpScope(nil, nil, nil, nil))
	require.NoError(t, err)
	assert.JSONEq(t, `{"url":"https://example.com","count":42}`, string(result))
}

func TestInterpolator_EmptyParams(t *testing.T) {
	interp := NewInterpolator(nil)

	result, err := interp.Resolve(context.Background(), nil, interpScope(nil, nil, nil, nil))
	require.NoError(t, err)
	assert.Nil(t, result)

	result, err = interp.Resolve(context.Background(), json.RawMessage(``), interpScope(nil, nil, nil, nil))
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestInterpolator_StepOutput_Full(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(
		map[string]any{"fetch": map[string]any{"url": "https://api.example.com", "status": float64(200)}},
		nil, nil, nil,
	)

	raw := json.RawMessage(`{"data":"${{steps.fetch.output}}"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	// The full output map is serialized as JSON inline.
	assert.Contains(t, string(result), `"url"`)
	assert.Contains(t, string(result), `"status"`)
}

func TestInterpolator_StepOutput_NestedField(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(
		map[string]any{"fetch": map[string]any{"url": "https://api.example.com", "status": float64(200)}},
		nil, nil, nil,
	)

	raw := json.RawMessage(`{"target":"${{steps.fetch.output.url}}"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	assert.JSONEq(t, `{"target":"https://api.example.com"}`, string(result))
}

func TestInterpolator_StepOutput_DeepNested(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(
		map[string]any{
			"api_call": map[string]any{
				"response": map[string]any{
					"body": map[string]any{
						"items": []any{"a", "b", "c"},
					},
				},
			},
		},
		nil, nil, nil,
	)

	raw := json.RawMessage(`{"items":"${{steps.api_call.output.response.body.items}}"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	assert.Contains(t, string(result), `["a","b","c"]`)
}

func TestInterpolator_Inputs(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(
		nil,
		map[string]any{"user_id": "usr-123", "count": float64(10)},
		nil, nil,
	)

	raw := json.RawMessage(`{"user":"${{inputs.user_id}}","limit":"${{inputs.count}}"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	assert.Contains(t, string(result), `"user":"usr-123"`)
	assert.Contains(t, string(result), `"limit":"10"`)
}

func TestInterpolator_WorkflowMetadata(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(
		nil, nil,
		map[string]any{"run_id": "wf-abc-123", "name": "test-workflow"},
		nil,
	)

	raw := json.RawMessage(`{"wf_id":"${{workflow.run_id}}"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	assert.JSONEq(t, `{"wf_id":"wf-abc-123"}`, string(result))
}

func TestInterpolator_Context(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(
		nil, nil, nil,
		map[string]any{"intent": "process-order", "agent": "bot-1"},
	)

	raw := json.RawMessage(`{"intent":"${{context.intent}}"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	assert.JSONEq(t, `{"intent":"process-order"}`, string(result))
}

func TestInterpolator_Secrets(t *testing.T) {
	vault := &interpMockVault{
		secrets: map[string][]byte{
			"API_KEY":    []byte("sk-secret-123"),
			"DB_PASS":    []byte("p@ssw0rd"),
		},
	}
	interp := NewInterpolator(vault)
	scope := interpScope(nil, nil, nil, nil)

	raw := json.RawMessage(`{"api_key":"${{secrets.API_KEY}}"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	assert.JSONEq(t, `{"api_key":"sk-secret-123"}`, string(result))
}

func TestInterpolator_TwoPassOrder(t *testing.T) {
	// Verify secrets are NOT resolved in pass 1 and non-secrets are NOT resolved in pass 2.
	vault := &interpMockVault{
		secrets: map[string][]byte{
			"TOKEN": []byte("secret-token"),
		},
	}
	interp := NewInterpolator(vault)
	scope := interpScope(
		nil,
		map[string]any{"url": "https://api.example.com"},
		nil, nil,
	)

	raw := json.RawMessage(`{"url":"${{inputs.url}}","token":"${{secrets.TOKEN}}"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	assert.Contains(t, string(result), `"url":"https://api.example.com"`)
	assert.Contains(t, string(result), `"token":"secret-token"`)
}

func TestInterpolator_MultipleRefsInOneValue(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(
		nil,
		map[string]any{"host": "example.com", "port": "8080"},
		nil, nil,
	)

	raw := json.RawMessage(`{"url":"https://${{inputs.host}}:${{inputs.port}}/api"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	assert.JSONEq(t, `{"url":"https://example.com:8080/api"}`, string(result))
}

// --- Error cases ---

func TestInterpolator_UnclosedExpression(t *testing.T) {
	interp := NewInterpolator(nil)
	raw := json.RawMessage(`{"x":"${{inputs.foo"}`)

	_, err := interp.Resolve(context.Background(), raw, interpScope(nil, nil, nil, nil))
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, schema.ErrCodeInterpolation, opErr.Code)
	assert.Contains(t, opErr.Message, "unclosed")
}

func TestInterpolator_EmptyExpression(t *testing.T) {
	interp := NewInterpolator(nil)
	raw := json.RawMessage(`{"x":"${{  }}"}`)

	_, err := interp.Resolve(context.Background(), raw, interpScope(nil, nil, nil, nil))
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, schema.ErrCodeInterpolation, opErr.Code)
	assert.Contains(t, opErr.Message, "empty")
}

func TestInterpolator_NestedInterpolationRejected(t *testing.T) {
	interp := NewInterpolator(nil)
	raw := json.RawMessage(`{"x":"${{steps.${{y}}.output}}"}`)

	_, err := interp.Resolve(context.Background(), raw, interpScope(nil, nil, nil, nil))
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, schema.ErrCodeInterpolation, opErr.Code)
	assert.Contains(t, opErr.Message, "nested")
}

func TestInterpolator_UnknownNamespace(t *testing.T) {
	interp := NewInterpolator(nil)
	raw := json.RawMessage(`{"x":"${{foobar.key}}"}`)

	_, err := interp.Resolve(context.Background(), raw, interpScope(nil, nil, nil, nil))
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, schema.ErrCodeInterpolation, opErr.Code)
	assert.Contains(t, opErr.Message, "unknown namespace")
}

func TestInterpolator_MissingStep(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(
		map[string]any{"other_step": map[string]any{"val": 1}},
		nil, nil, nil,
	)

	raw := json.RawMessage(`{"x":"${{steps.missing.output}}"}`)
	_, err := interp.Resolve(context.Background(), raw, scope)
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, schema.ErrCodeInterpolation, opErr.Code)
	assert.Contains(t, opErr.Message, "not found")
	assert.Contains(t, opErr.Message, "other_step")
}

func TestInterpolator_MissingNestedField(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(
		map[string]any{"fetch": map[string]any{"url": "https://example.com"}},
		nil, nil, nil,
	)

	raw := json.RawMessage(`{"x":"${{steps.fetch.output.nonexistent}}"}`)
	_, err := interp.Resolve(context.Background(), raw, scope)
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, schema.ErrCodeInterpolation, opErr.Code)
	assert.Contains(t, opErr.Message, "not found")
	assert.Contains(t, opErr.Message, "url") // lists available fields
}

func TestInterpolator_InvalidStepRef_NoOutput(t *testing.T) {
	interp := NewInterpolator(nil)
	raw := json.RawMessage(`{"x":"${{steps.fetch.status}}"}`)
	scope := interpScope(map[string]any{"fetch": map[string]any{}}, nil, nil, nil)

	_, err := interp.Resolve(context.Background(), raw, scope)
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Contains(t, opErr.Message, "only 'output' property is supported")
}

func TestInterpolator_InvalidStepRef_TooShort(t *testing.T) {
	interp := NewInterpolator(nil)
	raw := json.RawMessage(`{"x":"${{steps.fetch}}"}`)
	scope := interpScope(map[string]any{"fetch": map[string]any{}}, nil, nil, nil)

	_, err := interp.Resolve(context.Background(), raw, scope)
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Contains(t, opErr.Message, "expected steps.<id>.output")
}

func TestInterpolator_MissingInput(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(nil, map[string]any{"a": 1}, nil, nil)

	raw := json.RawMessage(`{"x":"${{inputs.missing}}"}`)
	_, err := interp.Resolve(context.Background(), raw, scope)
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, schema.ErrCodeInterpolation, opErr.Code)
}

func TestInterpolator_SecretNotFound(t *testing.T) {
	vault := &interpMockVault{secrets: map[string][]byte{}}
	interp := NewInterpolator(vault)
	scope := interpScope(nil, nil, nil, nil)

	raw := json.RawMessage(`{"x":"${{secrets.MISSING}}"}`)
	_, err := interp.Resolve(context.Background(), raw, scope)
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, schema.ErrCodeInterpolation, opErr.Code)
	assert.Contains(t, opErr.Message, "MISSING")
}

func TestInterpolator_NoVault(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(nil, nil, nil, nil)

	raw := json.RawMessage(`{"x":"${{secrets.KEY}}"}`)
	_, err := interp.Resolve(context.Background(), raw, scope)
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Contains(t, opErr.Message, "no vault configured")
}

func TestInterpolator_TraverseNonObject(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(
		map[string]any{"fetch": map[string]any{"count": float64(42)}},
		nil, nil, nil,
	)

	raw := json.RawMessage(`{"x":"${{steps.fetch.output.count.nested}}"}`)
	_, err := interp.Resolve(context.Background(), raw, scope)
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Contains(t, opErr.Message, "cannot traverse")
}

func TestInterpolator_EmptyInputsScope(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(nil, nil, nil, nil) // inputs is nil

	raw := json.RawMessage(`{"x":"${{inputs.name}}"}`)
	_, err := interp.Resolve(context.Background(), raw, scope)
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Contains(t, opErr.Message, "scope is empty")
}

func TestInterpolator_InvalidInputsPath(t *testing.T) {
	interp := NewInterpolator(nil)
	raw := json.RawMessage(`{"x":"${{inputs.}}"}`)

	_, err := interp.Resolve(context.Background(), raw, interpScope(nil, map[string]any{}, nil, nil))
	require.Error(t, err)
}

func TestInterpolator_InvalidWorkflowPath(t *testing.T) {
	interp := NewInterpolator(nil)
	raw := json.RawMessage(`{"x":"${{workflow.}}"}`)

	_, err := interp.Resolve(context.Background(), raw, interpScope(nil, nil, map[string]any{}, nil))
	require.Error(t, err)
}

func TestInterpolator_InvalidContextPath(t *testing.T) {
	interp := NewInterpolator(nil)
	raw := json.RawMessage(`{"x":"${{context.}}"}`)

	_, err := interp.Resolve(context.Background(), raw, interpScope(nil, nil, nil, map[string]any{}))
	require.Error(t, err)
}

func TestInterpolator_InvalidSecretPath(t *testing.T) {
	interp := NewInterpolator(nil)
	raw := json.RawMessage(`{"x":"${{secrets.}}"}`)

	_, err := interp.Resolve(context.Background(), raw, interpScope(nil, nil, nil, nil))
	require.Error(t, err)
}

// --- HasInterpolation ---

func TestHasInterpolation(t *testing.T) {
	assert.True(t, HasInterpolation(json.RawMessage(`{"x":"${{inputs.a}}"}`)))
	assert.False(t, HasInterpolation(json.RawMessage(`{"x":"plain value"}`)))
	assert.False(t, HasInterpolation(json.RawMessage(`{}`)))
	assert.False(t, HasInterpolation(nil))
}

// --- DetectCircularRefs ---

func TestDetectCircularRefs_NoCycle(t *testing.T) {
	steps := map[string]*schema.StepDefinition{
		"a": {ID: "a", Params: json.RawMessage(`{"url":"${{steps.b.output.url}}"}`)},
		"b": {ID: "b", Params: json.RawMessage(`{"count":42}`)},
	}
	err := DetectCircularRefs(steps)
	assert.NoError(t, err)
}

func TestDetectCircularRefs_DirectCycle(t *testing.T) {
	steps := map[string]*schema.StepDefinition{
		"a": {ID: "a", Params: json.RawMessage(`{"url":"${{steps.b.output.url}}"}`)},
		"b": {ID: "b", Params: json.RawMessage(`{"url":"${{steps.a.output.url}}"}`)},
	}
	err := DetectCircularRefs(steps)
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, schema.ErrCodeInterpolation, opErr.Code)
	assert.Contains(t, opErr.Message, "circular")
}

func TestDetectCircularRefs_TransitiveCycle(t *testing.T) {
	steps := map[string]*schema.StepDefinition{
		"a": {ID: "a", Params: json.RawMessage(`{"x":"${{steps.b.output}}"}`)},
		"b": {ID: "b", Params: json.RawMessage(`{"x":"${{steps.c.output}}"}`)},
		"c": {ID: "c", Params: json.RawMessage(`{"x":"${{steps.a.output}}"}`)},
	}
	err := DetectCircularRefs(steps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular")
}

func TestDetectCircularRefs_NoParams(t *testing.T) {
	steps := map[string]*schema.StepDefinition{
		"a": {ID: "a"},
		"b": {ID: "b"},
	}
	err := DetectCircularRefs(steps)
	assert.NoError(t, err)
}

func TestDetectCircularRefs_SelfRef(t *testing.T) {
	steps := map[string]*schema.StepDefinition{
		"a": {ID: "a", Params: json.RawMessage(`{"x":"${{steps.a.output}}"}`)},
	}
	err := DetectCircularRefs(steps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular")
}

// --- marshalInline ---

func TestMarshalInline(t *testing.T) {
	assert.Equal(t, "hello", marshalInline("hello"))
	assert.Equal(t, "null", marshalInline(nil))
	assert.Equal(t, "true", marshalInline(true))
	assert.Equal(t, "false", marshalInline(false))
	assert.Equal(t, "42", marshalInline(float64(42)))
	assert.Equal(t, "99", marshalInline(int(99)))
	assert.Equal(t, "100", marshalInline(int64(100)))
	assert.Equal(t, `{"a":"b"}`, marshalInline(json.RawMessage(`{"a":"b"}`)))
	assert.Equal(t, `["a","b"]`, marshalInline([]any{"a", "b"}))
}

// --- extractStepRefs ---

func TestExtractStepRefs(t *testing.T) {
	refs := extractStepRefs(`${{steps.a.output.x}} and ${{steps.b.output}} plus ${{inputs.c}}`)
	assert.True(t, refs["a"])
	assert.True(t, refs["b"])
	assert.False(t, refs["c"]) // inputs, not steps
	assert.Len(t, refs, 2)
}

func TestExtractStepRefs_Empty(t *testing.T) {
	refs := extractStepRefs(`no references here`)
	assert.Empty(t, refs)
}

// --- Boolean / numeric value types ---

func TestInterpolator_BooleanOutput(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(
		map[string]any{"check": map[string]any{"enabled": true}},
		nil, nil, nil,
	)

	raw := json.RawMessage(`{"flag":"${{steps.check.output.enabled}}"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	assert.Contains(t, string(result), `"flag":"true"`)
}

func TestInterpolator_NumericOutput(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(
		map[string]any{"calc": map[string]any{"total": float64(99.5)}},
		nil, nil, nil,
	)

	raw := json.RawMessage(`{"amount":"${{steps.calc.output.total}}"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	assert.Contains(t, string(result), `"amount":"99.5"`)
}

func TestInterpolator_NullOutput(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(
		map[string]any{"step1": map[string]any{"val": nil}},
		nil, nil, nil,
	)

	raw := json.RawMessage(`{"v":"${{steps.step1.output.val}}"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	assert.Contains(t, string(result), `"v":"null"`)
}

// --- mapKeys ---

func TestMapKeys_Sorted(t *testing.T) {
	m := map[string]any{"c": 1, "a": 2, "b": 3}
	keys := mapKeys(m)
	assert.Equal(t, []string{"a", "b", "c"}, keys)
}

func TestMapKeys_Nil(t *testing.T) {
	keys := mapKeys(nil)
	assert.Nil(t, keys)
}

// --- Mixed namespaces in single params ---

func TestInterpolator_MixedNamespaces(t *testing.T) {
	vault := &interpMockVault{secrets: map[string][]byte{"KEY": []byte("secret")}}
	interp := NewInterpolator(vault)
	scope := interpScope(
		map[string]any{"s1": map[string]any{"url": "http://x"}},
		map[string]any{"name": "test"},
		map[string]any{"run_id": "wf-1"},
		map[string]any{"intent": "deploy"},
	)

	raw := json.RawMessage(`{
		"url":"${{steps.s1.output.url}}",
		"name":"${{inputs.name}}",
		"run":"${{workflow.run_id}}",
		"intent":"${{context.intent}}",
		"auth":"${{secrets.KEY}}"
	}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	s := string(result)
	assert.Contains(t, s, `"url":"http://x"`)
	assert.Contains(t, s, `"name":"test"`)
	assert.Contains(t, s, `"run":"wf-1"`)
	assert.Contains(t, s, `"intent":"deploy"`)
	assert.Contains(t, s, `"auth":"secret"`)
}

// --- Inputs with nested map ---

func TestInterpolator_InputsNestedField(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(
		nil,
		map[string]any{
			"config": map[string]any{
				"retry": map[string]any{
					"max": float64(3),
				},
			},
		},
		nil, nil,
	)

	raw := json.RawMessage(`{"max":"${{inputs.config.retry.max}}"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	assert.Contains(t, string(result), `"max":"3"`)
}

// --- VaultError propagation ---

func TestInterpolator_VaultError(t *testing.T) {
	vault := &interpMockVault{err: errors.New("vault is locked")}
	interp := NewInterpolator(vault)

	raw := json.RawMessage(`{"x":"${{secrets.KEY}}"}`)
	_, err := interp.Resolve(context.Background(), raw, interpScope(nil, nil, nil, nil))
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Contains(t, opErr.Message, "vault is locked")
}

// --- Direct key lookup with dots ---

func TestInterpolator_InputsDirectKeyWithDots(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := interpScope(
		nil,
		map[string]any{"my.key.with.dots": "found-it"},
		nil, nil,
	)

	raw := json.RawMessage(`{"x":"${{inputs.my.key.with.dots}}"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	assert.Contains(t, string(result), `"x":"found-it"`)
}
