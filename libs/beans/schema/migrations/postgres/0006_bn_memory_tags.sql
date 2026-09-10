-- 0006_bn_memory_tags.sql
--
-- Normalized memory tags for portable all-tags filtering.

-- +goose Up
-- +goose StatementBegin

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM bn_memories WHERE jsonb_typeof(tags) <> 'array') THEN
        RAISE EXCEPTION 'bn_memory_tags backfill failed: memory tags must be JSON arrays';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM bn_memories m
        CROSS JOIN LATERAL jsonb_array_elements(m.tags) AS tag(value)
        WHERE jsonb_typeof(tag.value) <> 'string'
           OR tag.value #>> '{}' = ''
           OR char_length(tag.value #>> '{}') > 255
    ) THEN
        RAISE EXCEPTION 'bn_memory_tags backfill failed: memory tags must be non-empty strings <= 255 characters';
    END IF;
END $$;

CREATE TABLE bn_memory_tags (
    memory_id BIGINT       NOT NULL REFERENCES bn_memories(id) ON DELETE CASCADE,
    tag       VARCHAR(255) NOT NULL,
    PRIMARY KEY (memory_id, tag),
    CHECK (tag <> ''),
    CHECK (char_length(tag) <= 255)
);

INSERT INTO bn_memory_tags (memory_id, tag)
SELECT DISTINCT m.id, tag.value
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
