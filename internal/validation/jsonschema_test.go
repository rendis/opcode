package validation

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewJSONSchemaValidator(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)
	assert.NotNil(t, v)
	assert.NotNil(t, v.workflowSchema)
}

// --- ValidateDefinition ---

func TestValidateDefinition_Nil(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	err = v.ValidateDefinition(nil)
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
	assert.Contains(t, opErr.Message, "nil")
}

func TestValidateDefinition_MinimalValid(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "step-1"},
		},
	}
	err = v.ValidateDefinition(def)
	assert.NoError(t, err)
}

func TestValidateDefinition_FullValid(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{
				ID:        "fetch-data",
				Type:      schema.StepTypeAction,
				Action:    "http.request",
				Params:    json.RawMessage(`{"url": "https://api.example.com"}`),
				DependsOn: []string{},
				Condition: "inputs.enabled == true",
				Retry: &schema.RetryPolicy{
					Max:     3,
					Backoff: "exponential",
					Delay:   "1s",
				},
				Timeout: "30s",
				OnError: &schema.ErrorHandler{
					Strategy:     schema.ErrorStrategyFallbackStep,
					FallbackStep: "handle-error",
				},
			},
			{
				ID:     "handle-error",
				Type:   schema.StepTypeAction,
				Action: "log",
			},
		},
		Inputs:    map[string]any{"enabled": true},
		Timeout:   "5m",
		OnTimeout: "suspend",
		Metadata:  map[string]any{"version": "1.0"},
	}
	err = v.ValidateDefinition(def)
	assert.NoError(t, err)
}

func TestValidateDefinition_EmptySteps(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{},
	}
	err = v.ValidateDefinition(def)
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestValidateDefinition_MissingSteps(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{}
	err = v.ValidateDefinition(def)
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestValidateDefinition_StepMissingID(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: ""},
		},
	}
	err = v.ValidateDefinition(def)
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestValidateDefinition_DuplicateStepIDs(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "step-1"},
			{ID: "step-1"},
		},
	}
	err = v.ValidateDefinition(def)
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
	assert.Contains(t, opErr.Message, "duplicate")
}

func TestValidateDefinition_InvalidStepType(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "step-1", Type: "invalid_type"},
		},
	}
	err = v.ValidateDefinition(def)
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestValidateDefinition_InvalidTimeout(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "step-1"},
		},
		Timeout: "not-a-duration",
	}
	err = v.ValidateDefinition(def)
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestValidateDefinition_InvalidOnTimeout(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "step-1"},
		},
		OnTimeout: "explode",
	}
	err = v.ValidateDefinition(def)
	require.Error(t, err)
}

func TestValidateDefinition_InvalidRetryBackoff(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{
				ID: "step-1",
				Retry: &schema.RetryPolicy{
					Max:     1,
					Backoff: "random",
				},
			},
		},
	}
	err = v.ValidateDefinition(def)
	require.Error(t, err)
}

func TestValidateDefinition_AllStepTypes(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	types := []schema.StepType{
		schema.StepTypeAction,
		schema.StepTypeCondition,
		schema.StepTypeReasoning,
		schema.StepTypeParallel,
		schema.StepTypeLoop,
	}
	for _, st := range types {
		def := &schema.WorkflowDefinition{
			Steps: []schema.StepDefinition{
				{ID: "step-" + string(st), Type: st},
			},
		}
		err = v.ValidateDefinition(def)
		assert.NoError(t, err, "step type %s should be valid", st)
	}
}

func TestValidateDefinition_OnCompleteStep(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "main"},
		},
		OnComplete: &schema.StepDefinition{
			ID:     "cleanup",
			Action: "notify",
		},
	}
	err = v.ValidateDefinition(def)
	assert.NoError(t, err)
}

func TestValidateDefinition_OnErrorStep(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "main"},
		},
		OnError: &schema.StepDefinition{
			ID:     "error-handler",
			Action: "log.error",
		},
	}
	err = v.ValidateDefinition(def)
	assert.NoError(t, err)
}

