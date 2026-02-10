package store

import (
	"encoding/json"
	"time"

	"github.com/rendis/opcode/pkg/schema"
)

// Workflow is the persisted representation of a workflow execution.
type Workflow struct {
	ID              string                   `json:"id"`
	Name            string                   `json:"name,omitempty"`
	TemplateName    string                   `json:"template_name,omitempty"`
	TemplateVersion string                   `json:"template_version,omitempty"`
	Definition      schema.WorkflowDefinition `json:"definition"`
	Status          schema.WorkflowStatus    `json:"status"`
	AgentID         string                   `json:"agent_id"`
	ParentID        string                   `json:"parent_workflow_id,omitempty"`
	InputParams     map[string]any           `json:"input_params,omitempty"`
	Output          json.RawMessage          `json:"output,omitempty"`
	Error           json.RawMessage          `json:"error,omitempty"`
	CreatedAt       time.Time                `json:"created_at"`
	StartedAt       *time.Time               `json:"started_at,omitempty"`
	CompletedAt     *time.Time               `json:"completed_at,omitempty"`
	UpdatedAt       time.Time                `json:"updated_at"`
}

// Event is an immutable entry in the event sourcing log.
type Event struct {
	ID         int64           `json:"id"`
	WorkflowID string          `json:"workflow_id"`
	StepID     string          `json:"step_id,omitempty"`
	Type       string          `json:"event_type"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	AgentID    string          `json:"agent_id,omitempty"`
	Timestamp  time.Time       `json:"timestamp"`
	Sequence   int64           `json:"sequence"`
}

// StepState is the materialized view of a step's current execution state.
type StepState struct {
	WorkflowID  string             `json:"workflow_id"`
	StepID      string             `json:"step_id"`
	Status      schema.StepStatus  `json:"status"`
	Input       json.RawMessage    `json:"input,omitempty"`
	Output      json.RawMessage    `json:"output,omitempty"`
	Error       json.RawMessage    `json:"error,omitempty"`
	RetryCount  int                `json:"retry_count"`
	StartedAt   *time.Time         `json:"started_at,omitempty"`
	CompletedAt *time.Time         `json:"completed_at,omitempty"`
	DurationMs  int64              `json:"duration_ms,omitempty"`
}

// WorkflowContext stores metadata scoped to a workflow execution.
type WorkflowContext struct {
	WorkflowID      string          `json:"workflow_id"`
	AgentID         string          `json:"agent_id"`
	OriginalIntent  string          `json:"original_intent"`
	DecisionsLog    json.RawMessage `json:"decisions_log,omitempty"`
	AccumulatedData json.RawMessage `json:"accumulated_data,omitempty"`
	AgentNotes      string          `json:"agent_notes,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// PendingDecision represents a reasoning node awaiting agent input.
type PendingDecision struct {
	ID            string          `json:"id"`
	WorkflowID    string          `json:"workflow_id"`
	StepID        string          `json:"step_id"`
	AgentID       string          `json:"agent_id,omitempty"`
	TargetAgentID string          `json:"target_agent_id,omitempty"`
	Context       json.RawMessage `json:"context"`
	Options       json.RawMessage `json:"options"`
	TimeoutAt     *time.Time      `json:"timeout_at,omitempty"`
	Fallback      string          `json:"fallback,omitempty"`
	Resolution    json.RawMessage `json:"resolution,omitempty"`
	ResolvedBy    string          `json:"resolved_by,omitempty"`
	ResolvedAt    *time.Time      `json:"resolved_at,omitempty"`
	Status        string          `json:"status"`
	CreatedAt     time.Time       `json:"created_at"`
}

// Resolution is the agent's response to a pending decision.
type Resolution struct {
	Choice     string         `json:"choice"`
	Reasoning  string         `json:"reasoning,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
	ResolvedBy string         `json:"resolved_by,omitempty"`
}

// Agent represents a registered agent identity.
type Agent struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Type       string          `json:"type"` // llm, system, human, service
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	LastSeenAt *time.Time      `json:"last_seen_at,omitempty"`
}

// WorkflowTemplate is a reusable workflow definition registered via opcode.define.
type WorkflowTemplate struct {
	Name         string                   `json:"name"`
	Version      string                   `json:"version"`
	Description  string                   `json:"description,omitempty"`
	Definition   schema.WorkflowDefinition `json:"definition"`
	InputSchema  json.RawMessage          `json:"input_schema,omitempty"`
	OutputSchema json.RawMessage          `json:"output_schema,omitempty"`
	Triggers     json.RawMessage          `json:"triggers,omitempty"`
	Permissions  json.RawMessage          `json:"permissions,omitempty"`
	AgentID      string                   `json:"agent_id"`
	CreatedAt    time.Time                `json:"created_at"`
	UpdatedAt    time.Time                `json:"updated_at"`
}

// Plugin represents a registered external action provider.
type Plugin struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Type            string          `json:"type"` // mcp
	Config          json.RawMessage `json:"config"`
	Status          string          `json:"status"` // active, inactive, error
	LastHealthCheck *time.Time      `json:"last_health_check,omitempty"`
	ErrorMessage    string          `json:"error_message,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

// Secret is an encrypted key-value entry.
type Secret struct {
	Key       string    `json:"key"`
	Value     []byte    `json:"-"` // encrypted, never serialized
	CreatedAt time.Time `json:"created_at"`
}

// ScheduledJob is a cron-triggered workflow execution.
type ScheduledJob struct {
	ID              string     `json:"id"`
	TemplateName    string     `json:"template_name"`
	TemplateVersion string     `json:"template_version,omitempty"`
	CronExpression  string     `json:"cron_expression"`
	Params          json.RawMessage `json:"params,omitempty"`
	AgentID         string     `json:"agent_id"`
	Enabled         bool       `json:"enabled"`
	LastRunAt       *time.Time `json:"last_run_at,omitempty"`
	NextRunAt       *time.Time `json:"next_run_at,omitempty"`
	LastRunStatus   string     `json:"last_run_status,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// AuditEntry is an immutable record of an agent action (flag-activated).
type AuditEntry struct {
	ID           int64           `json:"id"`
	AgentID      string          `json:"agent_id"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id,omitempty"`
	Details      json.RawMessage `json:"details,omitempty"`
	Timestamp    time.Time       `json:"timestamp"`
}

// --- Filter and update types ---

// WorkflowFilter specifies criteria for listing workflows.
type WorkflowFilter struct {
	Status  *schema.WorkflowStatus `json:"status,omitempty"`
	AgentID string                 `json:"agent_id,omitempty"`
	Since   *time.Time             `json:"since,omitempty"`
	Limit   int                    `json:"limit,omitempty"`
	Offset  int                    `json:"offset,omitempty"`
}

// WorkflowUpdate specifies mutable fields of a workflow.
type WorkflowUpdate struct {
	Status      *schema.WorkflowStatus `json:"status,omitempty"`
	Output      json.RawMessage        `json:"output,omitempty"`
	Error       json.RawMessage        `json:"error,omitempty"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
}

// EventFilter specifies criteria for listing events.
type EventFilter struct {
	WorkflowID string     `json:"workflow_id,omitempty"`
	StepID     string     `json:"step_id,omitempty"`
	EventType  string     `json:"event_type,omitempty"`
	Since      *time.Time `json:"since,omitempty"`
	Limit      int        `json:"limit,omitempty"`
}

// DecisionFilter specifies criteria for listing pending decisions.
type DecisionFilter struct {
	WorkflowID    string `json:"workflow_id,omitempty"`
	AgentID       string `json:"agent_id,omitempty"`
	TargetAgentID string `json:"target_agent_id,omitempty"`
	Status        string `json:"status,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

// TemplateFilter specifies criteria for listing templates.
type TemplateFilter struct {
	Name    string `json:"name,omitempty"`
	AgentID string `json:"agent_id,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// ScheduledJobUpdate specifies mutable fields of a scheduled job.
type ScheduledJobUpdate struct {
	Enabled       *bool      `json:"enabled,omitempty"`
	LastRunAt     *time.Time `json:"last_run_at,omitempty"`
	NextRunAt     *time.Time `json:"next_run_at,omitempty"`
	LastRunStatus string     `json:"last_run_status,omitempty"`
}

// ScheduledJobFilter specifies criteria for listing scheduled jobs.
type ScheduledJobFilter struct {
	Enabled *bool  `json:"enabled,omitempty"`
	AgentID string `json:"agent_id,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}
