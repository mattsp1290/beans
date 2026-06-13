# 05 — Memory (`bn remember` / `bn memories`) — DEFERRED FROM v1

> **Status (per `09` review reconciliation §C): CUT from v1.** Memory is
> independently shippable and the **orchestrator never reads it** (it only needs
> `tracker.Tracker`), so it adds a whole FTS subsystem with no bearing on the
> de-circularization or the dispatch loop. Build it only after the issue/dep/
> import core lands, if a project actually needs `bn`-native memory. The design
> below stands for when that time comes.

Scope decision (when built): `bn` **replaces bd's memory system** for the projects that use it
(CLAUDE.md uses `bd remember` for persistent knowledge and `bd memories` to
search). This is a second, distinct subsystem alongside the issue tracker — be
explicit that it is its own concern, not a special issue type.

## Commands

- `bn remember "<insight>"` — persist a memory.
  - flags: `--type <user|feedback|project|reference>` (free-text, mirrors the
    project's memory taxonomy), `--tag <t>` (repeatable), `--project/--prefix`
    (scope; default the active project; `--global` for cross-project).
  - stores a `bn_memories` row (03). `--json` ⇒ the stored id/echo.
- `bn memories <keyword...>` — search.
  - default: full-text (`tsv @@ plainto_tsquery`) over `body`, ranked, scoped to
    the active project + globals; `--all` for every project; `--type`/`--tag`
    filters; `--json` ⇒ array.
  - **Document the behavior difference vs bd:** bd's matcher and `bn`'s Postgres
    FTS will rank/match differently; `bn` should also support a substring/`ILIKE`
    fallback for exact-keyword recall, and the help text should set expectations.

## Data model

See 03 (`bn_memories` with a generated `tsvector` + GIN index). Memories carry an
optional `prefix` (NULL = global). No dependency/state machinery — memories are
append-only knowledge (a `bn forget <id>` delete is optional, guarded).

## Relationship to issues

Memories and issues are **separate tables and separate commands**; they share
only the `store` package and the `bn_projects` prefix. The orchestrator does not
read memories (it only needs `tracker.Tracker`); memory is purely a
human/agent-facing knowledge store. So the in-process tracker adapter (02) is
unaffected by this subsystem.

## Parity contract

- `bn remember` accepts the same free-form body + optional type/tag shape the
  project already uses, so existing `bd remember`-style usage in new projects
  works.
- `bn memories <kw>` returns matching memories (id + body + type + tags) and a
  `--json` form for agents.

## Risks / notes

- **Search-quality parity is "good enough," not identical** to bd. Frame this as
  an accepted divergence (document it), not a bug.
- **No bd `MEMORY.md`/file fragmentation** (CLAUDE.md warns against MEMORY.md) —
  `bn` keeps memory in Postgres rows, consistent with that guidance.
- Memory is the lowest-priority slice of `bn`; it can ship after the
  issue/dep/import core if sequencing demands (08), since the orchestrator does
  not depend on it. Flag it as independently shippable.
