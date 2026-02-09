package validation

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/rendis/opcode/pkg/schema"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// workflowSchemaJSON is the JSON Schema for WorkflowDefinition validation.
// Embedded as a constant to avoid filesystem dependencies.
const workflowSchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://opcode.dev/schemas/workflow.json",
  "type": "object",
  "required": ["steps"],
  "properties": {
    "steps": {
      "type": "array",
      "minItems": 1,
      "items": { "$ref": "#/$defs/step" }
    },
    "inputs": {
      "type": "object"
    },
    "on_complete": { "$ref": "#/$defs/step" },
    "on_error": { "$ref": "#/$defs/step" },
    "timeout": {
      "type": "string",
      "pattern": "^[0-9]+(ns|us|µs|ms|s|m|h)$"
    },
    "on_timeout": {
      "type": "string",
      "enum": ["fail", "suspend", "cancel"]
    },
    "metadata": {
      "type": "object"
    }
  },
  "additionalProperties": false,
  "$defs": {
    "step": {
      "type": "object",
      "required": ["id"],
      "properties": {
        "id": {
          "type": "string",
          "minLength": 1
        },
        "type": {
          "type": "string",
          "enum": ["action", "condition", "reasoning", "parallel", "loop"]
        },
        "action": { "type": "string" },
        "params": {},
        "depends_on": {
          "type": "array",
          "items": { "type": "string" }
        },
        "condition": { "type": "string" },
        "retry": { "$ref": "#/$defs/retry" },
        "timeout": {
          "type": "string",
          "pattern": "^[0-9]+(ns|us|µs|ms|s|m|h)$"
        },
        "on_error": { "$ref": "#/$defs/error_handler" },
        "config": {}
      },
      "additionalProperties": false
    },
    "retry": {
      "type": "object",
      "required": ["max"],
      "properties": {
        "max": {
          "type": "integer",
          "minimum": 0
        },
        "backoff": {
          "type": "string",
          "enum": ["none", "linear", "exponential", "constant"]
        },
        "delay": {
          "type": "string",
          "pattern": "^[0-9]+(ns|us|µs|ms|s|m|h)$"
        },
        "max_delay": {
          "type": "string",
          "pattern": "^[0-9]+(ns|us|µs|ms|s|m|h)$"
        }
      },
      "additionalProperties": false
    },
    "error_handler": {
      "type": "object",
      "required": ["strategy"],
      "properties": {
        "strategy": {
          "type": "string",
          "enum": ["ignore", "fail_workflow", "fallback_step", "retry"]
        },
        "fallback_step": {
          "type": "string"
        }
      },
      "additionalProperties": false
    }
  }
}`

// JSONSchemaValidator implements the Validator interface using JSON Schema Draft 2020-12.
// It is safe for concurrent use.
type JSONSchemaValidator struct {
	workflowSchema *jsonschema.Schema

	// mu guards the cache and compiler for dynamic schema compilation.
	mu       sync.RWMutex
	compiler *jsonschema.Compiler
	cache    map[string]*jsonschema.Schema
}

// NewJSONSchemaValidator creates a new JSONSchemaValidator with the workflow schema pre-compiled.
func NewJSONSchemaValidator() (*JSONSchemaValidator, error) {
	c := jsonschema.NewCompiler()
	c.AssertFormat()

	schemaDoc, err := jsonschema.UnmarshalJSON(strings.NewReader(workflowSchemaJSON))
	if err != nil {
		return nil, fmt.Errorf("unmarshal workflow schema: %w", err)
	}
	if err := c.AddResource("https://opcode.dev/schemas/workflow.json", schemaDoc); err != nil {
		return nil, fmt.Errorf("add workflow schema resource: %w", err)
	}

	wfSchema, err := c.Compile("https://opcode.dev/schemas/workflow.json")
	if err != nil {
		return nil, fmt.Errorf("compile workflow schema: %w", err)
	}

	return &JSONSchemaValidator{
		workflowSchema: wfSchema,
		compiler:       newInputCompiler(),
		cache:          make(map[string]*jsonschema.Schema),
	}, nil
}

// ValidateDefinition validates a WorkflowDefinition against the workflow JSON Schema.
func (v *JSONSchemaValidator) ValidateDefinition(def *schema.WorkflowDefinition) error {
	if def == nil {
		return schema.NewError(schema.ErrCodeValidation, "workflow definition is nil")
	}

	doc, err := toJSONValue(def)
	if err != nil {
		return schema.NewError(schema.ErrCodeValidation, "failed to serialize workflow definition").WithCause(err)
	}

	if err := v.workflowSchema.Validate(doc); err != nil {
		return toOpcodeError(err)
	}

	// Structural checks that JSON Schema cannot express: duplicate step IDs.
	seen := make(map[string]struct{}, len(def.Steps))
	for _, step := range def.Steps {
		if _, exists := seen[step.ID]; exists {
			return schema.NewError(schema.ErrCodeValidation,
				fmt.Sprintf("duplicate step id %q", step.ID))
		}
		seen[step.ID] = struct{}{}
	}

	return nil
}

// ValidateInput validates input data against a JSON Schema provided as raw bytes.
// The schema is compiled and cached for subsequent calls with the same schema.
func (v *JSONSchemaValidator) ValidateInput(input map[string]any, inputSchema []byte) error {
	if input == nil {
		return schema.NewError(schema.ErrCodeValidation, "input is nil")
	}
	if len(inputSchema) == 0 {
		return nil // no schema means no validation needed
	}

	compiled, err := v.getOrCompile(inputSchema)
	if err != nil {
		return schema.NewError(schema.ErrCodeValidation, "invalid input schema").WithCause(err)
	}

	// Convert input to JSON-compatible value (json.Number for numbers).
	doc, err := toJSONValue(input)
	if err != nil {
		return schema.NewError(schema.ErrCodeValidation, "failed to serialize input").WithCause(err)
	}

	if err := compiled.Validate(doc); err != nil {
		return toOpcodeError(err)
	}

	return nil
}

// getOrCompile returns a cached compiled schema or compiles and caches a new one.
func (v *JSONSchemaValidator) getOrCompile(schemaBytes []byte) (*jsonschema.Schema, error) {
	key := string(schemaBytes)

	v.mu.RLock()
	if cached, ok := v.cache[key]; ok {
		v.mu.RUnlock()
		return cached, nil
	}
	v.mu.RUnlock()

	v.mu.Lock()
	defer v.mu.Unlock()

	// Double-check after acquiring write lock.
	if cached, ok := v.cache[key]; ok {
		return cached, nil
	}

	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(key))
	if err != nil {
		return nil, fmt.Errorf("unmarshal schema: %w", err)
	}

	// Each dynamic schema gets a unique URL to avoid collisions in the compiler.
	url := fmt.Sprintf("opcode://input-schema/%d", len(v.cache))

	// Use a fresh compiler per dynamic schema to avoid resource collision.
	c := newInputCompiler()
	if err := c.AddResource(url, doc); err != nil {
		return nil, fmt.Errorf("add schema resource: %w", err)
	}

	compiled, err := c.Compile(url)
	if err != nil {
		return nil, fmt.Errorf("compile schema: %w", err)
	}

	v.cache[key] = compiled
	return compiled, nil
}

// newInputCompiler creates a Compiler configured for input/output validation.
func newInputCompiler() *jsonschema.Compiler {
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	return c
}

// toJSONValue round-trips a Go value through JSON encoding/decoding so that
// numeric values become json.Number (required by the jsonschema library).
func toJSONValue(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(strings.NewReader(string(b)))
}

// toOpcodeError converts a jsonschema.ValidationError into an OpcodeError
// with clear, actionable messages for agent consumption.
func toOpcodeError(err error) *schema.OpcodeError {
	verr, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return schema.NewError(schema.ErrCodeValidation, err.Error())
	}

	violations := collectViolations(verr)
	if len(violations) == 0 {
		return schema.NewError(schema.ErrCodeValidation, verr.Error())
	}

	if len(violations) == 1 {
		return schema.NewError(schema.ErrCodeValidation, violations[0]).
			WithDetails(map[string]any{"violations": violations})
	}

	msg := fmt.Sprintf("validation failed with %d errors", len(violations))
	return schema.NewError(schema.ErrCodeValidation, msg).
		WithDetails(map[string]any{"violations": violations})
}

// collectViolations walks a ValidationError tree and collects leaf error messages
// with their instance locations for agent-friendly error reporting.
func collectViolations(verr *jsonschema.ValidationError) []string {
	if len(verr.Causes) == 0 {
		loc := "/"
		if len(verr.InstanceLocation) > 0 {
			loc = "/" + strings.Join(verr.InstanceLocation, "/")
		}
		return []string{fmt.Sprintf("%s: %s", loc, verr.Error())}
	}

	var violations []string
	for _, cause := range verr.Causes {
		violations = append(violations, collectViolations(cause)...)
	}
	return violations
}
