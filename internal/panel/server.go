package panel

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	"github.com/rendis/opcode/internal/engine"
	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/internal/streaming"
)

//go:embed templates static
var content embed.FS

// PanelDeps holds the dependencies for the panel server.
type PanelDeps struct {
	Store    store.Store
	Executor engine.Executor
	Hub      streaming.EventHub
	Logger   *slog.Logger
}

// PanelServer serves the web management panel.
type PanelServer struct {
	deps  PanelDeps
	pages map[string]*template.Template
}

// NewPanelServer creates a new PanelServer with parsed templates.
func NewPanelServer(deps PanelDeps) *PanelServer {
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}

	funcMap := template.FuncMap{
		"json":        toJSON,
		"timeAgo":     timeAgo,
		"statusBadge": statusBadge,
		"truncate":    truncate,
		"add":         add,
		"subtract":    subtract,
	}

	// Parse shared templates (base layout + partials).
	base := template.Must(
		template.New("").Funcs(funcMap).ParseFS(content,
			"templates/base.html",
			"templates/partials/*.html",
		),
	)

	// Build per-page template sets. Each page clones the shared set
	// so that its {{define "content"}} doesn't collide with others.
	pageFiles := []string{
		"dashboard.html",
		"workflows.html",
		"workflow_detail.html",
		"templates.html",
		"template_detail.html",
		"decisions.html",
		"scheduler.html",
		"events.html",
		"agents.html",
	}

	pages := make(map[string]*template.Template, len(pageFiles))
	for _, pf := range pageFiles {
		clone := template.Must(base.Clone())
		pages[pf] = template.Must(clone.ParseFS(content, "templates/"+pf))
	}

	return &PanelServer{
		deps:  deps,
		pages: pages,
	}
}

// Handler returns the HTTP handler for the panel routes.
func (s *PanelServer) Handler() http.Handler {
	mux := http.NewServeMux()

	// Static files.
	staticFS, _ := fs.Sub(content, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Pages.
	mux.HandleFunc("GET /{$}", s.handleDashboard)
	mux.HandleFunc("GET /templates", s.handleTemplates)
	mux.HandleFunc("GET /templates/{name}", s.handleTemplateDetail)
	mux.HandleFunc("GET /workflows", s.handleWorkflows)
	mux.HandleFunc("GET /workflows/{id}", s.handleWorkflowDetail)
	mux.HandleFunc("GET /decisions", s.handleDecisions)
	mux.HandleFunc("GET /scheduler", s.handleScheduler)
	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("GET /agents", s.handleAgents)

	// SSE streams.
	mux.HandleFunc("GET /sse/events", s.handleSSEGlobal)
	mux.HandleFunc("GET /sse/workflows/{id}", s.handleSSEWorkflow)

	// API mutations.
	mux.HandleFunc("POST /api/templates", s.handleCreateTemplate)
	mux.HandleFunc("DELETE /api/templates/{name}/{version}", s.handleDeleteTemplate)
	mux.HandleFunc("POST /api/decisions/{id}/resolve", s.handleResolveDecision)
	mux.HandleFunc("POST /api/workflows/{id}/cancel", s.handleCancelWorkflow)
	mux.HandleFunc("POST /api/workflows/{id}/rerun", s.handleRerunWorkflow)
	mux.HandleFunc("POST /api/scheduler", s.handleCreateJob)
	mux.HandleFunc("PUT /api/scheduler/{id}", s.handleUpdateJob)
	mux.HandleFunc("DELETE /api/scheduler/{id}", s.handleDeleteJob)

	return mux
}

// renderPage executes a page template by name.
func (s *PanelServer) renderPage(w http.ResponseWriter, page string, data any) {
	tmpl, ok := s.pages[page]
	if !ok {
		s.deps.Logger.Error("template not found", "page", page)
		http.Error(w, fmt.Sprintf("template %q not found", page), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		s.deps.Logger.Error("template render error", "page", page, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
