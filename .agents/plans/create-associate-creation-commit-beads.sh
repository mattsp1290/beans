#!/bin/bash
# Project: beans
# Change: Associate creation-time git commit with issue repo links
# Generated: 2026-06-26

set -euo pipefail

if [ ! -d ".beads" ]; then
    bd init
fi

echo "Creating change beads..."

# ========================================
# Parent epic -- organizational rollup only.
# Every task below is parented to it via --parent "$EPIC".
# Do not add dependency edges to or from this epic.
# ========================================

EPIC=$(bd create "Epic: Snapshot git HEAD commit on issue repo links" \
    -t epic \
    -p 0 \
    --label epic \
    --description "Add immutable creation-time commit metadata to bn_issue_repos so bn create records the exact git rev-parse HEAD SHA for the resolved repository context. Preserve topology-a repo auto-registration, existing repo resolution precedence, update/import/export compatibility, and JSON output stability." \
    --acceptance "bn_issue_repos has creation_commit TEXT NOT NULL DEFAULT ''; store and model surfaces hydrate CreationCommit; create captures full lowercase 40-character HEAD only when cwd repo identity matches the associated repo; update preserves existing values; export/import round-trip non-empty values; JSON includes repo.creation_commit with omitempty; normal gates pass." \
    --silent)
bd update "$EPIC" --status in_progress

# ========================================
# Phase 1: Analysis & Preparation
# ========================================

ANALYZE_REPO_RESOLUTION=$(bd create "Audit repo resolution and git detection paths for creation commit capture" \
    -t task \
    -p 0 \
    --label analysis \
    --parent "$EPIC" \
    --description "Review docs/specs/repo-resolution-precedence.md, docs/specs/topology-a-prefix-equals-slug.md, cmd/bn/git_resolver.go, cmd/bn/repo_resolve.go, cmd/bn/cmd_create.go, and cmd/bn/app.go. Document where cwd git root, remote.origin.url, synthesized file:// identity, .bn marker repo, and explicit --repo values are resolved today. Identify the exact place to decide whether cwd HEAD belongs to the repo being linked." \
    --acceptance "Notes or design comments identify the repo identity comparison inputs for plain auto-detect, .bn marker repo, explicit slug, explicit URL, local-only file:// repos, and prefix mismatch cases; existing precedence remains unchanged." \
    --silent)

ANALYZE_STORE_LINK_LIFECYCLE=$(bd create "Audit bn_issue_repos lifecycle and immutable metadata risks" \
    -t task \
    -p 0 \
    --label analysis \
    --parent "$EPIC" \
    --description "Review store/store.go IssueRepoInput, CreateIssue, UpdateIssue, insertIssueRepoGORM, populateIssueRepos, repoTargetFromIssueRepo, ImportIssuesFull, and store/gorm_models.go gormIssueRepo. Focus on the current UpdateIssue delete-and-reinsert behavior and import creation path so creation_commit cannot be accidentally cleared." \
    --acceptance "Implementation guidance identifies every store path that writes, replaces, reads, or exports bn_issue_repos and states how creation_commit should be supplied or preserved in each path." \
    --silent)

CHARACTERIZE_EXISTING_BEHAVIOR=$(bd create "Add characterization tests for current repo resolution and repo-link update behavior" \
    -t task \
    -p 1 \
    --label prep \
    --parent "$EPIC" \
    --description "Before changing behavior, add focused tests around cmd/bn/repo_resolve_test.go, cmd/bn/cmd_create_test.go, cmd/bn/cmd_auto_detect_test.go, and store/store_integration_test.go that lock current repo precedence, synthesized file:// auto-detect, explicit --repo behavior, and UpdateIssue repo retargeting semantics." \
    --acceptance "Tests fail only if existing repo resolution precedence or repo retargeting behavior changes unexpectedly; they do not yet require creation_commit." \
    --silent)
bd dep add "$CHARACTERIZE_EXISTING_BEHAVIOR" "$ANALYZE_REPO_RESOLUTION"
bd dep add "$CHARACTERIZE_EXISTING_BEHAVIOR" "$ANALYZE_STORE_LINK_LIFECYCLE"

