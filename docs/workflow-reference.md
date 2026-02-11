# OPCODE — Workflow Reference

## Error Handling

Each step can define an `on_error` strategy:

| Strategy        | Behavior                                                    |
| --------------- | ----------------------------------------------------------- |
| `ignore`        | Mark step as completed (error swallowed), continue workflow |
| `fail_workflow` | Immediately fail the entire workflow                        |
| `fallback_step` | Execute a designated fallback step                          |
| `retry`         | Defer to the retry policy                                   |

## Retry Policies

```json
{
  "retry": {
    "max": 5,
    "backoff": "exponential",
    "delay": "500ms",
    "max_delay": "30s"
  }
}
```

Backoff strategies: `none` (immediate retry), `linear` (delay _ attempt), `exponential` (delay _ 2^attempt), `constant` (fixed delay). Non-retryable errors (validation, permission denied, assertion failures) are never retried regardless of policy.

## Workflow Timeout

The `timeout` field at the workflow level sets a hard deadline. The `on_timeout` field controls behavior when the deadline fires:

| Value            | Behavior                                             |
| ---------------- | ---------------------------------------------------- |
| `fail` (default) | Fail the workflow with a timeout error               |
| `suspend`        | Suspend the workflow (can be resumed later)          |
| `cancel`         | Cancel the workflow, skipping all non-terminal steps |

---

## Built-in Actions

OPCODE ships with a comprehensive set of built-in actions:

### HTTP Actions

| Action         | Description                                                       |
| -------------- | ----------------------------------------------------------------- |
| `http.request` | General HTTP request with full control over method, headers, body |
| `http.get`     | Convenience GET request                                           |
| `http.post`    | Convenience POST request                                          |

### Filesystem Actions

| Action      | Description                                |
| ----------- | ------------------------------------------ |
| `fs.read`   | Read file contents                         |
| `fs.write`  | Write content to a file                    |
| `fs.list`   | List directory contents                    |
| `fs.stat`   | Get file metadata (size, mode, timestamps) |
| `fs.delete` | Delete a file                              |

Filesystem actions respect the isolator's path validation (deny paths, read-only paths, writable paths).

### Shell Actions

| Action       | Description                                                               |
| ------------ | ------------------------------------------------------------------------- |
| `shell.exec` | Execute a shell command with timeout and resource limits via the isolator |

### Crypto Actions

| Action          | Description                              |
| --------------- | ---------------------------------------- |
| `crypto.hash`   | Compute SHA-256 (or other) hash of input |
| `crypto.hmac`   | Compute HMAC signature                   |
| `crypto.uuid`   | Generate a new UUID                      |
| `crypto.encode` | Encode data (base64, hex)                |
| `crypto.decode` | Decode data (base64, hex)                |

### Assert Actions

| Action          | Description                         |
| --------------- | ----------------------------------- |
| `assert.equal`  | Assert two values are equal         |
| `assert.schema` | Validate data against a JSON Schema |
| `assert.truthy` | Assert a value is truthy            |

### Workflow Actions

| Action             | Description                                                     |
| ------------------ | --------------------------------------------------------------- |
| `workflow.run`     | Execute a child workflow from a template (sub-workflow)         |
| `workflow.emit`    | Publish a custom event to the EventHub                          |
| `workflow.context` | Read or update workflow context (accumulated data, agent notes) |
| `workflow.fail`    | Force-fail the current workflow with a reason                   |
| `workflow.log`     | Write a structured log entry with workflow context              |
| `workflow.notify`  | Push a real-time notification to the agent via MCP SSE          |

### Plugin Actions

External MCP servers can be loaded as plugins. Their tools are discovered via `tools/list` and registered in the action registry under a namespaced prefix (e.g., `github.create_issue`).

### Real-time Notifications

`workflow.notify` pushes notifications to the agent via MCP SSE. The agent decides where to be notified by placing `workflow.notify` steps anywhere in the workflow template. The workflow's `agent_id` determines who receives the notification. Best-effort: if the agent is not connected, the step completes without error.

```json
{
  "id": "alert",
  "action": "workflow.notify",
  "params": { "message": "Task complete", "data": "${{steps.process.output}}" },
  "depends_on": ["process"]
}
```

---

## Expression Interpolation

OPCODE supports `${{...}}` variable interpolation in step parameters. Interpolation is resolved in two passes: first non-secret variables, then secrets.

### Available Scopes

| Scope             | Syntax                              | Description                                           |
| ----------------- | ----------------------------------- | ----------------------------------------------------- |
| Step outputs      | `${{steps.step_id.field}}`          | Output from a completed step                          |
| Workflow inputs   | `${{inputs.param_name}}`            | Workflow input parameters                             |
| Workflow metadata | `${{workflow.run_id}}`              | Workflow execution metadata                           |
| Context           | `${{context.intent}}`               | Workflow context (intent, agent_id, accumulated data) |
| Secrets           | `${{secrets.KEY}}`                  | Resolved from the encrypted vault at runtime          |
| Loop variables    | `${{loop.item}}`, `${{loop.index}}` | Available inside loop step bodies                     |

### Expression Engines

Three expression engines are available for conditions and transforms:

| Engine   | Use Case                                    | Syntax Example                     |
| -------- | ------------------------------------------- | ---------------------------------- |
| **CEL**  | Condition evaluation, type-safe expressions | `steps.fetch.status_code == 200`   |
| **GoJQ** | JSON transformation and querying            | `.data[] \| select(.active)`       |
| **Expr** | General-purpose logic expressions           | `len(items) > 0 && items[0].valid` |

Note: CEL reserves the word `loop`. Inside CEL expressions, use `iter.item` and `iter.index` instead of `loop.item` / `loop.index`. The `${{loop.item}}` interpolation syntax is unaffected.

---

## Available Commands

| Command                                                              | Description                                |
| -------------------------------------------------------------------- | ------------------------------------------ |
| `go build -C /path/to/opcode ./...`                                  | Build all packages (verify compilation)    |
| `go build -C /path/to/opcode -o opcode ./cmd/opcode/`                | Build the opcode binary                    |
| `go test -C /path/to/opcode ./... -count=1 -timeout 60s`             | Run the full test suite (981 tests)        |
| `go test -C /path/to/opcode ./internal/engine/ -run TestExecutor -v` | Run a specific test subset                 |
| `go test -C /path/to/opcode ./internal/engine/ -v`                   | Run all engine tests verbosely             |
| `go test -C /path/to/opcode ./tests/e2e/ -v`                         | Run end-to-end integration tests           |
| `bash scripts/test-linux.sh`                                         | Run Linux cgroup isolation tests in Docker |
| `go vet -C /path/to/opcode ./...`                                    | Run Go static analysis                     |
