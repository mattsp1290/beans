# Migration Correctness — Review Overview

- **Branch:** `monorepo-consolidation` (base `main`, HEAD `40d8e6475e51`)
- **Date:** 2026-09-10
- **Reviewer:** Migration Correctness (`migration-correctness`)
- **Role:** Verify that the module rename, import rewrite and API-drift adaptation preserve behavior exactly, and that nothing in the moved code silently changed meaning.

## What the change does

This branch converts a single-module Go repository into a two-module monorepo. The
beans library and the `bn` CLI move from the repository root to `libs/beans/`
(module path `github.com/mattsp1290/beans` → `github.com/mattsp1290/beans/libs/beans`),
and `github.com/mattsp1290/bean-counter` is imported with full history into
`apps/bean-counter/` (module path → `github.com/mattsp1290/beans/apps/bean-counter`).
The application now depends on the library through a filesystem
`replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans` plus a `v0.0.0`
placeholder require, with a tracked root `go.work` as an editor convenience rather than
a build mechanism. Because bean-counter had been pinned to beans
`v0.1.2-0.20260615002029-e52dce57b52c` and `libs/beans` HEAD had since changed
`Store.ReadyIssues` and `Store.ListBlockingDeps` to take a `store.ListFilter` instead of a
project-prefix `string`, the migration adapted three call sites, two handler `Store`
interfaces and their test fakes. Supporting work re-homes CI into three path-scoped
workflows, moves `.dockerignore` and the API image build context to the repository root,
adds a fan-out root `Makefile`, and re-aims the production deploy script's beans-replace
gate from "reject any beans replace" to "require exactly the sanctioned one".

## Verdict

**NEEDS_DISCUSSION**

The rename, import rewrite and `ListFilter` adaptation are correct — I verified the
adapted call sites against the pinned library source at `e52dce57b52c` and against
`libs/beans/store/store.go` at HEAD, and the substitution is exactly behavior-preserving.
What is not settled is the *operational* consequence of the un-pinning: the image now
embeds beans migrations `0009`–`0011` instead of stopping at `0008`, which will hard-fail
the deploy script's mandatory schema-version parity gate against the shared
local-symphony Postgres, and no document on this branch says so or says what the operator
must do first. That is a conversation to have before merge, not a code defect to patch.

## Verification performed

| Gate | Result |
| --- | --- |
| `GOWORK=off go build ./...` in both modules | pass |
| `go vet ./libs/beans/... ./apps/bean-counter/...` | pass |
| `GOWORK=off go test ./...` in both modules | pass (all packages `ok`) |
| `gofmt -l` over both modules | clean |
| `go work sync` then `git status --porcelain` | no diff (idempotent) |
| `GOWORK=off go mod tidy` in both modules | no diff (idempotent) |
| Old vs new `ReadyIssues` / `ListBlockingDeps` / `ListIssues` semantics | verified equivalent |
| Stale `mattsp1290/beans/{model,store,repo,schema,version}` or `mattsp1290/bean-counter` imports in `*.go` | none |
| `libs/beans/deps.go` tools pins present as direct requires after tidy | all six survived |

## Stats

- **Files changed:** 237
- **Lines:** +17,130 / −422
- **Commits:** 125 total on `main..HEAD`, of which 11 are migration-authored
  (`d2c6584`, `bfe40cc`, `58d9abc`, `6895dea`, `14da22e`, `94da2fa`, `fb0b755`,
  `a73abfe`, `da07354`, `4af7ed0`, `40d8e64`); the remaining 114 are the imported,
  pre-existing bean-counter history and are out of scope.
