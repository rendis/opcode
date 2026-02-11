package e2e

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rendis/opcode/internal/actions"
	"github.com/rendis/opcode/internal/engine"
	"github.com/rendis/opcode/internal/isolation"
	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/internal/streaming"
	"github.com/rendis/opcode/internal/validation"
	"github.com/rendis/opcode/pkg/schema"
)

// --- Example test harness ---

type exampleHarness struct {
	t        *testing.T
	store    *store.LibSQLStore
	eventLog *store.EventLog
	executor engine.Executor
	registry *actions.Registry
	hub      *streaming.MemoryHub
	tempDir  string
}

func newExampleHarness(t *testing.T) *exampleHarness {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "examples.db")
	s, err := store.NewLibSQLStore("file:" + dbPath)
	require.NoError(t, err)
	require.NoError(t, s.Migrate(context.Background()))
	t.Cleanup(func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	})

	el := store.NewEventLog(s)
	reg := actions.NewRegistry()
	validator, err := validation.NewJSONSchemaValidator()
	require.NoError(t, err)

	isolator := isolation.NewFallbackIsolator()

	fsDir := filepath.Join(dir, "fsdata")
	require.NoError(t, os.MkdirAll(fsDir, 0755))

	err = actions.RegisterBuiltins(reg, validator,
		actions.HTTPConfig{},
		actions.FSConfig{
			Limits: isolation.ResourceLimits{
				WritablePaths: []string{fsDir},
				ReadOnlyPaths: []string{fsDir, examplesDir()},
			},
		},
		actions.ShellConfig{Isolator: isolator},
	)
	require.NoError(t, err)

	hub := streaming.NewMemoryHub()
	exec := engine.NewExecutor(s, el, reg, engine.ExecutorConfig{PoolSize: 4})

	// Register workflow.* actions (workflow.log, workflow.emit, etc.)
	require.NoError(t, actions.RegisterWorkflowActions(reg, actions.WorkflowActionDeps{
		Store:  s,
		Hub:    hub,
		Logger: slog.Default(),
	}))

	require.NoError(t, s.RegisterAgent(context.Background(), &store.Agent{
		ID:        "example-agent",
		Name:      "Example Test Agent",
		Type:      "system",
		CreatedAt: time.Now().UTC(),
	}))

	return &exampleHarness{
		t:        t,
		store:    s,
		eventLog: el,
		executor: exec,
		registry: reg,
		hub:      hub,
		tempDir:  fsDir,
	}
}

func examplesDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "examples")
}

// loadWorkflow reads a WorkflowDefinition from examples/<name>/workflow.json
// and patches shell.exec steps with CWD pointing to the example directory.
func loadWorkflow(t *testing.T, name string) schema.WorkflowDefinition {
	t.Helper()
	path := filepath.Join(examplesDir(), name, "workflow.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read %s", path)

	// Patch shell.exec CWD to resolve scripts/ relative paths.
	exampleDir := filepath.Join(examplesDir(), name)
	data = patchShellCWD(data, exampleDir)

	var wrapper struct {
		Definition schema.WorkflowDefinition `json:"definition"`
	}
	require.NoError(t, json.Unmarshal(data, &wrapper), "failed to parse %s", path)
	return wrapper.Definition
}

// patchShellCWD walks raw JSON and adds "cwd" to any shell.exec step params.
func patchShellCWD(data []byte, cwd string) []byte {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return data
	}
	walkPatchCWD(raw, cwd)
	patched, err := json.Marshal(raw)
	if err != nil {
		return data
	}
	return patched
}

func walkPatchCWD(v any, cwd string) {
	switch val := v.(type) {
	case map[string]any:
		if action, ok := val["action"]; ok && action == "shell.exec" {
			if params, ok := val["params"].(map[string]any); ok {
				if _, hasCwd := params["cwd"]; !hasCwd {
					params["cwd"] = cwd
				}
			}
		}
		for _, child := range val {
			walkPatchCWD(child, cwd)
		}
	case []any:
		for _, child := range val {
			walkPatchCWD(child, cwd)
		}
	}
}

