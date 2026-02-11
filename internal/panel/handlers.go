package panel

import (
	"net/http"
	"time"

	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
)

// --- Page data types ---

type pageData struct {
	Title  string
	Active string
}

type dashboardData struct {
	pageData
	ActiveCount    int
	SuspendedCount int
	FailedCount    int
	CompletedToday int
	Decisions      []*store.PendingDecision
	RecentEvents   []*store.Event
}

type templatesData struct {
	pageData
	Templates []*store.WorkflowTemplate
}

type templateDetailData struct {
	pageData
	Name       string
	Versions   []*store.WorkflowTemplate
	Current    *store.WorkflowTemplate
	Definition string // JSON for mermaid.js
}

type workflowsData struct {
	pageData
	Workflows []*store.Workflow
	Status    string
	AgentID   string
	Limit     int
	Offset    int
}

type workflowDetailData struct {
	pageData
	Workflow   *store.Workflow
	Steps     []*store.StepState
	Events    []*store.Event
	Decisions []*store.PendingDecision
}

type decisionsData struct {
	pageData
	Decisions []*store.PendingDecision
}

type schedulerData struct {
	pageData
	Jobs []*store.ScheduledJob
}

type eventsData struct {
	pageData
	Events     []*store.Event
	WorkflowID string
	EventType  string
}

type agentsData struct {
	pageData
	Agents []*store.Agent
}

// --- Page handlers ---

func (s *PanelServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Count workflows by status.
	activeStatus := schema.WorkflowStatusActive
	activeWfs, _ := s.deps.Store.ListWorkflows(ctx, store.WorkflowFilter{Status: &activeStatus, Limit: 1000})

	suspendedStatus := schema.WorkflowStatusSuspended
	suspendedWfs, _ := s.deps.Store.ListWorkflows(ctx, store.WorkflowFilter{Status: &suspendedStatus, Limit: 1000})

	failedStatus := schema.WorkflowStatusFailed
	failedWfs, _ := s.deps.Store.ListWorkflows(ctx, store.WorkflowFilter{Status: &failedStatus, Limit: 1000})

	completedStatus := schema.WorkflowStatusCompleted
	todayStart := time.Now().UTC().Truncate(24 * time.Hour)
	completedWfs, _ := s.deps.Store.ListWorkflows(ctx, store.WorkflowFilter{
		Status: &completedStatus,
		Since:  &todayStart,
		Limit:  1000,
	})

	// Pending decisions.
	decisions, _ := s.deps.Store.ListPendingDecisions(ctx, store.DecisionFilter{
		Status: "pending",
		Limit:  10,
	})

	// Recent events across all workflows (use a broad event type query).
	recentEvents, _ := s.deps.Store.GetEventsByType(ctx, "", store.EventFilter{Limit: 20})

	data := dashboardData{
		pageData:       pageData{Title: "Dashboard", Active: "dashboard"},
		ActiveCount:    len(activeWfs),
		SuspendedCount: len(suspendedWfs),
		FailedCount:    len(failedWfs),
		CompletedToday: len(completedWfs),
		Decisions:      decisions,
		RecentEvents:   recentEvents,
	}

	s.renderPage(w, "dashboard.html", data)
}

func (s *PanelServer) handleTemplates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	templates, err := s.deps.Store.ListTemplates(ctx, store.TemplateFilter{
		Limit: queryInt(r, "limit", 100),
	})
	if err != nil {
		s.deps.Logger.Error("list templates failed", "error", err)
		templates = nil
	}

	s.renderPage(w, "templates.html", templatesData{
		pageData:  pageData{Title: "Templates", Active: "templates"},
		Templates: templates,
	})
}

func (s *PanelServer) handleTemplateDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	versions, err := s.deps.Store.ListTemplates(ctx, store.TemplateFilter{Name: name, Limit: 100})
	if err != nil || len(versions) == 0 {
		http.NotFound(w, r)
		return
	}

	// Find the requested version or latest.
	reqVer := r.URL.Query().Get("version")
	var current *store.WorkflowTemplate
	if reqVer != "" {
		for _, v := range versions {
			if v.Version == reqVer {
				current = v
				break
			}
		}
	}
	if current == nil {
		current = versions[0] // latest (store returns ordered)
	}

	defJSON := toJSON(current.Definition)

	s.renderPage(w, "template_detail.html", templateDetailData{
		pageData:   pageData{Title: name, Active: "templates"},
		Name:       name,
		Versions:   versions,
		Current:    current,
		Definition: defJSON,
	})
}

