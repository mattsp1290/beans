# Regression Auditor — pass 3 overview

- **Branch:** `monorepo-consolidation` (HEAD `5bd9e07513ecc5e132ec2aa03002b12a3f3bed5c`, base `main`)
- **Date:** 2026-09-10
- **Reviewer:** Regression Auditor (`regression-auditor`)
- **Role:** Check that the whole branch still holds together after three rounds of edits, and that nothing earlier in the migration was undone. Lane is the branch as a whole; the deploy script's shell gates belong to the other reviewer in this pass.
- **Stats:** `git diff main...HEAD --shortstat` → `239 files changed, 17403 insertions(+), 422 deletions(-)`; `git log main..HEAD --oneline | wc -l` → `127`

## Verdict

`REQUEST_CHANGES`

## Summary

The migration held. I executed every success criterion that can be executed on this
machine and all of them pass: both modules build, test, vet and lint clean with the
workspace active and with `GOWORK=off`; `make ci` at the root is green end to end and
leaves `git status --porcelain` empty; `git log --follow` on `libs/beans/store/store.go`
reaches 41 pre-move commits, `git log` on `apps/bean-counter/internal/server/app.go`
reaches 5 pre-import commits, the move commit `58d9abc` is 112 `R100` rename entries and
nothing else, and the only remote is `origin → mattsp1290/beans`. The Beads
consolidation is exact — all 54 archived `bean-counter-*` issues and all 80 archived
dependency edges are present, nothing lost. Most importantly for this lane, the two
reactive fix commits did **not** undo earlier intent: the `GOWORK=off` discipline was
tightened in four more places, decision D7's per-module lint separation is byte-for-byte
untouched since the rename commit, the "root Makefile contains no build logic" rule was
actively *restored* (`2ef178e` deleted the one inlined `cd libs/beans && go test` and
replaced it with a fan-out), and there is still exactly one `AGENTS.md`, one `CLAUDE.md`,
and one `BEADS INTEGRATION` marker in each. I mutation-tested two gates the fixes
introduced or relied on and both are real rather than vacuous.

What I am requesting changes for is entirely documentation correctness and plan drift,
with no correctness, security or build impact. `apps/bean-counter/deploy/README.md:5`
still links to `../.agents/plans/deploy/`, a directory the migration deliberately hoisted
and renamed to `.agents/plans/bean-counter-deploy/` at the root — it is the only broken
relative markdown link in any tracked non-`.agents` file, and it survived the commit whose
whole job was sweeping stale references. `README.md` and `AGENTS.md` both assert the root
`.dockerignore` is "shared by every image build in the repo", which is false for the UI
image and contradicted by that file's own header comment. And two of the four layout
acceptance criteria now fail as written, because the plan document was never amended when
round-two fixes legitimately changed the root file set and the prod compose build context.
Every fix is a one-line edit.

## Success criteria (`.agents/plans/monorepo-consolidation/00-overview.md`)

| # | Criterion | Verdict | Evidence |
| --- | --- | --- | --- |
| 1 | `go build ./...` in both modules, workspace and `GOWORK=off` | **holds** | executed; all four invocations exit 0 |
| 2 | `go test ./...` passes in both modules | **holds** | executed; 6/6 packages ok in `libs/beans`, 12/12 ok in `apps/bean-counter` |
| 3 | `go vet ./...` and `golangci-lint run ./...` clean in both | **holds** | executed; vet exit 0 both; `0 issues.` under v2.1.6 (PATH) and under the CI-pinned v2.12.2 |
| 4 | `git log --follow -- libs/beans/store/store.go` reaches pre-restructure commits | **holds** | 41 commits, oldest `39d689c Extract bn into beans module` |
| 5 | `git log -- apps/bean-counter/internal/server/app.go` reaches pre-import commits | **holds** | 5 commits, oldest `433d900 ralph: iteration 1 checkpoint` |
| 6 | `docker build -f apps/bean-counter/Dockerfile .` produces a runnable image | **uncheckable** | local Docker engine down (`docker info` exit 1); tracked as `beans-oba` |
| 7 | `bd stats` ≥ 150 total and ≥ 7 open; `bd show bean-counter-m0p` resolves | **holds** | 170 total / 17 open / 152 closed / 14 ready; `bd show` resolves with 4 depends-on + 1 blocks |
| 8 | deploy script unit tests pass and `shellcheck` is clean | **holds** | `42 passed, 0 failed`; shellcheck exit 0 |
| 9 | `--dry-run` plan reflects the monorepo layout | **holds (path in criterion is stale)** | at the real path: repo-dir `$HOME/git/beans`, compose `-f apps/bean-counter/deploy/docker-compose.prod.yml`, both build contexts the repo root |
| 10 | `git status --porcelain` is empty | **holds** | empty at session start and after every gate, including `make ci` and `go work sync` (caveat in S8: `.agents/reviews/` is excluded machine-locally) |

Criterion 7's counts moved because this session created issues; the numbers above are the
actual measurement, all comfortably above the "at least" floors.

Criterion 9 names `scripts/deploy-production.sh` at the repository root. No such path
exists — the same plan's layout mapping puts the script at
`apps/bean-counter/scripts/deploy-production.sh`. Verified there instead; see
`01-critical-and-important.md`.

## Layout acceptance criteria (`01-target-layout-and-module-graph.md`)

| # | Criterion | Verdict | Evidence |
| --- | --- | --- | --- |
| 1 | No tracked `.go` outside `libs/beans/` and `apps/bean-counter/` | **holds** | `git ls-files '*.go'` filtered → empty |
| 2 | Root depth-1 tracked files are exactly the listed ten | **fails** | extra `.dockerignore`; `go.work.sum` absent (and not produced by `go work sync`) |
| 3 | `schema/migrations/{mysql,postgres,sqlite}` present and `go test ./schema/...` passes | **holds** | all three dirs present; `ok .../libs/beans/schema` |
| 4 | No `../../..` or `$HOME` path under `apps/bean-counter/` outside the stated exception | **fails** | `deploy/docker-compose.prod.yml:37` `context: ../../..`; `deploy/README.md:140` `$HOME/bean-counter-secrets/bn_dsn` |

Both failures are plan-text drift, not defects: the compose context resolves correctly to
the repository root (confirmed by rendering both compose files), and the root
`.dockerignore` is required now that the API build context is the root.
