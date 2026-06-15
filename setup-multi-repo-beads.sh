#!/bin/bash
# Project: beans (bn)
# Change: Add multi-repository support — cwd→repo auto-detect, auto-register,
#         per-repo ID prefixes (topology option (a): prefix == repo slug),
#         repo-scoped reads with --repo override and --all-repos escape hatch.
# Generated: 2026-06-15
#
# Topology is PRE-MADE: option (a), each auto-registered repo gets its own
# bn_projects row where prefix == repo slug, so all existing prefix-scoped
# queries keep working unchanged. (Verified against source: ListIssues,
# ReadyIssues, ListDeps, ListMembers, ListParents all filter on bn_issues.prefix;
# generateID(prefix) builds the ID from prefix; bn_issues.prefix is
# NOT NULL REFERENCES bn_projects(prefix).)
#
# Critical path is SERIAL, do NOT fan out everything after analysis:
#   topology decision -> NormalizeRemoteURL + 0009 migration ->
#   GetRepoByRemoteURL + auto-register -> git-resolver seam ->
#   store.go scoping (single-writer chain) -> per-command adoption (fans out) ->
#   tests -> docs/verify.
# cmd/bn/app.go and store/store.go are file-contention hotspots: edits to each
# are chained, not parallel siblings.

set -e

# Initialize beads if needed (this repo already has .beads/, so this is a no-op).
if [ ! -d ".beads" ]; then
    bd init
fi

echo "Creating multi-repo support beads..."

# ========================================
# Phase 0: Analysis & Decisions (-p 0 blockers)
# ========================================

DECIDE_TOPOLOGY=$(bd create "Decision record: confirm topology option (a) prefix==repo-slug against source" \
  -d "Deliverable: a short decision record (docs/adr/ or docs/specs/) confirming topology option (a) — each auto-registered repo gets its own bn_projects row where prefix == repo slug — against current source, and enumerating every store.go query site it touches. Confirm: generateID(prefix string) in store/store.go:1710 builds 'prefix+\"-\"+hex'; ListFilter (store/store.go:281) has only Prefix/States/Limit; ListIssues (store.go:288), ReadyIssues (store.go:333), ListDeps (store.go:687), ListMembers (store.go:739), ListParents (store.go:773) all filter on bn_issues.prefix; bn_issues.prefix is NOT NULL REFERENCES bn_projects(prefix) (schema/migrations/sqlite/0001_bn_init.sql:15); bn_repos.prefix REFERENCES bn_projects(prefix) with UNIQUE(prefix,slug). Deviate to option (b) ONLY if a named blocker is found and recorded here. This bead BLOCKS all implementation beads." \
  -p 0 --label analysis -t task --silent)

DECIDE_PRECEDENCE=$(bd create "Decision record: repo resolution precedence + edge-case behavior" \
  -d "Specify the exact resolution precedence and edge-case semantics that cmd/bn/app.go will implement. Current chain is resolveProjectPrefix (app.go:166) -> --project flag -> BN_PROJECT env -> .bn marker (parseActiveProjectConfig app.go:324, activeProjectMarkerPath app.go:257). New chain must define where cwd git auto-detect and --repo sit, e.g.: --repo flag > cwd git auto-detect > .bn marker repo > auto-register/error. Define explicitly: (1) interaction between auto-detect and an EXISTING .bn marker; (2) behavior OUTSIDE any git repo; (3) local-only repo with NO remote (slug falls back to git-root basename); (4) nested/submodule repos where git rev-parse --show-toplevel resolves to the inner repo; (5) bare/detached repos. Each must have a clear message and must not crash or silently write to the wrong repo. BLOCKS app.go plumbing." \
  -p 0 --label analysis -t task --silent)
bd dep add $DECIDE_PRECEDENCE $DECIDE_TOPOLOGY

