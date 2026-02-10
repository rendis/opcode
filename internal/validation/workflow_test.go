package validation

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Interface compliance ---

func TestWorkflowValidator_ImplementsValidator(t *testing.T) {
	var _ Validator = (*WorkflowValidator)(nil)
}

// --- Full pipeline ---

func TestWorkflowValidator_FullValid(t *testing.T) {
	wv, err := NewWorkflowValidator(newMockLookup("http.get"))
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "http.get"},
			{ID: "s2", Action: "http.get", DependsOn: []string{"s1"}},
		},
	}
	result := wv.Validate(def)
	assert.True(t, result.Valid())
	assert.Empty(t, result.Errors)
	assert.Empty(t, result.Warnings)
}

func TestWorkflowValidator_NilDef(t *testing.T) {
	wv, err := NewWorkflowValidator(nil)
	require.NoError(t, err)

	result := wv.Validate(nil)
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Message, "nil")
}

func TestWorkflowValidator_NilActionLookup(t *testing.T) {
	wv, err := NewWorkflowValidator(nil)
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "nonexistent.action"},
		},
	}
	result := wv.Validate(def)
	assert.True(t, result.Valid(), "nil lookup skips action checks")
}

// --- Short-circuit ---

func TestWorkflowValidator_StructuralFailShortCircuits(t *testing.T) {
	wv, err := NewWorkflowValidator(newMockLookup())
	require.NoError(t, err)

	// Missing steps → structural error. Semantic/DAG never run.
	def := &schema.WorkflowDefinition{}
	result := wv.Validate(def)
	require.False(t, result.Valid())
	// Only structural errors, no semantic errors about action names.
	for _, e := range result.Errors {
		assert.NotEqual(t, schema.ErrCodeActionUnavailable, e.Code)
	}
}

func TestWorkflowValidator_SemanticErrorsSkipDAG(t *testing.T) {
	wv, err := NewWorkflowValidator(newMockLookup())
	require.NoError(t, err)

	// Action not registered → semantic error. DAG stage skipped.
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "bad.action", DependsOn: []string{"s2"}},
			{ID: "s2", Action: "bad.action", DependsOn: []string{"s1"}},
		},
	}
	result := wv.Validate(def)
	require.False(t, result.Valid())
	// Should have action errors but NOT cycle error (DAG skipped).
	for _, e := range result.Errors {
		assert.NotEqual(t, schema.ErrCodeCycleDetected, e.Code,
			"DAG stage should be skipped when semantic has errors")
	}
}

// --- DAG errors ---

func TestWorkflowValidator_CycleDetected(t *testing.T) {
	wv, err := NewWorkflowValidator(newMockLookup("a"))
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "a", DependsOn: []string{"s2"}},
			{ID: "s2", Action: "a", DependsOn: []string{"s1"}},
		},
	}
	result := wv.Validate(def)
	require.False(t, result.Valid())

	hasCycle := false
	for _, e := range result.Errors {
		if e.Code == schema.ErrCodeCycleDetected {
			hasCycle = true
		}
	}
	assert.True(t, hasCycle, "should detect cycle")
}

// --- Warnings pass through ---

func TestWorkflowValidator_WarningsPassThrough(t *testing.T) {
	wv, err := NewWorkflowValidator(newMockLookup("a"))
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "a", Retry: &schema.RetryPolicy{Max: 50}},
		},
	}
	result := wv.Validate(def)
	assert.True(t, result.Valid())
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0].Message, "50")
}

// --- ValidateDefinition (Validator interface) ---

func TestWorkflowValidator_ValidateDefinition_Valid(t *testing.T) {
	wv, err := NewWorkflowValidator(newMockLookup("a"))
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{{ID: "s1", Action: "a"}},
	}
	assert.NoError(t, wv.ValidateDefinition(def))
}

func TestWorkflowValidator_ValidateDefinition_Error(t *testing.T) {
	wv, err := NewWorkflowValidator(newMockLookup())
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{{ID: "s1", Action: "missing"}},
	}
	err = wv.ValidateDefinition(def)
	require.Error(t, err)
	opErr, ok := err.(*schema.OpcodeError)
	require.True(t, ok)
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

