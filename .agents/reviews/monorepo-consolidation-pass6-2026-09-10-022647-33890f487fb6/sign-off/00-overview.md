# Sign Off — merge decision

- **Branch:** `monorepo-consolidation` @ `33890f487fb6c9a75c8b16a037f9bdade1eeee8f`
- **Base:** `main` @ `efc82834b04f1016cf68b7156465c314829342b5`
- **Date:** 2026-09-10
- **Reviewer:** Sign Off (`sign-off`)
- **Role:** Final merge decision on the branch as it now stands (sixth and intended final pass)

## Verdict

**APPROVE**

## Summary

I re-ran every gate that can run on this machine rather than reasoning about
any of them, and all of them pass: workspace build and vet, both modules under
`GOWORK=off`, root `make ci` (exit 0, `golangci-lint` 0 issues in both
modules), `gofmt`, `shellcheck` on the two deploy scripts CI checks, the shell
unit suite at 62/62 in six invocation modes, `go work sync` idempotence, the
`go list -m all` clean-tree fix, all three workflows parsing, the root Makefile
fan-out, the deploy dry-run, relative markdown links across all 89 delivered
docs, and both git-history criteria. I audited `33890f4`'s commit message the
way earlier rounds were audited, including by re-running the mutation
experiments it reports: **every substantive claim in it is true**, which is a
first for this branch. The one defect is a stale number — it says four
redundant containment entries "survive removal at 60/60" when the suite is now
62 tests and they survive at 62/62; the claim itself is correct, only the count
is from before this round added two tests. Nine of the ten success criteria in
`00-overview.md` are confirmed; criterion 6 (`docker build` produces a runnable
image) is uncheckable here because the local Docker engine is down, and it is
tracked as `beans-oba`. `IMPLEMENTATION-NOTES.md` is accurate, including its new
`go.work.sum` section, whose every factual claim I reproduced exactly. The
tracker is coherent: all four gaps the notes record are open, ready, and
correctly prioritised, and nothing the plan required was dropped rather than
tracked. The branch is sound. Merge it — with a true merge commit, never a
squash.

## Stats

```
$ git diff main...HEAD --shortstat
 295 files changed, 23937 insertions(+), 421 deletions(-)

$ git log main..HEAD --oneline | wc -l
     130
```

