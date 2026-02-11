# OPCODE — Design Rationale & Trade-offs

## 1. DAG + FSM: Complementary, Not "Versus"

A workflow engine must solve two distinct problems:

1. **Scheduling**: What steps execute in what order? Which can run in parallel?
2. **Lifecycle**: What state is each step/workflow in? What transitions are valid?

### Why Not Pure FSM

An FSM models state transitions (`pending → active → completed`). But it cannot express parallelism without composing states combinatorially. If steps A and B are independent, a pure FSM needs `A_running_B_running`, `A_done_B_running`, etc. — exponential blowup.

### Why Not Pure DAG

A DAG models dependencies and enables automatic parallelism via topological sort. But it has no concept of "state" — it cannot express suspension, retry, or cancellation.

### Combined: DAG for scheduling + FSM for lifecycle

| Layer        | Responsibility                                                    | Implementation                       |
| ------------ | ----------------------------------------------------------------- | ------------------------------------ |
| **DAG**      | Execution order, parallelism (Kahn's sort → levels)               | `internal/engine/dag.go` (273 lines) |
| **FSM**      | Valid state transitions, lifecycle management                     | `internal/engine/fsm.go`             |
| **Executor** | Walks DAG level-by-level, dispatches to WorkerPool, FSM validates | `internal/engine/executor.go`        |

Parallelism is automatic: if steps share no dependencies, the DAG parser places them in the same level and the executor dispatches them concurrently.

---

## 2. CEL Over Go Templates

### Problem

Workflows need conditional expressions: "execute step X only if step Y returned status 200 and input priority > 5."

### CEL

```cel
steps['fetch'].output.status_code == 200 && inputs.priority > 5
```

- Type-safe at compile time
- Sandboxed: no side effects, no I/O, no arbitrary code execution
- Production: Google Kubernetes admission policies, Envoy routing, Firebase security rules
- Thread-safe: compiled `cel.Program` reusable across goroutines
- Performance: ~1-10us per evaluation

### Go Templates (alternative considered)

```plaintext
{{if and (eq .Steps.Fetch.Output.StatusCode 200) (gt .Inputs.Priority 5)}}
```

- No type safety: missing fields return empty string silently
- Not sandboxed: `FuncMap` allows arbitrary Go function execution
- Verbose: simple boolean logic requires deep nesting
- String-based: output is always string, requires parsing for booleans/numbers

### Three-Engine Stack (D004)

| Engine   | Domain             | Why                                              |
| -------- | ------------------ | ------------------------------------------------ |
| **CEL**  | Conditions, guards | Type-safe, sandboxed, Google production          |
| **GoJQ** | JSON transforms    | jq syntax, composable pipes, no external process |
| **Expr** | Business logic     | Let bindings, nil coalescing, readable chains    |

Starlark (originally considered) was rejected: in agent-first architecture, the agent IS the dynamic logic. Expressions must be deterministic, not Turing-complete.

---

## 3. libSQL — CGO Trade-off

### Why CGO

`go-libsql` requires CGO (`//go:build cgo`). A C compiler is needed at build time and cross-compilation requires a cross-compiling C toolchain (`CC=x86_64-linux-musl-gcc` etc.).

This is an accepted trade-off. The README documents CGO requirements in Prerequisites, Deployment, and Troubleshooting sections.

### Why libSQL Over Alternatives

Primary reason: **concurrency model**.

libSQL provides WAL mode: concurrent readers while a single writer holds `BEGIN IMMEDIATE`. For a workflow engine where the read path (status queries, event replay) vastly outnumbers the write path (event appends), this is ideal.

| Driver               | CGO       | WAL Concurrency | Encryption | Embedded Replicas |
| -------------------- | --------- | --------------- | ---------- | ----------------- |
| `mattn/go-sqlite3`   | Yes       | Yes             | No         | No                |
| `modernc.org/sqlite` | No        | Yes             | No         | No                |
| `ncruces/go-sqlite3` | No (WASM) | Yes             | No         | No                |
| **`go-libsql`**      | **Yes**   | **Yes**         | **Yes**    | **Yes**           |

`modernc.org/sqlite` and `ncruces/go-sqlite3` avoid CGO but lack encryption at rest (needed for the secrets table) and embedded replicas.

### Mitigation

The `Store` interface (`internal/store/store.go`) abstracts persistence — swapping to another driver requires only a new implementation, no engine changes.

---

## 4. Event Sourcing Over CRUD

### CRUD

```sql
UPDATE step_state SET status = 'completed', output = '...' WHERE step_id = 'X';
```

Loses history. No replay capability. No audit trail without separate table.

### Event Sourcing

```sql
INSERT INTO events (workflow_id, sequence, type, data) VALUES (?, ?, ?, ?);
```

- Full history: event log IS the audit trail
- Deterministic replay: load events, reconstruct state, continue from last checkpoint
- Crash recovery: restart → replay → resume with zero data loss
- Reasoning nodes: agent decisions stored as events, loaded on replay (never re-executed)
- `step_state` is a materialized view rebuilt from events — optimizes status queries

Trade-off: more storage, but `UNIQUE(workflow_id, sequence)` ensures ordering and the append-only pattern prevents concurrency conflicts.

---

## 5. Scalability Analysis

### Worker Pool

Default `PoolSize = 10` (configurable via `--pool-size`, `OPCODE_POOL_SIZE`, or `settings.json`). Semaphore pattern — `Submit()` blocks when pool is full (backpressure). 10 is a conservative default, not a limit.

- I/O-bound tasks (HTTP, shell): 100-500 workers reasonable
- CPU-bound tasks (expressions): 1-2x `GOMAXPROCS` optimal

### Theoretical Throughput

| Scenario                    | Active goroutines | Events/sec | Viability                                      |
| --------------------------- | ----------------- | ---------- | ---------------------------------------------- |
| 100 workflows x 10 steps    | 10-50             | ~1k        | Trivial                                        |
| 1,000 workflows x 10 steps  | 50-100            | ~10k       | Viable (libSQL WAL ~50k writes/s)              |
| 10,000 workflows x 10 steps | 100-500           | ~100k      | Bottleneck: libSQL single-writer serialization |

### Bottleneck: libSQL Write Serialization

`BEGIN IMMEDIATE` serializes all write transactions. Each step emits ~2-3 events (started, completed/failed). At 1000 concurrent workflows with 10 steps each: ~30k writes. libSQL WAL handles ~50k simple writes/sec on NVMe — viable.

Beyond 10k concurrent workflows, libSQL becomes the bottleneck. Mitigation options: batch event coalescing, Turso cloud sync with distributed writes, or sharding by workflow ID.

### Safety Valves

| Mechanism                         | Protects Against             |
| --------------------------------- | ---------------------------- |
| Worker pool semaphore             | Unbounded goroutine spawning |
| Circuit breaker per-action        | Cascading failures           |
| Step timeout                      | Hung steps                   |
| Workflow timeout                  | Runaway workflows            |
| `max_concurrency` in parallel.map | Fan-out explosion            |
