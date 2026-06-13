-- 0001_bn_init.sql
--
-- Initial schema for the bn tracker store. The bn tracker is a distinct
-- concern with its own migration namespace, pool, and advisory lock.
--
-- Namespace: all tables use the "bn_" prefix to coexist safely in the same
-- Postgres database instance as other application schemas.
--
-- Concurrency model: Postgres handles concurrent bn CLI invocations and the
-- orchestrator adapter natively. close is idempotent; reads are consistent
-- per-statement. Cross-process dispatch dedup is out of scope.
--
-- Per project policy (see schema/schema.go), versioned
-- migrations do NOT use IF NOT EXISTS — the runner is responsible for
-- "should this run?" via goose_db_version + a session advisory lock.

-- +goose Up
-- +goose StatementBegin

-- ---------------------------------------------------------------------------
-- bn_projects
-- ---------------------------------------------------------------------------
-- One row per project prefix (bn init registers it). Issues reference their
-- project by prefix. A single Postgres instance can host multiple projects.

CREATE TABLE bn_projects (
    prefix     TEXT        PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- bn_issues
-- ---------------------------------------------------------------------------
-- One row per issue. id is "{prefix}-{shorthash}" (bd-compatible format).
-- prefix is denormalized here for fast prefix-scoped queries without a join.

CREATE TABLE bn_issues (
    id          TEXT        PRIMARY KEY,
    prefix      TEXT        NOT NULL REFERENCES bn_projects(prefix),
    identifier  TEXT,
    title       TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    priority    INT         NOT NULL DEFAULT 2 CHECK (priority BETWEEN 0 AND 4),
    issue_type  TEXT        NOT NULL DEFAULT 'task',
    state       TEXT        NOT NULL DEFAULT 'open',
    labels      JSONB       NOT NULL DEFAULT '[]',
    branch_name TEXT,
    url         TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Fast prefix+state scans (FetchCandidates, FetchByStates, ready queries).
CREATE INDEX bn_issues_prefix_state_idx ON bn_issues (prefix, state);

-- ---------------------------------------------------------------------------
-- bn_issue_deps
-- ---------------------------------------------------------------------------
-- Dependency edges: (issue_id) is blocked until (blocked_by_id) reaches a
-- terminal state. Both sides CASCADE on delete:
--   - If a blocker is deleted, the edge is removed (issue becomes unblocked).
--   - If the child is deleted, all its incoming dep edges are removed.
--
-- Cascade-on-delete semantics are intentional: a deleted issue should not
-- silently orphan edges that block other work. Operators who want to preserve
-- dep history should close rather than delete.
--
-- Self-reference is rejected by a CHECK constraint — a cycle of one is
-- always wrong, and the cycle-detection query handles longer cycles.

CREATE TABLE bn_issue_deps (
    issue_id      TEXT NOT NULL REFERENCES bn_issues(id) ON DELETE CASCADE,
    blocked_by_id TEXT NOT NULL REFERENCES bn_issues(id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, blocked_by_id),
    CHECK (issue_id != blocked_by_id)
);

-- Reverse lookup: "which issues are blocked by this one?" (dep tree queries).
CREATE INDEX bn_issue_deps_blocker_idx ON bn_issue_deps (blocked_by_id);

-- ---------------------------------------------------------------------------
-- bn_issue_notes
-- ---------------------------------------------------------------------------
-- Close --reason, --append-notes, and future comments. Immutable append log.

CREATE TABLE bn_issue_notes (
    id         BIGSERIAL   PRIMARY KEY,
    issue_id   TEXT        NOT NULL REFERENCES bn_issues(id) ON DELETE CASCADE,
    actor      TEXT,
    body       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- By-issue note timeline.
CREATE INDEX bn_issue_notes_issue_idx ON bn_issue_notes (issue_id, created_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE bn_issue_notes;
DROP TABLE bn_issue_deps;
DROP TABLE bn_issues;
DROP TABLE bn_projects;

-- +goose StatementEnd
