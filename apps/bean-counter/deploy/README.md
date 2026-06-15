# deploy/

Operator-facing deployment artifacts for running bean-counter in production on
the infra host (`infra-admin@10.0.0.106`). Full design:
[`../.agents/plans/deploy/`](../.agents/plans/deploy/).

## What's in here

- **`docker-compose.prod.yml`** — self-contained production Compose for the
  `api` + `ui` services. **No `db` service**: bean-counter connects to the
  EXISTING Postgres owned by the local-symphony stack. Always invoke with
  `-p bean-counter` so commands can never resolve to the local-symphony project.
- **`../scripts/deploy-production.sh`** — repeatable, audited deploy driver.

## How it's wired

bean-counter is a read/write dashboard over the **beans** tracker. In production
its API points at the shared local-symphony Postgres (`BN_PROJECT_PREFIX=local-symphony`),
so it shows live orchestrator tracker data. The UI is published on
`0.0.0.0:8088` (`http://10.0.0.106:8088`) — **unauthenticated LAN access** to a
read/write view. The DSN is never placed in the environment or compose file: the
api reads it from a bind-mounted secret via `BN_DSN_FILE=/run/secrets/bn_dsn`.

```
LAN :8088 ─► ui (nginx) ──/api/──► api (fiber) ──► shared local-symphony Postgres
                                         (joins external network
                                          local-symphony_symphony-internal)
```

## Deploying

```bash
# Preview the plan; resolve the target SHA; no tests, no remote mutation.
scripts/deploy-production.sh --ref main --dry-run

# Read-only local + remote preflight (SSH, Docker, shared Postgres health,
# external network, schema-version parity, DSN secret, UI port, compose render).
scripts/deploy-production.sh --ref main --check

# Full deploy of the current origin/main.
scripts/deploy-production.sh --ref main
```

`--help` lists every flag. Key safety properties (full design in the plan):

- **Pinned target.** `--ref main` requires local `main == origin/main`; an exact
  SHA is accepted only if reachable from `origin/main`. The deploy checks out the
  resolved SHA detached and never `git pull`s (no time-of-check/time-of-use race).
- **Schema-version parity gate.** Before connecting, the deploy compares
  bean-counter's embedded beans migration max to `max(version_id)` in the shared
  DB's `bn_schema_versions`. **It aborts if bean-counter is newer than prod** —
  otherwise connecting would migrate the orchestrator's schema. (Equal/older is a
  guaranteed no-op: beans migrations are goose forward-only.)
- **Backup first.** A consistent `pg_dump -Fc` of the shared DB is mandatory; if
  it fails the deploy aborts before any change.
- **Smoke gate.** A read-only HTTP smoke test (UI `/healthz`, `/api/v1/readyz`,
  `/api/v1/issues`) must pass. An empty-but-valid tracker response is a pass.
  There is no `--force` and `--skip-smoke` is rejected.
- **Audit trail.** Every live deploy writes a record under
  `~/.agents/deploy-runs/bean-counter/<stamp>-<short-sha>/` on infra
  (`summary.md`, `rollback.md`, `symphony.dump`, `compose-config.yml`,
  `version-parity.txt`, `smoke.txt`, logs). The path is printed at the end.

The secret (`BN_DSN`) is read on the remote from the mounted file, never passed
over SSH argv, printed, or written into the deploy record (the rendered compose
config is secret-scanned; captured logs are redacted).

## Rollback

Each run generates `rollback.md` from the captured previous state. Preferred path
retags the previous images (no data touched):

```bash
cd ~/git/bean-counter
docker compose -p bean-counter -f deploy/docker-compose.prod.yml stop api ui
docker tag <previous_api_image_id> bean-counter-api:prod
docker tag <previous_ui_image_id>  bean-counter-ui:prod
UI_PORT=8088 SYMPHONY_NETWORK=local-symphony_symphony-internal \
  BN_DSN_SECRET=$HOME/symphony-secrets/bn_dsn \
  docker compose -p bean-counter -f deploy/docker-compose.prod.yml up -d --no-build api ui
```

Back out entirely (orchestrator untouched):

```bash
docker compose -p bean-counter -f deploy/docker-compose.prod.yml down   # NEVER -v
```

**Never** `-v` / `docker volume rm` — the Postgres volume is owned by
local-symphony. Data rollback (restoring `symphony.dump` into the shared DB) is a
coordinated manual operation: stop the orchestrator first, then `pg_restore`. It
is never automated.

## Blast-radius warning

The deploy points bean-counter at the **live orchestrator tracker** with full
read/write and exposes the UI unauthenticated on the LAN. Anyone who can reach
`http://10.0.0.106:8088` can create/close/delete real tracker issues. The
pre-deploy backup bounds data loss to "since the last deploy." Hardening
(reverse proxy + auth, or a read-only mode) is tracked as follow-up work in the
plan's task sequence.

## Tests

Pure-helper and argument-parsing tests (hermetic — no network/Docker/SSH):

```bash
bash test/scripts/deploy-production_test.sh
shellcheck scripts/deploy-production.sh test/scripts/deploy-production_test.sh
```
