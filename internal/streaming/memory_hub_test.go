package streaming

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishSubscribe(t *testing.T) {
	hub := NewMemoryHub()
	ctx := context.Background()

	ch, cancel, err := hub.Subscribe(ctx, EventFilter{})
	require.NoError(t, err)
	defer cancel()

	event := StreamEvent{
		WorkflowID: "wf-1",
		StepID:     "step-1",
		EventType:  "step.completed",
		Payload:    map[string]any{"result": "ok"},
	}

	err = hub.Publish(ctx, event)
	require.NoError(t, err)

	select {
	case got := <-ch:
		assert.Equal(t, event.WorkflowID, got.WorkflowID)
		assert.Equal(t, event.StepID, got.StepID)
		assert.Equal(t, event.EventType, got.EventType)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestFilterByWorkflowID(t *testing.T) {
	hub := NewMemoryHub()
	ctx := context.Background()

	ch, cancel, err := hub.Subscribe(ctx, EventFilter{WorkflowID: "wf-1"})
	require.NoError(t, err)
	defer cancel()

	// Should be received (matching workflow)
	err = hub.Publish(ctx, StreamEvent{WorkflowID: "wf-1", EventType: "step.started"})
	require.NoError(t, err)

	// Should be dropped (different workflow)
	err = hub.Publish(ctx, StreamEvent{WorkflowID: "wf-2", EventType: "step.started"})
	require.NoError(t, err)

	select {
	case got := <-ch:
		assert.Equal(t, "wf-1", got.WorkflowID)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}

	// Channel should be empty -- the wf-2 event was filtered out.
	select {
	case evt := <-ch:
		t.Fatalf("unexpected event: %+v", evt)
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestFilterByEventType(t *testing.T) {
	hub := NewMemoryHub()
	ctx := context.Background()

	ch, cancel, err := hub.Subscribe(ctx, EventFilter{
		EventTypes: []string{"step.completed", "workflow.failed"},
	})
	require.NoError(t, err)
	defer cancel()

	// Should be received
	err = hub.Publish(ctx, StreamEvent{WorkflowID: "wf-1", EventType: "step.completed"})
	require.NoError(t, err)

	// Should be dropped
	err = hub.Publish(ctx, StreamEvent{WorkflowID: "wf-1", EventType: "step.started"})
	require.NoError(t, err)

	// Should be received
	err = hub.Publish(ctx, StreamEvent{WorkflowID: "wf-1", EventType: "workflow.failed"})
	require.NoError(t, err)

	var received []string
	for i := 0; i < 2; i++ {
		select {
		case got := <-ch:
			received = append(received, got.EventType)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	}
	assert.Equal(t, []string{"step.completed", "workflow.failed"}, received)

	// No more events
	select {
	case evt := <-ch:
		t.Fatalf("unexpected event: %+v", evt)
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestMultipleSubscribers(t *testing.T) {
	hub := NewMemoryHub()
	ctx := context.Background()

	ch1, cancel1, err := hub.Subscribe(ctx, EventFilter{})
	require.NoError(t, err)
	defer cancel1()

	ch2, cancel2, err := hub.Subscribe(ctx, EventFilter{})
	require.NoError(t, err)
	defer cancel2()

	event := StreamEvent{WorkflowID: "wf-1", EventType: "step.completed"}
	err = hub.Publish(ctx, event)
	require.NoError(t, err)

	for _, ch := range []<-chan StreamEvent{ch1, ch2} {
		select {
		case got := <-ch:
			assert.Equal(t, "wf-1", got.WorkflowID)
			assert.Equal(t, "step.completed", got.EventType)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	}
}

func TestCancelSubscription(t *testing.T) {
	hub := NewMemoryHub()
	ctx := context.Background()

	ch, cancel, err := hub.Subscribe(ctx, EventFilter{})
	require.NoError(t, err)

	// Cancel removes the subscriber
	cancel()

	err = hub.Publish(ctx, StreamEvent{WorkflowID: "wf-1", EventType: "step.completed"})
	require.NoError(t, err)

	select {
	case evt := <-ch:
		t.Fatalf("unexpected event after cancel: %+v", evt)
	case <-time.After(50 * time.Millisecond):
		// expected: subscriber was removed
	}

	// Verify subscriber map is empty
	hub.mu.RLock()
	assert.Empty(t, hub.subs)
	hub.mu.RUnlock()
}

func TestBackpressure(t *testing.T) {
	hub := NewMemoryHub()
	ctx := context.Background()

	ch, cancel, err := hub.Subscribe(ctx, EventFilter{})
	require.NoError(t, err)
	defer cancel()

	// Fill the channel buffer (64) then publish one more.
	// None of these should block.
	for i := 0; i < defaultChannelBuffer+10; i++ {
		err = hub.Publish(ctx, StreamEvent{
			WorkflowID: "wf-1",
			EventType:  "tick",
		})
		require.NoError(t, err)
	}

	// We should be able to drain exactly defaultChannelBuffer events.
	drained := 0
	for {
		select {
		case <-ch:
			drained++
		default:
			goto done
		}
	}
done:
	assert.Equal(t, defaultChannelBuffer, drained)
}

func TestConcurrentAccess(t *testing.T) {
	hub := NewMemoryHub()
	ctx := context.Background()
	const goroutines = 20
	const eventsPerGoroutine = 50

	var wg sync.WaitGroup

	// Start subscribers
	channels := make([]<-chan StreamEvent, goroutines)
	cancels := make([]func(), goroutines)
	for i := 0; i < goroutines; i++ {
		ch, cancel, err := hub.Subscribe(ctx, EventFilter{})
		require.NoError(t, err)
		channels[i] = ch
		cancels[i] = cancel
	}
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()

	// Concurrent publishers
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				_ = hub.Publish(ctx, StreamEvent{
					WorkflowID: "wf-concurrent",
					EventType:  "tick",
				})
			}
		}()
	}

	// Concurrent subscribers being added/removed
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, cancel, err := hub.Subscribe(ctx, EventFilter{})
			if err != nil {
				return
			}
			// drain a few then cancel
			for range 5 {
				select {
				case <-ch:
				case <-time.After(10 * time.Millisecond):
				}
			}
			cancel()
		}()
	}

	wg.Wait()
}

func TestPublishCancelledContext(t *testing.T) {
	hub := NewMemoryHub()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := hub.Publish(ctx, StreamEvent{WorkflowID: "wf-1", EventType: "tick"})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestSubscribeCancelledContext(t *testing.T) {
	hub := NewMemoryHub()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := hub.Subscribe(ctx, EventFilter{})
	assert.ErrorIs(t, err, context.Canceled)
}
