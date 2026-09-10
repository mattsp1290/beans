# Final Merge Gate — pass 5

- **Branch:** `monorepo-consolidation` @ `faa3de830dab5ee8bcf0f3889910b0f7febc5838`
- **Base:** `main` @ `efc82834b04f1016cf68b7156465c314829342b5` (merge-base = main tip; branch is fast-forwardable)
- **Date:** 2026-09-10
- **Reviewer:** Final Merge Gate (`final-merge-gate`)
- **Role:** Confirm the branch is mergeable after five rounds, and that round 4's fixes introduced nothing new.

## Verdict

**APPROVE**

## Summary

I re-ran every gate the branch claims and every gate I could construct, and the branch
holds up: both modules build, vet, lint, test and tidy clean with the workspace active
and with `GOWORK=off`; root `make ci` exits 0 with `0 issues.` from both golangci-lint
invocations; `gofmt -l` is silent; the shell suite is 60/60; `shellcheck` on the deploy
pair is clean; `go work sync` is a genuine no-op; all three workflows parse; every root
Makefile target resolves; the deploy dry-run prints monorepo paths end to end; the
worktree is byte-clean; and `git log --follow -- libs/beans/store/store.go` (41 commits,
back to 2026-06-13) and `git log -- apps/bean-counter/internal/server/app.go` (5 commits,
back to 2026-06-14) both still reach pre-migration work, with two root commits reachable
from HEAD and exactly 114 commits on the import merge's second parent. All three items the
previous merge gate raised are genuinely fixed in the records themselves, not merely
described as fixed: `bd memories` and `beans-ued` now both state that `e52dce5` embedded
through `0008` and mark the old "0007" figure as stale; no open issue names
`.agents/plans/deploy/` or an unprefixed `scripts/deploy-production.sh` any more; and
`apps/bean-counter/deploy/README.md` now carries a dedicated "the remote checkout" section
alongside the schema-parity one. Auditing `faa3de8`'s own message against the tree turned
up the third overstatement the pattern predicted — its mutation-coverage claim for the
`require_repo_root` containment loop is measurably false (4 of the loop's 9 entries fail
zero tests when swapped out), though the control itself is sound and the redundancy is
real. The one non-commit-message defect I found is that `go.work.sum` is neither tracked
nor ignored, so a workspace-mode `go list -m all` leaves an untracked file that aborts
`--check` and live deploys. Neither item breaks a gate this branch runs, both are
correctable in the merge commit or a one-line follow-up, and I do not consider either a
reason to hold a 129-commit restructure that is otherwise clean.

## Stats

```
$ git diff main...HEAD --shortstat
 284 files changed, 22807 insertions(+), 422 deletions(-)

$ git log main..HEAD --oneline | wc -l
     129
```

Topology: 15 first-parent commits (14 monorepo work commits + the import merge `bfe40cc`),
plus 114 commits on `bfe40cc^2` — bean-counter's full history, rooted at its own initial
commit `dd88be7`. Two root commits are reachable from HEAD (`c13fe1c` beans, `dd88be7`
bean-counter).

## Gates

