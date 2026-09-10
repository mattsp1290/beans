# Monorepo consolidation — overview

Status: Ready for implementation.
Implementation has not occurred. This directory contains planning documents only.
No repository file outside `.agents/plans/monorepo-consolidation/` has been changed by this plan.

## Requested outcome

Turn the `beans` repository at `github.com/mattsp1290/beans` into a monorepo that
contains both the existing `beans` Go library/CLI and the contents of the separate
`bean-counter` repository at `github.com/mattsp1290/bean-counter`. One additional
Postgres-backed application will join the monorepo later. This plan delivers the
two-component monorepo and defines the slot the third application will occupy. It
does not create the third application.

## Application context

```json
{
  "application_context": {
    "has_active_users": false,
    "backward_compatibility_required": false,
    "feature_flags": "not-applicable",
    "confirmation_digest": "bf0ab5d0b701c674238d475967363f2db71bb452c94f3c6810e161762c715cc0",
    "confirmed_at": "2026-09-10T04:00:10Z"
  }
}
```

The user confirmed that neither repository has external users or consumers, and that
the migration is free to break the `github.com/mattsp1290/beans` import path, the
`v0.1.0` and `v0.1.1` tags, and bean-counter's deploy artifact paths. Because both
booleans are false, feature flags are `not-applicable`, and no work package in this
plan carries a feature-flag gate.

Two consequences follow and are treated as accepted, not as risks to mitigate:

1. `go get github.com/mattsp1290/beans` stops working. The library moves to
   `github.com/mattsp1290/beans/libs/beans` (see [01-target-layout-and-module-graph.md](01-target-layout-and-module-graph.md)).
2. The existing `v0.1.0` and `v0.1.1` tags stop resolving to a Go module. They remain
   as git tags and are not deleted.

## Change type and affected areas

Inferred change type: repository restructure and build-system migration. No product
behavior changes.

Affected areas:

| Area | Effect |
| --- | --- |
| Go module graph | Two modules relocated, both module paths renamed, workspace added |
| Git history | bean-counter history imported into the beans repository |
| Build tooling | Root and per-module Makefiles, `LDFLAGS` version path, lint local-prefix |
| CI | Two workflow files merged into three path-scoped workflows |
| Containers and deploy | Docker build contexts, compose file paths, `scripts/deploy-production.sh` gates |
| Issue tracking | Two Beads Dolt databases consolidated into one |
| Agent configuration | `AGENTS.md`, `CLAUDE.md`, `.gitignore`, `.claude/settings.json` reconciled |

## Success criteria

The migration is complete when all of the following are observably true on a single
commit of `github.com/mattsp1290/beans`:

1. `go build ./...` succeeds in `libs/beans` and in `apps/bean-counter`, both with the
   workspace active and with `GOWORK=off`.
2. `go test ./...` passes in both modules.
3. `go vet ./...` and `golangci-lint run ./...` are clean in both modules.
4. `git log --follow -- libs/beans/store/store.go` shows commits authored before the
   restructure.
5. `git log -- apps/bean-counter/internal/server/app.go` shows commits authored in the
   bean-counter repository before the import, at the `apps/bean-counter/` path.
6. `docker build -f apps/bean-counter/Dockerfile .` from the repository root produces a
   runnable `bean-counter` image.
7. `bd stats` at the repository root reports at least 150 total issues and at least 7
   open issues, and `bd show bean-counter-m0p` resolves.
8. `bash apps/bean-counter/test/scripts/deploy-production_test.sh` passes, and
   `shellcheck apps/bean-counter/scripts/deploy-production.sh apps/bean-counter/test/scripts/deploy-production_test.sh`
   is clean.
9. `scripts/deploy-production.sh --dry-run` prints a plan whose compose path, repo
   directory, and image build contexts all reflect the monorepo layout.
10. `git status --porcelain` is empty at the repository root.

Criterion 7 uses "at least" because closed-issue counts can move if work happens
between planning and execution. The exact pre-migration counts are recorded in
[06-beads-and-agent-config.md](06-beads-and-agent-config.md) and must be re-measured
immediately before the import.

## Scope

In scope:

- Relocating the `beans` library and `bn` CLI to `libs/beans/`.
- Importing the bean-counter repository, with history, to `apps/bean-counter/`.
- Renaming both Go module paths and rewriting every import.
- Adding a tracked `go.work` and a `replace` directive for the intra-repo dependency.
- Reconciling root-level files that collide between the two repositories.
- Merging CI into path-scoped workflows.
- Repointing Docker build contexts, compose files, and the production deploy script.
- Consolidating the two Beads trackers into one at the repository root.
- Defining the directory and module conventions the third application will follow.