DECIDE_MEMORY_INIT=$(bd create "Decision record: remember/memories scoping + init semantics under topology (a)" \
  -d "Two semantic decisions, since both flow through prefix today. (1) remember/memories (cmd/bn/cmd_memory.go) are prefix-scoped today (rs.prefix at cmd_memory.go:29 and filter at :100; --global / --all escape hatches exist), so under topology (a) they become per-repo automatically — DECIDE and record whether memories should be per-repo or global, and how --global/--all map. (2) init (newInitCmd, app.go:46): decide whether it still takes --prefix or whether non-interactive auto-register subsumes it. Record both decisions; they block the cmd_memory.go and init adoption beads." \
  -p 1 --label analysis -t task --silent)
bd dep add $DECIDE_MEMORY_INIT $DECIDE_TOPOLOGY

# ========================================
# Phase 1: Net-new primitives (foundations)
# ========================================

NORMALIZE_URL=$(bd create "Add NormalizeRemoteURL() canonicalizer in repo/validation.go (net-new)" \
  -d "ADD a NEW NormalizeRemoteURL(remote string) (string, error) in repo/validation.go. No URL canonicalizer exists today (only ValidateRemoteURL at validation.go:63 validates; NormalizeCloneStrategy/NormalizeDefaultBranch cover other fields; isSCPRemote at :202 handles git@host:org/repo separately from url.Parse). Unify the SCP, ssh:// and https forms of the SAME repo into ONE canonical key: lowercase host, strip .git suffix, strip userinfo, strip default ports, unify git@host:org/repo <-> ssh://git@host/org/repo <-> https://host/org/repo. Specify the canonical form precisely. Include unit tests proving all three forms of one repo collapse to one key, on BOTH write and read sides. This key is what the 0009 unique index and GetRepoByRemoteURL match on." \
  -p 0 --label impl -t task --silent)
bd dep add $NORMALIZE_URL $DECIDE_TOPOLOGY

MIGRATION_0009=$(bd create "Add 0009 migration: unique index on normalized remote_url (all 3 drivers, atomic)" \
  -d "ADD migration 0009_* to schema/migrations/{sqlite,postgres,mysql}/ (head is 0008_bn_dep_type.sql, identical set across all three drivers — verified). Carries at minimum a UNIQUE index on the normalized remote_url for bn_repos (today bn_repos has UNIQUE(prefix,slug) only, no uniqueness on remote_url; remote_url is TEXT NOT NULL per 0004_bn_repos.sql:13). The stored remote_url must be the NormalizeRemoteURL canonical form so the unique index actually dedupes the SCP/https/ssh variants. Greenfield: no data migration needed; additive via Goose. SINGLE ATOMIC BEAD across all three drivers — serialization point, NOT parallelizable per-driver." \
  -p 0 --label migration -t task --silent)
bd dep add $MIGRATION_0009 $DECIDE_TOPOLOGY
bd dep add $MIGRATION_0009 $NORMALIZE_URL

# ========================================
# Phase 2: Store lookup-by-remote + non-interactive auto-register
# ========================================

GET_REPO_BY_REMOTE=$(bd create "Add GetRepoByRemoteURL() to store/repo_store.go (net-new lookup-by-remote)" \
  -d "ADD a NEW GetRepoByRemoteURL(ctx, normalizedRemoteURL string) (Repo, error) in store/repo_store.go. No lookup-by-remote exists today (only GetRepoBySlug at repo_store.go:411 keyed on prefix+slug, and ResolveRepoAlias at :420). Query bn_repos by the normalized remote_url (the unique-indexed column from 0009). Return a clear not-found error so callers can fall through to auto-register. Add unit/contract test coverage (sqlite in-memory) that a repo registered under any URL form is found by the normalized key from every other form." \
  -p 0 --label impl -t task --silent)
bd dep add $GET_REPO_BY_REMOTE $NORMALIZE_URL
bd dep add $GET_REPO_BY_REMOTE $MIGRATION_0009

