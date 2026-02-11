package diagram

import (
	"encoding/json"
	"fmt"

	"github.com/rendis/opcode/internal/engine"
	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
)

// Build constructs a DiagramModel from a WorkflowDefinition and optional step states.
// It uses engine.ParseDAG for topology and maps each step to a Node with the
// appropriate kind. Flow control steps get SubGraph children.
func Build(def *schema.WorkflowDefinition, states []*store.StepState) (*DiagramModel, error) {
	dag, err := engine.ParseDAG(def)
	if err != nil {
		return nil, fmt.Errorf("diagram: parse DAG: %w", err)
	}

	// Index states by step ID for fast lookup.
	stateMap := make(map[string]*store.StepState, len(states))
	for _, s := range states {
		stateMap[s.StepID] = s
	}

	// Build node index from DAG steps.
	nodeIndex := make(map[string]*Node, len(dag.Steps))
	nodes := make([]*Node, 0, len(dag.Steps)+2) // +2 for start/end

	// Virtual start node.
	startNode := &Node{ID: "__start__", Label: "Start", Kind: NodeKindStart}
	nodes = append(nodes, startNode)
	nodeIndex["__start__"] = startNode

	// Create nodes from DAG steps, preserving topological order.
	for _, stepID := range dag.Sorted {
		step := dag.Steps[stepID]
		node := stepToNode(step)
		overlayStatus(node, stateMap)
		buildChildren(node, step, stateMap)
		nodes = append(nodes, node)
		nodeIndex[stepID] = node
	}

	// Virtual end node.
	endNode := &Node{ID: "__end__", Label: "End", Kind: NodeKindEnd}
	nodes = append(nodes, endNode)
	nodeIndex["__end__"] = endNode

	// Build edges from DAG + virtual start/end edges.
	edges := buildEdges(dag)

	// Build levels including virtual nodes.
	levels := buildLevels(dag)

	return &DiagramModel{
		Title:  titleFromDef(def),
		Nodes:  nodes,
		Edges:  edges,
		Levels: levels,
	}, nil
}

// stepToNode maps a StepDefinition to a diagram Node.
func stepToNode(step *schema.StepDefinition) *Node {
	return &Node{
		ID:    step.ID,
		Label: nodeLabel(step),
		Kind:  stepTypeToKind(step.Type),
	}
}

// stepTypeToKind converts a schema.StepType to a NodeKind.
func stepTypeToKind(st schema.StepType) NodeKind {
	switch st {
	case schema.StepTypeAction:
		return NodeKindAction
	case schema.StepTypeCondition:
		return NodeKindCondition
	case schema.StepTypeReasoning:
		return NodeKindReasoning
	case schema.StepTypeParallel:
		return NodeKindParallel
	case schema.StepTypeLoop:
		return NodeKindLoop
	case schema.StepTypeWait:
		return NodeKindWait
	default:
		return NodeKindAction
	}
}

// nodeLabel creates a human-readable label for a node.
func nodeLabel(step *schema.StepDefinition) string {
	if step.Action != "" {
		return fmt.Sprintf("%s\n(%s)", step.ID, step.Action)
	}
	return step.ID
}

// overlayStatus applies runtime step state to a node.
func overlayStatus(node *Node, stateMap map[string]*store.StepState) {
	if ss, ok := stateMap[node.ID]; ok {
		errStr := ""
		if len(ss.Error) > 0 {
			errStr = string(ss.Error)
		}
		node.Status = &StatusOverlay{
			Status:     string(ss.Status),
			DurationMs: ss.DurationMs,
			RetryCount: ss.RetryCount,
			Error:      errStr,
		}
	}
}

