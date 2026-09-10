# Project Planning with Beads

## Agent Instructions

You are an expert software architect creating a comprehensive task breakdown. This task graph will be executed by AI agents working in parallel, coordinated through MCP Agent Mail with file reservations to prevent conflicts.

<quality_expectations>
Create a thorough, production-ready task graph. Include all necessary setup, implementation, testing, and documentation tasks. Go beyond the basics - consider edge cases, error handling, security considerations, and integration points. Each task should be specific enough for an agent to execute independently without ambiguity.
</quality_expectations>

## Project Information

### Links to Relevant Documentation
~/git/birbparty/web-api for fiber references

### Project Description
bean-counter (located in `~/git/bean-counter`) is a Go + Fiber v3 backend paired with a Svelte UI. The backend imports the beans library (`github.com/mattsp1290/beans`) the way `~/git/local-symphony` does, and exposes its issue-tracker store over a REST API. The Svelte UI is a **full CRUD interface over beans issues**: create / list / update / close issues, manage dependencies, and view the ready queue and dependency graph.

Concrete beans API surface to integrate against (verified in `~/git/beans/store/`):
- `store.New(ctx, store.Config) (*store.Store, error)` — constructs the store and **auto-runs schema migrations**. bean-counter must NOT add its own beans-table migrations.
- `store.Config` fields: `DSN`, `Driver` (`DriverPostgres` | `DriverMySQL` | `DriverSQLite`), `MaxConns`, `MinConns`, `ConnectTimeout`.
- Issue operations: `CreateIssue`, `ListIssues`, `ReadyIssues`, `UpdateIssue`, `CloseIssue`, `AddDep` (see `store/store.go`, `store/repo_store.go`).
- `store.SecretDSN` — reuse for DSN redaction; never log raw DSNs.

Integration pattern to mirror from local-symphony (verified): beans is a **normal tagged module dependency — NO `replace` directive**. local-symphony wraps `beans/store` behind a thin adapter using package-level type aliases (`type Store = beansstore.Store`, etc.) in `internal/tracker/postgres/adapter.go` and re-exports beans' sentinel errors. NOTE: local-symphony only exercises the Postgres path (it never sets `Config.Driver`), so it is NOT a precedent for multi-DB wiring — bean-counter must add driver selection itself.

### Technical Stack
- **Backend:** Go (target the Go 1.25.x toolchain that beans uses) + **Fiber v3**.
  - ⚠️ The Fiber reference repo `~/git/birbparty/web-api` uses **Fiber v2** (`fiber/v2 v2.52.9`), not v3. v3 has breaking changes (handler/context signatures, `app.Static` removed in favor of `static` middleware, binding/`BodyParser` changes, JWT contrib is v2-only). Treat the reference patterns as **conceptual, not copy-paste** — port them to the v3 API.
- **beans dependency:** `github.com/mattsp1290/beans` pinned at a published tag (e.g. `v0.1.0`), mirroring local-symphony. No `replace` directive.
- **Databases:** support the three drivers beans supports — **PostgreSQL, MySQL, SQLite** — via config-driven driver selection mapped into `store.Config{Driver, DSN}` (e.g. `BN_DRIVER` / `BN_DSN` env). Document the DSN format per dialect. SQLite is pure-Go/containerless; PostgreSQL and MySQL integration tests require Docker/testcontainers (build tag `integration`), mirroring beans' own test split.
- **Frontend:** Svelte served as a **separate Vite dev server** (its own process). In dev, Vite proxies to the Go API; in prod the two are deployed as separate processes. API should be versioned (e.g. `/api/v1`) with a CORS policy permitting the frontend origin.

### Specific Requirements
- **No authentication / authorization.** This service runs on a trusted **local network only** — do not build any auth, sessions, or JWT. (beans mutations take an `actor` string; supply a fixed/config-provided actor rather than an auth identity.)
- Still apply **input validation** on all write endpoints and **DSN secret redaction** (`store.SecretDSN`) — security hygiene independent of auth.
- bean-counter Go module path: `github.com/mattsp1290/bean-counter` (confirm before scaffolding).

---

## Your Task

Analyze this project and create a comprehensive **Beads task graph** using the `bd` CLI. Beads provides dependency-aware, conflict-free task management for multi-agent execution.

---

<critical_constraint>
Your ONLY output is a bash shell script. Do NOT use `bd add` — the correct command to create a bead is `bd create`. Use `bd dep add` for dependencies. Do not implement anything yourself.
</critical_constraint>

## Output Format

Generate a shell script that creates the full task graph. The script should:

1. **Initialize Beads** (if not already initialized)
2. **Create all beads** with appropriate priorities
3. **Establish dependencies** between beads
4. **Add labels** for phase grouping

### Example Output