func TestValidateDefinition_ValidTimeouts(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	validTimeouts := []string{"100ms", "30s", "5m", "1h", "500ns", "10us"}
	for _, timeout := range validTimeouts {
		def := &schema.WorkflowDefinition{
			Steps: []schema.StepDefinition{
				{ID: "step-1", Timeout: timeout},
			},
		}
		err = v.ValidateDefinition(def)
		assert.NoError(t, err, "timeout %q should be valid", timeout)
	}
}

func TestValidateDefinition_ErrorDetails(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: ""},
		},
	}
	err = v.ValidateDefinition(def)
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.NotNil(t, opErr.Details)
	assert.Contains(t, opErr.Details, "violations")
}

// --- ValidateInput ---

func TestValidateInput_NilInput(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	err = v.ValidateInput(nil, []byte(`{"type": "object"}`))
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
	assert.Contains(t, opErr.Message, "nil")
}

func TestValidateInput_EmptySchema(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	err = v.ValidateInput(map[string]any{"foo": "bar"}, nil)
	assert.NoError(t, err, "nil schema means no validation")

	err = v.ValidateInput(map[string]any{"foo": "bar"}, []byte{})
	assert.NoError(t, err, "empty schema means no validation")
}

func TestValidateInput_ValidObject(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{
		"type": "object",
		"required": ["name", "count"],
		"properties": {
			"name": {"type": "string"},
			"count": {"type": "integer", "minimum": 1}
		}
	}`)

	input := map[string]any{
		"name":  "test",
		"count": 5,
	}

	err = v.ValidateInput(input, inputSchema)
	assert.NoError(t, err)
}

func TestValidateInput_MissingRequired(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{
		"type": "object",
		"required": ["name"],
		"properties": {
			"name": {"type": "string"}
		}
	}`)

	input := map[string]any{
		"other": "value",
	}

	err = v.ValidateInput(input, inputSchema)
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestValidateInput_WrongType(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{
		"type": "object",
		"properties": {
			"count": {"type": "integer"}
		}
	}`)

	input := map[string]any{
		"count": "not-a-number",
	}

	err = v.ValidateInput(input, inputSchema)
	require.Error(t, err)
}

func TestValidateInput_MinimumViolation(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{
		"type": "object",
		"properties": {
			"age": {"type": "integer", "minimum": 0}
		}
	}`)

	input := map[string]any{
		"age": -1,
	}

	err = v.ValidateInput(input, inputSchema)
	require.Error(t, err)
}

func TestValidateInput_StringPattern(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{
		"type": "object",
		"properties": {
			"code": {"type": "string", "pattern": "^[A-Z]{3}$"}
		}
	}`)

	t.Run("valid", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"code": "ABC"}, inputSchema)
		assert.NoError(t, err)
	})

	t.Run("invalid", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"code": "abc"}, inputSchema)
		require.Error(t, err)
	})
}

func TestValidateInput_NestedObject(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{
		"type": "object",
		"properties": {
			"address": {
				"type": "object",
				"required": ["city"],
				"properties": {
					"city": {"type": "string"},
					"zip": {"type": "string", "pattern": "^[0-9]{5}$"}
				}
			}
		}
	}`)

	t.Run("valid nested", func(t *testing.T) {
		input := map[string]any{
			"address": map[string]any{
				"city": "Portland",
				"zip":  "97201",
			},
		}
		err := v.ValidateInput(input, inputSchema)
		assert.NoError(t, err)
	})

	t.Run("missing nested required", func(t *testing.T) {
		input := map[string]any{
			"address": map[string]any{
				"zip": "97201",
			},
		}
		err := v.ValidateInput(input, inputSchema)
		require.Error(t, err)
	})
}

func TestValidateInput_ArrayItems(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{
		"type": "object",
		"properties": {
			"tags": {
				"type": "array",
				"items": {"type": "string"},
				"minItems": 1
			}
		}
	}`)

	t.Run("valid array", func(t *testing.T) {
		input := map[string]any{"tags": []any{"go", "opcode"}}
		err := v.ValidateInput(input, inputSchema)
		assert.NoError(t, err)
	})

	t.Run("empty array", func(t *testing.T) {
		input := map[string]any{"tags": []any{}}
		err := v.ValidateInput(input, inputSchema)
		require.Error(t, err)
	})

	t.Run("wrong item type", func(t *testing.T) {
		input := map[string]any{"tags": []any{123}}
		err := v.ValidateInput(input, inputSchema)
		require.Error(t, err)
	})
}

func TestValidateInput_Enum(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{
		"type": "object",
		"properties": {
			"priority": {"type": "string", "enum": ["low", "medium", "high"]}
		}
	}`)

	t.Run("valid enum", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"priority": "high"}, inputSchema)
		assert.NoError(t, err)
	})

	t.Run("invalid enum", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"priority": "critical"}, inputSchema)
		require.Error(t, err)
	})
}

