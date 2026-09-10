# Review Overview

- **Branch:** `monorepo-consolidation`
- **Commit under review:** `2ef178e` ("Address findings from the dual review")
- **Date:** 2026-09-10
- **Reviewer:** CI Realism (`ci-realism`)
- **Role:** Check that every CI and Makefile change would actually succeed on a clean GitHub runner, not just locally.

## Stats

`git show --shortstat 2ef178e`:

```
15 files changed, 243 insertions(+), 67 deletions(-)
```

## Summary

Most of this commit's CI surface holds up under a clean-runner reading, and I
executed the parts I could. `go work sync` is genuinely a no-op on a fresh clone
with both a warm and a **cold** module cache — no diff, no `go.work.sum`, nothing
untracked — so `ci-workspace`'s new porcelain check is a stable gate rather than
a flake generator. `make tidy-check` is clean in both modules on a fresh clone,
so the new backend step will pass. Every target the root `Makefile` fans out to
exists in both modules (`make -n` on all nine root targets), and `ci-integration`
now resolves because `libs/beans` gained `test-integration`. golangci-lint
v2.12.2 accepts `libs/beans/.golangci.yml` — `version: "2"`, `run.go: "1.25"`,
`linters.default: none` with the allow-list, and the `formatters` block — with
0 issues and exit 0, and `actions/setup-go` really does put `$(go env GOPATH)/bin`
on `PATH` (`addBinToPath()`), so `make ci`'s bare `golangci-lint run` will resolve.
Both compose files render (`docker compose config`, rc=0, with the daemon down).

Two things do not hold up. First, the new `images` job's headline assertion is
**vacuous**: the runtime stage is `FROM alpine:3.22` and receives only
`/usr/local/bin/bean-counter`, so `/src` does not exist there and
`ls /src/go.work` fails unconditionally — the check passes no matter what, and
would still pass if someone added `COPY go.work` to the build stage, which is the
exact regression it claims to guard. Second, the six-line comment now baked into
`ci-libs-beans.yml` documents a failure mechanism that is false on a clean runner:
`go install pkg@version` picks `max(installed Go, module's go directive)`, never
downgrades, so v2.1.6 (go directive `1.23.0`) would have been built with the
runner's go1.25.7. I built it that way and ran it against `libs/beans`: 0 issues,
exit 0. The v2.12.2 pin is still worth keeping, but its stated justification is
wrong and will mislead the next person. Separately, the root `Makefile` — the
fan-out contract this commit edited — is exercised by no workflow at all.

I could not execute `docker build` or `docker run`: the local Docker engine is
down. Every Docker claim below is static reasoning and is marked as such.

## Verdict

**REQUEST_CHANGES**