AUTO_REGISTER=$(bd create "Add non-interactive auto-register entry point in store/repo_store.go" \
  -d "ADD a non-interactive auto-register path: given a git working dir's normalized remote URL + derived slug, create the bn_projects row where prefix == slug (topology (a)) AND the bn_repos row in one transaction, then return the Repo. Reuse CreateRepo (repo_store.go:211) / CreateRepoInput and cleanRepoSlug/repoSlugRE (defined in cmd/bn/cmd_repo.go:428/:19 — extract/share as needed). Derive slug from the normalized remote; fall back to git-root basename only when there is no remote (local-only). MUST be non-interactive (scripted/CI safe). Define behavior when a .bn marker is already present. CRITICAL: detect and DISAMBIGUATE slug collisions across DISTINCT remotes (e.g. me/app and you/app both deriving 'app') rather than silently merging into one prefix (success criterion 5). Add contract tests for the collision case." \
  -p 0 --label impl -t task --silent)
bd dep add $AUTO_REGISTER $GET_REPO_BY_REMOTE
bd dep add $AUTO_REGISTER $DECIDE_TOPOLOGY

# ========================================
# Phase 3: Git-resolver seam (net-new, testability-critical)
# ========================================

GIT_SEAM=$(bd create "Add injectable git-resolver seam (toplevel + remote.origin.url)" \
  -d "ADD a NEW git-resolver seam (interface or function var, injectable in tests) in cmd/bn/ that reads 'git config --get remote.origin.url' and the git toplevel (mirror existing gitRoot at app.go:367 which returns (root, bool, error)). No git mocking exists today — cmd/bn unit tests call cobra RunE directly and shell out for real. The seam must let tests inject fake remote/toplevel WITHOUT cd-ing the test process. Provide a real implementation (shells out) and a fake for tests. This is consumed by app.go resolution and cmd_create auto-detect." \
  -p 0 --label impl -t task --silent)
bd dep add $GIT_SEAM $DECIDE_TOPOLOGY

# ========================================
# Phase 4: store.go scoping — SINGLE-WRITER CHAIN (do not parallelize)
# ========================================

STORE_CREATE_CONTRACT=$(bd create "Redefine create contract: resolve repo first, derive prefix from repo (store/store.go)" \
  -d "store/store.go single-writer hotspot — edit as a tight chain. Today insertIssueRepoGORM (store.go:1284) calls getRepoBySlugGORM(ctx,tx,prefix,slug) (store.go:1539) resolving the repo from the ISSUE's prefix. Invert it: resolve the repo FIRST (by normalized remote / slug via the auto-register path) and derive the issue prefix FROM the repo, so prefix==slug holds for created issues. Update CreateIssue/CreateIssueInput (store.go:107) and IssueRepoInput (store.go:121) wiring accordingly. generateID(prefix) (store.go:1710) needs NO change under topology (a) — confirm and note. Keep populateIssueRepos (store.go:1339) behavior. Add contract test: creating in two distinct repos yields two distinct prefixes that do not collide." \
  -p 0 --label impl -t task --silent)
bd dep add $STORE_CREATE_CONTRACT $AUTO_REGISTER

STORE_LIST_FILTER=$(bd create "Add repo field to ListFilter and apply in ListIssues/ReadyIssues (store/store.go)" \
  -d "store/store.go chain (after STORE_CREATE_CONTRACT — same file, serialize). Add a RepoSlug/RepoID field to ListFilter (store.go:281, currently Prefix/States/Limit only) and apply the repo filter in ListIssues (store.go:288) and ReadyIssues (store.go:333). Under topology (a) prefix==slug, so the existing WHERE prefix = ? already scopes by repo; the net-new work is plumbing an explicit repo selector through ListFilter for the override path and verifying it composes with the existing prefix filter. Add contract tests for default-scope and override." \
  -p 1 --label impl -t task --silent)
bd dep add $STORE_LIST_FILTER $STORE_CREATE_CONTRACT

STORE_DEP_SCOPING=$(bd create "Apply repo scoping to ListDeps/ListMembers/ListParents (store/store.go)" \
  -d "store/store.go chain (after STORE_LIST_FILTER — same file, serialize). Ensure the dep/membership listings ListDeps (store.go:687), ListMembers (store.go:739), ListParents (store.go:773) honor repo scoping consistently with ListIssues. They already filter on i.prefix; confirm topology (a) makes them repo-scoped for free and add the explicit repo selector where the override path needs it. Add contract tests." \
  -p 1 --label impl -t task --silent)
