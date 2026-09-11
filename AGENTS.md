# Agent Instructions

This repository is one Go module, `github.com/mattsp1290/beans`, that builds
`bn`: a git-backed issue tracker and wiki for humans and coding agents.
Issues and docs are markdown files in one git repository, the hub, cloned at
`~/.beans/hub`; `bn serve` puts an issues board and a wiki over it.

## Repository layout

```text
beans/
├── go.mod                        module github.com/mattsp1290/beans
├── Makefile                      build, test, vet, lint, ui-*, ci, release-build
├── .golangci.yml                 one lint policy for the whole module
├── .github/workflows/ci.yml      one workflow, jobs `ui` and `go`
├── cmd/bn/                       cobra + fang entry point and every command
├── issue/                        issue model, frontmatter codec, ids, log lines, templates, config
├── plan/                         first-class plan bundle model, validation, lifecycle, graph schema
├── vault/                        hub paths, project resolution, index, queries, watcher
├── gitops/                       git resolver seam and the hub write pipeline
├── markdown/                     goldmark renderer for Obsidian-flavored markdown
├── internal/ops/                 mutations as replay-safe operations (CLI and server)
├── internal/server/              Fiber v3 API, SSE, embedded UI serving
├── ui/                           Svelte 5 app; ui/embed.go embeds ui/dist
├── version/                      build-time version string
├── docs/                         format.md, prime.md, decisions.md, release.md, beans.toml.example
└── .agents/                      plans and dated records (untracked)
```

Public packages `vault`, `issue`, `gitops`, and `markdown` sit at the module
root so a future consumer can import them; `internal/ops`, `internal/server`,
and `cmd/bn` are private glue.

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

## Issue tracking

This repository's issues live in the hub under `projects/beans/`. Run
`bn prime` for the rules; `bn ready`, `bn show <id>`, `bn update <id>
--claim`, `bn close <id> -r "reason"` are the daily loop. `bn` commits and
pushes the hub itself; never commit hub files by hand. `CLAUDE.md` has the
session-completion checklist.

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

Issue statuses come from `issue.WorkflowConfig`, loaded with the precedence
`BN_CONFIG` > project `beans.toml` `[workflow]` > hub `beans.toml`
`[workflow]` > built-in defaults, merged per key. Defaults include
`ready_for_review`, `ready_for_validation`, and `ready_for_merge` as hold
states: valid statuses that `bn ready` never returns and that do not satisfy
blockers. See `docs/beans.toml.example`.

## Versioning

`Makefile` derives `VERSION` from `git describe --tags --match 'v*'` and links
it into `version.Version`; `bn --version` prints it. The `LDFLAGS` path must
match the module path exactly: a wrong path is ignored by the linker with no
build error and `bn --version` silently prints the `dev` default. Releases
are tagged `vX.Y.Z` on `main`; see `docs/release.md`.
