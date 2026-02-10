package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/rendis/opcode/internal/store"
)

// SubWorkflowRunner is the interface the scheduler uses to run workflows.
// Satisfied by the executor (avoids import cycle).
type SubWorkflowRunner interface {
	RunFromTemplate(ctx context.Context, templateName, version string, params map[string]any, agentID string) error
}

// Scheduler polls the store for due scheduled jobs and runs them.
type Scheduler struct {
	store  store.Store
	runner SubWorkflowRunner
	parser cron.Parser
	logger *slog.Logger
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex

	inflightMu sync.Mutex
	inflight   map[string]struct{} // job IDs currently executing (dedup)
}

// NewScheduler creates a new Scheduler.
func NewScheduler(s store.Store, runner SubWorkflowRunner, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		store:    s,
		runner:   runner,
		parser:   cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
		logger:   logger,
		inflight: make(map[string]struct{}),
	}
}

// Start launches the background scheduling loop with a 60s ticker.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.done != nil {
		s.mu.Unlock()
		return fmt.Errorf("scheduler already started")
	}

	schedCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.mu.Unlock()

	go s.loop(schedCtx)
	s.logger.Info("scheduler started")
	return nil
}

func (s *Scheduler) loop(ctx context.Context) {
	defer close(s.done)

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	// Run an initial tick immediately.
	s.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick checks all enabled jobs and runs those that are due.
func (s *Scheduler) tick(ctx context.Context) {
	enabled := true
	jobs, err := s.store.ListScheduledJobs(ctx, store.ScheduledJobFilter{Enabled: &enabled})
	if err != nil {
		s.logger.Error("failed to list scheduled jobs", slog.String("error", err.Error()))
		return
	}

	now := time.Now().UTC()
	for _, job := range jobs {
		if job.NextRunAt == nil || !job.NextRunAt.After(now) {
			if !s.tryAcquire(job.ID) {
				continue // already running (dedup)
			}
			if err := s.runJob(ctx, job, now); err != nil {
				s.logger.Error("failed to run scheduled job",
					slog.String("job_id", job.ID),
					slog.String("error", err.Error()),
				)
			}
			s.releaseJob(job.ID)
		}
	}
}

// runJob executes a scheduled job and updates its timestamps.
func (s *Scheduler) runJob(ctx context.Context, job *store.ScheduledJob, now time.Time) error {
	s.logger.Info("running scheduled job",
		slog.String("job_id", job.ID),
		slog.String("template", job.TemplateName),
	)

	// Parse params.
	var params map[string]any
	if len(job.Params) > 0 {
		if err := json.Unmarshal(job.Params, &params); err != nil {
			return s.updateJobStatus(ctx, job, now, "error")
		}
	}

	// Run via runner.
	err := s.runner.RunFromTemplate(ctx, job.TemplateName, job.TemplateVersion, params, job.AgentID)
	status := "success"
	if err != nil {
		status = "error"
		s.logger.Error("scheduled job execution failed",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()),
		)
	}

	return s.updateJobStatus(ctx, job, now, status)
}

func (s *Scheduler) updateJobStatus(ctx context.Context, job *store.ScheduledJob, now time.Time, status string) error {
	nextRun, err := s.CalculateNextRun(job.CronExpression, now)
	if err != nil {
		return fmt.Errorf("calculate next run for job %q: %w", job.ID, err)
	}

	return s.store.UpdateScheduledJob(ctx, job.ID, store.ScheduledJobUpdate{
		LastRunAt:     &now,
		NextRunAt:     &nextRun,
		LastRunStatus: status,
	})
}

// tryAcquire returns true and marks the job as in-flight if it is not already running.
func (s *Scheduler) tryAcquire(jobID string) bool {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	if _, ok := s.inflight[jobID]; ok {
		return false
	}
	s.inflight[jobID] = struct{}{}
	return true
}

// releaseJob removes the job from the in-flight set.
func (s *Scheduler) releaseJob(jobID string) {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	delete(s.inflight, jobID)
}

// CalculateNextRun computes the next run time for a cron expression.
func (s *Scheduler) CalculateNextRun(cronExpr string, from time.Time) (time.Time, error) {
	schedule, err := s.parser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cron expression %q: %w", cronExpr, err)
	}
	return schedule.Next(from), nil
}

// Stop gracefully shuts down the scheduler.
func (s *Scheduler) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel == nil {
		return nil
	}

	s.cancel()
	<-s.done
	s.cancel = nil
	s.done = nil

	s.logger.Info("scheduler stopped")
	return nil
}

// RecoverMissed checks for jobs that missed their next_run_at and runs them once.
func (s *Scheduler) RecoverMissed(ctx context.Context) error {
	enabled := true
	jobs, err := s.store.ListScheduledJobs(ctx, store.ScheduledJobFilter{Enabled: &enabled})
	if err != nil {
		return fmt.Errorf("list missed jobs: %w", err)
	}

	now := time.Now().UTC()
	recovered := 0
	for _, job := range jobs {
		if job.NextRunAt != nil && job.NextRunAt.Before(now) {
			if !s.tryAcquire(job.ID) {
				continue
			}
			if err := s.runJob(ctx, job, now); err != nil {
				s.logger.Error("failed to recover missed job",
					slog.String("job_id", job.ID),
					slog.String("error", err.Error()),
				)
				s.releaseJob(job.ID)
				continue
			}
			s.releaseJob(job.ID)
			recovered++
		}
	}

	if recovered > 0 {
		s.logger.Info("recovered missed jobs", slog.Int("count", recovered))
	}
	return nil
}
