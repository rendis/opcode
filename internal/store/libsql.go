package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/tursodatabase/go-libsql"

	"github.com/rendis/opcode/pkg/schema"
)

// LibSQLStore implements the Store interface using libSQL (embedded SQLite fork).
type LibSQLStore struct {
	db *sql.DB
}

// NewLibSQLStore opens a libSQL database at the given path and returns a Store.
// The path should be a file URI, e.g. "file:/path/to/db.db".
func NewLibSQLStore(dbPath string) (*LibSQLStore, error) {
	db, err := sql.Open("libsql", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open libsql: %w", err)
	}
	db.SetMaxOpenConns(1)

	// Apply connection-level PRAGMAs. Some PRAGMAs return rows so we use QueryRow.
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA cache_size=-20000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA temp_store=MEMORY",
	}
	for _, p := range pragmas {
		var result string
		_ = db.QueryRow(p).Scan(&result)
	}

	return &LibSQLStore{db: db}, nil
}

// DB returns the underlying *sql.DB for advanced usage (e.g. event log).
func (s *LibSQLStore) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *LibSQLStore) Close() error { return s.db.Close() }

// Migrate runs all pending database migrations.
func (s *LibSQLStore) Migrate(ctx context.Context) error {
	return runMigrations(ctx, s.db)
}

// Vacuum runs VACUUM on the database.
func (s *LibSQLStore) Vacuum(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "VACUUM")
	return err
}

// --- Agents ---

func (s *LibSQLStore) RegisterAgent(ctx context.Context, agent *Agent) error {
	metadata, err := nullableJSON(agent.Metadata)
	if err != nil {
		return fmt.Errorf("marshal agent metadata: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO agents (id, name, type, metadata, created_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, type=excluded.type, metadata=excluded.metadata`,
		agent.ID, agent.Name, agent.Type, metadata, timeOrNow(agent.CreatedAt),
	)
	return err
}

func (s *LibSQLStore) GetAgent(ctx context.Context, id string) (*Agent, error) {
	a := &Agent{}
	var metadata sql.NullString
	var lastSeen sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, type, metadata, created_at, last_seen_at FROM agents WHERE id = ?`, id,
	).Scan(&a.ID, &a.Name, &a.Type, &metadata, &a.CreatedAt, &lastSeen)
	if err == sql.ErrNoRows {
		return nil, storeNotFound("agent", id)
	}
	if err != nil {
		return nil, err
	}
	a.Metadata = jsonOrNil(metadata)
	if lastSeen.Valid {
		a.LastSeenAt = &lastSeen.Time
	}
	return a, nil
}

func (s *LibSQLStore) UpdateAgentSeen(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE agents SET last_seen_at = CURRENT_TIMESTAMP WHERE id = ?`, id,
	)
	if err != nil {
		return err
	}
	return checkRowsAffected(res, "agent", id)
}

// --- Workflows ---

