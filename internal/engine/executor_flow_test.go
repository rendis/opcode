package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rendis/opcode/internal/actions"
	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test helpers ---

func flowConditionStep(id string, expression string, branches map[string][]schema.StepDefinition, defaultBranch []schema.StepDefinition, deps ...string) schema.StepDefinition {
	cfg := schema.ConditionConfig{
		Expression: expression,
		Branches:   branches,
		Default:    defaultBranch,
	}
	cfgJSON, _ := json.Marshal(cfg)
	return schema.StepDefinition{
		ID:        id,
		Type:      schema.StepTypeCondition,
		Config:    cfgJSON,
		DependsOn: deps,
	}
}

func flowLoopStep(id string, cfg schema.LoopConfig, deps ...string) schema.StepDefinition {
	cfgJSON, _ := json.Marshal(cfg)
	return schema.StepDefinition{
		ID:        id,
		Type:      schema.StepTypeLoop,
		Config:    cfgJSON,
		DependsOn: deps,
	}
}

func flowParallelStep(id string, branches [][]schema.StepDefinition, mode string, deps ...string) schema.StepDefinition {
	cfg := schema.ParallelConfig{
		Branches: branches,
		Mode:     mode,
	}
	cfgJSON, _ := json.Marshal(cfg)
	return schema.StepDefinition{
		ID:        id,
		Type:      schema.StepTypeParallel,
		Config:    cfgJSON,
		DependsOn: deps,
	}
}

func flowWaitStep(id string, cfg schema.WaitConfig, deps ...string) schema.StepDefinition {
	cfgJSON, _ := json.Marshal(cfg)
	return schema.StepDefinition{
		ID:        id,
		Type:      schema.StepTypeWait,
		Config:    cfgJSON,
		DependsOn: deps,
	}
}

func flowActionStep(id, actionName string) schema.StepDefinition {
	return schema.StepDefinition{
		ID:     id,
		Type:   schema.StepTypeAction,
		Action: actionName,
	}
}

func flowActionStepWithParams(id, actionName string, params map[string]any) schema.StepDefinition {
	paramsJSON, _ := json.Marshal(params)
	return schema.StepDefinition{
		ID:     id,
		Type:   schema.StepTypeAction,
		Action: actionName,
		Params: paramsJSON,
	}
}

// parseOutput is a helper to unmarshal step output JSON.
func parseOutput(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

// ============================================================
// 016a: Condition tests
// ============================================================

func TestFlowCondition_TrueBranch(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "on_true", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		return &actions.ActionOutput{Data: json.RawMessage(`{"result":"yes"}`)}, nil
	}})

	step := flowConditionStep("cond1", "true",
		map[string][]schema.StepDefinition{
			"true":  {flowActionStep("do_true", "on_true")},
			"false": {flowActionStep("do_false", "on_true")},
		}, nil)

	wf := newWorkflow("wf-cond-true", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["cond1"].Output)
	assert.Equal(t, "true", out["branch"])
}

func TestFlowCondition_FalseBranch(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "on_false", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		return &actions.ActionOutput{Data: json.RawMessage(`{"result":"no"}`)}, nil
	}})

	step := flowConditionStep("cond1", "false",
		map[string][]schema.StepDefinition{
			"true":  {flowActionStep("do_true", "on_false")},
			"false": {flowActionStep("do_false", "on_false")},
		}, nil)

	wf := newWorkflow("wf-cond-false", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["cond1"].Output)
	assert.Equal(t, "false", out["branch"])
}

func TestFlowCondition_DefaultBranch(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "fallback", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		return &actions.ActionOutput{Data: json.RawMessage(`{"result":"default"}`)}, nil
	}})

	step := flowConditionStep("cond1", `"unknown_value"`,
		map[string][]schema.StepDefinition{
			"yes": {flowActionStep("do_yes", "fallback")},
		},
		[]schema.StepDefinition{flowActionStep("do_default", "fallback")},
	)

	wf := newWorkflow("wf-cond-default", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["cond1"].Output)
	assert.Equal(t, "default", out["branch"])
}

