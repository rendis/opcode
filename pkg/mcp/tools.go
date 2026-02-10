package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
)

// handleRun executes a workflow from a template.
func (s *OpcodeServer) handleRun(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	templateName, err := req.RequireString("template_name")
	if err != nil {
		return mcp.NewToolResultError("template_name is required"), nil
	}
	agentID, err := req.RequireString("agent_id")
	if err != nil {
		return mcp.NewToolResultError("agent_id is required"), nil
	}
	version := req.GetString("version", "")
	params := mcp.ParseStringMap(req, "params", nil)

	// Ensure agent is registered.
	if regErr := s.ensureAgent(ctx, agentID); regErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to register agent: %v", regErr)), nil
	}

	// Resolve template.
	tpl, tplErr := s.resolveTemplate(ctx, templateName, version)
	if tplErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("template lookup failed: %v", tplErr)), nil
	}

	// Create workflow from template.
	now := time.Now().UTC()
	wf := &store.Workflow{
		ID:              uuid.New().String(),
		Name:            tpl.Name,
		TemplateName:    tpl.Name,
		TemplateVersion: tpl.Version,
		Definition:      tpl.Definition,
		Status:          schema.WorkflowStatusPending,
		AgentID:         agentID,
		InputParams:     params,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if createErr := s.store.CreateWorkflow(ctx, wf); createErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create workflow: %v", createErr)), nil
	}

	result, runErr := s.executor.Run(ctx, wf, params)
	if runErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("workflow execution failed: %v", runErr)), nil
	}

	return marshalResult(result)
}

// handleStatus returns the current state of a workflow.
func (s *OpcodeServer) handleStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	workflowID, err := req.RequireString("workflow_id")
	if err != nil {
		return mcp.NewToolResultError("workflow_id is required"), nil
	}

	status, statusErr := s.executor.Status(ctx, workflowID)
	if statusErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("status query failed: %v", statusErr)), nil
	}

	return marshalResult(status)
}

// handleSignal sends a signal to a suspended workflow.
func (s *OpcodeServer) handleSignal(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	workflowID, err := req.RequireString("workflow_id")
	if err != nil {
		return mcp.NewToolResultError("workflow_id is required"), nil
	}
	signalType, err := req.RequireString("signal_type")
	if err != nil {
		return mcp.NewToolResultError("signal_type is required"), nil
	}

	payload := mcp.ParseStringMap(req, "payload", nil)
	stepID := req.GetString("step_id", "")
	agentID := req.GetString("agent_id", "")
	reasoning := req.GetString("reasoning", "")

	signal := schema.Signal{
		Type:      schema.SignalType(signalType),
		StepID:    stepID,
		AgentID:   agentID,
		Payload:   payload,
		Reasoning: reasoning,
	}

	if sigErr := s.executor.Signal(ctx, workflowID, signal); sigErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("signal failed: %v", sigErr)), nil
	}

	return marshalResult(map[string]any{
		"ok":          true,
		"workflow_id": workflowID,
		"signal_type": signalType,
	})
}

// handleDefine registers a new workflow template with auto-versioning.
func (s *OpcodeServer) handleDefine(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("name is required"), nil
	}
	agentID, err := req.RequireString("agent_id")
	if err != nil {
		return mcp.NewToolResultError("agent_id is required"), nil
	}

	defRaw := mcp.ParseStringMap(req, "definition", nil)
	if defRaw == nil {
		return mcp.NewToolResultError("definition is required"), nil
	}

	// Marshal then unmarshal the definition to get a proper WorkflowDefinition.
	defBytes, marshalErr := json.Marshal(defRaw)
	if marshalErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid definition: %v", marshalErr)), nil
	}
	var def schema.WorkflowDefinition
	if unmarshalErr := json.Unmarshal(defBytes, &def); unmarshalErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid definition: %v", unmarshalErr)), nil
	}

	// Ensure agent is registered.
	if regErr := s.ensureAgent(ctx, agentID); regErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to register agent: %v", regErr)), nil
	}

	// Auto-increment version by finding latest existing.
	nextVersion := s.nextVersion(ctx, name)

	now := time.Now().UTC()
	tpl := &store.WorkflowTemplate{
		Name:        name,
		Version:     nextVersion,
		Description: req.GetString("description", ""),
		Definition:  def,
		AgentID:     agentID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Optional schemas and triggers as raw JSON.
	if inputSchema := mcp.ParseStringMap(req, "input_schema", nil); inputSchema != nil {
		if raw, err := json.Marshal(inputSchema); err == nil {
			tpl.InputSchema = raw
		}
	}
	if outputSchema := mcp.ParseStringMap(req, "output_schema", nil); outputSchema != nil {
		if raw, err := json.Marshal(outputSchema); err == nil {
			tpl.OutputSchema = raw
		}
	}
	if triggers := mcp.ParseStringMap(req, "triggers", nil); triggers != nil {
		if raw, err := json.Marshal(triggers); err == nil {
			tpl.Triggers = raw
		}
	}

	if storeErr := s.store.StoreTemplate(ctx, tpl); storeErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to store template: %v", storeErr)), nil
	}

	return marshalResult(map[string]any{
		"name":    name,
		"version": nextVersion,
	})
}

