package diagram

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderASCIILinear(t *testing.T) {
	model, err := Build(linearWorkflow(), nil)
	require.NoError(t, err)

	output := RenderASCII(model)
	assert.NotEmpty(t, output)

	// Verify title.
	assert.Contains(t, output, "ETL Pipeline")

	// Verify box-drawing characters.
	assert.Contains(t, output, "\u250c") // ┌
	assert.Contains(t, output, "\u2510") // ┐
	assert.Contains(t, output, "\u2514") // └
	assert.Contains(t, output, "\u2518") // ┘
	assert.Contains(t, output, "\u2502") // │
	assert.Contains(t, output, "\u2500") // ─

	// Verify node labels.
	assert.Contains(t, output, "Start")
	assert.Contains(t, output, "End")
	assert.Contains(t, output, "fetch")
	assert.Contains(t, output, "transform")
	assert.Contains(t, output, "store")
}

func TestRenderASCIIWithStatus(t *testing.T) {
	model := &DiagramModel{
		Title: "Test",
		Nodes: []*Node{
			{ID: "s", Label: "Start", Kind: NodeKindStart},
			{ID: "a", Label: "step-a", Kind: NodeKindAction, Status: &StatusOverlay{Status: "completed", DurationMs: 100}},
			{ID: "b", Label: "step-b", Kind: NodeKindAction, Status: &StatusOverlay{Status: "failed"}},
			{ID: "c", Label: "step-c", Kind: NodeKindAction, Status: &StatusOverlay{Status: "running"}},
			{ID: "d", Label: "step-d", Kind: NodeKindAction, Status: &StatusOverlay{Status: "suspended"}},
			{ID: "e", Label: "step-e", Kind: NodeKindAction, Status: &StatusOverlay{Status: "skipped"}},
			{ID: "f", Label: "step-f", Kind: NodeKindAction, Status: &StatusOverlay{Status: "pending"}},
			{ID: "end", Label: "End", Kind: NodeKindEnd},
		},
		Levels: [][]string{{"s"}, {"a", "b", "c"}, {"d", "e", "f"}, {"end"}},
	}

	output := RenderASCII(model)

	// Verify status indicators.
	assert.Contains(t, output, "[OK]")
	assert.Contains(t, output, "[FAIL]")
	assert.Contains(t, output, "[RUN]")
	assert.Contains(t, output, "[WAIT]")
	assert.Contains(t, output, "[SKIP]")
	assert.Contains(t, output, "[PEND]")
	assert.Contains(t, output, "100ms")
}

func TestRenderASCIIWithSubgraphs(t *testing.T) {
	model, err := Build(conditionWorkflow(), nil)
	require.NoError(t, err)

	output := RenderASCII(model)
	assert.Contains(t, output, "sub-steps")
}
