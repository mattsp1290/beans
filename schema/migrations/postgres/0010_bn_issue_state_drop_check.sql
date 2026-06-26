-- 0010_bn_issue_state_drop_check.sql
--
-- Move the issue-status vocabulary out of the database and into the application
-- layer (model.WorkflowConfig). The DB CHECK constraint made the status set
-- un-configurable: a config-defined status the DB rejects is useless. The bn
-- store now validates statuses against the loaded workflow config (write-strict)
-- and tolerates unknown statuses on read.

-- +goose Up
-- +goose StatementBegin

ALTER TABLE bn_issues DROP CONSTRAINT IF EXISTS bn_issues_state_check;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Restore the legacy vocabulary guard. NOT VALID so existing rows (which may
-- carry ready_for_* states) are not re-checked; only new writes are constrained.
ALTER TABLE bn_issues
    ADD CONSTRAINT bn_issues_state_check
    CHECK (state IN ('open', 'in_progress', 'blocked', 'closed', 'done'))
    NOT VALID;

-- +goose StatementEnd
