# 01 — Command surface (bd-compat contract)

`bn` mirrors the subset of `bd` the skills, the global beads rules, and the
orchestrator-author workflow actually use. Each command lists the flags that must
be honored and the **output contract** callers depend on. Where bd's surface is
Dolt-specific, `bn` diverges (noted).

## Issue lifecycle

| `bn` command | Mirrors `bd` | Must support | Output contract |
|---|---|---|---|
| `bn init` | `bd init` | `--prefix`, `--non-interactive`, `--quiet` | Initializes a project (registers the prefix; ensures schema). NOT a Dolt/git init. |
| `bn create "title"` | `bd create` | `-d/--description`, `-p/--priority` (0–4), `-l/--label` (repeatable), `-t/--type` (bug/feature/task/epic/chore), `--silent` | `--silent` ⇒ **bare id on stdout** (`{prefix}-{hash}`), nothing else (skills capture `ID=$(bn create … --silent)`). Non-silent ⇒ human line. |
| `bn ready` | `bd ready` | `--json`, `-n/--limit` | Lists **open + unblocked** issues. `--json` ⇒ array of issue objects (see 03). |
| `bn list` | `bd list` | `--status=<state>`, `--all`, `-n`, `--json` | Filter by status; `--all` overrides the default page cap. |
| `bn show <id>` | `bd show` | `--json`, `--` separator | One issue (incl. labels, deps, counts). NotFound ⇒ non-zero exit + stderr "not found". |
| `bn update <id>` | `bd update` | `--claim`, `--status=<state>`, `--title/--description/--notes/--design`, `--append-notes` | Mutates fields; `--append-notes` appends (non-idempotent). |
| `bn close <id>` | `bd close` | `-r/--reason`, `--force` | **Idempotent** (close-of-closed = 0-exit no-op). `-r` recorded as a note. |
| `bn delete <id>` | `bd delete` | `--force` | Hard delete (guarded). |

## Dependencies

| `bn` command | Mirrors `bd` | Notes |
|---|---|---|
| `bn dep add <child> <parent>` | `bd dep add` | child blocked until parent closes (the convention the skills rely on). Reject cycles. |
| `bn dep remove <child> <parent>` | `bd dep remove` | |
| `bn dep tree` | `bd dep tree` | Render the dependency tree (human; `--json` optional). |
| `bn dep cycles` | `bd dep cycles` | Detect cycles (used as a guard). |

## Memory (scope decision: included — see 05)

| `bn` command | Mirrors `bd` | Notes |
|---|---|---|
| `bn remember "insight"` | `bd remember` | Persist a memory; optional `--type`, `--tag`. |
| `bn memories <keyword>` | `bd memories` | Search memories; `--json`. |
| `bn prime` | `bd prime` | Print workflow/context help (static-ish; documents `bn` usage). |

## Import / export (explicitly requested)

| `bn` command | Mirrors `bd` | Notes |
|---|---|---|
| `bn export` | `bd export` | Emit **bd-export-compatible** JSONL (one issue/line). See 06. |
| `bn import [file]` | `bd import` | Upsert from bd-compatible JSONL (default `.beads/issues.jsonl` analogue → a configurable path). See 06. |

## Global flags (every command)

- `--json` — machine output (where applicable). The orchestrator does **not** rely
  on this (it reads the store in-process), but the skills/agents do.
- `--actor <name>` — audit actor (mirrors `bd --actor`); default `$BN_ACTOR` /
  git `user.name` / `$USER`.
- **`--db` is NOT used.** bd's `--db=<dir>` selects a Dolt data dir; `bn` selects a
  Postgres connection via env/config (see 02). `bn` may accept a `--project`/
  `--prefix` to scope multi-project DBs. (A `--db` alias could be accepted and
  rejected with a clear "use BN_DSN" message for muscle-memory.)

## bd-compat contract (what MUST match exactly)

These are the load-bearing compatibilities — verified by parity tests (08):

1. **`create --silent` prints only the id**, newline-terminated, to stdout.
2. **id format** `{prefix}-{shorthash}` (see 03) so skills/agents can pass ids
   around unchanged.
3. **`ready`** = open **and** all blockers in a terminal state (the configured
   terminal set, not hardcoded "closed" — see postgres-tracker review).
4. **`close` idempotent**; **`show`/`update` on missing id** ⇒ non-zero + a
   "not found"-class stderr (so callers can detect it).
5. **exit codes**: 0 success; non-zero on error, with stderr classifiable
   (not-found / validation / conflict) for scripted callers.

## Deliberate divergences from bd

- No `bd dolt push` / Dolt/git plumbing (Postgres has no working-set/commit model
  to push).
- No `bd graph` ASCII visualisation in v1 (deferred; `dep tree` covers the need).
- `--db` → DSN/config (above).
- Memory storage is Postgres rows, not bd's store (05); search is SQL/`ILIKE` or
  full-text, not bd's matcher (document the difference).
