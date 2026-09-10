# 01 — Target layout and module graph

This file is the authoritative mapping from every currently tracked path to its
post-migration location. [02-history-migration.md](02-history-migration.md) and
[03-go-module-restructure.md](03-go-module-restructure.md) execute this mapping.

Paths written as `beans:<path>` are relative to `$HOME/git/beans`. Paths written as
`bean-counter:<path>` are relative to `$HOME/git/bean-counter`. Destination paths are
relative to the monorepo root, which is the existing beans repository root.

## Module identity

| Module | Directory | Module path after migration |
| --- | --- | --- |
| beans library and `bn` CLI | `libs/beans/` | `github.com/mattsp1290/beans/libs/beans` |
| bean-counter API and UI | `apps/bean-counter/` | `github.com/mattsp1290/beans/apps/bean-counter` |
| future Postgres application | `apps/<name>/` | `github.com/mattsp1290/beans/apps/<name>` |

There is no `go.mod` at the repository root. The root holds `go.work` only. This is
deliberate: a root module would claim the `github.com/mattsp1290/beans` path and make
the nested module paths ambiguous to `go list`.

## beans repository — destination for each tracked path

`git ls-files` at `$HOME/git/beans` currently reports these root-level entries plus the
directory trees below them.

### Moves to `libs/beans/`

| Source | Destination |
| --- | --- |
| `beans:go.mod` | `libs/beans/go.mod` |
| `beans:go.sum` | `libs/beans/go.sum` |
| `beans:deps.go` | `libs/beans/deps.go` |
| `beans:.golangci.yml` | `libs/beans/.golangci.yml` |
| `beans:Makefile` | `libs/beans/Makefile` |
| `beans:README.md` | `libs/beans/README.md` |
| `beans:cmd/` | `libs/beans/cmd/` |
| `beans:model/` | `libs/beans/model/` |
| `beans:repo/` | `libs/beans/repo/` |
| `beans:schema/` | `libs/beans/schema/` |
| `beans:store/` | `libs/beans/store/` |
| `beans:version/` | `libs/beans/version/` |
| `beans:docs/` | `libs/beans/docs/` |

`beans:README.md` becomes the library README. A new monorepo `README.md` is written at
the root in WP9; it is a new file, not a move.

`beans:docs/` contains `bn.toml.example`, `prompts/`, `research/`, and `specs/`. Every
document there describes the library, the `bn` CLI, or its storage model, so the whole
tree moves. Verified by listing: `docs/specs/repo-resolution-precedence.md`,
`docs/specs/topology-a-prefix-equals-slug.md`, `docs/bn.toml.example`, and the twelve
files under `docs/research/`.

`beans:schema/schema.go` line 18 contains `//go:embed migrations/*/*.sql`. The embed
pattern is relative to the containing package directory, so moving `schema/` as a unit
keeps it valid. Do not flatten or split `schema/migrations/`.

### Stays at the repository root

| Path | Reason |
| --- | --- |
| `beans:LICENSE` | One license covers the monorepo |
| `beans:.gitignore` | Merged in WP9; see [06-beads-and-agent-config.md](06-beads-and-agent-config.md) |
| `beans:AGENTS.md` | Merged in WP9 |
| `beans:CLAUDE.md` | Merged in WP9 |
| `beans:setup-beads.sh` | Root tracker tooling |
| `beans:setup-multi-repo-beads.sh` | Root tracker tooling |
| `beans:.beads/` | The single monorepo tracker |
| `beans:.agents/` | Plans, reviews, and bead-swarm history |
| `beans:.claude/` | Local agent settings |
| `beans:.github/workflows/ci.yml` | Replaced in WP6 by three path-scoped workflows |

### Untracked cleanup

`beans:bn` is a 27 MB untracked Mach-O arm64 binary at the repository root. It matches
no `.gitignore` pattern, so it appears in `git status --porcelain`. Delete it before
G1. The merged root `.gitignore` adds a `/bn` entry so a future `go build -o bn` at the
root does not reintroduce the problem.

## bean-counter repository — destination for each tracked path

`git filter-repo --to-subdirectory-filter apps/bean-counter` relocates every path
mechanically. The table below records where each lands and which ones WP3 then removes
or hoists, because the mechanical result is not the final desired tree.

### Kept at `apps/bean-counter/` unchanged

`cmd/`, `internal/`, `test/`, `frontend/`, `deploy/`, `docs/`, `prompts/`, `scripts/`,
`go.mod`, `go.sum`, `Makefile`, `Dockerfile`, `.dockerignore`, `.env.example`,
`docker-compose.yml`, `docker-compose.stack.yml`, `.golangci.yml`, `README.md`.

`go.mod`, `Dockerfile`, `.dockerignore`, `Makefile`, `docker-compose.stack.yml`,
`deploy/docker-compose.prod.yml`, `scripts/deploy-production.sh`, and
`test/scripts/deploy-production_test.sh` all require content edits. Those edits are
specified in [03-go-module-restructure.md](03-go-module-restructure.md) and
[05-containers-and-deploy.md](05-containers-and-deploy.md).

