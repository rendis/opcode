package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rendis/opcode/internal/store"
)

// mockSchedulerStore satisfies store.Store for scheduler tests.
type mockSchedulerStore struct {
	store.Store
	mu   sync.Mutex
	jobs map[string]*store.ScheduledJob
}

func newMockSchedulerStore() *mockSchedulerStore {
	return &mockSchedulerStore{jobs: make(map[string]*store.ScheduledJob)}
}

func (m *mockSchedulerStore) CreateScheduledJob(_ context.Context, job *store.ScheduledJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *job
	m.jobs[job.ID] = &cp
	return nil
}

func (m *mockSchedulerStore) GetScheduledJob(_ context.Context, id string) (*store.ScheduledJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, nil
	}
	cp := *j
	return &cp, nil
}

func (m *mockSchedulerStore) UpdateScheduledJob(_ context.Context, id string, update store.ScheduledJobUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil
	}
	if update.Enabled != nil {
		j.Enabled = *update.Enabled
	}
	if update.LastRunAt != nil {
		j.LastRunAt = update.LastRunAt
	}
	if update.NextRunAt != nil {
		j.NextRunAt = update.NextRunAt
	}
	if update.LastRunStatus != "" {
		j.LastRunStatus = update.LastRunStatus
	}
	return nil
}

func (m *mockSchedulerStore) ListScheduledJobs(_ context.Context, filter store.ScheduledJobFilter) ([]*store.ScheduledJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*store.ScheduledJob
	for _, j := range m.jobs {
		if filter.Enabled != nil && j.Enabled != *filter.Enabled {
			continue
		}
		if filter.AgentID != "" && j.AgentID != filter.AgentID {
			continue
		}
		cp := *j
		result = append(result, &cp)
	}
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

func (m *mockSchedulerStore) DeleteScheduledJob(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.jobs, id)
	return nil
}

// mockRunner tracks RunFromTemplate calls.
type mockRunner struct {
	mu    sync.Mutex
	calls []runCall
	err   error
}

type runCall struct {
	TemplateName string
	Version      string
	Params       map[string]any
	AgentID      string
}

func (r *mockRunner) RunFromTemplate(_ context.Context, templateName, version string, params map[string]any, agentID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, runCall{
		TemplateName: templateName,
		Version:      version,
		Params:       params,
		AgentID:      agentID,
	})
	return r.err
}

func (r *mockRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func newTestScheduler(s store.Store, runner SubWorkflowRunner) *Scheduler {
	return NewScheduler(s, runner, slog.Default())
}

// --- Tests ---

func TestCalculateNextRun(t *testing.T) {
	sched := newTestScheduler(newMockSchedulerStore(), &mockRunner{})
	from := time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC)

	// Every hour at minute 0.
	next, err := sched.CalculateNextRun("0 * * * *", from)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 2, 10, 13, 0, 0, 0, time.UTC), next)

	// Every 15 minutes.
	next, err = sched.CalculateNextRun("*/15 * * * *", from)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 2, 10, 12, 15, 0, 0, time.UTC), next)

	// Daily at midnight.
	next, err = sched.CalculateNextRun("0 0 * * *", from)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC), next)

	// Invalid expression.
	_, err = sched.CalculateNextRun("invalid cron", from)
	require.Error(t, err)
}

func TestTickRunsDueJobs(t *testing.T) {
	ms := newMockSchedulerStore()
	runner := &mockRunner{}
	sched := newTestScheduler(ms, runner)

	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Hour)

	// Create a due job.
	require.NoError(t, ms.CreateScheduledJob(ctx, &store.ScheduledJob{
		ID:             "job-1",
		TemplateName:   "deploy",
		CronExpression: "0 * * * *",
		AgentID:        "system",
		Enabled:        true,
		NextRunAt:      &past,
	}))

	sched.tick(ctx)

	assert.Equal(t, 1, runner.callCount())

	// Verify job was updated.
	got, _ := ms.GetScheduledJob(ctx, "job-1")
	assert.NotNil(t, got.LastRunAt)
	assert.NotNil(t, got.NextRunAt)
	assert.Equal(t, "success", got.LastRunStatus)
}

