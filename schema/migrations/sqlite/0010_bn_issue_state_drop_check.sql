-- 0010_bn_issue_state_drop_check.sql
--
-- Move the issue-status vocabulary out of the database and into the application
-- layer (model.WorkflowConfig). SQLite bakes the CHECK constraint into the
-- CREATE TABLE statement and cannot ALTER ... DROP CONSTRAINT, so the column
-- list below must mirror bn_issues as it exists at this point in the migration
-- chain (unchanged since 0001; later migrations only add referencing tables).
--
-- The rebuild runs with foreign_keys disabled because DROP TABLE on a parent
-- with ON DELETE CASCADE children (bn_issue_deps, bn_issue_notes,
-- bn_issue_repos) would otherwise implicitly delete their rows. store.New pins
-- SQLite to a single connection during migration so this PRAGMA and the DDL
-- below execute on the same connection; PRAGMA foreign_keys is also a no-op
-- inside a transaction, hence NO TRANSACTION.

-- +goose NO TRANSACTION

-- +goose Up
-- +goose StatementBegin
PRAGMA foreign_keys=off;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE issues_rebuild_0010 (
    id          TEXT PRIMARY KEY,
    prefix      TEXT NOT NULL REFERENCES bn_projects(prefix),
    identifier  TEXT,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    priority    INTEGER NOT NULL DEFAULT 2 CHECK (priority BETWEEN 0 AND 4),
    issue_type  TEXT NOT NULL DEFAULT 'task',
    state       TEXT NOT NULL DEFAULT 'open',
    labels      TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(labels)),
    branch_name TEXT,
    url         TEXT,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO issues_rebuild_0010 (id, prefix, identifier, title, description, priority,
                           issue_type, state, labels, branch_name, url,
                           created_at, updated_at)
    SELECT id, prefix, identifier, title, description, priority, issue_type,
           state, labels, branch_name, url, created_at, updated_at
    FROM bn_issues;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE bn_issues;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE issues_rebuild_0010 RENAME TO bn_issues;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX bn_issues_prefix_state_idx ON bn_issues (prefix, state);
-- +goose StatementEnd

-- +goose StatementBegin
PRAGMA foreign_keys=on;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
PRAGMA foreign_keys=off;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE issues_rebuild_0010 (
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
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO issues_rebuild_0010 (id, prefix, identifier, title, description, priority,
                           issue_type, state, labels, branch_name, url,
                           created_at, updated_at)
    SELECT id, prefix, identifier, title, description, priority, issue_type,
           state, labels, branch_name, url, created_at, updated_at
    FROM bn_issues;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE bn_issues;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE issues_rebuild_0010 RENAME TO bn_issues;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX bn_issues_prefix_state_idx ON bn_issues (prefix, state);
-- +goose StatementEnd

-- +goose StatementBegin
PRAGMA foreign_keys=on;
-- +goose StatementEnd
