---
name: opcode
description: >
  Agent-first workflow orchestration engine exposed via MCP (stdio).
  Define, execute, monitor, and signal workflows with 5 MCP tools.
  Supports DAG-based execution, 6 step types (action, condition, loop,
  parallel, wait, reasoning), 24+ built-in actions, ${{}} interpolation,
  reasoning nodes for human-in-the-loop decisions, and secret vault.
  Use when defining workflows, running templates, checking status,
  sending signals, or querying workflow history.
license: MIT
metadata:
  version: "1.0"
  transport: stdio
---

# OPCODE

Workflow orchestration engine for AI agents. Workflows are JSON-defined DAGs executed level-by-level with automatic parallelism. Communication happens via 5 MCP tools over stdio (JSON-RPC).

**Token economy**: define a template once, execute it unlimited times for free (no re-reasoning). Reasoning nodes store decisions as events and never replay them.

## Prerequisites

- **Go 1.25+** (required)
- **CGO enabled** (`CGO_ENABLED=1`) — required by embedded libSQL (go-libsql)
- **C compiler** — `gcc` or `clang` for CGO compilation

## Installation

### Build from source

```bash
go build -o opcode ./cmd/opcode/
```

### As a Go module dependency

```bash
go get github.com/rendis/opcode@latest
```

## Running

OPCODE runs as a **stdio MCP server** (JSON-RPC over stdin/stdout). It is not a standalone daemon — your MCP client starts it as a subprocess.

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `OPCODE_DB_PATH` | `opcode.db` | Path to embedded libSQL database file |
| `OPCODE_VAULT_KEY` | _(empty)_ | Passphrase for secret vault. If unset, `${{secrets.*}}` interpolation is disabled |
| `OPCODE_POOL_SIZE` | `10` | Worker pool size for parallel step execution |
| `OPCODE_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |

### MCP Client Configuration

Add opcode as an MCP server in your client config. Examples:

**Claude Desktop** (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "opcode": {
      "command": "/path/to/opcode",
      "env": {
        "OPCODE_DB_PATH": "/path/to/opcode.db",
        "OPCODE_VAULT_KEY": "your-secret-passphrase"
      }
    }
  }
}
```

**Claude Code** (`.mcp.json` in project root):

```json
{
  "mcpServers": {
    "opcode": {
      "command": "/path/to/opcode",
      "env": {
        "OPCODE_DB_PATH": "/path/to/opcode.db",
        "OPCODE_VAULT_KEY": "your-secret-passphrase"
      }
    }
  }
}
```

The MCP client launches the binary, communicates via stdio, and exposes the 5 tools below to the agent.

### Startup Sequence

1. Opens/creates libSQL database at `OPCODE_DB_PATH`, runs migrations
2. Initializes secret vault (if `OPCODE_VAULT_KEY` set)
3. Registers 24 built-in actions
4. Starts cron scheduler (recovers missed jobs)
5. Begins listening for MCP JSON-RPC on stdin/stdout
6. Shuts down gracefully on SIGTERM/SIGINT (10s timeout)

## MCP Tools

### opcode.define

Register a reusable workflow template. Version auto-increments (v1, v2, v3...).

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Template name |
| `definition` | object | yes | Workflow definition (see below) |
| `agent_id` | string | yes | Defining agent ID |
| `description` | string | no | Template description |
| `input_schema` | object | no | JSON Schema for input validation |
| `output_schema` | object | no | JSON Schema for output validation |
| `triggers` | object | no | Trigger configuration |

**Returns**: `{ "name": "...", "version": "v1" }`

### opcode.run

Execute a workflow from a registered template.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `template_name` | string | yes | Template to execute |
| `agent_id` | string | yes | Initiating agent ID |
| `version` | string | no | Version (default: latest) |
| `params` | object | no | Input parameters |

**Returns**: ExecutionResult with `workflow_id`, `status`, `output`, per-step results.

### opcode.status

Get workflow execution status.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `workflow_id` | string | yes | Workflow to query |

