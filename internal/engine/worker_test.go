package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPool_BasicExecution(t *testing.T) {
	pool := NewWorkerPool(2)
	defer pool.Shutdown()

	var ran int64
	err := pool.Submit(context.Background(), func(ctx context.Context) error {
		atomic.AddInt64(&ran, 1)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected submit error: %v", err)
	}

	pool.Wait()

	if atomic.LoadInt64(&ran) != 1 {
		t.Error("work did not execute")
	}

	m := pool.Metrics()
	if m.Completed != 1 {
		t.Errorf("expected 1 completed, got %d", m.Completed)
	}
}

func TestWorkerPool_ConcurrencyLimit(t *testing.T) {
	poolSize := 3
	pool := NewWorkerPool(poolSize)
	defer pool.Shutdown()

	var maxConcurrent int64
	var current int64
	var mu sync.Mutex

	taskCount := 10
	for i := 0; i < taskCount; i++ {
		err := pool.Submit(context.Background(), func(ctx context.Context) error {
			c := atomic.AddInt64(&current, 1)
			mu.Lock()
			if c > maxConcurrent {
				maxConcurrent = c
			}
			mu.Unlock()

			time.Sleep(10 * time.Millisecond)
			atomic.AddInt64(&current, -1)
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected submit error: %v", err)
		}
	}

	pool.Wait()

	if maxConcurrent > int64(poolSize) {
		t.Errorf("max concurrent %d exceeded pool size %d", maxConcurrent, poolSize)
	}
	if maxConcurrent == 0 {
		t.Error("no concurrent execution detected")
	}
}

func TestWorkerPool_Backpressure(t *testing.T) {
	pool := NewWorkerPool(1)
	defer pool.Shutdown()

	started := make(chan struct{})
	block := make(chan struct{})

	// Fill the pool with a blocking task.
	err := pool.Submit(context.Background(), func(ctx context.Context) error {
		close(started)
		<-block
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected submit error: %v", err)
	}

	<-started // Wait for worker to start.

	// Second submit should block since pool is full (size=1).
	submitted := make(chan struct{})
	go func() {
		pool.Submit(context.Background(), func(ctx context.Context) error {
			return nil
		})
		close(submitted)
	}()

	select {
	case <-submitted:
		t.Error("second submit should have blocked")
	case <-time.After(50 * time.Millisecond):
		// Good, it's blocking (backpressure).
	}

	close(block) // Unblock the first task.

	select {
	case <-submitted:
		// Good, second submit unblocked.
	case <-time.After(time.Second):
		t.Error("second submit did not unblock after first task completed")
	}

	pool.Wait()
}

func TestWorkerPool_PanicRecovery(t *testing.T) {
	pool := NewWorkerPool(2)
	defer pool.Shutdown()

	err := pool.Submit(context.Background(), func(ctx context.Context) error {
		panic("test panic")
	})
	if err != nil {
		t.Fatalf("unexpected submit error: %v", err)
	}

	pool.Wait()

	m := pool.Metrics()
	if m.Panics != 1 {
		t.Errorf("expected 1 panic, got %d", m.Panics)
	}
	if m.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", m.Failed)
	}

	// Pool should still work after panic.
	var ran int64
	err = pool.Submit(context.Background(), func(ctx context.Context) error {
		atomic.AddInt64(&ran, 1)
		return nil
	})
	if err != nil {
		t.Fatalf("submit after panic failed: %v", err)
	}

	pool.Wait()

	if atomic.LoadInt64(&ran) != 1 {
		t.Error("work after panic did not execute")
	}
}

func TestWorkerPool_ContextCancellation(t *testing.T) {
	pool := NewWorkerPool(1)
	defer pool.Shutdown()

	block := make(chan struct{})

	// Fill the pool.
	pool.Submit(context.Background(), func(ctx context.Context) error {
		<-block
		return nil
	})

	// Try to submit with a context that will be cancelled.
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- pool.Submit(ctx, func(ctx context.Context) error {
			return nil
		})
	}()

	// Give the goroutine time to start waiting.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("submit did not return after context cancellation")
	}

	close(block)
	pool.Wait()
}

func TestWorkerPool_GracefulShutdown(t *testing.T) {
	pool := NewWorkerPool(2)

	var completed int64
	for i := 0; i < 5; i++ {
		pool.Submit(context.Background(), func(ctx context.Context) error {
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt64(&completed, 1)
			return nil
		})
	}

	pool.Shutdown()

	if atomic.LoadInt64(&completed) != 5 {
		t.Errorf("expected 5 completed after shutdown, got %d", atomic.LoadInt64(&completed))
	}
}

func TestWorkerPool_SubmitAfterShutdown(t *testing.T) {
	pool := NewWorkerPool(2)
	pool.Shutdown()

	err := pool.Submit(context.Background(), func(ctx context.Context) error {
		return nil
	})
	if err != ErrPoolShutdown {
		t.Errorf("expected ErrPoolShutdown, got %v", err)
	}
}

func TestWorkerPool_MetricsAccuracy(t *testing.T) {
	pool := NewWorkerPool(4)
	defer pool.Shutdown()

	errTarget := errors.New("intentional error")

	// Submit 3 successful and 2 failing tasks.
	for i := 0; i < 3; i++ {
		pool.Submit(context.Background(), func(ctx context.Context) error {
			return nil
		})
	}
	for i := 0; i < 2; i++ {
		pool.Submit(context.Background(), func(ctx context.Context) error {
			return errTarget
		})
	}

	pool.Wait()

	m := pool.Metrics()
	if m.Completed != 3 {
		t.Errorf("expected 3 completed, got %d", m.Completed)
	}
	if m.Failed != 2 {
		t.Errorf("expected 2 failed, got %d", m.Failed)
	}
	if m.Active != 0 {
		t.Errorf("expected 0 active after wait, got %d", m.Active)
	}
}

func TestWorkerPool_MultipleConcurrentCompletions(t *testing.T) {
	pool := NewWorkerPool(10)
	defer pool.Shutdown()

	var completed int64
	count := 50

	for i := 0; i < count; i++ {
		pool.Submit(context.Background(), func(ctx context.Context) error {
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt64(&completed, 1)
			return nil
		})
	}

	pool.Wait()

	if atomic.LoadInt64(&completed) != int64(count) {
		t.Errorf("expected %d completed, got %d", count, atomic.LoadInt64(&completed))
	}

	m := pool.Metrics()
	if m.Completed != int64(count) {
		t.Errorf("expected metrics completed=%d, got %d", count, m.Completed)
	}
}

func TestWorkerPool_DoubleShutdown(t *testing.T) {
	pool := NewWorkerPool(2)
	pool.Shutdown()
	pool.Shutdown() // Should not panic.
}
