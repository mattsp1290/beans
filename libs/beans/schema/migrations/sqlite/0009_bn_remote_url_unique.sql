-- 0009_bn_remote_url_unique.sql
--
-- Add a UNIQUE index on bn_repos.remote_url so that the NormalizeRemoteURL
-- canonical form (https://host/path, all SCP/ssh/https variants collapsed)
-- will enforce one row per distinct repository once the write path (CreateRepo
-- and the auto-register entry point) calls NormalizeRemoteURL before writing
-- (see beans-ph3 and beans-qea). The three common transport forms:
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
-- Greenfield: no existing rows to migrate. Per project policy, no IF NOT
-- EXISTS — the Goose runner gates execution via schema version tracking.

-- +goose Up
-- +goose StatementBegin

CREATE UNIQUE INDEX bn_repos_remote_url_idx ON bn_repos (remote_url);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX bn_repos_remote_url_idx;

-- +goose StatementEnd
