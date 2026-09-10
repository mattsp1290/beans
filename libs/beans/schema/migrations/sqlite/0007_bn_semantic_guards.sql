-- 0007_bn_semantic_guards.sql
--
-- Portable guard tables for dependency graph writes and repo-admin bootstrap.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE bn_dep_graph_guard (
    id         INTEGER PRIMARY KEY,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO bn_dep_graph_guard (id) VALUES (1);

CREATE TABLE bn_project_admin_bootstraps (
    prefix     TEXT PRIMARY KEY REFERENCES bn_projects(prefix) ON DELETE CASCADE,
    actor      TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (actor <> '')
);

INSERT OR IGNORE INTO bn_project_admin_bootstraps (prefix, actor, created_at)
SELECT a.prefix, a.actor, a.created_at
FROM bn_project_admins a
WHERE NOT EXISTS (
    SELECT 1
    FROM bn_project_admins b
    WHERE b.prefix = a.prefix
      AND (b.created_at < a.created_at OR (b.created_at = a.created_at AND b.actor < a.actor))
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE bn_project_admin_bootstraps;
DROP TABLE bn_dep_graph_guard;

-- +goose StatementEnd
