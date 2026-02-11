package diagram

import (
	"testing"

	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderImageLinear(t *testing.T) {
	model, err := Build(linearWorkflow(), nil)
	require.NoError(t, err)

	png, err := RenderImage(model)
	require.NoError(t, err)
	require.NotEmpty(t, png)

	// Verify PNG magic bytes: 0x89 P N G.
	assert.True(t, len(png) > 8, "PNG should be larger than header")
	assert.Equal(t, byte(0x89), png[0])
	assert.Equal(t, byte('P'), png[1])
	assert.Equal(t, byte('N'), png[2])
	assert.Equal(t, byte('G'), png[3])
}

func TestRenderImageCondition(t *testing.T) {
	model, err := Build(conditionWorkflow(), nil)
	require.NoError(t, err)

	png, err := RenderImage(model)
	require.NoError(t, err)
	require.NotEmpty(t, png)

	// PNG magic bytes.
	assert.Equal(t, byte(0x89), png[0])
	assert.Equal(t, byte('P'), png[1])
}

func TestRenderImageParallel(t *testing.T) {
	model, err := Build(parallelWorkflow(), nil)
	require.NoError(t, err)

	png, err := RenderImage(model)
	require.NoError(t, err)
	require.NotEmpty(t, png)
	assert.Equal(t, byte(0x89), png[0])
}

func TestRenderImageLoop(t *testing.T) {
	model, err := Build(loopWorkflow(), nil)
	require.NoError(t, err)

	png, err := RenderImage(model)
	require.NoError(t, err)
	require.NotEmpty(t, png)
	assert.Equal(t, byte(0x89), png[0])
}

func TestRenderImageWithStatus(t *testing.T) {
	def := linearWorkflow()
	states := []*store.StepState{
		{StepID: "fetch", Status: schema.StepStatusCompleted, DurationMs: 100},
		{StepID: "transform", Status: schema.StepStatusRunning},
		{StepID: "store", Status: schema.StepStatusFailed},
	}

	model, err := Build(def, states)
	require.NoError(t, err)

	png, err := RenderImage(model)
	require.NoError(t, err)
	require.NotEmpty(t, png)
	assert.Equal(t, byte(0x89), png[0])
}
