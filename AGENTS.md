# Agent Instructions

This repository is one Go module, `github.com/mattsp1290/beans`, that builds
the `bn` binary: a git-backed issue tracker and wiki. It is mid-way through
the hub vault redesign (plan: `.agents/plans/hub-vault-redesign/`, untracked);
the module collapse (WP1) is done and later work packages add behaviour.

## Repository layout

```text
beans/
├── go.mod                        module github.com/mattsp1290/beans
├── Makefile                      build, test, vet, lint, ui-*, ci, release-build
├── .golangci.yml                 one lint policy for the whole module
├── .github/workflows/ci.yml      one workflow, jobs `go` and `ui`
├── cmd/bn/                       cobra + fang entry point and commands
├── issue/                        issue model, workflow config, codec (WP2)
├── vault/                        hub and project resolution, remote-URL normalization, index (WP4)
├── gitops/                       git resolver seam; hub write pipeline (WP3)
├── internal/server/              Fiber v3 API and embedded UI serving (WP6)
├── ui/                           Svelte 5 app; ui/embed.go embeds ui/dist
├── version/                      build-time version string
├── docs/                         format spec, prime text, beans.toml example
├── .beads/                       this repository's issue tracker (bd) until WP7
└── .agents/                      plans and dated records (untracked)
```

Public packages `vault`, `issue`, `gitops`, and (from WP4) `markdown` sit at the module
root so a future consumer can import them; only `internal/server` and
`cmd/bn` are private glue.

## Commands

From the repository root:

```bash
make ci               # ui-install ui-test ui-check ui-build vet lint test build tidy-check
make build            # go build -o bin/bn (embeds whatever ui/dist holds; no Node needed)
make test             # go test ./...
make ui-build         # vite build into ui/dist/, picked up by the next make build
make release-build    # ui-install ui-build build
```

`ui/dist/index.html` is a committed placeholder so `go build` works without
Node; `make ui-build` overwrites it locally and the rest of `ui/dist/` is
gitignored. Do not commit a built `index.html`.

## Non-Interactive Shell Commands

**ALWAYS use non-interactive flags** with file operations to avoid hanging on
confirmation prompts.

Shell commands like `cp`, `mv`, and `rm` may be aliased to include `-i`
(interactive) mode on some systems, causing the agent to hang indefinitely
waiting for y/n input.

**Use these forms instead:**

```bash
# Force overwrite without prompting
cp -f source dest           # NOT: cp source dest
mv -f source dest           # NOT: mv source dest
rm -f file                  # NOT: rm file

# For recursive operations
rm -rf directory            # NOT: rm -r directory
cp -rf source dest          # NOT: cp -r source dest
```

**Other commands that may prompt:**

- `scp` - use `-o BatchMode=yes` for non-interactive
- `ssh` - use `-o BatchMode=yes` to fail instead of prompting
- `apt-get` - use `-y` flag
- `brew` - use `HOMEBREW_NO_AUTO_UPDATE=1` env var

## Workflow configuration

Issue statuses are driven by `issue.WorkflowConfig`. Defaults include
`ready_for_review`, `ready_for_validation`, and `ready_for_merge` as hold
states: valid statuses that `bn ready` never returns and that do not satisfy
blockers. WP2 defines where the config is read from (hub and project
`beans.toml`, `BN_CONFIG`). See `docs/beans.toml.example`.

## Versioning

`Makefile` derives `VERSION` from `git describe --tags --match 'v*'` and links
it into `version.Version`. `bn --version` prints it. The `LDFLAGS` path must
match the module path exactly: a wrong path produces an empty version string
with no build error. The first tag of the collapsed module will be `v0.2.0`
(WP7).

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
