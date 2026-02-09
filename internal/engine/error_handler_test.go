package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleStepError_NilHandler(t *testing.T) {
	ms := newMockStore()
	mel := &mockEventLog{store: ms}

	result, err := HandleStepError(context.Background(), mel, "wf-1", "s1", nil, errors.New("boom"))
	require.NoError(t, err)
	assert.False(t, result.Handled)
}

func TestHandleStepError_Ignore(t *testing.T) {
	ms := newMockStore()
	mel := &mockEventLog{store: ms}

	handler := &schema.ErrorHandler{Strategy: schema.ErrorStrategyIgnore}
	result, err := HandleStepError(context.Background(), mel, "wf-1", "s1", handler, errors.New("boom"))
	require.NoError(t, err)
	assert.True(t, result.Handled)
	assert.False(t, result.ShouldFailWorkflow)
	assert.Empty(t, result.FallbackStepID)

	// Verify events were emitted.
	ms.mu.Lock()
	eventTypes := make([]string, len(ms.events))
	for i, e := range ms.events {
		eventTypes[i] = e.Type
	}
	ms.mu.Unlock()
	assert.Contains(t, eventTypes, schema.EventErrorHandlerInvoked)
	assert.Contains(t, eventTypes, schema.EventStepIgnored)
}

func TestHandleStepError_FailWorkflow(t *testing.T) {
	ms := newMockStore()
	mel := &mockEventLog{store: ms}

	handler := &schema.ErrorHandler{Strategy: schema.ErrorStrategyFailWorkflow}
	result, err := HandleStepError(context.Background(), mel, "wf-1", "s1", handler, errors.New("boom"))
	require.NoError(t, err)
	assert.True(t, result.Handled)
	assert.True(t, result.ShouldFailWorkflow)
}

func TestHandleStepError_FallbackStep(t *testing.T) {
	ms := newMockStore()
	mel := &mockEventLog{store: ms}

	handler := &schema.ErrorHandler{
		Strategy:     schema.ErrorStrategyFallbackStep,
		FallbackStep: "recovery_step",
	}
	result, err := HandleStepError(context.Background(), mel, "wf-1", "s1", handler, errors.New("boom"))
	require.NoError(t, err)
	assert.True(t, result.Handled)
	assert.Equal(t, "recovery_step", result.FallbackStepID)

	// Verify fallback event emitted.
	ms.mu.Lock()
	eventTypes := make([]string, len(ms.events))
	for i, e := range ms.events {
		eventTypes[i] = e.Type
	}
	ms.mu.Unlock()
	assert.Contains(t, eventTypes, schema.EventStepFallback)
}

func TestHandleStepError_FallbackStep_EmptyFallback(t *testing.T) {
	ms := newMockStore()
	mel := &mockEventLog{store: ms}

	handler := &schema.ErrorHandler{
		Strategy:     schema.ErrorStrategyFallbackStep,
		FallbackStep: "", // no fallback step specified
	}
	result, err := HandleStepError(context.Background(), mel, "wf-1", "s1", handler, errors.New("boom"))
	require.NoError(t, err)
	assert.False(t, result.Handled) // can't handle without a fallback step
}

func TestHandleStepError_RetryStrategy(t *testing.T) {
	ms := newMockStore()
	mel := &mockEventLog{store: ms}

	handler := &schema.ErrorHandler{Strategy: schema.ErrorStrategyRetry}
	result, err := HandleStepError(context.Background(), mel, "wf-1", "s1", handler, errors.New("boom"))
	require.NoError(t, err)
	assert.False(t, result.Handled) // retry defers to retry loop
}

func TestHandleStepError_UnknownStrategy(t *testing.T) {
	ms := newMockStore()
	mel := &mockEventLog{store: ms}

	handler := &schema.ErrorHandler{Strategy: "unknown_strategy"}
	result, err := HandleStepError(context.Background(), mel, "wf-1", "s1", handler, errors.New("boom"))
	require.NoError(t, err)
	assert.False(t, result.Handled)
}
