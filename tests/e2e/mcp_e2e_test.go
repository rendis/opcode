package e2e

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rendis/opcode/internal/actions"
	"github.com/rendis/opcode/internal/engine"
	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/internal/streaming"
	opcmcp "github.com/rendis/opcode/pkg/mcp"
	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test infrastructure ---

// testEnv holds all real dependencies for E2E tests.
type testEnv struct {
	store    *store.LibSQLStore
	eventLog *store.EventLog
	registry *actions.Registry
	executor engine.Executor
	hub      *streaming.MemoryHub
	server   *opcmcp.OpcodeServer
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "e2e.db")
	s, err := store.NewLibSQLStore("file:" + dbPath)
	require.NoError(t, err)
	require.NoError(t, s.Migrate(context.Background()))
	t.Cleanup(func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	})

	eventLog := store.NewEventLog(s)
	reg := actions.NewRegistry()
	require.NoError(t, reg.Register(&noopAction{}))
	require.NoError(t, reg.Register(&echoAction{}))

	hub := streaming.NewMemoryHub()

	exec := engine.NewExecutor(s, eventLog, reg, engine.ExecutorConfig{PoolSize: 4})

	srv := opcmcp.NewOpcodeServer(opcmcp.OpcodeServerDeps{
		Executor: exec,
		Store:    s,
		Registry: reg,
		Hub:      hub,
	})

	return &testEnv{
		store:    s,
		eventLog: eventLog,
		registry: reg,
		executor: exec,
		hub:      hub,
		server:   srv,
	}
}

