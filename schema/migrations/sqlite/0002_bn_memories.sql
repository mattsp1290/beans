-- 0002_bn_memories.sql
--
-- Adds the bn_memories table for SQLite.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE bn_memories (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    prefix     TEXT REFERENCES bn_projects(prefix) ON DELETE CASCADE,
    body       TEXT NOT NULL,
    mtype      TEXT,
    tags       TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tags)),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX bn_memories_prefix_idx ON bn_memories (prefix);

CREATE VIRTUAL TABLE bn_memories_fts USING fts5(
    body,
    content='bn_memories',
    content_rowid='id'
);

CREATE TRIGGER bn_memories_fts_ai AFTER INSERT ON bn_memories BEGIN
    INSERT INTO bn_memories_fts(rowid, body) VALUES (new.id, new.body);
END;

CREATE TRIGGER bn_memories_fts_ad AFTER DELETE ON bn_memories BEGIN
    INSERT INTO bn_memories_fts(bn_memories_fts, rowid, body)
    VALUES ('delete', old.id, old.body);
END;

CREATE TRIGGER bn_memories_fts_au AFTER UPDATE ON bn_memories BEGIN
    INSERT INTO bn_memories_fts(bn_memories_fts, rowid, body)
    VALUES ('delete', old.id, old.body);
    INSERT INTO bn_memories_fts(rowid, body) VALUES (new.id, new.body);
END;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER bn_memories_fts_au;
DROP TRIGGER bn_memories_fts_ad;
DROP TRIGGER bn_memories_fts_ai;
DROP TABLE bn_memories_fts;
DROP TABLE bn_memories;

-- +goose StatementEnd
