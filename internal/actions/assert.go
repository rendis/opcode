package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/rendis/opcode/internal/validation"
	"github.com/rendis/opcode/pkg/schema"
)

// AssertActions returns all assertion-related actions.
func AssertActions(validator *validation.JSONSchemaValidator) []Action {
	return []Action{
		&assertEqualsAction{},
		&assertContainsAction{},
		&assertMatchesAction{},
		&assertSchemaAction{validator: validator},
	}
}

// normalizeJSON converts Go numeric types to float64 for consistent deep-equal comparison.
// JSON unmarshaling produces float64 for numbers; this normalizes int, int64, json.Number
// so reflect.DeepEqual works across boundaries.
func normalizeJSON(v any) any {
	switch val := v.(type) {
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case int32:
		return float64(val)
	case json.Number:
		if f, err := val.Float64(); err == nil {
			return f
		}
		return v
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = normalizeJSON(item)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = normalizeJSON(item)
		}
		return out
	default:
		return v
	}
}

var passResult = func() json.RawMessage {
	b, _ := json.Marshal(map[string]any{"pass": true})
	return b
}()

// --- assert.equals ---

type assertEqualsAction struct{}

func (a *assertEqualsAction) Name() string { return "assert.equals" }

func (a *assertEqualsAction) Schema() ActionSchema {
	return ActionSchema{Description: "Assert that two values are deeply equal"}
}

func (a *assertEqualsAction) Validate(input map[string]any) error {
	if _, ok := input["expected"]; !ok {
		return schema.NewError(schema.ErrCodeValidation, "assert.equals requires 'expected' parameter")
	}
	if _, ok := input["actual"]; !ok {
		return schema.NewError(schema.ErrCodeValidation, "assert.equals requires 'actual' parameter")
	}
	return nil
}

func (a *assertEqualsAction) Execute(_ context.Context, input ActionInput) (*ActionOutput, error) {
	expected := normalizeJSON(input.Params["expected"])
	actual := normalizeJSON(input.Params["actual"])

	if reflect.DeepEqual(expected, actual) {
		return &ActionOutput{Data: passResult}, nil
	}

	msg := "assertion failed: values are not equal"
	if m, ok := input.Params["message"].(string); ok && m != "" {
		msg = m
	}

	return nil, schema.NewError(schema.ErrCodeAssertionFailed, msg).
		WithDetails(map[string]any{"expected": input.Params["expected"], "actual": input.Params["actual"]})
}

// --- assert.contains ---

type assertContainsAction struct{}

func (a *assertContainsAction) Name() string { return "assert.contains" }

func (a *assertContainsAction) Schema() ActionSchema {
	return ActionSchema{Description: "Assert that a string or array contains a value"}
}

func (a *assertContainsAction) Validate(input map[string]any) error {
	if _, ok := input["haystack"]; !ok {
		return schema.NewError(schema.ErrCodeValidation, "assert.contains requires 'haystack' parameter")
	}
	if _, ok := input["needle"]; !ok {
		return schema.NewError(schema.ErrCodeValidation, "assert.contains requires 'needle' parameter")
	}
	return nil
}

func (a *assertContainsAction) Execute(_ context.Context, input ActionInput) (*ActionOutput, error) {
	haystack := input.Params["haystack"]
	needle := input.Params["needle"]

	msg := "assertion failed: value not found"
	if m, ok := input.Params["message"].(string); ok && m != "" {
		msg = m
	}

	switch hs := haystack.(type) {
	case string:
		if strings.Contains(hs, fmt.Sprintf("%v", needle)) {
			return &ActionOutput{Data: passResult}, nil
		}
		return nil, schema.NewError(schema.ErrCodeAssertionFailed, msg).
			WithDetails(map[string]any{"haystack": haystack, "needle": needle})
	case []any:
		normalizedNeedle := normalizeJSON(needle)
		for _, item := range hs {
			if reflect.DeepEqual(normalizeJSON(item), normalizedNeedle) {
				return &ActionOutput{Data: passResult}, nil
			}
		}
		return nil, schema.NewError(schema.ErrCodeAssertionFailed, msg).
			WithDetails(map[string]any{"haystack": haystack, "needle": needle})
	default:
		return nil, schema.NewErrorf(schema.ErrCodeValidation,
			"assert.contains: haystack must be string or array, got %T", haystack)
	}
}

// --- assert.matches ---

type assertMatchesAction struct{}

func (a *assertMatchesAction) Name() string { return "assert.matches" }

func (a *assertMatchesAction) Schema() ActionSchema {
	return ActionSchema{Description: "Assert that a string matches a regular expression"}
}

func (a *assertMatchesAction) Validate(input map[string]any) error {
	if _, ok := input["value"].(string); !ok {
		return schema.NewError(schema.ErrCodeValidation, "assert.matches requires 'value' string parameter")
	}
	if _, ok := input["pattern"].(string); !ok {
		return schema.NewError(schema.ErrCodeValidation, "assert.matches requires 'pattern' string parameter")
	}
	return nil
}

func (a *assertMatchesAction) Execute(_ context.Context, input ActionInput) (*ActionOutput, error) {
	value, _ := input.Params["value"].(string)
	pattern, _ := input.Params["pattern"].(string)

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeValidation, "invalid regex pattern: %s", err)
	}

	if !re.MatchString(value) {
		msg := "assertion failed: value does not match pattern"
		if m, ok := input.Params["message"].(string); ok && m != "" {
			msg = m
		}
		return nil, schema.NewError(schema.ErrCodeAssertionFailed, msg).
			WithDetails(map[string]any{"value": value, "pattern": pattern})
	}

	match := re.FindString(value)
	out, _ := json.Marshal(map[string]any{"pass": true, "matches": match})
	return &ActionOutput{Data: out}, nil
}

// --- assert.schema ---

type assertSchemaAction struct {
	validator *validation.JSONSchemaValidator
}

func (a *assertSchemaAction) Name() string { return "assert.schema" }

func (a *assertSchemaAction) Schema() ActionSchema {
	return ActionSchema{Description: "Assert that data conforms to a JSON Schema"}
}

func (a *assertSchemaAction) Validate(input map[string]any) error {
	if _, ok := input["data"]; !ok {
		return schema.NewError(schema.ErrCodeValidation, "assert.schema requires 'data' parameter")
	}
	if _, ok := input["schema"]; !ok {
		return schema.NewError(schema.ErrCodeValidation, "assert.schema requires 'schema' parameter")
	}
	return nil
}

func (a *assertSchemaAction) Execute(_ context.Context, input ActionInput) (*ActionOutput, error) {
	data := input.Params["data"]
	schemaObj := input.Params["schema"]

	schemaBytes, err := json.Marshal(schemaObj)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeValidation, "failed to serialize schema: %s", err)
	}

	// Convert data to map[string]any if it isn't already.
	dataMap, ok := data.(map[string]any)
	if !ok {
		return nil, schema.NewError(schema.ErrCodeValidation, "assert.schema: data must be an object")
	}

	if err := a.validator.ValidateInput(dataMap, schemaBytes); err != nil {
		msg := "assertion failed: data does not match schema"
		if m, ok := input.Params["message"].(string); ok && m != "" {
			msg = m
		}
		// Extract violations from the validation error if available.
		details := map[string]any{"error": err.Error()}
		var opErr *schema.OpcodeError
		if errors.As(err, &opErr) && opErr.Details != nil {
			details["violations"] = opErr.Details["violations"]
		}
		return nil, schema.NewError(schema.ErrCodeAssertionFailed, msg).WithDetails(details)
	}

	return &ActionOutput{Data: passResult}, nil
}