```bash
#!/bin/bash
# Project: bean-counter
# Generated: 2026-06-14

set -e

# Initialize beads if needed
if [ ! -d ".beads" ]; then
    bd init
fi

echo "Creating project beads..."

# ========================================
# Phase 1: Backend Setup & Infrastructure
# ========================================

SETUP_GOMOD=$(bd create "Initialize Go module + Fiber v3 skeleton" \
  -d "go mod init github.com/mattsp1290/bean-counter (Go 1.25.x). Add github.com/gofiber/fiber/v3 and github.com/mattsp1290/beans@v0.1.0 (tagged dep, NO replace). Lay out cmd/bean-counter, internal/. Reservations: go.mod, go.sum, cmd/**, main.go" \
  -p 0 -t task -l setup --silent)

SETUP_LINT=$(bd create "Configure golangci-lint, gofmt, and Makefile targets" \
  -d "Lint/vet/build/test targets. Reservations: .golangci.yml, Makefile, .github/**" \
  -p 1 -t chore -l setup --silent)
bd dep add $SETUP_LINT $SETUP_GOMOD

SETUP_FE=$(bd create "Scaffold Svelte + Vite frontend as separate dev server" \
  -d "Create frontend/ with Svelte + Vite. Configure Vite dev proxy to the Go API (/api/v1). Reservations: frontend/**" \
  -p 1 -t task -l setup --silent)

# ========================================
# Phase 2: Core Architecture (beans integration)
# ========================================

BEANS_ADAPTER=$(bd create "Wrap beans store behind an adapter with type aliases" \
  -d "Mirror local-symphony internal/tracker/postgres/adapter.go: type-alias beansstore.Store/Config, re-export sentinel errors. Reservations: internal/store/**" \
  -p 0 -t feature -l core --silent)
bd dep add $BEANS_ADAPTER $SETUP_GOMOD

DB_CONFIG=$(bd create "Config-driven DB driver selection (postgres/mysql/sqlite)" \
  -d "Map BN_DRIVER/BN_DSN into store.Config{Driver,DSN,MaxConns,...}. store.New auto-runs migrations; do NOT add beans migrations. Redact DSN via store.SecretDSN. Reservations: internal/config/**" \
  -p 0 -t feature -l core --silent)
bd dep add $DB_CONFIG $BEANS_ADAPTER

FIBER_APP=$(bd create "Build Fiber v3 app: router, error handler, CORS, /api/v1 group" \
  -d "Port v2 patterns from ~/git/birbparty/web-api to the Fiber v3 API (handler/context signatures changed; no app.Static). CORS allows the Vite frontend origin. No auth (local network only). Reservations: internal/server/**" \
  -p 0 -t feature -l core --silent)
bd dep add $FIBER_APP $SETUP_GOMOD

# ... continue: issue CRUD endpoints, dependency endpoints, ready-queue/graph
#     endpoints, Svelte views per resource, integration tests (sqlite unit +
#     testcontainers postgres/mysql), Dockerfiles, CI, docs ...

echo ""
echo "Bead graph created! View with:"
echo "  bd ready              # List unblocked tasks"
```

---

## Bead Creation Guidelines

### Priority Levels
- `-p 0` = Critical (blocking other work)
- `-p 1` = High (important but not blocking)
- `-p 2` = Medium (standard work)
- `-p 3` = Low (nice to have)

### Labels (Phase Grouping)
Use `--label` to group beads by phase:
- `setup` - Project initialization
- `core` - Core architecture
- `auth` - Authentication/authorization
- `ui` - UI components
- `feature-{name}` - Feature-specific work
- `testing` - Test coverage
- `docs` - Documentation
- `deploy` - Deployment/CI

### Dependency Rules
1. Never create cycles
2. Every bead should have a clear dependency chain back to setup tasks
3. Use `bd dep add CHILD PARENT` (child depends on parent completing first)
4. Parallel work should share a common ancestor, not depend on each other

### Task Granularity
- Each bead should be completable in **under 750 lines of code**
- Tasks should be atomic enough for one agent to complete without coordination
- If a task requires multiple file areas, consider splitting by file area

---

## File Reservation Planning

For each major work area, note the file patterns that will need exclusive reservation:

```bash
# Example reservation notes (add as bead descriptions)
# beans adapter:   internal/store/**
# DB config:       internal/config/**
# Fiber server:    internal/server/**, cmd/bean-counter/**
# HTTP handlers:   internal/handlers/<resource>/**  (issues, deps, ready, graph)
# Frontend views:  frontend/src/routes/<route>/**, frontend/src/lib/<feature>/**
# Integration tests (testcontainers): test/integration/** (build tag: integration)
```

Reservation surfaces here are Go packages and Svelte routes — size beads to a single package or route, not a React component tree.

This helps agents claim appropriate file surfaces when they start work.

---

## Context Documentation

Place any important context in `prompts/docs/` for agents to reference. This includes:
- Architecture decisions
- API documentation
- Design system specs
- External service integration guides

---

## Verification Steps

After generating the script:

1. **Run it**: `chmod +x setup-beads.sh && ./setup-beads.sh`
2. **Check ready work**: `bd ready` should show initial setup tasks

**Project "done" criteria** (the task graph must drive toward these, not just produce a valid bead graph):
- `go build ./...` and `go vet ./...` pass; `golangci-lint` clean.
- Backend boots against **SQLite** (containerless) and serves `/api/v1` issue CRUD end to end.
- Integration tests pass against **PostgreSQL and MySQL** via testcontainers (`-tags=integration`).
- Svelte frontend builds (`vite build`) and, via the dev proxy, performs full issue CRUD + dependency management + ready-queue/graph views against the live API.

---

## Completeness Checklist

Ensure your task graph includes:

- [ ] All setup and configuration tasks
- [ ] Core architecture and shared utilities
- [ ] Feature implementation tasks (broken into small units)
- [ ] Error handling and edge cases
- [ ] Unit and integration tests for each feature
- [ ] API documentation
- [ ] Security considerations (input validation, DSN secret redaction — NO auth; local network only)
- [ ] Performance considerations where relevant
- [ ] CI/CD and deployment tasks
- [ ] Clear dependency chains with no cycles
