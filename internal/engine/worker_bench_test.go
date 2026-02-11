package engine

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkWorkerPool(b *testing.B) {
	for _, size := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			benchWorkerPool(b, size)
		})
	}
}

func benchWorkerPool(b *testing.B, poolSize int) {
	pool := NewWorkerPool(poolSize)
	defer pool.Shutdown()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Submit(ctx, func(ctx context.Context) error {
			return nil
		})
	}
	pool.Wait()
}

func BenchmarkWorkerPool_Backpressure(b *testing.B) {
	for _, tasks := range []int{1000, 5000} {
		b.Run(fmt.Sprintf("pool=10_tasks=%d", tasks), func(b *testing.B) {
			pool := NewWorkerPool(10)
			defer pool.Shutdown()
			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for j := 0; j < tasks; j++ {
					pool.Submit(ctx, func(ctx context.Context) error {
						return nil
					})
				}
				pool.Wait()
			}
		})
	}
}

func BenchmarkWorkerPool_IOBound(b *testing.B) {
	pool := NewWorkerPool(50)
	defer pool.Shutdown()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 100; j++ {
			pool.Submit(ctx, func(ctx context.Context) error {
				time.Sleep(time.Microsecond) // Simulate minimal I/O
				return nil
			})
		}
		pool.Wait()
	}
}

func BenchmarkWorkerPool_Throughput(b *testing.B) {
	for _, size := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("pool=%d", size), func(b *testing.B) {
			pool := NewWorkerPool(size)
			defer pool.Shutdown()
			ctx := context.Background()

			var completed int64
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				pool.Submit(ctx, func(ctx context.Context) error {
					atomic.AddInt64(&completed, 1)
					return nil
				})
			}
			pool.Wait()
		})
	}
}