func (s *PanelServer) handleWorkflows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	statusFilter := r.URL.Query().Get("status")
	agentFilter := r.URL.Query().Get("agent_id")
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	filter := store.WorkflowFilter{
		AgentID: agentFilter,
		Limit:   limit,
		Offset:  offset,
	}
	if statusFilter != "" {
		ws := schema.WorkflowStatus(statusFilter)
		filter.Status = &ws
	}

	workflows, err := s.deps.Store.ListWorkflows(ctx, filter)
	if err != nil {
		s.deps.Logger.Error("list workflows failed", "error", err)
		workflows = nil
	}

	s.renderPage(w, "workflows.html", workflowsData{
		pageData:  pageData{Title: "Workflows", Active: "workflows"},
		Workflows: workflows,
		Status:    statusFilter,
		AgentID:   agentFilter,
		Limit:     limit,
		Offset:    offset,
	})
}

func (s *PanelServer) handleWorkflowDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	wf, err := s.deps.Store.GetWorkflow(ctx, id)
	if err != nil || wf == nil {
		http.NotFound(w, r)
		return
	}

	steps, _ := s.deps.Store.ListStepStates(ctx, id)
	events, _ := s.deps.Store.GetEvents(ctx, id, 0)
	decisions, _ := s.deps.Store.ListPendingDecisions(ctx, store.DecisionFilter{
		WorkflowID: id,
	})

	s.renderPage(w, "workflow_detail.html", workflowDetailData{
		pageData:  pageData{Title: "Workflow Detail", Active: "workflows"},
		Workflow:   wf,
		Steps:     steps,
		Events:    events,
		Decisions: decisions,
	})
}

func (s *PanelServer) handleDecisions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	decisions, err := s.deps.Store.ListPendingDecisions(ctx, store.DecisionFilter{
		Status: "pending",
		Limit:  queryInt(r, "limit", 50),
	})
	if err != nil {
		s.deps.Logger.Error("list decisions failed", "error", err)
		decisions = nil
	}

	s.renderPage(w, "decisions.html", decisionsData{
		pageData:  pageData{Title: "Decisions", Active: "decisions"},
		Decisions: decisions,
	})
}

func (s *PanelServer) handleScheduler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	jobs, err := s.deps.Store.ListScheduledJobs(ctx, store.ScheduledJobFilter{
		Limit: queryInt(r, "limit", 50),
	})
	if err != nil {
		s.deps.Logger.Error("list scheduled jobs failed", "error", err)
		jobs = nil
	}

	s.renderPage(w, "scheduler.html", schedulerData{
		pageData: pageData{Title: "Scheduler", Active: "scheduler"},
		Jobs:     jobs,
	})
}

func (s *PanelServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	workflowID := r.URL.Query().Get("workflow_id")
	eventType := r.URL.Query().Get("event_type")
	limit := queryInt(r, "limit", 100)

	var events []*store.Event
	var err error

	if eventType != "" {
		events, err = s.deps.Store.GetEventsByType(ctx, eventType, store.EventFilter{
			WorkflowID: workflowID,
			Limit:      limit,
		})
	} else if workflowID != "" {
		events, err = s.deps.Store.GetEvents(ctx, workflowID, 0)
	}
	// If neither filter provided, events stays nil (can't query all events without filter).

	if err != nil {
		s.deps.Logger.Error("list events failed", "error", err)
		events = nil
	}

	s.renderPage(w, "events.html", eventsData{
		pageData:   pageData{Title: "Events", Active: "events"},
		Events:     events,
		WorkflowID: workflowID,
		EventType:  eventType,
	})
}

func (s *PanelServer) handleAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agents, err := s.deps.Store.ListAgents(ctx)
	if err != nil {
		s.deps.Logger.Error("list agents failed", "error", err)
		agents = nil
	}

	s.renderPage(w, "agents.html", agentsData{
		pageData: pageData{Title: "Agents", Active: "agents"},
		Agents:   agents,
	})
}