# ========================================
# Phase 2: Schema, Store, and Domain Model
# ========================================

ADD_SCHEMA_MIGRATION=$(bd create "Add 0011 creation_commit migration for bn_issue_repos across sqlite postgres mysql" \
    -t task \
    -p 0 \
    --label migration \
    --parent "$EPIC" \
    --description "Create schema/migrations/sqlite/0011_bn_issue_repos_creation_commit.sql, schema/migrations/postgres/0011_bn_issue_repos_creation_commit.sql, and schema/migrations/mysql/0011_bn_issue_repos_creation_commit.sql. Each migration must add creation_commit TEXT NOT NULL DEFAULT '' to bn_issue_repos without breaking existing rows. Update schema/schema_test.go expectedMigrations and required DDL assertions for dialect parity." \
    --acceptance "Fresh and migrated schemas include bn_issue_repos.creation_commit with NOT NULL DEFAULT ''; schema tests cover migration 0011 for all dialects." \
    --silent)
bd dep add "$ADD_SCHEMA_MIGRATION" "$ANALYZE_STORE_LINK_LIFECYCLE"

EXTEND_STORE_MODEL=$(bd create "Thread CreationCommit through IssueRepoInput, gormIssueRepo, and model.RepoTarget" \
    -t task \
    -p 0 \
    --label impl \
    --parent "$EPIC" \
    --description "Update model/issue.go RepoTarget, store/store.go IssueRepoInput, store/gorm_models.go gormIssueRepo, insertIssueRepoGORM, populateIssueRepos, and repoTargetFromIssueRepo so creation_commit is stored and hydrated consistently from bn_issue_repos. Validate non-empty commits as full lowercase 40-character hex object IDs; reject symbolic refs, abbreviations, uppercase, and arbitrary strings at the store boundary." \
    --acceptance "CreateIssue with IssueRepoInput.CreationCommit persists and returns the value; GetIssue/ListIssues/ReadyIssues hydrate model.RepoTarget.CreationCommit; invalid non-empty values return a clear validation error before writing; empty string remains accepted for existing and non-git cases." \
    --silent)
bd dep add "$EXTEND_STORE_MODEL" "$ADD_SCHEMA_MIGRATION"

PRESERVE_UPDATE_COMMIT=$(bd create "Preserve creation_commit when UpdateIssue retargets repo routing" \
    -t task \
    -p 0 \
    --label impl \
    --parent "$EPIC" \
    --description "Update store/store.go UpdateIssue so bn update --repo, --ref, and --subdir preserve an existing bn_issue_repos.creation_commit when replacing the repo link unless the caller explicitly supplies a valid CreationCommit for a new imported row. Avoid accidental clearing caused by the current delete-and-reinsert flow." \
    --acceptance "Retargeting an issue with an existing creation_commit keeps the original value; updating ref/subdir also keeps it; missing existing link plus empty input still creates an empty creation_commit; invalid explicit values are rejected." \
    --silent)
bd dep add "$PRESERVE_UPDATE_COMMIT" "$EXTEND_STORE_MODEL"

ADD_STORE_TESTS=$(bd create "Cover creation_commit persistence, hydration, validation, and update immutability in store tests" \
    -t task \
    -p 1 \
    --label testing \
    --parent "$EPIC" \
    --description "Extend store/store_sqlite_contract_test.go, store/store_integration_test.go, and store/store_infra_test.go as appropriate. Cover CreateIssue persistence, GetIssue/ListIssues/ReadyIssues hydration, invalid commit validation, default empty commit for existing-style rows, and UpdateIssue preservation under retarget/ref/subdir changes. Include cross-dialect integration coverage for Postgres and MySQL where the existing integration harness supports it." \
    --acceptance "Store tests exercise sqlite contract behavior and integration behavior for creation_commit; UpdateIssue cannot regress by clearing the value; migration/schema coverage includes MySQL and Postgres DDL." \
    --silent)
bd dep add "$ADD_STORE_TESTS" "$PRESERVE_UPDATE_COMMIT"

# ========================================
# Phase 3: Git Capture and CLI Create
# ========================================