func mockServer(t *testing.T, response any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (h *exampleHarness) run(def schema.WorkflowDefinition, params map[string]any) *engine.ExecutionResult {
	h.t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	wf := &store.Workflow{
		ID:         uuid.New().String(),
		Name:       h.t.Name(),
		Definition: def,
		Status:     schema.WorkflowStatusPending,
		AgentID:    "example-agent",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	require.NoError(h.t, h.store.CreateWorkflow(ctx, wf))
	result, err := h.executor.Run(ctx, wf, params)
	require.NoError(h.t, err)
	return result
}

func (h *exampleHarness) runSuspended(def schema.WorkflowDefinition, params map[string]any) (*engine.ExecutionResult, string) {
	h.t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	wfID := uuid.New().String()
	wf := &store.Workflow{
		ID:          wfID,
		Name:        h.t.Name(),
		Definition:  def,
		Status:      schema.WorkflowStatusPending,
		AgentID:     "example-agent",
		InputParams: params,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(h.t, h.store.CreateWorkflow(ctx, wf))
	result, err := h.executor.Run(ctx, wf, params)
	require.NoError(h.t, err)
	return result, wfID
}

func (h *exampleHarness) signal(wfID, stepID, choice string) {
	h.t.Helper()
	h.signalAs(wfID, stepID, choice, "example-agent")
}

func (h *exampleHarness) signalAs(wfID, stepID, choice, agentID string) {
	h.t.Helper()
	err := h.executor.Signal(context.Background(), wfID, schema.Signal{
		Type:    schema.SignalDecision,
		StepID:  stepID,
		AgentID: agentID,
		Payload: map[string]any{"choice": choice},
	})
	require.NoError(h.t, err)
}

func (h *exampleHarness) resume(wfID string) *engine.ExecutionResult {
	h.t.Helper()
	result, err := h.executor.Resume(context.Background(), wfID)
	require.NoError(h.t, err)
	return result
}

func (h *exampleHarness) writeFile(name, content string) string {
	h.t.Helper()
	path := filepath.Join(h.tempDir, name)
	dir := filepath.Dir(path)
	require.NoError(h.t, os.MkdirAll(dir, 0755))
	require.NoError(h.t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func (h *exampleHarness) getStepStates(wfID string) map[string]*store.StepState {
	h.t.Helper()
	states, err := h.store.ListStepStates(context.Background(), wfID)
	require.NoError(h.t, err)
	result := make(map[string]*store.StepState, len(states))
	for _, s := range states {
		result[s.StepID] = s
	}
	return result
}

// --- Example Tests ---

// === Agent Ops ===

func TestExample_ContentSummarizer(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "content-summarizer")

	srv := mockServer(t, map[string]any{
		"title": "Test Article",
		"body":  "This is a test article about workflow automation.",
	})

	result, wfID := h.runSuspended(def, map[string]any{
		"url":         srv.URL,
		"output_path": filepath.Join(h.tempDir, "summary.txt"),
	})
	require.Equal(t, schema.WorkflowStatusSuspended, result.Status)

	h.signal(wfID, "review-quality", "approve")
	result = h.resume(wfID)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_MultiSourceResearch(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "multi-source-research")

	srvA := mockServer(t, map[string]any{"source": "a", "data": "alpha"})
	srvB := mockServer(t, map[string]any{"source": "b", "data": "beta"})
	srvC := mockServer(t, map[string]any{"source": "c", "data": "gamma"})

	result, wfID := h.runSuspended(def, map[string]any{
		"source_a_url": srvA.URL,
		"source_b_url": srvB.URL,
		"source_c_url": srvC.URL,
	})
	require.Equal(t, schema.WorkflowStatusSuspended, result.Status)

	h.signal(wfID, "pick-best", "source_a")
	result = h.resume(wfID)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_IterativeRefinement(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "iterative-refinement")

	result, wfID := h.runSuspended(def, map[string]any{
		"draft_content": "Draft text for review",
	})
	require.Equal(t, schema.WorkflowStatusSuspended, result.Status)

	// Reasoning is a top-level step (not inside loop).
	h.signal(wfID, "review", "approve")
	result = h.resume(wfID)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

// === DevOps / CI-CD ===

func TestExample_DeployGate(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "deploy-gate")

	deploySrv := mockServer(t, map[string]any{"deployed": true})

	// Reasoning is a top-level step with depends_on check-tests.
	result, wfID := h.runSuspended(def, map[string]any{
		"deploy_webhook_url":  deploySrv.URL,
		"deploy_environment": "staging",
	})
	if result.Status == schema.WorkflowStatusSuspended {
		h.signal(wfID, "approve-deploy", "approve")
		result = h.resume(wfID)
	}
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_LogAnomalyTriage(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "log-anomaly-triage")

	notifySrv := mockServer(t, map[string]any{"ok": true})
	logPath := h.writeFile("app.log", "2024-01-01 ERROR connection timeout\n2024-01-01 INFO normal\n")

	// Reasoning is a top-level step (triage).
	result, wfID := h.runSuspended(def, map[string]any{
		"log_path":         logPath,
		"notification_url": notifySrv.URL,
	})
	if result.Status == schema.WorkflowStatusSuspended {
		h.signal(wfID, "triage", "escalate")
		result = h.resume(wfID)
	}
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_HealthCheckSweep(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "health-check-sweep")

	srvA := mockServer(t, map[string]any{"status": "healthy"})
	srvB := mockServer(t, map[string]any{"status": "healthy"})
	srvC := mockServer(t, map[string]any{"status": "healthy"})

	result := h.run(def, map[string]any{
		"endpoint_a": srvA.URL,
		"endpoint_b": srvB.URL,
		"endpoint_c": srvC.URL,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

// === Data Pipelines ===

func TestExample_ETLWithValidation(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "etl-with-validation")

	srcSrv := mockServer(t, map[string]any{
		"records": []map[string]any{
			{"id": 1, "name": "Alice"},
			{"id": 2, "name": "Bob"},
		},
	})
	dstSrv := mockServer(t, map[string]any{"inserted": 2})

	result := h.run(def, map[string]any{
		"source_url":      srcSrv.URL,
		"destination_url": dstSrv.URL,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_BatchFileProcessor(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "batch-file-processor")

	h.writeFile("input/file1.txt", "hello")
	h.writeFile("input/file2.txt", "world")

	result := h.run(def, map[string]any{
		"input_dir":  filepath.Join(h.tempDir, "input"),
		"output_dir": filepath.Join(h.tempDir, "output"),
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_SyncDriftDetection(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "sync-drift-detection")

	srvA := mockServer(t, map[string]any{"version": 1, "data": "alpha"})
	srvB := mockServer(t, map[string]any{"version": 2, "data": "beta"})
	syncSrv := mockServer(t, map[string]any{"synced": true})

	// Reasoning is inside condition check-drift.true.
	result, wfID := h.runSuspended(def, map[string]any{
		"source_a_url": srvA.URL,
		"source_b_url": srvB.URL,
		"sync_url":     syncSrv.URL,
	})
	if result.Status == schema.WorkflowStatusSuspended {
		h.signal(wfID, "check-drift.true.triage-drift", "sync")
		result = h.resume(wfID)
	}
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

// === Security / Compliance ===

func TestExample_SecretRotation(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "secret-rotation")

	updateSrv := mockServer(t, map[string]any{"updated": true, "status": "ok"})

	result := h.run(def, map[string]any{
		"service_url":  updateSrv.URL,
		"service_name": "test-service",
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_DependencyAudit(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "dependency-audit")

	notifySrv := mockServer(t, map[string]any{"notified": true})

	// Reasoning is a top-level step with depends_on check-vulns.
	result, wfID := h.runSuspended(def, map[string]any{
		"notification_url": notifySrv.URL,
	})
	if result.Status == schema.WorkflowStatusSuspended {
		h.signal(wfID, "triage", "patch_now")
		result = h.resume(wfID)
	}
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

// === Agentic / Human-in-the-Loop ===

func TestExample_ApprovalChain(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "approval-chain")

	result, wfID := h.runSuspended(def, map[string]any{
		"request_id":      "REQ-001",
		"request_summary": "Test approval request",
	})
	require.Equal(t, schema.WorkflowStatusSuspended, result.Status)

	h.signal(wfID, "reviewer1", "approve")
	result = h.resume(wfID)
	require.Equal(t, schema.WorkflowStatusSuspended, result.Status)

	h.signal(wfID, "reviewer2", "approve")
	result = h.resume(wfID)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_FreeFormDecision(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "free-form-decision")

	fetchSrv := mockServer(t, map[string]any{"data": "context data for assessment"})
	alertSrv := mockServer(t, map[string]any{"alerted": true})

	result, wfID := h.runSuspended(def, map[string]any{
		"data_url":  fetchSrv.URL,
		"alert_url": alertSrv.URL,
	})
	require.Equal(t, schema.WorkflowStatusSuspended, result.Status)

	// Free-form: any text as choice.
	h.signal(wfID, "assess", "no critical issues detected")
	result = h.resume(wfID)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_EscalationLadder(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "escalation-ladder")

	// Mock returns 200 so auto-check passes → condition false → no reasoning.
	checkSrv := mockServer(t, map[string]any{"status": "ok"})
	escalateSrv := mockServer(t, map[string]any{"escalated": true})

	result := h.run(def, map[string]any{
		"health_url":            checkSrv.URL,
		"escalation_webhook_url": escalateSrv.URL,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

// === Daily / Management ===

func TestExample_StandupReport(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "standup-report")

	ticketsSrv := mockServer(t, map[string]any{"tickets": []map[string]any{{"id": 1, "title": "Bug fix"}}})
	prsSrv := mockServer(t, map[string]any{"prs": []map[string]any{{"id": 10, "title": "Feature A"}}})
	logsSrv := mockServer(t, map[string]any{"entries": []string{"deploy v1.2"}})

	result := h.run(def, map[string]any{
		"tickets_url": ticketsSrv.URL,
		"prs_url":     prsSrv.URL,
		"logs_url":    logsSrv.URL,
		"output_path": filepath.Join(h.tempDir, "standup.md"),
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_TaskTriage(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "task-triage")

	// Return a plain array so loop's over expression iterates correctly.
	ticketsSrv := mockServer(t, []map[string]any{
		{"id": 1, "title": "Server down", "description": "Production unresponsive"},
	})
	updateSrv := mockServer(t, map[string]any{"updated": true})

	// Reasoning inside a loop does not suspend the workflow — the loop continues
	// past the suspended sub-step. The workflow completes after the loop finishes.
	result := h.run(def, map[string]any{
		"tickets_url": ticketsSrv.URL,
		"update_url":  updateSrv.URL,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_MeetingPrepBrief(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "meeting-prep-brief")

	calendarSrv := mockServer(t, map[string]any{"events": []map[string]any{{"title": "Standup"}}})
	docsSrv := mockServer(t, map[string]any{"docs": []string{"design-doc.md"}})
	notesSrv := mockServer(t, map[string]any{"notes": "Previous meeting notes..."})

	result := h.run(def, map[string]any{
		"calendar_url":  calendarSrv.URL,
		"documents_url": docsSrv.URL,
		"notes_url":     notesSrv.URL,
		"output_path":   filepath.Join(h.tempDir, "brief.md"),
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_KnowledgeBaseUpdater(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "knowledge-base-updater")

	srcSrv := mockServer(t, map[string]any{"content": "new knowledge content"})
	kbPath := h.writeFile("kb.md", "# Knowledge Base\n")

	// Reasoning inside condition check-changed.true.
	result, wfID := h.runSuspended(def, map[string]any{
		"source_url": srcSrv.URL,
		"last_hash":  "old-hash-value",
		"kb_path":    kbPath,
	})
	if result.Status == schema.WorkflowStatusSuspended {
		h.signal(wfID, "check-changed.true.review-relevance", "relevant")
		result = h.resume(wfID)
	}
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_OnboardingChecklist(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "onboarding-checklist")

	// Reasoning is a top-level step (review-results) after the loop.
	result, wfID := h.runSuspended(def, map[string]any{
		"checklist_items": []any{"setup-ssh", "install-tools"},
	})
	if result.Status == schema.WorkflowStatusSuspended {
		h.signal(wfID, "review-results", "approve")
		result = h.resume(wfID)
	}
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_NotificationRouter(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "notification-router")

	slackSrv := mockServer(t, map[string]any{"ok": true})
	emailSrv := mockServer(t, map[string]any{"sent": true})

	// Use "warning" severity to avoid the critical branch with reasoning.
	result := h.run(def, map[string]any{
		"severity":         "warning",
		"title":            "Disk Usage Alert",
		"message":          "Disk usage above 80%",
		"slack_webhook_url": slackSrv.URL,
		"email_api_url":    emailSrv.URL,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

// === Integrations / Sync ===

func TestExample_WebhookHandler(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "webhook-handler")

	destSrv := mockServer(t, map[string]any{"received": true})

	result := h.run(def, map[string]any{
		"payload": map[string]any{
			"event": "user.created",
			"data":  map[string]any{"id": 123, "name": "Alice"},
		},
		"destination_url": destSrv.URL,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_LeadEnrichment(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "lead-enrichment")

	companySrv := mockServer(t, map[string]any{"company": "Acme Inc", "size": 100})
	socialSrv := mockServer(t, map[string]any{"linkedin": "linkedin.com/alice"})
	crmSrv := mockServer(t, map[string]any{"updated": true})

	result := h.run(def, map[string]any{
		"company_lookup_url": companySrv.URL,
		"social_lookup_url":  socialSrv.URL,
		"crm_url":            crmSrv.URL,
		"lead_email":         "alice@example.com",
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_TwoWaySync(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "two-way-sync")

	srvA := mockServer(t, map[string]any{"records": []map[string]any{{"id": 1, "v": 1}}})
	srvB := mockServer(t, map[string]any{"records": []map[string]any{{"id": 1, "v": 2}}})
	upsertSrv := mockServer(t, map[string]any{"upserted": true})

	result := h.run(def, map[string]any{
		"system_a_url": srvA.URL,
		"system_b_url": srvB.URL,
		"upsert_url":   upsertSrv.URL,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_FormProcessor(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "form-processor")

	ticketSrv := mockServer(t, map[string]any{"ticket_id": 456})
	crmSrv := mockServer(t, map[string]any{"lead_id": 789})
	inboxSrv := mockServer(t, map[string]any{"received": true})

	result := h.run(def, map[string]any{
		"form_data": map[string]any{
			"name":    "Alice",
			"email":   "alice@example.com",
			"type":    "support",
			"message": "Need help with billing",
		},
		"ticket_api_url": ticketSrv.URL,
		"crm_api_url":    crmSrv.URL,
		"inbox_api_url":  inboxSrv.URL,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

// === Communication ===

func TestExample_AlertPipeline(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "alert-pipeline")

	slackSrv := mockServer(t, map[string]any{"ok": true})
	emailSrv := mockServer(t, map[string]any{"sent": true})

	result := h.run(def, map[string]any{
		"severity":          "warning",
		"title":             "High memory usage",
		"message":           "Memory at 90%",
		"slack_dm_url":      slackSrv.URL,
		"slack_channel_url": slackSrv.URL,
		"email_api_url":     emailSrv.URL,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_DigestBuilder(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "digest-builder")

	src1 := mockServer(t, map[string]any{"title": "News 1", "summary": "First item"})
	src2 := mockServer(t, map[string]any{"title": "News 2", "summary": "Second item"})
	sendSrv := mockServer(t, map[string]any{"sent": true})

	result := h.run(def, map[string]any{
		"sources":      []any{src1.URL, src2.URL},
		"send_api_url": sendSrv.URL,
		"recipient":    "team@example.com",
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_AutoResponder(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "auto-responder")

	respondSrv := mockServer(t, map[string]any{"sent": true})

	// Use FAQ keyword match to avoid reasoning branch.
	result := h.run(def, map[string]any{
		"message":           "How do I reset my password?",
		"faq_keywords":      []any{"password", "billing", "refund"},
		"respond_api_url":   respondSrv.URL,
		"sender_id":         "user-123",
		"template_response": "Please visit our password reset page.",
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

// === E-Commerce / Business ===

func TestExample_OrderProcessing(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "order-processing")

	stockSrv := mockServer(t, map[string]any{"available": true, "quantity": 10})
	paymentSrv := mockServer(t, map[string]any{"payment_id": "pay_123", "status": "success"})
	fulfillSrv := mockServer(t, map[string]any{"order_id": "ord_456"})
	notifySrv := mockServer(t, map[string]any{"sent": true})

	result := h.run(def, map[string]any{
		"order": map[string]any{
			"id":       "ord_456",
			"product":  "Widget",
			"quantity": 2,
			"price":    29.99,
		},
		"stock_api_url":        stockSrv.URL,
		"payment_api_url":      paymentSrv.URL,
		"fulfillment_api_url":  fulfillSrv.URL,
		"notification_api_url": notifySrv.URL,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_InvoiceGenerator(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "invoice-generator")

	orderSrv := mockServer(t, map[string]any{
		"id":    "ord_789",
		"items": []map[string]any{{"name": "Widget", "qty": 2, "price": 29.99}},
		"total": 59.98,
	})
	emailSrv := mockServer(t, map[string]any{"sent": true})

	result := h.run(def, map[string]any{
		"order_api_url":   orderSrv.URL,
		"order_id":        "ord_789",
		"output_path":     filepath.Join(h.tempDir, "invoice.json"),
		"email_api_url":   emailSrv.URL,
		"recipient_email": "billing@example.com",
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_PriceMonitor(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "price-monitor")

	productSrv := mockServer(t, map[string]any{"price": 19.99, "name": "Widget"})
	notifySrv := mockServer(t, map[string]any{"notified": true})

	result := h.run(def, map[string]any{
		"products": []any{
			map[string]any{"name": "Widget", "url": productSrv.URL, "threshold": 25.00},
		},
		"notification_api_url": notifySrv.URL,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

// === Content / Publishing ===

func TestExample_ContentPipeline(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "content-pipeline")

	publishSrv := mockServer(t, map[string]any{"published": true})
	slackSrv := mockServer(t, map[string]any{"ok": true})
	emailSrv := mockServer(t, map[string]any{"sent": true})

	result, wfID := h.runSuspended(def, map[string]any{
		"content":           "Draft blog post about opcode workflows.",
		"publish_api_url":   publishSrv.URL,
		"slack_webhook_url": slackSrv.URL,
		"email_api_url":     emailSrv.URL,
	})
	require.Equal(t, schema.WorkflowStatusSuspended, result.Status)

	h.signal(wfID, "review-content", "publish")
	result = h.resume(wfID)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_ImageProcessing(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "image-processing")

	cdnSrv := mockServer(t, map[string]any{"url": "https://cdn.example.com/img.jpg"})
	srcPath := h.writeFile("image.jpg", "fake-image-data")

	result := h.run(def, map[string]any{
		"source_path":    srcPath,
		"output_path":    filepath.Join(h.tempDir, "processed.jpg"),
		"cdn_upload_url": cdnSrv.URL,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_SocialCrossPost(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "social-cross-post")

	twitterSrv := mockServer(t, map[string]any{"id": "tw_123"})
	linkedinSrv := mockServer(t, map[string]any{"id": "li_456"})
	blogSrv := mockServer(t, map[string]any{"id": "blog_789"})

	result := h.run(def, map[string]any{
		"content":          "Check out our new workflow engine!",
		"title":            "New Release",
		"twitter_api_url":  twitterSrv.URL,
		"linkedin_api_url": linkedinSrv.URL,
		"blog_api_url":     blogSrv.URL,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

// === Monitoring / Ops ===

func TestExample_ErrorAggregator(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "error-aggregator")

	logsSrv := mockServer(t, map[string]any{
		"entries": []map[string]any{
			{"level": "error", "message": "connection timeout", "count": 5},
		},
	})
	ticketSrv := mockServer(t, map[string]any{"ticket_id": "TK-100"})

	// Reasoning inside condition check-new-errors.true.
	result, wfID := h.runSuspended(def, map[string]any{
		"error_logs_url": logsSrv.URL,
		"ticket_api_url": ticketSrv.URL,
	})
	if result.Status == schema.WorkflowStatusSuspended {
		h.signal(wfID, "check-new-errors.true.triage-decision", "investigate")
		result = h.resume(wfID)
	}
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_UptimeMonitor(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "uptime-monitor")

	endpoint1 := mockServer(t, map[string]any{"status": "ok"})
	endpoint2 := mockServer(t, map[string]any{"status": "ok"})
	alertSrv := mockServer(t, map[string]any{"alerted": true})

	result := h.run(def, map[string]any{
		"endpoints": []any{
			map[string]any{"name": "Service 1", "url": endpoint1.URL},
			map[string]any{"name": "Service 2", "url": endpoint2.URL},
		},
		"alert_webhook_url": alertSrv.URL,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_BackupVerification(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "backup-verification")

	// No backup files in tempDir, so check-backup outputs exists=false.
	// Condition goes to default branch (log-missing), no reasoning triggers.
	result := h.run(def, map[string]any{
		"backup_dir":    h.tempDir,
		"max_age_hours": 24,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

// === Data Transformation ===

func TestExample_CSVToAPI(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "csv-to-api")

	apiSrv := mockServer(t, map[string]any{"inserted": true})
	csvPath := h.writeFile("data.csv", "name,age\nAlice,30\nBob,25\n")

	result := h.run(def, map[string]any{
		"csv_path": csvPath,
		"api_url":  apiSrv.URL,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_DataAnonymizer(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "data-anonymizer")

	inputPath := h.writeFile("users.json", `[{"name":"Alice","email":"alice@test.com"},{"name":"Bob","email":"bob@test.com"}]`)

	result := h.run(def, map[string]any{
		"input_path":  inputPath,
		"output_path": filepath.Join(h.tempDir, "anonymized.json"),
		"pii_fields":  []any{"email"},
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_ReportGenerator(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "report-generator")

	salesSrv := mockServer(t, map[string]any{"revenue": 50000, "orders": 120})
	usersSrv := mockServer(t, map[string]any{"total_users": 500, "active": 350})
	metricsSrv := mockServer(t, map[string]any{"uptime": 99.9, "latency_ms": 45})
	emailSrv := mockServer(t, map[string]any{"sent": true})

	result := h.run(def, map[string]any{
		"sales_api_url":   salesSrv.URL,
		"users_api_url":   usersSrv.URL,
		"metrics_api_url": metricsSrv.URL,
		"email_api_url":   emailSrv.URL,
		"report_path":     filepath.Join(h.tempDir, "report.json"),
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

// === User Lifecycle ===

func TestExample_OnboardingDrip(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "onboarding-drip")

	emailSrv := mockServer(t, map[string]any{"sent": true})

	result, wfID := h.runSuspended(def, map[string]any{
		"user_email":    "alice@example.com",
		"user_name":     "Alice",
		"email_api_url": emailSrv.URL,
	})
	require.Equal(t, schema.WorkflowStatusSuspended, result.Status)

	h.signal(wfID, "engagement-decision", "send-advanced")
	result = h.resume(wfID)
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

// === Git / Version Control ===

func TestExample_GitChangelog(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "git-changelog")

	// Reasoning inside condition check-breaking.true.
	result, wfID := h.runSuspended(def, map[string]any{
		"repo_dir":      h.tempDir,
		"changelog_path": filepath.Join(h.tempDir, "CHANGELOG.md"),
	})
	if result.Status == schema.WorkflowStatusSuspended {
		h.signal(wfID, "check-breaking.true.release-decision", "minor")
		result = h.resume(wfID)
	}
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_GitBranchCleanup(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "git-branch-cleanup")

	result := h.run(def, map[string]any{
		"repo_dir": h.tempDir,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_OpenclawVersionChecker(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "openclaw-version-checker")

	notifySrv := mockServer(t, map[string]any{"notified": true})

	result := h.run(def, map[string]any{
		"repo_url":        "https://github.com/example/openclaw",
		"current_version": "v1.0.0",
		"install_dir":     h.tempDir,
		"notify_url":      notifySrv.URL,
	})
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_RepoHealthCheck(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "repo-health-check")

	// Reasoning inside condition evaluate-results.default.
	result, wfID := h.runSuspended(def, map[string]any{
		"repo_dir": h.tempDir,
	})
	if result.Status == schema.WorkflowStatusSuspended {
		h.signal(wfID, "evaluate-results.default.issue-decision", "investigate")
		result = h.resume(wfID)
	}
	assert.Equal(t, schema.WorkflowStatusCompleted, result.Status)
}

func TestExample_MultiAgentReview(t *testing.T) {
	h := newExampleHarness(t)
	def := loadWorkflow(t, "multi-agent-review")

	// Mock http.get response with items containing scores.
	mockData := map[string]any{
		"items": []map[string]any{
			{"id": 1, "score": 0.9, "title": "High quality item"},
			{"id": 2, "score": 0.5, "title": "Low quality item"},
			{"id": 3, "score": 0.85, "title": "Good item"},
			{"id": 4, "score": 0.3, "title": "Poor item"},
		},
	}
	srv := mockServer(t, mockData)

	// Execute workflow with threshold 0.8 - should filter to 2 items.
	result, wfID := h.runSuspended(def, map[string]any{
		"data_url":  srv.URL,
		"threshold": 0.8,
	})

	// Debug: print failure details if not suspended
	if result.Status != schema.WorkflowStatusSuspended {
		t.Logf("Workflow failed. Error: %v", result.Error)
	}

	// Fetch step states once — used for both debug logging and assertions.
	steps := h.getStepStates(wfID)
	if result.Status != schema.WorkflowStatusSuspended {
		for id, step := range steps {
			if step.Status == schema.StepStatusFailed {
				t.Logf("Failed step %s: %s", id, string(step.Error))
			}
		}
	}

	require.Equal(t, schema.WorkflowStatusSuspended, result.Status)

	// Verify analyze step completed successfully with expr.eval action.
	analyzeStep, ok := steps["analyze"]
	require.True(t, ok, "analyze step should exist")
	assert.Equal(t, schema.StepStatusCompleted, analyzeStep.Status, "analyze step should complete")

	// Verify the expr.eval output: filtered 2 items with score >= 0.8
	var analyzeOutput map[string]any
	require.NoError(t, json.Unmarshal(analyzeStep.Output, &analyzeOutput))
	require.Contains(t, analyzeOutput, "result", "expr.eval output should have result field")

	filtered, ok := analyzeOutput["result"].([]any)
	require.True(t, ok, "result should be an array")
	assert.Len(t, filtered, 2, "should filter to 2 items with score >= 0.8")
}
