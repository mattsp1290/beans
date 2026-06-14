-- 0003_bn_issue_state_check.sql
--
-- Enforce the issue-state vocabulary for MySQL.

-- +goose Up
-- +goose StatementBegin

ALTER TABLE bn_issues
    ADD CONSTRAINT bn_issues_state_check
    CHECK (state IN ('open', 'in_progress', 'blocked', 'closed', 'done'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE bn_issues
    DROP CONSTRAINT bn_issues_state_check;

-- +goose StatementEnd