| # | Gate | How | Result |
| --- | --- | --- | --- |
| 1 | Workspace build, `libs/beans` | `go -C libs/beans build ./...` | **executed** — rc=0 |
| 2 | Workspace vet, `libs/beans` | `go -C libs/beans vet ./...` | **executed** — rc=0 |
| 3 | Workspace build, `apps/bean-counter` | `go -C apps/bean-counter build ./...` | **executed** — rc=0 |
| 4 | Workspace vet, `apps/bean-counter` | `go -C apps/bean-counter vet ./...` | **executed** — rc=0 |
| 5 | `GOWORK=off` build + vet, both modules | 4 invocations | **executed** — all rc=0 |
| 6 | `GOWORK=off` module resolution | `go list -m -f '{{.Dir}}' …/libs/beans` | **executed** — resolves to `/Users/punk1290/git/beans/libs/beans` via the `replace` |
| 7 | Root `make ci` | `make ci` | **executed** — rc=0; `0 issues.` from both linters; all test packages `ok`; both `tidy-check` diffs empty |
| 8 | `gofmt -l` | over `libs/beans` and `apps/bean-counter` | **executed** — no output |
| 9 | `shellcheck` (deploy pair) | script + test file | **executed** — clean, rc=0 |
| 10 | `shellcheck` (all tracked `*.sh`) | 5 files | **executed** — rc=1; 4 info findings in `setup-beads.sh`, 100 in `setup-multi-repo-beads.sh`. Identical counts on `main` → pre-existing, not a branch regression |
| 11 | Shell unit suite | `bash apps/bean-counter/test/scripts/deploy-production_test.sh` | **executed** — **60 passed, 0 failed** |
| 12 | `go work sync` idempotence | from a clean tree | **executed** — no-op: `go.work` unchanged, **no** `go.work.sum` produced, porcelain empty |
| 13 | Workflows parse | Ruby `YAML.safe_load` on all three | **executed** — all parse; jobs `workspace` / `gates` / `backend,frontend,deploy-scripts,images,integration` |
| 14 | Root Makefile fan-out | `make -n` on all 11 targets | **executed** — all resolve (CI's loop checks 8 of them) |
| 15 | Deploy dry-run | `./apps/bean-counter/scripts/deploy-production.sh --ref main --dry-run` | **executed** — rc=0; repo-dir `$HOME/git/beans`, compose `apps/bean-counter/deploy/docker-compose.prod.yml`, `embedded_max: 11` |
| 16 | Relative markdown links | 125 relative links across 302 tracked `*.md` | **executed** — 0 broken outside `.agents/reviews/`; the 22 flagged there are absolute `/Users/…:line` citations in pre-existing review records |
| 17 | `git status --porcelain` | at repo root | **executed** — empty |
| 18 | History: `libs/beans/store/store.go` | `git log --follow` | **executed** — 41 commits, oldest `39d689c` 2026-06-13 |
| 19 | History: `apps/bean-counter/internal/server/app.go` | `git log` | **executed** — 5 commits, oldest `433d900` 2026-06-14, at the `apps/bean-counter/` path |
| 20 | Import fidelity | `git rev-list --count bfe40cc^2` | **executed** — 114, rooted at `dd88be7` |
| 21 | `bd stats` | serial | **executed** — 170 total, 17 open (criterion 7 needs ≥150 / ≥7) |
| 22 | `bd show beans-ued` | serial | **executed** — corrected text present |
| 23 | `bd show bean-counter-m0p` | serial | **executed** — resolves; paths repointed |
| 24 | `bd memories --json` | serial | **executed** — memory carries the `CORRECTED 2026-09-10` text |
| 25 | Dead-path scan across all 17 open issues | scripted over `bd list --status=open --json` | **executed** — zero hits for `.agents/plans/deploy/` or unprefixed `scripts/deploy-production.sh` |
| 26 | `deploy/README.md` covers both blockers | read | **executed** — both sections present and numerically correct |
| 27 | Module graph | greps + `go list -m` | **executed** — no `libs/beans` → `apps/bean-counter` edge; no residual old module paths |
| 28 | Single-tracker invariant | `find -name .beads` | **executed** — exactly one, at the root |
| 29 | One `AGENTS.md` / one `CLAUDE.md`, one BEADS marker each | `git ls-files` + grep | **executed** — one each; one BEGIN/END pair each, same hash `ca08a54f` |
| 30 | Mutation coverage of the containment loop | 10 mutants against a scratch copy of the script + test | **executed** — see `01-critical-and-important.md` finding I1 |
| 31 | `require_in_repo` adversarial inputs | 18 input shapes against a scratch fixture | **executed** — every out-of-tree shape rejected |
| 32 | `go.work.sum` handling | bisected 7 go commands | **executed** — `go list -m all` creates an untracked, un-ignored `go.work.sum`; see finding I2 |
| 33 | `docker build -f apps/bean-counter/Dockerfile .` | — | **NOT RUN** — `docker info` rc=1, `Cannot connect to the Docker daemon at unix:///Users/punk1290/.docker/run/docker.sock`. Covered by the `images` job in `ci-apps-bean-counter.yml`; tracked as `beans-oba` |
| 34 | `docker build ./apps/bean-counter/frontend` | — | **NOT RUN** — same daemon outage; same CI coverage |
| 35 | `docker run` build-stage `go.work` probe | — | **NOT RUN** — same daemon outage; the `images` job asserts it |
| 36 | `make ci-integration` | — | **NOT RUN** — testcontainers needs the same daemon. `make -n ci-integration` resolves; the `integration` jobs in both workflows cover it |
| 37 | Squash-safety of the merge | reasoned from topology (gate 20) | **reasoned** — a squash or rebase-merge destroys criteria 4 and 5; see `04-action-items.md` |

Nothing in the repository was modified. Confirmed after every mutating experiment:
`git status --porcelain` empty, `git diff HEAD --stat` empty, HEAD still `faa3de8`. All
mutation and probe work was done on copies under the session scratchpad.
