# beans

`bn` (beans) is a git-backed issue tracker and wiki for humans and coding
agents. Issues, docs, memories, and plans are markdown files with YAML frontmatter
in one git repository, the hub, cloned at `~/.beans/hub`. Every `bn` command
that changes something makes one commit and pushes it, so git is the source
of truth across machines and every change has an author, a time, and a
reason.

The hub is a valid Obsidian vault: issues are notes, `blocked_by` and
`parent` are wikilinks that show up in the graph view, and docs are plain
markdown pages. It also reads well on GitHub without `bn`.

`bn serve` runs a local web app over the hub: an issues board grouped by
workflow status, an issue page with rendered markdown, blockers, children,
backlinks, and log, a dependency graph, a wiki with a page tree and
backlinks, and search. Edits made in the UI go through the same commit
pipeline as the CLI, and edits made in an editor show up in the UI within a
second.

## Install

```bash
go install github.com/mattsp1290/beans/cmd/bn@latest   # after a tagged release
# or, from a checkout:
make release-build && cp bin/bn ~/bin/
```

`bn` needs `git` on PATH. Only `go install` builds without the UI; use
`make release-build` for a binary that serves the board.

## Quick start

```bash
bn init git@github.com:you/beans-hub.git    # clone (or initialize) the hub
cd ~/git/myapp                               # the project is the repository you are in
bn create "Fix the login redirect" -p 1 -l bug
bn ready                                     # issues with no open blockers
bn update myapp-a3f2 --claim
bn close myapp-a3f2 -r "shipped in 4c1d2e"
bn plan init "Add authentication" --output ./auth-plan
bn plan validate ./auth-plan
bn plan put ./auth-plan
bn serve --open                              # the board and wiki in a browser
bn prime                                     # the rules, for agents
```

## Hub layout

```text
~/.beans/
├── config.toml                  actor, hub.remote, hub.branch, fetch.throttle
├── cache/                       lock, fetch timestamps, operation journal
└── hub/                         git clone; one Obsidian vault
    ├── beans.toml               [workflow] [types] [ids]
    ├── docs/                    hub-wide wiki
    ├── memories/                hub-wide memories
    └── projects/<name>/
        ├── beans.toml           name, prefix, remotes
        ├── issues/<id>-<slug>.md
		├── requests/<id>-<slug>.md
        ├── archive/<YYYY>/<id>-<slug>.md
        ├── docs/
        ├── memories/<key>.md
		├── plans/<id>-<slug>/plan.md
        └── templates/<type>.md
```

The file format is specified in [`docs/format.md`](docs/format.md); `bn`
preserves every key, comment, and line it does not own, so hand edits in
Obsidian or any editor are first class. The next `bn` command commits them
as `bn: hand edits`.

Requests are durable project-scoped Markdown artifacts: use `bn request
create`, then `bn request link` to associate work. Their fixed lifecycle is
managed from the CLI; `bn serve` offers read-only browsing, search, and issue
relationship summaries.

## Development

```bash
make ci               # everything CI runs: UI tests and build, vet, lint, Go tests, build, tidy check
make build            # bin/bn embedding whatever ui/dist holds
```

Decisions and their reasons are in [`docs/decisions.md`](docs/decisions.md).
`AGENTS.md` and `CLAUDE.md` are the instructions for AI coding agents.

## License

MIT. See [LICENSE](LICENSE).