Out of scope:

- Creating, scaffolding, or designing the third Postgres application.
- Any change to bean-counter's HTTP API, its Svelte frontend behavior, or the beans
  store/schema packages.
- Performing the first production deploy of bean-counter. That work is tracked
  separately as `bean-counter-m0p` and is a consumer of this migration, not part of it.
- Archiving or deleting the `github.com/mattsp1290/bean-counter` GitHub repository.
  This plan stops at leaving it read-only-by-convention; see the cutover gate below.
- Migrating any Dolt remote to a new hosting location.

## Repository-grounded findings

These findings are verified against the working trees at `$HOME/git/beans` and
`$HOME/git/bean-counter` and drive the design.

1. **bean-counter depends on beans as a published module.** `go.mod` in bean-counter
   requires `github.com/mattsp1290/beans v0.1.2-0.20260615002029-e52dce57b52c`, a
   pseudo-version above the `v0.1.1` tag. Only three beans packages are imported:
   `model`, `repo`, and `store`. Confirmed in `internal/store/adapter.go`,
   `internal/store/adapter_test.go`, and `internal/api/validate/validate.go`.

2. **The beans module path is compiled into the build.** `Makefile` line 4 sets
   `LDFLAGS := -X github.com/mattsp1290/beans/version.Version=$(VERSION)`. A module
   rename that misses this line produces a binary with an empty version string and no
   build error.

3. **Both repositories gitignore `go.work`.** Each `.gitignore` contains a `go.work`
   and `go.work.sum` entry. A workspace file cannot be committed until that entry is
   removed.

4. **The production deploy script forbids a local `replace`.** In
   `scripts/deploy-production.sh`, `require_clean_local_ref` calls `fatal` when
   `go.mod` matches `replace .*github\.com/mattsp1290/beans`. The monorepo requires
   exactly such a replace, so this gate inverts rather than disappears. Its original
   purpose — refusing to deploy library code that is not published — is served in the
   monorepo by the single commit SHA that pins both modules.

