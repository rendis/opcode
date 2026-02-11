package store

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/rendis/opcode/pkg/schema"
)

func newBenchStore(b *testing.B) (*LibSQLStore, *EventLog) {
	b.Helper()
	dir := b.TempDir()
	s, err := NewLibSQLStore("file:" + dir + "/bench.db")
	if err != nil {
		b.Fatal(err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = s.Close() })
	return s, NewEventLog(s)
}

func seedBenchAgent(b *testing.B, s *LibSQLStore) string {
	b.Helper()
	id := uuid.New().String()
	if err := s.RegisterAgent(context.Background(), &Agent{
		ID:   id,
		Name: "bench-agent",
		Type: "system",
	}); err != nil {
		b.Fatal(err)
	}
	return id
}

func seedBenchWorkflow(b *testing.B, s *LibSQLStore, agentID string) string {
	b.Helper()
	wfID := uuid.New().String()
	if err := s.CreateWorkflow(context.Background(), &Workflow{
		ID:      wfID,
		AgentID: agentID,
		Status:  schema.WorkflowStatusActive,
		Definition: schema.WorkflowDefinition{
			Steps: []schema.StepDefinition{
				{ID: "s1", Type: "action", Action: "noop"},
			},
		},
	}); err != nil {
		b.Fatal(err)
	}
	return wfID
}

func BenchmarkEventAppend_Sequential(b *testing.B) {
	s, el := newBenchStore(b)
	agentID := seedBenchAgent(b, s)
	wfID := seedBenchWorkflow(b, s, agentID)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		el.AppendEvent(ctx, &Event{
			WorkflowID: wfID,
			StepID:     "s1",
			Type:       schema.EventStepStarted,
			AgentID:    agentID,
		})
	}
}

func BenchmarkEventAppend_MultipleWorkflows(b *testing.B) {
	s, el := newBenchStore(b)
	agentID := seedBenchAgent(b, s)
	ctx := context.Background()

	// Pre-create 100 workflows.
	wfIDs := make([]string, 100)
	for i := range wfIDs {
		wfIDs[i] = seedBenchWorkflow(b, s, agentID)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wfID := wfIDs[i%len(wfIDs)]
		el.AppendEvent(ctx, &Event{
			WorkflowID: wfID,
			StepID:     "s1",
			Type:       schema.EventStepStarted,
			AgentID:    agentID,
		})
	}
}

func BenchmarkEventAppend_Concurrent(b *testing.B) {
	for _, writers := range []int{10, 50, 100} {
		b.Run(fmt.Sprintf("writers=%d", writers), func(b *testing.B) {
			benchEventAppendConcurrent(b, writers)
		})
	}
}

func benchEventAppendConcurrent(b *testing.B, writers int) {
	s, el := newBenchStore(b)
	agentID := seedBenchAgent(b, s)
	ctx := context.Background()

	// Each writer gets its own workflow to avoid sequence contention.
	wfIDs := make([]string, writers)
	for i := range wfIDs {
		wfIDs[i] = seedBenchWorkflow(b, s, agentID)
	}

	b.ResetTimer()
	var wg sync.WaitGroup
	perWriter := b.N / writers
	if perWriter == 0 {
		perWriter = 1
	}

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(wfID string) {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				el.AppendEvent(ctx, &Event{
					WorkflowID: wfID,
					StepID:     fmt.Sprintf("s%d", j%10),
					Type:       schema.EventStepStarted,
					AgentID:    agentID,
				})
			}
		}(wfIDs[w])
	}
	wg.Wait()
}

func BenchmarkEventReplay(b *testing.B) {
	for _, count := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("events=%d", count), func(b *testing.B) {
			s, el := newBenchStore(b)
			agentID := seedBenchAgent(b, s)
			wfID := seedBenchWorkflow(b, s, agentID)
			ctx := context.Background()

			// Pre-populate events.
			for i := 0; i < count; i++ {
				stepID := fmt.Sprintf("s%d", i%10)
				typ := schema.EventStepStarted
				if i%2 == 1 {
					typ = schema.EventStepCompleted
				}
				el.AppendEvent(ctx, &Event{
					WorkflowID: wfID,
					StepID:     stepID,
					Type:       typ,
					AgentID:    agentID,
				})
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				el.ReplayEvents(ctx, wfID)
			}
		})
	}
}
