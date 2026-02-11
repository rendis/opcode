# Multi-Agent Review

Two agents coordinating through the same OPCODE instance using reasoning nodes as synchronization points.

## Agents

| Agent                | Role                                                             | Actions                                        |
| -------------------- | ---------------------------------------------------------------- | ---------------------------------------------- |
| **researcher-agent** | Fetches data, runs analysis, defines and executes workflow       | `opcode.define`, `opcode.run`, `opcode.status` |
| **reviewer-agent**   | Reviews results, approves or rejects via targeted reasoning node | `opcode.status`, `opcode.signal`               |

## Flow

```
researcher-agent                OPCODE                      reviewer-agent
     │                            │                              │
     │── opcode.run(workflow) ──→ │                              │
     │                            │── [fetch-data] GET ──→       │
     │                            │── [analyze] filter ──→       │
     │                            │                              │
     │                            │── [review-gate] SUSPEND ──→  │
     │                            │   target_agent: reviewer     │
     │                            │                              │
     │── opcode.status ─────────→ │                              │
     │←── {suspended, pending} ── │                              │
     │                            │                              │
     │                            │←── opcode.status ────────────│
     │                            │──→ {pending_decision} ───────│
     │                            │                              │
     │                            │←── opcode.signal(approve) ───│
     │                            │                              │
     │                            │── [publish] POST ──→         │
     │                            │── [log-result] ──→           │
     │                            │                              │
     │── opcode.status ─────────→ │                              │
     │←── {completed} ─────────── │                              │
```

## MCP Tool Calls

### researcher-agent runs workflow

```json
// opcode.run
{
  "template": "multi-agent-review",
  "inputs": {
    "data_url": "https://api.example.com/data",
    "threshold": 0.85
  }
}
// Response: {"workflow_id": "wf-abc-123", "status": "active"}
```

### researcher-agent checks status

```json
// opcode.status
{ "workflow_id": "wf-abc-123" }
// Response: {"status": "suspended", "pending_decisions": [...]}
```

### reviewer-agent discovers and resolves decision

```json
// opcode.status (discovers pending decision targeted at reviewer-agent)
{"workflow_id": "wf-abc-123"}

// opcode.signal (resolves the decision)
{
  "workflow_id": "wf-abc-123",
  "step_id": "review-gate",
  "payload": {
    "choice": "approve",
    "reason": "Results meet quality threshold"
  }
}
// Response: {"status": "accepted"}
```

## Key Design Points

- **`target_agent`** in reasoning config routes the decision to a specific agent
- The workflow suspends until the target agent calls `opcode.signal`
- Both agents connect to the same OPCODE instance via MCP
- `timeout` + `fallback` provide safety: if reviewer-agent doesn't respond within 2h, defaults to "reject"
- No authentication between agents in current design (single-instance trust model)
