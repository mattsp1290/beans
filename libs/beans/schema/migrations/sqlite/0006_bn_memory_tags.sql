-- 0006_bn_memory_tags.sql
--
-- Normalized memory tags for portable all-tags filtering in SQLite.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE bn_memory_tags (
    memory_id INTEGER NOT NULL REFERENCES bn_memories(id) ON DELETE CASCADE,
    tag       TEXT NOT NULL COLLATE BINARY,
    PRIMARY KEY (memory_id, tag),
    CHECK (tag <> ''),
    CHECK (length(tag) <= 255)
);

INSERT INTO bn_memory_tags (memory_id, tag)
SELECT id, NULL
FROM bn_memories
WHERE json_type(tags) <> 'array';

INSERT INTO bn_memory_tags (memory_id, tag)
SELECT DISTINCT m.id,
       CASE
           WHEN json_each.type = 'text' THEN json_each.value
           ELSE NULL
       END
FROM bn_memories m, json_each(m.tags);

CREATE INDEX bn_memory_tags_tag_memory_idx ON bn_memory_tags (tag, memory_id);
CREATE INDEX bn_memory_tags_memory_idx ON bn_memory_tags (memory_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE bn_memory_tags;

-- +goose StatementEnd
