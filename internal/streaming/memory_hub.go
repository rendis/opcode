package streaming

import (
	"context"
	"sync"
	"sync/atomic"
)

const defaultChannelBuffer = 64

// subscriber holds a channel and filter for a single subscriber.
type subscriber struct {
	ch     chan StreamEvent
	filter EventFilter
}

// MemoryHub is an in-memory EventHub implementation using channels.
type MemoryHub struct {
	mu   sync.RWMutex
	subs map[uint64]*subscriber
	seq  atomic.Uint64
}

// NewMemoryHub creates a new MemoryHub.
func NewMemoryHub() *MemoryHub {
	return &MemoryHub{
		subs: make(map[uint64]*subscriber),
	}
}

// Publish sends an event to all matching subscribers.
// Non-blocking: if a subscriber's channel is full the event is dropped.
func (h *MemoryHub) Publish(ctx context.Context, event StreamEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, sub := range h.subs {
		if !matchFilter(sub.filter, event) {
			continue
		}
		select {
		case sub.ch <- event:
		default:
			// backpressure: drop event for slow subscriber
		}
	}
	return nil
}

// Subscribe creates a new subscription filtered by the given EventFilter.
// Returns a receive-only channel, a cancel function, and any error.
func (h *MemoryHub) Subscribe(ctx context.Context, filter EventFilter) (<-chan StreamEvent, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	id := h.seq.Add(1)
	ch := make(chan StreamEvent, defaultChannelBuffer)

	h.mu.Lock()
	h.subs[id] = &subscriber{ch: ch, filter: filter}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		delete(h.subs, id)
		h.mu.Unlock()
	}

	return ch, cancel, nil
}

// matchFilter returns true if the event passes the filter criteria.
func matchFilter(f EventFilter, e StreamEvent) bool {
	if f.WorkflowID != "" && f.WorkflowID != e.WorkflowID {
		return false
	}
	if len(f.EventTypes) > 0 {
		found := false
		for _, t := range f.EventTypes {
			if t == e.EventType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