func (s *LibSQLStore) CreateWorkflow(ctx context.Context, wf *Workflow) error {
	def, err := json.Marshal(wf.Definition)
	if err != nil {
		return fmt.Errorf("marshal definition: %w", err)
	}
	inputParams, err := marshalMapOrDefault(wf.InputParams)
	if err != nil {
		return fmt.Errorf("marshal input_params: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO workflows (id, name, template_name, template_version, definition, status, agent_id, parent_workflow_id, input_params, output, error, created_at, started_at, completed_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		wf.ID, nullStr(wf.Name), nullStr(wf.TemplateName), nullStr(wf.TemplateVersion),
		string(def), string(wf.Status), wf.AgentID, nullStr(wf.ParentID),
		string(inputParams), nullRaw(wf.Output), nullRaw(wf.Error),
		timeOrNow(wf.CreatedAt), nullTime(wf.StartedAt), nullTime(wf.CompletedAt), timeOrNow(wf.UpdatedAt),
	)
	return err
}

func (s *LibSQLStore) GetWorkflow(ctx context.Context, id string) (*Workflow, error) {
	wf := &Workflow{}
	var (
		name, tmplName, tmplVer, parentID sql.NullString
		defJSON, inputJSON               string
		outputJSON, errorJSON            sql.NullString
		startedAt, completedAt           sql.NullTime
		status                           string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, template_name, template_version, definition, status, agent_id, parent_workflow_id, input_params, output, error, created_at, started_at, completed_at, updated_at
		 FROM workflows WHERE id = ?`, id,
	).Scan(&wf.ID, &name, &tmplName, &tmplVer, &defJSON, &status, &wf.AgentID, &parentID,
		&inputJSON, &outputJSON, &errorJSON, &wf.CreatedAt, &startedAt, &completedAt, &wf.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, storeNotFound("workflow", id)
	}
	if err != nil {
		return nil, err
	}
	wf.Name = name.String
	wf.TemplateName = tmplName.String
	wf.TemplateVersion = tmplVer.String
	wf.ParentID = parentID.String
	wf.Status = schema.WorkflowStatus(status)
	if err := json.Unmarshal([]byte(defJSON), &wf.Definition); err != nil {
		return nil, fmt.Errorf("unmarshal definition: %w", err)
	}
	if inputJSON != "" {
		_ = json.Unmarshal([]byte(inputJSON), &wf.InputParams)
	}
	wf.Output = rawOrNil(outputJSON)
	wf.Error = rawOrNil(errorJSON)
	if startedAt.Valid {
		wf.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		wf.CompletedAt = &completedAt.Time
	}
	return wf, nil
}

func (s *LibSQLStore) UpdateWorkflow(ctx context.Context, id string, update WorkflowUpdate) error {
	var sets []string
	var args []any

	if update.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, string(*update.Status))
	}
	if update.Output != nil {
		sets = append(sets, "output = ?")
		args = append(args, string(update.Output))
	}
	if update.Error != nil {
		sets = append(sets, "error = ?")
		args = append(args, string(update.Error))
	}
	if update.StartedAt != nil {
		sets = append(sets, "started_at = ?")
		args = append(args, *update.StartedAt)
	}
	if update.CompletedAt != nil {
		sets = append(sets, "completed_at = ?")
		args = append(args, *update.CompletedAt)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id)

	query := fmt.Sprintf("UPDATE workflows SET %s WHERE id = ?", strings.Join(sets, ", "))
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	return checkRowsAffected(res, "workflow", id)
}

func (s *LibSQLStore) ListWorkflows(ctx context.Context, filter WorkflowFilter) ([]*Workflow, error) {
	var where []string
	var args []any

	if filter.Status != nil {
		where = append(where, "status = ?")
		args = append(args, string(*filter.Status))
	}
	if filter.AgentID != "" {
		where = append(where, "agent_id = ?")
		args = append(args, filter.AgentID)
	}
	if filter.Since != nil {
		where = append(where, "created_at >= ?")
		args = append(args, *filter.Since)
	}

	query := "SELECT id, name, template_name, template_version, definition, status, agent_id, parent_workflow_id, input_params, output, error, created_at, started_at, completed_at, updated_at FROM workflows"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
		if filter.Offset > 0 {
			query += fmt.Sprintf(" OFFSET %d", filter.Offset)
		}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workflows []*Workflow
	for rows.Next() {
		wf := &Workflow{}
		var (
			name, tmplName, tmplVer, parentID sql.NullString
			defJSON, inputJSON               string
			outputJSON, errorJSON            sql.NullString
			startedAt, completedAt           sql.NullTime
			status                           string
		)
		if err := rows.Scan(&wf.ID, &name, &tmplName, &tmplVer, &defJSON, &status, &wf.AgentID, &parentID,
			&inputJSON, &outputJSON, &errorJSON, &wf.CreatedAt, &startedAt, &completedAt, &wf.UpdatedAt); err != nil {
			return nil, err
		}
		wf.Name = name.String
		wf.TemplateName = tmplName.String
		wf.TemplateVersion = tmplVer.String
		wf.ParentID = parentID.String
		wf.Status = schema.WorkflowStatus(status)
		if err := json.Unmarshal([]byte(defJSON), &wf.Definition); err != nil {
			return nil, fmt.Errorf("unmarshal definition: %w", err)
		}
		if inputJSON != "" {
			_ = json.Unmarshal([]byte(inputJSON), &wf.InputParams)
		}
		wf.Output = rawOrNil(outputJSON)
		wf.Error = rawOrNil(errorJSON)
		if startedAt.Valid {
			wf.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			wf.CompletedAt = &completedAt.Time
		}
		workflows = append(workflows, wf)
	}
	return workflows, rows.Err()
}

func (s *LibSQLStore) DeleteWorkflow(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM workflows WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return checkRowsAffected(res, "workflow", id)
}

// --- Events ---

func (s *LibSQLStore) AppendEvent(ctx context.Context, event *Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Get next sequence number for this workflow
	var seq int64
	err = tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(sequence), 0) + 1 FROM events WHERE workflow_id = ?`, event.WorkflowID,
	).Scan(&seq)
	if err != nil {
		return fmt.Errorf("get next sequence: %w", err)
	}
	event.Sequence = seq

	payload := nullRaw(event.Payload)
	ts := timeOrNow(event.Timestamp)

	_, err = tx.ExecContext(ctx,
		`INSERT INTO events (workflow_id, step_id, event_type, payload, agent_id, timestamp, sequence)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		event.WorkflowID, nullStr(event.StepID), event.Type, payload, nullStr(event.AgentID), ts, seq,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit event: %w", err)
	}
	return nil
}

func (s *LibSQLStore) GetEvents(ctx context.Context, workflowID string, since int64) ([]*Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, workflow_id, step_id, event_type, payload, agent_id, timestamp, sequence
		 FROM events WHERE workflow_id = ? AND sequence > ? ORDER BY sequence ASC`,
		workflowID, since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *LibSQLStore) GetEventsByType(ctx context.Context, eventType string, filter EventFilter) ([]*Event, error) {
	var where []string
	var args []any

	where = append(where, "event_type = ?")
	args = append(args, eventType)

	if filter.WorkflowID != "" {
		where = append(where, "workflow_id = ?")
		args = append(args, filter.WorkflowID)
	}
	if filter.StepID != "" {
		where = append(where, "step_id = ?")
		args = append(args, filter.StepID)
	}
	if filter.Since != nil {
		where = append(where, "timestamp >= ?")
		args = append(args, *filter.Since)
	}

	query := `SELECT id, workflow_id, step_id, event_type, payload, agent_id, timestamp, sequence FROM events`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY timestamp DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]*Event, error) {
	var events []*Event
	for rows.Next() {
		e := &Event{}
		var stepID, agentID sql.NullString
		var payload sql.NullString
		if err := rows.Scan(&e.ID, &e.WorkflowID, &stepID, &e.Type, &payload, &agentID, &e.Timestamp, &e.Sequence); err != nil {
			return nil, err
		}
		e.StepID = stepID.String
		e.AgentID = agentID.String
		e.Payload = rawOrNil(payload)
		events = append(events, e)
	}
	return events, rows.Err()
}

// --- Step State ---

func (s *LibSQLStore) UpsertStepState(ctx context.Context, state *StepState) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO step_state (workflow_id, step_id, status, input, output, error, retry_count, started_at, completed_at, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(workflow_id, step_id) DO UPDATE SET
		   status=excluded.status, input=excluded.input, output=excluded.output, error=excluded.error,
		   retry_count=excluded.retry_count, started_at=excluded.started_at, completed_at=excluded.completed_at,
		   duration_ms=excluded.duration_ms`,
		state.WorkflowID, state.StepID, string(state.Status),
		nullRaw(state.Input), nullRaw(state.Output), nullRaw(state.Error),
		state.RetryCount, nullTime(state.StartedAt), nullTime(state.CompletedAt), state.DurationMs,
	)
	return err
}

func (s *LibSQLStore) GetStepState(ctx context.Context, workflowID, stepID string) (*StepState, error) {
	ss := &StepState{}
	var status string
	var input, output, errJSON sql.NullString
	var startedAt, completedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT workflow_id, step_id, status, input, output, error, retry_count, started_at, completed_at, duration_ms
		 FROM step_state WHERE workflow_id = ? AND step_id = ?`, workflowID, stepID,
	).Scan(&ss.WorkflowID, &ss.StepID, &status, &input, &output, &errJSON,
		&ss.RetryCount, &startedAt, &completedAt, &ss.DurationMs)
	if err == sql.ErrNoRows {
		return nil, storeNotFound("step_state", workflowID+"/"+stepID)
	}
	if err != nil {
		return nil, err
	}
	ss.Status = schema.StepStatus(status)
	ss.Input = rawOrNil(input)
	ss.Output = rawOrNil(output)
	ss.Error = rawOrNil(errJSON)
	if startedAt.Valid {
		ss.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		ss.CompletedAt = &completedAt.Time
	}
	return ss, nil
}

