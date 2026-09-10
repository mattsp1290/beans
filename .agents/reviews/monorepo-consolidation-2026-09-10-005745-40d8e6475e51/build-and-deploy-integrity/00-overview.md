# Review Overview

- **Branch:** `monorepo-consolidation` (base `main`, HEAD `40d8e6475e51`)
- **Date:** 2026-09-10
- **Reviewer:** Build and Deploy Integrity (`build-and-deploy-integrity`)
- **Role:** Verify that every build, container, CI and deploy path resolves correctly from its real working directory, and that the deploy script's safety gates are no weaker than before.

## Summary

The path work in this migration is, with one exception, careful and correct. I verified
the Dockerfile's `COPY` set against `go list -deps ./cmd/bean-counter` (it copies exactly
the packages the binary needs, plus the whole `libs/beans` tree the `go:embed` requires),
confirmed the standalone `GOWORK=off` build succeeds through the `replace` alone, and
rendered both compose files with `docker compose config` — the two different depths
(`../..` and `../../..`) both resolve to the repository root, and the prod `ui` context
correctly resolves to `apps/bean-counter/frontend`. The workflows keep `uses:` step inputs
repo-root-relative while `run` steps use `working-directory`, which is the classic trap and
it is avoided. Every relative path inside `deploy-production.sh`, local and inside both
remote heredocs, was repointed consistently.

Two things block. First, the replace gate is genuinely weaker than the one it replaced:
`check_sanctioned_replace` matches with `grep -F`, a *substring* test, so
`replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans/vendored-fork` is
accepted — the old `grep -E 'replace.*github\.com/mattsp1290/beans'` gate rejected exactly
that, and the `../../libs/beans/...` variant is inside the tree the Dockerfile copies, so it
builds and ships. I confirmed three distinct bypasses by sourcing the real script and
calling the real function. Second, `ci-libs-beans.yml` pins golangci-lint `v2.1.6`, whose
`go install` build reports `go1.23.4`; it refuses `libs/beans/.golangci.yml`'s
`run.go: "1.25"` and exits 3, so `make ci` fails at `lint` on every run of that workflow. I
reproduced both by building the pinned binary and running it. Beyond those, the notable
gaps are missing gates rather than wrong paths: nothing in CI runs `apps/bean-counter`'s
`tidy-check`, nothing in CI builds the API image, and `/.dockerignore` — now a load-bearing
input to the root-context build — appears in no workflow's `paths` filter.

## Verdict

**REQUEST_CHANGES**

## Stats

- Files changed: **237**
- Lines: **+17130 / -422**
- Commits on branch: **125** (`git log main..HEAD --oneline | wc -l`; 114 of these are the
  imported bean-counter history and are out of scope, leaving ~11 migration-authored commits)
