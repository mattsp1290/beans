# 02 — Architecture

## Two thin surfaces over one store

```
                ┌──────────────────────────────┐
   humans /     │  bn  (cmd/bn, fang+cobra)     │   author + manage + memory
   CC agents ──▶│  create/dep/ready/close/...   │
                └──────────────┬───────────────┘
                               │  (Go calls, same process)
                        ┌──────▼───────┐
                        │  store pkg    │  Postgres data access:
                        │ (issues/deps/ │  Issues, Deps, Memories, Notes
                        │  memories)    │  — pgx v5, no shell-out
                        └──────▲───────┘
                               │  (Go calls, in-process)
                ┌──────────────┴───────────────┐
   orchestrator │ tracker/postgres adapter      │   FetchCandidates/Close/...
   (in-process) │ implements tracker.Tracker    │   (the postgres-tracker plan)
                └──────────────────────────────┘
```

- **`store`** (`internal/tracker/postgres/store` or `internal/track/store`): the
  single Go package that owns the Postgres schema + all queries (create, read,
  filter, close, dep add/remove, ready-set, memory upsert/search, import upsert).
  Returns `core.Issue` / domain types.
- **`bn`** (`cmd/bn`): a fang/Cobra CLI that is a *thin presentation layer* over
  `store` (parse flags → call store → format human/`--json`/`--silent` output).
- **postgres tracker adapter** (`internal/tracker/postgres`): implements
  `tracker.Tracker` (FetchCandidates/FetchByStates/FetchStatesByIDs/Close/…) over
  the **same** `store`. This is the orchestrator's read/close path — in-process,
  no `bn` subprocess.

Because both surfaces sit on one `store`, the authoring CLI and the orchestrator
can never disagree about schema or "ready" semantics. This is the concrete fix
for the postgres-tracker review's "circular ingestion" finding: `bn` writes,
the adapter reads, one store, no bd in the loop.

## Where `bn` lives

`cmd/bn` **in the beans repo**, sharing the `store` package with the
tracker adapter. Rationale: they must evolve together (one schema, one
"ready" definition); a split module guarantees drift. beans **building**
`bn` does not mean beans **uses** it for its own tracking — per the
greenfield decision, beans stays bd/Dolt; `bn` is a tool the repo
produces for *other* projects.

(Alternative considered: a standalone `bn` module. Rejected for v1 — it would
duplicate `core.Issue`/`store` or force a shared library extraction before
there's a second consumer. Revisit if `bn` needs an independent release cadence.)

## Connection / configuration

- **Connection:** `BN_DSN` (a Postgres DSN) — distinct from the orchestrator's
  `POSTGRES_DSN` so `bn`'s tracker DB can be a different instance/db/schema than
  the orchestrator audit store if desired. Default to a documented local DSN.
- **Project / prefix scoping:** a single Postgres can host multiple projects.
  Scope by a `project`/`prefix` column (03) selected via `BN_PROJECT` or
  `--project`/`--prefix`. `bn init --prefix=foo` registers a project.
- **No `--db`** (Dolt-specific). `--actor` honored as in bd.
- **Secrets:** `BN_DSN` may carry a password — never log it; redact in any
  verbose/`--json` config dump (mirror `TrackerConfig.LogValue` discipline).

## Schema ownership & coexistence

- The orchestrator's audit store (`run_attempts`, `events`) and the `bn` tracker
  store are **different concerns**; keep the `bn` tracker in its **own schema**
  (or its own database). Decide per deployment (shared instance, separate schema
  = reuse infra, decoupled blast radius for migrations). Do **not** borrow the
  persistence pool — give `bn`/the adapter their own pgx pool to avoid starving
  audit writes (postgres-tracker review §3).
- Migrations: a `bn`-owned migration namespace (a small migrator, or reuse the
  pattern in `internal/persistence/migrate.go`), separate from `0001_init.sql`.

## Concurrency

Postgres handles concurrent `bn` invocations and the orchestrator adapter
natively — **no `runMu`, no embedded lock, no cross-uid bind-mount problem.**
`close` is idempotent; reads are consistent per-statement. (Cross-process
*dispatch* dedup is still the `Manager`+`run_attempts` concern — out of scope,
see 00 non-goals.)
