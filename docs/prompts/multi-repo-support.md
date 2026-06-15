# Big Change Planning with Beads

## Agent Instructions

You are an expert software architect creating a comprehensive task breakdown for a change to an existing codebase. This task graph will be executed by AI agents working in parallel, coordinated through MCP Agent Mail with file reservations to prevent conflicts.

<quality_expectations>
Create a thorough, production-ready task graph. Include all necessary analysis, preparation, implementation, testing, and documentation tasks. Go beyond the basics — consider edge cases, error handling, security considerations, backwards compatibility, and integration points. Each task should be specific enough for an agent to execute independently without ambiguity.
</quality_expectations>

<critical_constraint>
You must NOT implement any of the changes yourself. Your ONLY output is a bash shell script containing `bd create` and `bd dep add` commands. Do NOT use `bd add` — the correct command is `bd create`. Do not write code. Do not create files other than the shell script. Do not modify existing files. Read and analyze the codebase, then produce the script.
</critical_constraint>

## Change Information

### Change Type
NEW_FEATURE

### Description
Add multi-repository support to beans (`bn`). Today beans resolves a single project context per invocation; the goal is for `bn` to operate seamlessly across the many git repositories under `~/git/`. Specifically:

- Running `bn create` from **any valid git repository** under `~/git/` should create an issue that records **which repo** it came from, auto-detecting the repo from the current git working directory.
- Running `bn list` (and every other read/query command where it makes sense) from inside a git repo should, by default, scope its output to **that repo's** issues.
- Both `bn create` and `bn list` (and other appropriate commands) must accept a flag to **explicitly override** the repo, so the user can target a different repo than the cwd.
- The behavior must extend to **all CLI commands where repo scoping is appropriate** (e.g. `create`, `list`, `ready`, `show`, `update`, `close`, `delete`, `dep`, `export`, `import`), not just `create`/`list`.

**Confirmed design decisions (from planning interview):**

1. **Unregistered repos → auto-register.** When `bn create`/`bn list` runs inside a git repo that beans has not seen before, beans should auto-register it (create the necessary `bn_repos` row, deriving the slug and remote from git) and then proceed — no manual `bn repo add` required first. Keep this path non-interactive so it works in scripts.
2. **Repo identity = normalized git remote URL.** Match the current working directory to a repo by reading `git config --get remote.origin.url` and normalizing it (e.g. canonicalize `git@github.com:me/app.git` vs `https://github.com/me/app.git`, strip trailing `.git`, lowercase host). This is the stable key, since `bn_repos.remote_url` already exists. The slug is derived from the remote (fall back to the git-root directory basename only when there is no remote — local-only repos).
3. **Per-repo ID prefixes (topology decision pre-made: option (a)).** Each repo gets its own issue-ID prefix (e.g. `app-a1b2`, `web-c3d4`) rather than all repos sharing one project-wide prefix. **This is not a `bn_repos` column tweak.** Scoping today flows through `bn_issues.prefix` (which is `NOT NULL REFERENCES bn_projects(prefix)` — see `schema/migrations/sqlite/0001_bn_init.sql`), `generateID(prefix)` builds the issue ID from that prefix (`store/store.go`), and **every** read path filters on `prefix` (`ListIssues`, `ReadyIssues`, `ListDeps`, `ListMembers`, `ListParents`, import-conflict logic). The decision is therefore **pre-made: each auto-registered repo gets its own `bn_projects` row where `prefix == repo slug`** (topology option (a)). This makes `prefix` *become* the repo key, so all existing `prefix`-scoped queries keep working unchanged and repo scoping comes "for free." The rejected alternative — a separate per-repo-prefix column on `bn_repos`/`bn_issues` with `bn_projects` relaxed (option (b)) — would force rewriting every prefix-scoped WHERE clause and is an unjustified teardown given the greenfield constraint. The implementation must confirm this against source and only deviate if a named blocker surfaces.

### Links to Relevant Documentation
N/A

### Affected Areas
Go codebase rooted at `/Users/punk1290/git/beans` (Cobra CLI + embedded Dolt/SQL store, Goose migrations).