EXTEND_GIT_RESOLVER=$(bd create "Extend gitResolver seam to capture full lowercase HEAD commit best-effort" \
    -t task \
    -p 0 \
    --label impl \
    --parent "$EPIC" \
    --description "Update cmd/bn/git_resolver.go and cmd/bn/git_resolver_test.go with a HeadCommit(root string) (sha string, ok bool, err error) style method. The real resolver should run git rev-parse HEAD in the repo root and accept only full lowercase 40-character hex output. Collapse git-not-found, unborn HEAD, permission, and rev-parse failures to ok=false without disrupting create behavior." \
    --acceptance "Detached HEAD, dirty worktrees, merge/rebase states, linked worktrees, and submodules return exact HEAD when git rev-parse HEAD succeeds; unborn/outside/missing git cases return empty ok=false; fake resolver supports tests without shelling out." \
    --silent)
bd dep add "$EXTEND_GIT_RESOLVER" "$CHARACTERIZE_EXISTING_BEHAVIOR"

IMPLEMENT_COMMIT_CAPTURE=$(bd create "Capture cwd HEAD only when cwd repo identity matches the associated repo" \
    -t task \
    -p 0 \
    --label impl \
    --parent "$EPIC" \
    --description "Update cmd/bn/repo_resolve.go and cmd/bn/cmd_create.go so bn create supplies IssueRepoInput.CreationCommit only when cwd is inside a git repo and the normalized cwd remote URL, or synthesized file:///abs/toplevel URL for local-only repos, resolves to the same repo row selected by auto-detect, .bn marker, or explicit --repo. Do not error solely because commit capture fails." \
    --acceptance "Plain cwd auto-detect records HEAD by construction; .bn marker and explicit --repo record HEAD only on repo identity match; mismatched cwd repo creates the bean normally with empty creation_commit; local-only file:// repos record HEAD; non-git/unborn/missing git failures leave the field empty." \
    --silent)
bd dep add "$IMPLEMENT_COMMIT_CAPTURE" "$EXTEND_GIT_RESOLVER"
bd dep add "$IMPLEMENT_COMMIT_CAPTURE" "$EXTEND_STORE_MODEL"

ADD_CLI_CREATE_TESTS=$(bd create "Test bn create creation_commit capture across auto-detect marker explicit and mismatch cases" \
    -t task \
    -p 1 \
    --label testing \
    --parent "$EPIC" \
    --description "Extend cmd/bn/cmd_create_test.go, cmd/bn/cmd_auto_detect_test.go, cmd/bn/repo_resolve_test.go, and related fake git resolver tests. Cover auto-detect with remote origin, local-only synthesized file:// identity, .bn marker matching cwd repo, explicit --repo slug or URL matching cwd repo, explicit/marker mismatch leaving empty commit, invalid git output ignored, and prefix mismatch guard behavior." \
    --acceptance "CLI create tests assert stored Repo.CreationCommit and JSON repo.creation_commit for successful captures, and assert empty/omitted values when capture is not valid or repo identity does not match." \
    --silent)
bd dep add "$ADD_CLI_CREATE_TESTS" "$IMPLEMENT_COMMIT_CAPTURE"

# ========================================
# Phase 4: JSON, Export, and Import
# ========================================

UPDATE_JSON_OUTPUT=$(bd create "Expose creation_commit in stable CLI repo JSON with omitempty" \
    -t task \
    -p 1 \
    --label impl \
    --parent "$EPIC" \
    --description "Update cmd/bn/app.go repoTargetJSON and toIssueJSON so bn show --json, bn list --json, and bn ready --json include repo.creation_commit only when non-empty. Keep table output and non-JSON output unchanged." \
    --acceptance "JSON output includes creation_commit for new captured rows and omits it for empty existing rows; app_test.go covers show/list/ready JSON behavior without changing table output expectations." \
    --silent)
bd dep add "$UPDATE_JSON_OUTPUT" "$EXTEND_STORE_MODEL"

