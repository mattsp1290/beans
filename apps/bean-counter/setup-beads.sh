#!/bin/bash
# Project: bean-counter — Go + Fiber v3 backend over the beans issue store, Svelte CRUD UI
# Generated: 2026-06-14
#
# Grounded against:
#   beans  @ v0.1.0  (github.com/mattsp1290/beans, go 1.25.7) — store.Config/New, issue ops verified
#   local-symphony   — adapter type-alias pattern, beans as tagged dep (NO replace)
#   birbparty/web-api — Fiber v2 reference (CONCEPTUAL only; port to v3)
#
# Run:   chmod +x setup-beads.sh && ./setup-beads.sh
# Then:  bd ready

set -euo pipefail

# Initialize beads if needed
if [ ! -d ".beads" ]; then
    bd init
fi

echo "Creating bean-counter task graph..."

# ============================================================
# Phase 1: Setup & Infrastructure  (label: setup)
# ============================================================

SETUP_GOMOD=$(bd create "Initialize Go module + Fiber v3 skeleton" \
  -d "go mod init github.com/mattsp1290/bean-counter (CONFIRM module path first). Target Go 1.25.x (beans uses 1.25.7). Add deps as tagged modules, NO replace directive: github.com/gofiber/fiber/v3, github.com/mattsp1290/beans@v0.1.0. Lay out cmd/bean-counter/main.go (stub), internal/ tree. go build ./... must pass with the empty skeleton. Reservations: go.mod, go.sum, cmd/bean-counter/**, .gitignore" \
  -p 0 -t task -l setup --silent)

SETUP_LINT=$(bd create "Configure golangci-lint, gofmt, Makefile targets" \
  -d "Add .golangci.yml and a Makefile with build/vet/lint/test/test-integration/run targets (test-integration passes -tags=integration). gofmt clean. Reservations: .golangci.yml, Makefile" \
  -p 1 -t chore -l setup --silent)
bd dep add "$SETUP_LINT" "$SETUP_GOMOD"

SETUP_FE=$(bd create "Scaffold Svelte + Vite frontend as a separate dev server" \
  -d "Create frontend/ with Svelte + Vite (own package.json/process). Configure vite.config dev proxy: /api -> http://localhost:<api-port>. vite build must succeed on the empty scaffold. Reservations: frontend/** (package.json, vite.config.*, src/app skeleton, index.html)" \
  -p 1 -t task -l setup --silent)

SETUP_DEVENV=$(bd create "Local dev env: docker-compose for Postgres+MySQL and .env.example" \
  -d "docker-compose.yml bringing up postgres and mysql for local/integration dev (SQLite needs no container). Provide .env.example documenting BN_DRIVER, BN_DSN, BN_PROJECT_PREFIX, BN_ACTOR, BN_CORS_ORIGIN, BN_ADDR with one DSN sample per dialect. Reservations: docker-compose.yml, .env.example" \
  -p 2 -t chore -l setup --silent)
bd dep add "$SETUP_DEVENV" "$SETUP_GOMOD"

# ============================================================
# Phase 2: Core Architecture  (label: core)
# ============================================================

BEANS_ADAPTER=$(bd create "Wrap beans store behind an adapter with type aliases" \
  -d "Mirror local-symphony internal/tracker/postgres/adapter.go. Package-level type aliases: type Store = beansstore.Store; Config; SecretDSN; Issue; ListFilter; CreateIssueInput; UpdateIssueInput; DepEdge. Re-export sentinels: ErrNotFound, ErrCycle, ErrDuplicateDep, ErrConflict, ErrEmptyDSN, ErrUnsupportedDriver. Adapter holds store + project prefix + terminal/active states (needed by ReadyIssues/ListDeps). store.New auto-runs migrations — do NOT add any beans migrations. Reservations: internal/store/**" \
  -p 0 -t feature -l core --silent)
bd dep add "$BEANS_ADAPTER" "$SETUP_GOMOD"

