package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
)

// handleCreateTemplate creates a new workflow template with auto-versioning.
func (s *PanelServer) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var body struct {
		Name        string                    `json:"name"`
		Description string                    `json:"description"`
		Definition  schema.WorkflowDefinition `json:"definition"`
		AgentID     string                    `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	nextVer := s.nextVersion(ctx, body.Name)

	now := time.Now().UTC()
	tpl := &store.WorkflowTemplate{
		Name:        body.Name,
		Version:     nextVer,
		Description: body.Description,
		Definition:  body.Definition,
		AgentID:     body.AgentID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.deps.Store.StoreTemplate(ctx, tpl); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("store template: %v", err))
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"name":    body.Name,
		"version": nextVer,
	})
}

// handleDeleteTemplate deletes a specific template version.
func (s *PanelServer) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	// Store interface doesn't include DeleteTemplate yet.
	writeError(w, http.StatusNotImplemented, "template deletion not yet supported")
}

// handleResolveDecision resolves a pending decision.
func (s *PanelServer) handleResolveDecision(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	decisionID := r.PathValue("id")

	var body struct {
		Choice    string         `json:"choice"`
		Reasoning string         `json:"reasoning"`
		Data      map[string]any `json:"data"`
		AgentID   string         `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if body.Choice == "" {
		writeError(w, http.StatusBadRequest, "choice is required")
		return
	}

	resolution := &store.Resolution{
		Choice:     body.Choice,
		Reasoning:  body.Reasoning,
		Data:       body.Data,
		ResolvedBy: body.AgentID,
	}
	if resolution.ResolvedBy == "" {
		resolution.ResolvedBy = "panel"
	}

	if err := s.deps.Store.ResolveDecision(ctx, decisionID, resolution); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("resolve decision: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"ok":          "true",
		"decision_id": decisionID,
		"choice":      body.Choice,
	})
}

// handleCancelWorkflow cancels a running or suspended workflow.
func (s *PanelServer) handleCancelWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workflowID := r.PathValue("id")

	if err := s.deps.Executor.Cancel(ctx, workflowID, "cancelled via panel"); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("cancel workflow: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"ok":          "true",
		"workflow_id": workflowID,
	})
}

// handleRerunWorkflow creates a new workflow from the same template and params.
func (s *PanelServer) handleRerunWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workflowID := r.PathValue("id")

	original, err := s.deps.Store.GetWorkflow(ctx, workflowID)
	if err != nil || original == nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}

	now := time.Now().UTC()
	wf := &store.Workflow{
		ID:              uuid.New().String(),
		Name:            original.Name,
		TemplateName:    original.TemplateName,
		TemplateVersion: original.TemplateVersion,
		Definition:      original.Definition,
		Status:          schema.WorkflowStatusPending,
		AgentID:         original.AgentID,
		InputParams:     original.InputParams,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if createErr := s.deps.Store.CreateWorkflow(ctx, wf); createErr != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("create workflow: %v", createErr))
		return
	}

	result, runErr := s.deps.Executor.Run(ctx, wf, original.InputParams)
	if runErr != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("run workflow: %v", runErr))
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"workflow_id": wf.ID,
		"status":      result.Status,
	})
}

// handleCreateJob creates a new scheduled job.
func (s *PanelServer) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var body struct {
		TemplateName    string          `json:"template_name"`
		TemplateVersion string          `json:"template_version"`
		CronExpression  string          `json:"cron_expression"`
		Params          json.RawMessage `json:"params"`
		AgentID         string          `json:"agent_id"`
		Enabled         bool            `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if body.TemplateName == "" || body.CronExpression == "" {
		writeError(w, http.StatusBadRequest, "template_name and cron_expression are required")
		return
	}

	job := &store.ScheduledJob{
		ID:              uuid.New().String(),
		TemplateName:    body.TemplateName,
		TemplateVersion: body.TemplateVersion,
		CronExpression:  body.CronExpression,
		Params:          body.Params,
		AgentID:         body.AgentID,
		Enabled:         body.Enabled,
		CreatedAt:       time.Now().UTC(),
	}

	if err := s.deps.Store.CreateScheduledJob(ctx, job); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("create job: %v", err))
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"id": job.ID})
}

// handleUpdateJob updates a scheduled job.
func (s *PanelServer) handleUpdateJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	jobID := r.PathValue("id")

	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if err := s.deps.Store.UpdateScheduledJob(ctx, jobID, store.ScheduledJobUpdate{
		Enabled: body.Enabled,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("update job: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "id": jobID})
}

// handleDeleteJob deletes a scheduled job.
func (s *PanelServer) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	jobID := r.PathValue("id")

	if err := s.deps.Store.DeleteScheduledJob(ctx, jobID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("delete job: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "id": jobID})
}

// nextVersion computes the next version string (v1, v2, v3...) for a template name.
func (s *PanelServer) nextVersion(ctx context.Context, name string) string {
	templates, err := s.deps.Store.ListTemplates(ctx, store.TemplateFilter{Name: name, Limit: 100})
	if err != nil || len(templates) == 0 {
		return "v1"
	}

	sort.Slice(templates, func(i, j int) bool {
		return versionNum(templates[i].Version) > versionNum(templates[j].Version)
	})
	return fmt.Sprintf("v%d", versionNum(templates[0].Version)+1)
}

// versionNum extracts the numeric part from "v3" -> 3.
func versionNum(v string) int {
	v = strings.TrimPrefix(v, "v")
	n, _ := strconv.Atoi(v)
	return n
}
