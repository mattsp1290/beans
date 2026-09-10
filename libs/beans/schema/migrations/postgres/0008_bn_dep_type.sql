-- 0008_bn_dep_type.sql
--
-- Add a dependency edge kind to bn_issue_deps so membership (parent-child) can
-- be recorded distinctly from blocking (blocks). Only 'blocks' edges gate
-- readiness and cycle detection; every other type is non-blocking metadata.
--
-- The primary key stays (issue_id, blocked_by_id) — dep_type is NOT part of it,
-- so at most one edge of any kind exists per ordered pair (first write wins).
-- This is intentional for the two-level epic->leaf model. ESCAPE HATCH: if a
-- pair ever needs BOTH a blocking and a membership edge (or multi-level epics),
-- the PK must become (issue_id, blocked_by_id, dep_type) via a dedupe migration.
--
-- Per project policy (see schema/schema.go), versioned migrations do NOT use
-- IF NOT EXISTS — the runner gates "should this run?" via bn_schema_versions.

-- +goose Up
-- +goose StatementBegin

ALTER TABLE bn_issue_deps ADD COLUMN dep_type TEXT NOT NULL DEFAULT 'blocks';

-- Composite index supports the ListMembers/ListParents lookups that filter on
-- (blocked_by_id, dep_type); a lone dep_type index on a ~2-value column is not
-- selective enough to be useful.
CREATE INDEX bn_issue_deps_parent_idx ON bn_issue_deps (blocked_by_id, dep_type);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX bn_issue_deps_parent_idx;
ALTER TABLE bn_issue_deps DROP COLUMN dep_type;

-- +goose StatementEnd
