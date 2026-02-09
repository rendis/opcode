package expressions

import (
	"encoding/json"
	"sync"

	"github.com/rendis/opcode/pkg/schema"
)

// ScopeBuilder constructs InterpolationScopes with proper variable isolation.
// It enforces:
//   - Step outputs are immutable after completion (frozen on insert).
//   - Append-only: new step outputs are added after each DAG level completes.
//   - Loop variables ($item, $index) are scoped per iteration.
//   - Parallel branch variables are isolated from sibling branches.
//   - Resolution order: step local -> workflow inputs -> secrets.
type ScopeBuilder struct {
	mu       sync.RWMutex
	steps    map[string]any // step ID -> frozen output (deep-copied on insert)
	inputs   map[string]any // workflow input params (immutable after init)
	workflow map[string]any // workflow metadata (immutable after init)
	context  map[string]any // workflow context (immutable after init)

	// loop holds the current loop iteration variables.
	// nil when not inside a loop.
	loop *LoopVars
}

// LoopVars holds the scoped variables for a single loop iteration.
type LoopVars struct {
	Item  any // current iteration value
	Index int // current iteration index
}

// NewScopeBuilder creates a ScopeBuilder initialized with workflow-level data.
// inputs, workflow, and context are deep-copied to prevent external mutation.
func NewScopeBuilder(inputs, workflow, context map[string]any) *ScopeBuilder {
	return &ScopeBuilder{
		steps:    make(map[string]any),
		inputs:   deepCopyMap(inputs),
		workflow: deepCopyMap(workflow),
		context:  deepCopyMap(context),
	}
}

// AddStepOutput registers a completed step's output. The output is frozen
// (deep-copied) at the time of insertion. Subsequent calls with the same stepID
// are rejected -- step outputs are immutable after completion.
func (sb *ScopeBuilder) AddStepOutput(stepID string, output json.RawMessage) error {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	if _, exists := sb.steps[stepID]; exists {
		return schema.NewErrorf(schema.ErrCodeInterpolation,
			"step %q output already registered; step outputs are immutable after completion", stepID)
	}

	if len(output) == 0 {
		sb.steps[stepID] = nil
		return nil
	}

	var parsed any
	if err := json.Unmarshal(output, &parsed); err != nil {
		return schema.NewErrorf(schema.ErrCodeInterpolation,
			"cannot parse step %q output: %s", stepID, err.Error())
	}

	// Deep-copy to freeze the value.
	sb.steps[stepID] = deepCopyAny(parsed)
	return nil
}

// Build creates an InterpolationScope snapshot. The returned scope is safe
// for concurrent use (all data is copied). If loop vars are set, they are
// included under the "loop" namespace.
func (sb *ScopeBuilder) Build() *InterpolationScope {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	scope := &InterpolationScope{
		Steps:    deepCopyMap(sb.steps),
		Inputs:   sb.inputs,   // already frozen at init
		Workflow: sb.workflow,  // already frozen at init
		Context:  sb.context,  // already frozen at init
	}

	// Inject loop variables if inside a loop iteration.
	if sb.loop != nil {
		if scope.Context == nil {
			scope.Context = make(map[string]any)
		}
		// Loop vars available as ${{loop.item}} and ${{loop.index}}.
		// Stored in a dedicated "loop" key within the scope.
		scope.Loop = &LoopScope{
			Item:  deepCopyAny(sb.loop.Item),
			Index: sb.loop.Index,
		}
	}

	return scope
}

// WithLoopVars returns a child ScopeBuilder with loop-scoped variables.
// The child shares the same steps/inputs/workflow/context but has its own
// loop vars. This ensures loop vars are scoped to the iteration.
func (sb *ScopeBuilder) WithLoopVars(item any, index int) *ScopeBuilder {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	return &ScopeBuilder{
		steps:    sb.steps,    // shared (append-only, safe)
		inputs:   sb.inputs,   // shared (immutable)
		workflow: sb.workflow,  // shared (immutable)
		context:  sb.context,  // shared (immutable)
		loop: &LoopVars{
			Item:  deepCopyAny(item),
			Index: index,
		},
	}
}

// ForParallelBranch returns a child ScopeBuilder for a parallel branch.
// The child gets a snapshot of current step outputs but has its own isolated
// step output map. Branch-local step completions do NOT leak to siblings.
func (sb *ScopeBuilder) ForParallelBranch() *ScopeBuilder {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	return &ScopeBuilder{
		steps:    deepCopyMap(sb.steps), // isolated copy
		inputs:   sb.inputs,              // shared (immutable)
		workflow: sb.workflow,             // shared (immutable)
		context:  sb.context,             // shared (immutable)
	}
}

// MergeBranchOutputs merges completed step outputs from a parallel branch
// back into the parent scope. Only new step IDs are added; existing ones
// are preserved (immutability rule).
func (sb *ScopeBuilder) MergeBranchOutputs(branch *ScopeBuilder) {
	branch.mu.RLock()
	branchSteps := branch.steps
	branch.mu.RUnlock()

	sb.mu.Lock()
	defer sb.mu.Unlock()

	for stepID, output := range branchSteps {
		if _, exists := sb.steps[stepID]; !exists {
			sb.steps[stepID] = deepCopyAny(output)
		}
	}
}

// StepOutputs returns a read-only copy of the current step outputs.
func (sb *ScopeBuilder) StepOutputs() map[string]any {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return deepCopyMap(sb.steps)
}

// --- Deep copy utilities ---

// deepCopyMap creates a deep copy of a map[string]any.
func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = deepCopyAny(v)
	}
	return cp
}

// deepCopyAny recursively deep-copies a value.
// Handles maps, slices, and primitives (which are inherently immutable).
func deepCopyAny(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopyMap(val)
	case []any:
		cp := make([]any, len(val))
		for i, item := range val {
			cp[i] = deepCopyAny(item)
		}
		return cp
	case json.RawMessage:
		if val == nil {
			return nil
		}
		cp := make(json.RawMessage, len(val))
		copy(cp, val)
		return cp
	default:
		// Primitives (string, float64, bool, nil, int, int64) are value types.
		return v
	}
}