func (s *LibSQLStore) ListStepStates(ctx context.Context, workflowID string) ([]*StepState, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT workflow_id, step_id, status, input, output, error, retry_count, started_at, completed_at, duration_ms
		 FROM step_state WHERE workflow_id = ?`, workflowID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var states []*StepState
	for rows.Next() {
		ss := &StepState{}
		var status string
		var input, output, errJSON sql.NullString
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(&ss.WorkflowID, &ss.StepID, &status, &input, &output, &errJSON,
			&ss.RetryCount, &startedAt, &completedAt, &ss.DurationMs); err != nil {
			return nil, err
		}
		ss.Status = schema.StepStatus(status)
		ss.Input = rawOrNil(input)
		ss.Output = rawOrNil(output)
		ss.Error = rawOrNil(errJSON)
		if startedAt.Valid {
			ss.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			ss.CompletedAt = &completedAt.Time
		}
		states = append(states, ss)
	}
	return states, rows.Err()
}

// --- Workflow Context ---

func (s *LibSQLStore) UpsertWorkflowContext(ctx context.Context, wfCtx *WorkflowContext) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workflow_context (workflow_id, agent_id, original_intent, decisions_log, accumulated_data, agent_notes, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(workflow_id) DO UPDATE SET
		   agent_id=excluded.agent_id, original_intent=excluded.original_intent,
		   decisions_log=excluded.decisions_log, accumulated_data=excluded.accumulated_data,
		   agent_notes=excluded.agent_notes, updated_at=CURRENT_TIMESTAMP`,
		wfCtx.WorkflowID, wfCtx.AgentID, wfCtx.OriginalIntent,
		nullRaw(wfCtx.DecisionsLog), nullRaw(wfCtx.AccumulatedData),
		nullStr(wfCtx.AgentNotes), timeOrNow(wfCtx.CreatedAt), timeOrNow(wfCtx.UpdatedAt),
	)
	return err
}

