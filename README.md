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
