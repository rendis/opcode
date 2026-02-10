package engine

import (
	"encoding/json"
	"testing"

	"github.com/rendis/opcode/pkg/schema"
)

// --- helpers ---

func actionStep(id string, depends ...string) schema.StepDefinition {
	return schema.StepDefinition{
		ID:        id,
		Type:      schema.StepTypeAction,
		Action:    "noop",
		DependsOn: depends,
	}
}

func reasoningStep(id string, depends ...string) schema.StepDefinition {
	cfg, _ := json.Marshal(schema.ReasoningConfig{
		PromptContext: "test",
		Options:       []schema.ReasoningOption{{ID: "a"}, {ID: "b"}},
	})
	return schema.StepDefinition{
		ID:        id,
		Type:      schema.StepTypeReasoning,
		Config:    cfg,
		DependsOn: depends,
	}
}

func conditionStep(id string, depends ...string) schema.StepDefinition {
	cfg, _ := json.Marshal(schema.ConditionConfig{
		Expression: "true",
		Branches:   map[string][]schema.StepDefinition{"true": {}},
	})
	return schema.StepDefinition{
		ID:        id,
		Type:      schema.StepTypeCondition,
		Config:    cfg,
		DependsOn: depends,
	}
}

func parallelStep(id string, depends ...string) schema.StepDefinition {
	cfg, _ := json.Marshal(schema.ParallelConfig{
		Branches: [][]schema.StepDefinition{{{ID: "inner", Type: schema.StepTypeAction, Action: "noop"}}},
	})
	return schema.StepDefinition{
		ID:        id,
		Type:      schema.StepTypeParallel,
		Config:    cfg,
		DependsOn: depends,
	}
}

func loopStep(id string, maxIter int, depends ...string) schema.StepDefinition {
	cfg, _ := json.Marshal(schema.LoopConfig{
		Mode:    "for_each",
		MaxIter: maxIter,
		Body:    []schema.StepDefinition{{ID: "body", Type: schema.StepTypeAction, Action: "noop"}},
	})
	return schema.StepDefinition{
		ID:        id,
		Type:      schema.StepTypeLoop,
		Config:    cfg,
		DependsOn: depends,
	}
}

func loopStepWithCondition(id, condition string, depends ...string) schema.StepDefinition {
	cfg, _ := json.Marshal(schema.LoopConfig{
		Mode:      "while",
		Condition: condition,
		Body:      []schema.StepDefinition{{ID: "body", Type: schema.StepTypeAction, Action: "noop"}},
	})
	return schema.StepDefinition{
		ID:        id,
		Type:      schema.StepTypeLoop,
		Config:    cfg,
		DependsOn: depends,
	}
}

func assertError(t *testing.T, err error, expectedCode string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	opcErr, ok := err.(*schema.OpcodeError)
	if !ok {
		t.Fatalf("expected OpcodeError, got %T: %v", err, err)
	}
	if opcErr.Code != expectedCode {
		t.Errorf("expected code %s, got %s: %s", expectedCode, opcErr.Code, opcErr.Message)
	}
}

// indexOf returns the position of each step in the sorted order.
func indexOf(dag *DAG) map[string]int {
	m := make(map[string]int, len(dag.Sorted))
	for i, s := range dag.Sorted {
		m[s] = i
	}
	return m
}

// --- graph structure tests ---

func TestParseDAG_LinearChain(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			actionStep("a"),
			actionStep("b", "a"),
			actionStep("c", "b"),
		},
	}

	dag, err := ParseDAG(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	idx := indexOf(dag)
	if idx["a"] >= idx["b"] || idx["b"] >= idx["c"] {
		t.Errorf("incorrect topological order: %v", dag.Sorted)
	}
	if len(dag.Roots) != 1 || dag.Roots[0] != "a" {
		t.Errorf("expected roots=[a], got %v", dag.Roots)
	}
	if len(dag.Levels) != 3 {
		t.Errorf("expected 3 levels, got %d", len(dag.Levels))
	}
}

func TestParseDAG_Diamond(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			actionStep("a"),
			actionStep("b", "a"),
			actionStep("c", "a"),
			actionStep("d", "b", "c"),
		},
	}

	dag, err := ParseDAG(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	idx := indexOf(dag)
	if idx["a"] >= idx["b"] || idx["a"] >= idx["c"] {
		t.Errorf("a must come before b and c: %v", dag.Sorted)
	}
	if idx["b"] >= idx["d"] || idx["c"] >= idx["d"] {
		t.Errorf("b and c must come before d: %v", dag.Sorted)
	}
	if len(dag.Levels) != 3 {
		t.Fatalf("expected 3 levels, got %d", len(dag.Levels))
	}
	if len(dag.Levels[1]) != 2 {
		t.Errorf("level 1 should have 2 parallel steps, got %v", dag.Levels[1])
	}
}