func (s *LibSQLStore) GetWorkflowContext(ctx context.Context, workflowID string) (*WorkflowContext, error) {
	wc := &WorkflowContext{}
	var decisionsLog, accData sql.NullString
	var notes sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT workflow_id, agent_id, original_intent, decisions_log, accumulated_data, agent_notes, created_at, updated_at
		 FROM workflow_context WHERE workflow_id = ?`, workflowID,
	).Scan(&wc.WorkflowID, &wc.AgentID, &wc.OriginalIntent, &decisionsLog, &accData, &notes, &wc.CreatedAt, &wc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, storeNotFound("workflow_context", workflowID)
	}
	if err != nil {
		return nil, err
	}
	wc.DecisionsLog = rawOrNil(decisionsLog)
	wc.AccumulatedData = rawOrNil(accData)
	wc.AgentNotes = notes.String
	return wc, nil
}

// --- Pending Decisions ---

func (s *LibSQLStore) CreateDecision(ctx context.Context, dec *PendingDecision) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pending_decisions (id, workflow_id, step_id, agent_id, target_agent_id, context, options, timeout_at, fallback, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dec.ID, dec.WorkflowID, dec.StepID, nullStr(dec.AgentID), nullStr(dec.TargetAgentID),
		string(dec.Context), string(dec.Options),
		nullTime(dec.TimeoutAt), nullStr(dec.Fallback), dec.Status, timeOrNow(dec.CreatedAt),
	)
	return err
}

func (s *LibSQLStore) ResolveDecision(ctx context.Context, id string, resolution *Resolution) error {
	resJSON, err := json.Marshal(resolution)
	if err != nil {
		return fmt.Errorf("marshal resolution: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE pending_decisions SET resolution = ?, resolved_by = ?, resolved_at = CURRENT_TIMESTAMP, status = 'resolved'
		 WHERE id = ? AND status = 'pending'`,
		string(resJSON), resolution.Choice, id,
	)
	if err != nil {
		return err
	}
	return checkRowsAffected(res, "pending_decision", id)
}