DB_CONFIG=$(bd create "Config-driven DB driver selection (postgres/mysql/sqlite)" \
  -d "Parse env into store.Config: BN_DRIVER -> Driver (DriverPostgres|DriverMySQL|DriverSQLite), BN_DSN -> DSN, plus MaxConns/MinConns/ConnectTimeout. Also load BN_PROJECT_PREFIX, BN_ACTOR (fixed actor for CloseIssue/mutations — NO auth), BN_CORS_ORIGIN, BN_ADDR. Validate via Config.Validate(). NEVER log raw DSNs — redact with store.SecretDSN. Reservations: internal/config/**" \
  -p 0 -t feature -l core --silent)
bd dep add "$DB_CONFIG" "$BEANS_ADAPTER"

DTO=$(bd create "Define API DTOs and beans<->JSON mappers" \
  -d "Request/response structs for issues, deps, ready, graph (json tags, stable field names). Mappers between beans Issue/DepEdge/CreateIssueInput/UpdateIssueInput and DTOs. Keep beans model types out of the HTTP layer. Reservations: internal/api/dto/**" \
  -p 0 -t feature -l core --silent)
bd dep add "$DTO" "$BEANS_ADAPTER"

FIBER_APP=$(bd create "Build Fiber v3 app: router, CORS, /api/v1 group" \
  -d "Port v2 patterns from ~/git/birbparty/web-api to the Fiber v3 API (handler/ctx signatures changed; app.Static removed -> static middleware; BodyParser/binding changes). Recover + request-logger middleware. CORS restricted to BN_CORS_ORIGIN. Versioned /api/v1 route group. No auth (local network only). Reservations: internal/server/app.go, internal/server/middleware/**" \
  -p 0 -t feature -l core --silent)
bd dep add "$FIBER_APP" "$SETUP_GOMOD"

ERROR_MAP=$(bd create "Central error handler: map beans sentinels to HTTP status" \
  -d "Fiber v3 ErrorHandler mapping re-exported sentinels to status + JSON error body: ErrNotFound->404, ErrCycle/ErrDuplicateDep/ErrConflict->409, validation->400, ErrUnsupportedDriver/ErrEmptyDSN->500 (startup). Consistent {error,message} shape. Reservations: internal/server/errors.go" \
  -p 0 -t feature -l core --silent)
bd dep add "$ERROR_MAP" "$FIBER_APP"
bd dep add "$ERROR_MAP" "$BEANS_ADAPTER"

VALIDATION=$(bd create "Input validation helpers for write endpoints" \
  -d "Validate request bodies on all writes (required fields, length bounds, enum/state values, non-empty IDs). Return 400 with field-level detail via the central error handler. Reservations: internal/api/validate/**" \
  -p 1 -t feature -l core --silent)
bd dep add "$VALIDATION" "$DTO"

CONTRACT=$(bd create "Author the /api/v1 REST contract (shared by BE + FE)" \
  -d "Document every endpoint, request/response DTO, and status code for issues CRUD, deps add/remove/list, ready queue, dependency graph. Reflect real beans types/sentinels. This is the integration seam that lets frontend work proceed in parallel with handlers. Place in docs/api/contract.md. Reservations: docs/api/contract.md" \
  -p 0 -t task -l core --silent)
bd dep add "$CONTRACT" "$DTO"

STORE_LIFECYCLE=$(bd create "Wire main: build config+store+app, graceful shutdown" \
  -d "cmd/bean-counter/main.go: load config (internal/config), construct adapter via store.New(ctx,cfg) (auto-migrates), build Fiber app, register route groups, Listen on BN_ADDR, handle SIGINT/SIGTERM with graceful shutdown + store pool close. Reservations: cmd/bean-counter/main.go, internal/server/run.go" \
  -p 0 -t feature -l core --silent)
bd dep add "$STORE_LIFECYCLE" "$DB_CONFIG"
bd dep add "$STORE_LIFECYCLE" "$FIBER_APP"

HEALTH=$(bd create "Health/readiness endpoint" \
  -d "GET /api/v1/healthz (liveness) and /api/v1/readyz (pings store). Used by compose/CI healthchecks. Reservations: internal/handlers/health/**" \
  -p 2 -t feature -l core --silent)
