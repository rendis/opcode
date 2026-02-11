package diagram

import (
	"testing"

	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderMermaidLinear(t *testing.T) {
	model, err := Build(linearWorkflow(), nil)
	require.NoError(t, err)

	output := RenderMermaid(model)

	// Must start with graph TD.
	assert.Contains(t, output, "graph TD")

	// Verify node shapes: action nodes use square brackets.
	assert.Contains(t, output, "fetch[")
	assert.Contains(t, output, "transform[")
	assert.Contains(t, output, "store[")

	// Start/end use double parens (circle).
	assert.Contains(t, output, "__start__((")
	assert.Contains(t, output, "__end__((")

	// Edges present.
	assert.Contains(t, output, "-->")

	// Class definitions.
	assert.Contains(t, output, "classDef completed")
	assert.Contains(t, output, "classDef failed")
	assert.Contains(t, output, "classDef running")
}

func TestRenderMermaidCondition(t *testing.T) {
	model, err := Build(conditionWorkflow(), nil)
	require.NoError(t, err)

	output := RenderMermaid(model)
	assert.Contains(t, output, "graph TD")

	// Condition node uses diamond shape.
	assert.Contains(t, output, "decide{")

	// Subgraph present.
	assert.Contains(t, output, "subgraph")
	assert.Contains(t, output, "end")
}

func TestRenderMermaidParallel(t *testing.T) {
	model, err := Build(parallelWorkflow(), nil)
	require.NoError(t, err)

	output := RenderMermaid(model)
	assert.Contains(t, output, "graph TD")

	// Parallel node uses double brackets.
	assert.Contains(t, output, "fan_out[[")
}

func TestRenderMermaidLoop(t *testing.T) {
	model, err := Build(loopWorkflow(), nil)
	require.NoError(t, err)

	output := RenderMermaid(model)
	assert.Contains(t, output, "graph TD")

	// Loop node uses double brackets.
	assert.Contains(t, output, "iterate[[")
}

func TestRenderMermaidWithStatus(t *testing.T) {
	def := linearWorkflow()
	states := []*store.StepState{
		{StepID: "fetch", Status: schema.StepStatusCompleted},
		{StepID: "transform", Status: schema.StepStatusRunning},
		{StepID: "store", Status: schema.StepStatusPending},
	}

	model, err := Build(def, states)
	require.NoError(t, err)

	output := RenderMermaid(model)

	// Verify class assignments.
	assert.Contains(t, output, "class fetch completed")
	assert.Contains(t, output, "class transform running")
	assert.Contains(t, output, "class store pending")
}

func TestMermaidSafeID(t *testing.T) {
	assert.Equal(t, "a_b_c", mermaidSafeID("a.b.c"))
	assert.Equal(t, "my_step", mermaidSafeID("my-step"))
	assert.Equal(t, "simple", mermaidSafeID("simple"))
}
