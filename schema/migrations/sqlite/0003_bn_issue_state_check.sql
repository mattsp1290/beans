-- 0003_bn_issue_state_check.sql
--
-- SQLite cannot add a named CHECK constraint with ALTER TABLE. The state
-- vocabulary is enforced by the bn_issues definition in 0001.

-- +goose Up
-- +goose StatementBegin

SELECT 1;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

SELECT 1;

-- +goose StatementEnd
