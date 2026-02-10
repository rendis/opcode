package reasoning

import (
	"encoding/json"
	"testing"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDecisionContext_StepOutputs(t *testing.T) {
	p := ContextParams{
		Config: schema.ReasoningConfig{
			PromptContext: "Choose next action",
			Options:       []schema.ReasoningOption{{ID: "a"}, {ID: "b"}},
		},
		StepOutputs: map[string]any{
			"s1": map[string]any{"status": "ok"},
			"s2": map[string]any{"count": 42},
		},
		WorkflowIntent: "Deploy the application",
		AgentNotes:     "Consider rollback risk",
	}

	raw := BuildDecisionContext(p)

	var dc DecisionContext
	require.NoError(t, json.Unmarshal(raw, &dc))
	assert.Equal(t, "Choose next action", dc.PromptContext)
	assert.Equal(t, "Deploy the application", dc.WorkflowIntent)
	assert.Equal(t, "Consider rollback risk", dc.AgentNotes)
	assert.Len(t, dc.StepOutputs, 2)
	s1 := dc.StepOutputs["s1"].(map[string]any)
	assert.Equal(t, "ok", s1["status"])
}

func TestBuildDecisionContext_DataInject(t *testing.T) {
	p := ContextParams{
		Config: schema.ReasoningConfig{
			PromptContext: "Review data",
		},
		ResolvedInjects: map[string]any{
			"user_count":  100,
			"environment": "production",
		},
		AccumulatedData: map[string]any{
			"session": "abc123",
		},
	}

	raw := BuildDecisionContext(p)

	var dc DecisionContext
	require.NoError(t, json.Unmarshal(raw, &dc))
	assert.Equal(t, float64(100), dc.DataInject["user_count"])
	assert.Equal(t, "production", dc.DataInject["environment"])
	assert.Equal(t, "abc123", dc.AccumulatedData["session"])
}

func TestBuildDecisionContext_EmptyOptionalFields(t *testing.T) {
	p := ContextParams{
		Config: schema.ReasoningConfig{
			PromptContext: "Minimal context",
		},
	}

	raw := BuildDecisionContext(p)

	var dc DecisionContext
	require.NoError(t, json.Unmarshal(raw, &dc))
	assert.Equal(t, "Minimal context", dc.PromptContext)
	assert.Empty(t, dc.StepOutputs)
	assert.Empty(t, dc.WorkflowIntent)
	assert.Empty(t, dc.AgentNotes)
	assert.Empty(t, dc.DataInject)
	assert.Empty(t, dc.AccumulatedData)
}