func (s *LibSQLStore) ListPendingDecisions(ctx context.Context, filter DecisionFilter) ([]*PendingDecision, error) {
	var where []string
	var args []any

	if filter.WorkflowID != "" {
		where = append(where, "workflow_id = ?")
		args = append(args, filter.WorkflowID)
	}
	if filter.AgentID != "" {
		where = append(where, "agent_id = ?")
		args = append(args, filter.AgentID)
	}
	if filter.TargetAgentID != "" {
		where = append(where, "target_agent_id = ?")
		args = append(args, filter.TargetAgentID)
	}
	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filter.Status)
	}

	query := `SELECT id, workflow_id, step_id, agent_id, target_agent_id, context, options, timeout_at, fallback, resolution, resolved_by, resolved_at, status, created_at FROM pending_decisions`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []*PendingDecision
	for rows.Next() {
		d := &PendingDecision{}
		var agentID, targetAgentID, fallback, resolvedBy sql.NullString
		var contextJSON, optionsJSON string
		var resolutionJSON sql.NullString
		var timeoutAt, resolvedAt sql.NullTime
		if err := rows.Scan(&d.ID, &d.WorkflowID, &d.StepID, &agentID, &targetAgentID,
			&contextJSON, &optionsJSON, &timeoutAt, &fallback,
			&resolutionJSON, &resolvedBy, &resolvedAt, &d.Status, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.AgentID = agentID.String
		d.TargetAgentID = targetAgentID.String
		d.Context = json.RawMessage(contextJSON)
		d.Options = json.RawMessage(optionsJSON)
		d.Fallback = fallback.String
		d.ResolvedBy = resolvedBy.String
		d.Resolution = rawOrNil(resolutionJSON)
		if timeoutAt.Valid {
			d.TimeoutAt = &timeoutAt.Time
		}
		if resolvedAt.Valid {
			d.ResolvedAt = &resolvedAt.Time
		}
		decisions = append(decisions, d)
	}
	return decisions, rows.Err()
}

// --- Secrets ---

func (s *LibSQLStore) StoreSecret(ctx context.Context, key string, value []byte) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO secrets (key, value, created_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, rotated_at=CURRENT_TIMESTAMP`,
		key, value,
	)
	return err
}

func (s *LibSQLStore) GetSecret(ctx context.Context, key string) ([]byte, error) {
	var value []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM secrets WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return nil, storeNotFound("secret", key)
	}
	return value, err
}

func (s *LibSQLStore) DeleteSecret(ctx context.Context, key string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM secrets WHERE key = ?`, key)
	if err != nil {
		return err
	}
	return checkRowsAffected(res, "secret", key)
}

// --- Templates ---

