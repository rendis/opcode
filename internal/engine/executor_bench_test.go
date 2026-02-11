package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
)

func newBenchEnv(poolSize int) *testEnv {
	ms := newMockStore()
	mel := &mockEventLog{store: ms}
	reg := newMockRegistry()

	exec := NewExecutor(ms, mel, reg, ExecutorConfig{PoolSize: poolSize})

	return &testEnv{
		store:    ms,
		eventLog: mel,
		registry: reg,
		executor: exec,
	}
}

func BenchmarkExecuteDAG_Parallel(b *testing.B) {
	for _, steps := range []int{10, 50, 100, 200, 500} {
		b.Run(fmt.Sprintf("steps=%d", steps), func(b *testing.B) {
			poolSize := steps
			if poolSize > 100 {
				poolSize = 100
			}
			benchExecuteParallel(b, steps, poolSize)
		})
	}
}

// benchExecuteParallel creates N independent steps (1 level, full parallel)
// and executes the workflow b.N times.
func benchExecuteParallel(b *testing.B, stepCount, poolSize int) {
	te := newBenchEnv(poolSize)
	te.registry.Register(&mockAction{name: "noop"})

	steps := make([]schema.StepDefinition, stepCount)
	for i := range steps {
		steps[i] = execActionStep(fmt.Sprintf("s%d", i), "noop")
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wfID := fmt.Sprintf("wf-bench-%d", i)
		wf := newWorkflow(wfID, steps...)
		te.store.CreateWorkflow(ctx, wf)
		te.executor.Run(ctx, wf, nil)
	}
}

func BenchmarkExecuteDAG_Sequential(b *testing.B) {
	for _, count := range []int{5, 10, 20, 50} {
		b.Run(fmt.Sprintf("steps=%d", count), func(b *testing.B) {
			te := newBenchEnv(4)
			te.registry.Register(&mockAction{name: "noop"})

			steps := make([]schema.StepDefinition, count)
			for i := range steps {
				var deps []string
				if i > 0 {
					deps = []string{fmt.Sprintf("s%d", i-1)}
				}
				steps[i] = schema.StepDefinition{
					ID:        fmt.Sprintf("s%d", i),
					Type:      schema.StepTypeAction,
					Action:    "noop",
					DependsOn: deps,
				}
			}

			ctx := context.Background()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				wfID := fmt.Sprintf("wf-seq-%d", i)
				wf := newWorkflow(wfID, steps...)
				te.store.CreateWorkflow(ctx, wf)
				te.executor.Run(ctx, wf, nil)
			}
		})
	}
}

func BenchmarkExecuteDAG_MixedLevels(b *testing.B) {
	// Diamond: start → {a, b, c} → merge
	te := newBenchEnv(10)
	te.registry.Register(&mockAction{name: "noop"})

	steps := []schema.StepDefinition{
		execActionStep("start", "noop"),
		{ID: "a", Type: schema.StepTypeAction, Action: "noop", DependsOn: []string{"start"}},
		{ID: "b", Type: schema.StepTypeAction, Action: "noop", DependsOn: []string{"start"}},
		{ID: "c", Type: schema.StepTypeAction, Action: "noop", DependsOn: []string{"start"}},
		{ID: "merge", Type: schema.StepTypeAction, Action: "noop", DependsOn: []string{"a", "b", "c"}},
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wfID := fmt.Sprintf("wf-diamond-%d", i)
		wf := newWorkflow(wfID, steps...)
		te.store.CreateWorkflow(ctx, wf)
		te.executor.Run(ctx, wf, nil)
	}
}

func BenchmarkExecuteDAG_ConcurrentWorkflows(b *testing.B) {
	for _, count := range []int{10, 50, 100} {
		b.Run(fmt.Sprintf("wf=%d", count), func(b *testing.B) {
			te := newBenchEnv(20)
			te.registry.Register(&mockAction{name: "noop"})

			steps := []schema.StepDefinition{
				execActionStep("s1", "noop"),
				execActionStep("s2", "noop"),
				execActionStep("s3", "noop"),
			}

			ctx := context.Background()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				done := make(chan struct{}, count)
				for j := 0; j < count; j++ {
					wfID := fmt.Sprintf("wf-par-%d-%d", i, j)
					wf := newWorkflow(wfID, steps...)
					te.store.CreateWorkflow(ctx, wf)
					go func(w *store.Workflow) {
						te.executor.Run(ctx, w, nil)
						done <- struct{}{}
					}(wf)
				}
				for j := 0; j < count; j++ {
					<-done
				}
			}
		})
	}
}
