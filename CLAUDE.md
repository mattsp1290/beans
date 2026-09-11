# Project Instructions for AI Agents

This file provides instructions and context for AI coding agents working on
this repository. `AGENTS.md` is the fuller reference; this file records the
build commands, the architecture, and the conventions.

## Beans issue tracker

This repository tracks its own issues with `bn`, the binary it builds. Run
`bn prime` for the rules; the short version:

```bash
bn ready                      # available work
bn show <id>                  # issue detail
bn update <id> --claim        # claim work (status in_progress, assignee you)
bn close <id> -r "reason"     # complete work
bn remember "insight"         # persistent knowledge (project memory)
```

`bn` commits and pushes the hub (`~/.beans/hub`) itself on every mutating
command; never commit hub files by hand. The hub is a separate repository
from this one, so the session-completion checklist below only pushes this
repository's code.

- Use `bn` for ALL task tracking; do NOT use TodoWrite, TaskCreate, or
  markdown TODO lists.
- Use `bn remember` for persistent knowledge; do NOT use MEMORY.md files.

## Session completion

When ending a work session, complete every step. Work is NOT complete until
`git push` succeeds.

1. File issues for remaining work (`bn create`).
2. Run quality gates if code changed (`make ci`).
3. Update issue status (`bn close`, `bn update`).
4. Push this repository:
   ```bash
   git pull --rebase
   git push
   git status   # must show "up to date with origin"
   ```
5. Verify the hub is pushed too: `bn status` shows ahead 0.
6. Hand off: leave context for the next session in the issue log
   (`bn note <id> ...`).

## Build & Test

Run from the repository root:

```bash
make ci               # ui-install ui-test ui-check ui-build vet lint test build tidy-check
make build            # bin/bn; embeds whatever ui/dist holds, needs no Node
make test             # go test ./...
make ui-build         # vite build into ui/dist/, picked up by the next make build
make release-build    # build the UI, then the binary that embeds it
bin/bn serve --open   # the issues board and wiki over ~/.beans/hub
```

`cmd/bn` and `internal/server` tests create bare git repositories under the
test temp directory and run the real pipeline against them; the system `git`
must be on PATH.

## Architecture Overview

One Go module, `github.com/mattsp1290/beans`, building one binary `bn`. The
hub is a git repository cloned at `~/.beans/hub` (an Obsidian vault); every
project's issues live under `projects/<name>/`.

| Path | What it is |
| --- | --- |
| `cmd/bn/` | cobra + fang entry point and every command |
| `issue/` | issue and memory model, the round-trip-safe frontmatter codec, ids, log lines, templates, config |
| `plan/` | validated project plan bundles, lifecycle, sections, and change graph schema |
| `vault/` | hub paths, project resolution, remote-URL normalization, the in-memory index, queries, watcher |
| `gitops/` | the git write pipeline: lock, fetch throttle, rebase, commit, push with replay |
| `markdown/` | goldmark renderer for Obsidian-flavored markdown (wikilinks, embeds, callouts, highlight) |
| `internal/ops/` | every mutation as a replay-safe operation, shared by the CLI and the server |
| `internal/server/` | Fiber v3 JSON API, SSE reload events, embedded UI |
| `ui/` | Svelte 5 app; `ui/embed.go` embeds `ui/dist` |
| `docs/` | `format.md` (normative file format), `prime.md` (agent rules), `decisions.md`, `release.md`, `beans.toml.example` |
| `version/` | build-time version string |

Write path for every mutation (CLI or UI): resolve hub and project, take
`cache/hub.lock`, commit stray hand edits, fetch and rebase, apply the
operation, commit `bn: <verb> <id> — <summary>` with a `Bn-Run` nonce
trailer, push; on rejection fetch, rebase, and re-derive the operation on
the new tip (up to three attempts). Only the commit this run created can
ever be discarded. Details and invariants: `gitops/hub.go`.

CI is one workflow, `.github/workflows/ci.yml`: the `ui` job builds the app
and uploads `ui/dist`; the `go` job downloads it, then vets, lints, tests,
builds, and checks `go mod tidy`. `main` is not protected; if a required
check is added, require the `go` and `ui` jobs.

## Conventions & Patterns

- **Public packages** `vault`, `issue`, `markdown`, `gitops` live at the
  module root; `internal/ops`, `internal/server`, and `cmd/bn` are private.
  No stability commitment exists until an external consumer does.
- **`docs/format.md` is normative.** The `issue` codec must keep
  `Encode(Parse(x)) == x` for every valid file; a mutation touches only the
  lines it changes. Fixtures under `issue/testdata/roundtrip/` are the gate.
- **Operations are replay-safe.** An `internal/ops` `Apply` re-reads the
  files it changes every time it runs; the pipeline may run it more than
  once after a push race.
- **The hub is the source of truth.** No database, no JSONL except the
  one-time `bn import bd` path, no derived cache (measured: loading 5,000
  issues takes about a third of a second).
- **`ui/dist/index.html` is a placeholder.** `make ui-build` overwrites it
  locally; never commit the built one. The rest of `ui/dist/` is gitignored.
- **Tidy at the root.** `go mod tidy` then `git diff --exit-code go.mod
  go.sum` is the `tidy-check` target.
- **One lint policy**, `.golangci.yml`, an explicit allow-list.
- **One `AGENTS.md` and one `CLAUDE.md`, both at the root.**
- **`.agents/` is a historical record.** Plans and reviews under it are
  dated, untracked, and not rewritten when paths change.