func TestFlowCondition_NoMatchingBranch_Skip(t *testing.T) {
	te := newTestEnv()

	step := flowConditionStep("cond1", `"nonexistent"`,
		map[string][]schema.StepDefinition{
			"a": {flowActionStep("do_a", "noop")},
		}, nil)

	wf := newWorkflow("wf-cond-skip", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["cond1"].Output)
	assert.Equal(t, true, out["skipped"])
}

func TestFlowCondition_StringExpressionResult(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "noop"})

	step := flowConditionStep("cond1", `"route_b"`,
		map[string][]schema.StepDefinition{
			"route_a": {flowActionStep("do_a", "noop")},
			"route_b": {flowActionStep("do_b", "noop")},
		}, nil)

	wf := newWorkflow("wf-cond-str", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["cond1"].Output)
	assert.Equal(t, "route_b", out["branch"])
}

func TestFlowCondition_NumericExpressionResult(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "noop"})

	step := flowConditionStep("cond1", `1 + 1`,
		map[string][]schema.StepDefinition{
			"2": {flowActionStep("do_two", "noop")},
			"3": {flowActionStep("do_three", "noop")},
		}, nil)

	wf := newWorkflow("wf-cond-num", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["cond1"].Output)
	assert.Equal(t, "2", out["branch"])
}

func TestFlowCondition_CELError(t *testing.T) {
	te := newTestEnv()

	step := flowConditionStep("cond1", `nonexistent_var.field`,
		map[string][]schema.StepDefinition{
			"true": {flowActionStep("do_true", "noop")},
		}, nil)

	wf := newWorkflow("wf-cond-err", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusFailed, result.Status)
}

func TestFlowCondition_NestedCondition(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "inner_action", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		return &actions.ActionOutput{Data: json.RawMessage(`{"deep":true}`)}, nil
	}})

	innerCondition := flowConditionStep("inner_cond", "true",
		map[string][]schema.StepDefinition{
			"true": {flowActionStep("leaf", "inner_action")},
		}, nil)

	outerCondition := flowConditionStep("outer_cond", "true",
		map[string][]schema.StepDefinition{
			"true": {innerCondition},
		}, nil)

	wf := newWorkflow("wf-nested-cond", outerCondition)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestFlowCondition_SubStepFailure(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "fail_action", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		return nil, fmt.Errorf("sub-step boom")
	}})

	step := flowConditionStep("cond1", "true",
		map[string][]schema.StepDefinition{
			"true": {flowActionStep("do_fail", "fail_action")},
		}, nil)

	wf := newWorkflow("wf-cond-subfail", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusFailed, result.Status)
}

func TestFlowCondition_SubStepOutputsVisible(t *testing.T) {
	te := newTestEnv()

	var callCount int32
	te.registry.Register(&mockAction{name: "seq_action", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		n := atomic.AddInt32(&callCount, 1)
		return &actions.ActionOutput{Data: json.RawMessage(fmt.Sprintf(`{"call":%d}`, n))}, nil
	}})

	step := flowConditionStep("cond1", "true",
		map[string][]schema.StepDefinition{
			"true": {
				flowActionStep("first", "seq_action"),
				flowActionStep("second", "seq_action"),
			},
		}, nil)

	wf := newWorkflow("wf-cond-seq", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	// Both sub-steps should have been executed.
	assert.Equal(t, int32(2), atomic.LoadInt32(&callCount))
}

