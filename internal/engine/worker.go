package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// PoolMetrics tracks worker pool operational metrics.
type PoolMetrics struct {
	Active    int64 `json:"active"`
	Completed int64 `json:"completed"`
	Failed    int64 `json:"failed"`
	Panics    int64 `json:"panics"`
}

// ErrPoolShutdown is returned when work is submitted to a shut-down pool.
var ErrPoolShutdown = errors.New("worker pool is shut down")

// WorkerPool is a bounded goroutine pool for concurrent step execution.
type WorkerPool struct {
	sem     chan struct{}
	wg      sync.WaitGroup
	metrics PoolMetrics
	mu      sync.Mutex
	done    chan struct{}
	closed  bool
}

// NewWorkerPool creates a pool with the given max concurrency.
func NewWorkerPool(size int) *WorkerPool {
	if size <= 0 {
		size = 1
	}
	return &WorkerPool{
		sem:  make(chan struct{}, size),
		done: make(chan struct{}),
	}
}

// Submit enqueues work into the pool. It blocks if the pool is at capacity
// (backpressure) and respects context cancellation while waiting. Returns
// ErrPoolShutdown if the pool has been shut down.
func (p *WorkerPool) Submit(ctx context.Context, fn func(ctx context.Context) error) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrPoolShutdown
	}
	p.mu.Unlock()

	// Acquire semaphore slot, respecting context cancellation and shutdown.
	select {
	case p.sem <- struct{}{}:
		// Slot acquired.
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return ErrPoolShutdown
	}

	// Re-check closed after acquiring the slot, in case Shutdown raced.
	// wg.Add(1) MUST be inside the lock to prevent race with Shutdown's wg.Wait().
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		<-p.sem // release slot
		return ErrPoolShutdown
	}
	p.wg.Add(1)
	atomic.AddInt64(&p.metrics.Active, 1)
	p.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				atomic.AddInt64(&p.metrics.Panics, 1)
				atomic.AddInt64(&p.metrics.Failed, 1)
			}
			atomic.AddInt64(&p.metrics.Active, -1)
			<-p.sem // release slot
			p.wg.Done()
		}()

		err := fn(ctx)
		if err != nil {
			atomic.AddInt64(&p.metrics.Failed, 1)
		} else {
			atomic.AddInt64(&p.metrics.Completed, 1)
		}
	}()

	return nil
}

// Wait blocks until all submitted work completes.
func (p *WorkerPool) Wait() {
	p.wg.Wait()
}

// Shutdown gracefully stops the pool. It prevents new submissions and waits
// for all active work to complete.
func (p *WorkerPool) Shutdown() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	close(p.done)
	p.mu.Unlock()

	p.wg.Wait()
}

// Metrics returns a snapshot of the current pool metrics.
func (p *WorkerPool) Metrics() PoolMetrics {
	return PoolMetrics{
		Active:    atomic.LoadInt64(&p.metrics.Active),
		Completed: atomic.LoadInt64(&p.metrics.Completed),
		Failed:    atomic.LoadInt64(&p.metrics.Failed),
		Panics:    atomic.LoadInt64(&p.metrics.Panics),
	}
}