// --- ValidateInput ---

func TestWorkflowValidator_ValidateInput(t *testing.T) {
	wv, err := NewWorkflowValidator(nil)
	require.NoError(t, err)

	input := map[string]any{"name": "test"}
	inputSchema := []byte(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`)
	assert.NoError(t, wv.ValidateInput(input, inputSchema))
}

// --- Complex scenarios ---

func TestWorkflowValidator_LoopWithInvalidNestedAction(t *testing.T) {
	wv, err := NewWorkflowValidator(newMockLookup("a"))
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{
				ID:   "loop1",
				Type: schema.StepTypeLoop,
				Config: mustJSON(schema.LoopConfig{
					Body:    []schema.StepDefinition{{ID: "ls1", Action: "bad.action"}},
					MaxIter: 5,
				}),
			},
		},
	}
	result := wv.Validate(def)
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors[0].Message, "bad.action")
}

func TestWorkflowValidator_OnCompleteWithBadAction(t *testing.T) {
	wv, err := NewWorkflowValidator(newMockLookup("a"))
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps:      []schema.StepDefinition{{ID: "s1", Action: "a"}},
		OnComplete: &schema.StepDefinition{ID: "oc", Action: "bad.action"},
	}
	result := wv.Validate(def)
	require.False(t, result.Valid())
	assert.Equal(t, "on_complete.action", result.Errors[0].Path)
}

func TestWorkflowValidator_MixedErrorsAndWarnings(t *testing.T) {
	wv, err := NewWorkflowValidator(newMockLookup())
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "bad", Retry: &schema.RetryPolicy{Max: 20}},
		},
	}
	result := wv.Validate(def)
	assert.False(t, result.Valid())
	assert.NotEmpty(t, result.Errors)
	assert.NotEmpty(t, result.Warnings)
}

// --- Concurrent safety ---

func TestWorkflowValidator_Concurrent(t *testing.T) {
	wv, err := NewWorkflowValidator(newMockLookup("a"))
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "a"},
			{ID: "s2", Action: "a", DependsOn: []string{"s1"}},
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := wv.Validate(def)
			assert.True(t, result.Valid())
		}()
	}
	wg.Wait()
}

// --- Wait step type accepted ---

func TestWorkflowValidator_WaitStepType(t *testing.T) {
	wv, err := NewWorkflowValidator(nil)
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "w1", Type: schema.StepTypeWait, Config: json.RawMessage(`{"duration":"5s"}`)},
		},
	}
	result := wv.Validate(def)
	assert.True(t, result.Valid())
}

// --- All step types pass structural ---

func TestWorkflowValidator_AllStepTypes(t *testing.T) {
	wv, err := NewWorkflowValidator(newMockLookup("a"))
	require.NoError(t, err)

	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Type: schema.StepTypeAction, Action: "a"},
			{ID: "s2", Type: schema.StepTypeCondition, Config: mustJSON(schema.ConditionConfig{
				Expression: "true",
				Branches:   map[string][]schema.StepDefinition{"yes": {{ID: "cs1", Action: "a"}}},
			})},
			{ID: "s3", Type: schema.StepTypeLoop, Config: mustJSON(schema.LoopConfig{
				Body: []schema.StepDefinition{{ID: "ls1", Action: "a"}}, MaxIter: 3,
			})},
			{ID: "s4", Type: schema.StepTypeParallel, Config: mustJSON(schema.ParallelConfig{
				Branches: [][]schema.StepDefinition{{{ID: "ps1", Action: "a"}}},
			})},
			{ID: "s5", Type: schema.StepTypeReasoning, Config: mustJSON(schema.ReasoningConfig{
				PromptContext: "choose", Options: []schema.ReasoningOption{{ID: "o1"}},
			})},
			{ID: "s6", Type: schema.StepTypeWait, Config: mustJSON(schema.WaitConfig{Duration: "5s"})},
		},
	}
	result := wv.Validate(def)
	assert.True(t, result.Valid(), "all step types should pass validation: %+v", result.Errors)
}
