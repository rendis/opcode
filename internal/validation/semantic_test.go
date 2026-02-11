package validation

import (
	"encoding/json"
	"testing"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockActionLookup implements ActionLookup for tests.
type mockActionLookup struct {
	registered map[string]bool
}

func (m *mockActionLookup) Has(name string) bool {
	return m.registered[name]
}

func newMockLookup(names ...string) *mockActionLookup {
	m := &mockActionLookup{registered: make(map[string]bool)}
	for _, n := range names {
		m.registered[n] = true
	}
	return m
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// --- Action existence ---

func TestSemantic_ActionExists(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "http.get"},
		},
	}
	result := validateSemantic(def, newMockLookup("http.get"))
	assert.True(t, result.Valid())
}

func TestSemantic_ActionNotRegistered(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "http.get"},
		},
	}
	result := validateSemantic(def, newMockLookup("http.post"))
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "steps[0].action", result.Errors[0].Path)
	assert.Equal(t, schema.ErrCodeActionUnavailable, result.Errors[0].Code)
	assert.Contains(t, result.Errors[0].Message, "http.get")
}

func TestSemantic_NilLookupSkipsActionCheck(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "nonexistent.action"},
		},
	}
	result := validateSemantic(def, nil)
	assert.True(t, result.Valid())
}

func TestSemantic_NonActionStepIgnoresLookup(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Type: schema.StepTypeWait, Config: mustJSON(schema.WaitConfig{Duration: "5s"})},
		},
	}
	result := validateSemantic(def, newMockLookup())
	assert.True(t, result.Valid())
}

func TestSemantic_DefaultTypeIsAction(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "missing.action"}, // Type="" defaults to action
		},
	}
	result := validateSemantic(def, newMockLookup())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, schema.ErrCodeActionUnavailable, result.Errors[0].Code)
}

// --- depends_on references ---

func TestSemantic_ValidDependsOn(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "a"},
			{ID: "s2", Action: "a", DependsOn: []string{"s1"}},
		},
	}
	result := validateSemantic(def, newMockLookup("a"))
	assert.True(t, result.Valid())
}

func TestSemantic_InvalidDependsOn(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "a"},
			{ID: "s2", Action: "a", DependsOn: []string{"nonexistent"}},
		},
	}
	result := validateSemantic(def, newMockLookup("a"))
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "steps[1].depends_on[0]", result.Errors[0].Path)
	assert.Contains(t, result.Errors[0].Message, "nonexistent")
}

// --- fallback_step references ---

func TestSemantic_FallbackStepExists(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "a", OnError: &schema.ErrorHandler{
				Strategy: schema.ErrorStrategyFallbackStep, FallbackStep: "s2",
			}},
			{ID: "s2", Action: "a"},
		},
	}
	result := validateSemantic(def, newMockLookup("a"))
	assert.True(t, result.Valid())
}

func TestSemantic_FallbackStepMissing(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "a", OnError: &schema.ErrorHandler{
				Strategy: schema.ErrorStrategyFallbackStep, FallbackStep: "nonexistent",
			}},
		},
	}
	result := validateSemantic(def, newMockLookup("a"))
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "steps[0].on_error.fallback_step", result.Errors[0].Path)
	assert.Contains(t, result.Errors[0].Message, "nonexistent")
}

func TestSemantic_FallbackStepEmpty(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "a", OnError: &schema.ErrorHandler{
				Strategy: schema.ErrorStrategyFallbackStep,
			}},
		},
	}
	result := validateSemantic(def, newMockLookup("a"))
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Message, "requires a fallback_step ID")
}

func TestSemantic_NonFallbackStrategyIgnored(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "a", OnError: &schema.ErrorHandler{
				Strategy: schema.ErrorStrategyIgnore,
			}},
		},
	}
	result := validateSemantic(def, newMockLookup("a"))
	assert.True(t, result.Valid())
}

// --- Handler validation ---

func TestSemantic_OnCompleteValid(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps:      []schema.StepDefinition{{ID: "s1", Action: "a"}},
		OnComplete: &schema.StepDefinition{ID: "oc", Action: "a"},
	}
	result := validateSemantic(def, newMockLookup("a"))
	assert.True(t, result.Valid())
}