func TestFlowCondition_ContextCancellation(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "slow_action", execFn: func(ctx context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		select {
		case <-time.After(10 * time.Second):
			return &actions.ActionOutput{Data: json.RawMessage(`{"ok":true}`)}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}})

	step := flowConditionStep("cond1", "true",
		map[string][]schema.StepDefinition{
			"true": {flowActionStep("slow", "slow_action")},
		}, nil)

	wf := newWorkflow("wf-cond-cancel", step)
	te.store.CreateWorkflow(context.Background(), wf)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := te.executor.Run(ctx, wf, nil)
	require.NoError(t, err)
	// Should be failed or cancelled due to timeout.
	assert.NotEqual(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestFlowCondition_EmptyBranchBody(t *testing.T) {
	te := newTestEnv()

	step := flowConditionStep("cond1", "true",
		map[string][]schema.StepDefinition{
			"true": {},
		}, nil)

	wf := newWorkflow("wf-cond-empty", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["cond1"].Output)
	assert.Equal(t, "true", out["branch"])
}

func TestFlowCondition_EventEmission(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "noop"})

	step := flowConditionStep("cond1", "true",
		map[string][]schema.StepDefinition{
			"true": {flowActionStep("do_true", "noop")},
		}, nil)

	wf := newWorkflow("wf-cond-events", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	// Check that condition_evaluated event was emitted.
	events, _ := te.eventLog.GetEventsByType(context.Background(), schema.EventConditionEvaluated, eventFilterForWF("wf-cond-events"))
	assert.GreaterOrEqual(t, len(events), 1)
}

func TestFlowCondition_InputBasedBranching(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "noop"})

	step := flowConditionStep("cond1", `inputs.mode`,
		map[string][]schema.StepDefinition{
			"fast": {flowActionStep("do_fast", "noop")},
			"slow": {flowActionStep("do_slow", "noop")},
		}, nil)

	wf := newWorkflow("wf-cond-inputs", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, map[string]any{"mode": "fast"})
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["cond1"].Output)
	assert.Equal(t, "fast", out["branch"])
}

// ============================================================
// 016b: Loop tests
// ============================================================

func TestFlowLoop_ForEach_IterateArray(t *testing.T) {
	te := newTestEnv()

	var items []string
	te.registry.Register(&mockAction{name: "collect", execFn: func(_ context.Context, input actions.ActionInput) (*actions.ActionOutput, error) {
		// The action just echoes back its params.
		data, _ := json.Marshal(input.Params)
		return &actions.ActionOutput{Data: data}, nil
	}})

	cfg := schema.LoopConfig{
		Mode:    "for_each",
		Over:    `["a","b","c"]`,
		Body:    []schema.StepDefinition{flowActionStep("process", "collect")},
		MaxIter: 10,
	}

	step := flowLoopStep("loop1", cfg)
	wf := newWorkflow("wf-loop-foreach", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["loop1"].Output)
	assert.Equal(t, float64(3), out["iterations"])

	_ = items // suppress unused
}

func TestFlowLoop_ForEach_EmptyArray(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "noop"})

	cfg := schema.LoopConfig{
		Mode:    "for_each",
		Over:    `[]`,
		Body:    []schema.StepDefinition{flowActionStep("process", "noop")},
		MaxIter: 10,
	}

	step := flowLoopStep("loop1", cfg)
	wf := newWorkflow("wf-loop-empty", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["loop1"].Output)
	assert.Equal(t, float64(0), out["iterations"])
}

func TestFlowLoop_ForEach_CELError(t *testing.T) {
	te := newTestEnv()

	cfg := schema.LoopConfig{
		Mode:    "for_each",
		Over:    `undefined_var.items`,
		Body:    []schema.StepDefinition{flowActionStep("process", "noop")},
		MaxIter: 10,
	}

	step := flowLoopStep("loop1", cfg)
	wf := newWorkflow("wf-loop-cel-err", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusFailed, result.Status)
}

func TestFlowLoop_While_ConditionTrue(t *testing.T) {
	te := newTestEnv()

	var counter int32
	te.registry.Register(&mockAction{name: "incr", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		n := atomic.AddInt32(&counter, 1)
		return &actions.ActionOutput{Data: json.RawMessage(fmt.Sprintf(`{"count":%d}`, n))}, nil
	}})

	cfg := schema.LoopConfig{
		Mode:      "while",
		Condition: "true",
		Body:      []schema.StepDefinition{flowActionStep("incr_step", "incr")},
		MaxIter:   5,
	}

	step := flowLoopStep("loop1", cfg)
	wf := newWorkflow("wf-loop-while", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["loop1"].Output)
	assert.Equal(t, float64(5), out["iterations"])
}

