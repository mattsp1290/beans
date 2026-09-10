# Architecture Decisions for Agents

This note captures project decisions that future agents should preserve unless
a Bead explicitly changes them. Treat the implementation as authoritative when
it differs from older planning prompts.

## Scope

bean-counter is a Go/Fiber API plus Svelte frontend for local-network issue
tracking through `github.com/mattsp1290/beans`. It exposes issue CRUD,
dependencies, ready queue, graph, and health endpoints under `/api/v1`.

The service is designed for trusted local or private networks. It deliberately
does not implement authentication, authorization, sessions, JWT, or per-user
identity. Mutating beans operations use the configured `BN_ACTOR` string.

## Beans Dependency

`github.com/mattsp1290/beans` is a normal tagged module dependency in
`go.mod`. Do not add a local `replace` directive for beans during feature work.

The beans store owns schema migration. `internal/store.NewStore` calls
`beans/store.New`, which auto-runs its migrations. Do not add bean-counter
migrations for beans-owned tables.

## Store Boundary

Use `internal/store` as the only application boundary around beans store types.
That package intentionally re-exports beans types and sentinel errors with
package-level type aliases:

- Store/config primitives: `Store`, `Config`, `Driver`, `SecretDSN`.
- Domain types: `Issue`, `IssueState`, `ListFilter`, `CreateIssueInput`,
  `UpdateIssueInput`, `IssueRepoInput`, `DepEdge`.
- Errors: `ErrNotFound`, `ErrCycle`, `ErrDuplicateDep`, `ErrConflict`,
  `ErrDisabled`, `ErrEmptyDSN`, `ErrUnsupportedDriver`.

Handlers may use `Adapter.Store()` for raw CRUD/dependency/graph operations
(the raw store also exposes `ListBlockingDeps`, which the `/deps` and `/graph`
handlers use to ignore the parent-child membership edges beans records since
migration 0008). Use `Adapter.ReadyIssues` when behavior must be scoped by the
configured project prefix and state buckets.

## Driver Selection

Database selection is runtime configuration, not build-time branching.
`internal/config` maps:

- `BN_DRIVER=sqlite|postgres|mysql`
- `BN_DSN=<driver-specific dsn>`
- optional `BN_MAX_CONNS`, `BN_MIN_CONNS`, `BN_CONNECT_TIMEOUT`

into `internal/store.Config`.

SQLite is the default for `make run` and local containerless development.
Direct binary execution defaults the driver to Postgres and still requires an
explicit DSN. Postgres and MySQL integration coverage uses testcontainers under
the `integration` build tag.

## Secrets and Logging

Keep DSNs in `internal/store.SecretDSN`. Do not log, format, marshal, or return
raw DSN values. Configuration errors must not include the raw DSN. Tests in
`internal/config` cover redaction behavior.

## API and Server Posture

The API is versioned under `/api/v1`. Server wiring lives in
`cmd/bean-counter/main.go`; route registration belongs in handler packages and
is assembled by `server.New`.

Fiber v3 is in use. Do not copy Fiber v2 APIs into this project.

CORS permits a single configured origin from `BN_CORS_ORIGIN`. The local
development default is `http://localhost:5173`; the full-stack compose setup
overrides this for the packaged frontend.

Because there is no auth, continue to enforce input validation, project-prefix
checks, and central error mapping. These are not optional security controls.

## Frontend and Deployment

The Svelte frontend is a separate Vite application under `frontend/`. In
development, Vite proxies `/api` to the Go API. In the full-stack Docker setup,
Nginx serves the built frontend and proxies API requests to the backend.

Do not make the Go binary responsible for serving frontend static assets unless
a future Bead explicitly changes the deployment model.

## Testing and CI

Default tests are containerless and run with `go test ./...`. Integration tests
live under `test/integration` with the `integration` build tag and require
Docker/testcontainers.

GitHub Actions runs:

- backend formatting, vet, golangci-lint, unit tests, and build;
- frontend install, tests, type/Svelte checks, and build;
- separate MySQL and Postgres integration jobs.

When adding new integration tests, decide whether they should run in both
matrix entries, in one database entry, or through a broader integration command.