bd dep add $STORE_DEP_SCOPING $STORE_LIST_FILTER

# ========================================
# Phase 5: app.go resolution plumbing — CONTENTION HOTSPOT (chain)
# ========================================

APP_RESOLUTION=$(bd create "Wire cwd git auto-detect + --repo override + precedence in cmd/bn/app.go" \
  -d "cmd/bn/app.go contention hotspot — chain edits, reserve the file. Implement the precedence chain from the DECIDE_PRECEDENCE record: --repo flag > cwd git auto-detect (via git seam) > .bn marker repo > auto-register/error. Add a --repo persistent/command flag plumbing alongside existing --project/--actor/--json (app.go:41-43); extend appState (app.go:20, currently prefix/actor/jsonOut/store) and resolveProjectPrefix (app.go:166) / parseActiveProjectConfig (app.go:324) to incorporate repo resolution; call the auto-register entry point when a valid git repo is unregistered. Handle all edge cases from the precedence record (outside-repo, local-only, nested/submodule, bare). Reserve cmd/bn/app.go for this bead." \
  -p 0 --label impl -t task --silent)
bd dep add $APP_RESOLUTION $GIT_SEAM
bd dep add $APP_RESOLUTION $AUTO_REGISTER
bd dep add $APP_RESOLUTION $DECIDE_PRECEDENCE

# ========================================
# Phase 6: Per-command adoption — THIS LAYER FANS OUT (independent cmd_*.go files)
# ========================================

CMD_CREATE=$(bd create "cmd_create.go: git auto-detect + auto-register on create" \
  -d "cmd/bn/cmd_create.go: newCreateCmd (cmd_create.go:12) currently reads --repo/marker only and calls cleanRepoSlug (cmd_create.go:56) with NO git-remote auto-detection — this is net-new. Add: when running inside a valid git repo with no explicit --repo, auto-detect the repo via the git seam and auto-register if unseen, then create the issue recorded against that repo. --repo (cmd_create.go:110) overrides and takes precedence. Verify via bn show / JSON (toIssueJSON app.go:419 / model RepoTarget) that the issue records the repo. Reserve cmd_create.go." \
  -p 1 --label impl -t task --silent)
bd dep add $CMD_CREATE $APP_RESOLUTION
bd dep add $CMD_CREATE $STORE_CREATE_CONTRACT

CMD_LIST=$(bd create "cmd_list.go: repo-scoped default + --repo override + --all-repos escape hatch" \
  -d "cmd/bn/cmd_list.go: newListCmd (cmd_list.go:14) defaults to repo-scoped output (the cwd repo) using the new ListFilter repo field. Add --repo override and a DISTINCT --all-repos escape hatch for cross-repo listing. DO NOT conflate with the pre-existing --all (cmd_list.go:89) which means page-cap override — keep both, distinct semantics. Reserve cmd_list.go." \
  -p 1 --label impl -t task --silent)
bd dep add $CMD_LIST $APP_RESOLUTION
bd dep add $CMD_LIST $STORE_LIST_FILTER

CMD_READY_DEP=$(bd create "ready + dep/members/parents listings: default repo-scope + --repo + --all-repos" \
  -d "Apply the same default-scope-by-repo + --repo override + --all-repos escape hatch to the list-style commands beyond list: newReadyCmd (app.go:48) and the dep/membership LISTINGS (newDepCmd app.go:54 listing path, members, parents). Use the ListFilter repo field and the scoped ListDeps/ListMembers/ListParents. Mirror cmd_list.go flag conventions. These are genuine repo filtering (not ID-addressed)." \
  -p 1 --label impl -t task --silent)
bd dep add $CMD_READY_DEP $APP_RESOLUTION
bd dep add $CMD_READY_DEP $STORE_DEP_SCOPING