func TestFlowLoop_While_StartsFalse(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "noop"})

	cfg := schema.LoopConfig{
		Mode:      "while",
		Condition: "false",
		Body:      []schema.StepDefinition{flowActionStep("step", "noop")},
		MaxIter:   10,
	}

	step := flowLoopStep("loop1", cfg)
	wf := newWorkflow("wf-loop-while-false", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["loop1"].Output)
	assert.Equal(t, float64(0), out["iterations"])
}

func TestFlowLoop_Until_ExecutesBodyAtLeastOnce(t *testing.T) {
	te := newTestEnv()

	var execCount int32
	te.registry.Register(&mockAction{name: "once", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		atomic.AddInt32(&execCount, 1)
		return &actions.ActionOutput{Data: json.RawMessage(`{"ok":true}`)}, nil
	}})

	cfg := schema.LoopConfig{
		Mode:      "until",
		Condition: "true", // condition is true on first check → stop after 1 iteration
		Body:      []schema.StepDefinition{flowActionStep("step", "once")},
		MaxIter:   10,
	}

	step := flowLoopStep("loop1", cfg)
	wf := newWorkflow("wf-loop-until", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["loop1"].Output)
	assert.Equal(t, float64(1), out["iterations"])
	assert.Equal(t, int32(1), atomic.LoadInt32(&execCount))
}

func TestFlowLoop_MaxIter_StopsAtLimit(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "noop"})

	cfg := schema.LoopConfig{
		Mode:    "for_each",
		Over:    `[1,2,3,4,5,6,7,8,9,10]`,
		Body:    []schema.StepDefinition{flowActionStep("step", "noop")},
		MaxIter: 3,
	}

	step := flowLoopStep("loop1", cfg)
	wf := newWorkflow("wf-loop-maxiter", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["loop1"].Output)
	assert.Equal(t, float64(3), out["iterations"])
}

func TestFlowLoop_NestedLoop(t *testing.T) {
	te := newTestEnv()

	var execCount int32
	te.registry.Register(&mockAction{name: "count", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		atomic.AddInt32(&execCount, 1)
		return &actions.ActionOutput{Data: json.RawMessage(`{"ok":true}`)}, nil
	}})

	innerLoop := flowLoopStep("inner", schema.LoopConfig{
		Mode:    "for_each",
		Over:    `[1,2]`,
		Body:    []schema.StepDefinition{flowActionStep("leaf", "count")},
		MaxIter: 10,
	})

	outerLoop := flowLoopStep("outer", schema.LoopConfig{
		Mode:    "for_each",
		Over:    `[1,2,3]`,
		Body:    []schema.StepDefinition{innerLoop},
		MaxIter: 10,
	})

	wf := newWorkflow("wf-nested-loop", outerLoop)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	// 3 outer * 2 inner = 6 leaf executions
	assert.Equal(t, int32(6), atomic.LoadInt32(&execCount))
}

func TestFlowLoop_BodySubStepFailure(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "fail", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		return nil, fmt.Errorf("loop body error")
	}})

	cfg := schema.LoopConfig{
		Mode:    "for_each",
		Over:    `[1,2,3]`,
		Body:    []schema.StepDefinition{flowActionStep("step", "fail")},
		MaxIter: 10,
	}

	step := flowLoopStep("loop1", cfg)
	wf := newWorkflow("wf-loop-fail", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusFailed, result.Status)
}

func TestFlowLoop_ContextCancellation(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "slow", execFn: func(ctx context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		select {
		case <-time.After(10 * time.Second):
			return &actions.ActionOutput{Data: json.RawMessage(`{"ok":true}`)}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}})

	cfg := schema.LoopConfig{
		Mode:    "for_each",
		Over:    `[1,2,3,4,5]`,
		Body:    []schema.StepDefinition{flowActionStep("step", "slow")},
		MaxIter: 10,
	}

	step := flowLoopStep("loop1", cfg)
	wf := newWorkflow("wf-loop-cancel", step)
	te.store.CreateWorkflow(context.Background(), wf)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := te.executor.Run(ctx, wf, nil)
	require.NoError(t, err)
	assert.NotEqual(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestFlowLoop_WhileWithMaxIter(t *testing.T) {
	te := newTestEnv()

	var execCount int32
	te.registry.Register(&mockAction{name: "incr", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		atomic.AddInt32(&execCount, 1)
		return &actions.ActionOutput{Data: json.RawMessage(`{"ok":true}`)}, nil
	}})

	cfg := schema.LoopConfig{
		Mode:      "while",
		Condition: "true", // always true
		Body:      []schema.StepDefinition{flowActionStep("step", "incr")},
		MaxIter:   7,
	}

	step := flowLoopStep("loop1", cfg)
	wf := newWorkflow("wf-loop-while-max", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["loop1"].Output)
	assert.Equal(t, float64(7), out["iterations"])
}

