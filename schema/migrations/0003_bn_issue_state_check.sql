-- 0003_bn_issue_state_check.sql
--
-- Enforce the issue-state vocabulary used by the local bn tracker. The bn CLI
-- writes open/in_progress/blocked/closed; imports and Symphony defaults also
-- recognize done as a terminal state.

-- +goose Up
-- +goose StatementBegin

ALTER TABLE bn_issues
    ADD CONSTRAINT bn_issues_state_check
    CHECK (state IN ('open', 'in_progress', 'blocked', 'closed', 'done'))
    NOT VALID;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE bn_issues
    DROP CONSTRAINT bn_issues_state_check;

-- +goose StatementEnd
