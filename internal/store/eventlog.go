package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rendis/opcode/pkg/schema"
)

// EventLog provides event-sourcing operations on top of a LibSQLStore.
type EventLog struct {
	store *LibSQLStore
}

// NewEventLog wraps a LibSQLStore to provide event-sourcing operations.
func NewEventLog(s *LibSQLStore) *EventLog {
	return &EventLog{store: s}
}

// AppendEvent appends an event with a monotonically increasing per-workflow sequence.
// Uses BEGIN IMMEDIATE to ensure sequence correctness under concurrency.
func (el *EventLog) AppendEvent(ctx context.Context, event *Event) error {
	db := el.store.DB()

	// BEGIN IMMEDIATE acquires a write lock immediately to prevent concurrent writers
	// from interleaving sequence reads and writes.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin immediate tx: %w", err)
	}
	defer tx.Rollback()

	// Acquire write lock by executing a write-intent statement.
	// In WAL mode, BeginTx alone may start a deferred transaction.
	// We use an immediate-mode write to force lock acquisition.
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO schema_version (version, name) VALUES (-1, '_lock_noop')`); err != nil {
		return fmt.Errorf("acquire write lock: %w", err)
	}
	// Clean up the noop row.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM schema_version WHERE version = -1`); err != nil {
		return fmt.Errorf("cleanup write lock: %w", err)
	}

	var seq int64
	err = tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(sequence), 0) + 1 FROM events WHERE workflow_id = ?`, event.WorkflowID,
	).Scan(&seq)
	if err != nil {
		return fmt.Errorf("get next sequence: %w", err)
	}
	event.Sequence = seq

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	payload := nullRaw(event.Payload)

	_, err = tx.ExecContext(ctx,
		`INSERT INTO events (workflow_id, step_id, event_type, payload, agent_id, timestamp, sequence)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		event.WorkflowID, nullStr(event.StepID), event.Type, payload, nullStr(event.AgentID), event.Timestamp, seq,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit event: %w", err)
	}
	return nil
}

// GetEvents returns events for a workflow with sequence > since, ordered by sequence ASC.
func (el *EventLog) GetEvents(ctx context.Context, workflowID string, since int64) ([]*Event, error) {
	return el.store.GetEvents(ctx, workflowID, since)
}

// GetEventsByType returns events of a specific type matching the filter.
func (el *EventLog) GetEventsByType(ctx context.Context, eventType string, filter EventFilter) ([]*Event, error) {
	return el.store.GetEventsByType(ctx, eventType, filter)
}

// ReplayEvents replays all events for a workflow and returns the reconstructed step states.
// Returns an error if sequence gaps are detected.
func (el *EventLog) ReplayEvents(ctx context.Context, workflowID string) (map[string]*StepState, error) {
	events, err := el.store.GetEvents(ctx, workflowID, 0)
	if err != nil {
		return nil, fmt.Errorf("get events for replay: %w", err)
	}

	if len(events) == 0 {
		return make(map[string]*StepState), nil
	}

	// Validate sequence contiguity.
	for i, e := range events {
		expected := int64(i + 1)
		if e.Sequence != expected {
			return nil, schema.NewErrorf(schema.ErrCodeStore,
				"sequence gap in workflow %s: expected %d, got %d", workflowID, expected, e.Sequence)
		}
	}

	states := make(map[string]*StepState)

	for _, e := range events {
		if e.StepID == "" {
			continue
		}

		ss, ok := states[e.StepID]
		if !ok {
			ss = &StepState{
				WorkflowID: workflowID,
				StepID:     e.StepID,
				Status:     schema.StepStatusPending,
			}
			states[e.StepID] = ss
		}

		switch e.Type {
		case schema.EventStepStarted:
			ss.Status = schema.StepStatusRunning
			ts := e.Timestamp
			ss.StartedAt = &ts

		case schema.EventStepCompleted:
			ss.Status = schema.StepStatusCompleted
			ts := e.Timestamp
			ss.CompletedAt = &ts
			ss.Output = e.Payload
			if ss.StartedAt != nil {
				ss.DurationMs = ts.Sub(*ss.StartedAt).Milliseconds()
			}

		case schema.EventStepFailed:
			ss.Status = schema.StepStatusFailed
			ss.Error = e.Payload

		case schema.EventStepSkipped:
			ss.Status = schema.StepStatusSkipped

		case schema.EventStepRetrying:
			ss.Status = schema.StepStatusRetrying
			ss.RetryCount++

		case schema.EventDecisionRequested:
			ss.Status = schema.StepStatusSuspended

		case schema.EventDecisionResolved:
			// Decision resolution is noted; the executor uses this to resume the step.
			// We don't change the step status here — the executor will transition it.
		}
	}

	return states, nil
}

// SnapshotPayload is used to extract typed data from event payloads.
type SnapshotPayload struct {
	Output json.RawMessage `json:"output,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}