func TestValidateInput_FormatEmail(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{
		"type": "object",
		"properties": {
			"email": {"type": "string", "format": "email"}
		}
	}`)

	t.Run("valid email", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"email": "user@example.com"}, inputSchema)
		assert.NoError(t, err)
	})

	t.Run("invalid email", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"email": "not-an-email"}, inputSchema)
		require.Error(t, err)
	})
}

func TestValidateInput_FormatURI(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "format": "uri"}
		}
	}`)

	t.Run("valid uri", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"url": "https://example.com/path"}, inputSchema)
		assert.NoError(t, err)
	})

	t.Run("invalid uri", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"url": "not a uri"}, inputSchema)
		require.Error(t, err)
	})
}

func TestValidateInput_FormatDateTime(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{
		"type": "object",
		"properties": {
			"ts": {"type": "string", "format": "date-time"}
		}
	}`)

	t.Run("valid date-time", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"ts": "2026-02-09T10:30:00Z"}, inputSchema)
		assert.NoError(t, err)
	})

	t.Run("invalid date-time", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"ts": "not-a-datetime"}, inputSchema)
		require.Error(t, err)
	})
}

func TestValidateInput_RefSupport(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{
		"type": "object",
		"properties": {
			"primary": { "$ref": "#/$defs/address" },
			"billing": { "$ref": "#/$defs/address" }
		},
		"$defs": {
			"address": {
				"type": "object",
				"required": ["street", "city"],
				"properties": {
					"street": {"type": "string"},
					"city": {"type": "string"}
				}
			}
		}
	}`)

	t.Run("valid with ref", func(t *testing.T) {
		input := map[string]any{
			"primary": map[string]any{"street": "123 Main", "city": "Portland"},
			"billing": map[string]any{"street": "456 Oak", "city": "Seattle"},
		}
		err := v.ValidateInput(input, inputSchema)
		assert.NoError(t, err)
	})

	t.Run("invalid ref target", func(t *testing.T) {
		input := map[string]any{
			"primary": map[string]any{"street": "123 Main"}, // missing city
		}
		err := v.ValidateInput(input, inputSchema)
		require.Error(t, err)
	})
}

func TestValidateInput_InvalidSchema(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	err = v.ValidateInput(map[string]any{"foo": "bar"}, []byte(`{not json`))
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
	assert.Contains(t, opErr.Message, "invalid input schema")
}

// --- Schema caching ---

func TestValidateInput_SchemaCaching(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{"type": "object", "properties": {"x": {"type": "integer"}}}`)
	input := map[string]any{"x": 42}

	// First call compiles and caches.
	err = v.ValidateInput(input, inputSchema)
	assert.NoError(t, err)

	v.mu.RLock()
	cacheLen := len(v.cache)
	v.mu.RUnlock()
	assert.Equal(t, 1, cacheLen, "schema should be cached")

	// Second call uses cache.
	err = v.ValidateInput(input, inputSchema)
	assert.NoError(t, err)

	v.mu.RLock()
	cacheLen2 := len(v.cache)
	v.mu.RUnlock()
	assert.Equal(t, 1, cacheLen2, "cache size should not change")
}

// --- Thread safety ---

func TestValidateInput_Concurrent(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	schema1 := []byte(`{"type": "object", "properties": {"a": {"type": "string"}}}`)
	schema2 := []byte(`{"type": "object", "properties": {"b": {"type": "integer"}}}`)

	var wg sync.WaitGroup
	errs := make([]error, 100)

	for i := range 100 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var s []byte
			var input map[string]any
			if idx%2 == 0 {
				s = schema1
				input = map[string]any{"a": "hello"}
			} else {
				s = schema2
				input = map[string]any{"b": 42}
			}
			errs[idx] = v.ValidateInput(input, s)
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		assert.NoError(t, e, "goroutine %d should not error", i)
	}
}

func TestValidateDefinition_Concurrent(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make([]error, 50)

	for i := range 50 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			def := &schema.WorkflowDefinition{
				Steps: []schema.StepDefinition{
					{ID: "step-1", Type: schema.StepTypeAction, Action: "test"},
				},
			}
			errs[idx] = v.ValidateDefinition(def)
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		assert.NoError(t, e, "goroutine %d should not error", i)
	}
}

// --- Additional property validation ---

func TestValidateInput_AdditionalProperties(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"}
		},
		"additionalProperties": false
	}`)

	t.Run("no extra props", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"name": "test"}, inputSchema)
		assert.NoError(t, err)
	})

	t.Run("extra props rejected", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"name": "test", "extra": true}, inputSchema)
		require.Error(t, err)
	})
}

