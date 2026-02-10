package engine

import (
	"encoding/json"
	"fmt"

	"github.com/rendis/opcode/pkg/schema"
)

// DAG is the in-memory directed acyclic graph representation of a workflow.
// Built from a WorkflowDefinition, used by the Executor to determine execution order.
type DAG struct {
	Steps   map[string]*schema.StepDefinition // step ID → definition
	Edges   map[string][]string               // step ID → dependencies (depends_on)
	Reverse map[string][]string               // step ID → dependents (who depends on me)
	Sorted  []string                          // topological order
	Roots   []string                          // steps with no dependencies
	Levels  [][]string                        // parallel execution levels
}

// validStepTypes is the set of recognized step types.
var validStepTypes = map[schema.StepType]bool{
	schema.StepTypeAction:    true,
	schema.StepTypeCondition: true,
	schema.StepTypeReasoning: true,
	schema.StepTypeParallel:  true,
	schema.StepTypeLoop:      true,
	schema.StepTypeWait:      true,
}

// ParseDAG parses a WorkflowDefinition into an executable DAG.
// It validates the definition, builds adjacency lists, performs topological
// sorting using Kahn's algorithm, detects cycles, and computes parallel
// execution levels.
func ParseDAG(def *schema.WorkflowDefinition) (*DAG, error) {
	if def == nil {
		return nil, schema.NewError(schema.ErrCodeValidation, "workflow definition is nil")
	}

	if len(def.Steps) == 0 {
		return nil, schema.NewError(schema.ErrCodeValidation, "workflow has no steps")
	}

	dag := &DAG{
		Steps:   make(map[string]*schema.StepDefinition, len(def.Steps)),
		Edges:   make(map[string][]string, len(def.Steps)),
		Reverse: make(map[string][]string, len(def.Steps)),
	}

	// First pass: register all steps and check for duplicates.
	for i := range def.Steps {
		step := &def.Steps[i]

		if step.ID == "" {
			return nil, schema.NewError(schema.ErrCodeValidation, fmt.Sprintf("step at index %d has empty ID", i))
		}

		if _, exists := dag.Steps[step.ID]; exists {
			return nil, schema.NewErrorf(schema.ErrCodeValidation, "duplicate step ID: %s", step.ID)
		}

		// Default step type to action when empty.
		if step.Type == "" {
			step.Type = schema.StepTypeAction
		}

		if !validStepTypes[step.Type] {
			return nil, schema.NewErrorf(schema.ErrCodeValidation, "step %s has unknown type: %s", step.ID, step.Type)
		}

		dag.Steps[step.ID] = step
	}

	// Second pass: validate step-type-specific constraints.
	for _, step := range dag.Steps {
		if err := validateStepConfig(step); err != nil {
			return nil, err
		}
	}

	// Third pass: build adjacency lists and validate dependencies.
	for id, step := range dag.Steps {
		seen := make(map[string]bool, len(step.DependsOn))
		deps := make([]string, 0, len(step.DependsOn))
		for _, dep := range step.DependsOn {
			if _, exists := dag.Steps[dep]; !exists {
				return nil, schema.NewErrorf(schema.ErrCodeValidation, "step %s depends on non-existent step: %s", id, dep)
			}
			if dep == id {
				return nil, schema.NewErrorf(schema.ErrCodeCycleDetected, "step %s depends on itself", id)
			}
			if seen[dep] {
				return nil, schema.NewErrorf(schema.ErrCodeValidation, "step %s has duplicate dependency: %s", id, dep)
			}
			seen[dep] = true
			deps = append(deps, dep)
			dag.Reverse[dep] = append(dag.Reverse[dep], id)
		}
		dag.Edges[id] = deps
	}

	// Kahn's algorithm: topological sort + cycle detection.
	inDegree := make(map[string]int, len(dag.Steps))
	for id := range dag.Steps {
		inDegree[id] = len(dag.Edges[id])
	}

	// Queue steps with in-degree 0 (roots).
	queue := make([]string, 0)
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	// Sort roots for deterministic ordering.
	sortStrings(queue)
	dag.Roots = make([]string, len(queue))
	copy(dag.Roots, queue)

	sorted := make([]string, 0, len(dag.Steps))
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, node)

		// For each dependent of this node, decrement its in-degree.
		dependents := make([]string, len(dag.Reverse[node]))
		copy(dependents, dag.Reverse[node])
		sortStrings(dependents)

		for _, dep := range dependents {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(sorted) != len(dag.Steps) {
		return nil, schema.NewError(schema.ErrCodeCycleDetected, "workflow contains a cycle")
	}

	dag.Sorted = sorted

	// Compute parallel execution levels using topological depth.
	dag.Levels = computeLevels(dag)

	return dag, nil
}

