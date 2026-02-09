package store

import "context"

// Store defines the persistence layer contract.
// All implementations must be safe for concurrent use.
type Store interface {
	// Workflows
	CreateWorkflow(ctx context.Context, wf *Workflow) error
	GetWorkflow(ctx context.Context, id string) (*Workflow, error)
	UpdateWorkflow(ctx context.Context, id string, update WorkflowUpdate) error
	ListWorkflows(ctx context.Context, filter WorkflowFilter) ([]*Workflow, error)
	DeleteWorkflow(ctx context.Context, id string) error

	// Event Sourcing (append-only)
	AppendEvent(ctx context.Context, event *Event) error
	GetEvents(ctx context.Context, workflowID string, since int64) ([]*Event, error)
	GetEventsByType(ctx context.Context, eventType string, filter EventFilter) ([]*Event, error)

	// Step State (materialized view)
	UpsertStepState(ctx context.Context, state *StepState) error
	GetStepState(ctx context.Context, workflowID, stepID string) (*StepState, error)
	ListStepStates(ctx context.Context, workflowID string) ([]*StepState, error)

	// Workflow Context
	UpsertWorkflowContext(ctx context.Context, wfCtx *WorkflowContext) error
	GetWorkflowContext(ctx context.Context, workflowID string) (*WorkflowContext, error)

	// Pending Decisions
	CreateDecision(ctx context.Context, dec *PendingDecision) error
	ResolveDecision(ctx context.Context, id string, resolution *Resolution) error
	ListPendingDecisions(ctx context.Context, filter DecisionFilter) ([]*PendingDecision, error)

	// Agents
	RegisterAgent(ctx context.Context, agent *Agent) error
	GetAgent(ctx context.Context, id string) (*Agent, error)
	UpdateAgentSeen(ctx context.Context, id string) error

	// Secrets
	StoreSecret(ctx context.Context, key string, value []byte) error
	GetSecret(ctx context.Context, key string) ([]byte, error)
	DeleteSecret(ctx context.Context, key string) error

	// Templates
	StoreTemplate(ctx context.Context, tpl *WorkflowTemplate) error
	GetTemplate(ctx context.Context, name string, version string) (*WorkflowTemplate, error)
	ListTemplates(ctx context.Context, filter TemplateFilter) ([]*WorkflowTemplate, error)

	// Maintenance
	Migrate(ctx context.Context) error
	Vacuum(ctx context.Context) error

	// Lifecycle
	Close() error
}
