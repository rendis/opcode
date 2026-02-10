package validation

import "github.com/rendis/opcode/pkg/schema"

// WorkflowValidator orchestrates the three-stage validation pipeline:
// 1. Structural (JSON Schema)
// 2. Semantic (action refs, step refs, nested sub-steps)
// 3. DAG (cycles, reachability)
type WorkflowValidator struct {
	jsonSchema *JSONSchemaValidator
	actions    ActionLookup
}

// NewWorkflowValidator creates a WorkflowValidator.
// lookup may be nil to skip action existence checks.
func NewWorkflowValidator(lookup ActionLookup) (*WorkflowValidator, error) {
	jsv, err := NewJSONSchemaValidator()
	if err != nil {
		return nil, err
	}
	return &WorkflowValidator{
		jsonSchema: jsv,
		actions:    lookup,
	}, nil
}

// Validate runs the full 3-stage pipeline and returns an aggregated result.
// Structural errors short-circuit: semantic and DAG stages are skipped.
func (wv *WorkflowValidator) Validate(def *schema.WorkflowDefinition) *schema.ValidationResult {
	if def == nil {
		r := &schema.ValidationResult{}
		r.AddError("/", schema.ErrCodeValidation, "workflow definition is nil")
		return r
	}

	// Stage 1: Structural (JSON Schema).
	result := validateStructural(wv.jsonSchema, def)
	if !result.Valid() {
		return result
	}

	// Stage 2: Semantic.
	result.Merge(validateSemantic(def, wv.actions))

	// Stage 3: DAG (skip if semantic errors — graph may be invalid).
	if result.Valid() {
		result.Merge(validateDAG(def))
	}

	return result
}

// ValidateDefinition satisfies the Validator interface.
func (wv *WorkflowValidator) ValidateDefinition(def *schema.WorkflowDefinition) error {
	return wv.Validate(def).ToError()
}

// ValidateInput delegates to the underlying JSONSchemaValidator.
func (wv *WorkflowValidator) ValidateInput(input map[string]any, inputSchema []byte) error {
	return wv.jsonSchema.ValidateInput(input, inputSchema)
}

// validateStructural wraps JSONSchemaValidator.ValidateDefinition, converting
// its error output into ValidationResult.
func validateStructural(v *JSONSchemaValidator, def *schema.WorkflowDefinition) *schema.ValidationResult {
	result := &schema.ValidationResult{}

	err := v.ValidateDefinition(def)
	if err == nil {
		return result
	}

	opErr, ok := err.(*schema.OpcodeError)
	if !ok {
		result.AddError("/", schema.ErrCodeValidation, err.Error())
		return result
	}

	if opErr.Details != nil {
		if violations, ok := opErr.Details["violations"].([]string); ok {
			for _, v := range violations {
				result.AddError("/", schema.ErrCodeValidation, v)
			}
			return result
		}
	}
	result.AddError("/", schema.ErrCodeValidation, opErr.Message)
	return result
}
