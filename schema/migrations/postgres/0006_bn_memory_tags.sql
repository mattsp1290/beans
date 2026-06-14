-- 0006_bn_memory_tags.sql
--
-- Normalized memory tags for portable all-tags filtering.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE bn_memory_tags (
    memory_id BIGINT       NOT NULL REFERENCES bn_memories(id) ON DELETE CASCADE,
    tag       VARCHAR(255) NOT NULL,
    PRIMARY KEY (memory_id, tag),
    CHECK (tag <> ''),
    CHECK (char_length(tag) <= 255)
);

INSERT INTO bn_memory_tags (memory_id, tag)
SELECT m.id, tag.value
FROM bn_memories m
CROSS JOIN LATERAL jsonb_array_elements_text(m.tags) AS tag(value)
ON CONFLICT DO NOTHING;

CREATE INDEX bn_memory_tags_tag_memory_idx ON bn_memory_tags (tag, memory_id);
CREATE INDEX bn_memory_tags_memory_idx ON bn_memory_tags (memory_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE bn_memory_tags;

-- +goose StatementEnd
