# 03 — Data model (Postgres schema)

Schema lives in a dedicated `bn` schema/namespace (02). Types map to
`core.Issue` (`internal/core/issue.go`: `ID, Identifier, Title, Description,
Priority, State, BranchName, URL, Labels[], BlockedBy[], CreatedAt, UpdatedAt`)
so the orchestrator adapter returns `core.Issue` unchanged.

## Tables

```sql
-- one row per project/prefix (bn init registers it)
CREATE TABLE bn_projects (
  prefix      TEXT PRIMARY KEY,           -- e.g. "lunusdotai"
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE bn_issues (
  id          TEXT PRIMARY KEY,           -- "{prefix}-{shorthash}"
  prefix      TEXT NOT NULL REFERENCES bn_projects(prefix),
  identifier  TEXT,                       -- human identifier if used
  title       TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  priority    INT  NOT NULL DEFAULT 2,    -- 0..4 (0=critical)
  issue_type  TEXT NOT NULL DEFAULT 'task', -- bug|feature|task|epic|chore
  state       TEXT NOT NULL DEFAULT 'open', -- open|in_progress|closed|... (see states)
  labels      JSONB NOT NULL DEFAULT '[]', -- string[]
  branch_name TEXT,
  url         TEXT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX bn_issues_prefix_state_idx ON bn_issues (prefix, state);

CREATE TABLE bn_issue_deps (
  issue_id      TEXT NOT NULL REFERENCES bn_issues(id) ON DELETE CASCADE, -- child
  blocked_by_id TEXT NOT NULL REFERENCES bn_issues(id) ON DELETE CASCADE, -- parent
  PRIMARY KEY (issue_id, blocked_by_id)
);

CREATE TABLE bn_issue_notes (              -- close --reason, --append-notes, comments
  id         BIGSERIAL PRIMARY KEY,
  issue_id   TEXT NOT NULL REFERENCES bn_issues(id) ON DELETE CASCADE,
  actor      TEXT,
  body       TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE bn_memories (                 -- bn remember / bn memories (05)
  id         BIGSERIAL PRIMARY KEY,
  prefix     TEXT REFERENCES bn_projects(prefix),  -- NULL = global
  body       TEXT NOT NULL,
  mtype      TEXT,                          -- user|feedback|project|reference (free)
  tags       JSONB NOT NULL DEFAULT '[]',
  tsv        tsvector GENERATED ALWAYS AS (to_tsvector('english', body)) STORED,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX bn_memories_tsv_idx ON bn_memories USING GIN (tsv);
```

## ID scheme (bd-compat)

bd ids are `{prefix}-{hash}` (e.g. `lunusdotai-a3f2dd`). `bn` replicates this so
ids are drop-in across skills/agents/orchestrator:
- `id = prefix + "-" + shorthash`, where `shorthash` is a short (6-char) base32/hex
  of a random or content seed. **Verify uniqueness** within the prefix on insert
  (retry on the rare collision; the PK enforces it).
- **No `Date.now()`/`rand` reproducibility constraints** apply here (unlike the
  workflow scripts) — `bn` is a normal binary.

## State model (parity is load-bearing)

- States: at minimum `open`, `in_progress`, `closed`. bd also has `deferred`/
  blocked-derived; `bn` should support the same vocabulary the orchestrator's
  `activeStates` (`["open","in_progress"]`, `poller.go:314`) and reconcile
  terminal states expect.
- **"ready" / blocked**: an issue is ready iff `state ∈ active` **and** every
  `blocked_by` issue is in a **terminal** state. The terminal set is **config-
  driven** (`WorkspaceConfig.TerminalStates` — includes e.g. `done`), per the
  postgres-tracker review; do **not** hardcode `= 'closed'`.
- **State vocabulary preserved verbatim** on import (no normalization), so the
  orchestrator's reconcile terminal-state match still works (postgres-tracker
  review §6).

## Mapping to `core.Issue`

`store` returns `core.Issue{ID, Identifier, Title, Description, Priority(int→
core.Priority), State(core.IssueState), Labels(jsonb→[]string), BlockedBy(from
bn_issue_deps), BranchName, URL, CreatedAt, UpdatedAt}`. `bn`'s `--json` output
matches bd's issue JSON keys (`id,title,status,priority,issue_type,labels,
dependency_count,…`) so agent-side parsers (and the bd-export interop in 06)
behave. Note bd uses `status`; map `state`↔`status` at the JSON boundary.