Of those 130 commits, 39 are merges and 114 are bean-counter's imported history,
brought in by merge `bfe40cc` ("Import bean-counter into apps/bean-counter with
history"), which carries a second root commit `dd88be7`.

## Gates

Every row marked **executed** was run on this machine at `33890f4` and its
output read. No row is marked passing on reasoning alone.

| # | Gate | Command | Result |
|---|------|---------|--------|
| 1 | Workspace build | `go build ./libs/beans/... ./apps/bean-counter/...` | **PASS** (exit 0) — executed |
| 2 | Workspace vet | `go vet ./libs/beans/... ./apps/bean-counter/...` | **PASS** (exit 0) — executed |
| 3 | `libs/beans` standalone | `GOWORK=off go build ./... && GOWORK=off go vet ./...` | **PASS** (0, 0) — executed |
| 4 | `apps/bean-counter` standalone | `GOWORK=off go build ./... && GOWORK=off go vet ./...` | **PASS** (0, 0) — executed |
| 5 | Replace resolves w/o workspace | `GOWORK=off go list -m .../libs/beans` | **PASS** — `v0.0.0 => ../../libs/beans` — executed |
| 6 | Root CI gate | `make ci` | **PASS** (exit 0); golangci-lint `0 issues` in both modules; all test packages `ok`; tidy-check clean — executed |
| 7 | Formatting | `gofmt -l libs apps` | **PASS** (no output) — executed |
| 8 | Shellcheck (CI's set) | `shellcheck apps/bean-counter/scripts/deploy-production.sh apps/bean-counter/test/scripts/deploy-production_test.sh` | **PASS** (exit 0, both) — executed |
| 9 | Shell suite — normal | `bash .../deploy-production_test.sh` | **PASS** 62/62, rc 0 — executed |
| 10 | Shell suite — `GIT_DIR` set | same, `GIT_DIR=<canary>/.git` | **PASS** 62/62, rc 0; canary repo HEAD and worktree unchanged — executed |
| 11 | Shell suite — `GIT_WORK_TREE` set | same, `GIT_WORK_TREE=<canary>` | **PASS** 62/62, rc 0 — executed |
| 12 | Shell suite — both set | same, both vars | **PASS** 62/62, rc 0 — executed |
| 13 | Shell suite — unrelated cwd | run from a non-git scratch dir | **PASS** 62/62, rc 0 — executed |
| 14 | Shell suite — from `/` | run from filesystem root | **PASS** 62/62, rc 0 — executed |
| 15 | `go work sync` idempotence | run twice, hash `go.work` each time | **PASS** — `go.work` sha identical before/after/after; tree clean — executed |
| 16 | `go work sync` creates no sum | delete `go.work.sum`, `go work sync` | **PASS** — file not regenerated — executed |
| 17 | `go list -m all` leaves tree clean | delete sum, `go list -m all`, `git status --porcelain` | **PASS** — 3215-byte sum regenerated, status **empty**; `git check-ignore -v` → `.gitignore:33:/go.work.sum` — executed |
| 18 | `go.work` still tracked | `git ls-files --error-unmatch` | **PASS** — `go.work` tracked, `go.work.sum` not — executed |
| 19 | Workflows parse | Ruby `YAML.safe_load` on all three | **PASS** — all three parse; jobs and path filters enumerated — executed |
| 20 | Root Makefile fan-out | `make -n` for `build test vet lint tidy-check ci ci-integration clean` | **PASS** 8/8, plus `fmt-check` and both escape hatches (`beans TARGET=build`, `bean-counter TARGET=test`) — executed |
| 21 | Deploy dry-run | `./apps/bean-counter/scripts/deploy-production.sh --ref main --dry-run` | **PASS** (exit 0) — prints monorepo paths (`apps/bean-counter/deploy/docker-compose.prod.yml`, `$HOME/git/beans`, `make -C apps/bean-counter test`), `embedded_max: 11` — executed |
| 22 | Relative markdown links | all 89 tracked `.md` outside `.agents/reviews/` | **PASS** — 107/107 resolve, 0 broken — executed |
| 23 | History criterion 4 | `git log --follow -- libs/beans/store/store.go` | **PASS** — 41 commits, oldest `39d689c` 2026-06-13 "Extract bn into beans module" (pre-migration) — executed |
| 24 | History criterion 5 | `git log -- apps/bean-counter/internal/server/app.go` | **PASS** — 5 commits, oldest `433d900` 2026-06-14 "ralph: iteration 1 checkpoint - initialize Go Fiber skeleton" (pre-import, at the new path) — executed |
| 25 | Tracked-symlink invariant | `git ls-files -s -- libs/beans apps/bean-counter \| awk '$1=="120000"'` | **PASS** — 0 under the deployed trees; 0 anywhere in the repo — executed |
| 26 | Containment mutation audit | 9 single-entry drops + 3 structural drops, in an isolated local clone | **PASS** — reproduces the commit message exactly (see `01`) — executed |
| 27 | Tracker | `bd stats`, `bd show beans-ued`, `bd ready`, `bd show bean-counter-m0p` — run serially | **PASS** — 170 total / 17 open / 14 ready; all four notes-recorded gaps are open and ready — executed |
| 28 | Repo left pristine | `git diff HEAD --stat`, `git status --porcelain` | **PASS** — both empty; HEAD still `33890f4` — executed |
| 29 | `docker build -f apps/bean-counter/Dockerfile .` | — | **NOT RUN** — Docker engine down (`docker info` → `ENGINE DOWN`). Tracked as `beans-oba`. |
| 30 | `docker run` of the built image | — | **NOT RUN** — same cause. |
| 31 | `make ci-integration` | — | **NOT RUN** — both modules' integration suites need testcontainers, i.e. a Docker daemon. |

**Nothing in this review is marked "reasoned".** Every claim above was executed.
The three NOT RUN rows are stated as unrun, not inferred to pass.

## Ten success criteria (`.agents/plans/monorepo-consolidation/00-overview.md`)

| # | Criterion | Status |
|---|-----------|--------|
| 1 | `go build ./...` in both modules, workspace active and `GOWORK=off` | **MET** — all four combinations, gates 1–4 |
| 2 | `go test ./...` passes in both modules | **MET** — via `make ci`, every package `ok` |
| 3 | `go vet` and `golangci-lint run` clean in both modules | **MET** — `0 issues` in both |
| 4 | `git log --follow -- libs/beans/store/store.go` reaches pre-restructure commits | **MET** — back to 2026-06-13 |
| 5 | `git log -- apps/bean-counter/internal/server/app.go` reaches pre-import commits at the new path | **MET** — back to 2026-06-14, no `--follow` needed |
| 6 | `docker build -f apps/bean-counter/Dockerfile .` produces a runnable image | **UNCHECKABLE WITHOUT DOCKER** — engine down; tracked as `beans-oba`; a CI job builds both images on merge |
| 7 | `bd stats` ≥ 150 total and ≥ 7 open; `bd show bean-counter-m0p` resolves | **MET** — 170 total, 17 open; `bean-counter-m0p` resolves with 4 dependencies |
| 8 | Shell suite passes and `shellcheck` is clean on both files | **MET** — 62/62, shellcheck exit 0 |
| 9 | Deploy dry-run prints monorepo compose path, repo dir, build contexts | **MET** — per the corrected invocation in `IMPLEMENTATION-NOTES.md` (`--ref main --dry-run`) |
| 10 | `git status --porcelain` empty at the repository root | **MET** — empty, including after `make ci`, `go work sync` and `go list -m all` |

**Nine met, one (criterion 6) uncheckable without Docker.** That is the only
criterion Docker blocks; criteria 1–5 and 7–10 need no daemon and all passed.

## Proposed merge-commit body

The exact text is in
[`03-positive-notes.md`](03-positive-notes.md#proposed-merge-commit-body).

## Merge instruction

```
git checkout main
git merge --no-ff monorepo-consolidation      # NOT --squash, NOT rebase
```

A squash or rebase merge would permanently destroy success criteria 4 and 5.
See `03-positive-notes.md` for why.
