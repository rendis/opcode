package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOpcodeServer(t *testing.T) {
	s := NewOpcodeServer(OpcodeServerDeps{})
	require.NotNil(t, s)
	assert.NotNil(t, s.mcpServer)
	assert.NotNil(t, s.logger)
}

func TestToolRegistration(t *testing.T) {
	s := NewOpcodeServer(OpcodeServerDeps{})

	tools := s.mcpServer.ListTools()
	require.Len(t, tools, 6)

	expectedTools := []string{
		"opcode.run",
		"opcode.status",
		"opcode.signal",
		"opcode.define",
		"opcode.query",
		"opcode.diagram",
	}
	for _, name := range expectedTools {
		tool := s.mcpServer.GetTool(name)
		assert.NotNil(t, tool, "tool %s should be registered", name)
	}
}

func TestToolDefinitions(t *testing.T) {
	tests := []struct {
		name        string
		toolName    string
		description string
	}{
		{"run", "opcode.run", "Execute a workflow from a registered template. Returns workflow_id, status, and per-step results. Workflows with reasoning steps will return status 'suspended'"},
		{"status", "opcode.status", "Get workflow execution status. Returns steps, events, and pending_decisions for suspended workflows"},
		{"signal", "opcode.signal", "Send a signal to a suspended workflow. Decision signals automatically resume the workflow and return the final status"},
		{"define", "opcode.define", "Register a reusable workflow template. Version auto-increments (v1, v2, v3...)"},
		{"query", "opcode.query", "Query workflows, events, or templates. Returns {\"<resource>\": [...]} where key matches the queried resource type"},
		{"diagram", "opcode.diagram", "Generate a visual diagram of a workflow. Returns ASCII art, Mermaid flowchart syntax, or base64-encoded PNG image"},
	}

	s := NewOpcodeServer(OpcodeServerDeps{})

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tool := s.mcpServer.GetTool(tc.toolName)
			require.NotNil(t, tool)
			assert.Equal(t, tc.description, tool.Tool.Description)
		})
	}
}