bd dep add "$HEALTH" "$FIBER_APP"

# ============================================================
# Phase 3: Backend HTTP handlers  (labels: feature-*)
# ============================================================

H_ISSUES=$(bd create "Issue CRUD handlers (create/list/get/update/close/delete)" \
  -d "POST /issues (CreateIssue), GET /issues (ListIssues w/ ListFilter query params), GET /issues/:id (GetIssue), PATCH /issues/:id (UpdateIssue), POST /issues/:id/close (CloseIssue using config actor+reason), DELETE /issues/:id (DeleteIssue). Use DTO mappers, VALIDATION, ERROR_MAP. Reservations: internal/handlers/issues/**" \
  -p 0 -t feature -l feature-issues --silent)
bd dep add "$H_ISSUES" "$STORE_LIFECYCLE"
bd dep add "$H_ISSUES" "$DTO"
bd dep add "$H_ISSUES" "$VALIDATION"
bd dep add "$H_ISSUES" "$ERROR_MAP"
bd dep add "$H_ISSUES" "$CONTRACT"

H_DEPS=$(bd create "Dependency handlers (add/remove/list)" \
  -d "POST /issues/:id/deps (AddDep child=:id, parent in body), DELETE /issues/:id/deps/:parent (RemoveDep), GET /deps (ListDeps by config prefix). Map ErrCycle/ErrDuplicateDep->409. Reservations: internal/handlers/deps/**" \
  -p 1 -t feature -l feature-deps --silent)
bd dep add "$H_DEPS" "$STORE_LIFECYCLE"
bd dep add "$H_DEPS" "$DTO"
bd dep add "$H_DEPS" "$ERROR_MAP"
bd dep add "$H_DEPS" "$CONTRACT"

H_READY=$(bd create "Ready-queue handler" \
  -d "GET /ready -> ReadyIssues(prefix, terminalStates, activeStates) using config-provided prefix/states. Returns unblocked issues. Reservations: internal/handlers/ready/**" \
  -p 1 -t feature -l feature-ready --silent)
bd dep add "$H_READY" "$STORE_LIFECYCLE"
bd dep add "$H_READY" "$DTO"
bd dep add "$H_READY" "$CONTRACT"

H_GRAPH=$(bd create "Dependency-graph handler" \
  -d "GET /graph -> build nodes (ListIssues) + edges (ListDeps DepEdge) into a graph DTO suitable for the FE visualization. Reservations: internal/handlers/graph/**" \
  -p 1 -t feature -l feature-graph --silent)
bd dep add "$H_GRAPH" "$STORE_LIFECYCLE"
bd dep add "$H_GRAPH" "$DTO"
bd dep add "$H_GRAPH" "$CONTRACT"

# ============================================================
# Phase 4: Svelte frontend  (label: ui)
# ============================================================

FE_LAYOUT=$(bd create "Frontend app shell: routing, nav, layout" \
  -d "Svelte routing + top-level layout/nav linking Issues, Ready, Graph. Shared styles, error/loading components. Reservations: frontend/src/routes/+layout.*, frontend/src/lib/components/**, frontend/src/app.css" \
  -p 1 -t feature -l ui --silent)
bd dep add "$FE_LAYOUT" "$SETUP_FE"

FE_API_CLIENT=$(bd create "Typed frontend API client for /api/v1" \
  -d "fetch wrapper + typed methods for issues CRUD, deps, ready, graph matching docs/api/contract.md. Centralized error handling, base URL via Vite proxy. Reservations: frontend/src/lib/api/**" \
  -p 0 -t feature -l ui --silent)
bd dep add "$FE_API_CLIENT" "$SETUP_FE"
bd dep add "$FE_API_CLIENT" "$CONTRACT"

FE_ISSUES=$(bd create "Issues UI: list, create/edit form, detail/close" \
  -d "Routes for listing issues (with filters), create/edit form (validation mirroring backend), detail view with close + delete actions. Reservations: frontend/src/routes/issues/**" \
  -p 0 -t feature -l feature-issues --silent)
bd dep add "$FE_ISSUES" "$FE_API_CLIENT"
bd dep add "$FE_ISSUES" "$FE_LAYOUT"

