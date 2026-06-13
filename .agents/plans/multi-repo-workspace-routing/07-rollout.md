# Rollout Plan

## Phase 0: Grounding

Create beads for the implementation epics before code work:

- Repo registry schema and store.
- `bn repo` CLI.
- Issue repo metadata.
- Runtime repo-aware tracker reads.
- Workspace checkout router.
- Repo authorization, host allowlist, and auth-ref resolution.
- Host deployment docs.
- E2E multi-repo test.

## Phase 1: Repo Registry Store

Implement:

- `bn_repos` migration.
- `bn_repo_aliases` migration.
- `bn_repo_audit` and `bn_project_admins` migrations.
- Store methods:
  - `CreateRepo`
  - `UpdateRepo`
  - `ListRepos`
  - `GetRepoBySlug`
  - `ResolveRepoAlias`
  - `DisableRepo`
  - `AuthorizeRepoAdmin`
  - `InsertRepoAudit`
  - `AddRepoAdmin`
  - `ListRepoAdmins`
- Unit and integration tests with testcontainers Postgres.

Done when repo rows can be created, listed, updated, disabled, resolved by
alias, authorized by project admin, and audited with redacted old/new values.

## Phase 2: `bn repo` Commands

Implement:

- `bn repo add`
- `bn repo list`
- `bn repo show`
- `bn repo update`
- `bn repo remove`
- `bn repo doctor`
- `bn repo admin add/list/remove`

Extend `.bn` marker read/write with optional `repo=` and `remote=`.

Done when a user can run `bn repo add` from `~/git/boxy`, then `bn create`
from that checkout and have repo inferred. Repo mutation commands must enforce
project-admin authorization.

## Phase 3: Issue Repo Metadata

Implement:

- `bn_issue_repos` migration.
- `CreateIssueInput` repo fields.
- `UpdateIssueInput` repo fields.
- `bn create --repo/--ref/--subdir`.
- `bn update --repo/--ref/--subdir`.
- `bn show` repo display.
- JSON output repo object.
- Transactional create/update paths that cannot leave an issue without its
  intended repo link.

Done when issues carry structured repo targets and old repo-less issues remain
valid.

## Phase 4: Tracker Adapter and Core Types

Implement:

- `core.RepoTarget`.
- Tracker Postgres adapter joins issue repo metadata.
- Ready/list/show paths expose repo metadata.
- Redacted logging.

Done when the orchestrator can see repo metadata without separate DB queries in
the dispatch hot path.

## Phase 5: Workspace Router

Implement:

- Repo resolver interface.
- Git mirror cache.
- Per-attempt checkout preparation.
- Ref resolution and revision snapshot.
- Workspace retention defaults.
- Error categories for repo failures.
- Runtime URL allowlist validation.
- Per-command `auth_ref` to git environment resolution.
- Repo/attempt-aware cleanup replacing current issue-keyed removal.

Done when a unit test can route two issues to two different repos and prepare
separate checkouts under one workspace root.

## Phase 6: Runtime Integration

Implement:

- Runtime builds repo registry/router for Postgres tracker mode.
- Dispatch creates a run-attempt row, then prepares workspace before worker run.
- Worker receives both checkout root and cwd from prepared workspace.
- Run attempts persist repo snapshot fields.
- Prompt context includes repo/workspace fields after `core.RenderContext` and
  template validation are extended.

Done when an integration test dispatches one issue for `repo-a` and one for
`repo-b`, and each agent command runs in the right checkout.

## Phase 7: Operator Deployment

Implement docs and examples:

- `WORKFLOW.infra.example.md` using `tracker.kind: postgres`.
- Compose override for infra host.
- Git credential secret mounting.
- `bn repo doctor`.
- Runbook for onboarding `clckr`, `boxy`, and `shady`.

Done when a fresh infra host can be started and repos can be onboarded from the
Mac without editing `WORKFLOW.md`. The documented sequence is: install git
secrets, configure known hosts and host allowlist, run `bn repo add`, run
`bn repo doctor --from-orchestrator`, then create issues.

## Phase 8: Hardening

Add:

- Workspace GC command.
- Optional PR branch push.
- Metrics:
  - repo checkout duration by outcome.
  - repo fetch failures by category.
  - workspace prepare failures by category.