CMD_ID_ADDRESSED=$(bd create "show/update/close/delete: VALIDATE by repo, do NOT silently filter" \
  -d "cmd/bn/cmd_show.go, cmd_update.go, cmd_close.go, cmd_delete.go are ID-addressed: GetIssue looks up by primary-key id and the fully-qualified id already encodes the repo prefix (e.g. app-a1b2), so 'bn show app-a1b2' is unambiguous regardless of cwd. DO NOT repo-filter these — filtering would break legitimate cross-repo lookups. At most accept --repo/auto-detect for UX consistency and VALIDATE that the requested id belongs to the resolved repo (warn/error on mismatch), never silently drop. Add tests proving cross-repo id lookup still works from any cwd." \
  -p 1 --label impl -t task --silent)
bd dep add $CMD_ID_ADDRESSED $APP_RESOLUTION

CMD_MEMORY=$(bd create "remember/memories: implement decided per-repo-vs-global scoping" \
  -d "cmd/bn/cmd_memory.go: implement the scoping decided in DECIDE_MEMORY_INIT. remember (cmd_memory.go:12) uses rs.prefix (:29); memories (cmd_memory.go:67) filters on rs.prefix (:100) with an existing --all (:141) for cross-project. Wire repo resolution so behavior matches the decision record and keep --global/--all mapping consistent with the rest of the CLI. Reserve cmd_memory.go." \
  -p 2 --label impl -t task --silent)
bd dep add $CMD_MEMORY $APP_RESOLUTION
bd dep add $CMD_MEMORY $DECIDE_MEMORY_INIT

CMD_INIT=$(bd create "init: implement decided semantics (--prefix vs auto-register subsumes)" \
  -d "cmd/bn/ newInitCmd (registered app.go:46): implement the init semantics decided in DECIDE_MEMORY_INIT — whether it still takes --prefix or whether non-interactive auto-register subsumes it. prime (newPrimeCmd app.go:60) skips DB init and needs NO change. Update help text/usage to match." \
  -p 2 --label impl -t task --silent)
bd dep add $CMD_INIT $APP_RESOLUTION
bd dep add $CMD_INIT $DECIDE_MEMORY_INIT

CMD_EXPORT_IMPORT=$(bd create "export/import: repo scoping + per-repo-prefix import-conflict logic" \
  -d "cmd/bn/cmd_export.go (newExportCmd app.go:56) and cmd_import.go (newImportCmd app.go:57): apply repo scoping consistent with list-style commands for export, and update the import-conflict logic which filters on prefix so it behaves correctly now that prefix==repo-slug (per-repo prefixes). Verify import of issues from a different repo prefix does not false-conflict. Add coverage." \
  -p 2 --label impl -t task --silent)
bd dep add $CMD_EXPORT_IMPORT $APP_RESOLUTION
bd dep add $CMD_EXPORT_IMPORT $STORE_LIST_FILTER

# ========================================
# Phase 7: Testing
# ========================================

TEST_GIT_SEAM=$(bd create "Tests: git fixtures (git init + git remote add in t.TempDir) + auto-detect/register" \
  -d "cmd/bn tests: build fixture repos with git init + git remote add in t.TempDir() and/or use the injected git seam (no cd-ing the test process). Cover: auto-detect from a valid repo, auto-register-on-first-use, --repo override precedence over auto-detect, and that the same repo under SCP/https/ssh remote forms resolves to ONE prefix (exercises NormalizeRemoteURL on the read side)." \
  -p 1 --label testing -t task --silent)
bd dep add $TEST_GIT_SEAM $GIT_SEAM
bd dep add $TEST_GIT_SEAM $CMD_CREATE

TEST_STORE_SCOPING=$(bd create "Tests: store-layer repo scoping (testcontainers PG16/MySQL8.4 + sqlite contract)" \
  -d "Store tests across all backends: integration tests behind //go:build integration (run with go test -tags integration) use testcontainers Postgres 16 + MySQL 8.4 (store/store_integration_test.go: testPostgresDSN, testMySQLDSN); sqlite uses the in-memory contract path (store_sqlite_contract_test.go: sqliteMemoryDSN). Cover: create in two distinct repos -> distinct non-colliding prefixes (criterion 5); slug-collision disambiguation across distinct remotes; ListFilter repo default-scope + override; ListDeps/ListMembers/ListParents scoping; GetRepoByRemoteURL normalized-key matching; 0009 unique index enforced." \
  -p 1 --label testing -t task --silent)
