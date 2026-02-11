package mcp

import (
	"context"
	"errors"

	"github.com/mark3labs/mcp-go/server"
)

// AgentNotifier pushes notifications to connected agents.
type AgentNotifier interface {
	Notify(ctx context.Context, agentID string, payload map[string]any) error
}

// MCPNotifier implements AgentNotifier using MCP SSE push.
type MCPNotifier struct {
	mcpServer *server.MCPServer
	sessions  *SessionRegistry
}

// NewMCPNotifier creates a notifier that pushes via MCP SSE.
func NewMCPNotifier(mcpServer *server.MCPServer, sessions *SessionRegistry) *MCPNotifier {
	return &MCPNotifier{mcpServer: mcpServer, sessions: sessions}
}

// Notify sends a notification to the agent's SSE session.
// Best-effort: returns nil if the agent is not connected.
func (n *MCPNotifier) Notify(_ context.Context, agentID string, payload map[string]any) error {
	sessionID, ok := n.sessions.SessionFor(agentID)
	if !ok {
		return nil // agent not connected, best-effort
	}
	err := n.mcpServer.SendNotificationToSpecificClient(sessionID, "notifications/message", payload)
	if errors.Is(err, server.ErrSessionNotFound) {
		// Session expired between lookup and send — not an error.
		n.sessions.Remove(sessionID)
		return nil
	}
	return err
}
