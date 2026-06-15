-- 0009_bn_remote_url_unique.sql
--
-- Add a UNIQUE index on bn_repos.remote_url so that the NormalizeRemoteURL
-- canonical form (https://host/path, all SCP/ssh/https variants collapsed)
-- will enforce one row per distinct repository once the write path (CreateRepo
-- and the auto-register entry point) calls NormalizeRemoteURL before writing.
-- See beans-ph3 and beans-qea for that enforcement.  The three transport forms:
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
-- MySQL-specific changes (SQLite/Postgres only add the index):
--
--   1. TEXT → VARCHAR(768): MySQL cannot create a prefix-free UNIQUE index on a
--      TEXT column. VARCHAR(768) with utf8mb4 = 3072 bytes — exactly InnoDB's
--      key-length limit under ROW_FORMAT=DYNAMIC (MySQL 8 default).
--
--   2. COLLATE utf8mb4_bin: NormalizeRemoteURL lowercases only the host, not
--      the path, so /Alice/App and /alice/app are intentionally distinct keys.
--      The default utf8mb4_unicode_ci collation is case-insensitive and would
--      incorrectly deduplicate them. utf8mb4_bin matches the case-sensitive
--      behaviour of Postgres and SQLite and follows the bn_memory_tags.tag
--      convention already in this schema.
--
-- Greenfield: no existing rows to migrate.

-- +goose Up
-- +goose StatementBegin

ALTER TABLE bn_repos MODIFY remote_url VARCHAR(768) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL;
CREATE UNIQUE INDEX bn_repos_remote_url_idx ON bn_repos (remote_url);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX bn_repos_remote_url_idx ON bn_repos;
ALTER TABLE bn_repos MODIFY remote_url TEXT CHARACTER SET utf8mb4 NOT NULL;

-- +goose StatementEnd