func TestTickSkipsNotDueJobs(t *testing.T) {
	ms := newMockSchedulerStore()
	runner := &mockRunner{}
	sched := newTestScheduler(ms, runner)

	ctx := context.Background()
	future := time.Now().UTC().Add(time.Hour)

	// Create a not-yet-due job.
	require.NoError(t, ms.CreateScheduledJob(ctx, &store.ScheduledJob{
		ID:             "job-future",
		TemplateName:   "deploy",
		CronExpression: "0 * * * *",
		AgentID:        "system",
		Enabled:        true,
		NextRunAt:      &future,
	}))

	sched.tick(ctx)

	assert.Equal(t, 0, runner.callCount())
}

func TestMissedRecovery(t *testing.T) {
	ms := newMockSchedulerStore()
	runner := &mockRunner{}
	sched := newTestScheduler(ms, runner)

	ctx := context.Background()
	past := time.Now().UTC().Add(-2 * time.Hour)

	require.NoError(t, ms.CreateScheduledJob(ctx, &store.ScheduledJob{
		ID:             "job-missed",
		TemplateName:   "cleanup",
		CronExpression: "0 * * * *",
		AgentID:        "system",
		Enabled:        true,
		NextRunAt:      &past,
	}))

	require.NoError(t, sched.RecoverMissed(ctx))

	assert.Equal(t, 1, runner.callCount())

	got, _ := ms.GetScheduledJob(ctx, "job-missed")
	assert.Equal(t, "success", got.LastRunStatus)
	assert.NotNil(t, got.NextRunAt)
	assert.True(t, got.NextRunAt.After(time.Now().UTC()))
}

func TestDisabledJobsSkipped(t *testing.T) {
	ms := newMockSchedulerStore()
	runner := &mockRunner{}
	sched := newTestScheduler(ms, runner)

	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Hour)

	require.NoError(t, ms.CreateScheduledJob(ctx, &store.ScheduledJob{
		ID:             "job-disabled",
		TemplateName:   "deploy",
		CronExpression: "0 * * * *",
		AgentID:        "system",
		Enabled:        false,
		NextRunAt:      &past,
	}))

	sched.tick(ctx)

	assert.Equal(t, 0, runner.callCount())
}

func TestJobUpdateAfterRun(t *testing.T) {
	ms := newMockSchedulerStore()
	runner := &mockRunner{}
	sched := newTestScheduler(ms, runner)

	ctx := context.Background()
	past := time.Now().UTC().Add(-30 * time.Minute)

	require.NoError(t, ms.CreateScheduledJob(ctx, &store.ScheduledJob{
		ID:             "job-update",
		TemplateName:   "process",
		CronExpression: "*/15 * * * *",
		Params:         json.RawMessage(`{"env":"staging"}`),
		AgentID:        "agent-1",
		Enabled:        true,
		NextRunAt:      &past,
	}))

	sched.tick(ctx)

	assert.Equal(t, 1, runner.callCount())
	runner.mu.Lock()
	call := runner.calls[0]
	runner.mu.Unlock()

	assert.Equal(t, "process", call.TemplateName)
	assert.Equal(t, "agent-1", call.AgentID)
	assert.Equal(t, "staging", call.Params["env"])

	got, _ := ms.GetScheduledJob(ctx, "job-update")
	assert.NotNil(t, got.LastRunAt)
	assert.NotNil(t, got.NextRunAt)
	assert.Equal(t, "success", got.LastRunStatus)
	// NextRunAt should be in the future.
	assert.True(t, got.NextRunAt.After(time.Now().UTC().Add(-time.Second)))
}

func TestJobRunFailure(t *testing.T) {
	ms := newMockSchedulerStore()
	runner := &mockRunner{err: assert.AnError}
	sched := newTestScheduler(ms, runner)

	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Hour)

	require.NoError(t, ms.CreateScheduledJob(ctx, &store.ScheduledJob{
		ID:             "job-fail",
		TemplateName:   "deploy",
		CronExpression: "0 * * * *",
		AgentID:        "system",
		Enabled:        true,
		NextRunAt:      &past,
	}))

	sched.tick(ctx)

	got, _ := ms.GetScheduledJob(ctx, "job-fail")
	assert.Equal(t, "error", got.LastRunStatus)
	assert.NotNil(t, got.NextRunAt)
}

