package validation

import "github.com/rendis/opcode/pkg/schema"

// Validator checks workflow definitions for correctness before execution.
// Uses JSON Schema Draft 2020-12 for input/output validation.
type Validator interface {
	ValidateDefinition(def *schema.WorkflowDefinition) error
	ValidateInput(input map[string]any, inputSchema []byte) error
}
