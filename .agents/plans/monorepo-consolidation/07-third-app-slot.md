# 07 — The third application slot

The user stated that one more Postgres-backed application will join this monorepo. This
plan does not create it. This file records the conventions that application will follow,
so the migration produces a structure it fits into rather than one it has to fight.

Nothing in this file is implemented by this plan. Every item here is a **proposed**
convention, and every path is a placeholder written as `apps/<name>/`.

## Why this file exists

Two components can share a repository by accident. Three cannot. The decisions below are
the ones that would otherwise be made twice — once badly during this migration, once
again under pressure when the third application arrives.

## Conventions

### Location and module path

The application lives at `apps/<name>/` with module path
`github.com/mattsp1290/beans/apps/<name>`, matching
[01-target-layout-and-module-graph.md](01-target-layout-and-module-graph.md).

Shared library code belongs in `libs/`. A second library would be `libs/<name>/` with
module path `github.com/mattsp1290/beans/libs/<name>`. Do not put shared code in an
application's `internal/` and import it across applications; Go forbids that across
module boundaries and the compiler error arrives late.

### Depending on the beans library

```text
require github.com/mattsp1290/beans/libs/beans v0.0.0
replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans
```

The `replace` is required, not optional, for the reason in decision D3 of
[00-overview.md](00-overview.md): the container build and every `GOWORK=off` invocation
resolve through it.

Add one `use ./apps/<name>` line to `go.work` and run `go work sync`.

### Postgres

`libs/beans/store` already supports Postgres, MySQL, and SQLite, and
`libs/beans/schema/migrations/postgres/` holds goose migrations that
`libs/beans/schema/schema.go` embeds. A new Postgres application that stores beans-shaped
issue data should consume that store rather than defining its own schema.

An application that needs its own tables beyond the beans schema owns those tables and
their migrations inside `apps/<name>/`. It must not add tables to
`libs/beans/schema/migrations/`, because every consumer of the library runs those
migrations.

If the new application shares a database instance with bean-counter, note the precedent
already set in `apps/bean-counter/deploy/docker-compose.prod.yml`: bean-counter attaches
to an external Postgres it does not own, and
`apps/bean-counter/scripts/deploy-production.sh` gates on embedded-migration parity to
avoid migrating a database it does not control. A second application sharing that
database inherits the same hazard and needs the same class of gate.

### Build integration

- Add a `Makefile` at `apps/<name>/` exposing at least `build`, `test`, `vet`, `lint`,
  and `tidy-check`, so the root Makefile's fan-out works unchanged.
- Add `apps/<name>` to the root `Makefile`'s `MODULES` variable.
- Add a `.golangci.yml` at `apps/<name>/`. Per decision D7, lint policy is per module.

### CI

Add `.github/workflows/ci-apps-<name>.yml` following the shape of
`ci-apps-bean-counter.yml`, with this path filter:

```yaml
    paths:
      - 'apps/<name>/**'
      - 'libs/beans/**'
      - 'go.work'
      - 'go.work.sum'
      - '.github/workflows/ci-apps-<name>.yml'
```

`libs/beans/**` must be in the filter. A library change reaches the application through
the filesystem `replace`, so a filter that omits it lets a breaking change merge green.

`ci-workspace.yml` needs one edit: add `./apps/<name>/...` to its `go build` argument
list.

### Containers

If the application ships an image, follow the pattern in
[05-containers-and-deploy.md](05-containers-and-deploy.md): the Dockerfile lives at
`apps/<name>/Dockerfile`, the build context is the repository root, `GOWORK=off` is set
in the build stage, and the whole `libs/beans` tree is copied so the migration embed
resolves.

The root `.dockerignore` is shared. Adding an application-specific exclusion there
affects every image build in the repository; check the others before adding one.

### Issue tracking

Use the single root tracker. Choose an issue prefix distinct from `beans-` and
`bean-counter-`. Do not run `bd init` inside `apps/<name>/`; a second `.beads/` directory
recreates exactly the split that
[06-beads-and-agent-config.md](06-beads-and-agent-config.md) removes.

### Agent instructions

Add a section to the root `AGENTS.md` under a `## apps/<name>` heading. Do not add an
`AGENTS.md` or `CLAUDE.md` inside the application directory.

## Checklist for adding the third application

1. `mkdir -p apps/<name>` and `go mod init github.com/mattsp1290/beans/apps/<name>`.
2. Add the beans `require` and `replace` pair.
3. `go work use ./apps/<name>` and `go work sync`.
4. Add `apps/<name>/Makefile` and `apps/<name>/.golangci.yml`.
5. Add `apps/<name>` to the root `Makefile`'s `MODULES`.
6. Add `.github/workflows/ci-apps-<name>.yml`.
7. Add `./apps/<name>/...` to `ci-workspace.yml`'s build step.
8. Add a `## apps/<name>` section to the root `AGENTS.md` and to the root `README.md` map.
9. Confirm `make ci` at the root still passes and includes the new module.

## Out of scope

This plan does not choose the third application's name, its responsibility, its schema,
its deployment target, or whether it shares bean-counter's database. Those are product
decisions the user owns.
