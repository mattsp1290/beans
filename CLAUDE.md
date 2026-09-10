# Project Instructions for AI Agents

This file provides instructions and context for AI coding agents working on
this repository. `AGENTS.md` is the fuller reference; this file records the
build commands, the architecture, and the conventions that differ from a
single-module Go repository.

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->

## Build & Test

Run from the repository root:

```bash
make ci               # vet, lint, test, build, tidy-check across every module
make ci-integration   # testcontainers suites; requires a running Docker daemon
make build            # every module
make test             # every module

make beans TARGET=build          # one target in libs/beans
make bean-counter TARGET=test    # one target in apps/bean-counter
```

Per module, when you need the standalone view CI and the container use:

```bash
( cd libs/beans        && GOWORK=off go build ./... && GOWORK=off go test ./... )
( cd apps/bean-counter && GOWORK=off go build ./... && GOWORK=off go test ./... )
```

Deploy-surface gates for bean-counter:

```bash
shellcheck apps/bean-counter/scripts/deploy-production.sh \
           apps/bean-counter/test/scripts/deploy-production_test.sh
bash apps/bean-counter/test/scripts/deploy-production_test.sh
```

## Architecture Overview

A monorepo of Go modules, with no module at the repository root:

| Path | Module path | What it is |
| --- | --- | --- |
| `libs/beans` | `github.com/mattsp1290/beans/libs/beans` | The beans library and the `bn` CLI. No intra-repo dependencies. |
| `apps/bean-counter` | `github.com/mattsp1290/beans/apps/bean-counter` | Go + Fiber v3 API and Svelte UI over the beans store. |
| `apps/<name>` | `github.com/mattsp1290/beans/apps/<name>` | Reserved slot for a Postgres-backed application. Not yet created. |

Applications depend on the library through a filesystem `replace`
(`=> ../../libs/beans`) plus a `v0.0.0` placeholder `require`. The root
`go.work` is a tracked convenience; the `replace` is the mechanism. There is
deliberately no root `go.mod`: one would claim the
`github.com/mattsp1290/beans` path and make the nested module paths ambiguous.

CI is three workflows: `ci-workspace` always runs and proves the workspace is
coherent; `ci-libs-beans` and `ci-apps-bean-counter` are path-scoped and run
with `GOWORK=off` so they prove each module stands on its own.
`ci-apps-bean-counter`'s path filter includes `libs/beans/**`, because a
library change reaches the application through the `replace`.

The full design, including why each decision was made, is in
`.agents/plans/monorepo-consolidation/`.

## Conventions & Patterns

- **The `replace` is not optional.** The container build and every `GOWORK=off`
  invocation resolve the library through it. `apps/bean-counter`'s deploy
  script aborts if it is missing or points anywhere but `../../libs/beans`.
- **Tidy with `GOWORK=off`.** A workspace-active `go mod tidy` can resolve
  through a sibling module and write a `go.sum` that is incomplete for a
  standalone build - which is exactly how the container builds. Follow it with
  `go work sync` at the root.
- **Lint policy is per module.** `libs/beans` uses an explicit allow-list;
  `apps/bean-counter` uses `default: standard` plus extras, and pins its own
  golangci-lint version in its Makefile. Unifying them is deliberate deferred
  work, not an oversight.
- **Images build from the repository root.** `docker build -f
  apps/bean-counter/Dockerfile .` - the build needs `libs/beans`, and the whole
  library tree is copied because `libs/beans/schema` `go:embed`s its migration
  SQL.
- **One tracker, at the root.** Do not run `bd init` inside a module. A new
  application picks an issue prefix distinct from `beans-` and
  `bean-counter-`.
- **One `AGENTS.md` and one `CLAUDE.md`, both at the root.** A new application
  adds a `## apps/<name>` section to `AGENTS.md` rather than its own file.
- **`.agents/` is a historical record.** Plans and reviews under it are dated
  and are not rewritten when paths change.