// --- Multiple errors ---

func TestValidateInput_MultipleErrors(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{
		"type": "object",
		"required": ["name", "age"],
		"properties": {
			"name": {"type": "string", "minLength": 1},
			"age": {"type": "integer", "minimum": 0}
		}
	}`)

	input := map[string]any{} // missing both required fields
	err = v.ValidateInput(input, inputSchema)
	require.Error(t, err)

	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.NotNil(t, opErr.Details)
	violations, ok := opErr.Details["violations"].([]string)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(violations), 1)
}

// --- Numeric edge cases ---

func TestValidateInput_NumericBoundaries(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{
		"type": "object",
		"properties": {
			"score": {
				"type": "number",
				"minimum": 0,
				"maximum": 100,
				"exclusiveMinimum": 0
			}
		}
	}`)

	t.Run("valid number", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"score": 50.5}, inputSchema)
		assert.NoError(t, err)
	})

	t.Run("at max", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"score": 100}, inputSchema)
		assert.NoError(t, err)
	})

	t.Run("at exclusive min", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"score": 0}, inputSchema)
		require.Error(t, err) // exclusiveMinimum: 0 means > 0
	})

	t.Run("above max", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"score": 101}, inputSchema)
		require.Error(t, err)
	})
}

// --- Template definitions (opcode.define) ---

func TestValidateInput_TemplateDefinitionSchema(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	// A schema that might be used for opcode.define template validation.
	templateSchema := []byte(`{
		"type": "object",
		"required": ["template_name", "workflow"],
		"properties": {
			"template_name": {"type": "string", "minLength": 1},
			"description": {"type": "string"},
			"workflow": {
				"type": "object",
				"required": ["steps"],
				"properties": {
					"steps": {
						"type": "array",
						"minItems": 1
					}
				}
			}
		}
	}`)

	t.Run("valid template", func(t *testing.T) {
		input := map[string]any{
			"template_name": "order-processing",
			"description":   "Process customer orders",
			"workflow": map[string]any{
				"steps": []any{
					map[string]any{"id": "step-1"},
				},
			},
		}
		err := v.ValidateInput(input, templateSchema)
		assert.NoError(t, err)
	})

	t.Run("missing template_name", func(t *testing.T) {
		input := map[string]any{
			"workflow": map[string]any{
				"steps": []any{map[string]any{"id": "step-1"}},
			},
		}
		err := v.ValidateInput(input, templateSchema)
		require.Error(t, err)
	})
}

// --- Interface compliance ---

func TestJSONSchemaValidator_ImplementsValidator(t *testing.T) {
	var _ Validator = (*JSONSchemaValidator)(nil)
}

// --- OneOf / AnyOf composition ---

func TestValidateInput_OneOf(t *testing.T) {
	v, err := NewJSONSchemaValidator()
	require.NoError(t, err)

	inputSchema := []byte(`{
		"type": "object",
		"properties": {
			"value": {
				"oneOf": [
					{"type": "string"},
					{"type": "integer"}
				]
			}
		}
	}`)

	t.Run("string matches", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"value": "hello"}, inputSchema)
		assert.NoError(t, err)
	})

	t.Run("integer matches", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"value": 42}, inputSchema)
		assert.NoError(t, err)
	})

	t.Run("boolean fails", func(t *testing.T) {
		err := v.ValidateInput(map[string]any{"value": true}, inputSchema)
		require.Error(t, err)
	})
}
