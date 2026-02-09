package streaming

import "context"

// StreamEvent is a real-time event emitted during workflow execution.
type StreamEvent struct {
	WorkflowID string `json:"workflow_id"`
	StepID     string `json:"step_id,omitempty"`
	EventType  string `json:"event_type"`
	Payload    any    `json:"payload,omitempty"`
}

// EventFilter specifies which events a subscriber wants to receive.
type EventFilter struct {
	WorkflowID string `json:"workflow_id,omitempty"`
	EventTypes []string `json:"event_types,omitempty"`
}

// EventHub provides pub/sub for real-time workflow events.
type EventHub interface {
	Publish(ctx context.Context, event StreamEvent) error
	Subscribe(ctx context.Context, filter EventFilter) (<-chan StreamEvent, func(), error)
}
