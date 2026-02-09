package expressions

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ScopeBuilder tests ---

func TestScopeBuilder_NewScopeBuilder(t *testing.T) {
	inputs := map[string]any{"user": "alice"}
	wf := map[string]any{"run_id": "wf-1"}
	ctx := map[string]any{"intent": "deploy"}

	sb := NewScopeBuilder(inputs, wf, ctx)
	require.NotNil(t, sb)

	scope := sb.Build()
	assert.Equal(t, "alice", scope.Inputs["user"])
	assert.Equal(t, "wf-1", scope.Workflow["run_id"])
	assert.Equal(t, "deploy", scope.Context["intent"])
	assert.Empty(t, scope.Steps)
	assert.Nil(t, scope.Loop)
}

func TestScopeBuilder_NilInputs(t *testing.T) {
	sb := NewScopeBuilder(nil, nil, nil)
	scope := sb.Build()
	assert.Nil(t, scope.Inputs)
	assert.Nil(t, scope.Workflow)
	assert.Nil(t, scope.Context)
}

func TestScopeBuilder_AddStepOutput(t *testing.T) {
	sb := NewScopeBuilder(nil, nil, nil)

	output := json.RawMessage(`{"url":"https://api.example.com","status":200}`)
	err := sb.AddStepOutput("fetch", output)
	require.NoError(t, err)

	scope := sb.Build()
	fetchOut, ok := scope.Steps["fetch"]
	require.True(t, ok)
	m, ok := fetchOut.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "https://api.example.com", m["url"])
	assert.Equal(t, float64(200), m["status"])
}

func TestScopeBuilder_AddStepOutput_Empty(t *testing.T) {
	sb := NewScopeBuilder(nil, nil, nil)
	err := sb.AddStepOutput("empty", nil)
	require.NoError(t, err)

	scope := sb.Build()
	_, exists := scope.Steps["empty"]
	assert.True(t, exists)
}

