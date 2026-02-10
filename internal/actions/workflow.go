package actions

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/internal/streaming"
	"github.com/rendis/opcode/pkg/schema"
)

// SubWorkflowRunner is a function that executes a child workflow and returns its output.
// The executor satisfies this by wiring it after construction (late-bind).
type SubWorkflowRunner func(ctx context.Context, templateName, version string, params map[string]any, parentID, agentID string) (json.RawMessage, error)

// WorkflowActionDeps holds the dependencies injected into workflow actions.
type WorkflowActionDeps struct {
	RunSubWorkflow SubWorkflowRunner
	Store          store.Store
	Hub            streaming.EventHub
	Logger         *slog.Logger
}

// WorkflowActions returns the five workflow-scoped actions.
func WorkflowActions(deps WorkflowActionDeps) []Action {
	return []Action{
		&workflowRunAction{deps: deps},
		&workflowEmitAction{deps: deps},
		&workflowContextAction{deps: deps},
		&workflowFailAction{},
		&workflowLogAction{deps: deps},
	}
}

// RegisterWorkflowActions registers workflow actions into the given registry.
// Called after the executor is created so the SubWorkflowRunner can be wired.
func RegisterWorkflowActions(reg *Registry, deps WorkflowActionDeps) error {
	for _, a := range WorkflowActions(deps) {
		if err := reg.Register(a); err != nil {
			return err
		}
	}
	return nil
}

// --- workflow.run ---

type workflowRunAction struct {
	deps WorkflowActionDeps
}

func (a *workflowRunAction) Name() string { return "workflow.run" }

func (a *workflowRunAction) Schema() ActionSchema {
	return ActionSchema{
		Description: "Execute a child workflow from a registered template.",
	}
}

func (a *workflowRunAction) Validate(input map[string]any) error {
	if stringParam(input, "template_name", "") == "" {
		return schema.NewError(schema.ErrCodeValidation, "workflow.run: missing required param 'template_name'")
	}
	return nil
}

func (a *workflowRunAction) Execute(ctx context.Context, input ActionInput) (*ActionOutput, error) {
	p := input.Params
	if p == nil {
		p = map[string]any{}
	}

	templateName := stringParam(p, "template_name", "")
	version := stringParam(p, "version", "")

	var params map[string]any
	if raw, ok := p["params"]; ok {
		if m, ok := raw.(map[string]any); ok {
			params = m
		}
	}

	parentID := stringParam(input.Context, "workflow_id", "")
	agentID := stringParam(input.Context, "agent_id", "")

	if a.deps.RunSubWorkflow == nil {
		return nil, schema.NewError(schema.ErrCodeExecution, "workflow.run: sub-workflow runner not configured")
	}

	result, err := a.deps.RunSubWorkflow(ctx, templateName, version, params, parentID, agentID)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "workflow.run: child workflow failed: %v", err).WithCause(err)
	}

	return &ActionOutput{Data: result}, nil
}

// --- workflow.emit ---

type workflowEmitAction struct {
	deps WorkflowActionDeps
}

func (a *workflowEmitAction) Name() string { return "workflow.emit" }

func (a *workflowEmitAction) Schema() ActionSchema {
	return ActionSchema{
		Description: "Publish a custom event to the EventHub for real-time subscribers.",
	}
}

func (a *workflowEmitAction) Validate(input map[string]any) error {
	if stringParam(input, "event_type", "") == "" {
		return schema.NewError(schema.ErrCodeValidation, "workflow.emit: missing required param 'event_type'")
	}
	return nil
}

func (a *workflowEmitAction) Execute(ctx context.Context, input ActionInput) (*ActionOutput, error) {
	p := input.Params
	if p == nil {
		p = map[string]any{}
	}

	eventType := stringParam(p, "event_type", "")
	var payload any
	if raw, ok := p["payload"]; ok {
		payload = raw
	}

	workflowID := stringParam(input.Context, "workflow_id", "")
	stepID := stringParam(input.Context, "step_id", "")

	if a.deps.Hub == nil {
		return nil, schema.NewError(schema.ErrCodeExecution, "workflow.emit: event hub not configured")
	}

	err := a.deps.Hub.Publish(ctx, streaming.StreamEvent{
		WorkflowID: workflowID,
		StepID:     stepID,
		EventType:  eventType,
		Payload:    payload,
	})
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "workflow.emit: publish failed: %v", err).WithCause(err)
	}

	out, _ := json.Marshal(map[string]any{"emitted": true})
	return &ActionOutput{Data: json.RawMessage(out)}, nil
}

// --- workflow.context ---

type workflowContextAction struct {
	deps WorkflowActionDeps
}

func (a *workflowContextAction) Name() string { return "workflow.context" }

func (a *workflowContextAction) Schema() ActionSchema {
	return ActionSchema{
		Description: "Read or update the workflow context (accumulated data, agent notes).",
	}
}

