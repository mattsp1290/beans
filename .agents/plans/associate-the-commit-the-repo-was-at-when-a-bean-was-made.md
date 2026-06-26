# Big Change Planning with Beads

## Agent Instructions

You are an expert software architect creating a comprehensive task breakdown for a change to an existing codebase. This task graph will be executed by AI agents working in parallel, coordinated through MCP Agent Mail with file reservations to prevent conflicts.

<quality_expectations>
Create a thorough, production-ready task graph. Include all necessary analysis, preparation, implementation, testing, and documentation tasks. Go beyond the basics — consider edge cases, error handling, security considerations, backwards compatibility, and integration points. Each task should be specific enough for an agent to execute independently without ambiguity.
</quality_expectations>

<critical_constraint>
You must NOT implement any of the changes yourself. Your ONLY output is a bash shell script containing `bd create` and `bd dep add` commands. Do NOT use `bd add` — the correct command is `bd create`. Do not write code. Do not create files other than the shell script. Do not modify existing files. Read and analyze the codebase, then produce the script.

The script MUST create a single parent **epic** first (`bd create -t epic`) and parent **every** task bead to it via `--parent "$EPIC"`, so the whole change is one trackable rollup. The epic is an organizational rollup only — never make it a blocking dependency (do NOT `bd dep add` to or from the epic; `bd dep add` is for real ordering edges between task beads, and a blocking edge on an epic both excludes it wrongly and inverts `bd dep tree`). Membership is the `--parent` relationship, nothing else.
</critical_constraint>

## Change Information

### Change Type
NEW_FEATURE

### Description
Associate the commit the current git repository was at when a bean was made. When `bn create` is run from repositories such as `~/git/topdown` or `~/git/kitty`, the shared Postgres database should record both the normalized origin repo and the exact commit SHA from that repo at creation time.

The commit is a creation-time snapshot of the resolved repository context, not mutable repo registry state. Store it on the per-issue repo link row (`bn_issue_repos`), not on `bn_repos`.

Use this concrete data contract:
- Database column: `bn_issue_repos.creation_commit TEXT NOT NULL DEFAULT ''`.
- Store input/model field: `IssueRepoInput.CreationCommit` and `model.RepoTarget.CreationCommit`.
- CLI JSON field: nested repo field `creation_commit`, with `omitempty` so existing rows with `''` do not emit a misleading value.
- Validation: when non-empty, the value must be the full lowercase 40-character hex object ID returned by `git rev-parse HEAD`. Do not accept symbolic refs, abbreviated hashes, or arbitrary object names.

Commit capture rules:
- Capture the cwd `HEAD` only when the local git repo resolved from cwd is the same repo being associated to the new issue.
- For plain cwd auto-detect, this is true by construction: `bn` reads the cwd git root, normalizes `remote.origin.url` or synthesizes `file:///abs/toplevel`, auto-registers that repo, and stores that same repo's `HEAD`.
- For `.bn` marker repo and explicit `--repo <slug-or-url>`, capture cwd `HEAD` only if cwd is inside a git repo and its normalized remote URL, or synthesized `file://` URL for local-only repos, resolves to the same repo row selected by the marker or flag. If it does not match, create the bean normally and leave `creation_commit` empty.
- Do not error solely because a commit cannot be captured. Outside git repos, unborn repos with no `HEAD`, missing `git`, permission errors, or `git rev-parse HEAD` failures should leave `creation_commit` empty while preserving existing create behavior.
- Detached `HEAD`, merge states, rebase states, dirty worktrees, linked worktrees, and submodules should record the exact `HEAD` commit if `git rev-parse HEAD` succeeds and the repo identity check matches. Dirty state is intentionally not recorded in this change.
- Local-only repos with no `remote.origin.url` should continue using the synthesized `file:///abs/toplevel` identity and should record `HEAD` when available.

Update and import/export rules:
- `creation_commit` is immutable creation metadata. `bn update --repo`, `--ref`, or `--subdir` must preserve the existing value when retargeting an issue unless import explicitly supplies a value while creating a new row.
- Store-level `UpdateIssue` must avoid accidentally clearing `creation_commit` when it deletes/reinserts or replaces `bn_issue_repos`.
- `bn export` currently uses the JSONL `bdExportLine` shape in `cmd/bn/cmd_export.go`. Extend that shape with an optional nested `repo` object when an issue has repo routing metadata. Export at least `slug`, `remote_url`, `requested_ref`, `base_ref`, `work_branch`, `worktree_subdir`, `metadata`, and non-empty `creation_commit`; include other existing `RepoTarget` fields if that is simpler to keep the shape aligned with `toIssueJSON`.
- `bn import` should preserve `creation_commit` only when the imported JSON line includes enough repo identity to create a repo link: prefer `repo.remote_url` and auto-register/resolve it via the existing create-time remote URL path; otherwise use `repo.slug` only if that repo already exists in the target prefix. If `creation_commit` is present without a resolvable repo target, reject that line with a clear import error instead of silently dropping the commit. Older JSON without `repo` or without `creation_commit` should continue to import with no repo link or an empty commit value as today.