func TestScopeBuilder_StepOutputImmutable(t *testing.T) {
	sb := NewScopeBuilder(nil, nil, nil)

	err := sb.AddStepOutput("fetch", json.RawMessage(`{"url":"v1"}`))
	require.NoError(t, err)

	// Second add of same step ID must fail.
	err = sb.AddStepOutput("fetch", json.RawMessage(`{"url":"v2"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "immutable")

	// Verify the original value is preserved.
	scope := sb.Build()
	m := scope.Steps["fetch"].(map[string]any)
	assert.Equal(t, "v1", m["url"])
}

func TestScopeBuilder_FrozenOnInsert(t *testing.T) {
	sb := NewScopeBuilder(nil, nil, nil)

	original := map[string]any{"key": "original"}
	b, _ := json.Marshal(original)
	err := sb.AddStepOutput("s1", b)
	require.NoError(t, err)

	// Mutate the original map — should not affect the scope.
	original["key"] = "mutated"

	scope := sb.Build()
	m := scope.Steps["s1"].(map[string]any)
	assert.Equal(t, "original", m["key"])
}

func TestScopeBuilder_BuildReturnsCopy(t *testing.T) {
	sb := NewScopeBuilder(nil, nil, nil)
	_ = sb.AddStepOutput("s1", json.RawMessage(`{"k":"v"}`))

	scope1 := sb.Build()
	scope2 := sb.Build()

	// Mutating scope1.Steps must not affect scope2.
	scope1.Steps["s1"] = "tampered"
	m := scope2.Steps["s1"].(map[string]any)
	assert.Equal(t, "v", m["k"])
}

func TestScopeBuilder_InputsImmutableFromExternal(t *testing.T) {
	inputs := map[string]any{"key": "original"}
	sb := NewScopeBuilder(inputs, nil, nil)

	// Mutate the original inputs map.
	inputs["key"] = "mutated"

	scope := sb.Build()
	assert.Equal(t, "original", scope.Inputs["key"])
}

// --- Loop variable scoping ---

func TestScopeBuilder_WithLoopVars(t *testing.T) {
	sb := NewScopeBuilder(
		map[string]any{"name": "test"},
		nil, nil,
	)
	_ = sb.AddStepOutput("s1", json.RawMessage(`{"data":"hello"}`))

	child := sb.WithLoopVars("item-value", 2)
	scope := child.Build()

	// Loop vars present.
	require.NotNil(t, scope.Loop)
	assert.Equal(t, "item-value", scope.Loop.Item)
	assert.Equal(t, 2, scope.Loop.Index)

	// Parent data still accessible.
	assert.Equal(t, "test", scope.Inputs["name"])
	m := scope.Steps["s1"].(map[string]any)
	assert.Equal(t, "hello", m["data"])
}

func TestScopeBuilder_LoopVarsScoped(t *testing.T) {
	sb := NewScopeBuilder(nil, nil, nil)

	// Parent has no loop.
	parentScope := sb.Build()
	assert.Nil(t, parentScope.Loop)

	// Child iteration 0.
	child0 := sb.WithLoopVars("a", 0)
	scope0 := child0.Build()
	assert.Equal(t, "a", scope0.Loop.Item)
	assert.Equal(t, 0, scope0.Loop.Index)

	// Child iteration 1.
	child1 := sb.WithLoopVars("b", 1)
	scope1 := child1.Build()
	assert.Equal(t, "b", scope1.Loop.Item)
	assert.Equal(t, 1, scope1.Loop.Index)

	// Parent still has no loop.
	parentScope = sb.Build()
	assert.Nil(t, parentScope.Loop)
}

func TestScopeBuilder_LoopVarsObjectItem(t *testing.T) {
	sb := NewScopeBuilder(nil, nil, nil)
	item := map[string]any{"name": "alice", "age": float64(30)}
	child := sb.WithLoopVars(item, 0)
	scope := child.Build()

	require.NotNil(t, scope.Loop)
	m, ok := scope.Loop.Item.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "alice", m["name"])

	// Mutating original doesn't affect scoped copy.
	item["name"] = "bob"
	scope2 := child.Build()
	m2 := scope2.Loop.Item.(map[string]any)
	assert.Equal(t, "alice", m2["name"])
}

// --- Parallel branch isolation ---

func TestScopeBuilder_ForParallelBranch(t *testing.T) {
	sb := NewScopeBuilder(map[string]any{"input": "shared"}, nil, nil)
	_ = sb.AddStepOutput("s1", json.RawMessage(`{"data":"parent"}`))

	branch := sb.ForParallelBranch()

	// Branch sees parent's steps.
	branchScope := branch.Build()
	m := branchScope.Steps["s1"].(map[string]any)
	assert.Equal(t, "parent", m["data"])

	// Branch adds its own step.
	err := branch.AddStepOutput("branch_step", json.RawMessage(`{"result":"branch-data"}`))
	require.NoError(t, err)

	// Branch sees its own step.
	branchScope = branch.Build()
	_, exists := branchScope.Steps["branch_step"]
	assert.True(t, exists)

	// Parent does NOT see branch's step.
	parentScope := sb.Build()
	_, exists = parentScope.Steps["branch_step"]
	assert.False(t, exists)
}

func TestScopeBuilder_ParallelBranchIsolation(t *testing.T) {
	sb := NewScopeBuilder(nil, nil, nil)

	branch1 := sb.ForParallelBranch()
	branch2 := sb.ForParallelBranch()

	_ = branch1.AddStepOutput("b1_step", json.RawMessage(`{"v":1}`))
	_ = branch2.AddStepOutput("b2_step", json.RawMessage(`{"v":2}`))

	// branch1 cannot see branch2's step.
	scope1 := branch1.Build()
	_, exists := scope1.Steps["b2_step"]
	assert.False(t, exists)

	// branch2 cannot see branch1's step.
	scope2 := branch2.Build()
	_, exists = scope2.Steps["b1_step"]
	assert.False(t, exists)
}

func TestScopeBuilder_MergeBranchOutputs(t *testing.T) {
	sb := NewScopeBuilder(nil, nil, nil)
	_ = sb.AddStepOutput("s1", json.RawMessage(`{"existing":"yes"}`))

	branch := sb.ForParallelBranch()
	_ = branch.AddStepOutput("b_step", json.RawMessage(`{"result":"merged"}`))

	sb.MergeBranchOutputs(branch)

	scope := sb.Build()
	_, exists := scope.Steps["b_step"]
	assert.True(t, exists)

	// Existing step preserved.
	m := scope.Steps["s1"].(map[string]any)
	assert.Equal(t, "yes", m["existing"])
}

func TestScopeBuilder_MergeBranchDoesNotOverwrite(t *testing.T) {
	sb := NewScopeBuilder(nil, nil, nil)
	_ = sb.AddStepOutput("shared", json.RawMessage(`{"v":"parent"}`))

	branch := sb.ForParallelBranch()
	// Branch already inherited "shared" from parent snapshot.
	// MergeBranchOutputs should NOT overwrite parent's "shared".
	sb.MergeBranchOutputs(branch)

	scope := sb.Build()
	m := scope.Steps["shared"].(map[string]any)
	assert.Equal(t, "parent", m["v"])
}

// --- StepOutputs ---

func TestScopeBuilder_StepOutputs(t *testing.T) {
	sb := NewScopeBuilder(nil, nil, nil)
	_ = sb.AddStepOutput("a", json.RawMessage(`{"x":1}`))
	_ = sb.AddStepOutput("b", json.RawMessage(`{"y":2}`))

	outputs := sb.StepOutputs()
	assert.Len(t, outputs, 2)

	// Mutating returned map shouldn't affect internal state.
	outputs["c"] = "injected"
	assert.Len(t, sb.StepOutputs(), 2)
}

// --- Loop interpolation integration ---

func TestInterpolator_LoopItem(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := &InterpolationScope{
		Loop: &LoopScope{Item: "current-value", Index: 3},
	}

	raw := json.RawMessage(`{"item":"${{loop.item}}","idx":"${{loop.index}}"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	assert.Contains(t, string(result), `"item":"current-value"`)
	assert.Contains(t, string(result), `"idx":"3"`)
}

func TestInterpolator_LoopItemNested(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := &InterpolationScope{
		Loop: &LoopScope{
			Item:  map[string]any{"name": "alice", "email": "alice@example.com"},
			Index: 0,
		},
	}

	raw := json.RawMessage(`{"name":"${{loop.item.name}}","email":"${{loop.item.email}}"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)
	assert.Contains(t, string(result), `"name":"alice"`)
	assert.Contains(t, string(result), `"email":"alice@example.com"`)
}

func TestInterpolator_LoopOutsideContext(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := &InterpolationScope{} // no Loop

	raw := json.RawMessage(`{"x":"${{loop.item}}"}`)
	_, err := interp.Resolve(context.Background(), raw, scope)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside of a loop")
}

func TestInterpolator_LoopUnknownField(t *testing.T) {
	interp := NewInterpolator(nil)
	scope := &InterpolationScope{
		Loop: &LoopScope{Item: "x", Index: 0},
	}

	raw := json.RawMessage(`{"x":"${{loop.unknown}}"}`)
	_, err := interp.Resolve(context.Background(), raw, scope)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown loop field")
}

func TestInterpolator_LoopInvalidPath(t *testing.T) {
	interp := NewInterpolator(nil)
	raw := json.RawMessage(`{"x":"${{loop.}}"}`)

	_, err := interp.Resolve(context.Background(), raw, &InterpolationScope{
		Loop: &LoopScope{Item: "x", Index: 0},
	})
	require.Error(t, err)
}

// --- Deep copy ---

func TestDeepCopyMap(t *testing.T) {
	original := map[string]any{
		"a": "hello",
		"b": map[string]any{"nested": float64(42)},
		"c": []any{"x", "y"},
	}

	copied := deepCopyMap(original)

	// Modify original.
	original["a"] = "mutated"
	original["b"].(map[string]any)["nested"] = float64(99)
	original["c"].([]any)[0] = "z"

	// Copy unaffected.
	assert.Equal(t, "hello", copied["a"])
	assert.Equal(t, float64(42), copied["b"].(map[string]any)["nested"])
	assert.Equal(t, "x", copied["c"].([]any)[0])
}

func TestDeepCopyMap_Nil(t *testing.T) {
	assert.Nil(t, deepCopyMap(nil))
}

func TestDeepCopyAny_RawMessage(t *testing.T) {
	orig := json.RawMessage(`{"test":true}`)
	copied := deepCopyAny(orig).(json.RawMessage)

	// Modify original.
	orig[0] = '['

	assert.Equal(t, byte('{'), copied[0])
}

func TestDeepCopyAny_Primitives(t *testing.T) {
	assert.Equal(t, "hello", deepCopyAny("hello"))
	assert.Equal(t, float64(42), deepCopyAny(float64(42)))
	assert.Equal(t, true, deepCopyAny(true))
	assert.Nil(t, deepCopyAny(nil))
}

// --- End-to-end: ScopeBuilder + Interpolator ---

func TestScopeBuilder_EndToEnd_WithInterpolator(t *testing.T) {
	sb := NewScopeBuilder(
		map[string]any{"base_url": "https://api.example.com"},
		map[string]any{"run_id": "wf-123"},
		map[string]any{"intent": "process"},
	)

	// Level 0 step completes.
	_ = sb.AddStepOutput("fetch", json.RawMessage(`{"token":"abc123","items":[1,2,3]}`))

	// Level 1 step references level 0 output.
	interp := NewInterpolator(nil)
	scope := sb.Build()

	raw := json.RawMessage(`{"url":"${{inputs.base_url}}/data","auth":"${{steps.fetch.output.token}}","wf":"${{workflow.run_id}}"}`)
	result, err := interp.Resolve(context.Background(), raw, scope)
	require.NoError(t, err)

	assert.Contains(t, string(result), `"url":"https://api.example.com/data"`)
	assert.Contains(t, string(result), `"auth":"abc123"`)
	assert.Contains(t, string(result), `"wf":"wf-123"`)
}

func TestScopeBuilder_EndToEnd_LoopIteration(t *testing.T) {
	sb := NewScopeBuilder(
		map[string]any{"prefix": "item"},
		nil, nil,
	)

	items := []any{
		map[string]any{"id": "a", "name": "alpha"},
		map[string]any{"id": "b", "name": "beta"},
	}

	interp := NewInterpolator(nil)

	for i, item := range items {
		child := sb.WithLoopVars(item, i)
		scope := child.Build()

		raw := json.RawMessage(`{"label":"${{inputs.prefix}}-${{loop.item.name}}","idx":"${{loop.index}}"}`)
		result, err := interp.Resolve(context.Background(), raw, scope)
		require.NoError(t, err)

		s := string(result)
		m := item.(map[string]any)
		assert.Contains(t, s, `"label":"item-`+m["name"].(string)+`"`)
	}
}
