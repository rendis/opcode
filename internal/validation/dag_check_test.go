package validation

import (
	"testing"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Cycle detection ---

func TestDAG_NoCycle_Linear(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "a"},
			{ID: "b", DependsOn: []string{"a"}},
			{ID: "c", DependsOn: []string{"b"}},
		},
	}
	result := validateDAG(def)
	assert.True(t, result.Valid())
	assert.Empty(t, result.Warnings)
}

func TestDAG_NoCycle_Diamond(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "a"},
			{ID: "b", DependsOn: []string{"a"}},
			{ID: "c", DependsOn: []string{"a"}},
			{ID: "d", DependsOn: []string{"b", "c"}},
		},
	}
	result := validateDAG(def)
	assert.True(t, result.Valid())
	assert.Empty(t, result.Warnings)
}

func TestDAG_SimpleCycle(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "a", DependsOn: []string{"c"}},
			{ID: "b", DependsOn: []string{"a"}},
			{ID: "c", DependsOn: []string{"b"}},
		},
	}
	result := validateDAG(def)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, schema.ErrCodeCycleDetected, result.Errors[0].Code)
}

func TestDAG_SelfCycle(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "a", DependsOn: []string{"a"}},
		},
	}
	result := validateDAG(def)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, schema.ErrCodeCycleDetected, result.Errors[0].Code)
}

func TestDAG_ComplexCycle(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "a"},
			{ID: "b", DependsOn: []string{"a", "d"}},
			{ID: "c", DependsOn: []string{"b"}},
			{ID: "d", DependsOn: []string{"c"}},
		},
	}
	result := validateDAG(def)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, schema.ErrCodeCycleDetected, result.Errors[0].Code)
}

// --- Reachability ---

func TestDAG_AllReachable(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "root"},
			{ID: "child", DependsOn: []string{"root"}},
		},
	}
	result := validateDAG(def)
	assert.True(t, result.Valid())
	assert.Empty(t, result.Warnings)
}

func TestDAG_DisconnectedRoots(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "root1"},
			{ID: "root2"},
			{ID: "child", DependsOn: []string{"root1"}},
		},
	}
	result := validateDAG(def)
	assert.True(t, result.Valid())
	assert.Empty(t, result.Warnings, "all steps reachable from some root")
}

func TestDAG_SingleStep(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "only"},
		},
	}
	result := validateDAG(def)
	assert.True(t, result.Valid())
	assert.Empty(t, result.Warnings)
}

func TestDAG_UnreachableFromInvalidDep(t *testing.T) {
	// Step "island" depends on "ghost" which doesn't exist.
	// Semantic catches the bad ref; DAG skips invalid refs and sees "island" as a root.
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "root"},
			{ID: "island", DependsOn: []string{"ghost"}},
		},
	}
	result := validateDAG(def)
	assert.True(t, result.Valid())
	// "island" is reachable as root since "ghost" is filtered out.
	assert.Empty(t, result.Warnings)
}

func TestDAG_SkipsDuplicateDeps(t *testing.T) {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "a"},
			{ID: "b", DependsOn: []string{"a", "a", "a"}},
		},
	}
	result := validateDAG(def)
	assert.True(t, result.Valid())
}
