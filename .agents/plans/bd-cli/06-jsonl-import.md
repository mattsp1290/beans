# 06 — `bn import` (JSONL) — and `bn export` (DEFERRED)

> **Status (per `09` §C): `import` (create-only) is in v1; `export`/round-trip is
> DEFERRED.** Reframed (per `09 §B/§F`): `bn import` is a **best-effort seed**, not
> a bd round-trip contract. `bn→bn` is lossless via an explicit edge field;
> `bd→bn` is best-effort because **bd may export dependency *counts*, not edges**
> — gate the import milestone on capturing a real `bd export` line to confirm.

Explicitly requested: `bn import` from a JSONL file. To keep it useful for
seeding, interop, and a possible future bd→bn migration, the format is
**bd-export-compatible**.

## Format

bd's `bd export` emits **newline-delimited JSON**, one issue per line, e.g.
(observed): `{"id","title","status","priority","issue_type","labels",
"created_at","updated_at","dependency_count","dependent_count","comment_count",…}`.
`bn export` emits the same shape (so `bn export` ≈ `bd export`), and `bn import`
accepts that shape (so a `bd export` file imports into `bn`).

Open question for review: bd export's exact dependency representation — does each
line carry its `blocked_by`/dep edges, or only `dependency_count`? `bn import`
must reconstruct `bn_issue_deps`, so the loader needs the **edges**, not just
counts. **Verify bd's export line for dependency edges before finalizing** (a
grounding gate); if bd only exports counts, `bn export` must add an edge field
(documented as a `bn` extension) and `bn import` of a *real* bd file will import
issues without deps (flag this limitation).

## `bn import [file]`

- Default path: a configurable `--file`/positional (no implicit `.beads/…` since
  `bn` isn't Dolt/git-backed; default to `./issues.jsonl` or require the arg).
- **Upsert semantics**, keyed by `id`: insert new; update existing. BUT — the
  **state-ownership hazard** from the postgres-tracker review applies: a blind
  upsert of `state` from an external file can **resurrect a closed issue**. So:
  - `--mode=create-only` (default): insert new ids; **skip** existing ids (never
    overwrite state) — safe for seeding.
  - `--mode=merge`: update fields but **never regress a terminal state** (if the
    DB row is terminal, keep it terminal regardless of the file).
  - `--mode=replace`: explicit, dangerous, overwrites verbatim (guarded; for
    intentional restores only).
  - Default to `create-only` so import can't re-open work the orchestrator closed.
- Preserve the **state vocabulary verbatim** (no normalization), per the
  postgres-tracker review, so reconcile terminal-state matching survives.
- Idempotent re-runs (create-only / merge are safe to repeat).
- **Two-pass, single transaction (per `09 §D4`):** `bn_issue_deps.blocked_by_id`
  is a NOT NULL FK, so streaming edge inserts fail on forward references. Insert
  **all issues first, then all edges**, inside one transaction; state the
  isolation expectation (the orchestrator's concurrent `Close` writes the same
  `state` column — `09 §D10`).
- `--json` summary: `{created, updated, skipped, deps_added}`.

## `bn export`

- Emit every issue in the active project (or `--all`) as bd-compatible JSONL,
  including dependency edges, to stdout (`> issues.jsonl`).
- Use for: snapshotting, git-tracking the issue set as text, and feeding another
  `bn`/`bd` instance.

## What this gives the postgres-tracker plan

This is the **interop/seed** path — NOT the primary ingestion path. The primary
ingestion is **native `bn create`/`bn dep add`** (which is what makes the
postgres-tracker non-circular). `import` exists for: seeding a new project from
an existing JSONL, a one-time bd→bn lift (greenfield projects that start from a
bd export), and round-tripping. Because authoring is native, the lost-update /
circular-sync problem that sank the postgres-tracker's import-only plan does not
recur here — import is opt-in and `create-only` by default.

## Risks

- **bd-export dependency fidelity** (the format gate above) — confirm before
  promising full bd→bn round-trip.
- **`replace` mode is a footgun** — guard it (confirmation / `--force`), never
  default.
- Large imports: stream line-by-line (don't load the whole file); batch inserts.
