package diagram

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RenderASCIIAuto tries to render using the mermaid-ascii CLI binary if available,
// falling back to the hand-rolled RenderASCII renderer.
func RenderASCIIAuto(model *DiagramModel, binDir string) string {
	if binDir != "" {
		binPath := filepath.Join(binDir, "mermaid-ascii")
		if _, err := os.Stat(binPath); err == nil {
			result, err := RenderASCIIViaCLI(model, binPath)
			if err == nil {
				return result
			}
		}
	}
	return RenderASCII(model)
}

// RenderASCIIViaCLI pipes simplified Mermaid syntax through the mermaid-ascii binary.
func RenderASCIIViaCLI(model *DiagramModel, binPath string) (string, error) {
	mermaid := RenderMermaidForCLI(model)

	cmd := exec.Command(binPath)
	cmd.Stdin = strings.NewReader(mermaid)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("mermaid-ascii: %w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

// RenderMermaidForCLI generates simplified Mermaid syntax compatible with the
// mermaid-ascii CLI tool. Unlike RenderMermaid, this avoids node declarations
// with ["label"] syntax (which mermaid-ascii cannot parse) and instead embeds
// status information directly in edge-referenced node IDs.
// Subgraphs (condition branches, parallel branches, loop bodies) are flattened
// into top-level edges since mermaid-ascii silently ignores subgraph blocks.
func RenderMermaidForCLI(model *DiagramModel) string {
	var b strings.Builder
	b.WriteString("graph TD\n")

	// Build a mapping from node ID to display ID (with status info).
	// Includes both top-level nodes and nested subgraph nodes.
	displayID := make(map[string]string, len(model.Nodes))
	for _, node := range model.Nodes {
		displayID[node.ID] = cliNodeID(node)
		for _, sg := range node.Children {
			for _, subNode := range sg.Nodes {
				displayID[subNode.ID] = cliNodeID(subNode)
			}
		}
	}

	// Helper to resolve display ID.
	resolve := func(id string) string {
		if d, ok := displayID[id]; ok {
			return d
		}
		return mermaidSafeID(id)
	}

	// Emit top-level edges.
	for _, edge := range model.Edges {
		label := ""
		if edge.Label != "" {
			label = fmt.Sprintf("|%s|", edge.Label)
		}
		b.WriteString(fmt.Sprintf("    %s -->%s %s\n", resolve(edge.From), label, resolve(edge.To)))
	}

	// Flatten subgraph edges into top-level edges.
	for _, node := range model.Nodes {
		for _, sg := range node.Children {
			// Connect parent → first sub-node with branch label.
			if len(sg.Nodes) > 0 {
				label := fmt.Sprintf("|%s|", sg.Label)
				b.WriteString(fmt.Sprintf("    %s -->%s %s\n", resolve(node.ID), label, resolve(sg.Nodes[0].ID)))
			}
			// Emit internal subgraph edges.
			for _, edge := range sg.Edges {
				label := ""
				if edge.Label != "" {
					label = fmt.Sprintf("|%s|", edge.Label)
				}
				b.WriteString(fmt.Sprintf("    %s -->%s %s\n", resolve(edge.From), label, resolve(edge.To)))
			}
		}
	}

	return b.String()
}

// cliNodeID builds a display ID for the mermaid-ascii CLI.
// Embeds status tag and duration into the ID for visibility.
func cliNodeID(node *Node) string {
	id := node.Label
	if id == "" {
		id = node.ID
	}
	id = firstLine(id)

	// Strip action type suffix like " (http.request)" for cleaner IDs.
	if idx := strings.Index(id, " ("); idx > 0 {
		id = id[:idx]
	}

	if node.Status != nil {
		tag := cliStatusTag(node.Status.Status)
		if tag != "" {
			id += "-" + tag
		}
		if node.Status.DurationMs > 0 {
			id += fmt.Sprintf("-%dms", node.Status.DurationMs)
		}
	}

	// Replace spaces with dashes for valid Mermaid IDs.
	id = strings.ReplaceAll(id, " ", "-")
	return id
}

// cliStatusTag returns a compact status indicator for node IDs.
func cliStatusTag(status string) string {
	switch status {
	case "completed":
		return "OK"
	case "failed":
		return "FAIL"
	case "running":
		return "RUN"
	case "suspended":
		return "WAIT"
	case "skipped":
		return "SKIP"
	case "pending", "scheduled":
		return "PEND"
	case "retrying":
		return "RETRY"
	default:
		return ""
	}
}
