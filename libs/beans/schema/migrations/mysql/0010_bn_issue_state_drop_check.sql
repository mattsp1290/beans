-- 0010_bn_issue_state_drop_check.sql
--
-- Move the issue-status vocabulary out of the database and into the application
-- layer (model.WorkflowConfig). The DB CHECK constraint made the status set
-- un-configurable. The bn store now validates statuses against the loaded
-- workflow config (write-strict) and tolerates unknown statuses on read.

-- +goose Up
-- +goose StatementBegin

ALTER TABLE bn_issues DROP CHECK bn_issues_state_check;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- HAZARD: unlike Postgres (which re-adds the constraint without re-validating
-- existing rows), MySQL 8.0.16+ validates all existing rows when a CHECK
-- constraint is added. If any issue has reached a ready_for_* (or other
-- out-of-vocabulary) state, this rollback fails with a constraint violation.
-- Scrub or convert such rows to the legacy vocabulary before rolling back.
ALTER TABLE bn_issues
    ADD CONSTRAINT bn_issues_state_check
    CHECK (state IN ('open', 'in_progress', 'blocked', 'closed', 'done'));

-- +goose StatementEnd
