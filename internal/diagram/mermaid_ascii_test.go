package diagram

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderMermaidForCLI_BasicDAG(t *testing.T) {
	model := &DiagramModel{
		Title: "Test",
		Nodes: []*Node{
			{ID: "__start__", Label: "Start", Kind: NodeKindStart},
			{ID: "fetch", Label: "fetch", Kind: NodeKindAction},
			{ID: "process", Label: "process", Kind: NodeKindAction},
			{ID: "__end__", Label: "End", Kind: NodeKindEnd},
		},
		Edges: []Edge{
			{From: "__start__", To: "fetch"},
			{From: "fetch", To: "process"},
			{From: "process", To: "__end__"},
		},
	}

	result := RenderMermaidForCLI(model)

	assert.Contains(t, result, "graph TD")
	assert.Contains(t, result, "Start --> fetch")
	assert.Contains(t, result, "fetch --> process")
	assert.Contains(t, result, "process --> End")
	// Must NOT contain node declarations with ["..."] syntax.
	assert.NotContains(t, result, "[\"")
	assert.NotContains(t, result, "classDef")
}

func TestRenderMermaidForCLI_WithStatus(t *testing.T) {
	model := &DiagramModel{
		Title: "Test",
		Nodes: []*Node{
			{ID: "__start__", Label: "Start", Kind: NodeKindStart},
			{ID: "fetch-data", Label: "fetch-data", Kind: NodeKindAction,
				Status: &StatusOverlay{Status: "completed", DurationMs: 450}},
			{ID: "validate", Label: "validate", Kind: NodeKindAction,
				Status: &StatusOverlay{Status: "completed", DurationMs: 12}},
			{ID: "decide", Label: "decide", Kind: NodeKindReasoning,
				Status: &StatusOverlay{Status: "suspended"}},
			{ID: "__end__", Label: "End", Kind: NodeKindEnd},
		},
		Edges: []Edge{
			{From: "__start__", To: "fetch-data"},
			{From: "fetch-data", To: "validate"},
			{From: "validate", To: "decide"},
			{From: "decide", To: "__end__"},
		},
	}

	result := RenderMermaidForCLI(model)

	assert.Contains(t, result, "fetch-data-OK-450ms")
	assert.Contains(t, result, "validate-OK-12ms")
	assert.Contains(t, result, "decide-WAIT")
}

func TestRenderMermaidForCLI_EdgeLabels(t *testing.T) {
	model := &DiagramModel{
		Nodes: []*Node{
			{ID: "decide", Label: "decide", Kind: NodeKindCondition},
			{ID: "fast", Label: "fast", Kind: NodeKindAction},
			{ID: "slow", Label: "slow", Kind: NodeKindAction},
		},
		Edges: []Edge{
			{From: "decide", To: "fast", Label: "quick"},
			{From: "decide", To: "slow", Label: "thorough"},
		},
	}

	result := RenderMermaidForCLI(model)

	assert.Contains(t, result, "-->|quick|")
	assert.Contains(t, result, "-->|thorough|")
}

func TestRenderMermaidForCLI_FlattensBranches(t *testing.T) {
	model := &DiagramModel{
		Nodes: []*Node{
			{ID: "__start__", Label: "Start", Kind: NodeKindStart},
			{ID: "check", Label: "check", Kind: NodeKindCondition,
				Children: []*SubGraph{
					{Label: "yes", Nodes: []*Node{
						{ID: "check.yes.pay", Label: "pay", Kind: NodeKindAction,
							Status: &StatusOverlay{Status: "completed", DurationMs: 100}},
					}},
					{Label: "no", Nodes: []*Node{
						{ID: "check.no.reject", Label: "reject", Kind: NodeKindAction,
							Status: &StatusOverlay{Status: "skipped"}},
					}},
				}},
			{ID: "__end__", Label: "End", Kind: NodeKindEnd},
		},
		Edges: []Edge{
			{From: "__start__", To: "check"},
			{From: "check", To: "__end__"},
		},
	}

	result := RenderMermaidForCLI(model)

	// Branch edges should be flattened with labels.
	assert.Contains(t, result, "-->|yes|")
	assert.Contains(t, result, "-->|no|")
	// Sub-node IDs should include status.
	assert.Contains(t, result, "pay-OK-100ms")
	assert.Contains(t, result, "reject-SKIP")
	// No subgraph syntax.
	assert.NotContains(t, result, "subgraph")
}

