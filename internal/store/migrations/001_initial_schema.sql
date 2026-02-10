-- 001_initial_schema.sql
-- Initial database schema for OPCODE workflow engine.
-- PRAGMAs are applied at connection time in NewLibSQLStore, not here.

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT DEFAULT 'llm' CHECK(type IN ('llm', 'system', 'human', 'service')),
    metadata JSON DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_seen_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS workflows (
    id TEXT PRIMARY KEY,
    name TEXT,
    template_name TEXT,
    template_version TEXT,
    definition JSON NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK(status IN ('pending','active','suspended','completed','failed','cancelled')),
    agent_id TEXT NOT NULL,
    parent_workflow_id TEXT,
    input_params JSON DEFAULT '{}',
    output JSON,
    error JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (agent_id) REFERENCES agents(id),
    FOREIGN KEY (parent_workflow_id) REFERENCES workflows(id)
);

CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workflow_id TEXT NOT NULL,
    step_id TEXT,
    event_type TEXT NOT NULL,
    payload JSON,
    agent_id TEXT,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    sequence INTEGER NOT NULL,
    FOREIGN KEY (workflow_id) REFERENCES workflows(id),
    UNIQUE(workflow_id, sequence)
);

CREATE TABLE IF NOT EXISTS step_state (
    workflow_id TEXT,
    step_id TEXT,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK(status IN ('pending','scheduled','running','completed','failed','skipped','suspended','retrying')),
    input JSON,
    output JSON,
    error JSON,
    retry_count INTEGER DEFAULT 0,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    duration_ms INTEGER,
    PRIMARY KEY (workflow_id, step_id),
    FOREIGN KEY (workflow_id) REFERENCES workflows(id)
);

CREATE TABLE IF NOT EXISTS workflow_context (
    workflow_id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    original_intent TEXT NOT NULL,
    decisions_log JSON DEFAULT '[]',
    accumulated_data JSON DEFAULT '{}',
    agent_notes TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (workflow_id) REFERENCES workflows(id)
);

CREATE TABLE IF NOT EXISTS pending_decisions (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    step_id TEXT NOT NULL,
    agent_id TEXT,
    target_agent_id TEXT,
    context JSON NOT NULL,
    options JSON NOT NULL,
    timeout_at TIMESTAMP,
    fallback TEXT,
    resolution JSON,
    resolved_by TEXT,
    resolved_at TIMESTAMP,
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending', 'resolved', 'timed_out', 'cancelled')),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (workflow_id) REFERENCES workflows(id)
);

CREATE TABLE IF NOT EXISTS workflow_templates (
    name TEXT,
    version TEXT,
    description TEXT,
    definition JSON NOT NULL,
    input_schema JSON,
    output_schema JSON,
    triggers JSON DEFAULT '[]',
    permissions JSON DEFAULT '{}',
    agent_id TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (name, version)
);

CREATE TABLE IF NOT EXISTS plugins (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('mcp')),
    config JSON NOT NULL,
    status TEXT DEFAULT 'inactive' CHECK(status IN ('active', 'inactive', 'error')),
    last_health_check TIMESTAMP,
    error_message TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS secrets (
    key TEXT PRIMARY KEY,
    value BLOB NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS scheduled_jobs (
    id TEXT PRIMARY KEY,
    template_name TEXT NOT NULL,
    template_version TEXT,
    cron_expression TEXT NOT NULL,
    params JSON DEFAULT '{}',
    agent_id TEXT NOT NULL,
    enabled BOOLEAN DEFAULT 1,
    last_run_at TIMESTAMP,
    next_run_at TIMESTAMP,
    last_run_status TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id TEXT NOT NULL,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT,
    details JSON,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_workflows_status ON workflows(status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_workflows_agent ON workflows(agent_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_workflows_parent ON workflows(parent_workflow_id);
CREATE INDEX IF NOT EXISTS idx_events_workflow ON events(workflow_id, sequence);
CREATE INDEX IF NOT EXISTS idx_events_type ON events(event_type, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_step_state_status ON step_state(status);
CREATE INDEX IF NOT EXISTS idx_decisions_pending ON pending_decisions(status, target_agent_id);
CREATE INDEX IF NOT EXISTS idx_decisions_workflow ON pending_decisions(workflow_id);
CREATE INDEX IF NOT EXISTS idx_scheduled_next ON scheduled_jobs(next_run_at) WHERE enabled = 1;
CREATE INDEX IF NOT EXISTS idx_audit_agent ON audit_log(agent_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_context_agent ON workflow_context(agent_id, updated_at DESC);
