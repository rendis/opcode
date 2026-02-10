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
	require.Len(t, tools, 5)

	expectedTools := []string{
		"opcode.run",
		"opcode.status",
		"opcode.signal",
		"opcode.define",
		"opcode.query",
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
		{"run", "opcode.run", "Execute a workflow from a registered template"},
		{"status", "opcode.status", "Get workflow execution status"},
		{"signal", "opcode.signal", "Send a signal to a suspended workflow"},
		{"define", "opcode.define", "Register a reusable workflow template"},
		{"query", "opcode.query", "Query workflows, events, or templates"},
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
