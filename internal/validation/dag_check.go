package validation

import (
	"fmt"
	"sort"

	"github.com/rendis/opcode/pkg/schema"
)

// validateDAG performs graph analysis on top-level steps:
// cycle detection (Kahn's algorithm) and dead-step reachability (BFS from roots).
func validateDAG(def *schema.WorkflowDefinition) *schema.ValidationResult {
	result := &schema.ValidationResult{}

	// Build step ID set and adjacency lists.
	stepIDs := make(map[string]bool, len(def.Steps))
	for _, s := range def.Steps {
		stepIDs[s.ID] = true
	}

	// edges[id] = dependencies of step id, reverse[id] = dependents of step id.
	edges := make(map[string][]string, len(def.Steps))
	reverse := make(map[string][]string, len(def.Steps))

	for _, s := range def.Steps {
		seen := make(map[string]bool, len(s.DependsOn))
		for _, dep := range s.DependsOn {
			if !stepIDs[dep] || seen[dep] {
				continue // invalid refs already caught by semantic
			}
			seen[dep] = true
			edges[s.ID] = append(edges[s.ID], dep)
			reverse[dep] = append(reverse[dep], s.ID)
		}
	}

	// Kahn's algorithm for cycle detection.
	inDegree := make(map[string]int, len(def.Steps))
	for id := range stepIDs {
		inDegree[id] = len(edges[id])
	}

	queue := make([]string, 0, len(def.Steps))
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	// Sort roots for deterministic output.
	sort.Strings(queue)

	visited := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		visited++
		for _, dep := range reverse[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if visited != len(stepIDs) {
		result.AddError("steps", schema.ErrCodeCycleDetected, "workflow contains a dependency cycle")
		return result // cycle makes reachability analysis meaningless
	}

	// Reachability: BFS from root steps (no dependencies) through reverse edges.
	roots := make([]string, 0)
	for id := range stepIDs {
		if len(edges[id]) == 0 {
			roots = append(roots, id)
		}
	}

	reachable := make(map[string]bool, len(stepIDs))
	bfsQueue := make([]string, len(roots))
	copy(bfsQueue, roots)
	for _, r := range roots {
		reachable[r] = true
	}

	for len(bfsQueue) > 0 {
		node := bfsQueue[0]
		bfsQueue = bfsQueue[1:]
		for _, dep := range reverse[node] {
			if !reachable[dep] {
				reachable[dep] = true
				bfsQueue = append(bfsQueue, dep)
			}
		}
	}

	for _, s := range def.Steps {
		if !reachable[s.ID] {
			result.AddWarning(fmt.Sprintf("steps[%s]", s.ID),
				schema.ErrCodeValidation,
				fmt.Sprintf("step %q is unreachable from any root step", s.ID))
		}
	}

	return result
}