func TestParseDAG_ComplexMultiLevelDiamond(t *testing.T) {
	//     a
	//    / \
	//   b   c
	//   |   |
	//   d   e
	//    \ /
	//     f
	//     |
	//     g
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			actionStep("a"),
			actionStep("b", "a"),
			actionStep("c", "a"),
			actionStep("d", "b"),
			actionStep("e", "c"),
			actionStep("f", "d", "e"),
			actionStep("g", "f"),
		},
	}

	dag, err := ParseDAG(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dag.Levels) != 5 {
		t.Fatalf("expected 5 levels, got %d: %v", len(dag.Levels), dag.Levels)
	}
	// Level 0: [a], Level 1: [b,c], Level 2: [d,e], Level 3: [f], Level 4: [g]
	if len(dag.Levels[0]) != 1 {
		t.Errorf("level 0 should be [a], got %v", dag.Levels[0])
	}
	if len(dag.Levels[1]) != 2 {
		t.Errorf("level 1 should have 2 parallel steps, got %v", dag.Levels[1])
	}
	if len(dag.Levels[2]) != 2 {
		t.Errorf("level 2 should have 2 parallel steps, got %v", dag.Levels[2])
	}
}

func TestParseDAG_WideParallelism(t *testing.T) {
	// root → a,b,c,d,e (5 parallel) → sink
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			actionStep("root"),
			actionStep("a", "root"),
			actionStep("b", "root"),
			actionStep("c", "root"),
			actionStep("d", "root"),
			actionStep("e", "root"),
			actionStep("sink", "a", "b", "c", "d", "e"),
		},
	}

	dag, err := ParseDAG(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dag.Levels) != 3 {
		t.Fatalf("expected 3 levels, got %d", len(dag.Levels))
	}
	if len(dag.Levels[1]) != 5 {
		t.Errorf("level 1 should have 5 parallel steps, got %d", len(dag.Levels[1]))
	}
}

func TestParseDAG_SingleStep(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{actionStep("only")},
	}

	dag, err := ParseDAG(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dag.Sorted) != 1 || dag.Sorted[0] != "only" {
		t.Errorf("expected sorted=[only], got %v", dag.Sorted)
	}
	if len(dag.Roots) != 1 || dag.Roots[0] != "only" {
		t.Errorf("expected roots=[only], got %v", dag.Roots)
	}
}

func TestParseDAG_MultipleRoots(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			actionStep("a"),
			actionStep("b"),
			actionStep("c"),
		},
	}

	dag, err := ParseDAG(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dag.Roots) != 3 {
		t.Errorf("expected 3 roots, got %d: %v", len(dag.Roots), dag.Roots)
	}
	if len(dag.Levels) != 1 || len(dag.Levels[0]) != 3 {
		t.Errorf("expected 1 level with 3 steps, got %v", dag.Levels)
	}
}

func TestParseDAG_EdgesAndReverse(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			actionStep("a"),
			actionStep("b", "a"),
			actionStep("c", "a"),
		},
	}

	dag, err := ParseDAG(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dag.Edges["a"]) != 0 {
		t.Errorf("a should have 0 deps, got %v", dag.Edges["a"])
	}
	if len(dag.Edges["b"]) != 1 || dag.Edges["b"][0] != "a" {
		t.Errorf("b should depend on [a], got %v", dag.Edges["b"])
	}
	if len(dag.Reverse["a"]) != 2 {
		t.Errorf("a should have 2 dependents, got %v", dag.Reverse["a"])
	}
}

// --- cycle detection tests ---

func TestParseDAG_CycleDetection(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			actionStep("a", "c"),
			actionStep("b", "a"),
			actionStep("c", "b"),
		},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeCycleDetected)
}

func TestParseDAG_SelfCycle(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{actionStep("a", "a")},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeCycleDetected)
}

func TestParseDAG_LargerCycle(t *testing.T) {
	// A → B → C → D → B (cycle in B-C-D)
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			actionStep("a"),
			actionStep("b", "a", "d"),
			actionStep("c", "b"),
			actionStep("d", "c"),
		},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeCycleDetected)
}

func TestParseDAG_CycleInSubgraph(t *testing.T) {
	// Valid root branch + cycle in separate subgraph
	// a → b (valid), c → d → e → c (cycle)
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			actionStep("a"),
			actionStep("b", "a"),
			actionStep("c", "e"),
			actionStep("d", "c"),
			actionStep("e", "d"),
		},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeCycleDetected)
}

func TestParseDAG_TwoNodeCycle(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			actionStep("a", "b"),
			actionStep("b", "a"),
		},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeCycleDetected)
}

// --- validation error tests ---

func TestParseDAG_NilDefinition(t *testing.T) {
	_, err := ParseDAG(nil)
	assertError(t, err, schema.ErrCodeValidation)
}

func TestParseDAG_EmptyWorkflow(t *testing.T) {
	def := &schema.WorkflowDefinition{Steps: []schema.StepDefinition{}}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeValidation)
}

func TestParseDAG_EmptyStepID(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{{ID: "", Type: schema.StepTypeAction, Action: "noop"}},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeValidation)
}

func TestParseDAG_DuplicateStepIDs(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{actionStep("a"), actionStep("a")},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeValidation)
}

func TestParseDAG_InvalidDependsOnReference(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{actionStep("a", "nonexistent")},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeValidation)
}