FE_DEPS=$(bd create "Dependency management UI" \
  -d "From an issue detail, add/remove dependencies; surface 409 cycle/duplicate errors clearly. Reservations: frontend/src/routes/issues/[id]/deps/**, frontend/src/lib/deps/**" \
  -p 1 -t feature -l feature-deps --silent)
bd dep add "$FE_DEPS" "$FE_API_CLIENT"
bd dep add "$FE_DEPS" "$FE_LAYOUT"

FE_READY=$(bd create "Ready-queue view" \
  -d "Route showing the ready (unblocked) queue from GET /ready with refresh. Reservations: frontend/src/routes/ready/**" \
  -p 1 -t feature -l feature-ready --silent)
bd dep add "$FE_READY" "$FE_API_CLIENT"
bd dep add "$FE_READY" "$FE_LAYOUT"

FE_GRAPH=$(bd create "Dependency-graph visualization" \
  -d "Route rendering the dependency graph from GET /graph (nodes/edges) with a graph/network view. Reservations: frontend/src/routes/graph/**, frontend/src/lib/graph/**" \
  -p 2 -t feature -l feature-graph --silent)
bd dep add "$FE_GRAPH" "$FE_API_CLIENT"
bd dep add "$FE_GRAPH" "$FE_LAYOUT"

# ============================================================
# Phase 5: Testing  (label: testing)
# ============================================================

TEST_BACKEND_UNIT=$(bd create "Backend handler unit tests (SQLite in-memory)" \
  -d "Table-driven tests for all handlers against a real store on SQLite in-memory (containerless, default build). Cover happy path + error mapping (404/409/400) + validation. Reservations: internal/handlers/**/*_test.go, internal/server/*_test.go" \
  -p 1 -t task -l testing --silent)
bd dep add "$TEST_BACKEND_UNIT" "$H_ISSUES"
bd dep add "$TEST_BACKEND_UNIT" "$H_DEPS"
bd dep add "$TEST_BACKEND_UNIT" "$H_READY"
bd dep add "$TEST_BACKEND_UNIT" "$H_GRAPH"

TEST_SQLITE_E2E=$(bd create "SQLite end-to-end boot + CRUD smoke test" \
  -d "Spin the assembled app on SQLite, exercise issue create->list->update->close + dep add/list + ready over HTTP via httptest. Verifies the 'boots against SQLite, serves /api/v1 end to end' done-criterion. Reservations: test/e2e/**" \
  -p 1 -t task -l testing --silent)
bd dep add "$TEST_SQLITE_E2E" "$STORE_LIFECYCLE"
bd dep add "$TEST_SQLITE_E2E" "$H_ISSUES"

TEST_INT_PG=$(bd create "Integration tests: PostgreSQL via testcontainers" \
  -d "Build tag 'integration' (mirror beans test split). Start postgres testcontainer, run store.New (auto-migrate), exercise CRUD+deps+ready through handlers. make test-integration. Reservations: test/integration/postgres_test.go, test/integration/helpers.go" \
  -p 1 -t task -l testing --silent)
bd dep add "$TEST_INT_PG" "$H_ISSUES"
bd dep add "$TEST_INT_PG" "$H_DEPS"
bd dep add "$TEST_INT_PG" "$SETUP_DEVENV"

TEST_INT_MYSQL=$(bd create "Integration tests: MySQL via testcontainers" \
  -d "Build tag 'integration'. Start mysql testcontainer, run store.New (auto-migrate), exercise CRUD+deps+ready through handlers. Reservations: test/integration/mysql_test.go" \
  -p 1 -t task -l testing --silent)
bd dep add "$TEST_INT_MYSQL" "$H_ISSUES"
bd dep add "$TEST_INT_MYSQL" "$H_DEPS"
bd dep add "$TEST_INT_MYSQL" "$SETUP_DEVENV"

TEST_FE=$(bd create "Frontend tests (component + e2e)" \
  -d "Vitest component tests for the API client + key views; optional Playwright happy-path against a mocked or live API. Reservations: frontend/src/**/*.test.ts, frontend/tests/**" \
  -p 2 -t task -l testing --silent)
