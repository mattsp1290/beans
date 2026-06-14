-- 0002_bn_memories.sql
--
-- Adds the bn_memories table for MySQL.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE bn_memories (
    id         BIGINT PRIMARY KEY AUTO_INCREMENT,
    prefix     VARCHAR(255),
    body       TEXT NOT NULL,
    mtype      VARCHAR(64),
    tags       JSON NOT NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT bn_memories_prefix_fk
        FOREIGN KEY (prefix) REFERENCES bn_projects(prefix) ON DELETE CASCADE
);

CREATE FULLTEXT INDEX bn_memories_body_ft_idx ON bn_memories (body);
CREATE INDEX bn_memories_prefix_idx ON bn_memories (prefix);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE bn_memories;

-- +goose StatementEnd
