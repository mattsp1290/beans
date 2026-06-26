# beans

`beans` is a database-backed issue tracker with a `bd`/beads-like CLI surface.
The binary is `bn`. The store supports PostgreSQL, MySQL, and SQLite through an
explicit database driver setting.

## Build

```bash
make build
make install
```

## Quickstart

Set `BN_DRIVER` and `BN_DSN`, initialize a project prefix, then use the issue
commands:

```bash
export BN_DRIVER=sqlite
export BN_DSN='file:beans.db?_pragma=foreign_keys(1)'
bn init --prefix demo
bn create "wire the tracker" -p 2
bn ready
bn list
bn show demo-abc123
bn update demo-abc123 --claim
bn close demo-abc123
```

Migrations run automatically the first time the store is opened
(`store.New` calls `schema.Migrate`). `bn init --prefix <project>` registers the
project and writes the active project marker.

### Database Configuration

`BN_DRIVER` selects the database dialect and must be one of `postgres`, `mysql`,
or `sqlite`. Accepted aliases are `postgresql`, `pg`, and `sqlite3`. For
backwards compatibility, `BN_DRIVER` may be omitted only when `BN_DSN` is
clearly a PostgreSQL URL or keyword DSN.

Examples:

```bash
BN_DRIVER=postgres BN_DSN='postgres://user:pass@localhost:5432/beans?sslmode=disable'
BN_DRIVER=mysql BN_DSN='user:pass@tcp(localhost:3306)/beans?charset=utf8mb4&parseTime=true&loc=UTC'
BN_DRIVER=sqlite BN_DSN='file:beans.db?_pragma=foreign_keys(1)'
```

SQLite uses a pure-Go driver and is the default no-Docker development and test
path. MySQL DSNs should use `parseTime=true` and `loc=UTC` so timestamps scan
and compare consistently.

### Status Workflow Configuration

The set of issue statuses is configurable per deployment. With no config file,
`bn` uses the built-in vocabulary:

```
open  in_progress  ready_for_review  ready_for_validation  ready_for_merge  blocked  closed  done
```

`open` is the only **active** (dispatchable) status surfaced by `bn ready`;
`closed` and `done` are **terminal** (they satisfy blockers and signal cleanup).
The three `ready_for_*` states are **hold** states — in flight, so they are
neither dispatched by `ready` nor counted as done.

To customize, drop a `bn.toml` or `bn.yaml` next to your project (it is
discovered by walking up from the working directory) or point `BN_CONFIG` at an
explicit path. Discovery precedence:

1. `BN_CONFIG=/path/to/bn.toml` (explicit; missing file is an error)
2. `bn.toml` / `bn.yaml` / `bn.yml` found walking up from the working directory
3. `$XDG_CONFIG_HOME/bn/config.{toml,yaml,yml}` (or `~/.config/bn/...`)

Example `bn.toml` (see [`docs/bn.toml.example`](docs/bn.toml.example)):

```toml
[workflow]
statuses = ["open", "in_progress", "ready_for_review", "ready_for_validation", "ready_for_merge", "blocked", "closed", "done"]
default  = "open"          # status assigned to new issues
active   = ["open"]         # dispatchable (bn ready)
terminal = ["closed", "done"] # satisfy blockers, trigger cleanup
```

Any omitted key inherits the built-in default. Validation runs at startup: an
invalid config (empty vocabulary, a `default`/`active`/`terminal` value not in
`statuses`, or active/terminal overlap) fails fast with an error. Statuses are
validated in the application layer — `create`, `update --status`, and `import`
reject values outside the configured vocabulary (write-strict), while existing
rows carrying an unknown status still display (read-tolerant).

## Multi-Repository Workflow

`bn` supports work spanning multiple git repositories within a single shared
database. The current repository is auto-detected from the git remote URL; no
`--repo` flag is required for everyday use.

### How it works

- **Topology**: Each registered repository gets its own project prefix equal to
  its slug (derived from the remote URL). This means all existing per-prefix
  queries continue to work unchanged — `list`, `ready`, `dep tree`, and `dep
  cycles` all scope to the current repository by default.
- **Auto-detect**: When you run `bn create` (or any command that needs repo
  context), `bn` reads the `git config --get remote.origin.url` value,
  normalizes it, and auto-registers the repo on first use. SCP form
  (`git@github.com:org/repo`), HTTPS, and SSH URLs for the same physical repo
  resolve to the same entry.
- **Local-only repos** (no remote) get a synthetic `file:///` URL key so they
  can still be registered and tracked.
- **Creation commit**: Issues linked to a repo store the exact cwd `HEAD`
  commit at creation time in `creation_commit` when the cwd repo identity
  matches the selected issue repo. Dirty state is not recorded.

Creation-commit capture follows repo selection. With normal auto-detect, the
auto-detected cwd repo is selected and its HEAD is stored. With an active `.bn`
repo marker or an explicit `--repo`, that selected repo still captures HEAD only
when the current directory resolves to the same registered repo row. Local-only
repos compare by the synthesized `file:///abs/git-toplevel` identity. If cwd is
outside git, HEAD is unavailable, git returns an unusable object ID, or cwd is a
different registered repo, issue creation still succeeds and `creation_commit`
is left empty. The field is immutable issue routing metadata on
`bn_issue_repos`, not mutable repo registry state.