- **`cmd/bn/`** — primary surface. The full registered command set (`newRootCmd()` in `app.go`) is: `init, create, ready, list, show, update, close, delete, dep, repo, export, import, remember, memories, prime`. Repo behavior must be applied deliberately per command (see below), **not** uniformly.
  - `app.go` — `appState`, `newRootCmd()`, persistent flags (`--project`, `--actor`, `--json`), `gitRoot()`, `activeProjectConfig`, `activeProjectMarkerPath()`, `parseActiveProjectConfig()`, `resolveProjectPrefix()`, `toIssueJSON()`. This is where git/cwd→repo resolution and the new `--repo` override plumbing live. **Contention hotspot** — sequence edits here as a tight chain, do not fan out parallel siblings against it.
  - `cmd_create.go` — `newCreateCmd()` (calls but does **not** define `cleanRepoSlug`). Add git auto-detect + auto-register. There is currently **no git-remote auto-detection here** — `cmd_create.go` reads `--repo`/marker only; this is net-new.
  - `cmd_list.go` — `newListCmd()` (add repo-scoped default + `--repo` override + `--all-repos` escape hatch). NOTE: `--all` already exists in `cmd_list.go` meaning "return all results / page-cap override" — do **not** conflate; use a distinct `--all-repos` for cross-repo listing.
  - **List-style commands that should DEFAULT-SCOPE by repo** (genuine repo filtering): `list`, `ready`, and the dep/membership listings (`dep` listing, members, parents). These need the new `ListFilter` repo field.
  - **ID-addressed commands that should VALIDATE, not filter** (`cmd_show.go`, `cmd_update.go`, `cmd_close.go`, `cmd_delete.go`): `GetIssue` looks up by primary-key `id`, and the ID already encodes the repo prefix (`app-a1b2`), so `bn show app-a1b2` is unambiguous regardless of cwd. Do **not** repo-filter these — filtering would break legitimate cross-repo lookups. At most accept `--repo`/auto-detect for consistency of UX and validate.
  - **`remember` / `memories`** (`cmd_memory.go`) — prefix-scoped today, so under topology (a) they become repo-scoped automatically; this needs explicit test coverage and a decision on whether memories are per-repo or global.
  - **`init`** — needs an explicit semantics decision: does it still take `--prefix`, or does auto-register subsume it?
  - **`prime`** — skips DB init; no change needed.
  - `cmd_repo.go` — `newRepoCmd()` and subcommands (`add`, `list`, `show`, `update`, `remove`, `doctor`, `admin`); **defines `cleanRepoSlug()` and `repoSlugRE`** (reserve this file for the auto-register path). Reuse/extend its slug/remote logic.
- **`store/`**:
  - `store.go` — **single-writer contention hotspot (~1700 lines)**: `Store`, `CreateIssue()`/`CreateIssueInput`, `IssueRepoInput`, `generateID()` (per-repo prefix; free function `prefix + "-" + hex`), `ListFilter` (add a `RepoSlug`/`RepoID` field), `ListIssues()`/`ReadyIssues()` (add repo filter). Today repos are populated *after* the query via `populateIssueRepos`; the repo filter is net-new. Also: `insertIssueRepoGORM` → `getRepoBySlugGORM(ctx, tx, prefix, slug)` resolves the repo using the **issue's** prefix, so the create contract must be redefined to resolve the repo first and derive the prefix *from the repo*. **Sequence all `store.go` edits as one bead or a tight chain — not parallel siblings.**
  - `repo_store.go` — `Repo`, `CreateRepo()`/`CreateRepoInput`, `GetRepoBySlug()`, `ListRepos()`, `ResolveRepoAlias()`. Add **net-new** `GetRepoByRemoteURL()` (no lookup-by-remote exists today) and the non-interactive auto-register entry point.
  - `gorm_models.go` — `gormIssue`, `gormRepo`, `gormIssueRepo`, `gormProject`, `gormRepoAlias`. Note `bn_repos` has `UNIQUE(prefix, slug)` only — no uniqueness on remote URL. `bn_issue_repos.issue_id` is the PRIMARY KEY (1 repo per issue).
- **`model/issue.go`** — `Issue`, `RepoTarget` (already present; confirm fields cover what `list` needs to display).
- **`repo/validation.go`** — `ValidateRemoteURL()` exists but **only validates**; there is **no remote-URL canonicalizer** (`Normalize*` helpers cover clone-strategy/branch, not URLs), and `isSCPRemote` handles `git@host:org/repo` separately from `url.Parse`. Add a **net-new `NormalizeRemoteURL()`** that unifies the SCP and URL-scheme branches into one canonical key.
- **Git seam (net-new, testability-critical)** — auto-detect reads `git config --get remote.origin.url`; introduce a git-resolver seam (interface or function var, injectable in tests) so the feature is unit-testable without `cd`-ing the test process.
- **`schema/migrations/{postgres,mysql,sqlite}/`** — a new migration **is required** (not optional): at minimum a unique index on the normalized `remote_url`. Head is `0008_bn_dep_type.sql`, so the new one is `0009_*`. It must land in **all three** drivers as a single atomic bead (a hidden serialization point — not parallelizable per-driver).
- **Tests** — store integration tests use **Docker/testcontainers Postgres 16 + MySQL 8.4** (`store_integration_test.go`, `testStore`, `testPostgresDSN`, `testMySQLDSN`); SQLite uses an **in-memory contract** path (`store_sqlite_contract_test.go`, `sqliteMemoryDSN`). `cmd/bn` unit tests call cobra `RunE` directly and **do not mock git at all** — auto-detect/auto-register/normalization tests must build fixture repos with `git init` + `git remote add` in `t.TempDir()` and/or use the injected git seam. `setup-beads.sh` / README for end-to-end.

