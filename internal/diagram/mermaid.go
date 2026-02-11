package diagram

import (
	"fmt"
	"strings"
)

// RenderMermaid renders a DiagramModel as a Mermaid flowchart string.
func RenderMermaid(model *DiagramModel) string {
	var b strings.Builder

	b.WriteString("graph TD\n")

	// Title as comment.
	if model.Title != "" {
		b.WriteString(fmt.Sprintf("    %%%% %s\n", model.Title))
	}

	// Render nodes with shapes based on kind.
	for _, node := range model.Nodes {
		b.WriteString(fmt.Sprintf("    %s\n", mermaidNodeDef(node)))

		// Subgraphs for children.
		for _, sg := range node.Children {
			b.WriteString(fmt.Sprintf("    subgraph %s[\"%s: %s\"]\n",
				mermaidSafeID(node.ID+"_"+sg.Label), node.ID, sg.Label))
			for _, subNode := range sg.Nodes {
				b.WriteString(fmt.Sprintf("        %s\n", mermaidNodeDef(subNode)))
			}
			for _, edge := range sg.Edges {
				label := ""
				if edge.Label != "" {
					label = fmt.Sprintf("|%s|", edge.Label)
				}
				b.WriteString(fmt.Sprintf("        %s -->%s %s\n",
					mermaidSafeID(edge.From), label, mermaidSafeID(edge.To)))
			}
			b.WriteString("    end\n")
		}
	}

	// Render edges.
	for _, edge := range model.Edges {
		label := ""
		if edge.Label != "" {
			label = fmt.Sprintf("|%s|", edge.Label)
		}
		b.WriteString(fmt.Sprintf("    %s -->%s %s\n",
			mermaidSafeID(edge.From), label, mermaidSafeID(edge.To)))
	}

	// Status class definitions.
	b.WriteString("\n")
	b.WriteString("    classDef completed fill:#2d6a2d,stroke:#1a4a1a,color:#fff\n")
	b.WriteString("    classDef failed fill:#8b1a1a,stroke:#5c0e0e,color:#fff\n")
	b.WriteString("    classDef running fill:#1a5276,stroke:#0e3a52,color:#fff\n")
	b.WriteString("    classDef suspended fill:#b7791a,stroke:#8a5c14,color:#fff\n")
	b.WriteString("    classDef pending fill:#6b6b6b,stroke:#4a4a4a,color:#fff\n")
	b.WriteString("    classDef skipped fill:#4a4a4a,stroke:#333,color:#aaa,stroke-dasharray:5 5\n")

	// Apply status classes.
	for _, node := range model.Nodes {
		if node.Status != nil {
			cls := mermaidStatusClass(node.Status.Status)
			if cls != "" {
				b.WriteString(fmt.Sprintf("    class %s %s\n", mermaidSafeID(node.ID), cls))
			}
		}
		for _, sg := range node.Children {
			for _, subNode := range sg.Nodes {
				if subNode.Status != nil {
					cls := mermaidStatusClass(subNode.Status.Status)
					if cls != "" {
						b.WriteString(fmt.Sprintf("    class %s %s\n", mermaidSafeID(subNode.ID), cls))
					}
				}
			}
		}
	}

	return b.String()
}

// mermaidNodeDef returns a Mermaid node definition with the appropriate shape.
func mermaidNodeDef(node *Node) string {
	id := mermaidSafeID(node.ID)
	label := mermaidEscapeLabel(firstLine(node.Label))

	switch node.Kind {
	case NodeKindCondition:
		return fmt.Sprintf("%s{%q}", id, label)
	case NodeKindReasoning:
		return fmt.Sprintf("%s{{%q}}", id, label)
	case NodeKindWait:
		return fmt.Sprintf("%s([%q])", id, label)
	case NodeKindParallel:
		return fmt.Sprintf("%s[[%q]]", id, label)
	case NodeKindLoop:
		return fmt.Sprintf("%s[[%q]]", id, label)
	case NodeKindStart:
		return fmt.Sprintf("%s((%q))", id, label)
	case NodeKindEnd:
		return fmt.Sprintf("%s((%q))", id, label)
	default: // action
		return fmt.Sprintf("%s[%q]", id, label)
	}
}

// mermaidSafeID converts a node ID to a Mermaid-safe identifier.
// Replaces dots and dashes with underscores.
func mermaidSafeID(id string) string {
	r := strings.NewReplacer(".", "_", "-", "_", " ", "_")
	return r.Replace(id)
}

// mermaidEscapeLabel escapes special characters for Mermaid labels.
func mermaidEscapeLabel(s string) string {
	return s
}

// mermaidStatusClass maps a status string to a Mermaid class name.
func mermaidStatusClass(status string) string {
	switch status {
	case "completed":
		return "completed"
	case "failed":
		return "failed"
	case "running":
		return "running"
	case "suspended":
		return "suspended"
	case "pending", "scheduled":
		return "pending"
	case "skipped":
		return "skipped"
	default:
		return ""
	}
}