// handleQuery lists workflows, events, or templates based on filters.
func (s *OpcodeServer) handleQuery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resource, err := req.RequireString("resource")
	if err != nil {
		return mcp.NewToolResultError("resource is required"), nil
	}

	filter := mcp.ParseStringMap(req, "filter", nil)

	switch resource {
	case "workflows":
		return s.queryWorkflows(ctx, filter)
	case "events":
		return s.queryEvents(ctx, filter)
	case "templates":
		return s.queryTemplates(ctx, filter)
	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown resource type: %s", resource)), nil
	}
}

// --- Query helpers ---

func (s *OpcodeServer) queryWorkflows(ctx context.Context, filter map[string]any) (*mcp.CallToolResult, error) {
	wf := store.WorkflowFilter{
		Limit: extractInt(filter, "limit", 50),
	}
	if status, ok := filter["status"].(string); ok && status != "" {
		ws := schema.WorkflowStatus(status)
		wf.Status = &ws
	}
	if agentID, ok := filter["agent_id"].(string); ok {
		wf.AgentID = agentID
	}
	if since, ok := filter["since"].(string); ok && since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			wf.Since = &t
		}
	}

	workflows, err := s.store.ListWorkflows(ctx, wf)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query failed: %v", err)), nil
	}
	return marshalResult(workflows)
}

func (s *OpcodeServer) queryEvents(ctx context.Context, filter map[string]any) (*mcp.CallToolResult, error) {
	ef := store.EventFilter{
		Limit: extractInt(filter, "limit", 100),
	}
	if wfID, ok := filter["workflow_id"].(string); ok {
		ef.WorkflowID = wfID
	}
	if stepID, ok := filter["step_id"].(string); ok {
		ef.StepID = stepID
	}
	if eventType, ok := filter["event_type"].(string); ok {
		ef.EventType = eventType
	}
	if since, ok := filter["since"].(string); ok && since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			ef.Since = &t
		}
	}

	if ef.EventType != "" {
		// Filter by specific event type.
		events, err := s.store.GetEventsByType(ctx, ef.EventType, ef)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("query failed: %v", err)), nil
		}
		return marshalResult(events)
	}

	// No event type filter — use GetEvents (requires workflow_id).
	if ef.WorkflowID == "" {
		return mcp.NewToolResultError("event query requires either 'event_type' or 'workflow_id' in filter"), nil
	}
	var since int64
	events, err := s.store.GetEvents(ctx, ef.WorkflowID, since)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query failed: %v", err)), nil
	}
	return marshalResult(events)
}

func (s *OpcodeServer) queryTemplates(ctx context.Context, filter map[string]any) (*mcp.CallToolResult, error) {
	tf := store.TemplateFilter{
		Limit: extractInt(filter, "limit", 50),
	}
	if name, ok := filter["name"].(string); ok {
		tf.Name = name
	}
	if agentID, ok := filter["agent_id"].(string); ok {
		tf.AgentID = agentID
	}

	templates, err := s.store.ListTemplates(ctx, tf)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query failed: %v", err)), nil
	}
	return marshalResult(templates)
}

// --- Internal helpers ---

// resolveTemplate finds a template by name and optional version.
// If version is empty, it fetches the latest by listing all versions and sorting.
func (s *OpcodeServer) resolveTemplate(ctx context.Context, name, version string) (*store.WorkflowTemplate, error) {
	if version != "" {
		return s.store.GetTemplate(ctx, name, version)
	}

	// Find the latest version.
	templates, err := s.store.ListTemplates(ctx, store.TemplateFilter{Name: name})
	if err != nil {
		return nil, err
	}
	if len(templates) == 0 {
		return nil, fmt.Errorf("template %q not found", name)
	}

	// Sort by version descending (v2 > v1).
	sort.Slice(templates, func(i, j int) bool {
		return versionNum(templates[i].Version) > versionNum(templates[j].Version)
	})
	return templates[0], nil
}

// nextVersion computes the next version string (v1, v2, v3...) for a template name.
func (s *OpcodeServer) nextVersion(ctx context.Context, name string) string {
	templates, err := s.store.ListTemplates(ctx, store.TemplateFilter{Name: name})
	if err != nil || len(templates) == 0 {
		return "v1"
	}

	maxVer := 0
	for _, t := range templates {
		if n := versionNum(t.Version); n > maxVer {
			maxVer = n
		}
	}
	return fmt.Sprintf("v%d", maxVer+1)
}

// ensureAgent creates an agent record if it doesn't already exist.
func (s *OpcodeServer) ensureAgent(ctx context.Context, agentID string) error {
	_, err := s.store.GetAgent(ctx, agentID)
	if err == nil {
		// Agent exists, update last seen.
		return s.store.UpdateAgentSeen(ctx, agentID)
	}

	// Register new agent.
	now := time.Now().UTC()
	return s.store.RegisterAgent(ctx, &store.Agent{
		ID:        agentID,
		Name:      agentID,
		Type:      "llm",
		CreatedAt: now,
	})
}

// versionNum extracts the numeric part from a version string like "v3".
func versionNum(v string) int {
	v = strings.TrimPrefix(v, "v")
	n, _ := strconv.Atoi(v)
	return n
}

// extractInt safely extracts an integer from a filter map.
func extractInt(filter map[string]any, key string, defaultVal int) int {
	if filter == nil {
		return defaultVal
	}
	v, ok := filter[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case string:
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
	}
	return defaultVal
}

// marshalResult converts a value to a JSON text tool result.
func marshalResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}
	return mcp.NewToolResultJSON(json.RawMessage(data))
}