### Success Criteria
> Note on `~/git/`: that is simply where the user's repos live, **not** an enforced path boundary. The feature must work from **any** git repo; no `~/git/`-prefix check is required (there is no such seam today and adding one is explicitly out of scope unless a separate task is added). Criteria below say "any git repo" so they are agent-verifiable with `t.TempDir()` fixtures.

Every criterion below must be verifiable by an agent via tests/commands (using the git-resolver seam + `git init` fixtures), not only by a human:

1. From **any** git repo, `bn create "title"` creates an issue recorded against that repo (verifiable via `bn show` / JSON output showing the repo), with **no** `--repo` flag and **no** prior manual registration required (auto-register fires).
2. From **any** git repo, `bn list` returns, by default, only the issues belonging to that repo. The same default-scoping applies to `ready` and the dep/membership listings.
3. Both `bn create` and `bn list` accept an explicit repo override flag (`--repo`) that targets a different repo than the cwd, and it takes precedence over auto-detection.
4. Repo behavior is applied per command-class, consistently: **list-style** commands (`list`, `ready`, dep/members/parents listings) default-scope by repo with `--repo` override and an `--all-repos` escape hatch; **ID-addressed** commands (`show`, `update`, `close`, `delete`) remain addressable by their fully-qualified issue ID across repos and must NOT silently repo-filter; `remember`/`memories` scoping behavior is decided and tested; `init` semantics are decided.
5. Issue IDs are prefixed per-repo (e.g. `app-…`, `web-…`), and creating issues in two different repos under one database produces issues with distinct repo prefixes that do not collide. Slug-derivation collisions across distinct remotes (e.g. `me/app` and `you/app` both deriving `app`) are detected and disambiguated rather than silently merged.
6. Running a `bn` command outside any git repo (or in a local-only repo with no remote) behaves sensibly with a clear message and does not crash or silently write to the wrong repo. Nested/submodule repos (where `git rev-parse --show-toplevel` resolves to the inner repo) and bare/detached repos have explicitly defined, tested behavior.
7. The full test suite passes (`make test` / `go test ./...`, including the Docker-backed Postgres/MySQL integration tests and the in-memory SQLite contract tests), `go vet` / `golangci-lint` is clean for the changed surface, and the README/docs demonstrate the new multi-repo workflow.

