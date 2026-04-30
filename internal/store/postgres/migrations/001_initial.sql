-- 001_initial.sql — PostgreSQL schema for the Nous coordination layer.
--
-- Conventions:
--   * uuid columns are first-class.
--   * jsonb everywhere a JSON payload is stored — gives us indexable
--     filtering for free if we ever need it.
--   * timestamptz; the application normalises to UTC on read/write.

CREATE TABLE IF NOT EXISTS goals (
    id           uuid        PRIMARY KEY,
    owner_id     text        NOT NULL,
    title        text        NOT NULL,
    description  text        NOT NULL,
    priority     integer     NOT NULL DEFAULT 0,
    status       text        NOT NULL,
    source_refs  jsonb       NOT NULL DEFAULT '[]'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_goals_owner_status
    ON goals(owner_id, status);

CREATE TABLE IF NOT EXISTS commitments (
    id                uuid        PRIMARY KEY,
    owner_id          text        NOT NULL,
    description       text        NOT NULL,
    source_refs       jsonb       NOT NULL DEFAULT '[]'::jsonb,
    entities          jsonb       NOT NULL DEFAULT '[]'::jsonb,
    due_at            timestamptz,
    status            text        NOT NULL,
    confidence        double precision NOT NULL,
    risk_score        double precision NOT NULL DEFAULT 0,
    last_evaluated_at timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_commitments_status ON commitments(status);
CREATE INDEX IF NOT EXISTS idx_commitments_owner  ON commitments(owner_id);
CREATE INDEX IF NOT EXISTS idx_commitments_active_scan
    ON commitments(status, risk_score DESC, due_at NULLS LAST, created_at);

CREATE TABLE IF NOT EXISTS tasks (
    id            uuid        PRIMARY KEY,
    goal_id       uuid        REFERENCES goals(id),
    plan_id       uuid,
    commitment_id uuid        REFERENCES commitments(id),
    title         text        NOT NULL,
    description   text        NOT NULL,
    assigned_to   text,
    status        text        NOT NULL,
    priority      integer     NOT NULL DEFAULT 0,
    due_at        timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_tasks_goal     ON tasks(goal_id);
CREATE INDEX IF NOT EXISTS idx_tasks_assigned ON tasks(assigned_to);
CREATE INDEX IF NOT EXISTS idx_tasks_status   ON tasks(status);

CREATE TABLE IF NOT EXISTS decisions (
    id           uuid        PRIMARY KEY,
    subject      text        NOT NULL,
    context_refs jsonb       NOT NULL DEFAULT '[]'::jsonb,
    inputs       jsonb       NOT NULL DEFAULT '{}'::jsonb,
    outcome      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    reason       text        NOT NULL,
    confidence   double precision NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_decisions_subject_created
    ON decisions(subject, created_at DESC);

CREATE TABLE IF NOT EXISTS interventions (
    id               uuid        PRIMARY KEY,
    commitment_id    uuid        REFERENCES commitments(id),
    task_id          uuid        REFERENCES tasks(id),
    type             text        NOT NULL,
    message          text        NOT NULL,
    suggested_action jsonb,
    status           text        NOT NULL,
    triggered_at     timestamptz NOT NULL DEFAULT now(),
    resolved_at      timestamptz
);

CREATE INDEX IF NOT EXISTS idx_interventions_commitment ON interventions(commitment_id);
CREATE INDEX IF NOT EXISTS idx_interventions_task       ON interventions(task_id);
CREATE INDEX IF NOT EXISTS idx_interventions_status_triggered
    ON interventions(status, triggered_at DESC);

CREATE TABLE IF NOT EXISTS assignments (
    id            uuid        PRIMARY KEY,
    task_id       uuid        NOT NULL REFERENCES tasks(id),
    agent_id      text        NOT NULL,
    status        text        NOT NULL,
    assigned_at   timestamptz NOT NULL DEFAULT now(),
    completed_at  timestamptz
);

CREATE INDEX IF NOT EXISTS idx_assignments_task ON assignments(task_id);