func TestFlowLoop_EventEmission(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "noop"})

	cfg := schema.LoopConfig{
		Mode:    "for_each",
		Over:    `[1,2]`,
		Body:    []schema.StepDefinition{flowActionStep("step", "noop")},
		MaxIter: 10,
	}

	step := flowLoopStep("loop1", cfg)
	wf := newWorkflow("wf-loop-events", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	// Check loop events were emitted.
	iterStarted, _ := te.eventLog.GetEventsByType(context.Background(), schema.EventLoopIterStarted, eventFilterForWF("wf-loop-events"))
	iterCompleted, _ := te.eventLog.GetEventsByType(context.Background(), schema.EventLoopIterCompleted, eventFilterForWF("wf-loop-events"))
	loopCompleted, _ := te.eventLog.GetEventsByType(context.Background(), schema.EventLoopCompleted, eventFilterForWF("wf-loop-events"))

	assert.Equal(t, 2, len(iterStarted))
	assert.Equal(t, 2, len(iterCompleted))
	assert.Equal(t, 1, len(loopCompleted))
}

func TestFlowLoop_ForEach_InputBased(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "noop"})

	cfg := schema.LoopConfig{
		Mode:    "for_each",
		Over:    `inputs.items`,
		Body:    []schema.StepDefinition{flowActionStep("step", "noop")},
		MaxIter: 10,
	}

	step := flowLoopStep("loop1", cfg)
	wf := newWorkflow("wf-loop-input", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, map[string]any{
		"items": []any{"x", "y", "z"},
	})
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["loop1"].Output)
	assert.Equal(t, float64(3), out["iterations"])
}

// ============================================================
// 016c: Parallel tests
// ============================================================

func TestFlowParallel_All_TwoBranches(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "branch_a", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		return &actions.ActionOutput{Data: json.RawMessage(`{"branch":"a"}`)}, nil
	}})
	te.registry.Register(&mockAction{name: "branch_b", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		return &actions.ActionOutput{Data: json.RawMessage(`{"branch":"b"}`)}, nil
	}})

	step := flowParallelStep("par1",
		[][]schema.StepDefinition{
			{flowActionStep("a1", "branch_a")},
			{flowActionStep("b1", "branch_b")},
		}, "all")

	wf := newWorkflow("wf-par-all", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["par1"].Output)
	assert.Equal(t, "all", out["mode"])
	branches := out["branches"].([]any)
	assert.Len(t, branches, 2)
}

func TestFlowParallel_All_OneBranchFails(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "ok_action"})
	te.registry.Register(&mockAction{name: "fail_action", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		return nil, fmt.Errorf("branch failed")
	}})

	step := flowParallelStep("par1",
		[][]schema.StepDefinition{
			{flowActionStep("ok_step", "ok_action")},
			{flowActionStep("fail_step", "fail_action")},
		}, "all")

	wf := newWorkflow("wf-par-fail", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusFailed, result.Status)
}

func TestFlowParallel_Race_FirstWins(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "fast", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		return &actions.ActionOutput{Data: json.RawMessage(`{"speed":"fast"}`)}, nil
	}})
	te.registry.Register(&mockAction{name: "slow", execFn: func(ctx context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		select {
		case <-time.After(5 * time.Second):
			return &actions.ActionOutput{Data: json.RawMessage(`{"speed":"slow"}`)}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}})

	step := flowParallelStep("par1",
		[][]schema.StepDefinition{
			{flowActionStep("fast_step", "fast")},
			{flowActionStep("slow_step", "slow")},
		}, "race")

	wf := newWorkflow("wf-par-race", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["par1"].Output)
	assert.Equal(t, "race", out["mode"])
	// The fast branch (index 0) should win.
	assert.Equal(t, float64(0), out["winner"])
}

