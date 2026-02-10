package reasoning

import (
	"testing"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateResolution_ValidChoice(t *testing.T) {
	opts := []schema.ReasoningOption{
		{ID: "opt_a", Description: "Option A"},
		{ID: "opt_b", Description: "Option B"},
	}
	assert.NoError(t, ValidateResolution(opts, "opt_a"))
	assert.NoError(t, ValidateResolution(opts, "opt_b"))
}

func TestValidateResolution_InvalidChoice(t *testing.T) {
	opts := []schema.ReasoningOption{
		{ID: "opt_a", Description: "Option A"},
		{ID: "opt_b", Description: "Option B"},
	}
	err := ValidateResolution(opts, "opt_c")
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
	assert.Contains(t, opErr.Error(), "opt_c")
}

func TestValidateResolution_EmptyOptions_FreeForm(t *testing.T) {
	// Empty options = free-form, any choice accepted.
	assert.NoError(t, ValidateResolution(nil, "any"))
	assert.NoError(t, ValidateResolution([]schema.ReasoningOption{}, "arbitrary_choice"))
}
