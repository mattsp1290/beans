-- 0005_bn_issue_repos.sql
--
-- Links issues to onboarded repositories for MySQL.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE bn_issue_repos (
    issue_id         VARCHAR(255) PRIMARY KEY,
    repo_id          VARCHAR(255) NOT NULL,
    requested_ref    TEXT NOT NULL,
    base_ref         TEXT NOT NULL,
    work_branch      TEXT NOT NULL,
    worktree_subdir  TEXT NOT NULL,
    metadata         JSON NOT NULL,
    created_at       TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at       TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CHECK (worktree_subdir = '' OR (
        worktree_subdir NOT LIKE '/%' AND
        worktree_subdir <> '..' AND
        worktree_subdir NOT LIKE '../%' AND
        worktree_subdir NOT LIKE '%/../%'
    )),
    CONSTRAINT bn_issue_repos_issue_fk
        FOREIGN KEY (issue_id) REFERENCES bn_issues(id) ON DELETE CASCADE,
    CONSTRAINT bn_issue_repos_repo_fk
        FOREIGN KEY (repo_id) REFERENCES bn_repos(id)
);

CREATE INDEX bn_issue_repos_repo_idx ON bn_issue_repos (repo_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE bn_issue_repos;

-- +goose StatementEnd
