-- 0001_bn_init.sql
--
-- Initial SQLite schema for the bn tracker store.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE bn_projects (
    prefix     TEXT PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE bn_issues (
    id          TEXT PRIMARY KEY,
    prefix      TEXT NOT NULL REFERENCES bn_projects(prefix),
    identifier  TEXT,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    priority    INTEGER NOT NULL DEFAULT 2 CHECK (priority BETWEEN 0 AND 4),
    issue_type  TEXT NOT NULL DEFAULT 'task',
    state       TEXT NOT NULL DEFAULT 'open'
                    CHECK (state IN ('open', 'in_progress', 'blocked', 'closed', 'done')),
    labels      TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(labels)),
    branch_name TEXT,
    url         TEXT,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX bn_issues_prefix_state_idx ON bn_issues (prefix, state);

CREATE TABLE bn_issue_deps (
    issue_id      TEXT NOT NULL REFERENCES bn_issues(id) ON DELETE CASCADE,
    blocked_by_id TEXT NOT NULL REFERENCES bn_issues(id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, blocked_by_id),
    CHECK (issue_id <> blocked_by_id)
);

CREATE INDEX bn_issue_deps_blocker_idx ON bn_issue_deps (blocked_by_id);

CREATE TABLE bn_issue_notes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id   TEXT NOT NULL REFERENCES bn_issues(id) ON DELETE CASCADE,
    actor      TEXT,
    body       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX bn_issue_notes_issue_idx ON bn_issue_notes (issue_id, created_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE bn_issue_notes;
DROP TABLE bn_issue_deps;
DROP TABLE bn_issues;
DROP TABLE bn_projects;

-- +goose StatementEnd
