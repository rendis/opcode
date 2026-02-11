package mcp

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rendis/opcode/internal/actions"
	"github.com/rendis/opcode/internal/engine"
	"github.com/rendis/opcode/internal/secrets"
	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/internal/streaming"
)

// OpcodeServerDeps holds the dependencies for creating an OpcodeServer.
type OpcodeServerDeps struct {
	Executor engine.Executor
	Store    store.Store
	Vault    secrets.Vault
	Registry actions.ActionRegistry
	Hub      streaming.EventHub
	Sessions *SessionRegistry
	Logger   *slog.Logger
}

// OpcodeServer wraps an MCP server with opcode-specific tool handlers.
type OpcodeServer struct {
	executor  engine.Executor
	store     store.Store
	vault     secrets.Vault
	registry  actions.ActionRegistry
	hub       streaming.EventHub
	sessions  *SessionRegistry
	logger    *slog.Logger
	mcpServer *server.MCPServer
}

// NewOpcodeServer creates a new OpcodeServer with all 6 tools registered.
func NewOpcodeServer(deps OpcodeServerDeps) *OpcodeServer {
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}

	sessions := deps.Sessions
	if sessions == nil {
		sessions = NewSessionRegistry()
	}

	s := &OpcodeServer{
		executor: deps.Executor,
		store:    deps.Store,
		vault:    deps.Vault,
		registry: deps.Registry,
		hub:      deps.Hub,
		sessions: sessions,
		logger:   logger,
	}

	mcpSrv := server.NewMCPServer(
		"opcode",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithRecovery(),
		server.WithInstructions("Opcode is an agent-first workflow orchestration engine. Use opcode.run to execute workflows, opcode.status to check progress, opcode.signal to send signals to suspended workflows, opcode.define to register templates, opcode.query to list workflows/events/templates, and opcode.diagram to generate workflow visualizations."),
	)

	mcpSrv.AddTools(s.tools()...)
	s.mcpServer = mcpSrv
	return s
}

// ServeSSE starts the SSE transport and blocks until ctx is cancelled.
func (s *OpcodeServer) ServeSSE(ctx context.Context, addr, baseURL string) error {
	sseServer := server.NewSSEServer(s.mcpServer,
		server.WithBaseURL(baseURL),
		server.WithKeepAliveInterval(30*time.Second),
	)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = sseServer.Shutdown(shutdownCtx)
	}()

	return sseServer.Start(addr)
}

// NewSSEServer creates an SSE server for use with a custom HTTP mux.
// The returned SSEServer implements http.Handler and can be mounted at /sse and /message.
func (s *OpcodeServer) NewSSEServer(baseURL string) *server.SSEServer {
	return server.NewSSEServer(s.mcpServer,
		server.WithBaseURL(baseURL),
		server.WithKeepAliveInterval(30*time.Second),
	)
}

// MCPServer returns the underlying MCPServer for testing or custom transports.
func (s *OpcodeServer) MCPServer() *server.MCPServer {
	return s.mcpServer
}

// Sessions returns the session registry for creating notifiers.
func (s *OpcodeServer) Sessions() *SessionRegistry {
	return s.sessions
}

// tools returns the 6 registered MCP tools as ServerTool entries.
func (s *OpcodeServer) tools() []server.ServerTool {
	return []server.ServerTool{
		{Tool: runTool(), Handler: s.handleRun},
		{Tool: statusTool(), Handler: s.handleStatus},
		{Tool: signalTool(), Handler: s.handleSignal},
		{Tool: defineTool(), Handler: s.handleDefine},
		{Tool: queryTool(), Handler: s.handleQuery},
		{Tool: diagramTool(), Handler: s.handleDiagram},
	}
}

// --- Tool definitions ---

func runTool() mcp.Tool {
	return mcp.NewTool("opcode.run",
		mcp.WithDescription("Execute a workflow from a registered template. Returns workflow_id, status, and per-step results. Workflows with reasoning steps will return status 'suspended'"),
		mcp.WithString("template_name", mcp.Required(), mcp.Description("Name of the workflow template to execute")),
		mcp.WithString("version", mcp.Description("Template version (default: latest)")),
		mcp.WithObject("params", mcp.Description("Input parameters for the workflow")),
		mcp.WithString("agent_id", mcp.Required(), mcp.Description("ID of the agent initiating the workflow")),
	)
}

func statusTool() mcp.Tool {
	return mcp.NewTool("opcode.status",
		mcp.WithDescription("Get workflow execution status. Returns steps, events, and pending_decisions for suspended workflows"),
		mcp.WithString("workflow_id", mcp.Required(), mcp.Description("ID of the workflow to query")),
	)
}

func signalTool() mcp.Tool {
	return mcp.NewTool("opcode.signal",
		mcp.WithDescription("Send a signal to a suspended workflow. Decision signals automatically resume the workflow and return the final status"),
		mcp.WithString("workflow_id", mcp.Required(), mcp.Description("ID of the target workflow")),
		mcp.WithString("signal_type", mcp.Required(),
			mcp.Enum("decision", "data", "cancel", "retry", "skip"),
			mcp.Description("Type of signal to send"),
		),
		mcp.WithObject("payload", mcp.Required(), mcp.Description("Signal payload")),
		mcp.WithString("step_id", mcp.Description("Target step ID (required for some signal types)")),
		mcp.WithString("agent_id", mcp.Description("ID of the signaling agent")),
		mcp.WithString("reasoning", mcp.Description("Agent's reasoning for this signal")),
	)
}

func defineTool() mcp.Tool {
	return mcp.NewTool("opcode.define",
		mcp.WithDescription("Register a reusable workflow template. Version auto-increments (v1, v2, v3...)"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Template name")),
		mcp.WithObject("definition", mcp.Required(), mcp.Description("Workflow definition object")),
		mcp.WithObject("input_schema", mcp.Description("JSON Schema for input validation")),
		mcp.WithObject("output_schema", mcp.Description("JSON Schema for output validation")),
		mcp.WithObject("triggers", mcp.Description("Trigger configuration")),
		mcp.WithString("agent_id", mcp.Required(), mcp.Description("ID of the defining agent")),
		mcp.WithString("description", mcp.Description("Template description")),
	)
}

func queryTool() mcp.Tool {
	return mcp.NewTool("opcode.query",
		mcp.WithDescription("Query workflows, events, or templates. Returns {\"<resource>\": [...]} where key matches the queried resource type"),
		mcp.WithString("resource", mcp.Required(),
			mcp.Enum("workflows", "events", "templates"),
			mcp.Description("Type of resource to query"),
		),
		mcp.WithObject("filter", mcp.Description("Filter criteria (status, agent_id, since, limit, event_type, workflow_id, name)")),
	)
}
