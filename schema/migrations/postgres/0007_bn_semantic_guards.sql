-- 0007_bn_semantic_guards.sql
--
-- Portable guard tables for dependency graph writes and repo-admin bootstrap.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE bn_dep_graph_guard (
    id         SMALLINT    PRIMARY KEY,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO bn_dep_graph_guard (id)
VALUES (1)
ON CONFLICT DO NOTHING;

CREATE TABLE bn_project_admin_bootstraps (
    prefix     TEXT        PRIMARY KEY REFERENCES bn_projects(prefix) ON DELETE CASCADE,
    actor      TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (actor <> '')
);

INSERT INTO bn_project_admin_bootstraps (prefix, actor, created_at)
SELECT DISTINCT ON (prefix) prefix, actor, created_at
FROM bn_project_admins
ORDER BY prefix, created_at, actor
ON CONFLICT DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE bn_project_admin_bootstraps;
DROP TABLE bn_dep_graph_guard;

-- +goose StatementEnd
