-- 0004_bn_repos.sql
--
-- Repository registry foundation for SQLite.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE bn_repos (
    id              TEXT PRIMARY KEY,
    prefix          TEXT NOT NULL REFERENCES bn_projects(prefix) ON DELETE CASCADE,
    slug            TEXT NOT NULL,
    display_name    TEXT NOT NULL,
    remote_url      TEXT NOT NULL,
    default_branch  TEXT NOT NULL DEFAULT 'main',
    worktree_subdir TEXT NOT NULL DEFAULT '',
    clone_strategy  TEXT NOT NULL DEFAULT 'mirror-cache',
    auth_ref        TEXT NOT NULL,
    enabled         INTEGER NOT NULL DEFAULT 1,
    metadata        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata)),
    created_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by      TEXT NOT NULL DEFAULT '',
    updated_by      TEXT NOT NULL DEFAULT '',
    UNIQUE(prefix, slug),
    CHECK (slug <> '' AND slug NOT GLOB '*[^a-z0-9._-]*' AND substr(slug, 1, 1) GLOB '[a-z0-9]'),
    CHECK (display_name <> ''),
    CHECK (remote_url <> ''),
    CHECK (default_branch <> ''),
    CHECK (clone_strategy <> ''),
    CHECK (auth_ref <> ''),
    CHECK (worktree_subdir = '' OR (
        worktree_subdir NOT LIKE '/%' AND
        worktree_subdir <> '..' AND
        worktree_subdir NOT LIKE '../%' AND
        worktree_subdir NOT LIKE '%/../%'
    ))
);

CREATE INDEX bn_repos_prefix_enabled_idx ON bn_repos (prefix, enabled, slug);

CREATE TABLE bn_repo_aliases (
    prefix     TEXT NOT NULL REFERENCES bn_projects(prefix) ON DELETE CASCADE,
    alias      TEXT NOT NULL,
    repo_id    TEXT NOT NULL REFERENCES bn_repos(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(prefix, alias),
    CHECK (alias <> '')
);

CREATE INDEX bn_repo_aliases_repo_idx ON bn_repo_aliases (repo_id);

CREATE TABLE bn_project_admins (
    prefix     TEXT NOT NULL REFERENCES bn_projects(prefix) ON DELETE CASCADE,
    actor      TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(prefix, actor),
    CHECK (actor <> '')
);

CREATE TABLE bn_repo_audit (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    prefix     TEXT NOT NULL REFERENCES bn_projects(prefix) ON DELETE CASCADE,
    repo_id    TEXT REFERENCES bn_repos(id) ON DELETE SET NULL,
    action     TEXT NOT NULL,
    actor      TEXT NOT NULL,
    old_values TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(old_values)),
    new_values TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(new_values)),
    command    TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (action <> ''),
    CHECK (actor <> '')
);

CREATE INDEX bn_repo_audit_prefix_created_idx ON bn_repo_audit (prefix, created_at DESC);
CREATE INDEX bn_repo_audit_repo_created_idx ON bn_repo_audit (repo_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE bn_repo_audit;
DROP TABLE bn_project_admins;
DROP TABLE bn_repo_aliases;
DROP TABLE bn_repos;

-- +goose StatementEnd