bd dep add "$TEST_FE" "$FE_ISSUES"
bd dep add "$TEST_FE" "$FE_DEPS"

# ============================================================
# Phase 6: Documentation  (label: docs)
# ============================================================

DOCS_README=$(bd create "README: run instructions, env vars, DSN formats" \
  -d "How to run backend (SQLite default, Postgres/MySQL via env) and frontend dev server. Document BN_DRIVER/BN_DSN/BN_PROJECT_PREFIX/BN_ACTOR/BN_CORS_ORIGIN/BN_ADDR and a DSN example per dialect. Note: local-network only, no auth. Reservations: README.md" \
  -p 2 -t task -l docs --silent)
bd dep add "$DOCS_README" "$STORE_LIFECYCLE"

DOCS_API=$(bd create "Finalize API documentation from contract + impl" \
  -d "Reconcile docs/api/contract.md with shipped handlers; add request/response examples per endpoint. Reservations: docs/api/**" \
  -p 2 -t task -l docs --silent)
bd dep add "$DOCS_API" "$H_ISSUES"
bd dep add "$DOCS_API" "$H_DEPS"
bd dep add "$DOCS_API" "$H_READY"
bd dep add "$DOCS_API" "$H_GRAPH"

DOCS_ARCH=$(bd create "Architecture notes / ADRs for agents" \
  -d "Capture key decisions: beans as tagged dep (no replace), adapter type-alias seam, driver selection, no-auth/local-network posture, DSN redaction. Place in prompts/docs/ for agent reference. Reservations: prompts/docs/**" \
  -p 3 -t task -l docs --silent)
bd dep add "$DOCS_ARCH" "$BEANS_ADAPTER"

# ============================================================
# Phase 7: Deployment & CI  (label: deploy)
# ============================================================

DOCKER_BACKEND=$(bd create "Backend Dockerfile (multi-stage, CGO-free SQLite)" \
  -d "Multi-stage build of cmd/bean-counter. Ensure the chosen SQLite driver builds in the target image. Small runtime image, configurable via BN_* env. Reservations: Dockerfile, .dockerignore" \
  -p 2 -t task -l deploy --silent)
bd dep add "$DOCKER_BACKEND" "$STORE_LIFECYCLE"

DOCKER_FE=$(bd create "Frontend Dockerfile (vite build + static serve)" \
  -d "Build frontend with vite and serve static assets (nginx or equivalent) as a separate process from the API. Reservations: frontend/Dockerfile, frontend/.dockerignore, frontend/nginx.conf" \
  -p 2 -t task -l deploy --silent)
bd dep add "$DOCKER_FE" "$FE_LAYOUT"

COMPOSE_STACK=$(bd create "Full-stack docker-compose (api + ui + db)" \
  -d "Compose profile wiring backend + frontend + a chosen DB with healthchecks and env. Extends the dev compose from setup. Reservations: docker-compose.stack.yml" \
  -p 2 -t task -l deploy --silent)
bd dep add "$COMPOSE_STACK" "$DOCKER_BACKEND"
bd dep add "$COMPOSE_STACK" "$DOCKER_FE"

CI=$(bd create "CI pipeline: build/vet/lint/test + integration matrix" \
  -d "GitHub Actions: go build/vet, golangci-lint, unit tests (SQLite), integration job with -tags=integration (postgres+mysql services/testcontainers), frontend build + tests. Reservations: .github/workflows/**" \
  -p 1 -t task -l deploy --silent)
bd dep add "$CI" "$SETUP_LINT"
bd dep add "$CI" "$TEST_BACKEND_UNIT"
bd dep add "$CI" "$TEST_INT_PG"
bd dep add "$CI" "$TEST_INT_MYSQL"
bd dep add "$CI" "$TEST_FE"

echo ""
echo "Bean-counter task graph created."
echo "  bd ready    # unblocked tasks (expect: Go skeleton, Svelte scaffold)"
echo "  bd graph    # dependency graph"
echo "  bd list     # all tasks"
