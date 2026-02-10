package reasoning

import (
	"github.com/rendis/opcode/pkg/schema"
)

// ValidateResolution checks that the chosen option ID exists in the available options.
// If options is empty, any choice is accepted (free-form reasoning).
// Returns nil if valid, or an OpcodeError if the choice is invalid.
func ValidateResolution(options []schema.ReasoningOption, choice string) error {
	if len(options) == 0 {
		return nil // free-form: any choice accepted
	}
	for _, opt := range options {
		if opt.ID == choice {
			return nil
		}
	}
	return schema.NewErrorf(schema.ErrCodeValidation, "invalid choice %q: not in available options", choice)
}
