-- 0006_bn_memory_tags.sql
--
-- Normalized memory tags for portable all-tags filtering in MySQL.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE bn_memory_tags (
    memory_id BIGINT NOT NULL,
    tag       VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    PRIMARY KEY (memory_id, tag),
    CHECK (tag <> ''),
    CONSTRAINT bn_memory_tags_memory_fk
        FOREIGN KEY (memory_id) REFERENCES bn_memories(id) ON DELETE CASCADE
);

INSERT INTO bn_memory_tags (memory_id, tag)
SELECT DISTINCT m.id, jt.tag
FROM bn_memories m
JOIN JSON_TABLE(
    m.tags,
    '$[*]' COLUMNS (
        tag VARCHAR(255) PATH '$' ERROR ON EMPTY ERROR ON ERROR
    )
) AS jt;

CREATE INDEX bn_memory_tags_tag_memory_idx ON bn_memory_tags (tag, memory_id);
CREATE INDEX bn_memory_tags_memory_idx ON bn_memory_tags (memory_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE bn_memory_tags;

-- +goose StatementEnd