// buildChildren parses flow control config and creates SubGraph children.
func buildChildren(node *Node, step *schema.StepDefinition, stateMap map[string]*store.StepState) {
	if len(step.Config) == 0 {
		return
	}

	switch step.Type {
	case schema.StepTypeCondition:
		var cfg schema.ConditionConfig
		if json.Unmarshal(step.Config, &cfg) != nil {
			return
		}
		for branchName, branchSteps := range cfg.Branches {
			sg := buildSubGraph(branchName, step.ID, branchName, branchSteps, stateMap)
			node.Children = append(node.Children, sg)
		}
		if len(cfg.Default) > 0 {
			sg := buildSubGraph("default", step.ID, "default", cfg.Default, stateMap)
			node.Children = append(node.Children, sg)
		}

	case schema.StepTypeParallel:
		var cfg schema.ParallelConfig
		if json.Unmarshal(step.Config, &cfg) != nil {
			return
		}
		for i, branchSteps := range cfg.Branches {
			label := fmt.Sprintf("branch_%d", i)
			sg := buildSubGraph(label, step.ID, label, branchSteps, stateMap)
			node.Children = append(node.Children, sg)
		}

	case schema.StepTypeLoop:
		var cfg schema.LoopConfig
		if json.Unmarshal(step.Config, &cfg) != nil {
			return
		}
		if len(cfg.Body) > 0 {
			sg := buildSubGraph("body", step.ID, "body", cfg.Body, stateMap)
			node.Children = append(node.Children, sg)
		}
	}
}

// buildSubGraph creates a SubGraph from a list of nested step definitions.
// Sub-step IDs follow executor naming: parentID.namespace.subStepID
func buildSubGraph(label, parentID, namespace string, steps []schema.StepDefinition, stateMap map[string]*store.StepState) *SubGraph {
	sg := &SubGraph{Label: label}
	nodeMap := make(map[string]bool, len(steps))

	for i := range steps {
		sub := &steps[i]
		qualifiedID := fmt.Sprintf("%s.%s.%s", parentID, namespace, sub.ID)
		subNode := &Node{
			ID:    qualifiedID,
			Label: subNodeLabel(sub),
			Kind:  stepTypeToKind(sub.Type),
		}
		// Try overlaying status with qualified ID.
		if ss, ok := stateMap[qualifiedID]; ok {
			errStr := ""
			if len(ss.Error) > 0 {
				errStr = string(ss.Error)
			}
			subNode.Status = &StatusOverlay{
				Status:     string(ss.Status),
				DurationMs: ss.DurationMs,
				RetryCount: ss.RetryCount,
				Error:      errStr,
			}
		}
		sg.Nodes = append(sg.Nodes, subNode)
		nodeMap[sub.ID] = true
	}

	// Build edges within the subgraph.
	for i := range steps {
		sub := &steps[i]
		qualifiedID := fmt.Sprintf("%s.%s.%s", parentID, namespace, sub.ID)
		for _, dep := range sub.DependsOn {
			if nodeMap[dep] {
				sg.Edges = append(sg.Edges, Edge{
					From: fmt.Sprintf("%s.%s.%s", parentID, namespace, dep),
					To:   qualifiedID,
				})
			}
		}
	}

	return sg
}

// subNodeLabel creates a label for a sub-step node.
func subNodeLabel(step *schema.StepDefinition) string {
	if step.Action != "" {
		return fmt.Sprintf("%s (%s)", step.ID, step.Action)
	}
	return step.ID
}

// buildEdges constructs Edge list from DAG adjacency, adding virtual start/end edges.
func buildEdges(dag *engine.DAG) []Edge {
	var edges []Edge

	// Start → roots.
	for _, root := range dag.Roots {
		edges = append(edges, Edge{From: "__start__", To: root})
	}

	// Step dependencies (reverse direction: dependency → dependent).
	for stepID, deps := range dag.Edges {
		for _, dep := range deps {
			edges = append(edges, Edge{From: dep, To: stepID})
		}
	}

	// Leaves → end (steps with no dependents).
	for stepID := range dag.Steps {
		if len(dag.Reverse[stepID]) == 0 {
			edges = append(edges, Edge{From: stepID, To: "__end__"})
		}
	}

	return edges
}

// buildLevels wraps DAG levels with virtual start/end levels.
func buildLevels(dag *engine.DAG) [][]string {
	levels := make([][]string, 0, len(dag.Levels)+2)
	levels = append(levels, []string{"__start__"})
	levels = append(levels, dag.Levels...)
	levels = append(levels, []string{"__end__"})
	return levels
}

// titleFromDef generates a diagram title from workflow metadata.
func titleFromDef(def *schema.WorkflowDefinition) string {
	if def.Metadata != nil {
		if name, ok := def.Metadata["name"].(string); ok && name != "" {
			return name
		}
	}
	return "Workflow"
}