func TestCLINodeID_StripsActionSuffix(t *testing.T) {
	node := &Node{
		ID:     "check.yes.pay",
		Label:  "pay (http.request)",
		Status: &StatusOverlay{Status: "completed", DurationMs: 200},
	}
	assert.Equal(t, "pay-OK-200ms", cliNodeID(node))
}

func TestCLINodeID(t *testing.T) {
	tests := []struct {
		name     string
		node     *Node
		expected string
	}{
		{
			name:     "no status",
			node:     &Node{ID: "fetch", Label: "fetch"},
			expected: "fetch",
		},
		{
			name:     "completed with duration",
			node:     &Node{ID: "fetch", Label: "fetch", Status: &StatusOverlay{Status: "completed", DurationMs: 450}},
			expected: "fetch-OK-450ms",
		},
		{
			name:     "suspended no duration",
			node:     &Node{ID: "decide", Label: "decide", Status: &StatusOverlay{Status: "suspended"}},
			expected: "decide-WAIT",
		},
		{
			name:     "pending",
			node:     &Node{ID: "process", Label: "process", Status: &StatusOverlay{Status: "pending"}},
			expected: "process-PEND",
		},
		{
			name:     "uses label over id",
			node:     &Node{ID: "__start__", Label: "Start"},
			expected: "Start",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, cliNodeID(tt.node))
		})
	}
}

func TestRenderASCIIViaCLI(t *testing.T) {
	// Skip if mermaid-ascii is not installed.
	binPath := findMermaidASCII()
	if binPath == "" {
		t.Skip("mermaid-ascii binary not found, skipping CLI test")
	}

	model := &DiagramModel{
		Title: "Test",
		Nodes: []*Node{
			{ID: "__start__", Label: "Start", Kind: NodeKindStart},
			{ID: "fetch", Label: "fetch", Kind: NodeKindAction},
			{ID: "__end__", Label: "End", Kind: NodeKindEnd},
		},
		Edges: []Edge{
			{From: "__start__", To: "fetch"},
			{From: "fetch", To: "__end__"},
		},
	}

	result, err := RenderASCIIViaCLI(model, binPath)
	require.NoError(t, err)

	assert.Contains(t, result, "Start")
	assert.Contains(t, result, "fetch")
	assert.Contains(t, result, "End")
	// Should contain box-drawing characters.
	assert.Contains(t, result, "┌")
	assert.Contains(t, result, "└")
}

func TestRenderASCIIAuto_Fallback(t *testing.T) {
	model := &DiagramModel{
		Title: "Test",
		Nodes: []*Node{
			{ID: "__start__", Label: "Start", Kind: NodeKindStart},
			{ID: "fetch", Label: "fetch", Kind: NodeKindAction},
			{ID: "__end__", Label: "End", Kind: NodeKindEnd},
		},
		Edges: []Edge{
			{From: "__start__", To: "fetch"},
			{From: "fetch", To: "__end__"},
		},
		Levels: [][]string{{"__start__"}, {"fetch"}, {"__end__"}},
	}

	// With non-existent binDir, should fallback to hand-rolled.
	result := RenderASCIIAuto(model, "/nonexistent/path")

	assert.Contains(t, result, "=== Test ===")
	assert.Contains(t, result, "Start")
	assert.Contains(t, result, "fetch")
}

func TestRenderASCIIAuto_EmptyBinDir(t *testing.T) {
	model := &DiagramModel{
		Title: "Test",
		Nodes: []*Node{
			{ID: "__start__", Label: "Start", Kind: NodeKindStart},
		},
		Levels: [][]string{{"__start__"}},
	}

	// Empty binDir should fallback gracefully.
	result := RenderASCIIAuto(model, "")
	assert.Contains(t, result, "Start")
}

// findMermaidASCII checks common paths for the mermaid-ascii binary.
func findMermaidASCII() string {
	// Check ~/.opcode/bin first.
	home, _ := os.UserHomeDir()
	if home != "" {
		p := home + "/.opcode/bin/mermaid-ascii"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Check /tmp (used during development).
	if _, err := os.Stat("/tmp/mermaid-ascii"); err == nil {
		return "/tmp/mermaid-ascii"
	}
	return ""
}
