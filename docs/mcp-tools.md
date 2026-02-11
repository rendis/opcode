# OPCODE — MCP Tools Reference

OPCODE exposes six MCP tools over SSE.

## opcode.run

Execute a workflow from a registered template.

| Parameter       | Required | Description                             |
| --------------- | -------- | --------------------------------------- |
| `template_name` | Yes      | Name of the workflow template           |
| `version`       | No       | Template version (default: latest)      |
| `params`        | No       | Input parameters as a JSON object       |
| `agent_id`      | Yes      | ID of the agent initiating the workflow |

Returns an `ExecutionResult` with `workflow_id`, `status`, `output`, `error`, and per-step results.

## opcode.status

Get the current state of a workflow.

| Parameter     | Required | Description                 |
| ------------- | -------- | --------------------------- |
| `workflow_id` | Yes      | ID of the workflow to query |

Returns the workflow status, step states, pending decisions, workflow context, and event history.

## opcode.signal

Send a signal to a suspended workflow (e.g., resolve a reasoning decision).

| Parameter     | Required | Description                                              |
| ------------- | -------- | -------------------------------------------------------- |
| `workflow_id` | Yes      | Target workflow ID                                       |
| `signal_type` | Yes      | One of:`decision`, `data`, `cancel`, `retry`, `skip`     |
| `payload`     | Yes      | Signal payload (for decisions:`{"choice": "option_id"}`) |
| `step_id`     | No       | Target step ID (required for decision signals)           |
| `agent_id`    | No       | ID of the signaling agent                                |
| `reasoning`   | No       | Agent's reasoning for the signal                         |

## opcode.define

Register a reusable workflow template with auto-versioning.

| Parameter       | Required | Description                       |
| --------------- | -------- | --------------------------------- |
| `name`          | Yes      | Template name                     |
| `definition`    | Yes      | Workflow definition object        |
| `agent_id`      | Yes      | ID of the defining agent          |
| `description`   | No       | Template description              |
| `input_schema`  | No       | JSON Schema for input validation  |
| `output_schema` | No       | JSON Schema for output validation |
| `triggers`      | No       | Trigger configuration             |

Returns the template `name` and auto-incremented `version` (v1, v2, v3...).

## opcode.query

Query workflows, events, or templates with filtering.

| Parameter  | Required | Description                               |
| ---------- | -------- | ----------------------------------------- |
| `resource` | Yes      | One of:`workflows`, `events`, `templates` |
| `filter`   | No       | Filter criteria object                    |

**Filter fields by resource:**

| Resource    | Filter Fields                                            |
| ----------- | -------------------------------------------------------- |
| `workflows` | `status`, `agent_id`, `since` (RFC3339), `limit`         |
| `events`    | `workflow_id`, `step_id`, `event_type`, `since`, `limit` |
| `templates` | `name`, `agent_id`, `limit`                              |

## opcode.diagram

Generate a visual representation of a workflow DAG.

| Parameter        | Required | Description                                                    |
| ---------------- | -------- | -------------------------------------------------------------- |
| `template_name`  | No\*     | Template to visualize (structure preview before execution)     |
| `version`        | No       | Template version (default: latest)                             |
| `workflow_id`    | No\*     | Running/completed workflow to visualize (with runtime status)  |
| `format`         | Yes      | Output format:`ascii`, `mermaid`, or `image`                   |
| `include_status` | No       | Show runtime status overlay (default: true if workflow_id set) |

\* One of `template_name` or `workflow_id` is required.

**Use cases:**

- `template_name` -- Preview a template's DAG structure before running it
- `workflow_id` -- Visualize a workflow with live step status (completed, running, suspended, pending)
- `format: "ascii"` -- Text output for CLI agents and terminal display
- `format: "mermaid"` -- Flowchart syntax for markdown renderers and web UIs
- `format: "image"` -- Base64-encoded PNG for visual channels (Telegram, WhatsApp, Slack)

---

## Agent Identity

Agents identify themselves by passing an `agent_id` string in every tool call. Opcode auto-registers new agents in the `agents` table on first contact (type: `llm`). There is no authentication -- agents choose their own ID.

**In multi-agent setups**, each agent should use a stable, unique ID so workflows are correctly attributed. Use `opcode.query` with `filter: { "agent_id": "my-agent" }` to list only workflows owned by a specific agent.

Agent types: `llm` (default for auto-registered), `system`, `human`, `service`.

---

## Connecting via mcporter

[mcporter](https://github.com/steipete/mcporter) is a universal MCP CLI client (npm).

```bash
mcporter config add opcode http://localhost:4100/sse --allow-http
mcporter list opcode --schema       # 6 tools with JSON schemas
```

Call tools (single-quote to protect parentheses from bash):

```bash
mcporter call 'opcode.run(template_name: "my-wf", agent_id: "bot-1")'
```

Or flag-based:

```bash
mcporter call --server opcode --tool opcode.define --args '{
  "name": "my-workflow",
  "agent_id": "bot-1",
  "definition": { "steps": [...] }
}'
```

Manual config (`~/.mcporter/mcporter.json`):

```json
{
  "mcpServers": {
    "opcode": {
      "url": "http://localhost:4100/sse"
    }
  }
}
```

> Non-HTTPS endpoints require `--allow-http` on each call, or `--insecure` as alias.
