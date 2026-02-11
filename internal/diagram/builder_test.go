package diagram

import (
	"encoding/json"
	"testing"

	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test workflow builders ---

func linearWorkflow() *schema.WorkflowDefinition {
	return &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "fetch", Type: schema.StepTypeAction, Action: "http.request"},
			{ID: "transform", Type: schema.StepTypeAction, Action: "jq", DependsOn: []string{"fetch"}},
			{ID: "store", Type: schema.StepTypeAction, Action: "db.write", DependsOn: []string{"transform"}},
		},
		Metadata: map[string]any{"name": "ETL Pipeline"},
	}
}

func conditionWorkflow() *schema.WorkflowDefinition {
	branches := map[string][]schema.StepDefinition{
		"true":  {{ID: "deploy", Type: schema.StepTypeAction, Action: "shell.exec"}},
		"false": {{ID: "notify", Type: schema.StepTypeAction, Action: "http.request"}},
	}
	cfgBytes, _ := json.Marshal(schema.ConditionConfig{
		Expression: "result.ok == true",
		Branches:   branches,
	})
	return &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "check", Type: schema.StepTypeAction, Action: "http.request"},
			{ID: "decide", Type: schema.StepTypeCondition, DependsOn: []string{"check"}, Config: cfgBytes},
		},
	}
}

func parallelWorkflow() *schema.WorkflowDefinition {
	branches := [][]schema.StepDefinition{
		{{ID: "a1", Type: schema.StepTypeAction, Action: "http.request"}},
		{{ID: "b1", Type: schema.StepTypeAction, Action: "http.request"}},
	}
	cfgBytes, _ := json.Marshal(schema.ParallelConfig{Branches: branches})
	return &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "setup", Type: schema.StepTypeAction, Action: "noop"},
			{ID: "fan-out", Type: schema.StepTypeParallel, DependsOn: []string{"setup"}, Config: cfgBytes},
		},
	}
}

func loopWorkflow() *schema.WorkflowDefinition {
	body := []schema.StepDefinition{
		{ID: "process", Type: schema.StepTypeAction, Action: "http.request"},
	}
	cfgBytes, _ := json.Marshal(schema.LoopConfig{
		Over:    "items",
		Body:    body,
		MaxIter: 10,
	})
	return &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "iterate", Type: schema.StepTypeLoop, Config: cfgBytes},
		},
	}
}

// --- Tests ---

func TestBuildLinearWorkflow(t *testing.T) {
	model, err := Build(linearWorkflow(), nil)
	require.NoError(t, err)

	assert.Equal(t, "ETL Pipeline", model.Title)
	// 3 steps + start + end = 5
	assert.Len(t, model.Nodes, 5)
	assert.NotEmpty(t, model.Edges)
	assert.NotEmpty(t, model.Levels)

	// First level is start, last is end.
	assert.Equal(t, []string{"__start__"}, model.Levels[0])
	assert.Equal(t, []string{"__end__"}, model.Levels[len(model.Levels)-1])

	// Verify node kinds.
	kinds := make(map[string]NodeKind)
	for _, n := range model.Nodes {
		kinds[n.ID] = n.Kind
	}
	assert.Equal(t, NodeKindStart, kinds["__start__"])
	assert.Equal(t, NodeKindEnd, kinds["__end__"])
	assert.Equal(t, NodeKindAction, kinds["fetch"])
	assert.Equal(t, NodeKindAction, kinds["transform"])
	assert.Equal(t, NodeKindAction, kinds["store"])
}

func TestBuildConditionWorkflow(t *testing.T) {
	model, err := Build(conditionWorkflow(), nil)
	require.NoError(t, err)

	// Find condition node.
	var condNode *Node
	for _, n := range model.Nodes {
		if n.ID == "decide" {
			condNode = n
			break
		}
	}
	require.NotNil(t, condNode)
	assert.Equal(t, NodeKindCondition, condNode.Kind)
	assert.NotEmpty(t, condNode.Children, "condition node should have children subgraphs")

	// Verify branches.
	branchLabels := make(map[string]bool)
	for _, sg := range condNode.Children {
		branchLabels[sg.Label] = true
		assert.NotEmpty(t, sg.Nodes)
	}
	assert.True(t, branchLabels["true"])
	assert.True(t, branchLabels["false"])
}

func TestBuildParallelWorkflow(t *testing.T) {
	model, err := Build(parallelWorkflow(), nil)
	require.NoError(t, err)

	var parNode *Node
	for _, n := range model.Nodes {
		if n.ID == "fan-out" {
			parNode = n
			break
		}
	}
	require.NotNil(t, parNode)
	assert.Equal(t, NodeKindParallel, parNode.Kind)
	assert.Len(t, parNode.Children, 2, "parallel node should have 2 branches")

	// Verify sub-step IDs follow naming convention.
	for _, sg := range parNode.Children {
		for _, subNode := range sg.Nodes {
			assert.Contains(t, subNode.ID, "fan-out.")
		}
	}
}

func TestBuildLoopWorkflow(t *testing.T) {
	model, err := Build(loopWorkflow(), nil)
	require.NoError(t, err)

	var loopNode *Node
	for _, n := range model.Nodes {
		if n.ID == "iterate" {
			loopNode = n
			break
		}
	}
	require.NotNil(t, loopNode)
	assert.Equal(t, NodeKindLoop, loopNode.Kind)
	require.Len(t, loopNode.Children, 1)
	assert.Equal(t, "body", loopNode.Children[0].Label)
	assert.Len(t, loopNode.Children[0].Nodes, 1)
	assert.Contains(t, loopNode.Children[0].Nodes[0].ID, "iterate.body.process")
}

func TestBuildWithStatusOverlay(t *testing.T) {
	def := linearWorkflow()
	states := []*store.StepState{
		{WorkflowID: "wf-1", StepID: "fetch", Status: schema.StepStatusCompleted, DurationMs: 150},
		{WorkflowID: "wf-1", StepID: "transform", Status: schema.StepStatusCompleted, DurationMs: 42},
		{WorkflowID: "wf-1", StepID: "store", Status: schema.StepStatusFailed, DurationMs: 300, Error: json.RawMessage(`"connection timeout"`)},
	}

	model, err := Build(def, states)
	require.NoError(t, err)

	for _, node := range model.Nodes {
		switch node.ID {
		case "fetch":
			require.NotNil(t, node.Status)
			assert.Equal(t, "completed", node.Status.Status)
			assert.Equal(t, int64(150), node.Status.DurationMs)
		case "transform":
			require.NotNil(t, node.Status)
			assert.Equal(t, "completed", node.Status.Status)
		case "store":
			require.NotNil(t, node.Status)
			assert.Equal(t, "failed", node.Status.Status)
			assert.NotEmpty(t, node.Status.Error)
		case "__start__", "__end__":
			assert.Nil(t, node.Status)
		}
	}
}

func TestBuildNilDefinition(t *testing.T) {
	_, err := Build(nil, nil)
	require.Error(t, err)
}

func TestBuildEmptySteps(t *testing.T) {
	_, err := Build(&schema.WorkflowDefinition{}, nil)
	require.Error(t, err)
}