func (s *LibSQLStore) StoreTemplate(ctx context.Context, tpl *WorkflowTemplate) error {
	def, err := json.Marshal(tpl.Definition)
	if err != nil {
		return fmt.Errorf("marshal template definition: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO workflow_templates (name, version, description, definition, input_schema, output_schema, triggers, permissions, agent_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name, version) DO UPDATE SET
		   description=excluded.description, definition=excluded.definition,
		   input_schema=excluded.input_schema, output_schema=excluded.output_schema,
		   triggers=excluded.triggers, permissions=excluded.permissions,
		   updated_at=CURRENT_TIMESTAMP`,
		tpl.Name, tpl.Version, nullStr(tpl.Description), string(def),
		nullRaw(tpl.InputSchema), nullRaw(tpl.OutputSchema),
		nullRaw(tpl.Triggers), nullRaw(tpl.Permissions),
		tpl.AgentID, timeOrNow(tpl.CreatedAt), timeOrNow(tpl.UpdatedAt),
	)
	return err
}

func (s *LibSQLStore) GetTemplate(ctx context.Context, name string, version string) (*WorkflowTemplate, error) {
	t := &WorkflowTemplate{}
	var desc sql.NullString
	var defJSON string
	var inputSchema, outputSchema, triggers, permissions sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT name, version, description, definition, input_schema, output_schema, triggers, permissions, agent_id, created_at, updated_at
		 FROM workflow_templates WHERE name = ? AND version = ?`, name, version,
	).Scan(&t.Name, &t.Version, &desc, &defJSON, &inputSchema, &outputSchema, &triggers, &permissions, &t.AgentID, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, storeNotFound("template", name+":"+version)
	}
	if err != nil {
		return nil, err
	}
	t.Description = desc.String
	if err := json.Unmarshal([]byte(defJSON), &t.Definition); err != nil {
		return nil, fmt.Errorf("unmarshal template definition: %w", err)
	}
	t.InputSchema = rawOrNil(inputSchema)
	t.OutputSchema = rawOrNil(outputSchema)
	t.Triggers = rawOrNil(triggers)
	t.Permissions = rawOrNil(permissions)
	return t, nil
}

func (s *LibSQLStore) ListTemplates(ctx context.Context, filter TemplateFilter) ([]*WorkflowTemplate, error) {
	var where []string
	var args []any

	if filter.Name != "" {
		where = append(where, "name = ?")
		args = append(args, filter.Name)
	}
	if filter.AgentID != "" {
		where = append(where, "agent_id = ?")
		args = append(args, filter.AgentID)
	}

	query := `SELECT name, version, description, definition, input_schema, output_schema, triggers, permissions, agent_id, created_at, updated_at FROM workflow_templates`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY name, version DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []*WorkflowTemplate
	for rows.Next() {
		t := &WorkflowTemplate{}
		var desc sql.NullString
		var defJSON string
		var inputSchema, outputSchema, triggers, permissions sql.NullString
		if err := rows.Scan(&t.Name, &t.Version, &desc, &defJSON, &inputSchema, &outputSchema, &triggers, &permissions, &t.AgentID, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Description = desc.String
		if err := json.Unmarshal([]byte(defJSON), &t.Definition); err != nil {
			return nil, fmt.Errorf("unmarshal template definition: %w", err)
		}
		t.InputSchema = rawOrNil(inputSchema)
		t.OutputSchema = rawOrNil(outputSchema)
		t.Triggers = rawOrNil(triggers)
		t.Permissions = rawOrNil(permissions)
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

// --- Helpers ---

func storeNotFound(resource, id string) *schema.OpcodeError {
	return schema.NewErrorf(schema.ErrCodeNotFound, "%s %q not found", resource, id)
}

func checkRowsAffected(res sql.Result, resource, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return storeNotFound(resource, id)
	}
	return nil
}

func timeOrNow(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullRaw(r json.RawMessage) any {
	if len(r) == 0 {
		return nil
	}
	return string(r)
}

func rawOrNil(ns sql.NullString) json.RawMessage {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	return json.RawMessage(ns.String)
}

func jsonOrNil(ns sql.NullString) json.RawMessage {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	return json.RawMessage(ns.String)
}

func nullableJSON(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	return string(raw), nil
}

func marshalMapOrDefault(m map[string]any) (json.RawMessage, error) {
	if len(m) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(m)
}
