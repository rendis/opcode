# OPCODE — Testing

## Running Tests

```bash
# Run the full test suite
go test -C /path/to/opcode ./... -count=1 -timeout 60s

# Run tests for a specific package
go test -C /path/to/opcode ./internal/engine/ -v

# Run tests matching a pattern
go test -C /path/to/opcode ./internal/engine/ -run TestExecutor -v

# Run with race detector
go test -C /path/to/opcode ./... -race -count=1 -timeout 120s
```

## Test Architecture

Tests run directly on macOS with the FallbackIsolator. Linux-specific isolation tests (cgroups v2, PID namespaces, OOM kills) run inside Docker:

```bash
# Build and run Linux isolation tests
bash scripts/test-linux.sh
```

This builds a Docker image from `Dockerfile.test` and runs it with `--privileged --cgroupns=host` for full cgroup access.

## Test Organization

| Package                 | Test Focus                                                                    |
| ----------------------- | ----------------------------------------------------------------------------- |
| `internal/store/`       | Real embedded libSQL (temp file per test), CRUD operations, event replay      |
| `internal/engine/`      | Executor, DAG parsing, FSM transitions, retry, circuit breakers, flow control |
| `internal/actions/`     | Action execution, input validation, HTTP/FS/shell/crypto/assert actions       |
| `internal/expressions/` | CEL/GoJQ/Expr evaluation, interpolation, scope building                       |
| `internal/isolation/`   | Path validation, FallbackIsolator (macOS), LinuxIsolator (Docker)             |
| `internal/reasoning/`   | Decision context building, resolution validation                              |
| `internal/identity/`    | Agent registration and validation                                             |
| `internal/secrets/`     | AES vault encrypt/decrypt, key derivation                                     |
| `internal/streaming/`   | EventHub publish/subscribe, fan-out                                           |
| `internal/validation/`  | Workflow definition validation, DAG checks, JSON Schema                       |
| `internal/plugins/`     | Plugin lifecycle, MCP handshake, action discovery                             |
| `internal/scheduler/`   | Cron parsing, job scheduling, missed-run recovery                             |
| `internal/diagram/`     | Diagram model building, ASCII/Mermaid/PNG rendering                           |
| `pkg/mcp/`              | MCP tool handlers (including diagram), request/response serialization         |
| `pkg/schema/`           | Validation result types                                                       |
| `tests/e2e/`            | Full end-to-end workflow execution, MCP integration                           |

## Key Testing Patterns

- **Store tests** use real embedded libSQL with a temporary database file per test -- no mocks for persistence
- **Engine tests** use the `EventLogger` interface with mock implementations to avoid CGO dependencies in unit tests
- **Test helpers** are prefixed per file to avoid naming collisions (e.g., `execActionStep` in executor_test vs `actionStep` in dag_test)
- **Assertions** use `testify/assert` and `testify/require`
