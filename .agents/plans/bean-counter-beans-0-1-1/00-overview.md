# Upgrade bean-counter to the latest beans

Plan owner: deploy/dependency workstream • Status: **revision 2 — reviewed by
two independent Opus reviewers; all findings folded in (see `05` log)**

## Goal

Move bean-counter's `github.com/mattsp1290/beans` dependency from the current
pin to the version production already runs, so bean-counter and the
local-symphony orchestrator share one beans schema/version. This **resolves the
runtime-compatibility finding** from the deploy `--check` (recorded in
`bean-counter-am5` / `bd remember`): prod is at beans schema version **8**,
bean-counter ships **7**.

## Versions (grounded)

| | Version | beans schema max | Notes |
|---|---------|------------------|-------|
| bean-counter now | `v0.1.1` (tag) | 7 (`0007_bn_semantic_guards`) | `go.mod` |
| Production (local-symphony) | `v0.1.2-0.20260615002029-e52dce57b52c` | 8 (`0008_bn_dep_type`) | local-symphony `go.mod` |
| beans `main` tip (`~/git/beans`) | commit `e52dce5` | 8 | **same commit as prod** |

The only published tags are `v0.1.0` and `v0.1.1`; production runs a
**pseudo-version** built from commit `e52dce57b52c`, which is also the current
tip of beans `main`. So "the latest beans" is unambiguous: **commit
`e52dce57b52c`**. Resolve it with:

```bash
go get github.com/mattsp1290/beans@e52dce57b52c
# -> github.com/mattsp1290/beans v0.1.2-0.20260615002029-e52dce57b52c
```

Targeting this exact commit makes bean-counter's embedded migration max = 8 =
prod, which flips the deploy parity gate from "older (compat risk)" to "exact
match."

## Why this is a small, low-risk upgrade

Verified by diffing `v0.1.1` against the target:

- **`model` package is byte-identical** — `Issue`, `Priority`, `IssueState`,
  `RepoTarget` (all re-exported by `internal/store/adapter.go`) are unchanged.
- **`go.mod` is unchanged** — same `go` directive (1.25.7-compatible) and the
  **same `require` set** (no new transitive dependencies).
- **`store` API changes are additive**: `DepEdge` gains a `DepType` field;
  `AddDep` is preserved (now a thin wrapper over the new `AddTypedDep(...,
  "blocks")`); new methods (`AddTypedDep`, `ListBlockingDeps`, `ListMembers`,
  `ListParents`) and consts (`DepTypeBlocks`, `DepTypeParentChild`,
  `ValidateDepType`) are new. `New`, `Config`, `ListFilter`, `CreateIssueInput`,
  `UpdateIssueInput`, and the error sentinels are unchanged.

bean-counter therefore **compiles without source changes**. There is exactly
one *behavioral* change to weigh (next).

## The one behavioral change to decide

`0008_bn_dep_type` adds a `dep_type` column to `bn_issue_deps`, distinguishing
**blocking** edges (`blocks`) from **membership** edges (`parent-child`, used by
the new epic hierarchy). Two store-behavior shifts follow:

1. **`ListDeps` now returns ALL edge kinds** (blocking *and* parent-child),
   ordered deterministically. bean-counter's `GET /api/v1/deps` and
   `GET /api/v1/graph` both call `ListDeps`, so after the bump they would
   surface parent-child membership edges mixed with blocking ones — and the
   **production tracker has real epic data**, so this is not hypothetical.
   beans adds `ListBlockingDeps` for the blocks-only view.
2. **`ReadyIssues` now excludes epics** (`issue_type='epic'`) and only blocking
   edges gate readiness. This is a transparent correctness improvement for
   bean-counter's `GET /api/v1/ready`; no code change needed.
3. **`blocked_by` is now blocks-only** — `populateBlockedBy` filters to
   `dep_type='blocks'`, so the `blocked_by` array in `GET /api/v1/issues` no
   longer includes parent-child edges. Transparent improvement; no code change,
   but worth a test assertion. (Found in review; see `01`.)

**Recommendation (see `01`/`02`):** preserve current behavior by pointing the
`deps` and `graph` handlers at `ListBlockingDeps`, keeping "dependency graph =
blocking edges." Surfacing epics/membership in the UI is a deliberate feature,
filed as a follow-up — not bundled into a dependency bump.

## Document map

- `01-impact-analysis.md` — grounded, file-by-file impact on bean-counter.
- `02-implementation.md` — exact steps and code changes.
- `03-validation-and-rollback.md` — gates, prod-parity verification, rollback.
- `04-task-sequence.md` — beads-ready ordered tasks.
- `05-open-questions-and-review.md` — decisions + reviewer reconciliation log.
