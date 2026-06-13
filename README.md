# beans

`beans` is a Postgres-backed issue tracker with a `bd`/beads-like CLI surface.
The binary is `bn`.

## Build

```bash
make build
make install
```

## Quickstart

`bn` requires Postgres. Set `BN_DSN`, initialize a project prefix, then use the
issue commands:

```bash
export BN_DSN=postgres://user:pass@localhost:5432/beans
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

## Library Packages

Beans also exposes the packages used by downstream orchestration code:

- `github.com/mattsp1290/beans/model` for issue-domain structs.
- `github.com/mattsp1290/beans/repo` for repo target validation.
- `github.com/mattsp1290/beans/schema` for embedded goose migrations.
- `github.com/mattsp1290/beans/store` for the pgx-backed CRUD store.
- `github.com/mattsp1290/beans/version` for the `bn` build version.

The `bn_*` table names and the `bn` binary name are stable compatibility
contracts.
