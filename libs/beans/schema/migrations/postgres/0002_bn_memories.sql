-- 0002_bn_memories.sql
--
-- Adds the bn_memories table for bn remember / bn memories.
-- Memories are append-only knowledge entries scoped to a project or global
-- (prefix IS NULL). Full-text search via a generated tsvector + GIN index.
--
-- Per project policy: no IF NOT EXISTS — the runner owns "should this run?"
-- via bn_schema_versions + a session advisory lock.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE bn_memories (
    id         BIGSERIAL PRIMARY KEY,
    prefix     TEXT REFERENCES bn_projects(prefix) ON DELETE CASCADE,
    body       TEXT NOT NULL,
    mtype      TEXT,
    tags       JSONB NOT NULL DEFAULT '[]',
    tsv        tsvector GENERATED ALWAYS AS (to_tsvector('english', body)) STORED,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX bn_memories_tsv_idx ON bn_memories USING GIN (tsv);
CREATE INDEX bn_memories_prefix_idx ON bn_memories (prefix);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE bn_memories;

-- +goose StatementEnd
