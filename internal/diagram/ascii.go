package diagram

import (
	"fmt"
	"strings"
)

// statusTag returns a short ASCII indicator for a status string.
func statusTag(status string) string {
	switch status {
	case "completed":
		return "[OK]"
	case "failed":
		return "[FAIL]"
	case "running":
		return "[RUN]"
	case "suspended":
		return "[WAIT]"
	case "skipped":
		return "[SKIP]"
	case "pending", "scheduled":
		return "[PEND]"
	case "retrying":
		return "[RETRY]"
	default:
		return ""
	}
}

// RenderASCII renders a DiagramModel as a text-based ASCII diagram.
// It uses a level-based layout with box-drawing characters.
func RenderASCII(model *DiagramModel) string {
	var b strings.Builder

	// Title.
	if model.Title != "" {
		b.WriteString(fmt.Sprintf("=== %s ===\n\n", model.Title))
	}

	// Render each level.
	for levelIdx, level := range model.Levels {
		// Collect boxes for this level.
		var boxes []asciiBox
		for _, nodeID := range level {
			node := findNode(model.Nodes, nodeID)
			if node == nil {
				continue
			}
			boxes = append(boxes, makeBox(node))
		}

		// Render boxes side-by-side.
		renderBoxRow(&b, boxes)

		// Draw connectors between levels (except after last level).
		if levelIdx < len(model.Levels)-1 {
			renderConnector(&b, len(boxes))
		}
	}

	// Render subgraphs for nodes with children.
	for _, node := range model.Nodes {
		if len(node.Children) > 0 {
			b.WriteString(fmt.Sprintf("\n--- %s sub-steps ---\n", node.ID))
			for _, sg := range node.Children {
				renderSubGraph(&b, sg)
			}
		}
	}

	return b.String()
}

// asciiBox holds the rendered lines of a single box.
type asciiBox struct {
	lines []string
	width int
}

// makeBox creates an ASCII box for a node.
func makeBox(node *Node) asciiBox {
	// Build content lines.
	var contentLines []string

	// For start/end, use a simple label.
	label := firstLine(node.Label)
	contentLines = append(contentLines, label)

	// Add status tag if present.
	if node.Status != nil {
		tag := statusTag(node.Status.Status)
		if tag != "" {
			contentLines = append(contentLines, tag)
		}
		if node.Status.DurationMs > 0 {
			contentLines = append(contentLines, fmt.Sprintf("%dms", node.Status.DurationMs))
		}
	}

	// Calculate width.
	maxLen := 0
	for _, line := range contentLines {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}
	width := maxLen + 4 // 2 border + 2 padding

	// Build box lines.
	var lines []string
	top := "\u250c" + strings.Repeat("\u2500", width-2) + "\u2510"
	bot := "\u2514" + strings.Repeat("\u2500", width-2) + "\u2518"
	lines = append(lines, top)
	for _, content := range contentLines {
		padded := content + strings.Repeat(" ", maxLen-len(content))
		lines = append(lines, "\u2502 "+padded+" \u2502")
	}
	lines = append(lines, bot)

	return asciiBox{lines: lines, width: width}
}

// firstLine returns only the first line of a multi-line label.
func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// renderBoxRow writes boxes side by side.
func renderBoxRow(b *strings.Builder, boxes []asciiBox) {
	if len(boxes) == 0 {
		return
	}

	// Find max height.
	maxHeight := 0
	for _, box := range boxes {
		if len(box.lines) > maxHeight {
			maxHeight = len(box.lines)
		}
	}

	// Render line by line.
	for row := 0; row < maxHeight; row++ {
		for i, box := range boxes {
			if i > 0 {
				b.WriteString("  ") // gap between boxes
			}
			if row < len(box.lines) {
				b.WriteString(box.lines[row])
			} else {
				b.WriteString(strings.Repeat(" ", box.width))
			}
		}
		b.WriteByte('\n')
	}
}

// renderConnector draws a vertical connector between levels.
func renderConnector(b *strings.Builder, boxCount int) {
	if boxCount == 0 {
		return
	}
	// Simple center connector.
	b.WriteString("       \u2502\n")
	b.WriteString("       \u25bc\n")
}

// renderSubGraph renders a subgraph section.
func renderSubGraph(b *strings.Builder, sg *SubGraph) {
	b.WriteString(fmt.Sprintf("  [%s]\n", sg.Label))
	for _, node := range sg.Nodes {
		tag := ""
		if node.Status != nil {
			tag = " " + statusTag(node.Status.Status)
		}
		b.WriteString(fmt.Sprintf("    %s%s\n", firstLine(node.Label), tag))
	}
	for _, edge := range sg.Edges {
		b.WriteString(fmt.Sprintf("    %s \u2500\u2192 %s\n", shortID(edge.From), shortID(edge.To)))
	}
}

// shortID returns the last segment of a dot-separated ID.
func shortID(id string) string {
	if i := strings.LastIndex(id, "."); i >= 0 {
		return id[i+1:]
	}
	return id
}

// findNode looks up a node by ID in the model's node list.
func findNode(nodes []*Node, id string) *Node {
	for _, n := range nodes {
		if n.ID == id {
			return n
		}
	}
	return nil
}