bd dep add $TEST_STORE_SCOPING $STORE_DEP_SCOPING
bd dep add $TEST_STORE_SCOPING $AUTO_REGISTER

TEST_CMD_E2E=$(bd create "Tests: cmd/bn end-to-end for create/list/ready/show/update/close/delete + edge cases" \
  -d "cmd/bn end-to-end tests (cobra RunE directly + git fixtures/seam). Cover success criteria 1-6: create records repo with no flag/no prior registration; list/ready default to cwd repo; --repo override; --all-repos cross-repo; ID-addressed commands remain cross-repo addressable and do NOT silently filter; and the edge cases from DECIDE_PRECEDENCE — outside any git repo, local-only repo with no remote, nested/submodule (git rev-parse --show-toplevel inner), bare/detached — each with a clear message and no crash / no wrong-repo write." \
  -p 1 --label testing -t task --silent)
bd dep add $TEST_CMD_E2E $CMD_CREATE
bd dep add $TEST_CMD_E2E $CMD_LIST
bd dep add $TEST_CMD_E2E $CMD_READY_DEP
bd dep add $TEST_CMD_E2E $CMD_ID_ADDRESSED

TEST_MEMORY_INIT=$(bd create "Tests: remember/memories scoping + init semantics" \
  -d "Test the decided remember/memories per-repo-vs-global behavior (cmd_memory.go) and the decided init semantics, matching the DECIDE_MEMORY_INIT record. Verify --global/--all mapping and that memories scope as decided across two repos." \
  -p 2 --label testing -t task --silent)
bd dep add $TEST_MEMORY_INIT $CMD_MEMORY
bd dep add $TEST_MEMORY_INIT $CMD_INIT

TEST_EXPORT_IMPORT=$(bd create "Tests: export/import repo scoping + import-conflict under per-repo prefixes" \
  -d "Verify export is repo-scoped consistent with list, and import-conflict logic does not false-conflict across distinct repo prefixes (prefix==repo-slug). Round-trip export then import for two repos." \
  -p 2 --label testing -t task --silent)
bd dep add $TEST_EXPORT_IMPORT $CMD_EXPORT_IMPORT

# ========================================
# Phase 8: Docs & final verification gate
# ========================================

DOCS=$(bd create "Docs: README + setup-beads.sh demonstrate the multi-repo workflow" \
  -d "Update README (and setup-beads.sh end-to-end where relevant) to demonstrate the multi-repo workflow: running bn create/list from any git repo, auto-register, --repo override, --all-repos, per-repo prefixes, and the documented edge-case behaviors. Match existing doc conventions." \
  -p 2 --label docs -t task --silent)
bd dep add $DOCS $CMD_CREATE
bd dep add $DOCS $CMD_LIST
bd dep add $DOCS $CMD_READY_DEP
bd dep add $DOCS $CMD_ID_ADDRESSED

VERIFY=$(bd create "Final verification gate: make test (+ -tags integration), go vet, golangci-lint clean" \
  -d "Run the full gate and report results honestly: 'go test ./...' AND the Docker-backed integration suite via 'go test -tags integration ./...' (testcontainers Postgres 16 + MySQL 8.4) AND the in-memory sqlite contract tests; go vet and golangci-lint clean for the changed surface (Makefile ci target chains tidy-check vet lint test build). All success criteria 1-7 demonstrably pass. This is the exit gate — depends on every test and docs bead." \
  -p 0 --label testing -t task --silent)
bd dep add $VERIFY $TEST_GIT_SEAM
bd dep add $VERIFY $TEST_STORE_SCOPING
bd dep add $VERIFY $TEST_CMD_E2E
bd dep add $VERIFY $TEST_MEMORY_INIT
bd dep add $VERIFY $TEST_EXPORT_IMPORT
bd dep add $VERIFY $DOCS

echo ""
echo "Multi-repo task graph created. View with:"
echo "  bd ready      # unblocked tasks (should show the Phase 0 decision beads)"
echo "  bd dep tree   # full dependency tree"
echo "  bd dep cycles # confirm no cycles"
