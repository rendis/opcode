package engine

import (
	"context"
	"encoding/json"

	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
)

// ErrorHandlerResult describes the outcome of an error handler invocation.
type ErrorHandlerResult struct {
	// Handled is true if the error was handled (ignore, fallback).
	Handled bool
	// FallbackStepID is set when the handler triggers a fallback step.
	FallbackStepID string
	// ShouldFailWorkflow is true when the handler explicitly requests workflow failure.
	ShouldFailWorkflow bool
}

// HandleStepError evaluates the on_error handler for a step and determines the next action.
// It logs the invocation as an event. If no handler is configured, it returns unhandled.
func HandleStepError(
	ctx context.Context,
	eventLog EventAppender,
	workflowID, stepID string,
	handler *schema.ErrorHandler,
	stepErr error,
) (*ErrorHandlerResult, error) {
	if handler == nil {
		return &ErrorHandlerResult{Handled: false}, nil
	}

	// Log the error handler invocation.
	payload, _ := json.Marshal(map[string]any{
		"strategy":  string(handler.Strategy),
		"step_id":   stepID,
		"error":     stepErr.Error(),
		"fallback":  handler.FallbackStep,
	})
	_ = eventLog.AppendEvent(ctx, &store.Event{
		WorkflowID: workflowID,
		StepID:     stepID,
		Type:       schema.EventErrorHandlerInvoked,
		Payload:    payload,
	})

	switch handler.Strategy {
	case schema.ErrorStrategyIgnore:
		// Log ignore event.
		_ = eventLog.AppendEvent(ctx, &store.Event{
			WorkflowID: workflowID,
			StepID:     stepID,
			Type:       schema.EventStepIgnored,
			Payload:    payload,
		})
		return &ErrorHandlerResult{Handled: true}, nil

	case schema.ErrorStrategyFallbackStep:
		if handler.FallbackStep == "" {
			return &ErrorHandlerResult{Handled: false}, nil
		}
		// Log fallback event.
		fbPayload, _ := json.Marshal(map[string]any{
			"original_step":  stepID,
			"fallback_step":  handler.FallbackStep,
			"original_error": stepErr.Error(),
		})
		_ = eventLog.AppendEvent(ctx, &store.Event{
			WorkflowID: workflowID,
			StepID:     stepID,
			Type:       schema.EventStepFallback,
			Payload:    fbPayload,
		})
		return &ErrorHandlerResult{
			Handled:        true,
			FallbackStepID: handler.FallbackStep,
		}, nil

	case schema.ErrorStrategyFailWorkflow:
		return &ErrorHandlerResult{
			Handled:            true,
			ShouldFailWorkflow: true,
		}, nil

	case schema.ErrorStrategyRetry:
		// Retry is handled by the retry loop itself — this just signals
		// that we should defer to retry policy rather than failing immediately.
		return &ErrorHandlerResult{Handled: false}, nil

	default:
		// Unknown strategy — treat as unhandled.
		return &ErrorHandlerResult{Handled: false}, nil
	}
}