### Removed after the import

| Path after filter-repo | Action | Reason |
| --- | --- | --- |
| `apps/bean-counter/LICENSE` | Delete | The root `LICENSE` covers the monorepo |
| `apps/bean-counter/setup-beads.sh` | Delete | The root tracker is `beans`; this script initializes `bean_counter` |
| `apps/bean-counter/.beads/` | Delete after WP8 verifies the import | Retired per D6 |
| `apps/bean-counter/.github/workflows/ci.yml` | Delete | Superseded by the root workflows in WP6 |
| `apps/bean-counter/.claude/settings.json` | Delete after merging into the root `.claude/settings.json` | One agent settings file per repository |
| `apps/bean-counter/.gitignore` | Delete after merging into the root `.gitignore` | See [06-beads-and-agent-config.md](06-beads-and-agent-config.md) |
| `apps/bean-counter/AGENTS.md` | Delete after merging into the root `AGENTS.md` | See [06-beads-and-agent-config.md](06-beads-and-agent-config.md) |
| `apps/bean-counter/CLAUDE.md` | Delete after merging into the root `CLAUDE.md` | See [06-beads-and-agent-config.md](06-beads-and-agent-config.md) |

Before deleting `apps/bean-counter/setup-beads.sh`, diff it against the root
`setup-beads.sh`. The two files are known to differ. If the bean-counter variant
contains logic the root variant lacks, port that logic into the root script in the same
commit instead of dropping it silently.

`frontend/.gitignore` and `frontend/.dockerignore` are frontend-scoped and stay where
filter-repo puts them.

### Moved out of `apps/bean-counter/`

| Path after filter-repo | Destination | Reason |
| --- | --- | --- |
| `apps/bean-counter/.agents/plans/beans-0-1-1/` | `.agents/plans/bean-counter-beans-0-1-1/` | Plans are a repository-wide concern; the prefix avoids collision with existing root plan names |
| `apps/bean-counter/.agents/plans/deploy/` | `.agents/plans/bean-counter-deploy/` | Same |

`.agents/plans/` at the beans root already contains `bd-cli`,
`configurable-issue-statuses`, `multi-repo-workspace-routing`, `postgres-tracker`, and
three loose files. Neither incoming plan name collides, but the `bean-counter-` prefix
makes ownership legible once a third application adds its own plans.

`bean-counter:.gitignore` ignores `.agents/reviews/`, and bean-counter's tracked file
list confirms no review files were ever committed there. The beans repository does
track `.agents/reviews/`. WP9 resolves this: the root `.gitignore` does not ignore
`.agents/reviews/`, matching the beans behavior, so existing tracked reviews stay
tracked.

## Import path rewrite

Every occurrence of the old library import prefix changes:

```text
github.com/mattsp1290/beans/model    →  github.com/mattsp1290/beans/libs/beans/model
github.com/mattsp1290/beans/repo     →  github.com/mattsp1290/beans/libs/beans/repo
github.com/mattsp1290/beans/schema   →  github.com/mattsp1290/beans/libs/beans/schema
github.com/mattsp1290/beans/store    →  github.com/mattsp1290/beans/libs/beans/store
github.com/mattsp1290/beans/version  →  github.com/mattsp1290/beans/libs/beans/version
```

Known non-Go occurrences of the old path that a `--include="*.go"` grep would miss:

| File | Occurrence |
| --- | --- |
| `beans:Makefile` line 4 | `LDFLAGS := -X github.com/mattsp1290/beans/version.Version=$(VERSION)` |
| `beans:.golangci.yml` line 40 | `local-prefixes: - github.com/mattsp1290/beans` |
| `beans:README.md` lines 300-304 | Five consumer-facing import examples |
| `bean-counter:scripts/deploy-production.sh` line 54 | `BEANS_MODULE="github.com/mattsp1290/beans"` |

Historical documents under `beans:.agents/reviews/` also contain the old path. Those are
dated records of past reviews. Do not rewrite them. WP4's verification grep must exclude
`.agents/` for this reason, and must not exclude anything else.

## Acceptance criteria for this mapping

1. After WP3, `git ls-files` at the repository root lists no `.go` file outside
   `libs/beans/` and `apps/bean-counter/`.
2. After WP3, the repository root contains exactly these tracked files at depth 1:
   `.gitignore`, `AGENTS.md`, `CLAUDE.md`, `LICENSE`, `Makefile`, `README.md`,
   `go.work`, `go.work.sum`, `setup-beads.sh`, `setup-multi-repo-beads.sh`.
3. `libs/beans/schema/migrations/` contains the `mysql/`, `postgres/`, and `sqlite/`
   subdirectories, and `go test ./schema/...` passes inside `libs/beans`.
4. No file under `apps/bean-counter/` references a path beginning `../../..` or an
   absolute path under `$HOME`, except the deliberately unexpanded `'$HOME/...'`
   literals in `scripts/deploy-production.sh` documented in
   [05-containers-and-deploy.md](05-containers-and-deploy.md).
