-- 0008_bn_dep_type.sql
--
-- Add a dependency edge kind to bn_issue_deps so membership (parent-child) can
-- be recorded distinctly from blocking (blocks). Only 'blocks' edges gate
-- readiness and cycle detection; every other type is non-blocking metadata.
--
-- The primary key stays (issue_id, blocked_by_id) — dep_type is NOT part of it,
-- so at most one edge of any kind exists per ordered pair (first write wins).
-- This is intentional for the two-level epic→leaf model; dual blocks+membership
-- edges between the same pair are out of scope.

-- +goose Up
-- +goose StatementBegin

ALTER TABLE bn_issue_deps ADD COLUMN dep_type TEXT NOT NULL DEFAULT 'blocks';

CREATE INDEX bn_issue_deps_type_idx ON bn_issue_deps (dep_type);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX bn_issue_deps_type_idx;
ALTER TABLE bn_issue_deps DROP COLUMN dep_type;

-- +goose StatementEnd
