# beans

`bn` (beans) is a git-backed issue tracker and wiki for humans and coding
agents. Issues, docs, memories, requests, plans, and session handoffs are markdown files with YAML frontmatter
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
# From a source checkout with Rust 1.98.1 and Node 24:
make release-build
# Install the same complete app into Cargo's configured bin directory:
make install
# Equivalent direct Cargo install, after building the UI:
make ui-install ui-build
cargo install --locked --path .
```

`bn` needs system `git` on PATH. Assets are embedded during compilation;
`make build` works without Node using the committed placeholder, while
`make release-build` and `make install` embed the complete board and wiki.
No package registry release is currently published.

## Quick start

```bash
bn init git@github.com:you/beans-hub.git       # clone the hub repository
cd ~/git/myapp                               # the project is the repository you are in
issue=$(bn create "Fix the login redirect" -p 1 -l bug)
bn ready                                     # issues with no open blockers
bn update "$issue" --claim
bn close "$issue" -r "shipped in 4c1d2e"
bn plan init "Add authentication" --output ./auth-plan
bn plan validate ./auth-plan
bn plan put ./auth-plan
bn serve --host 127.0.0.1 --port 3000          # leave this running in its terminal
# Open http://127.0.0.1:3000 in your browser; use a second terminal for bn prime.
bn prime                                     # the rules, for agents
```

## Hub layout

```text
~/.beans/
├── config.toml                  actor, hub.remote, hub.branch, fetch.throttle, git policy
├── cache/                       legacy evidence only
├── .beans-state/hub/            clone-scoped lock, stamps, operation journal
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
        ├── handoffs/<id>-<slug>.md
        ├── handoffs/archive/<YYYY>/<id>-<slug>.md
        ├── plans/<id>-<slug>/plan.md
        └── templates/<type>.md
```

The complete [CLI command/flag reference](docs/cli.md) covers output, exit codes and shell/browser integration; the [server reference](docs/server.md) covers HTTP, rendering and filesystem security.

The file format is specified in [`docs/format.md`](docs/format.md); `bn`
preserves every key, comment, and line it does not own, so hand edits in
Obsidian or any editor are first class. The next `bn` command commits them
as `bn: hand edits`.

Requests are durable project-scoped Markdown artifacts: use `bn request
create`, then `bn request link` to associate work. Their fixed lifecycle is
managed from the CLI; `bn serve` offers read-only browsing, search, and issue
relationship summaries.

Plans can track execution without changing their authored lifecycle: publish a
bundle, create its issues, run `bn plan link PLAN NODE ISSUE`, then `bn sync`
and `bn plan status PLAN --json`. A stale `plan put` is rejected; retrieve and
merge the latest bundle rather than overwriting newer bindings.

## Development

```bash
make ci               # UI and native checks: UI tests and build, vet, lint, Rust tests, build, tidy check
make build            # bin/bn embedding whatever ui/dist holds
```

Decisions and their reasons are in [`docs/decisions.md`](docs/decisions.md).
`AGENTS.md` and `CLAUDE.md` are the instructions for AI coding agents.

## License

MIT. See [LICENSE](LICENSE).

Clone identity, process budgets, failure recovery and diagnostic timings are
documented in [Git write coordination](docs/git-coordination.md).