func TestSemantic_OnCompleteActionMissing(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps:      []schema.StepDefinition{{ID: "s1", Action: "a"}},
		OnComplete: &schema.StepDefinition{ID: "oc", Action: "bad.action"},
	}
	result := validateSemantic(def, newMockLookup("a"))
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "on_complete.action", result.Errors[0].Path)
}

func TestSemantic_OnErrorHandlerValid(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps:   []schema.StepDefinition{{ID: "s1", Action: "a"}},
		OnError: &schema.StepDefinition{ID: "oe", Action: "a"},
	}
	result := validateSemantic(def, newMockLookup("a"))
	assert.True(t, result.Valid())
}

func TestSemantic_HandlerWithDependsOn(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps:      []schema.StepDefinition{{ID: "s1", Action: "a"}},
		OnComplete: &schema.StepDefinition{ID: "oc", Action: "a", DependsOn: []string{"s1"}},
	}
	result := validateSemantic(def, newMockLookup("a"))
	assert.True(t, result.Valid(), "warnings should not invalidate result")
	require.Len(t, result.Warnings, 1)
	assert.Equal(t, "on_complete.depends_on", result.Warnings[0].Path)
}

// --- Recursive sub-step validation ---

func TestSemantic_ParallelBranchActionCheck(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{
				ID:   "p1",
				Type: schema.StepTypeParallel,
				Config: mustJSON(schema.ParallelConfig{
					Branches: [][]schema.StepDefinition{
						{{ID: "b1s1", Action: "bad.action"}},
					},
				}),
			},
		},
	}
	result := validateSemantic(def, newMockLookup("a"))
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Path, "branches[0][0].action")
	assert.Contains(t, result.Errors[0].Message, "bad.action")
}

func TestSemantic_LoopBodyActionCheck(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{
				ID:   "l1",
				Type: schema.StepTypeLoop,
				Config: mustJSON(schema.LoopConfig{
					Body: []schema.StepDefinition{
						{ID: "ls1", Action: "bad.action"},
					},
					MaxIter: 5,
				}),
			},
		},
	}
	result := validateSemantic(def, newMockLookup("a"))
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Path, "body[0].action")
}

func TestSemantic_ConditionBranchActionCheck(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{
				ID:   "c1",
				Type: schema.StepTypeCondition,
				Config: mustJSON(schema.ConditionConfig{
					Expression: "true",
					Branches: map[string][]schema.StepDefinition{
						"yes": {{ID: "cs1", Action: "bad.action"}},
					},
				}),
			},
		},
	}
	result := validateSemantic(def, newMockLookup("a"))
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Path, "branches[yes][0].action")
}

func TestSemantic_ConditionDefaultActionCheck(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{
				ID:   "c1",
				Type: schema.StepTypeCondition,
				Config: mustJSON(schema.ConditionConfig{
					Expression: "true",
					Branches:   map[string][]schema.StepDefinition{},
					Default:    []schema.StepDefinition{{ID: "ds1", Action: "bad.action"}},
				}),
			},
		},
	}
	result := validateSemantic(def, newMockLookup("a"))
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Path, "default[0].action")
}

func TestSemantic_NestedParallelInLoop(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{
				ID:   "l1",
				Type: schema.StepTypeLoop,
				Config: mustJSON(schema.LoopConfig{
					Body: []schema.StepDefinition{
						{
							ID:   "p1",
							Type: schema.StepTypeParallel,
							Config: mustJSON(schema.ParallelConfig{
								Branches: [][]schema.StepDefinition{
									{{ID: "deep", Action: "missing.deep"}},
								},
							}),
						},
					},
					MaxIter: 3,
				}),
			},
		},
	}
	result := validateSemantic(def, newMockLookup("a"))
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Message, "missing.deep")
}

