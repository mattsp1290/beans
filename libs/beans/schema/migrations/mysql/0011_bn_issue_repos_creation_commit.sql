-- 0011_bn_issue_repos_creation_commit.sql
--
-- Record the git commit present when an issue was linked to a repository.
-- MySQL 8.x requires expression syntax for TEXT defaults.

-- +goose Up
-- +goose StatementBegin

ALTER TABLE bn_issue_repos ADD COLUMN creation_commit TEXT NOT NULL DEFAULT ('');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE bn_issue_repos DROP COLUMN creation_commit;

-- +goose StatementEnd