func TestStartStop(t *testing.T) {
	ms := newMockSchedulerStore()
	runner := &mockRunner{}
	sched := newTestScheduler(ms, runner)

	ctx := context.Background()

	require.NoError(t, sched.Start(ctx))

	// Double start should error.
	err := sched.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already started")

	require.NoError(t, sched.Stop())

	// Stop again should be a no-op.
	require.NoError(t, sched.Stop())
}

func TestTickWithNilNextRunAt(t *testing.T) {
	ms := newMockSchedulerStore()
	runner := &mockRunner{}
	sched := newTestScheduler(ms, runner)

	ctx := context.Background()

	// Job with nil NextRunAt — should be run (treated as overdue).
	require.NoError(t, ms.CreateScheduledJob(ctx, &store.ScheduledJob{
		ID:             "job-nil-next",
		TemplateName:   "deploy",
		CronExpression: "0 * * * *",
		AgentID:        "system",
		Enabled:        true,
		NextRunAt:      nil,
	}))

	sched.tick(ctx)

	assert.Equal(t, 1, runner.callCount())
}

func TestDedupPreventsDoubleRun(t *testing.T) {
	ms := newMockSchedulerStore()
	runner := &mockRunner{}
	sched := newTestScheduler(ms, runner)

	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Hour)

	require.NoError(t, ms.CreateScheduledJob(ctx, &store.ScheduledJob{
		ID:             "job-dedup",
		TemplateName:   "deploy",
		CronExpression: "0 * * * *",
		AgentID:        "system",
		Enabled:        true,
		NextRunAt:      &past,
	}))

	// Pre-acquire the job to simulate an in-flight execution.
	acquired := sched.tryAcquire("job-dedup")
	assert.True(t, acquired)

	// Tick should skip the job because it's in-flight.
	sched.tick(ctx)
	assert.Equal(t, 0, runner.callCount())

	// Release and tick again — now it should run.
	sched.releaseJob("job-dedup")
	sched.tick(ctx)
	assert.Equal(t, 1, runner.callCount())
}

func TestDedupReleasedAfterTick(t *testing.T) {
	ms := newMockSchedulerStore()
	runner := &mockRunner{}
	sched := newTestScheduler(ms, runner)

	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Hour)

	require.NoError(t, ms.CreateScheduledJob(ctx, &store.ScheduledJob{
		ID:             "job-release",
		TemplateName:   "deploy",
		CronExpression: "0 * * * *",
		AgentID:        "system",
		Enabled:        true,
		NextRunAt:      &past,
	}))

	// Run once.
	sched.tick(ctx)
	assert.Equal(t, 1, runner.callCount())

	// Inflight should be released after tick completes.
	// Reset NextRunAt to past so it's due again.
	past2 := time.Now().UTC().Add(-time.Hour)
	require.NoError(t, ms.UpdateScheduledJob(ctx, "job-release", store.ScheduledJobUpdate{
		NextRunAt: &past2,
	}))

	sched.tick(ctx)
	assert.Equal(t, 2, runner.callCount())
}

func TestMultipleJobsSomeDue(t *testing.T) {
	ms := newMockSchedulerStore()
	runner := &mockRunner{}
	sched := newTestScheduler(ms, runner)

	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Hour)
	future := time.Now().UTC().Add(time.Hour)

	require.NoError(t, ms.CreateScheduledJob(ctx, &store.ScheduledJob{
		ID: "due-1", TemplateName: "alpha", CronExpression: "0 * * * *",
		AgentID: "sys", Enabled: true, NextRunAt: &past,
	}))
	require.NoError(t, ms.CreateScheduledJob(ctx, &store.ScheduledJob{
		ID: "not-due", TemplateName: "beta", CronExpression: "0 * * * *",
		AgentID: "sys", Enabled: true, NextRunAt: &future,
	}))
	require.NoError(t, ms.CreateScheduledJob(ctx, &store.ScheduledJob{
		ID: "due-2", TemplateName: "gamma", CronExpression: "0 * * * *",
		AgentID: "sys", Enabled: true, NextRunAt: nil,
	}))

	sched.tick(ctx)

	assert.Equal(t, 2, runner.callCount())
	runner.mu.Lock()
	names := make([]string, len(runner.calls))
	for i, c := range runner.calls {
		names[i] = c.TemplateName
	}
	runner.mu.Unlock()
	assert.Contains(t, names, "alpha")
	assert.Contains(t, names, "gamma")
	assert.NotContains(t, names, "beta")
}
