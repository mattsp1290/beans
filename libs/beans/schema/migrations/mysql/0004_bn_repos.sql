-- 0004_bn_repos.sql
--
-- Repository registry foundation for MySQL.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE bn_repos (
    id              VARCHAR(255) PRIMARY KEY,
    prefix          VARCHAR(255) NOT NULL,
    slug            VARCHAR(255) NOT NULL,
    display_name    TEXT NOT NULL,
    remote_url      TEXT NOT NULL,
    default_branch  VARCHAR(255) NOT NULL DEFAULT 'main',
    worktree_subdir TEXT NOT NULL,
    clone_strategy  VARCHAR(64) NOT NULL DEFAULT 'mirror-cache',
    auth_ref        TEXT NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    metadata        JSON NOT NULL,
    created_at      TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at      TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    created_by      TEXT NOT NULL,
    updated_by      TEXT NOT NULL,
    UNIQUE(prefix, slug),
    CHECK (REGEXP_LIKE(slug, '^[a-z0-9][a-z0-9._-]*$')),
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
    )),
    CONSTRAINT bn_repos_prefix_fk
        FOREIGN KEY (prefix) REFERENCES bn_projects(prefix) ON DELETE CASCADE
);

CREATE INDEX bn_repos_prefix_enabled_idx ON bn_repos (prefix, enabled, slug);

CREATE TABLE bn_repo_aliases (
    prefix     VARCHAR(255) NOT NULL,
    alias      VARCHAR(255) NOT NULL,
    repo_id    VARCHAR(255) NOT NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY(prefix, alias),
    CHECK (alias <> ''),
    CONSTRAINT bn_repo_aliases_prefix_fk
        FOREIGN KEY (prefix) REFERENCES bn_projects(prefix) ON DELETE CASCADE,
    CONSTRAINT bn_repo_aliases_repo_fk
        FOREIGN KEY (repo_id) REFERENCES bn_repos(id) ON DELETE CASCADE
);

CREATE INDEX bn_repo_aliases_repo_idx ON bn_repo_aliases (repo_id);

CREATE TABLE bn_project_admins (
    prefix     VARCHAR(255) NOT NULL,
    actor      VARCHAR(255) NOT NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY(prefix, actor),
    CHECK (actor <> ''),
    CONSTRAINT bn_project_admins_prefix_fk
        FOREIGN KEY (prefix) REFERENCES bn_projects(prefix) ON DELETE CASCADE
);

CREATE TABLE bn_repo_audit (
    id         BIGINT PRIMARY KEY AUTO_INCREMENT,
    prefix     VARCHAR(255) NOT NULL,
    repo_id    VARCHAR(255),
    action     VARCHAR(255) NOT NULL,
    actor      VARCHAR(255) NOT NULL,
    old_values JSON NOT NULL,
    new_values JSON NOT NULL,
    command    TEXT NOT NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CHECK (action <> ''),
    CHECK (actor <> ''),
    CONSTRAINT bn_repo_audit_prefix_fk
        FOREIGN KEY (prefix) REFERENCES bn_projects(prefix) ON DELETE CASCADE,
    CONSTRAINT bn_repo_audit_repo_fk
        FOREIGN KEY (repo_id) REFERENCES bn_repos(id) ON DELETE SET NULL
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
