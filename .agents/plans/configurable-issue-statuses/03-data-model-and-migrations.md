# 03 — Data Model & Migrations

## The model type: unchanged

`IssueState` stays a typed string (`model/issue.go:48`); the `State` field and
its tags (`model/issue.go:71`, `json:"state" db:"state"`) are untouched. New
statuses are just new string values — no schema-of-the-struct change. The
`bd`-compatible JSON mapping (`status` external / `state` internal,
`cmd/bn/app.go:425,473`) flows the new values through unchanged.

The GORM model `gormIssue.State string` (`store/gorm_models.go`) is likewise
unchanged — it is already a free `string` column.

## The database constraint problem

Three dialects encode the vocabulary in the DB, and they differ in *how*:

| Dialect  | Where the CHECK lives                                        | How to remove        |
|----------|-------------------------------------------------------------|----------------------|
| SQLite   | **inline** in `CREATE TABLE` (`sqlite/0001_bn_init.sql:22`)  | table rebuild (hard) |
| Postgres | separate `ADD CONSTRAINT ... NOT VALID` (`postgres/0003`)   | `DROP CONSTRAINT`    |
| MySQL    | separate `ADD CONSTRAINT` (`mysql/0003`)                    | `DROP CONSTRAINT`    |

Per the **core decision** (`00-overview.md`), we remove the DB-level vocabulary
guard entirely and let the application layer be the sole authority. This is what
makes the vocabulary configurable: a config-defined status the DB would reject is
worthless.

Migrations are embedded and run via goose (`schema/schema.go:117` `Migrate`,
`//go:embed migrations/*/*.sql` at `schema/schema.go:18`). Add migration
**`0010_bn_issue_state_drop_check`** in each dialect directory. Goose discovers
them automatically by filename ordering.

### Postgres — `postgres/0010_bn_issue_state_drop_check.sql`

```sql
-- +goose Up
-- +goose StatementBegin
ALTER TABLE bn_issues DROP CONSTRAINT IF EXISTS bn_issues_state_check;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE bn_issues
    ADD CONSTRAINT bn_issues_state_check
    CHECK (state IN ('open', 'in_progress', 'blocked', 'closed', 'done'))
    NOT VALID;
-- +goose StatementEnd
```

### MySQL — `mysql/0010_bn_issue_state_drop_check.sql`

```sql
-- +goose Up
-- +goose StatementBegin
ALTER TABLE bn_issues DROP CONSTRAINT bn_issues_state_check;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE bn_issues
    ADD CONSTRAINT bn_issues_state_check
    CHECK (state IN ('open', 'in_progress', 'blocked', 'closed', 'done'));
-- +goose StatementEnd
```

> MySQL 8.0.16+ supports `DROP CONSTRAINT` / `DROP CHECK`. Confirm the project's
> MySQL floor (the testcontainers MySQL module pins a version — check
> `store/*_test.go` setup) and use `DROP CHECK bn_issues_state_check` if
> `DROP CONSTRAINT` is unsupported on the pinned version.

### SQLite — the table-rebuild wrinkle

SQLite cannot `ALTER TABLE ... DROP CONSTRAINT`. The CHECK is baked into the
`CREATE TABLE` (`sqlite/0001_bn_init.sql:13-28`). Removing it requires the
standard 12-step table rebuild. `sqlite/0010_bn_issue_state_drop_check.sql`:

```sql
-- +goose Up
-- +goose StatementBegin
PRAGMA foreign_keys=off;

CREATE TABLE bn_issues_new (
    id          TEXT PRIMARY KEY,
    prefix      TEXT NOT NULL REFERENCES bn_projects(prefix),
    identifier  TEXT,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    priority    INTEGER NOT NULL DEFAULT 2 CHECK (priority BETWEEN 0 AND 4),
    issue_type  TEXT NOT NULL DEFAULT 'task',
    state       TEXT NOT NULL DEFAULT 'open',   -- CHECK removed
    labels      TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(labels)),
    branch_name TEXT,
    url         TEXT,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO bn_issues_new
    SELECT id, prefix, identifier, title, description, priority, issue_type,
           state, labels, branch_name, url, created_at, updated_at
    FROM bn_issues;

DROP TABLE bn_issues;
ALTER TABLE bn_issues_new RENAME TO bn_issues;

PRAGMA foreign_keys=on;
-- +goose StatementEnd

-- +goose Down  (recreate WITH the original CHECK; symmetric rebuild)
-- +goose StatementBegin
-- ... mirror image: rebuild with state CHECK (state IN (...legacy 5...))
-- +goose StatementEnd
```

**Critical implementation notes for the SQLite rebuild:**

- **Copy the column list from the *current* head schema, not from `0001`.**
  Later migrations may have altered `bn_issues` (check `0004`–`0009`). The
  rebuild must reproduce the table as it exists at `0010`, including any columns
  / indexes / triggers added since. Verify against the actual post-`0009` shape
  before writing this file — this is the single highest-risk step in the plan.
- **Recreate any indexes/triggers** that referenced `bn_issues` after the rename.
- Goose runs each `.sql` statement block; keep the rebuild inside one
  `StatementBegin/End` so it is atomic per goose's executor. Confirm the SQLite
  driver (`glebarez/go-sqlite`) tolerates multi-statement blocks; if not, split
  into multiple `StatementBegin/End` pairs in dependency order.

## Backward / forward data compatibility

- **Existing rows** (`open`/`in_progress`/`blocked`/`closed`/`done`) remain valid
  — all are in the default config vocabulary.
- **Down migrations** restore the legacy 5-value CHECK. If any
  `ready_for_*` rows exist at downgrade time, the Postgres re-add uses
  `NOT VALID` (won't fail on existing rows) but MySQL/SQLite would reject them.
  Document downgrade as "only safe before new statuses are used"; this is an
  acceptable, clearly-flagged limitation for a vocabulary-expanding change.

## Default status on insert

The DB column keeps `DEFAULT 'open'`, but inserts should set `state` explicitly
from `WorkflowConfig.Default` so a deployment that changes the default status
takes effect. Confirm where create assembles the row (the store `CreateIssue`
path) and set `State` from config there if not already explicitly provided.
Leaving the DB default as `'open'` is a harmless backstop.