func TestParseDAG_DuplicateDependency(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			actionStep("a"),
			actionStep("b", "a", "a"), // duplicate dep
		},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeValidation)
}

func TestParseDAG_UnknownStepType(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{{ID: "bad", Type: "unknown_type", Action: "noop"}},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeValidation)
}

func TestParseDAG_DefaultStepType(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{{ID: "no_type", Action: "http.get"}},
	}
	dag, err := ParseDAG(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dag.Steps["no_type"].Type != schema.StepTypeAction {
		t.Errorf("expected default type action, got %s", dag.Steps["no_type"].Type)
	}
}

func TestParseDAG_ActionStepWithoutAction(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{{ID: "bad", Type: schema.StepTypeAction}},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeValidation)
}

// --- step type config validation tests ---

func TestParseDAG_MixedStepTypes(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			actionStep("fetch"),
			reasoningStep("decide", "fetch"),
			conditionStep("branch", "decide"),
			parallelStep("parallel_work", "branch"),
			loopStep("iterate", 10, "parallel_work"),
		},
	}

	dag, err := ParseDAG(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if dag.Steps["fetch"].Type != schema.StepTypeAction {
		t.Error("fetch should be action")
	}
	if dag.Steps["decide"].Type != schema.StepTypeReasoning {
		t.Error("decide should be reasoning")
	}
	if dag.Steps["branch"].Type != schema.StepTypeCondition {
		t.Error("branch should be condition")
	}
	if dag.Steps["parallel_work"].Type != schema.StepTypeParallel {
		t.Error("parallel_work should be parallel")
	}
	if dag.Steps["iterate"].Type != schema.StepTypeLoop {
		t.Error("iterate should be loop")
	}
}

func TestParseDAG_LoopWithoutStopCondition(t *testing.T) {
	cfg, _ := json.Marshal(schema.LoopConfig{
		Mode: "while",
		Body: []schema.StepDefinition{{ID: "body", Type: schema.StepTypeAction, Action: "noop"}},
		// No MaxIter, no Condition → infinite loop risk
	})
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{{
			ID:     "bad_loop",
			Type:   schema.StepTypeLoop,
			Config: cfg,
		}},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeValidation)
}

func TestParseDAG_LoopWithMaxIter(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{loopStep("ok_loop", 100)},
	}
	_, err := ParseDAG(def)
	if err != nil {
		t.Fatalf("loop with max_iter should be valid: %v", err)
	}
}

func TestParseDAG_LoopWithCondition(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{loopStepWithCondition("ok_loop", "count < 10")},
	}
	_, err := ParseDAG(def)
	if err != nil {
		t.Fatalf("loop with condition should be valid: %v", err)
	}
}

func TestParseDAG_LoopWithEmptyBody(t *testing.T) {
	cfg, _ := json.Marshal(schema.LoopConfig{
		Mode:    "for_each",
		MaxIter: 10,
		Body:    []schema.StepDefinition{}, // empty body
	})
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{{ID: "empty_loop", Type: schema.StepTypeLoop, Config: cfg}},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeValidation)
}

func TestParseDAG_LoopWithoutConfig(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{{ID: "no_cfg", Type: schema.StepTypeLoop}},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeValidation)
}

func TestParseDAG_ParallelWithoutBranches(t *testing.T) {
	cfg, _ := json.Marshal(schema.ParallelConfig{Branches: [][]schema.StepDefinition{}})
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{{ID: "no_branch", Type: schema.StepTypeParallel, Config: cfg}},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeValidation)
}

func TestParseDAG_ParallelWithoutConfig(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{{ID: "no_cfg", Type: schema.StepTypeParallel}},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeValidation)
}

func TestParseDAG_ConditionWithoutExpression(t *testing.T) {
	cfg, _ := json.Marshal(schema.ConditionConfig{
		Expression: "",
		Branches:   map[string][]schema.StepDefinition{"a": {}},
	})
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{{ID: "no_expr", Type: schema.StepTypeCondition, Config: cfg}},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeValidation)
}

func TestParseDAG_ConditionWithoutConfig(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{{ID: "no_cfg", Type: schema.StepTypeCondition}},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeValidation)
}

func TestParseDAG_ReasoningWithoutOptions_FreeForm(t *testing.T) {
	cfg, _ := json.Marshal(schema.ReasoningConfig{
		PromptContext: "test",
		Options:       []schema.ReasoningOption{}, // no options = free-form
	})
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{{ID: "no_opts", Type: schema.StepTypeReasoning, Config: cfg}},
	}
	_, err := ParseDAG(def)
	if err != nil {
		t.Fatalf("expected no error for free-form reasoning, got: %v", err)
	}
}

func TestParseDAG_ReasoningWithoutConfig(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{{ID: "no_cfg", Type: schema.StepTypeReasoning}},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeValidation)
}

func TestParseDAG_InvalidConfigJSON(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{{
			ID:     "bad_json",
			Type:   schema.StepTypeLoop,
			Config: json.RawMessage(`{invalid`),
		}},
	}
	_, err := ParseDAG(def)
	assertError(t, err, schema.ErrCodeValidation)
}
