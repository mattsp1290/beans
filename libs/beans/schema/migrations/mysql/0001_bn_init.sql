-- 0001_bn_init.sql
--
-- Initial MySQL schema for the bn tracker store.

-- +goose Up
-- +goose StatementBegin

CREATE TABLE bn_projects (
    prefix     VARCHAR(255) PRIMARY KEY,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);

CREATE TABLE bn_issues (
    id          VARCHAR(255) PRIMARY KEY,
    prefix      VARCHAR(255) NOT NULL,
    identifier  VARCHAR(255),
    title       TEXT NOT NULL,
    description TEXT NOT NULL,
    priority    INT NOT NULL DEFAULT 2 CHECK (priority BETWEEN 0 AND 4),
    issue_type  VARCHAR(64) NOT NULL DEFAULT 'task',
    state       VARCHAR(32) NOT NULL DEFAULT 'open',
    labels      JSON NOT NULL,
    branch_name TEXT,
    url         TEXT,
    created_at  TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at  TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT bn_issues_prefix_fk FOREIGN KEY (prefix) REFERENCES bn_projects(prefix)
);

CREATE INDEX bn_issues_prefix_state_idx ON bn_issues (prefix, state);

CREATE TABLE bn_issue_deps (
    issue_id      VARCHAR(255) NOT NULL,
    blocked_by_id VARCHAR(255) NOT NULL,
    PRIMARY KEY (issue_id, blocked_by_id),
    CHECK (issue_id <> blocked_by_id),
    CONSTRAINT bn_issue_deps_issue_fk
        FOREIGN KEY (issue_id) REFERENCES bn_issues(id) ON DELETE CASCADE,
    CONSTRAINT bn_issue_deps_blocker_fk
        FOREIGN KEY (blocked_by_id) REFERENCES bn_issues(id) ON DELETE CASCADE
);

CREATE INDEX bn_issue_deps_blocker_idx ON bn_issue_deps (blocked_by_id);

CREATE TABLE bn_issue_notes (
    id         BIGINT PRIMARY KEY AUTO_INCREMENT,
    issue_id   VARCHAR(255) NOT NULL,
    actor      TEXT,
    body       TEXT NOT NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT bn_issue_notes_issue_fk
        FOREIGN KEY (issue_id) REFERENCES bn_issues(id) ON DELETE CASCADE
);

CREATE INDEX bn_issue_notes_issue_idx ON bn_issue_notes (issue_id, created_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE bn_issue_notes;
DROP TABLE bn_issue_deps;
DROP TABLE bn_issues;
DROP TABLE bn_projects;

-- +goose StatementEnd
