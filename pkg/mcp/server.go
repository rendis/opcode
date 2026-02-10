package mcp

import (
	"context"
	"log/slog"
	"os"

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
	Logger   *slog.Logger
}

// OpcodeServer wraps an MCP server with opcode-specific tool handlers.
type OpcodeServer struct {
	executor  engine.Executor
	store     store.Store
	vault     secrets.Vault
	registry  actions.ActionRegistry
	hub       streaming.EventHub
	logger    *slog.Logger
	mcpServer *server.MCPServer
}

// NewOpcodeServer creates a new OpcodeServer with all 5 tools registered.
func NewOpcodeServer(deps OpcodeServerDeps) *OpcodeServer {
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}

	s := &OpcodeServer{
		executor: deps.Executor,
		store:    deps.Store,
		vault:    deps.Vault,
		registry: deps.Registry,
		hub:      deps.Hub,
		logger:   logger,
	}

	mcpSrv := server.NewMCPServer(
		"opcode",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithRecovery(),
		server.WithInstructions("Opcode is an agent-first workflow orchestration engine. Use opcode.run to execute workflows, opcode.status to check progress, opcode.signal to send signals to suspended workflows, opcode.define to register templates, and opcode.query to list workflows/events/templates."),
	)

	mcpSrv.AddTools(s.tools()...)
	s.mcpServer = mcpSrv
	return s
}

// Serve starts the stdio transport and blocks until ctx is cancelled or stdin closes.
func (s *OpcodeServer) Serve(ctx context.Context) error {
	stdio := server.NewStdioServer(s.mcpServer)
	return stdio.Listen(ctx, os.Stdin, os.Stdout)
}

// MCPServer returns the underlying MCPServer for testing or custom transports.
func (s *OpcodeServer) MCPServer() *server.MCPServer {
	return s.mcpServer
}

// tools returns the 5 registered MCP tools as ServerTool entries.
func (s *OpcodeServer) tools() []server.ServerTool {
	return []server.ServerTool{
		{Tool: runTool(), Handler: s.handleRun},
		{Tool: statusTool(), Handler: s.handleStatus},
		{Tool: signalTool(), Handler: s.handleSignal},
		{Tool: defineTool(), Handler: s.handleDefine},
		{Tool: queryTool(), Handler: s.handleQuery},
	}
}

// --- Tool definitions ---

func runTool() mcp.Tool {
	return mcp.NewTool("opcode.run",
		mcp.WithDescription("Execute a workflow from a registered template"),
		mcp.WithString("template_name", mcp.Required(), mcp.Description("Name of the workflow template to execute")),
		mcp.WithString("version", mcp.Description("Template version (default: latest)")),
		mcp.WithObject("params", mcp.Description("Input parameters for the workflow")),
		mcp.WithString("agent_id", mcp.Required(), mcp.Description("ID of the agent initiating the workflow")),
	)
}

func statusTool() mcp.Tool {
	return mcp.NewTool("opcode.status",
		mcp.WithDescription("Get workflow execution status"),
		mcp.WithString("workflow_id", mcp.Required(), mcp.Description("ID of the workflow to query")),
	)
}

func signalTool() mcp.Tool {
	return mcp.NewTool("opcode.signal",
		mcp.WithDescription("Send a signal to a suspended workflow"),
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
		mcp.WithDescription("Register a reusable workflow template"),
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
		mcp.WithDescription("Query workflows, events, or templates"),
		mcp.WithString("resource", mcp.Required(),
			mcp.Enum("workflows", "events", "templates"),
			mcp.Description("Type of resource to query"),
		),
		mcp.WithObject("filter", mcp.Description("Filter criteria (status, agent_id, since, limit, event_type, workflow_id, name)")),
	)
}
