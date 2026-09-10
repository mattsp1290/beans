# Merge Gate — pass 4 overview

- **Branch:** `monorepo-consolidation` @ `e3875a8bf6db5001c286421852bcc219e52f61e5`
- **Base:** `main` @ `efc82834b04f1016cf68b7156465c314829342b5`
- **Date:** 2026-09-10
- **Reviewer:** Merge Gate (`merge-gate`)
- **Role:** Decide whether this branch is ready to merge — run every gate end to end,
  and judge whether four rounds of fixes left the branch coherent.

## Verdict

**APPROVE**

## Summary

Every gate I could execute is green, and I executed all of them except the two that
need a Docker daemon. Workspace build and vet, both modules standalone under
`GOWORK=off`, root `make ci` (which fans vet/lint/test/build/tidy-check across both
modules and leaves the tree byte-clean), `gofmt`, `shellcheck`, the 52-case shell
suite, `go work sync` idempotence, all three workflows parsing, the eight-target root
fan-out, and the production deploy dry-run all pass. The tree is clean at `e3875a8`
by both `git status --porcelain` and `git diff HEAD --stat`. The branch is a strict
descendant of `main` (merge-base *is* `main`, zero commits behind), so nothing on
`main` is at risk. I verified the history import structurally rather than trusting
the commit message: `bfe40cc` is a real two-parent merge, the second parent chain is
exactly 114 commits with June-2026 authorship dates intact, and `git log --
apps/bean-counter/<path>` reaches pre-import commits without `--follow`. The
deferred-work accounting reconciles exactly — 161 issues after the tracker import
plus the 9 the migration filed equals the 170 `bd stats` reports today, and all six
items in the plan's deferred-work table have a corresponding open bead.

I checked every factual claim in the six migration commit messages against the tree
and found one wrong number, plus a related contradiction that reaches further than
the commit message: a stale `bd` memory recorded bean-counter as pinning beans
`v0.1.1` (embedded migration `0007`), a state that commit `df029e9` superseded back
in June 2026. That figure was copied verbatim into commit `40d8e64` ("from 7 to 11")
and into the open **P0** issue `beans-ued`, while `deploy/README.md` and
`IMPLEMENTATION-NOTES.md` carry the correct figure (`0008`, exact parity with prod).
Separately, four carried-over open deploy issues still name paths this migration
itself invalidated — including the `.agents/plans/deploy/` directory whose one
*file-level* reference `e3875a8` went out of its way to fix. None of this is in code
or in a workflow; nothing fails to build, and nothing gets worse by merging. Two of
the three findings live in the Dolt tracker, which git does not carry at all, so they
cannot be "fixed before merge" in any meaningful sense. They are required before the
next production deploy, not before the merge — and the dependency graph already
enforces that ordering, since `beans-nlc` and `beans-ued` both block
`bean-counter-m0p`.

On the question the brief actually asks — does the whole thing hold together after
four rounds — yes. I looked specifically for intent eroded by later fixes and did not
find any. The retraction discipline across the rounds is the strongest signal here:
`5bd9e07` withdrew `2ef178e`'s golangci-lint mechanism claim after disproving it by
execution, and `e3875a8` withdrew `5bd9e07`'s "the status capture is load-bearing"
claim on the same grounds. Commit messages that correct themselves are commit
messages worth reading.

## Stats

```
git diff main...HEAD --shortstat
 273 files changed, 21536 insertions(+), 422 deletions(-)

git log main..HEAD --oneline | wc -l
 128
```

The 128 decomposes as 14 first-parent migration commits plus the 114 imported
bean-counter commits (`git rev-list --count --first-parent main..HEAD` = 14;
`git rev-list --count fba12e9` = 114).

## Gates