**Returns**: `{ "status": "...", "steps": {...}, "output": {...} }`

Workflow statuses: `pending`, `active`, `suspended`, `completed`, `failed`, `cancelled`.

### opcode.signal

Send a signal to a suspended workflow.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `workflow_id` | string | yes | Target workflow |
| `signal_type` | enum | yes | `decision` / `data` / `cancel` / `retry` / `skip` |
| `payload` | object | yes | Signal payload |
| `step_id` | string | no | Target step (required for decision signals) |
| `agent_id` | string | no | Signaling agent |
| `reasoning` | string | no | Agent's reasoning |

For decisions: `payload: { "choice": "<option_id>" }`.

**Returns**: `{ "ok": true, "workflow_id": "...", "signal_type": "..." }`

### opcode.query

Query workflows, events, or templates.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `resource` | enum | yes | `workflows` / `events` / `templates` |
| `filter` | object | no | Filter criteria |

**Filter fields by resource**:

| Resource | Fields |
|----------|--------|
| `workflows` | `status`, `agent_id`, `since` (RFC3339), `limit` |
| `events` | `workflow_id`, `step_id`, `event_type`, `since`, `limit` |
| `templates` | `name`, `agent_id`, `limit` |

Note: event queries require either `event_type` or `workflow_id` in filter.

## Workflow Definition

```json
{
  "steps": [ ... ],
  "inputs": { "key": "value or ${{secrets.KEY}}" },
  "timeout": "5m",
  "on_timeout": "fail | suspend | cancel",
  "on_complete": { /* step definition */ },
  "on_error": { /* step definition */ },
  "metadata": {}
}
```

### Step Definition

```json
{
  "id": "step-id",
  "type": "action | condition | loop | parallel | wait | reasoning",
  "action": "http.get",
  "params": { ... },
  "depends_on": ["other-step"],
  "condition": "CEL guard expression",
  "timeout": "30s",
  "retry": { "max": 3, "backoff": "exponential", "delay": "1s", "max_delay": "30s" },
  "on_error": { "strategy": "ignore | fail_workflow | fallback_step | retry", "fallback_step": "id" },
  "config": { /* type-specific */ }
}
```

`type` defaults to `action`. The `config` field varies by type. See [workflow-schema.md](references/workflow-schema.md) for all config blocks.

## Step Types

### action (default)

Executes a registered action. Set `action` to the action name, `params` for input.

### condition

Evaluates a CEL expression and branches. Config: `expression`, `branches` (map value -> steps), `default`.

```json
{ "expression": "inputs.env", "branches": { "prod": [...], "staging": [...] }, "default": [...] }
```

### loop

Iterates over a collection or condition. Config: `mode` (for_each/while/until), `over`, `condition`, `body`, `max_iter`.

```json
{ "mode": "for_each", "over": "[\"a\",\"b\",\"c\"]", "body": [...], "max_iter": 100 }
```

Loop variables: `${{loop.item}}`, `${{loop.index}}`.

### parallel

Executes branches concurrently. Config: `branches` (array of step arrays), `mode` (all/race).

```json
{ "mode": "all", "branches": [ [{...}], [{...}] ] }
```

### wait

Delays execution. Config: `duration` or `signal`.

```json
{ "duration": "5s" }
```

### reasoning

Suspends workflow for agent decision. Config: `prompt_context`, `options`, `timeout`, `fallback`, `data_inject`, `target_agent`.

```json
{
  "prompt_context": "Review data and decide",
  "options": [ {"id": "approve", "description": "Proceed"}, {"id": "reject", "description": "Stop"} ],
  "timeout": "1h",
  "fallback": "reject"
}
```

Empty `options` array = free-form (any choice accepted).

## Variable Interpolation

Syntax: `${{namespace.path}}`

| Namespace | Example | Description |
|-----------|---------|-------------|
| `steps` | `${{steps.fetch.output.body}}` | Previous step outputs |
| `inputs` | `${{inputs.api_key}}` | Workflow input params |
| `workflow` | `${{workflow.run_id}}` | Workflow metadata |
| `context` | `${{context.intent}}` | Workflow context |
| `secrets` | `${{secrets.DB_PASS}}` | Encrypted vault secrets |
| `loop` | `${{loop.item}}`, `${{loop.index}}` | Loop iteration vars |

