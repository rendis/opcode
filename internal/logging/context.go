package logging

import (
	"context"
	"log/slog"
)

type ctxKey int

const (
	workflowIDKey ctxKey = iota
	stepIDKey
	agentIDKey
)

// WithWorkflowID returns a context with the workflow ID set.
func WithWorkflowID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, workflowIDKey, id)
}

// WithStepID returns a context with the step ID set.
func WithStepID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, stepIDKey, id)
}

// WithAgentID returns a context with the agent ID set.
func WithAgentID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, agentIDKey, id)
}

// WorkflowID extracts the workflow ID from the context, or "" if absent.
func WorkflowID(ctx context.Context) string {
	v, _ := ctx.Value(workflowIDKey).(string)
	return v
}

// StepID extracts the step ID from the context, or "" if absent.
func StepID(ctx context.Context) string {
	v, _ := ctx.Value(stepIDKey).(string)
	return v
}

// AgentID extracts the agent ID from the context, or "" if absent.
func AgentID(ctx context.Context) string {
	v, _ := ctx.Value(agentIDKey).(string)
	return v
}

// WithIDs sets all three correlation IDs on the context at once.
func WithIDs(ctx context.Context, workflowID, stepID, agentID string) context.Context {
	ctx = WithWorkflowID(ctx, workflowID)
	ctx = WithStepID(ctx, stepID)
	ctx = WithAgentID(ctx, agentID)
	return ctx
}

// LogWith returns a logger enriched with correlation IDs from the context.
// Only non-empty values are added as attributes.
func LogWith(ctx context.Context, logger *slog.Logger) *slog.Logger {
	if wfID := WorkflowID(ctx); wfID != "" {
		logger = logger.With(slog.String("workflow_id", wfID))
	}
	if sID := StepID(ctx); sID != "" {
		logger = logger.With(slog.String("step_id", sID))
	}
	if aID := AgentID(ctx); aID != "" {
		logger = logger.With(slog.String("agent_id", aID))
	}
	return logger
}

// CorrelationHandler wraps an slog.Handler, automatically injecting
// correlation IDs from the context into every log record.
// Use with slog.New(NewCorrelationHandler(inner)) so callers can use
// logger.InfoContext(ctx, ...) and IDs appear automatically.
type CorrelationHandler struct {
	inner slog.Handler
}

// NewCorrelationHandler wraps the given handler with automatic correlation ID injection.
func NewCorrelationHandler(inner slog.Handler) *CorrelationHandler {
	return &CorrelationHandler{inner: inner}
}

func (h *CorrelationHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *CorrelationHandler) Handle(ctx context.Context, r slog.Record) error {
	if v := WorkflowID(ctx); v != "" {
		r.AddAttrs(slog.String("workflow_id", v))
	}
	if v := StepID(ctx); v != "" {
		r.AddAttrs(slog.String("step_id", v))
	}
	if v := AgentID(ctx); v != "" {
		r.AddAttrs(slog.String("agent_id", v))
	}
	return h.inner.Handle(ctx, r)
}

func (h *CorrelationHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &CorrelationHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *CorrelationHandler) WithGroup(name string) slog.Handler {
	return &CorrelationHandler{inner: h.inner.WithGroup(name)}
}
