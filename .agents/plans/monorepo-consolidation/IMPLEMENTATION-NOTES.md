# Implementation notes

The files 00 through 08 in this directory are the plan as written before
implementation, and they are left as written. This file records where the
implementation departed from them and why, so a reader of the plan is not
misled by a criterion that no longer describes the repository.

Implemented on branch `monorepo-consolidation`.

## Findings that contradicted the plan's assumptions

**The infra host is running bean-counter in production.** Plan finding 5 in
[00-overview.md](00-overview.md) inferred from the open issues
`bean-counter-m0p`, `bean-counter-log` and `bean-counter-am5` that no remote
checkout or running stack existed. The WP1 remote-state check says otherwise:
`/home/infra-admin/git/bean-counter` exists, and `bean-counter-api-1` and
`bean-counter-ui-1` have both been up and healthy for about two months on
ports 8081 and 8088. The remote-side move is therefore a real production
migration with a rollback path, not the no-op the plan expected. It was **not**
performed; it is tracked as `beans-nlc` and needs explicit approval.

**The deploy parity gate inverts.** Before the migration, bean-counter pinned
beans at `v0.1.2-0.20260615002029-e52dce57b52c`, embedding migrations through
`0008` against a production database at `0008`, so the gate passed. Building
against `libs/beans` at HEAD raises the embedded maximum to `0011`, so the gate
now aborts — correctly, since applying them would migrate a Postgres that
bean-counter does not own. [05-containers-and-deploy.md](05-containers-and-deploy.md)
called this change "a semantic improvement" and did not anticipate the numeric
jump. Tracked as `beans-ued`, and documented for operators in
`apps/bean-counter/deploy/README.md`.

## API drift the plan did not anticipate

[03-go-module-restructure.md](03-go-module-restructure.md) excluded "any change
to a function, type, or test assertion". Two were unavoidable: `libs/beans`
HEAD had changed `Store.ReadyIssues` and `Store.ListBlockingDeps` to take a
`store.ListFilter` instead of a project-prefix string. Both call sites, the
`deps` and `graph` handler interfaces, and their test fakes were adapted to
pass `ListFilter{Prefix: projectPrefix}`, which selects exactly what the string
argument used to select.

## Corrections to the plan's own instructions

- **`bn version` is not a subcommand.** [03-go-module-restructure.md](03-go-module-restructure.md)
  step 7 says to verify with `./bin/bn version`; the CLI exposes `--version`.
  The plan anticipated this and said to confirm the spelling first.
- **`bd dep tree` requires an issue id** in this bd build, so the pre-import
  dependency snapshot in [06-beads-and-agent-config.md](06-beads-and-agent-config.md)
  step 1 was taken from `bd export` instead. All 80 edges were compared
  set-wise before and after; none were lost.
- **`bd import --dry-run` does not report creates versus updates** in this
  build. The collision check the plan wanted was done instead by confirming no
  `bean-counter-*` id existed in the root database before the import.
- **The `deploy/docker-compose.prod.yml` build contexts.** [05-containers-and-deploy.md](05-containers-and-deploy.md)
  says the `api` context becomes `../..` and the `ui` context is "unchanged".
  Both are wrong: that file sits one level deeper than
  `docker-compose.stack.yml`, so the repository root is `../../..`, and its
  `./frontend` had been resolving to `apps/bean-counter/deploy/frontend`, a
  path that has never existed. Rendering the file, rather than reading it,
  caught this.

## `go.work.sum` is ignored, not tracked

[03-go-module-restructure.md](03-go-module-restructure.md) step 5 says both
`go.work` and `go.work.sum` are committed, and
[00-overview.md](00-overview.md)'s target tree lists `go.work.sum` as "new,
tracked". `go work sync` generates no `go.work.sum` for this workspace, so
there was nothing to commit.

It is not simply absent, though: read commands generate it. `go list -m all`
writes a 3.2 KB `go.work.sum`, and because the file was neither tracked nor
ignored it then appeared in `git status --porcelain` — which
`apps/bean-counter/scripts/deploy-production.sh`'s clean-worktree gate treats
as a reason to abort. A developer who ran `go list -m all` could not deploy
until they deleted a file no build step maintains. `--dry-run` does not call
that gate, which is why no check on this branch caught it.

The root `.gitignore` now carries an anchored `/go.work.sum`. `go.work` itself
stays tracked.

## Stale acceptance criteria

- **[01-target-layout-and-module-graph.md](01-target-layout-and-module-graph.md)
  AC2** lists the tracked depth-1 files. The real list adds `.dockerignore`
  (introduced by WP5, after that document was written) and omits `go.work.sum`,
  which `go work sync` does not generate for this workspace.
- **[01-target-layout-and-module-graph.md](01-target-layout-and-module-graph.md)
  AC4** forbids any path under `apps/bean-counter/` beginning `../../..`. The
  prod compose file's `context: ../../..` is correct, per the depth correction
  above, and belongs in that criterion's exception list.
- **[00-overview.md](00-overview.md) success criterion 9** invokes
  `scripts/deploy-production.sh --dry-run`. The script lives at
  `apps/bean-counter/scripts/deploy-production.sh` and requires `--ref`; the
  equivalent is
  `./apps/bean-counter/scripts/deploy-production.sh --ref main --dry-run`.
- **[00-overview.md](00-overview.md) success criterion 7** expects at least 150
  total and at least 7 open issues. Both hold, but the numbers moved because
  implementation itself created issues.
- **[01-target-layout-and-module-graph.md](01-target-layout-and-module-graph.md)
  AC2 and [00-overview.md](00-overview.md)'s target tree** both list
  `go.work.sum` among the tracked root files. See the section above: it is
  ignored, not tracked.

## Not verified

**Success criterion 6, and WP5 acceptance criteria 1 and 2** — that
`docker build -f apps/bean-counter/Dockerfile .` produces a runnable image, and
that the UI image builds. Docker Desktop on this machine will not start; its
backend crashes with `opening tray: starting electron: unmarshaling start
request: unexpected EOF`. `make ci-integration` is unrun for the same reason.
Everything not requiring a daemon was verified, both compose files were checked
by rendering, and a CI job now builds both images and asserts the API build
stage resolves the library without `go.work`. Tracked as `beans-oba`.

## Gate G3 was not executed

`github.com/mattsp1290/bean-counter` is untouched: not archived, not made
read-only, not deleted. `$HOME/git/bean-counter` is unchanged from its
pre-migration state and still has its `origin` remote, and its `bean_counter`
Dolt database is intact as the recovery source. Tracked as `beans-ad3`.