### Constraints
- **Greenfield — no users yet.** No data migration of existing rows, no DB backups, and no backwards-compatibility guarantees for existing on-disk databases are required. It is acceptable to require `bn init`/re-init or to drop and recreate local dev databases.
- **Schema migrations are still additive via Goose** — the new migration (`0009_*`, head is `0008_bn_dep_type.sql`) is **required**, not optional (it carries at minimum a unique index on the normalized `remote_url`). It must be added for all three drivers (`schema/migrations/{postgres,mysql,sqlite}/`) as a **single atomic bead** — this is a serialization point, not parallelizable per-driver. Migrations need not preserve or transform pre-existing data.
- **Topology decision is pre-made — option (a), prefix == repo slug** (see Description decision 3). The design/analysis bead's deliverable is a short decision record confirming this against source and enumerating the `store.go` query sites it touches; it must remain a `-p 0` blocker that every implementation bead depends on. Deviate to option (b) only if a named blocker is found.
- **Sequencing — there is a serial critical path; do not "fan out everything after analysis."** Order: topology decision → `NormalizeRemoteURL` + the `0009` unique-index migration → `GetRepoByRemoteURL` + non-interactive auto-register → git-resolver seam → `generateID`/`ListFilter`/scoping changes in `store.go` (single-writer chain) → per-command adoption in `cmd_*.go` (this layer genuinely fans out) → tests → docs. `cmd/bn/app.go` and `store/store.go` are file-contention hotspots — chain edits to each, reserve them, and parallelize the independent `cmd_*.go` files instead.
- **Three prerequisites are net-new code, not edits to existing helpers**: `NormalizeRemoteURL` (no URL canonicalizer exists), `GetRepoByRemoteURL` (no lookup-by-remote exists), and the git auto-detect/resolver seam (no git mocking exists). Task wording must say "add new …", not "extend …".
- **Auto-register must stay non-interactive** so scripted/CI usage is unaffected. Define what it does when a `.bn` marker is already present.
- **Resolution precedence must be specified explicitly** (currently `resolveProjectPrefix` → `--project` flag → `BN_PROJECT` env → `.bn` marker). The new chain must define where cwd auto-detect and `--repo` sit, e.g. `--repo` flag > cwd git auto-detect > `.bn` marker repo > error/auto-register, and how auto-detect interacts with an existing marker.
- **Remote-URL normalization is a correctness boundary** — `git@host:org/repo.git`, `https://host/org/repo.git`, and `ssh://git@host/org/repo` forms of the same repo must normalize to one key. Specify the canonical form precisely (lowercase host, strip `.git` suffix, strip userinfo, strip default ports, unify SCP ↔ ssh:// ↔ https) and verify it on both the write (register) and read (match) sides. The unique index in `0009` is on this normalized key.
- **1:1 issue↔repo** — `bn_issue_repos.issue_id` is a PRIMARY KEY, so each issue maps to exactly one repo. Do not model many-repos-per-issue. The existing `worktree_subdir` column covers the monorepo-subdir case.
- Keep changes idiomatic to the existing Cobra/GORM/Goose patterns; match existing error-wrapping, flag-naming (note the pre-existing `--all` in `cmd_list.go`/`cmd_repo.go` — use a distinct `--all-repos`), and JSON-output conventions.

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
2. **Create all beads** with appropriate priorities
3. **Establish dependencies** between beads
4. **Add labels** for phase grouping

### Example Output

```bash
#!/bin/bash
# Project: beans
# Change: Refactor auth middleware for compliance
# Generated: 2026-06-15

set -e

# Initialize beads if needed
if [ ! -d ".beads" ]; then
    bd init
fi

echo "Creating change beads..."

# ========================================
# Phase 1: Analysis & Preparation
# ========================================

ANALYZE_CURRENT=$(bd create "Analyze current auth middleware implementation in src/auth/ — document all session token storage patterns and consumer dependencies" -p 0 --label analysis --silent)

IDENTIFY_DEPS=$(bd create "Map all modules importing from src/auth/ and catalog their usage patterns" -p 0 --label analysis --silent)

CHAR_TESTS=$(bd create "Add characterization tests capturing current auth middleware behavior before refactoring" -p 0 --label prep --silent)
bd dep add $CHAR_TESTS $ANALYZE_CURRENT

# ========================================
# Phase 2: Core Implementation
# ========================================

IMPL_NEW_STORAGE=$(bd create "Implement compliant session token storage in src/auth/session.ts replacing in-memory store" -p 0 --label impl --silent)
bd dep add $IMPL_NEW_STORAGE $CHAR_TESTS
bd dep add $IMPL_NEW_STORAGE $IDENTIFY_DEPS

IMPL_MIGRATION=$(bd create "Create migration script for existing session data to new storage format" -p 1 --label impl --silent)
bd dep add $IMPL_MIGRATION $IMPL_NEW_STORAGE

UPDATE_CONSUMERS=$(bd create "Update all consumer modules to use new auth middleware API surface" -p 1 --label impl --silent)
bd dep add $UPDATE_CONSUMERS $IMPL_NEW_STORAGE

# ========================================
# Phase 3: Testing & Validation
# ========================================

UNIT_TESTS=$(bd create "Add unit tests for new session storage implementation" -p 1 --label testing --silent)
bd dep add $UNIT_TESTS $IMPL_NEW_STORAGE

INTEGRATION_TESTS=$(bd create "Add integration tests for auth flow end-to-end with new middleware" -p 1 --label testing --silent)
bd dep add $INTEGRATION_TESTS $UPDATE_CONSUMERS

REGRESSION_CHECK=$(bd create "Run full regression suite and verify characterization tests still pass" -p 0 --label testing --silent)
bd dep add $REGRESSION_CHECK $INTEGRATION_TESTS
bd dep add $REGRESSION_CHECK $UNIT_TESTS

# ========================================
# Phase 4: Cleanup & Documentation
# ========================================

UPDATE_DOCS=$(bd create "Update auth middleware documentation and API reference" -p 2 --label docs --silent)
bd dep add $UPDATE_DOCS $REGRESSION_CHECK

CLEANUP=$(bd create "Remove deprecated session storage code and update changelog" -p 3 --label cleanup --silent)
bd dep add $CLEANUP $REGRESSION_CHECK

echo ""
echo "Bead graph created! View with:"
echo "  bd ready              # List unblocked tasks"
```

---

## Bead Creation Guidelines

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
# CAUTION: These files have many consumers
# Auth refactor: src/auth/**, tests/auth/** (coordinate with API team)
# Shared utils: src/lib/utils.ts (high contention - keep changes minimal)
```

This helps agents claim appropriate file surfaces when they start work.

---

## Verification Steps

After generating the script:

1. **Run it**: `chmod +x setup-beads.sh && ./setup-beads.sh`
2. **Check ready work**: `bd ready` should show initial analysis/prep tasks

---

## Completeness Checklist

Ensure your task graph includes:

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
- [ ] Cleanup tasks for post-rollout
- [ ] Clear dependency chains with no cycles
