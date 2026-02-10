package logging

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextKeys(t *testing.T) {
	ctx := context.Background()

	// Initially empty.
	assert.Equal(t, "", WorkflowID(ctx))
	assert.Equal(t, "", StepID(ctx))
	assert.Equal(t, "", AgentID(ctx))

	// Set values.
	ctx = WithWorkflowID(ctx, "wf-123")
	ctx = WithStepID(ctx, "step-1")
	ctx = WithAgentID(ctx, "agent-42")

	// Round-trip.
	assert.Equal(t, "wf-123", WorkflowID(ctx))
	assert.Equal(t, "step-1", StepID(ctx))
	assert.Equal(t, "agent-42", AgentID(ctx))
}

func TestLogWith(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx := context.Background()
	ctx = WithWorkflowID(ctx, "wf-abc")
	ctx = WithStepID(ctx, "step-x")
	ctx = WithAgentID(ctx, "agent-7")

	enriched := LogWith(ctx, logger)
	enriched.Info("test message")

	output := buf.String()
	assert.Contains(t, output, "workflow_id=wf-abc")
	assert.Contains(t, output, "step_id=step-x")
	assert.Contains(t, output, "agent_id=agent-7")
	assert.Contains(t, output, "test message")
}

func TestLogWithMissingKeys(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Only set workflow ID — step and agent should not appear.
	ctx := WithWorkflowID(context.Background(), "wf-only")

	enriched := LogWith(ctx, logger)
	enriched.Info("partial context")

	output := buf.String()
	assert.Contains(t, output, "workflow_id=wf-only")
	assert.NotContains(t, output, "step_id")
	assert.NotContains(t, output, "agent_id")
}

func TestLogWithEmptyContext(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// No correlation IDs — no extra attrs.
	enriched := LogWith(context.Background(), logger)
	enriched.Info("no context")

	output := buf.String()
	assert.NotContains(t, output, "workflow_id")
	assert.NotContains(t, output, "step_id")
	assert.NotContains(t, output, "agent_id")
	assert.Contains(t, output, "no context")
}

func TestWithIDs(t *testing.T) {
	ctx := WithIDs(context.Background(), "wf-1", "step-2", "agent-3")
	assert.Equal(t, "wf-1", WorkflowID(ctx))
	assert.Equal(t, "step-2", StepID(ctx))
	assert.Equal(t, "agent-3", AgentID(ctx))
}

func TestCorrelationHandler(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(NewCorrelationHandler(inner))

	ctx := WithIDs(context.Background(), "wf-auto", "step-auto", "agent-auto")
	logger.InfoContext(ctx, "auto inject")

	output := buf.String()
	assert.Contains(t, output, `"workflow_id":"wf-auto"`)
	assert.Contains(t, output, `"step_id":"step-auto"`)
	assert.Contains(t, output, `"agent_id":"agent-auto"`)
	assert.Contains(t, output, "auto inject")
}

func TestCorrelationHandlerEmptyContext(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(NewCorrelationHandler(inner))

	logger.InfoContext(context.Background(), "bare log")

	output := buf.String()
	assert.NotContains(t, output, "workflow_id")
	assert.NotContains(t, output, "step_id")
	assert.NotContains(t, output, "agent_id")
	assert.Contains(t, output, "bare log")
}

func TestCorrelationHandlerPartialContext(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(NewCorrelationHandler(inner))

	ctx := WithWorkflowID(context.Background(), "wf-only")
	logger.InfoContext(ctx, "partial")

	output := buf.String()
	assert.Contains(t, output, `"workflow_id":"wf-only"`)
	assert.NotContains(t, output, "step_id")
	assert.NotContains(t, output, "agent_id")
}

func TestCorrelationHandlerWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewCorrelationHandler(inner)
	logger := slog.New(handler.WithAttrs([]slog.Attr{slog.String("component", "engine")}))

	ctx := WithWorkflowID(context.Background(), "wf-attr")
	logger.InfoContext(ctx, "with attrs")

	output := buf.String()
	assert.Contains(t, output, `"workflow_id":"wf-attr"`)
	assert.Contains(t, output, `"component":"engine"`)
}

func TestCorrelationHandlerWithGroup(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewCorrelationHandler(inner)
	logger := slog.New(handler.WithGroup("engine"))

	ctx := WithWorkflowID(context.Background(), "wf-grp")
	logger.InfoContext(ctx, "grouped", "key", "val")

	output := buf.String()
	assert.Contains(t, output, "wf-grp")
	assert.Contains(t, output, "grouped")
}
