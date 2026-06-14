# GORM Multi-Database Rollback and Validation Notes

Issue: `beans-3b7`

These notes describe how to roll back or validate the one-way migration from the
original Postgres/pgx-only store toward the GORM-backed, multi-database store.
There is no dual-write or data migration path to maintain; backwards
compatibility with the old persistence internals was explicitly dropped.

## Rollback Strategy

Use a normal Git revert branch rather than trying to partially toggle runtime
behavior:

```bash
git checkout main
git pull --ff-only origin main
git checkout -b rollback/gorm-multidb
git revert <merge-sha-or-range>
make ci
go test -tags=integration ./...
git push -u origin rollback/gorm-multidb
```

Prefer reverting the merge commits that introduced the GORM/multi-database
work, newest first. If a direct revert conflicts, resolve by restoring the
previous Postgres-only versions of the affected areas listed below, then rerun
the full validation gates before publishing the branch.

Do not attempt an in-place rollback against a live database without an operator
decision. The migration now owns separate Postgres, MySQL, and SQLite migration
directories, and rollback expectations differ per backend. Treat schema rollback
as a code rollback plus database restore from backup.

## Files and Areas to Inspect

The migration primarily touched these areas:

- `store/`: GORM models, pool/config wiring, driver-specific query behavior,
  error normalization, memory search, repo registry behavior, and the shared
  store contract tests.
- `schema/`: driver-aware migration selection, migration locking, and
  `schema/migrations/{postgres,mysql,sqlite}/`.
- `cmd/bn/`: `BN_DRIVER`/`BN_DSN` wiring and dialect-neutral command help.
- `go.mod` and `go.sum`: GORM, datatypes, MySQL, SQLite, and testcontainers
  dependencies.
- `docs/research/` and `docs/prompts/`: migration design and verification
  notes.

## Dependency Rollback

After reverting code, run:

```bash
go mod tidy
git diff -- go.mod go.sum
make ci
```

Only remove dependencies when no imports remain. At the time these notes were
written, `pgx` and `puddle` are still intentionally imported for transitional
Postgres paths: DSN parsing, legacy pgx pool support, Postgres error
classification, pool-closed normalization, and Postgres integration tests.
`goose` remains the migration runner.

## Validation Checkpoints

No-Docker local validation:

```bash
make test
make vet
make lint
make build
make ci
go test ./...
```

Docker-backed validation:

```bash
go test -tags=integration ./...
```

Expected coverage:

- Default `go test ./...` runs without Docker and exercises the SQLite/default
  store path.
- `go test -tags=integration ./...` starts Postgres and MySQL testcontainers and
  also runs the SQLite integration path.
- The cross-dialect store contract covers issue CRUD, dependencies, imports,
  repos, audit rows, memories, timestamps, close behavior, concurrency guards,
  and selected error semantics.
- Schema tests cover migration ordering, dialect-specific migration roots,
  required table/index/constraint presence, migration lockers, and SQLite
  migration behavior.

Latest verified state:

- `make test`, `make vet`, `make lint`, `make build`, and `make ci` passed.
- `go test ./...` passed without Docker.
- `go test -tags=integration ./...` passed with Postgres and MySQL
  testcontainers available.

## Follow-Up Notes

Post-migration cleanup should be handled separately from rollback. In
particular, pruning `pgx`, `puddle`, or `goose` should wait until the relevant
imports are actually gone and a fresh `go mod tidy` plus full validation passes.