func TestSemantic_ValidParallelSubSteps(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{
				ID:   "p1",
				Type: schema.StepTypeParallel,
				Config: mustJSON(schema.ParallelConfig{
					Branches: [][]schema.StepDefinition{
						{{ID: "b1", Action: "a"}, {ID: "b2", Action: "a"}},
					},
				}),
			},
		},
	}
	result := validateSemantic(def, newMockLookup("a"))
	assert.True(t, result.Valid())
}

// --- Warnings ---

func TestSemantic_HighRetryCountWarning(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "a", Retry: &schema.RetryPolicy{Max: 20}},
		},
	}
	result := validateSemantic(def, newMockLookup("a"))
	assert.True(t, result.Valid(), "warning should not invalidate")
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0].Message, "20")
}

func TestSemantic_NormalRetryNoWarning(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "a", Retry: &schema.RetryPolicy{Max: 3}},
		},
	}
	result := validateSemantic(def, newMockLookup("a"))
	assert.True(t, result.Valid())
	assert.Empty(t, result.Warnings)
}

// --- Multiple errors ---

func TestSemantic_MultipleErrors(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "s1", Action: "bad1"},
			{ID: "s2", Action: "bad2", DependsOn: []string{"nonexistent"}},
		},
	}
	result := validateSemantic(def, newMockLookup())
	assert.Len(t, result.Errors, 3) // 2 bad actions + 1 bad depends_on
}

// --- Reasoning timeout warnings ---

func TestSemantic_ReasoningTimeoutExceedsStepTimeout(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{
				ID:      "r1",
				Type:    schema.StepTypeReasoning,
				Timeout: "10s",
				Config: mustJSON(schema.ReasoningConfig{
					PromptContext: "decide",
					Timeout:       "1h",
				}),
			},
		},
	}
	result := validateSemantic(def, nil)
	assert.True(t, result.Valid(), "warning should not invalidate")
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0].Message, "reasoning timeout (1h) exceeds step timeout (10s)")
	assert.Equal(t, "steps[0].config.timeout", result.Warnings[0].Path)
}

func TestSemantic_ReasoningTimeoutExceedsWorkflowTimeout(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Timeout: "30s",
		Steps: []schema.StepDefinition{
			{
				ID:   "r1",
				Type: schema.StepTypeReasoning,
				Config: mustJSON(schema.ReasoningConfig{
					PromptContext: "decide",
					Timeout:       "2h",
				}),
			},
		},
	}
	result := validateSemantic(def, nil)
	assert.True(t, result.Valid())
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0].Message, "reasoning timeout (2h) exceeds workflow timeout (30s)")
}

func TestSemantic_ReasoningTimeoutExceedsBothTimeouts(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Timeout: "1m",
		Steps: []schema.StepDefinition{
			{
				ID:      "r1",
				Type:    schema.StepTypeReasoning,
				Timeout: "30s",
				Config: mustJSON(schema.ReasoningConfig{
					PromptContext: "decide",
					Timeout:       "1h",
				}),
			},
		},
	}
	result := validateSemantic(def, nil)
	assert.True(t, result.Valid())
	assert.Len(t, result.Warnings, 2, "should warn for both step and workflow timeout")
}

func TestSemantic_ReasoningTimeoutWithinLimits(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Timeout: "2h",
		Steps: []schema.StepDefinition{
			{
				ID:      "r1",
				Type:    schema.StepTypeReasoning,
				Timeout: "2h",
				Config: mustJSON(schema.ReasoningConfig{
					PromptContext: "decide",
					Timeout:       "1h",
				}),
			},
		},
	}
	result := validateSemantic(def, nil)
	assert.True(t, result.Valid())
	assert.Empty(t, result.Warnings, "no warning when reasoning timeout is within limits")
}

func TestSemantic_ReasoningNoTimeoutNoWarning(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Timeout: "30s",
		Steps: []schema.StepDefinition{
			{
				ID:   "r1",
				Type: schema.StepTypeReasoning,
				Config: mustJSON(schema.ReasoningConfig{
					PromptContext: "decide",
				}),
			},
		},
	}
	result := validateSemantic(def, nil)
	assert.True(t, result.Valid())
	assert.Empty(t, result.Warnings)
}