func TestFlowParallel_ContextCancellation(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "block", execFn: func(ctx context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		select {
		case <-time.After(10 * time.Second):
			return &actions.ActionOutput{Data: json.RawMessage(`{}`)}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}})

	step := flowParallelStep("par1",
		[][]schema.StepDefinition{
			{flowActionStep("s1", "block")},
			{flowActionStep("s2", "block")},
		}, "all")

	wf := newWorkflow("wf-par-cancel", step)
	te.store.CreateWorkflow(context.Background(), wf)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := te.executor.Run(ctx, wf, nil)
	require.NoError(t, err)
	assert.NotEqual(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestFlowParallel_SingleBranch(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "solo"})

	step := flowParallelStep("par1",
		[][]schema.StepDefinition{
			{flowActionStep("s1", "solo")},
		}, "all")

	wf := newWorkflow("wf-par-single", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	out := parseOutput(t, result.Steps["par1"].Output)
	branches := out["branches"].([]any)
	assert.Len(t, branches, 1)
}

func TestFlowParallel_EventEmission(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "noop"})

	step := flowParallelStep("par1",
		[][]schema.StepDefinition{
			{flowActionStep("a", "noop")},
			{flowActionStep("b", "noop")},
		}, "all")

	wf := newWorkflow("wf-par-events", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	started, _ := te.eventLog.GetEventsByType(context.Background(), schema.EventParallelStarted, eventFilterForWF("wf-par-events"))
	completed, _ := te.eventLog.GetEventsByType(context.Background(), schema.EventParallelCompleted, eventFilterForWF("wf-par-events"))
	assert.GreaterOrEqual(t, len(started), 1)
	assert.GreaterOrEqual(t, len(completed), 1)
}

func TestFlowParallel_Race_AllFail(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "fail1", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		return nil, fmt.Errorf("fail 1")
	}})
	te.registry.Register(&mockAction{name: "fail2", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		return nil, fmt.Errorf("fail 2")
	}})

	step := flowParallelStep("par1",
		[][]schema.StepDefinition{
			{flowActionStep("s1", "fail1")},
			{flowActionStep("s2", "fail2")},
		}, "race")

	wf := newWorkflow("wf-par-race-fail", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusFailed, result.Status)
}

// ============================================================
// 016c: Wait tests
// ============================================================

func TestFlowWait_Duration(t *testing.T) {
	te := newTestEnv()

	step := flowWaitStep("wait1", schema.WaitConfig{Duration: "50ms"})
	wf := newWorkflow("wf-wait-dur", step)
	te.store.CreateWorkflow(context.Background(), wf)

	start := time.Now()
	result, err := te.executor.Run(context.Background(), wf, nil)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(40))

	out := parseOutput(t, result.Steps["wait1"].Output)
	assert.NotNil(t, out["waited"])
}