5. **bean-counter has not been deployed to production yet.** `bd list --status=open` in
   bean-counter shows `bean-counter-m0p` ("First live production deploy of
   bean-counter") and `bean-counter-log` ("Bootstrap remote bean-counter checkout + DSN
   secret on infra host") both open. The deploy script's
   `DEFAULT_REPO_DIR='$HOME/git/bean-counter'` therefore most likely names a remote
   directory that does not exist. This is an inference from issue state, not a verified
   fact about the host, and [05-containers-and-deploy.md](05-containers-and-deploy.md)
   makes verifying it a prerequisite rather than an assumption.

6. **The two Beads trackers use distinct issue prefixes.** beans issues are
   `beans-*`; bean-counter issues are `bean-counter-*`. `bd import` upserts by issue ID,
   so importing one JSONL export into the other database cannot collide.

7. **The two `.golangci.yml` files express different policies.** beans uses
   `linters.default: none` with an explicit allow-list including `bodyclose`,
   `rowserrcheck`, and `sqlclosecheck`. bean-counter uses `linters.default: standard`
   plus `errorlint`, `gocritic`, `nilerr`, `nolintlint`, and `wastedassign`. Merging
   them would change lint results in both components.

8. **A 27 MB untracked binary sits at the beans repository root.** `bn` is a Mach-O
   arm64 executable, is not tracked, and is not matched by any `.gitignore` pattern.
   It therefore appears in `git status --porcelain` and would fail the deploy script's
   clean-worktree gate.

9. **`git-filter-repo` is not installed on this machine.** `git subtree` is available.
   The history-import strategy in [02-history-migration.md](02-history-migration.md)
   depends on which of the two is used, and they produce materially different history.

10. **Both Beads databases replicate to their own GitHub repository over a Dolt
    `git+ssh` remote.** `bd dolt remote list` reports
    `origin git+ssh://git@github.com/mattsp1290/<repo>.git` in each. Retiring the
    bean-counter tracker therefore also retires a replication target.

## Key decisions

### D1 — Symmetric `libs/` and `apps/` layout

The user selected this layout. The library moves to `libs/beans/`; bean-counter moves
to `apps/bean-counter/`; the third application will occupy `apps/<name>/`. Repository
root holds only monorepo-wide concerns.

### D2 — Both module paths are renamed to nested paths under the repository

- `github.com/mattsp1290/beans` becomes `github.com/mattsp1290/beans/libs/beans`.
- `github.com/mattsp1290/bean-counter` becomes `github.com/mattsp1290/beans/apps/bean-counter`.

Rejected alternative: keep the module path `github.com/mattsp1290/beans` while the
`go.mod` lives at `libs/beans/go.mod`. Go resolves a module path to a repository root
plus a subdirectory. With no `go.mod` at the repository root declaring that path, any
remote fetch of `github.com/mattsp1290/beans` fails permanently, and `go list -m` inside
the workspace reports a path that no longer describes where the code lives. Because the
user waived import-path compatibility, the correct nested path costs nothing and avoids
a latent trap. The cost is longer import lines, which `goimports` local-prefix grouping
absorbs.

### D3 — `replace` directives are authoritative; `go.work` is a convenience

`apps/bean-counter/go.mod` gets
`replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans` alongside a
`require` of a placeholder `v0.0.0`. A tracked `go.work` is added for editor and
cross-module test convenience.

The replace, not the workspace, is what makes the Docker build and any `GOWORK=off`
invocation correct. CI runs each module's gates with `GOWORK=off` specifically to prove
the replace is sufficient on its own, and runs one workspace-wide build with the
workspace active. Relying on `go.work` alone would leave the container build silently
dependent on a file that `.dockerignore` or a partial `COPY` could omit.

### D4 — bean-counter history is imported with rewritten paths

`git filter-repo --to-subdirectory-filter apps/bean-counter` runs on a scratch clone,
and the rewritten history is merged with `--allow-unrelated-histories`. This makes
`git log -- apps/bean-counter/...` return pre-migration commits without `--follow`.

Rejected alternative: `git subtree add --prefix=apps/bean-counter`. It requires no new
tooling, but historical commits keep their original root-relative paths, so
path-scoped `git log` and `git blame` over the imported subtree are broken for every
pre-migration commit. Because `git-filter-repo` is not installed, installing it is an
explicit prerequisite in [02-history-migration.md](02-history-migration.md), and the
subtree fallback is documented there with this trade-off stated.

### D5 — The beans side moves with `git mv`, not a history rewrite

Rewriting beans history to place everything under `libs/beans/` would also relocate
`.beads/`, `.agents/`, `.github/`, `LICENSE`, and `README.md`, which must stay at the
root. Selective path renames across the whole history add risk for no benefit that
rename detection does not already provide. A single `git mv` commit preserves
`git log --follow` for individual files.

### D6 — One Beads tracker at the repository root

The root `.beads/` keeps the existing `beans` Dolt database. bean-counter's 54 issues
are exported to JSONL and imported, keeping their `bean-counter-` prefix. The
`bean_counter` Dolt database and its `git+ssh` remote are retired after the import is
verified, and its export is committed as an archive artifact so the migration is
reversible from git alone.

### D7 — Per-module lint configuration is preserved

Each module keeps its own `.golangci.yml`. The two policies differ substantively
(finding 7), and unifying them would change lint results in code this plan is not
otherwise touching. Unification is recorded as deferred follow-up work.

## Target architecture

```text
beans/                                  repository root, monorepo
├── go.work                             new, tracked: ./libs/beans, ./apps/bean-counter
├── go.work.sum                         new, tracked
├── Makefile                            rewritten: fans out to each module
├── README.md                           rewritten: monorepo map
├── AGENTS.md                           merged from both repositories
├── CLAUDE.md                           merged from both repositories
├── LICENSE                             unchanged
├── .gitignore                          merged; go.work entries removed
├── .golangci.yml                       DELETED from root; lives per module
├── setup-beads.sh                      kept (beans variant)
├── setup-multi-repo-beads.sh           kept
├── .github/workflows/
│   ├── ci-workspace.yml                new: always runs, workspace-wide gates
│   ├── ci-libs-beans.yml               new: path-scoped to libs/beans
│   └── ci-apps-bean-counter.yml        new: path-scoped to apps/bean-counter
├── .beads/                             single tracker, Dolt database "beans"
├── .agents/plans/                      both repositories' plans, namespaced
├── libs/
│   └── beans/                          module github.com/mattsp1290/beans/libs/beans
│       ├── go.mod  go.sum  deps.go  .golangci.yml  Makefile
│       ├── cmd/bn/  model/  repo/  schema/  store/  version/
│       └── docs/
└── apps/
    ├── bean-counter/                   module github.com/mattsp1290/beans/apps/bean-counter
    │   ├── go.mod  go.sum  .golangci.yml  Makefile
    │   ├── Dockerfile  .dockerignore  .env.example
    │   ├── docker-compose.yml  docker-compose.stack.yml
    │   ├── cmd/  internal/  test/  frontend/  deploy/  docs/  prompts/  scripts/
    │   └── README.md
    └── <postgres-app>/                 reserved slot; see 07-third-app-slot.md
```

Module dependency graph after the change:

```text
apps/bean-counter ──replace──▶ libs/apps/../beans   (filesystem: ../../libs/beans)
apps/<postgres-app> ─────────▶ libs/beans          (same pattern, when created)
libs/beans                                          (no intra-repo dependencies)
```

## Risks

| Risk | Severity | Mitigation |
| --- | --- | --- |
| A missed module-path reference compiles but misbehaves, e.g. the `LDFLAGS` version path | High | WP4 greps for the old path across all file types, not just `.go`, and asserts `bn version` is non-empty |
| The deploy script's inverted `replace` gate is weakened rather than replaced, removing a real safety property | High | WP7 specifies the replacement gate's exact assertion and requires the shell unit test to cover it |
| `git filter-repo` rewrite is run against the live `$HOME/git/bean-counter` clone instead of a scratch clone, destroying the origin link | High | WP2 requires a fresh `git clone` into a scratch directory and forbids running filter-repo in `$HOME/git/bean-counter` |
| Beads import loses issues or dependency edges | Medium | WP8 compares `bd stats` and `bd dep tree` output before and after, and commits the source JSONL |
| Path-filtered CI workflows leave required status checks permanently pending | Medium | WP6 requires checking branch-protection settings before enabling path filters |
| Docker build succeeds locally via `go.work` but fails without it | Medium | D3; WP5 builds with `GOWORK=off` as an explicit gate |

## Assumptions

- `brew install git-filter-repo` is permitted on this machine. If it is not, the
  subtree fallback in [02-history-migration.md](02-history-migration.md) applies and
  success criterion 5 relaxes to `git log --follow` on a single file.
- No branch protection currently requires named status checks on `mattsp1290/beans`.
  WP6 verifies this rather than assuming it.
- No third party has cloned `mattsp1290/bean-counter`. This follows from the user's
  answer that neither repository has external consumers.

## Stop and go gates

- **G1 — before WP2.** Both working trees are clean, both Dolt databases are pushed,
  and a full backup of each repository exists. See
  [02-history-migration.md](02-history-migration.md).
- **G2 — before WP8 retires the bean-counter tracker.** The imported issue count and
  dependency edges match the pre-import measurement.
- **G3 — before the bean-counter GitHub repository is made read-only or archived.**
  This is an outward-facing action. It requires explicit user approval and is not
  performed by the implementing agent. It is listed as deferred work in
  [08-execution-handoff.md](08-execution-handoff.md).

## Open decisions

None are blocking. One non-blocking decision is deferred:

- **Lint policy unification** (owner: user). Whether `libs/beans` and
  `apps/bean-counter` should eventually share one `.golangci.yml`. Deferred per D7;
  resolution point is a follow-up Beads issue created in WP9.

## Document map

| File | Purpose |
| --- | --- |
| [00-overview.md](00-overview.md) | This file: context, decisions, risks, gates |
| [01-target-layout-and-module-graph.md](01-target-layout-and-module-graph.md) | Exact directory placement and module identity for every existing file |
| [02-history-migration.md](02-history-migration.md) | Git procedure for importing bean-counter with history |
| [03-go-module-restructure.md](03-go-module-restructure.md) | Module renames, import rewrite, `go.work`, `replace` |
| [04-build-lint-and-ci.md](04-build-lint-and-ci.md) | Makefiles, lint config, GitHub Actions workflows |
| [05-containers-and-deploy.md](05-containers-and-deploy.md) | Dockerfiles, compose files, `deploy-production.sh` |
| [06-beads-and-agent-config.md](06-beads-and-agent-config.md) | Tracker consolidation and root-level agent files |
| [07-third-app-slot.md](07-third-app-slot.md) | Conventions the future Postgres application follows |
| [08-execution-handoff.md](08-execution-handoff.md) | Ordered work packages, verification, definition of done |

## External requests

None. This plan changes only repositories the user owns and requires no capability from
another project.
