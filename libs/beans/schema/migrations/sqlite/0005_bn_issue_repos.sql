-- 0005_bn_issue_repos.sql
--
-- Links issues to onboarded repositories for SQLite.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE bn_issue_repos (
    issue_id         TEXT PRIMARY KEY REFERENCES bn_issues(id) ON DELETE CASCADE,
    repo_id          TEXT NOT NULL REFERENCES bn_repos(id),
    requested_ref    TEXT NOT NULL DEFAULT '',
    base_ref         TEXT NOT NULL DEFAULT '',
    work_branch      TEXT NOT NULL DEFAULT '',
    worktree_subdir  TEXT NOT NULL DEFAULT '',
    metadata         TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata)),
    created_at       TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (worktree_subdir = '' OR (
        worktree_subdir NOT LIKE '/%' AND
        worktree_subdir <> '..' AND
        worktree_subdir NOT LIKE '../%' AND
        worktree_subdir NOT LIKE '%/../%'
    ))
);

CREATE INDEX bn_issue_repos_repo_idx ON bn_issue_repos (repo_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE bn_issue_repos;

-- +goose StatementEnd