func TestFlowWait_DurationContextCancellation(t *testing.T) {
	te := newTestEnv()

	step := flowWaitStep("wait1", schema.WaitConfig{Duration: "10s"})
	wf := newWorkflow("wf-wait-cancel", step)
	te.store.CreateWorkflow(context.Background(), wf)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result, err := te.executor.Run(ctx, wf, nil)
	require.NoError(t, err)
	assert.NotEqual(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestFlowWait_InvalidDuration(t *testing.T) {
	te := newTestEnv()

	step := flowWaitStep("wait1", schema.WaitConfig{Duration: "not_a_duration"})
	wf := newWorkflow("wf-wait-invalid", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusFailed, result.Status)
}

func TestFlowWait_Signal(t *testing.T) {
	te := newTestEnv()

	step := flowWaitStep("wait1", schema.WaitConfig{Signal: "continue"})
	wf := newWorkflow("wf-wait-signal", step)
	te.store.CreateWorkflow(context.Background(), wf)

	// Run in goroutine, send signal after short delay.
	done := make(chan *ExecutionResult)
	go func() {
		result, _ := te.executor.Run(context.Background(), wf, nil)
		done <- result
	}()

	// Give the executor time to start the wait step.
	time.Sleep(50 * time.Millisecond)

	// Send the signal.
	err := te.executor.Signal(context.Background(), "wf-wait-signal", schema.Signal{
		Type:    "continue",
		StepID:  "wait1",
		Payload: map[string]any{"go": true},
	})
	require.NoError(t, err)

	select {
	case result := <-done:
		assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
		out := parseOutput(t, result.Steps["wait1"].Output)
		assert.Equal(t, "continue", out["signal"])
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for signal-based wait completion")
	}
}

func TestFlowWait_SignalTimeout(t *testing.T) {
	te := newTestEnv()

	step := flowWaitStep("wait1", schema.WaitConfig{Signal: "never"})
	wf := newWorkflow("wf-wait-sig-timeout", step)
	te.store.CreateWorkflow(context.Background(), wf)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := te.executor.Run(ctx, wf, nil)
	require.NoError(t, err)
	assert.NotEqual(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestFlowWait_EventEmission(t *testing.T) {
	te := newTestEnv()

	step := flowWaitStep("wait1", schema.WaitConfig{Duration: "10ms"})
	wf := newWorkflow("wf-wait-events", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	started, _ := te.eventLog.GetEventsByType(context.Background(), schema.EventWaitStarted, eventFilterForWF("wf-wait-events"))
	completed, _ := te.eventLog.GetEventsByType(context.Background(), schema.EventWaitCompleted, eventFilterForWF("wf-wait-events"))
	assert.GreaterOrEqual(t, len(started), 1)
	assert.GreaterOrEqual(t, len(completed), 1)
}

// ============================================================
// Integration tests: multi-level nesting
// ============================================================

func TestFlowIntegration_ConditionInsideLoop(t *testing.T) {
	te := newTestEnv()

	var trueCount, falseCount int32
	te.registry.Register(&mockAction{name: "true_action", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		atomic.AddInt32(&trueCount, 1)
		return &actions.ActionOutput{Data: json.RawMessage(`{"path":"true"}`)}, nil
	}})
	te.registry.Register(&mockAction{name: "false_action", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		atomic.AddInt32(&falseCount, 1)
		return &actions.ActionOutput{Data: json.RawMessage(`{"path":"false"}`)}, nil
	}})

	// Condition inside each loop iteration: alternate true/false based on iter.index % 2.
	// Note: CEL uses "iter" (not "loop" which is a CEL reserved keyword).
	condStep := flowConditionStep("branch", `iter.index % 2 == 0`,
		map[string][]schema.StepDefinition{
			"true":  {flowActionStep("on_true", "true_action")},
			"false": {flowActionStep("on_false", "false_action")},
		}, nil)

	cfg := schema.LoopConfig{
		Mode:    "for_each",
		Over:    `[0,1,2,3]`,
		Body:    []schema.StepDefinition{condStep},
		MaxIter: 10,
	}

	step := flowLoopStep("loop1", cfg)
	wf := newWorkflow("wf-integ-cond-loop", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	// Indices 0,2 → true (even), indices 1,3 → false (odd)
	assert.Equal(t, int32(2), atomic.LoadInt32(&trueCount))
	assert.Equal(t, int32(2), atomic.LoadInt32(&falseCount))
}

func TestFlowIntegration_ParallelWithLoopsInBranches(t *testing.T) {
	te := newTestEnv()

	var execCount int32
	te.registry.Register(&mockAction{name: "work", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		atomic.AddInt32(&execCount, 1)
		return &actions.ActionOutput{Data: json.RawMessage(`{"ok":true}`)}, nil
	}})

	// Branch 0: loop 3 times
	loopBranch := flowLoopStep("loop_branch", schema.LoopConfig{
		Mode:    "for_each",
		Over:    `[1,2,3]`,
		Body:    []schema.StepDefinition{flowActionStep("loop_work", "work")},
		MaxIter: 10,
	})

	// Branch 1: single action
	actionBranch := flowActionStep("action_branch", "work")

	step := flowParallelStep("par1",
		[][]schema.StepDefinition{
			{loopBranch},
			{actionBranch},
		}, "all")

	wf := newWorkflow("wf-integ-par-loop", step)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	// 3 loop iterations + 1 action = 4
	assert.Equal(t, int32(4), atomic.LoadInt32(&execCount))
}

func TestFlowIntegration_ConditionThenParallelThenLoop(t *testing.T) {
	te := newTestEnv()

	var execCount int32
	te.registry.Register(&mockAction{name: "work", execFn: func(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
		atomic.AddInt32(&execCount, 1)
		return &actions.ActionOutput{Data: json.RawMessage(`{"ok":true}`)}, nil
	}})

	// Condition → true branch → parallel step → one branch has a loop
	loopInBranch := flowLoopStep("inner_loop", schema.LoopConfig{
		Mode:    "for_each",
		Over:    `[1,2]`,
		Body:    []schema.StepDefinition{flowActionStep("inner_work", "work")},
		MaxIter: 10,
	})

	parallelInBranch := flowParallelStep("inner_parallel",
		[][]schema.StepDefinition{
			{flowActionStep("branch_a", "work")},
			{loopInBranch},
		}, "all")

	condStep := flowConditionStep("top_cond", "true",
		map[string][]schema.StepDefinition{
			"true": {parallelInBranch},
		}, nil)

	wf := newWorkflow("wf-integ-deep", condStep)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)

	// 1 action in branch_a + 2 loop iterations = 3
	assert.Equal(t, int32(3), atomic.LoadInt32(&execCount))
}

func TestFlowIntegration_SequentialFlowControlSteps(t *testing.T) {
	te := newTestEnv()
	te.registry.Register(&mockAction{name: "noop"})

	// condition step followed by wait step followed by loop step (sequential in DAG).
	condStep := flowConditionStep("step1", "true",
		map[string][]schema.StepDefinition{
			"true": {flowActionStep("c_action", "noop")},
		}, nil)
	condStep.DependsOn = nil

	waitStep := flowWaitStep("step2", schema.WaitConfig{Duration: "10ms"})
	waitStep.DependsOn = []string{"step1"}

	loopStep := flowLoopStep("step3", schema.LoopConfig{
		Mode:    "for_each",
		Over:    `[1,2]`,
		Body:    []schema.StepDefinition{flowActionStep("l_action", "noop")},
		MaxIter: 10,
	})
	loopStep.DependsOn = []string{"step2"}

	wf := newWorkflow("wf-integ-seq", condStep, waitStep, loopStep)
	te.store.CreateWorkflow(context.Background(), wf)

	result, err := te.executor.Run(context.Background(), wf, nil)
	require.NoError(t, err)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
	// 3 DAG-level steps (step1, step2, step3) + 1 condition sub-step + 2 loop sub-steps = 6
	assert.GreaterOrEqual(t, len(result.Steps), 3)
}

// ============================================================
// DAG validation tests for wait step
// ============================================================

func TestDAG_WaitStep_Valid(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			flowWaitStep("w1", schema.WaitConfig{Duration: "5s"}),
		},
	}
	dag, err := ParseDAG(def)
	require.NoError(t, err)
	assert.Contains(t, dag.Steps, "w1")
}

func TestDAG_WaitStep_NoDurationOrSignal(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			flowWaitStep("w1", schema.WaitConfig{}),
		},
	}
	_, err := ParseDAG(def)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must have duration or signal")
}

func TestDAG_WaitStep_SignalOnly(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			flowWaitStep("w1", schema.WaitConfig{Signal: "resume"}),
		},
	}
	dag, err := ParseDAG(def)
	require.NoError(t, err)
	assert.Contains(t, dag.Steps, "w1")
}

// --- Helper ---

func eventFilterForWF(wfID string) store.EventFilter {
	return store.EventFilter{WorkflowID: wfID}
}