### Example: two repos sharing a database

```bash
# Shared store (both repos use the same BN_DSN)
export BN_DRIVER=sqlite
export BN_DSN='file:/tmp/shared.db?_pragma=foreign_keys(1)'

# In repo-a/
cd ~/repos/my-api
bn create "add /health endpoint" -p 1
#  → auto-registers github.com/org/my-api, creates my-api-abc123

# In repo-b/
cd ~/repos/my-frontend
bn create "wire /health status badge" -p 2
#  → auto-registers github.com/org/my-frontend, creates my-frontend-xyz789

# Back in repo-a/ — only sees my-api issues by default
bn list
bn ready

# Cross-repo view
bn list --all-repos
bn ready --all-repos

# Explicit repo override (slug form)
bn list --repo my-frontend

# ID-addressed commands are always cross-repo
bn show my-frontend-xyz789
bn dep add my-frontend-xyz789 my-api-abc123   # frontend waits on API
```

### Flag reference

| Flag | Commands | Effect |
|------|----------|--------|
| _(none)_ | list, ready, dep tree/cycles | Scope to current repo (from cwd git remote) |
| `--all-repos` | list, ready, dep tree/cycles | Return issues from every registered repo |
| `--repo <slug>` | list, ready, dep tree/cycles | Scope to the named repo (read-only, no auto-register) |
| `--repo <slug>` | create | Link the issue to an already-registered repo slug |

Auto-registration on `bn create` is automatic: no `--repo` flag is needed. `bn`
detects the git remote and registers the repo if it has not been seen before.

ID-addressed commands (`show`, `update`, `close`, `delete`, `dep add/remove`)
look up the issue by ID across all repos — `GetIssue` applies no prefix filter.
They do require project context (provided automatically when inside any registered
git repo directory), but the lookup itself is cross-repo.

## Repo Routing

The repo registry commands manage repository targets attached to issues:

```bash
bn repo admin add "$USER" --bootstrap
bn repo add app --remote git@github.com:example/app.git --auth-ref ssh-key:github-default
bn repo list
bn repo doctor app
bn create "fix app build" --repo app --requested-ref main
```

## Memories

```bash
bn remember "prefer small migration steps" --tag process
bn memories process
```

Memory search uses each backend's search support:

- PostgreSQL: `tsvector`/`plainto_tsquery` ranking.
- MySQL: `FULLTEXT` / `MATCH ... AGAINST` natural-language ranking.
- SQLite: FTS5 with `bm25` ranking.

Tokenization and ranking can differ by dialect. When recall matters more than
ranking, narrow results with `--type`, repeated `--tag`, or project scope.

## Importing Legacy Beads Issues

`bn import` accepts issue JSONL from `github.com/gastownhall/beads` `bd export`
output, verified against `bd version 1.0.0 (72170267)`. For a one-time cutover
from a stopped legacy beads store, export issues without memories and import
them into the target beans project:

```bash
cd ~/git/local-symphony
bd export --no-memories -o legacy-beads-issues.jsonl

export BN_DRIVER=postgres
export BN_DSN='postgres://user:pass@host:5432/beans?sslmode=disable'
export BN_PROJECT=local-symphony
bn import --mode=create-only legacy-beads-issues.jsonl
bn ready
```

When exporting from a mounted production store instead of the repo root, pass
the embedded-Dolt database path explicitly:

```bash
bd --db /var/lib/symphony/beads/local-symphony/.beads/embeddeddolt export --no-memories -o legacy-beads-issues.jsonl
```

The expected JSONL shape is one issue object per line with fields such as
`id`, `title`, `description`, `status`, `priority`, `issue_type`, `labels`, and
`dependencies`. Dependency entries are objects with `issue_id`,
`depends_on_id`, and `type`; only `type:"blocks"` edges whose `issue_id` matches
the containing issue are imported. `status` maps to the beans issue state, and
priority values are used as-is (`0` critical through `4` backlog). Exported
fields without beans storage, including `owner`, `created_by`, `close_reason`,
timestamps, and count fields, are ignored.

The default `create-only` mode is safe to re-run: existing issues are skipped,
dependency edges are not duplicated, and an already-terminal beans issue is not
reopened by active legacy export state. Use `--mode=merge` only when you
intentionally want to refresh non-state fields from the legacy export; merge mode
still preserves existing terminal states when incoming legacy state is active.

## Testing

Default local checks do not require Docker:

```bash
make test
make vet
make lint
make build
make ci
go test ./...
```

Docker-backed integration tests use testcontainers for PostgreSQL and MySQL and
also exercise the SQLite integration path:

```bash
go test -tags=integration ./...
```

Run the integration suite where Docker is available. The normal `go test ./...`
path remains container-free.

## Library Packages

Beans also exposes the packages used by downstream orchestration code:

- `github.com/mattsp1290/beans/model` for issue-domain structs.
- `github.com/mattsp1290/beans/repo` for repo target validation.
- `github.com/mattsp1290/beans/schema` for embedded goose migrations.
- `github.com/mattsp1290/beans/store` for the multi-database CRUD store.
- `github.com/mattsp1290/beans/version` for the `bn` build version.

The `bn_*` table names and the `bn` binary name are stable compatibility
contracts.
