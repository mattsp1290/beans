-- 0009_bn_remote_url_unique.sql
--
-- Add a UNIQUE index on bn_repos.remote_url so that the NormalizeRemoteURL
-- canonical form (https://host/path, all SCP/ssh/https variants collapsed)
-- enforces one row per distinct repository. The write path (CreateRepo and the
-- auto-register entry point) must call NormalizeRemoteURL before writing so
-- that the index deduplication spans all three common transport forms:
--
--   git@github.com:alice/app.git
--   ssh://git@github.com/alice/app.git
--   https://github.com/alice/app.git
--
-- all collapse to https://github.com/alice/app and hit the same unique slot.
--
-- Local-only repos (no remote configured) must store a per-repo synthetic key
-- (file:///abs/git/toplevel) derived from the git toplevel path before being
-- passed to CreateRepo; two local-only repos in different paths are distinct
-- and do not collide on this index.
--
-- MySQL note: TEXT columns require a prefix length for UNIQUE indexes. 512 chars
-- covers all realistic NormalizeRemoteURL output. The canonical form is ASCII
-- (https://host/path) so 512 chars = 512 bytes with utf8mb4 for this field.
--
-- Greenfield: no existing rows to migrate.

-- +goose Up
-- +goose StatementBegin

ALTER TABLE bn_repos MODIFY remote_url VARCHAR(2048) NOT NULL;
CREATE UNIQUE INDEX bn_repos_remote_url_idx ON bn_repos (remote_url);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX bn_repos_remote_url_idx ON bn_repos;
ALTER TABLE bn_repos MODIFY remote_url TEXT NOT NULL;

-- +goose StatementEnd