### Links to Relevant Documentation
N/A

### Affected Areas
- `cmd/bn/git_resolver.go` and `cmd/bn/git_resolver_test.go`: extend the git resolver seam beyond `Toplevel` and `RemoteURL` so `bn` can capture the current `HEAD` commit without shelling out directly from command code.
- `cmd/bn/repo_resolve.go`: preserve the current auto-detect behavior for origin URL registration while making the detected commit available to repo-aware commands.
- `cmd/bn/cmd_create.go`: pass the creation-time commit into `store.CreateIssue` whenever a repo link is resolved from cwd auto-detect, `.bn` marker, or explicit `--repo` where a local git context is available.
- `cmd/bn/app.go`: include the new commit field in the stable CLI JSON `repo` object, while keeping table output and non-JSON output backwards-compatible.
- `cmd/bn/cmd_export.go` and `cmd/bn/cmd_import.go`: round-trip non-empty creation commits in repo target JSON while accepting older exports that lack the field.
- `model/issue.go`: add a repo-target snapshot field for the creation-time commit SHA.
- `store/store.go`: extend `IssueRepoInput`, `insertIssueRepoGORM`, `populateIssueRepos`, `repoTargetFromIssueRepo`, `CreateIssue`, and `UpdateIssue` behavior so the commit is stored and hydrated consistently.
- `store/gorm_models.go`: add the GORM mapping for the new `bn_issue_repos` column.
- `schema/migrations/sqlite`, `schema/migrations/postgres`, and `schema/migrations/mysql`: add migration `0011` that adds `creation_commit TEXT NOT NULL DEFAULT ''` to `bn_issue_repos` without breaking existing rows.
- `store/store_sqlite_contract_test.go` and `store/store_integration_test.go`: cover persistence, hydration, and cross-dialect behavior for the creation commit.
- `cmd/bn/cmd_auto_detect_test.go`, `cmd/bn/cmd_create_test.go`, `cmd/bn/app_test.go`, and related fake git resolver tests: cover CLI creation from different repo origins and JSON output.
- `docs/specs/repo-resolution-precedence.md`, `docs/specs/topology-a-prefix-equals-slug.md`, and `README.md`: document that issue repo routing now snapshots the detected creation commit.

### Success Criteria
- Running `bn create` with a shared Postgres store from `~/git/topdown` creates a bean whose repo target records the normalized `origin` URL for `topdown` and the exact `git rev-parse HEAD` SHA at the moment of creation.
- Running `bn create` with the same shared Postgres store from `~/git/kitty` creates a bean whose repo target records the normalized `origin` URL for `kitty` and that repo's exact `HEAD` SHA at creation.
- Existing repo auto-registration and scoping behavior remains unchanged: distinct origins still produce distinct `bn_repos` rows and issue prefixes under topology-a.
- Existing beans and existing `bn_issue_repos` rows continue to read successfully after migration, with an empty or absent creation commit represented predictably in Go and JSON.
- `bn show --json`, `bn list --json`, and `bn ready --json` expose the commit on the nested `repo` object for newly created beans.
- `bn export` includes non-empty `creation_commit` values and `bn import` preserves them on create while accepting older exports without the field.
- `bn update --repo`, `bn update --ref`, and `bn update --subdir` do not erase or replace the original `creation_commit` for an existing bean.
- Tests cover sqlite contract behavior, Postgres integration behavior, MySQL migration/schema behavior, and the CLI create path. The normal gates `make test`, `make vet`, and `make lint` pass, and Postgres coverage passes under the existing integration test setup.

### Constraints
N/A

---

## Your Task

Analyze this codebase change and create a comprehensive **Beads task graph** using the `bd` CLI. Beads provides dependency-aware, conflict-free task management for multi-agent execution.

Before creating the task graph, you MUST first analyze the affected areas of the codebase:

1. Check `docs/specs/` and `docs/adr/` for existing architectural decisions
2. Examine the directory/module structure of the affected areas listed above
3. Identify key interfaces, APIs, and integration points that must be preserved
4. Note existing test patterns and coverage in the affected areas
5. Assess risk areas where changes could break existing functionality

Use your analysis to make each bead specific — reference actual file paths, module names, and patterns you observed.

Then generate a shell script that creates the complete task graph.

**IMPORTANT: Your ONLY deliverable is a bash shell script with `bd create` commands. Not an implementation plan. Not a design document. Not a code review. A runnable `.sh` script.**

---

## Output Format

Generate a shell script that creates the full task graph. The script should:

1. **Initialize Beads** (if not already initialized)
2. **Create one parent epic** (`bd create -t epic`) representing the whole change, capturing its ID into `$EPIC`
3. **Create all task beads** with appropriate priorities, each parented to the epic via `--parent "$EPIC"`
4. **Establish dependencies** between task beads (ordering edges only — never to or from the epic)
5. **Add labels** for phase grouping (child beads inherit the epic's labels unless `--no-inherit-labels`)

### Example Output

```bash
#!/bin/bash
# Project: beans
# Change: Refactor auth middleware for compliance
# Generated: 2026-06-26

set -e

# Initialize beads if needed
if [ ! -d ".beads" ]; then
    bd init
fi

echo "Creating change beads..."

# ========================================
# Parent epic — every task below is parented to it (--parent "$EPIC").
# The epic is an organizational rollup: it is NEVER given a blocking dep
# (no `bd dep add` to or from it) and is never dispatched as work itself.
# ========================================

EPIC=$(bd create "Epic: Refactor auth middleware for compliance" -t epic -p 0 --label epic --silent)
bd update "$EPIC" --status in_progress   # rollup, not dispatchable work — keep it out of `bd ready`

# ========================================
# Phase 1: Analysis & Preparation
# ========================================

ANALYZE_CURRENT=$(bd create "Analyze current auth middleware implementation in src/auth/ — document all session token storage patterns and consumer dependencies" -p 0 --label analysis --parent "$EPIC" --silent)

IDENTIFY_DEPS=$(bd create "Map all modules importing from src/auth/ and catalog their usage patterns" -p 0 --label analysis --parent "$EPIC" --silent)

CHAR_TESTS=$(bd create "Add characterization tests capturing current auth middleware behavior before refactoring" -p 0 --label prep --parent "$EPIC" --silent)
bd dep add $CHAR_TESTS $ANALYZE_CURRENT

# ========================================
# Phase 2: Core Implementation
# ========================================

IMPL_NEW_STORAGE=$(bd create "Implement compliant session token storage in src/auth/session.ts replacing in-memory store" -p 0 --label impl --parent "$EPIC" --silent)
bd dep add $IMPL_NEW_STORAGE $CHAR_TESTS
bd dep add $IMPL_NEW_STORAGE $IDENTIFY_DEPS

IMPL_MIGRATION=$(bd create "Create migration script for existing session data to new storage format" -p 1 --label impl --parent "$EPIC" --silent)
bd dep add $IMPL_MIGRATION $IMPL_NEW_STORAGE

UPDATE_CONSUMERS=$(bd create "Update all consumer modules to use new auth middleware API surface" -p 1 --label impl --parent "$EPIC" --silent)
bd dep add $UPDATE_CONSUMERS $IMPL_NEW_STORAGE

# ========================================
# Phase 3: Testing & Validation
# ========================================

UNIT_TESTS=$(bd create "Add unit tests for new session storage implementation" -p 1 --label testing --parent "$EPIC" --silent)
bd dep add $UNIT_TESTS $IMPL_NEW_STORAGE

INTEGRATION_TESTS=$(bd create "Add integration tests for auth flow end-to-end with new middleware" -p 1 --label testing --parent "$EPIC" --silent)
bd dep add $INTEGRATION_TESTS $UPDATE_CONSUMERS

REGRESSION_CHECK=$(bd create "Run full regression suite and verify characterization tests still pass" -p 0 --label testing --parent "$EPIC" --silent)
bd dep add $REGRESSION_CHECK $INTEGRATION_TESTS
bd dep add $REGRESSION_CHECK $UNIT_TESTS

# ========================================
# Phase 4: Cleanup & Documentation
# ========================================

UPDATE_DOCS=$(bd create "Update auth middleware documentation and API reference" -p 2 --label docs --parent "$EPIC" --silent)
bd dep add $UPDATE_DOCS $REGRESSION_CHECK

CLEANUP=$(bd create "Remove deprecated session storage code and update changelog" -p 3 --label cleanup --parent "$EPIC" --silent)
bd dep add $CLEANUP $REGRESSION_CHECK

echo ""
echo "Bead graph created! View with:"
echo "  bd show $EPIC          # The parent epic and its rollup"
echo "  bd children $EPIC      # All task beads under the epic"
echo "  bd ready              # List unblocked tasks (the epic itself is not work)"
```

---

## Bead Creation Guidelines

### Epic / Hierarchy (REQUIRED)
- Create exactly **one parent epic** for the whole change: `EPIC=$(bd create "Epic: <change summary>" -t epic -p 0 --label epic --silent)`.
- Parent **every** task bead to it: add `--parent "$EPIC"` to every `bd create` (children inherit the epic's labels unless you pass `--no-inherit-labels`).
- The epic is a **rollup, not work**: never `bd dep add` to or from it. Membership is `--parent`; `bd dep add` is reserved for real ordering edges *between task beads*. A blocking edge on an epic wrongly keeps it out of (or drops it into) `bd ready` and inverts `bd dep tree`.
- **Keep the epic out of `bd ready`** by marking it active right after creation: `bd update "$EPIC" --status in_progress`. `bd ready` excludes `in_progress`/`blocked`/`deferred`/`hooked`. Do **not** rely on `--exclude-type epic` — that flag is ineffective on some `bd`/`bn` builds, whereas status-based exclusion works everywhere.
- An epic must have **≥ 2 children** to be meaningful — a one-task change does not need this skill.
- For very large changes you MAY use phase sub-epics (each `--parent "$EPIC"`, each with its own children), but a single top-level epic is the default and is sufficient for most changes.

### Priority Levels
- `-p 0` = Critical (blocking other work, or high-risk changes needing early validation)
- `-p 1` = High (important implementation work)
- `-p 2` = Medium (standard work)
- `-p 3` = Low (cleanup, nice-to-haves)

### Labels (Phase Grouping)
Use `--label` to group beads by phase:
- `analysis` - Understanding current state
- `prep` - Preparation work (characterization tests, feature flags, scaffolding)
- `impl` - Core implementation
- `testing` - Test coverage
- `migration` - Data/code migration
- `docs` - Documentation updates
- `cleanup` - Post-rollout cleanup

### Dependency Rules
1. Never create cycles
2. Analysis tasks should complete before implementation begins
3. Characterization tests should exist before changing code
4. Use `bd dep add CHILD PARENT` (child depends on parent completing first)
5. Parallel work should share a common ancestor, not depend on each other
6. `bd dep add` is for ordering edges **between task beads only** — never use it to attach a task to the epic (that is `--parent`), and never add a blocking edge to or from the epic

### Task Granularity
- Each bead should be completable in **under 750 lines of code changed**
- Tasks should be atomic enough for one agent to complete without coordination
- If a task requires multiple file areas, consider splitting by file area

---

## Change-Specific Considerations

### For New Features
- Start with analysis of similar existing features
- Consider feature flag for gradual rollout
- Plan for A/B testing if relevant
- Include documentation and changelog updates

### For Refactors
- Add characterization tests first (capture current behavior)
- Consider strangler fig pattern for large changes
- Plan incremental migration path
- Ensure no behavior changes unless intentional

### For Migrations
- Create rollback plan as an explicit task
- Plan data validation checkpoints
- Consider dual-write period if applicable
- Include monitoring/alerting tasks

### For Performance Changes
- Add benchmarks before and after
- Include load testing tasks
- Plan gradual rollout with monitoring
- Have rollback criteria defined

---

## File Reservation Planning

For each major work area, note the file patterns that will need exclusive reservation:

```bash
# Example reservation notes (add as bead descriptions)
# CAUTION: repo create path is shared by auto-detect and explicit --repo flows.
# Git resolver and create wiring: cmd/bn/git_resolver.go, cmd/bn/repo_resolve.go, cmd/bn/cmd_create.go, cmd/bn/*test.go
# Store/model/schema: model/issue.go, store/store.go, store/gorm_models.go, schema/migrations/{sqlite,postgres,mysql}/0011_*.sql
# JSON/import/export/docs: cmd/bn/app.go, cmd/bn/cmd_export.go, cmd/bn/cmd_import.go, README.md, docs/specs/*.md
```

This helps agents claim appropriate file surfaces when they start work.

---

## Verification Steps

After generating the script:

1. **Run it**: `chmod +x setup-beads.sh && ./setup-beads.sh`
2. **Check the rollup**: `bd children "$EPIC"` should list every task bead, and `bd dep tree` should show them under the epic with no orphan (un-parented) tasks
3. **Check ready work**: `bd ready` should show initial analysis/prep tasks and **not** the epic. Epics are rollups, never dispatched as work — and because some `bd`/`bn` builds do not exclude epic-typed issues from `ready` (with `--exclude-type epic` sometimes ineffective), the script marks the epic `in_progress` right after creating it; status-based exclusion keeps it out of `ready` on every build.
4. **Check no cycles**: `bd dep cycles` should report none

---

## Completeness Checklist

Ensure your task graph includes:

- [ ] A single parent epic (`-t epic`); every task bead parented to it via `--parent "$EPIC"`, with no orphan tasks and no blocking dep to/from the epic
- [ ] Analysis of current implementation in affected areas
- [ ] Characterization tests for existing behavior
- [ ] Feature flag or gradual rollout mechanism (if applicable)
- [ ] Core implementation broken into small units
- [ ] Unit tests for new/changed code
- [ ] Integration tests for affected workflows
- [ ] Regression testing plan
- [ ] Documentation updates
- [ ] Migration scripts (if data changes)
- [ ] Rollback plan
