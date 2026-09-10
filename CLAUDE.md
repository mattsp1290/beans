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
make ci               # ui-install ui-test ui-check ui-build vet lint test build tidy-check
make build            # bin/bn; embeds whatever ui/dist holds, needs no Node
make test             # go test ./...
make ui-build         # vite build into ui/dist/
```

## Architecture Overview

One Go module, `github.com/mattsp1290/beans`, building one binary `bn`.

| Path | What it is |
| --- | --- |
| `cmd/bn/` | cobra + fang entry point and commands |
| `issue/` | issue model, workflow config, frontmatter codec (WP2) |
| `vault/` | hub and project resolution, remote-URL normalization, index and queries (WP4) |
| `gitops/` | git resolver seam; hub lock, fetch, commit, push pipeline (WP3) |
| `internal/server/` | Fiber v3 API and embedded UI serving (WP6) |
| `ui/` | Svelte 5 app; `ui/embed.go` embeds `ui/dist` |
| `version/` | build-time version string |

CI is one workflow, `.github/workflows/ci.yml`, with jobs `go` and `ui` and no
path filters. If a required status check is configured, require the `go` and
`ui` jobs of `ci.yml`.

The full design is in `.agents/plans/hub-vault-redesign/` (untracked). The
earlier two-module layout (`libs/beans`, `apps/bean-counter`) is history only.

## Conventions & Patterns

- **Public packages** `vault`, `issue`, `gitops`, and (from WP4) `markdown` live at the module
  root; `internal/server` and `cmd/bn` are private glue.
- **Tidy at the root.** `go mod tidy` then `git diff --exit-code go.mod go.sum`
  is the `tidy-check` target.
- **One lint policy**, `.golangci.yml`, an explicit allow-list.
- **`ui/dist/index.html` is a placeholder.** `make ui-build` overwrites it
  locally; never commit the built one. The rest of `ui/dist/` is gitignored.
- **One tracker, at the root.** `.beads/` is this repository's tracker until
  WP7 migrates it into the hub.
- **One `AGENTS.md` and one `CLAUDE.md`, both at the root.**
- **`.agents/` is a historical record.** Plans and reviews under it are dated
  and are not rewritten when paths change.