UPDATE_EXPORT_IMPORT=$(bd create "Round-trip repo routing metadata and creation_commit through bn export/import" \
    -t task \
    -p 0 \
    --label impl \
    --parent "$EPIC" \
    --description "Update cmd/bn/cmd_export.go and cmd/bn/cmd_import.go bdExportLine shape with an optional nested repo object. Export slug, remote_url, requested_ref, base_ref, work_branch, worktree_subdir, metadata, and non-empty creation_commit; include other RepoTarget fields if keeping parity with toIssueJSON is simpler. Import should resolve repo.remote_url by auto-registering, or repo.slug only if already registered in the target prefix. Reject a line that has creation_commit without resolvable repo identity instead of silently dropping it." \
    --acceptance "Older JSONL without repo or creation_commit imports unchanged; non-empty creation_commit exports and imports into a new repo link; creation_commit without resolvable remote_url or registered slug returns a clear import error; import validates commit format." \
    --silent)
bd dep add "$UPDATE_EXPORT_IMPORT" "$EXTEND_STORE_MODEL"
bd dep add "$UPDATE_EXPORT_IMPORT" "$UPDATE_JSON_OUTPUT"

ADD_JSON_IMPORT_EXPORT_TESTS=$(bd create "Test creation_commit in CLI JSON output and export/import round trips" \
    -t task \
    -p 1 \
    --label testing \
    --parent "$EPIC" \
    --description "Extend cmd/bn/app_test.go, cmd/bn/cmd_export_test.go, and cmd/bn/cmd_import_test.go. Cover omitempty behavior, export nested repo payloads with creation_commit, older export compatibility, import preservation via repo.remote_url auto-registration, import via existing repo.slug, and hard failure when creation_commit is present but no repo target can be resolved." \
    --acceptance "Tests demonstrate bn show/list/ready JSON, bn export, and bn import all preserve non-empty creation_commit and remain compatible with older files that omit it." \
    --silent)
bd dep add "$ADD_JSON_IMPORT_EXPORT_TESTS" "$UPDATE_EXPORT_IMPORT"

# ========================================
# Phase 5: Documentation and Validation
# ========================================

UPDATE_DOCS=$(bd create "Document creation-time commit snapshots in repo routing specs and README" \
    -t task \
    -p 2 \
    --label docs \
    --parent "$EPIC" \
    --description "Update docs/specs/repo-resolution-precedence.md, docs/specs/topology-a-prefix-equals-slug.md, and README.md to explain that issue repo routing snapshots the exact creation-time HEAD commit on bn_issue_repos when cwd repo identity matches, and that dirty state is intentionally not recorded." \
    --acceptance "Docs describe capture rules for auto-detect, .bn marker, explicit --repo, local-only repos, mismatches, and failure-to-capture cases; docs make clear creation_commit is immutable issue metadata, not repo registry state." \
    --silent)
bd dep add "$UPDATE_DOCS" "$IMPLEMENT_COMMIT_CAPTURE"
bd dep add "$UPDATE_DOCS" "$UPDATE_EXPORT_IMPORT"

RUN_QUALITY_GATES=$(bd create "Run creation_commit feature quality gates and fix regressions" \
    -t task \
    -p 0 \
    --label testing \
    --parent "$EPIC" \
    --description "Run make test, make vet, make lint, and go test -tags=integration ./... when Docker-backed testcontainers are available. Fix any regressions caused by the creation_commit feature. Pay special attention to schema migration parity, sqlite contract tests, Postgres/MySQL integration tests, and CLI JSON golden expectations." \
    --acceptance "make test, make vet, and make lint pass; integration tests pass or are explicitly documented as skipped because Docker/testcontainers are unavailable; no unrelated worktree changes are included." \
    --silent)
bd dep add "$RUN_QUALITY_GATES" "$ADD_STORE_TESTS"
bd dep add "$RUN_QUALITY_GATES" "$ADD_CLI_CREATE_TESTS"
bd dep add "$RUN_QUALITY_GATES" "$ADD_JSON_IMPORT_EXPORT_TESTS"
bd dep add "$RUN_QUALITY_GATES" "$UPDATE_DOCS"

echo ""
echo "Bead graph created."
echo "Epic: $EPIC"
echo ""
echo "Inspect with:"
echo "  bd show $EPIC"
echo "  bd children $EPIC"
echo "  bd ready"
