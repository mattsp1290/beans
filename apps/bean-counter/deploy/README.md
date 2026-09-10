# deploy/

Operator-facing deployment artifacts for running bean-counter in production on
the infra host (`infra-admin@10.0.0.106`). Full design:
[`../../../.agents/plans/bean-counter-deploy/`](../../../.agents/plans/bean-counter-deploy/).

## What's in here

- **`docker-compose.prod.yml`** — self-contained production Compose for the
  `api` + `ui` services. **No `db` service**: bean-counter connects to the
  EXISTING Postgres owned by the local-symphony stack. Always invoke with
  `-p bean-counter` so commands can never resolve to the local-symphony project.
- **`../scripts/deploy-production.sh`** — repeatable, audited deploy driver.

## How it's wired

bean-counter is a read/write dashboard over the **beans** tracker. In production
its API points at the shared local-symphony Postgres (`BN_PROJECT_PREFIX=local-symphony`),
so it shows live orchestrator tracker data. The DSN is never placed in the
environment or compose file: the api reads it from a bind-mounted secret via
`BN_DSN_FILE=/run/secrets/bn_dsn`.

The infra host is a **k8s node** whose kube-router `FORWARD` policy is `DROP`, so
container-to-container traffic on the bean-counter docker bridge is dropped — the
UI cannot proxy `/api` to the api over the bridge. Instead, **both** containers
publish on host ports and **Traefik path-routes** (the `*.birb.homes` pattern,
same as Forgejo). The api joins the `local-symphony_symphony-internal` network
only to reach Postgres (allowed by the firewall):

```
                       ┌─ counter.birb.homes (Traefik ingress, TLS via cert-manager)
browser ──https──►─────┤  /      ─► host :8088 ─► ui  (nginx, static Svelte)
                       └  /api    ─► host :8081 ─► api (fiber) ─► shared local-symphony Postgres
```

- DNS: `counter.birb.homes` → the host/Traefik (cert via DNS-01, LAN-only).
- k8s manifest: `deploy/k8s/bean-counter-ingress.yaml` (Service + manual
  Endpoints → `10.0.0.106:{8088,8081}` + path-routing Ingress).
- The api is also reachable directly on `10.0.0.106:8081` (unauthenticated LAN);
  same for the ui on `:8088`. Hardening (auth) is tracked as follow-up.

## Deploying

```bash
# Preview the plan; resolve the target SHA; no tests, no remote mutation.
./apps/bean-counter/scripts/deploy-production.sh --ref main --dry-run

# Read-only local + remote preflight (SSH, Docker, shared Postgres health,
# external network, schema-version parity, DSN secret, UI port, compose render).
./apps/bean-counter/scripts/deploy-production.sh --ref main --check

# Full deploy of the current origin/main.
./apps/bean-counter/scripts/deploy-production.sh --ref main
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

### One-time secret provisioning

The api runs as the non-root image user (uid 100). The DSN secret must be
readable by that uid. We keep a dedicated copy, owned by the container uid and
not world-readable, separate from the orchestrator's `symphony-secrets` (which
stays `0600`). On a new host, provision it once (needs root):

```bash
sudo install -d -m 0711 -o 1000 -g 1000 /home/infra-admin/bean-counter-secrets
sudo install -m 0400 -o 100 -g 101 \
  /home/infra-admin/symphony-secrets/bn_dsn \
  /home/infra-admin/bean-counter-secrets/bn_dsn
```

The DSN must use the container host form (`@postgres:5432` / `host=postgres`)
since the api joins the symphony network. `--check` verifies readability from
the container uid before any deploy.

## Before the first post-monorepo deploy: schema parity

The deploy script's parity gate aborts when the beans migrations embedded in
the image are **newer** than the shared Postgres, because applying them would
migrate a database bean-counter does not own.

Before the monorepo, bean-counter pinned beans at
`v0.1.2-0.20260615002029-e52dce57b52c`, embedding through `0008` against a
production database at `0008` — the gate passed. Building against `libs/beans`
at HEAD raises the embedded maximum to **0011**
(`0009_bn_remote_url_unique`, `0010_bn_issue_state_drop_check`,
`0011_bn_issue_repos_creation_commit`), so the gate will now **abort**:

```
embedded migrations (11) NEWER than prod (8)
```

That is the gate working, not a bug, and there is no `--force`. Resolve it
deliberately before deploying — see the tracked issue for the decision:

- Advance the shared database to `0011` out of band, with the owner of the
  local-symphony stack, after reviewing `0010_bn_issue_state_drop_check.sql`
  (it drops the `bn_issues_state_check` CHECK constraint) and
  `0011_bn_issue_repos_creation_commit.sql` against the orchestrator's
  expectations. Confirm with
  `select max(version_id) from bn_schema_versions`.
- Or deploy a commit whose `libs/beans` embeds no more than the database
  already has.

Do not weaken the gate to get past it.

## Rollback

Each run generates `rollback.md` from the captured previous state. Preferred path
retags the previous images (no data touched):

```bash
cd ~/git/beans
docker compose -p bean-counter -f apps/bean-counter/deploy/docker-compose.prod.yml stop api ui
docker tag <previous_api_image_id> bean-counter-api:prod
docker tag <previous_ui_image_id>  bean-counter-ui:prod
UI_PORT=8088 SYMPHONY_NETWORK=local-symphony_symphony-internal \
  BN_DSN_SECRET=$HOME/bean-counter-secrets/bn_dsn \
  docker compose -p bean-counter -f apps/bean-counter/deploy/docker-compose.prod.yml up -d --no-build api ui
```

Back out entirely (orchestrator untouched):

```bash
docker compose -p bean-counter -f apps/bean-counter/deploy/docker-compose.prod.yml down   # NEVER -v
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
bash apps/bean-counter/test/scripts/deploy-production_test.sh
shellcheck apps/bean-counter/scripts/deploy-production.sh apps/bean-counter/test/scripts/deploy-production_test.sh
```
