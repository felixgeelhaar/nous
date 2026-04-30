-- 001_initial.sql — SQLite schema for the Nous coordination layer.
--
-- Conventions:
--   * UUIDs are stored as TEXT (canonical hyphenated form).
--   * JSON fields (source_refs, entities, inputs, outcome,
--     suggested_action) are TEXT carrying JSON payloads. The Go
--     layer encodes/decodes; SQLite has no jsonb to hand.
--   * Timestamps are TIMESTAMP and round-tripped via Go's
--     time.Time. The driver stores RFC3339 strings under the hood.

CREATE TABLE IF NOT EXISTS goals (
    id            TEXT      PRIMARY KEY,
    owner_id      TEXT      NOT NULL,
    title         TEXT      NOT NULL,
    description   TEXT      NOT NULL,
    priority      INTEGER   NOT NULL DEFAULT 0,
    status        TEXT      NOT NULL,
    source_refs   TEXT      NOT NULL DEFAULT '[]',
    created_at    TIMESTAMP NOT NULL,
    updated_at    TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_goals_owner_status
    ON goals(owner_id, status);

CREATE TABLE IF NOT EXISTS commitments (
    id                TEXT      PRIMARY KEY,
    owner_id          TEXT      NOT NULL,
    description       TEXT      NOT NULL,
    source_refs       TEXT      NOT NULL DEFAULT '[]',
    entities          TEXT      NOT NULL DEFAULT '[]',
    due_at            TIMESTAMP,
    status            TEXT      NOT NULL,
    confidence        REAL      NOT NULL,
    risk_score        REAL      NOT NULL DEFAULT 0,
    last_evaluated_at TIMESTAMP,
    created_at        TIMESTAMP NOT NULL,
    updated_at        TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_commitments_status
    ON commitments(status);
CREATE INDEX IF NOT EXISTS idx_commitments_owner
    ON commitments(owner_id);
-- Worker scan order: risk desc, due asc, created asc.
CREATE INDEX IF NOT EXISTS idx_commitments_active_scan
    ON commitments(status, risk_score DESC, due_at, created_at);

CREATE TABLE IF NOT EXISTS tasks (
    id            TEXT      PRIMARY KEY,
    goal_id       TEXT      REFERENCES goals(id),
    plan_id       TEXT,
    commitment_id TEXT      REFERENCES commitments(id),
    title         TEXT      NOT NULL,
    description   TEXT      NOT NULL,
    assigned_to   TEXT,
    status        TEXT      NOT NULL,
    priority      INTEGER   NOT NULL DEFAULT 0,
    due_at        TIMESTAMP,
    created_at    TIMESTAMP NOT NULL,
    updated_at    TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_goal     ON tasks(goal_id);
CREATE INDEX IF NOT EXISTS idx_tasks_assigned ON tasks(assigned_to);
CREATE INDEX IF NOT EXISTS idx_tasks_status   ON tasks(status);

CREATE TABLE IF NOT EXISTS decisions (
    id           TEXT      PRIMARY KEY,
    subject      TEXT      NOT NULL,
    context_refs TEXT      NOT NULL DEFAULT '[]',
    inputs       TEXT      NOT NULL DEFAULT '{}',
    outcome      TEXT      NOT NULL DEFAULT '{}',
    reason       TEXT      NOT NULL,
    confidence   REAL      NOT NULL,
    created_at   TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_decisions_subject_created
    ON decisions(subject, created_at DESC);

CREATE TABLE IF NOT EXISTS interventions (
    id               TEXT      PRIMARY KEY,
    commitment_id    TEXT      REFERENCES commitments(id),
    task_id          TEXT      REFERENCES tasks(id),
    type             TEXT      NOT NULL,
    message          TEXT      NOT NULL,
    suggested_action TEXT,
    status           TEXT      NOT NULL,
    triggered_at     TIMESTAMP NOT NULL,
    resolved_at      TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_interventions_commitment
    ON interventions(commitment_id);
CREATE INDEX IF NOT EXISTS idx_interventions_task
    ON interventions(task_id);
CREATE INDEX IF NOT EXISTS idx_interventions_status_triggered
    ON interventions(status, triggered_at DESC);

CREATE TABLE IF NOT EXISTS assignments (
    id            TEXT      PRIMARY KEY,
    task_id       TEXT      NOT NULL REFERENCES tasks(id),
    agent_id      TEXT      NOT NULL,
    status        TEXT      NOT NULL,
    assigned_at   TIMESTAMP NOT NULL,
    completed_at  TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_assignments_task ON assignments(task_id);
