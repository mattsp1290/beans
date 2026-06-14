-- 0008_bn_dep_type.sql
--
-- Add a dependency edge kind to bn_issue_deps so membership (parent-child) can
-- be recorded distinctly from blocking (blocks). Only 'blocks' edges gate
-- readiness and cycle detection; every other type is non-blocking metadata.
--
-- MySQL note: TEXT/BLOB columns cannot carry a literal DEFAULT before 8.0.13,
-- so dep_type is VARCHAR(64) (matches the bd 50-char type bound with headroom).

-- +goose Up
-- +goose StatementBegin

ALTER TABLE bn_issue_deps ADD COLUMN dep_type VARCHAR(64) NOT NULL DEFAULT 'blocks';

CREATE INDEX bn_issue_deps_type_idx ON bn_issue_deps (dep_type);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX bn_issue_deps_type_idx ON bn_issue_deps;
ALTER TABLE bn_issue_deps DROP COLUMN dep_type;

-- +goose StatementEnd
