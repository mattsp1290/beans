# bean-counter

bean-counter is a local-network web UI and JSON API for
[`beans`](https://github.com/mattsp1290/beans) issues. It provides issue CRUD,
dependency management, a ready queue, and a dependency graph API.

The app is intended for trusted local or private-network use. It does not
include authentication or authorization; do not expose it directly to the
public internet.

## Prerequisites

- Go 1.25 or newer
- Node.js and npm for the Svelte/Vite frontend
- Docker, only for the full-stack compose setup or external Postgres/MySQL

## Run Locally

Start the API with SQLite:

```sh
make run
```

`make run` sets `BN_DRIVER=sqlite` and `BN_DSN=file:bean-counter.db`, so no
database server is required. The API listens on `http://localhost:8080` and
serves routes under `/api/v1`.

In a second shell, start the frontend dev server:

```sh
npm --prefix frontend install
npm --prefix frontend run dev
```

The frontend runs through Vite and proxies `/api` to `http://localhost:8080` by
default. To point it at a different API address:

```sh
VITE_API_PROXY_TARGET=http://localhost:9090 npm --prefix frontend run dev
```

## Full Stack With Docker

Run Postgres, the API, and the built frontend together:

```sh
docker compose -f docker-compose.stack.yml up --build
```

Open `http://localhost:8080`. The compose stack publishes the UI on port 8080
and the API on `127.0.0.1:8081` for direct API checks.

The backend Docker image itself defaults to SQLite at `/data/bean-counter.db`.
The full-stack compose file overrides that to use Postgres.

## Backend Configuration

The backend reads configuration from environment variables:

| Variable | Meaning | Default used by `make run` |
| --- | --- | --- |
| `BN_DRIVER` | Store driver: `sqlite`, `postgres`, or `mysql` | `sqlite` |
| `BN_DSN` | Driver-specific database connection string | `file:bean-counter.db` |
| `BN_PROJECT_PREFIX` | Issue ID project prefix | `bean-counter` |
| `BN_ACTOR` | Actor recorded for mutations such as close | `bean-counter` |
| `BN_CORS_ORIGIN` | Single allowed browser origin for CORS | `http://localhost:5173` |
| `BN_ADDR` | API listen address | `:8080` |

Optional store pool settings are also supported:

| Variable | Meaning |
| --- | --- |
| `BN_MAX_CONNS` | Maximum open store connections; unset means driver default |
| `BN_MIN_CONNS` | Minimum idle store connections; unset means driver default |
| `BN_CONNECT_TIMEOUT` | Store connect timeout as a Go duration, for example `5s` |

If you run `go run ./cmd/bean-counter` or the compiled binary directly, set
`BN_DRIVER` and `BN_DSN` explicitly. The raw process config defaults the driver
to Postgres and requires a DSN.

## DSN Examples

SQLite:

```sh
BN_DRIVER=sqlite BN_DSN='file:bean-counter.db' go run ./cmd/bean-counter
```

SQLite through the `make run` wrapper:

```sh
RUN_DRIVER=sqlite RUN_DSN='file:bean-counter.db' make run
```

SQLite in-memory database for short-lived testing:

```sh
BN_DRIVER=sqlite BN_DSN='file::memory:' go run ./cmd/bean-counter
```

Postgres:

```sh
BN_DRIVER=postgres \
BN_DSN='postgres://bean_counter:bean_counter@localhost:5432/bean_counter?sslmode=disable' \
go run ./cmd/bean-counter
```

MySQL:

```sh
BN_DRIVER=mysql \
BN_DSN='bean_counter:bean_counter@tcp(localhost:3306)/bean_counter?parseTime=true' \
go run ./cmd/bean-counter
```

## Useful Commands

```sh
make build
make test
make vet
make lint
npm --prefix frontend run test
npm --prefix frontend run check
npm --prefix frontend run build
```

API details are documented in [docs/api/contract.md](docs/api/contract.md).
