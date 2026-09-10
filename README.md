# beans

`bn` (beans) is a git-backed issue tracker and wiki for humans and coding
agents. Issues and docs are markdown files in one git repository, the hub;
`bn serve` puts an issues board and a wiki over it. The redesign is in
progress; this README is rewritten when it lands (`v0.2.0`).

```text
beans/
├── go.mod                        module github.com/mattsp1290/beans
├── Makefile                      build, test, vet, lint, ui-*, ci, release-build
├── .github/workflows/ci.yml      one workflow, jobs `go` and `ui`
├── cmd/bn/                       the bn binary
├── issue/  vault/  gitops/            public packages (markdown/ arrives in WP4)
├── internal/server/              HTTP API and embedded UI serving
├── ui/                           Svelte 5 app, embedded via ui/embed.go
├── version/                      build-time version string
└── docs/                         format spec, prime text, config example
```

## Build

```bash
make ci               # ui-install ui-test ui-check ui-build vet lint test build tidy-check
make build            # bin/bn (embeds whatever ui/dist holds; no Node needed)
make release-build    # build the UI, then the binary that embeds it
```

## History

Until 2026-09-10 this repository was a two-module workspace (`libs/beans`, a
GORM store and the `bn` CLI, and `apps/bean-counter`, a Fiber API and Svelte
UI). The pre-monorepo `v0.1.0` and `v0.1.1` tags predate both layouts.

## Issue tracking

This repository's own issues are tracked with beads (`bd`) until the redesign
migrates them into the hub.

```bash
bd ready              # available work
bd show <id>          # issue detail
```

## Agents

`AGENTS.md` and `CLAUDE.md` at the root are the instructions for AI coding
agents. There is exactly one of each.

## License

MIT. See [LICENSE](LICENSE).