// computeLevels groups steps into parallel execution levels.
// Steps at the same level have all dependencies satisfied by previous levels.
func computeLevels(dag *DAG) [][]string {
	depth := make(map[string]int, len(dag.Steps))

	// Compute depth for each step based on max dependency depth + 1.
	for _, id := range dag.Sorted {
		maxDep := -1
		for _, dep := range dag.Edges[id] {
			if depth[dep] > maxDep {
				maxDep = depth[dep]
			}
		}
		depth[id] = maxDep + 1
	}

	// Find max level.
	maxLevel := 0
	for _, d := range depth {
		if d > maxLevel {
			maxLevel = d
		}
	}

	// Group steps by level.
	levels := make([][]string, maxLevel+1)
	for _, id := range dag.Sorted {
		d := depth[id]
		levels[d] = append(levels[d], id)
	}

	return levels
}

// validateStepConfig checks type-specific constraints on a step definition.
// Prevents invalid configurations like loops without stop conditions.
func validateStepConfig(step *schema.StepDefinition) error {
	switch step.Type {
	case schema.StepTypeLoop:
		if len(step.Config) == 0 {
			return schema.NewErrorf(schema.ErrCodeValidation, "loop step %s has no config", step.ID)
		}
		var cfg schema.LoopConfig
		if err := json.Unmarshal(step.Config, &cfg); err != nil {
			return schema.NewErrorf(schema.ErrCodeValidation, "loop step %s has invalid config: %v", step.ID, err)
		}
		if cfg.MaxIter <= 0 && cfg.Condition == "" {
			return schema.NewErrorf(schema.ErrCodeValidation, "loop step %s must have max_iter > 0 or a condition to prevent infinite loops", step.ID)
		}
		if len(cfg.Body) == 0 {
			return schema.NewErrorf(schema.ErrCodeValidation, "loop step %s has empty body", step.ID)
		}

	case schema.StepTypeParallel:
		if len(step.Config) == 0 {
			return schema.NewErrorf(schema.ErrCodeValidation, "parallel step %s has no config", step.ID)
		}
		var cfg schema.ParallelConfig
		if err := json.Unmarshal(step.Config, &cfg); err != nil {
			return schema.NewErrorf(schema.ErrCodeValidation, "parallel step %s has invalid config: %v", step.ID, err)
		}
		if len(cfg.Branches) == 0 {
			return schema.NewErrorf(schema.ErrCodeValidation, "parallel step %s has no branches", step.ID)
		}

	case schema.StepTypeCondition:
		if len(step.Config) == 0 {
			return schema.NewErrorf(schema.ErrCodeValidation, "condition step %s has no config", step.ID)
		}
		var cfg schema.ConditionConfig
		if err := json.Unmarshal(step.Config, &cfg); err != nil {
			return schema.NewErrorf(schema.ErrCodeValidation, "condition step %s has invalid config: %v", step.ID, err)
		}
		if cfg.Expression == "" {
			return schema.NewErrorf(schema.ErrCodeValidation, "condition step %s has no expression", step.ID)
		}

	case schema.StepTypeReasoning:
		if len(step.Config) == 0 {
			return schema.NewErrorf(schema.ErrCodeValidation, "reasoning step %s has no config", step.ID)
		}
		var cfg schema.ReasoningConfig
		if err := json.Unmarshal(step.Config, &cfg); err != nil {
			return schema.NewErrorf(schema.ErrCodeValidation, "reasoning step %s has invalid config: %v", step.ID, err)
		}
		// Options can be empty for free-form reasoning.

	case schema.StepTypeWait:
		if len(step.Config) == 0 {
			return schema.NewErrorf(schema.ErrCodeValidation, "wait step %s has no config", step.ID)
		}
		var cfg schema.WaitConfig
		if err := json.Unmarshal(step.Config, &cfg); err != nil {
			return schema.NewErrorf(schema.ErrCodeValidation, "wait step %s has invalid config: %v", step.ID, err)
		}
		if cfg.Duration == "" && cfg.Signal == "" {
			return schema.NewErrorf(schema.ErrCodeValidation, "wait step %s must have duration or signal", step.ID)
		}

	case schema.StepTypeAction:
		if step.Action == "" {
			return schema.NewErrorf(schema.ErrCodeValidation, "action step %s has no action name", step.ID)
		}
	}

	return nil
}

// sortStrings sorts a slice of strings in-place using insertion sort.
// Used for small slices to avoid importing sort package.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}