Two-pass resolution: non-secrets first, then secrets via AES-256-GCM vault.

**CEL gotcha**: `loop` is a reserved word in CEL. Use `iter.item` / `iter.index` in CEL expressions. The `${{loop.item}}` interpolation syntax is unaffected.

See [expressions.md](references/expressions.md) for CEL, GoJQ, Expr engine details.

## Built-in Actions

| Category | Actions |
|----------|---------|
| **HTTP** | `http.request`, `http.get`, `http.post` |
| **Filesystem** | `fs.read`, `fs.write`, `fs.append`, `fs.delete`, `fs.list`, `fs.stat`, `fs.copy`, `fs.move` |
| **Shell** | `shell.exec` |
| **Crypto** | `crypto.hash`, `crypto.hmac`, `crypto.uuid` |
| **Assert** | `assert.equals`, `assert.contains`, `assert.matches`, `assert.schema` |
| **Workflow** | `workflow.run`, `workflow.emit`, `workflow.context`, `workflow.fail`, `workflow.log` |

See [actions.md](references/actions.md) for full parameter specs.

## Scripting with shell.exec

`shell.exec` supports any language (Bash, Python, Node, Go, etc.). Scripts receive input via **stdin** and produce output via **stdout**. JSON stdout is **auto-parsed** — access fields directly with `${{steps.cmd.output.stdout.field}}`.

| Language | Command | Args | Boilerplate |
|----------|---------|------|-------------|
| Bash | `bash` | `["script.sh"]` | `set -euo pipefail; input=$(cat -)` |
| Python | `python3` | `["script.py"]` | `json.load(sys.stdin)` → `json.dump(result, sys.stdout)` |
| Node | `node` | `["script.js"]` | Read stdin stream → `JSON.parse` → `JSON.stringify` |
| Go | `go` | `["run","script.go"]` | `json.NewDecoder(os.Stdin)` → `json.NewEncoder(os.Stdout)` |

**Convention**: stdin=JSON, stdout=JSON, stderr=errors, non-zero exit=failure. Use `stdout_raw` for unprocessed text.

See [patterns.md](references/patterns.md#10-scripting-with-shellexec) for full templates.

## Reasoning Node Lifecycle

1. Workflow reaches a reasoning step
2. Executor creates PendingDecision, emits `decision_requested` event
3. Workflow status becomes `suspended`
4. Agent calls `opcode.status` to see pending decision with context and options
5. Agent resolves via `opcode.signal`:
   ```json
   { "workflow_id": "...", "signal_type": "decision", "step_id": "reason-step", "payload": { "choice": "approve" } }
   ```
6. Workflow auto-resumes after signal
7. If timeout expires: `fallback` option auto-selected, or step fails if no fallback

## Common Patterns

See [patterns.md](references/patterns.md) for complete JSON examples:

- **Linear pipeline** -- steps chained via `depends_on`
- **Conditional branching** -- CEL expression routes to branches
- **For-each loop** -- iterate over a collection
- **Parallel fan-out** -- concurrent branches with all/race mode
- **Human-in-the-loop** -- reasoning node for agent approval
- **Error recovery** -- retry + fallback_step
- **Sub-workflow** -- `workflow.run` calls child templates
- **MCP lifecycle** -- define -> run -> status -> signal -> status

## Error Handling

| Strategy | Behavior |
|----------|----------|
| `ignore` | Step skipped, workflow continues |
| `fail_workflow` | Entire workflow fails |
| `fallback_step` | Execute fallback step |
| `retry` | Defer to retry policy |

Backoff: `none`, `linear`, `exponential`, `constant`. Non-retryable errors (validation, permission, assertion) are never retried.

See [error-handling.md](references/error-handling.md) for circuit breakers, timeout interactions, error codes.
