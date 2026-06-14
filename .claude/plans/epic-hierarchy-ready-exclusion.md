# Plan: Epic/parent-child membership + ready exclusion for planning skills

Implements `~/.agents/projects/beans/requests/2026-06-14-epic-hierarchy-and-ready-exclusion-for-planning-skills.md`.

## Goals (from request Asks + Acceptance)

1. **Ask 1** — First-class parent-child membership edge, distinct from blocking. Non-blocking both
   directions, queryable, bd-export compatible (`type:"parent-child"`).
2. **Ask 2** — Exclude `issue_type='epic'` from `ready` / `ReadyIssues` (orchestrator never dispatches an epic).
3. **Ask 3** — `bn dep add … -t parent-child` must not hard-error; accept `-t/--type`, reject unsupported
   values with a beans-specific message naming supported kinds.

## Chosen surface

- Typed edge stored in `bn_issue_deps` via a new `dep_type` column (default `'blocks'`).
- `bn dep add <child> <parent> -t parent-child` — matches the exact bd invocation the skills emit.
- **Permissive `-t` (post-review)**: accept any non-empty type ≤50 chars (bd's `IsValid` rule) and
  store it verbatim. Reject only empty/too-long with a beans-specific message. This satisfies Ask 3's
  "copied bd scripts don't break" rather than whitelisting just two types.
- **Non-blocking semantics**: only `dep_type='blocks'` edges count as blockers and participate in
  cycle detection. Every other type (`parent-child`, `related`, …) is non-blocking membership/metadata.
- **Intentional divergence from bd**: bd's `AffectsReadyWork()` counts `parent-child` as blocking;
  beans deliberately does NOT (the request requires leaves not to block on their epic). bd's
  `IsBlockingEdge()` (excludes parent-child) is the closer analog. Do not "fix" toward bd parity.

> **Review fixes folded in:** C1 (both ready branches), schema_test `expectedMigrations` + parity must
> gain `{8,"bn_dep_type"}` (review I1), goose `StatementBegin/End` annotations per dialect (review I3),
> export keeps per-issue iteration using `ListDeps` only as a lookup table preserving `blocked_by_id ASC`
> (review I4), `list --epic` is the canonical ≥2-children surface (review I1), permissive `-t` (review I2).

## 1. Schema — new migration `0008_bn_dep_type.sql` (postgres / mysql / sqlite)

Add a `dep_type` column to `bn_issue_deps`. PK stays `(issue_id, blocked_by_id)` (one edge per pair).

- **sqlite**: `ALTER TABLE bn_issue_deps ADD COLUMN dep_type TEXT NOT NULL DEFAULT 'blocks';`
- **postgres**: `ALTER TABLE bn_issue_deps ADD COLUMN dep_type TEXT NOT NULL DEFAULT 'blocks';`
- **mysql**: `ALTER TABLE bn_issue_deps ADD COLUMN dep_type VARCHAR(64) NOT NULL DEFAULT 'blocks';`
  (MySQL TEXT cannot carry a literal DEFAULT pre-8.0.13; VARCHAR is safe.)
- Down: `ALTER TABLE bn_issue_deps DROP COLUMN dep_type;`
- Index for membership lookups (postgres/sqlite/mysql):
  `CREATE INDEX bn_issue_deps_type_idx ON bn_issue_deps (dep_type);`

No CHECK constraint — validation lives at the CLI/store so error messaging is beans-specific and the
DB stays migration-stable. `0008` is the next free version (current max is `0007`).

## 2. Store layer

### `store/gorm_models.go`
- Add `DepType string \`gorm:"column:dep_type;not null"\`` to `gormIssueDep`.

### `store/store.go`
- **Constants**: `DepTypeBlocks = "blocks"`, `DepTypeParentChild = "parent-child"`.
- **`AddDep` stays** (`childID, parentID`) → thin wrapper calling `AddTypedDep(..., DepTypeBlocks)`.
  Keeps ~25 existing call sites/tests unchanged.
- **New `AddTypedDep(ctx, childID, parentID, depType)`**: same body as today's `AddDep`, but:
  - sets `DepType: depType` on the inserted `gormIssueDep`;
  - the cycle/self checks **only apply to `depType == blocks`**. For `parent-child`, skip the
    `childID==parentID` self-as-cycle rejection? No — keep self-edge rejection for all types (a
    self membership edge is meaningless). But **skip the reachability cycle check** for non-blocking
    types (they don't create ordering cycles); still rely on PK/`OnConflict DoNothing` for dupes.
  - Duplicate (same pair) returns `ErrDuplicateDep` as today.
- **`loadDepGraph(db)`**: filter `WHERE dep_type = 'blocks'` so cycle detection and the `AddTypedDep`
  reachability check only see blocking edges.
- **`ReadyIssues`**: two additions —
  1. `q = q.Where("issue_type <> ?", "epic")` (Ask 2).
  2. **Both** blocker branches gain `AND d.dep_type = 'blocks'` so parent-child membership never
     blocks readiness. **Critical (review C1)**: there are two `NOT EXISTS` branches — the no-terminal
     no-JOIN branch at `store.go:357` (`SELECT 1 FROM bn_issue_deps d WHERE d.issue_id = …`) AND the
     JOIN branch at `:363`. Patch BOTH or a leaf with a parent-child edge goes non-ready when
     `terminalStates` is empty.
- **`DepEdge`**: add `DepType string`. `ListDeps` selects `d.dep_type` and orders stably.
- **`fetchBlockedBy` / `populateBlockedBy`**: add `AND dep_type = 'blocks'` (also `WHERE dep_type='blocks'`)
  so `BlockedBy` (used by show/list/JSON `blocked_by` and export) stays blocking-only. Parent-child
  edges must NOT surface as blockers.
- **New `ListMembers(ctx, parentID) ([]Issue, error)`**: issues that are parent-child children of
  `parentID` (`bn_issue_deps` where `blocked_by_id=parentID AND dep_type='parent-child'`, joined to
  `bn_issues`, ordered by priority/created_at). Backs `bn list --epic`.

### Import (store/store.go)
- `ImportInput`: add `ParentEdges []string` (epic/parent ids this issue is a parent-child member of).
  Keep `Deps []string` = blocking deps (unchanged → existing tests untouched).
- `importIssuesFullOnce` pass-2: after inserting blocking edges, insert parent-child edges from
  `ParentEdges` with `dep_type='parent-child'`, reusing the same missing-blocker/self/dup guards
  (skip the cycle guard for parent-child). Count successes in `DepsAdded`.

## 3. CLI

### `cmd/bn/cmd_dep.go`
- `dep add`: add `-t/--type` (default `blocks`). Permissive validation: trim; reject only empty or
  `len > 50` with a beans message (`fmt.Errorf("invalid dependency type %q: must be non-empty and at
  most 50 characters", depType)`). Otherwise store verbatim. Call
  `rs.store.AddTypedDep(ctx, child, parent, depType)`. Output message includes the type (and membership
  phrasing for `parent-child`).
- `dep tree` (review I1 — kept as ordering view): filter `ListDeps` to `DepType=='blocks'` before
  building the tree. Avoids the mixed-edge root-detection bug; membership lives in `list --epic`.
- `dep cycles`: filter edges to `DepType=='blocks'` before `detectCycles` (parent-child never cycles).

### `cmd/bn/cmd_list.go`
- Add `--epic <id>` flag. When set, call `rs.store.ListMembers(ctx, epicID)` and render with the same
  table/JSON path. Enables the planner's "every epic has ≥2 children" check without raw SQL.

### `cmd/bn/cmd_export.go`
- Replace per-issue `iss.BlockedBy` emission with a single `ListDeps(prefix)` grouped by child id,
  emitting `bdExportDep{IssueID, DependsOn, Type}` for **every** edge type (`blocks` + `parent-child`).
  Round-trips hierarchy (acceptance nice-to-have).

### `cmd/bn/cmd_import.go`
- `parseImportJSONL`: route `dep.Type=="blocks"` → `Deps`, `dep.Type=="parent-child"` → `ParentEdges`
  (only when `dep.IssueID==raw.ID`). Other types ignored as today.

### `cmd/bn/cmd_show.go` (small, optional-but-cheap)
- If the issue has parent-child membership, show `Parent:` / `Children:` lines via `ListMembers` /
  a parent lookup. Keeps epics inspectable in `show`. (Will implement if low-risk; otherwise dep tree
  + list --epic already satisfy acceptance.)

## 4. Tests

- **store contract/integration**: typed dep insert; `parent-child` edge does NOT block child or parent
  in `ReadyIssues`; `ReadyIssues` excludes `issue_type='epic'`; `ListMembers` returns the leaves;
  cycle detection ignores parent-child; `ListDeps` carries `DepType`.
- **cmd**: `dep add -t parent-child` succeeds; unsupported `-t foo` returns the beans error (not a cobra
  unknown-flag error); `dep add -t parent-child` then `ready` returns leaves but not the epic;
  `list --epic` lists members; export/import round-trip a parent-child edge.
- **schema**: migration count/version test (if present) updated for `0008`.
- Run full `go test ./...` + `go vet` + `golangci-lint` (per repo Makefile).

## 5. Acceptance mapping

| Acceptance | Covered by |
|---|---|
| epic + ≥2 leaves, members non-blocking, `ready` returns leaves not epic | §2 ReadyIssues (epic filter + blocks-only blocker), AddTypedDep |
| membership inspectable (`dep tree` / `list --epic`) | §3 dep tree render + list --epic / ListMembers |
| orchestrator `FetchCandidates` never dispatches epic | §2 ReadyIssues epic filter (FetchCandidates is pass-through) |
| `dep add … -t parent-child` no generic unknown-flag error | §3 dep add -t flag + validation |
| export/import round-trip membership (nice-to-have) | §3 export ListDeps grouping + import ParentEdges |
| new tag newer than v0.1.1 | bump VERSION after merge |

## Out of scope (per request)
- Multi-level epics/sub-epics (two levels only).
- Changing blocking-dep semantics / cycle detection for ordering edges.
- Priority/label/type vocabulary changes beyond the epic-readiness rule.
- dotfiles skill edits.