// callTool invokes a tool handler through the MCP server's HandleMessage (full JSON-RPC round-trip).
func (e *testEnv) callTool(t *testing.T, toolName string, args map[string]any) *mcp.CallToolResult {
	t.Helper()

	// Build JSON-RPC request.
	reqMsg := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}
	rawReq, err := json.Marshal(reqMsg)
	require.NoError(t, err)

	// Initialize session first.
	initMsg := map[string]any{
		"jsonrpc": "2.0",
		"id":      0,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":   map[string]any{},
			"clientInfo": map[string]any{
				"name":    "e2e-test",
				"version": "1.0.0",
			},
		},
	}
	rawInit, err := json.Marshal(initMsg)
	require.NoError(t, err)

	ctx := context.Background()
	mcpSrv := e.server.MCPServer()

	// Initialize.
	initResp := mcpSrv.HandleMessage(ctx, rawInit)
	require.NotNil(t, initResp)

	// Call tool.
	resp := mcpSrv.HandleMessage(ctx, rawReq)
	require.NotNil(t, resp)

	// Parse response.
	respBytes, err := json.Marshal(resp)
	require.NoError(t, err)

	var rpcResp struct {
		Result *mcp.CallToolResult `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(respBytes, &rpcResp))

	if rpcResp.Error != nil {
		t.Fatalf("JSON-RPC error: code=%d, msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	require.NotNil(t, rpcResp.Result)
	return rpcResp.Result
}

// extractJSON extracts text content from a tool result and parses it as JSON.
func extractJSON(t *testing.T, result *mcp.CallToolResult, target any) {
	t.Helper()
	require.NotEmpty(t, result.Content)
	text := mcp.GetTextFromContent(result.Content[0])
	require.NoError(t, json.Unmarshal([]byte(text), target))
}

// extractQueryResult extracts a named array from a wrapped query result.
func extractQueryResult[T any](t *testing.T, result *mcp.CallToolResult, key string) []T {
	t.Helper()
	var wrapper map[string][]T
	extractJSON(t, result, &wrapper)
	return wrapper[key]
}

// assertStructuredIsObject ensures structuredContent is a JSON object (not array/null).
func assertStructuredIsObject(t *testing.T, result *mcp.CallToolResult) {
	t.Helper()
	require.NotNil(t, result.StructuredContent, "structuredContent should be present")
	b, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	assert.True(t, len(b) > 0 && b[0] == '{', "structuredContent must be an object, got: %s", string(b[:min(len(b), 20)]))
}

// extractText extracts text content from a tool result.
func extractText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, result.Content)
	return mcp.GetTextFromContent(result.Content[0])
}

// --- Test actions ---

// noopAction completes immediately with static output.
type noopAction struct{}

func (a *noopAction) Name() string         { return "noop" }
func (a *noopAction) Schema() actions.ActionSchema { return actions.ActionSchema{Description: "no-op test action"} }
func (a *noopAction) Validate(_ map[string]any) error { return nil }
func (a *noopAction) Execute(_ context.Context, _ actions.ActionInput) (*actions.ActionOutput, error) {
	out, _ := json.Marshal(map[string]any{"done": true})
	return &actions.ActionOutput{Data: json.RawMessage(out)}, nil
}

// echoAction returns its input params as output.
type echoAction struct{}

func (a *echoAction) Name() string         { return "echo" }
func (a *echoAction) Schema() actions.ActionSchema { return actions.ActionSchema{Description: "echo test action"} }
func (a *echoAction) Validate(_ map[string]any) error { return nil }
func (a *echoAction) Execute(_ context.Context, input actions.ActionInput) (*actions.ActionOutput, error) {
	out, _ := json.Marshal(input.Params)
	return &actions.ActionOutput{Data: json.RawMessage(out)}, nil
}

// --- E2E Tests ---

// TestMCPFullLifecycle exercises the complete MCP lifecycle:
// define template -> run workflow -> check status -> query workflows/events.
func TestMCPFullLifecycle(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// 1. Register agent.
	require.NoError(t, env.store.RegisterAgent(ctx, &store.Agent{
		ID:   "e2e-agent",
		Name: "E2E Test Agent",
		Type: "system",
	}))

	// 2. Define template via opcode.define.
	defineResult := env.callTool(t, "opcode.define", map[string]any{
		"name": "e2e-workflow",
		"definition": map[string]any{
			"steps": []any{
				map[string]any{"id": "step-1", "action": "noop"},
			},
		},
		"agent_id":    "e2e-agent",
		"description": "E2E test template",
	})
	assert.False(t, defineResult.IsError, "define should succeed")

	var defineOut map[string]any
	extractJSON(t, defineResult, &defineOut)
	assert.Equal(t, "e2e-workflow", defineOut["name"])
	assert.Equal(t, "v1", defineOut["version"])

	// 3. Run workflow via opcode.run.
	runResult := env.callTool(t, "opcode.run", map[string]any{
		"template_name": "e2e-workflow",
		"agent_id":      "e2e-agent",
		"params":        map[string]any{"env": "test"},
	})
	assert.False(t, runResult.IsError, "run should succeed")

	var runOut map[string]any
	extractJSON(t, runResult, &runOut)
	assert.Equal(t, "completed", runOut["status"])
	wfID, ok := runOut["workflow_id"].(string)
	require.True(t, ok, "workflow_id should be a string")
	assert.NotEmpty(t, wfID)

	// 4. Check status via opcode.status.
	statusResult := env.callTool(t, "opcode.status", map[string]any{
		"workflow_id": wfID,
	})
	assert.False(t, statusResult.IsError, "status should succeed")

	var statusOut map[string]any
	extractJSON(t, statusResult, &statusOut)
	assert.Equal(t, wfID, statusOut["workflow_id"])
	assert.Equal(t, "completed", statusOut["status"])

	// 5. Query workflows via opcode.query.
	queryWfResult := env.callTool(t, "opcode.query", map[string]any{
		"resource": "workflows",
		"filter":   map[string]any{"agent_id": "e2e-agent"},
	})
	assert.False(t, queryWfResult.IsError, "query workflows should succeed")
	assertStructuredIsObject(t, queryWfResult)

	workflows := extractQueryResult[map[string]any](t, queryWfResult, "workflows")
	require.Len(t, workflows, 1)
	assert.Equal(t, wfID, workflows[0]["id"])

	// 6. Query events via opcode.query.
	queryEvResult := env.callTool(t, "opcode.query", map[string]any{
		"resource": "events",
		"filter":   map[string]any{"workflow_id": wfID},
	})
	assert.False(t, queryEvResult.IsError, "query events should succeed")
	assertStructuredIsObject(t, queryEvResult)

	events := extractQueryResult[map[string]any](t, queryEvResult, "events")
	assert.NotEmpty(t, events, "should have workflow events")

	// Verify key event types are present.
	eventTypes := make([]string, len(events))
	for i, e := range events {
		eventTypes[i], _ = e["event_type"].(string)
	}
	assert.Contains(t, eventTypes, "workflow_started")
	assert.Contains(t, eventTypes, "workflow_completed")
	assert.Contains(t, eventTypes, "step_started")
	assert.Contains(t, eventTypes, "step_completed")

	// 7. Query templates via opcode.query.
	queryTplResult := env.callTool(t, "opcode.query", map[string]any{
		"resource": "templates",
		"filter":   map[string]any{"name": "e2e-workflow"},
	})
	assert.False(t, queryTplResult.IsError, "query templates should succeed")
	assertStructuredIsObject(t, queryTplResult)

	templates := extractQueryResult[map[string]any](t, queryTplResult, "templates")
	require.Len(t, templates, 1)
	assert.Equal(t, "e2e-workflow", templates[0]["name"])
}

// TestTemplateDefineAndRun tests multi-version templates: define v1, define v2, run latest.
func TestTemplateDefineAndRun(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	require.NoError(t, env.store.RegisterAgent(ctx, &store.Agent{
		ID: "agent-v", Name: "versioning-agent", Type: "system",
	}))

	// Define v1.
	r1 := env.callTool(t, "opcode.define", map[string]any{
		"name": "versioned-wf",
		"definition": map[string]any{
			"steps": []any{
				map[string]any{"id": "s1", "action": "noop"},
			},
		},
		"agent_id":    "agent-v",
		"description": "v1 template",
	})
	assert.False(t, r1.IsError)
	var out1 map[string]any
	extractJSON(t, r1, &out1)
	assert.Equal(t, "v1", out1["version"])

	// Define v2 (same name, auto-increment).
	r2 := env.callTool(t, "opcode.define", map[string]any{
		"name": "versioned-wf",
		"definition": map[string]any{
			"steps": []any{
				map[string]any{"id": "s1", "action": "echo"},
			},
		},
		"agent_id":    "agent-v",
		"description": "v2 template with echo",
	})
	assert.False(t, r2.IsError)
	var out2 map[string]any
	extractJSON(t, r2, &out2)
	assert.Equal(t, "v2", out2["version"])

	// Run without specifying version — should use v2 (latest).
	runResult := env.callTool(t, "opcode.run", map[string]any{
		"template_name": "versioned-wf",
		"agent_id":      "agent-v",
	})
	assert.False(t, runResult.IsError, "run latest should succeed")

	var runOut map[string]any
	extractJSON(t, runResult, &runOut)
	assert.Equal(t, "completed", runOut["status"])

	// Verify the workflow used v2.
	wfID := runOut["workflow_id"].(string)
	wf, err := env.store.GetWorkflow(ctx, wfID)
	require.NoError(t, err)
	assert.Equal(t, "v2", wf.TemplateVersion)

	// Run with explicit v1.
	runV1 := env.callTool(t, "opcode.run", map[string]any{
		"template_name": "versioned-wf",
		"version":       "v1",
		"agent_id":      "agent-v",
	})
	assert.False(t, runV1.IsError, "run v1 should succeed")

	var runV1Out map[string]any
	extractJSON(t, runV1, &runV1Out)
	v1WfID := runV1Out["workflow_id"].(string)
	v1Wf, err := env.store.GetWorkflow(ctx, v1WfID)
	require.NoError(t, err)
	assert.Equal(t, "v1", v1Wf.TemplateVersion)

	// Query templates — should have 2 versions.
	qResult := env.callTool(t, "opcode.query", map[string]any{
		"resource": "templates",
		"filter":   map[string]any{"name": "versioned-wf"},
	})
	tpls := extractQueryResult[map[string]any](t, qResult, "templates")
	assert.Len(t, tpls, 2)
}

// TestEventSourcingReplay verifies that events are persisted correctly and can be replayed
// to reconstruct step states matching the final workflow state.
func TestEventSourcingReplay(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	require.NoError(t, env.store.RegisterAgent(ctx, &store.Agent{
		ID: "replay-agent", Name: "replay-agent", Type: "system",
	}))

	// Define and run a multi-step workflow.
	env.callTool(t, "opcode.define", map[string]any{
		"name": "replay-wf",
		"definition": map[string]any{
			"steps": []any{
				map[string]any{"id": "a", "action": "echo"},
				map[string]any{"id": "b", "action": "noop", "depends_on": []any{"a"}},
			},
		},
		"agent_id": "replay-agent",
	})

	runResult := env.callTool(t, "opcode.run", map[string]any{
		"template_name": "replay-wf",
		"agent_id":      "replay-agent",
		"params":        map[string]any{"msg": "hello"},
	})
	assert.False(t, runResult.IsError)

	var runOut map[string]any
	extractJSON(t, runResult, &runOut)
	wfID := runOut["workflow_id"].(string)
	assert.Equal(t, "completed", runOut["status"])

	// Verify events exist with correct sequencing.
	events, err := env.eventLog.GetEvents(ctx, wfID, 0)
	require.NoError(t, err)
	assert.NotEmpty(t, events, "events should have been appended")

	// Verify monotonically increasing sequences.
	for i, e := range events {
		assert.Equal(t, int64(i+1), e.Sequence, "event %d should have sequence %d", i, i+1)
	}

	// Replay events to reconstruct step states.
	states, err := env.eventLog.ReplayEvents(ctx, wfID)
	require.NoError(t, err)

	// Both steps should be completed.
	require.Contains(t, states, "a")
	require.Contains(t, states, "b")
	assert.Equal(t, schema.StepStatusCompleted, states["a"].Status)
	assert.Equal(t, schema.StepStatusCompleted, states["b"].Status)

	// Verify step "a" started before step "b" (dependency ordering).
	require.NotNil(t, states["a"].StartedAt)
	require.NotNil(t, states["b"].StartedAt)
	assert.True(t, !states["a"].StartedAt.After(*states["b"].StartedAt),
		"step 'a' should start before or at the same time as step 'b'")

	// Verify duration is set.
	assert.Greater(t, states["a"].DurationMs+1, int64(0), "duration should be non-negative")
	assert.Greater(t, states["b"].DurationMs+1, int64(0), "duration should be non-negative")
}

// TestSignalDecision exercises the reasoning/decision flow:
// define a workflow with a reasoning step, run it (suspends), signal a decision, check completed.
func TestSignalDecision(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	require.NoError(t, env.store.RegisterAgent(ctx, &store.Agent{
		ID: "decide-agent", Name: "decide-agent", Type: "llm",
	}))

	// Define a workflow with: noop step -> reasoning step (with options).
	env.callTool(t, "opcode.define", map[string]any{
		"name": "decision-wf",
		"definition": map[string]any{
			"steps": []any{
				map[string]any{"id": "prep", "action": "noop"},
				map[string]any{
					"id":         "decide",
					"type":       "reasoning",
					"depends_on": []any{"prep"},
					"config": map[string]any{
						"prompt_context": "Should we deploy?",
						"options": []any{
							map[string]any{"id": "yes", "description": "Deploy now"},
							map[string]any{"id": "no", "description": "Abort deploy"},
						},
						"timeout":  "30s",
						"fallback": "no",
					},
				},
				map[string]any{
					"id":         "final",
					"action":     "noop",
					"depends_on": []any{"decide"},
				},
			},
		},
		"agent_id": "decide-agent",
	})

	// Run — should suspend at the reasoning step.
	runResult := env.callTool(t, "opcode.run", map[string]any{
		"template_name": "decision-wf",
		"agent_id":      "decide-agent",
	})
	assert.False(t, runResult.IsError)

	var runOut map[string]any
	extractJSON(t, runResult, &runOut)
	wfID := runOut["workflow_id"].(string)
	assert.Equal(t, "suspended", runOut["status"], "workflow should suspend for reasoning")

	// Check status — should show pending decision.
	statusResult := env.callTool(t, "opcode.status", map[string]any{
		"workflow_id": wfID,
	})
	assert.False(t, statusResult.IsError)

	var statusOut map[string]any
	extractJSON(t, statusResult, &statusOut)
	assert.Equal(t, "suspended", statusOut["status"])

	pendingDecisions, _ := statusOut["pending_decisions"].([]any)
	require.NotEmpty(t, pendingDecisions, "should have pending decisions")

	// Signal the decision.
	signalResult := env.callTool(t, "opcode.signal", map[string]any{
		"workflow_id": wfID,
		"signal_type": "decision",
		"payload": map[string]any{
			"choice":    "yes",
			"reasoning": "tests are green, deploy now",
		},
		"step_id":   "decide",
		"agent_id":  "decide-agent",
		"reasoning": "tests are green",
	})
	assert.False(t, signalResult.IsError, "signal should succeed")

	// Signal auto-resumes for decision signals — verify workflow completed.
	var signalOut map[string]any
	extractJSON(t, signalResult, &signalOut)
	assert.Equal(t, true, signalOut["resumed"])
	assert.Equal(t, string(schema.WorkflowStatusCompleted), signalOut["status"])
}

// TestMultiStepDependencyChain tests a linear dependency chain: a -> b -> c.
func TestMultiStepDependencyChain(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	require.NoError(t, env.store.RegisterAgent(ctx, &store.Agent{
		ID: "chain-agent", Name: "chain-agent", Type: "system",
	}))

	env.callTool(t, "opcode.define", map[string]any{
		"name": "chain-wf",
		"definition": map[string]any{
			"steps": []any{
				map[string]any{"id": "a", "action": "echo"},
				map[string]any{"id": "b", "action": "noop", "depends_on": []any{"a"}},
				map[string]any{"id": "c", "action": "echo", "depends_on": []any{"b"}},
			},
		},
		"agent_id": "chain-agent",
	})

	runResult := env.callTool(t, "opcode.run", map[string]any{
		"template_name": "chain-wf",
		"agent_id":      "chain-agent",
		"params":        map[string]any{"x": 42},
	})
	assert.False(t, runResult.IsError)

	var runOut map[string]any
	extractJSON(t, runResult, &runOut)
	assert.Equal(t, "completed", runOut["status"])

	// Verify all steps completed in order via event replay.
	wfID := runOut["workflow_id"].(string)
	states, err := env.eventLog.ReplayEvents(ctx, wfID)
	require.NoError(t, err)
	require.Len(t, states, 3)

	for _, stepID := range []string{"a", "b", "c"} {
		require.Contains(t, states, stepID)
		assert.Equal(t, schema.StepStatusCompleted, states[stepID].Status, "step %s should be completed", stepID)
	}

	// Verify ordering: a started before b, b before c.
	require.NotNil(t, states["a"].StartedAt)
	require.NotNil(t, states["b"].StartedAt)
	require.NotNil(t, states["c"].StartedAt)
	assert.True(t, !states["b"].StartedAt.Before(*states["a"].CompletedAt),
		"step b should not start before step a completes")
	assert.True(t, !states["c"].StartedAt.Before(*states["b"].CompletedAt),
		"step c should not start before step b completes")
}

// TestQueryFilters tests various query filter combinations.
func TestQueryFilters(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	require.NoError(t, env.store.RegisterAgent(ctx, &store.Agent{
		ID: "filter-agent-1", Name: "filter-agent-1", Type: "system",
	}))
	require.NoError(t, env.store.RegisterAgent(ctx, &store.Agent{
		ID: "filter-agent-2", Name: "filter-agent-2", Type: "system",
	}))

	// Define template.
	env.callTool(t, "opcode.define", map[string]any{
		"name":       "filter-wf",
		"definition": map[string]any{"steps": []any{map[string]any{"id": "s1", "action": "noop"}}},
		"agent_id":   "filter-agent-1",
	})

	// Run 2 workflows from agent-1, 1 from agent-2.
	for i := 0; i < 2; i++ {
		r := env.callTool(t, "opcode.run", map[string]any{
			"template_name": "filter-wf", "agent_id": "filter-agent-1",
		})
		assert.False(t, r.IsError)
	}
	r := env.callTool(t, "opcode.run", map[string]any{
		"template_name": "filter-wf", "agent_id": "filter-agent-2",
	})
	assert.False(t, r.IsError)

	// Query all: should be 3.
	qAll := env.callTool(t, "opcode.query", map[string]any{"resource": "workflows"})
	assertStructuredIsObject(t, qAll)
	allWf := extractQueryResult[map[string]any](t, qAll, "workflows")
	assert.Len(t, allWf, 3)

	// Query by agent_id: should be 2.
	qAgent := env.callTool(t, "opcode.query", map[string]any{
		"resource": "workflows",
		"filter":   map[string]any{"agent_id": "filter-agent-1"},
	})
	agentWf := extractQueryResult[map[string]any](t, qAgent, "workflows")
	assert.Len(t, agentWf, 2)

	// Query by status.
	qStatus := env.callTool(t, "opcode.query", map[string]any{
		"resource": "workflows",
		"filter":   map[string]any{"status": "completed"},
	})
	completedWf := extractQueryResult[map[string]any](t, qStatus, "workflows")
	assert.Len(t, completedWf, 3)

	// Query with limit.
	qLimit := env.callTool(t, "opcode.query", map[string]any{
		"resource": "workflows",
		"filter":   map[string]any{"limit": float64(1)},
	})
	limitWf := extractQueryResult[map[string]any](t, qLimit, "workflows")
	assert.Len(t, limitWf, 1)
}

// TestErrorHandling tests error conditions in tool calls.
func TestErrorHandling(t *testing.T) {
	env := newTestEnv(t)

	t.Run("run_nonexistent_template", func(t *testing.T) {
		result := env.callTool(t, "opcode.run", map[string]any{
			"template_name": "does-not-exist",
			"agent_id":      "some-agent",
		})
		assert.True(t, result.IsError)
		assert.Contains(t, extractText(t, result), "not found")
	})

	t.Run("status_nonexistent_workflow", func(t *testing.T) {
		result := env.callTool(t, "opcode.status", map[string]any{
			"workflow_id": "nonexistent-wf-id",
		})
		assert.True(t, result.IsError)
	})

	t.Run("signal_nonexistent_workflow", func(t *testing.T) {
		result := env.callTool(t, "opcode.signal", map[string]any{
			"workflow_id": "nonexistent-wf-id",
			"signal_type": "cancel",
			"payload":     map[string]any{},
		})
		assert.True(t, result.IsError)
	})

	t.Run("query_invalid_resource", func(t *testing.T) {
		result := env.callTool(t, "opcode.query", map[string]any{
			"resource": "invalid-resource",
		})
		assert.True(t, result.IsError)
		assert.Contains(t, extractText(t, result), "unknown resource")
	})
}

// TestToolsListViaJSONRPC verifies tools/list returns all 6 tools through the JSON-RPC protocol.
func TestToolsListViaJSONRPC(t *testing.T) {
	env := newTestEnv(t)

	// Initialize.
	initMsg, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 0, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":   map[string]any{},
			"clientInfo":     map[string]any{"name": "test", "version": "1.0.0"},
		},
	})
	env.server.MCPServer().HandleMessage(context.Background(), initMsg)

	// List tools.
	listMsg, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list",
		"params": map[string]any{},
	})
	resp := env.server.MCPServer().HandleMessage(context.Background(), listMsg)
	require.NotNil(t, resp)

	respBytes, _ := json.Marshal(resp)
	var rpcResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(respBytes, &rpcResp))

	toolNames := make([]string, len(rpcResp.Result.Tools))
	for i, tool := range rpcResp.Result.Tools {
		toolNames[i] = tool.Name
	}

	assert.Contains(t, toolNames, "opcode.run")
	assert.Contains(t, toolNames, "opcode.status")
	assert.Contains(t, toolNames, "opcode.signal")
	assert.Contains(t, toolNames, "opcode.define")
	assert.Contains(t, toolNames, "opcode.query")
	assert.Contains(t, toolNames, "opcode.diagram")
	assert.Len(t, toolNames, 6)
}

// TestDiagramFromTemplate defines a template, then generates a Mermaid diagram from it.
func TestDiagramFromTemplate(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	require.NoError(t, env.store.RegisterAgent(ctx, &store.Agent{
		ID: "diagram-agent", Name: "diagram-agent", Type: "system",
	}))

	// Define a multi-step template.
	defineResult := env.callTool(t, "opcode.define", map[string]any{
		"name": "diagram-wf",
		"definition": map[string]any{
			"steps": []any{
				map[string]any{"id": "fetch", "action": "echo"},
				map[string]any{"id": "transform", "action": "noop", "depends_on": []any{"fetch"}},
				map[string]any{"id": "store", "action": "echo", "depends_on": []any{"transform"}},
			},
		},
		"agent_id": "diagram-agent",
	})
	assert.False(t, defineResult.IsError, "define should succeed")

	// Generate Mermaid diagram from template.
	mermaidResult := env.callTool(t, "opcode.diagram", map[string]any{
		"template_name": "diagram-wf",
		"format":        "mermaid",
	})
	assert.False(t, mermaidResult.IsError, "diagram mermaid should succeed")

	mermaidText := extractText(t, mermaidResult)
	assert.Contains(t, mermaidText, "graph TD")
	assert.Contains(t, mermaidText, "fetch")
	assert.Contains(t, mermaidText, "transform")
	assert.Contains(t, mermaidText, "store")
	assert.Contains(t, mermaidText, "-->")

	// Generate ASCII diagram from template.
	asciiResult := env.callTool(t, "opcode.diagram", map[string]any{
		"template_name": "diagram-wf",
		"format":        "ascii",
	})
	assert.False(t, asciiResult.IsError, "diagram ascii should succeed")

	asciiText := extractText(t, asciiResult)
	assert.Contains(t, asciiText, "Start")
	assert.Contains(t, asciiText, "End")
	assert.Contains(t, asciiText, "fetch")

	// Generate PNG diagram from template.
	imageResult := env.callTool(t, "opcode.diagram", map[string]any{
		"template_name": "diagram-wf",
		"format":        "image",
	})
	assert.False(t, imageResult.IsError, "diagram image should succeed")

	imageText := extractText(t, imageResult)
	assert.NotEmpty(t, imageText, "base64 PNG should not be empty")
}

// TestDiagramFromWorkflow runs a workflow, then generates a diagram with runtime status.
func TestDiagramFromWorkflow(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	require.NoError(t, env.store.RegisterAgent(ctx, &store.Agent{
		ID: "diagram-wf-agent", Name: "diagram-wf-agent", Type: "system",
	}))

	// Define and run a workflow.
	env.callTool(t, "opcode.define", map[string]any{
		"name": "diagram-status-wf",
		"definition": map[string]any{
			"steps": []any{
				map[string]any{"id": "a", "action": "echo"},
				map[string]any{"id": "b", "action": "noop", "depends_on": []any{"a"}},
			},
		},
		"agent_id": "diagram-wf-agent",
	})

	runResult := env.callTool(t, "opcode.run", map[string]any{
		"template_name": "diagram-status-wf",
		"agent_id":      "diagram-wf-agent",
		"params":        map[string]any{"key": "value"},
	})
	assert.False(t, runResult.IsError)

	var runOut map[string]any
	extractJSON(t, runResult, &runOut)
	wfID := runOut["workflow_id"].(string)
	assert.Equal(t, "completed", runOut["status"])

	// Generate ASCII diagram with status.
	diagramResult := env.callTool(t, "opcode.diagram", map[string]any{
		"workflow_id":    wfID,
		"format":        "ascii",
		"include_status": "true",
	})
	assert.False(t, diagramResult.IsError, "diagram from workflow should succeed")

	diagramText := extractText(t, diagramResult)
	assert.Contains(t, diagramText, "[OK]", "completed steps should show [OK] status")

	// Generate Mermaid diagram with status.
	mermaidResult := env.callTool(t, "opcode.diagram", map[string]any{
		"workflow_id": wfID,
		"format":     "mermaid",
	})
	assert.False(t, mermaidResult.IsError)

	mermaidText := extractText(t, mermaidResult)
	assert.Contains(t, mermaidText, "graph TD")
	assert.Contains(t, mermaidText, "class a completed")
	assert.Contains(t, mermaidText, "class b completed")
}

// --- Notification tests ---

// testNotifier captures notifications for testing.
type testNotifier struct {
	mu      sync.Mutex
	entries []notifyEntry
}

type notifyEntry struct {
	AgentID string
	Payload map[string]any
}

func (n *testNotifier) Notify(_ context.Context, agentID string, payload map[string]any) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.entries = append(n.entries, notifyEntry{AgentID: agentID, Payload: payload})
	return nil
}

func (n *testNotifier) list() []notifyEntry {
	n.mu.Lock()
	defer n.mu.Unlock()
	cp := make([]notifyEntry, len(n.entries))
	copy(cp, n.entries)
	return cp
}

// newTestEnvWithNotify creates a test env that includes workflow actions and a test notifier.
func newTestEnvWithNotify(t *testing.T) (*testEnv, *testNotifier) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "e2e.db")
	s, err := store.NewLibSQLStore("file:" + dbPath)
	require.NoError(t, err)
	require.NoError(t, s.Migrate(context.Background()))
	t.Cleanup(func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	})

	eventLog := store.NewEventLog(s)
	reg := actions.NewRegistry()
	require.NoError(t, reg.Register(&noopAction{}))
	require.NoError(t, reg.Register(&echoAction{}))

	hub := streaming.NewMemoryHub()
	exec := engine.NewExecutor(s, eventLog, reg, engine.ExecutorConfig{PoolSize: 4})

	notifier := &testNotifier{}
	require.NoError(t, actions.RegisterWorkflowActions(reg, actions.WorkflowActionDeps{
		Store:    s,
		Hub:      hub,
		Notifier: notifier,
		Logger:   slog.Default(),
	}))

	sessions := opcmcp.NewSessionRegistry()
	srv := opcmcp.NewOpcodeServer(opcmcp.OpcodeServerDeps{
		Executor: exec,
		Store:    s,
		Registry: reg,
		Hub:      hub,
		Sessions: sessions,
	})

	env := &testEnv{
		store:    s,
		eventLog: eventLog,
		registry: reg,
		executor: exec,
		hub:      hub,
		server:   srv,
	}
	return env, notifier
}

// TestWorkflowNotifyE2E verifies workflow.notify steps complete and deliver notifications.
func TestWorkflowNotifyE2E(t *testing.T) {
	env, notifier := newTestEnvWithNotify(t)
	ctx := context.Background()

	// Register agent.
	require.NoError(t, env.store.RegisterAgent(ctx, &store.Agent{
		ID: "notify-agent", Name: "Notify Agent", Type: "system",
	}))

	// Define template with notify steps mid-flow and at end.
	defineResult := env.callTool(t, "opcode.define", map[string]any{
		"name": "notify-e2e",
		"definition": map[string]any{
			"steps": []any{
				map[string]any{"id": "s1", "action": "echo", "params": map[string]any{"msg": "hello"}},
				map[string]any{
					"id": "n1", "action": "workflow.notify",
					"params":     map[string]any{"message": "mid-flow", "data": map[string]any{"progress": 50}},
					"depends_on": []any{"s1"},
				},
				map[string]any{
					"id": "s2", "action": "noop",
					"depends_on": []any{"n1"},
				},
				map[string]any{
					"id": "n2", "action": "workflow.notify",
					"params":     map[string]any{"message": "done"},
					"depends_on": []any{"s2"},
				},
			},
		},
		"agent_id": "notify-agent",
	})
	require.False(t, defineResult.IsError, "define should succeed")

	// Run workflow.
	runResult := env.callTool(t, "opcode.run", map[string]any{
		"template_name": "notify-e2e",
		"agent_id":      "notify-agent",
	})
	require.False(t, runResult.IsError, "run should succeed")

	var runOut map[string]any
	extractJSON(t, runResult, &runOut)
	assert.Equal(t, "completed", runOut["status"])

	// Verify all steps completed.
	steps, _ := runOut["steps"].(map[string]any)
	require.Len(t, steps, 4)
	for _, sid := range []string{"s1", "n1", "s2", "n2"} {
		step, _ := steps[sid].(map[string]any)
		assert.Equal(t, "completed", step["status"], "step %s should be completed", sid)
	}

	// Verify notify step outputs.
	n1step, _ := steps["n1"].(map[string]any)
	n1out, _ := n1step["output"].(map[string]any)
	assert.Equal(t, true, n1out["notified"])

	n2step, _ := steps["n2"].(map[string]any)
	n2out, _ := n2step["output"].(map[string]any)
	assert.Equal(t, true, n2out["notified"])

	// Verify notifications were captured by the test notifier.
	entries := notifier.list()
	require.Len(t, entries, 2, "should have 2 notifications")

	assert.Equal(t, "notify-agent", entries[0].AgentID)
	assert.Equal(t, "mid-flow", entries[0].Payload["message"])
	assert.NotEmpty(t, entries[0].Payload["workflow_id"])
	assert.Equal(t, "n1", entries[0].Payload["step_id"])
	data0, _ := entries[0].Payload["data"].(map[string]any)
	assert.Equal(t, float64(50), data0["progress"])

	assert.Equal(t, "notify-agent", entries[1].AgentID)
	assert.Equal(t, "done", entries[1].Payload["message"])
	assert.Equal(t, "n2", entries[1].Payload["step_id"])
}

// TestWorkflowNotifyNoNotifierE2E verifies workflow.notify completes even without a notifier.
func TestWorkflowNotifyNoNotifierE2E(t *testing.T) {
	// Create env WITHOUT notifier in workflow deps.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "e2e.db")
	s, err := store.NewLibSQLStore("file:" + dbPath)
	require.NoError(t, err)
	require.NoError(t, s.Migrate(context.Background()))
	t.Cleanup(func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	})

	eventLog := store.NewEventLog(s)
	reg := actions.NewRegistry()
	require.NoError(t, reg.Register(&noopAction{}))
	hub := streaming.NewMemoryHub()
	exec := engine.NewExecutor(s, eventLog, reg, engine.ExecutorConfig{PoolSize: 4})

	// Register workflow actions WITHOUT notifier.
	require.NoError(t, actions.RegisterWorkflowActions(reg, actions.WorkflowActionDeps{
		Store:  s,
		Hub:    hub,
		Logger: slog.Default(),
	}))

	srv := opcmcp.NewOpcodeServer(opcmcp.OpcodeServerDeps{
		Executor: exec, Store: s, Registry: reg, Hub: hub,
	})

	env := &testEnv{
		store: s, eventLog: eventLog, registry: reg,
		executor: exec, hub: hub, server: srv,
	}

	ctx := context.Background()
	require.NoError(t, env.store.RegisterAgent(ctx, &store.Agent{
		ID: "no-notifier-agent", Name: "No Notifier", Type: "system",
	}))

	defineResult := env.callTool(t, "opcode.define", map[string]any{
		"name": "no-notifier-test",
		"definition": map[string]any{
			"steps": []any{
				map[string]any{"id": "s1", "action": "noop"},
				map[string]any{
					"id": "n1", "action": "workflow.notify",
					"params":     map[string]any{"message": "test"},
					"depends_on": []any{"s1"},
				},
			},
		},
		"agent_id": "no-notifier-agent",
	})
	require.False(t, defineResult.IsError)

	runResult := env.callTool(t, "opcode.run", map[string]any{
		"template_name": "no-notifier-test",
		"agent_id":      "no-notifier-agent",
	})
	require.False(t, runResult.IsError)

	var runOut map[string]any
	extractJSON(t, runResult, &runOut)
	assert.Equal(t, "completed", runOut["status"])

	steps, _ := runOut["steps"].(map[string]any)
	n1step, _ := steps["n1"].(map[string]any)
	n1out, _ := n1step["output"].(map[string]any)
	assert.Equal(t, false, n1out["notified"])
	assert.Equal(t, "no_notifier", n1out["reason"])
}