| # | Gate | How | Result |
|---|---|---|---|
| 1 | Workspace build — `go build ./libs/beans/... ./apps/bean-counter/...` | executed | **PASS** (exit 0) |
| 2 | Workspace vet — `go vet ./libs/beans/... ./apps/bean-counter/...` | executed | **PASS** (exit 0, no output) |
| 3 | `libs/beans` build, `GOWORK=off` | executed | **PASS** |
| 4 | `libs/beans` vet, `GOWORK=off` | executed | **PASS** |
| 5 | `libs/beans` test, `GOWORK=off` | executed | **PASS** (6 packages ok) |
| 6 | `apps/bean-counter` build, `GOWORK=off` | executed | **PASS** |
| 7 | `apps/bean-counter` vet, `GOWORK=off` | executed | **PASS** |
| 8 | `apps/bean-counter` test, `GOWORK=off` | executed | **PASS** (11 ok, 2 no-test-files) |
| 9 | Root `make ci` (vet, lint, test, build, tidy-check × 2 modules) | executed | **PASS** (exit 0; both lints "0 issues"; tree clean after both `go mod tidy` runs) |
| 10 | `gofmt -l ./libs ./apps` | executed | **PASS** (empty) |
| 11 | `shellcheck` on `deploy-production.sh` + `deploy-production_test.sh` | executed | **PASS** (exit 0, no output) |
| 12 | Shell unit suite — `bash apps/bean-counter/test/scripts/deploy-production_test.sh` | executed | **PASS** — 52 passed, 0 failed |
| 13 | `go work sync` idempotence | executed | **PASS** — exit 0, `git diff --exit-code` clean, porcelain empty, no `go.work.sum` created |
| 14 | All three workflow files parse | executed (Ruby `YAML.safe_load`) | **PASS** — jobs enumerate; `ci-workspace` `pull_request:` is intentionally null |
| 15 | Root Makefile fan-out — `make -n` for all 8 delegated targets | executed | **PASS** (build, test, vet, lint, tidy-check, ci, ci-integration, clean) |
| 16 | `./apps/bean-counter/scripts/deploy-production.sh --ref main --dry-run` | executed | **PASS** — monorepo paths, `embedded_max: 11`, SSH ok, no remote mutation |
| 17 | `docker build` (API + UI + build-stage probe) | **NOT RUN** | Daemon down: `Cannot connect to the Docker daemon at unix:///Users/punk1290/.docker/run/docker.sock`. Reasoned only — see 03. |
| 18 | `make ci-integration` (testcontainers) | **NOT RUN** | Same daemon. Deliberately excluded from `make ci` by design. |
| 19 | Frontend gates (`npm ci` / `test` / `check` / `build`) | **NOT RUN** | Outside my assigned gate list; unchanged by this branch beyond path moves. |
| 20 | Relative markdown links outside `.agents/reviews/` | executed | **PASS** — 103 links checked, 0 broken |
| 21 | TODO / FIXME / XXX / HACK introduced by the branch | executed (grep of added lines in `main...HEAD`, excluding `.agents/`) | **PASS** — none |
| 22 | Branch divergence from `main` | executed | **PASS** — merge-base *is* `main`; 0 commits on `main` not in `HEAD` |
| 23 | History import integrity | executed | **PASS** — 114 imported commits, two-parent merge at `bfe40cc`, dates preserved, path-scoped `git log` reaches pre-import |
| 24 | Plan success criteria 1–5, 7, 8, 9, 10 | executed | **PASS** (6 unverifiable — Docker) |
| 25 | Deferred-work coverage — all 6 planned items filed as beads | executed (`bd list`, serially) | **PASS** — plus 3 finding-driven; 161 + 9 = 170 = `bd stats` |
| 26 | Commit-message claims checked against the tree | executed | **1 wrong figure** — see 01, finding I1 |
| 27 | `IMPLEMENTATION-NOTES.md` accuracy | executed | Accurate; two completeness gaps — findings I2, S2 |

Tree state confirmed clean after every mutating gate (`make ci` runs `go mod tidy`
twice; the deploy dry-run touches nothing). `.agents/reviews/` is hidden from
`git status` by `.git/info/exclude`, so I also confirmed with `git diff HEAD --stat`
and by listing the 222 tracked files under that path.

## Merge instructions

**Use a real merge commit. Never squash, never rebase-merge.** On GitHub that means
the "Create a merge commit" button only. This is structural, not stylistic:
`bfe40cc` is a two-parent merge whose second parent is the 114-commit filter-repo'd
bean-counter history. A squash collapses all 128 commits into one and destroys that
parent permanently — `git log -- apps/bean-counter/internal/server/app.go` would stop
at the squash commit, and the plan's success criterion 5 would become false forever
with no way to recover it from this repository. A fast-forward would preserve
everything, but `--no-ff` is preferable so the consolidation is one legible node.

The merge commit should note:

1. **The tracker does not ride along in git.** Zero files under `.beads/` change on
   this branch; the embedded Dolt database is gitignored. The 54 imported
   bean-counter issues, their 80 edges and the two memories exist only in the local
   Dolt DB. `bd dolt push` must accompany `git push`, or the consolidation that
   `4af7ed0` describes reaches nobody else. The committed
   `.agents/plans/monorepo-consolidation/bean-counter-issues.jsonl` (55 records) is
   the git-only recovery path if that push is missed.
2. **The library version string is now a bare short SHA** until the first
   `libs/beans/v*` tag exists (`bn --version` prints `e3875a8`, not
   `v0.1.1-308-g...`). Tracked as `beans-vgn`.
3. **Gate G3 was not executed** — `github.com/mattsp1290/bean-counter` is untouched
   and remains the recovery source (`beans-ad3`, `beans-a98`).
4. **Nothing may be deployed yet.** `beans-nlc` (remote checkout still at
   `$HOME/git/bean-counter`) and `beans-ued` (embedded 11 vs prod 8) are both open
   P0s blocking `bean-counter-m0p`, and the parity gate will abort by design.
5. **The three Important findings in this review** (01) are corrections to tracker
   text, one `bd` memory, and one operator doc — required before the next production
   deploy, not before this merge.