func (a *workflowContextAction) Validate(input map[string]any) error {
	action := stringParam(input, "action", "")
	if action != "read" && action != "update" {
		return schema.NewError(schema.ErrCodeValidation, "workflow.context: 'action' must be 'read' or 'update'")
	}
	if stringParam(input, "workflow_id", "") == "" {
		return schema.NewError(schema.ErrCodeValidation, "workflow.context: missing required param 'workflow_id'")
	}
	return nil
}

func (a *workflowContextAction) Execute(ctx context.Context, input ActionInput) (*ActionOutput, error) {
	p := input.Params
	if p == nil {
		p = map[string]any{}
	}

	action := stringParam(p, "action", "")
	workflowID := stringParam(p, "workflow_id", "")

	if a.deps.Store == nil {
		return nil, schema.NewError(schema.ErrCodeExecution, "workflow.context: store not configured")
	}

	switch action {
	case "read":
		wfCtx, err := a.deps.Store.GetWorkflowContext(ctx, workflowID)
		if err != nil {
			return nil, schema.NewErrorf(schema.ErrCodeExecution, "workflow.context: read failed: %v", err).WithCause(err)
		}
		data, err := json.Marshal(wfCtx)
		if err != nil {
			return nil, schema.NewErrorf(schema.ErrCodeExecution, "workflow.context: marshal failed").WithCause(err)
		}
		return &ActionOutput{Data: json.RawMessage(data)}, nil

	case "update":
		var dataRaw json.RawMessage
		if d, ok := p["data"]; ok {
			b, err := json.Marshal(d)
			if err != nil {
				return nil, schema.NewErrorf(schema.ErrCodeValidation, "workflow.context: invalid 'data'").WithCause(err)
			}
			dataRaw = b
		}

		agentNotes := stringParam(p, "agent_notes", "")
		agentID := stringParam(input.Context, "agent_id", "")

		wfCtx := &store.WorkflowContext{
			WorkflowID:      workflowID,
			AgentID:         agentID,
			AccumulatedData: dataRaw,
			AgentNotes:      agentNotes,
		}

		if err := a.deps.Store.UpsertWorkflowContext(ctx, wfCtx); err != nil {
			return nil, schema.NewErrorf(schema.ErrCodeExecution, "workflow.context: update failed: %v", err).WithCause(err)
		}

		out, _ := json.Marshal(map[string]any{"updated": true, "workflow_id": workflowID})
		return &ActionOutput{Data: json.RawMessage(out)}, nil

	default:
		return nil, schema.NewError(schema.ErrCodeValidation, "workflow.context: 'action' must be 'read' or 'update'")
	}
}

// --- workflow.fail ---

type workflowFailAction struct{}

func (a *workflowFailAction) Name() string { return "workflow.fail" }

func (a *workflowFailAction) Schema() ActionSchema {
	return ActionSchema{
		Description: "Force-fail the current workflow with a reason.",
	}
}

func (a *workflowFailAction) Validate(input map[string]any) error {
	if stringParam(input, "reason", "") == "" {
		return schema.NewError(schema.ErrCodeValidation, "workflow.fail: missing required param 'reason'")
	}
	return nil
}

func (a *workflowFailAction) Execute(_ context.Context, input ActionInput) (*ActionOutput, error) {
	p := input.Params
	if p == nil {
		p = map[string]any{}
	}

	reason := stringParam(p, "reason", "workflow.fail invoked")
	return nil, schema.NewError(schema.ErrCodeNonRetryable, reason)
}

// --- workflow.log ---

type workflowLogAction struct {
	deps WorkflowActionDeps
}

func (a *workflowLogAction) Name() string { return "workflow.log" }

func (a *workflowLogAction) Schema() ActionSchema {
	return ActionSchema{
		Description: "Write a structured log entry with workflow context.",
	}
}

func (a *workflowLogAction) Validate(input map[string]any) error {
	msg := stringParam(input, "message", "")
	if msg == "" {
		return schema.NewError(schema.ErrCodeValidation, "workflow.log: missing required param 'message'")
	}
	return nil
}

func (a *workflowLogAction) Execute(_ context.Context, input ActionInput) (*ActionOutput, error) {
	p := input.Params
	if p == nil {
		p = map[string]any{}
	}

	level := stringParam(p, "level", "info")
	message := stringParam(p, "message", "")

	workflowID := stringParam(input.Context, "workflow_id", "")
	stepID := stringParam(input.Context, "step_id", "")

	attrs := []any{
		slog.String("workflow_id", workflowID),
		slog.String("step_id", stepID),
	}

	if data, ok := p["data"]; ok {
		attrs = append(attrs, slog.Any("data", data))
	}

	logger := a.deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	switch level {
	case "debug":
		logger.Debug(message, attrs...)
	case "warn":
		logger.Warn(message, attrs...)
	case "error":
		logger.Error(message, attrs...)
	default:
		logger.Info(message, attrs...)
	}

	out, _ := json.Marshal(map[string]any{"logged": true})
	return &ActionOutput{Data: json.RawMessage(out)}, nil
}
