# 02 — Script contract & production compose

Two new artifacts:

- `scripts/deploy-production.sh` — the operator entrypoint (this repo has **no**
  `scripts/` dir yet; it is created here).
- `deploy/docker-compose.prod.yml` — the production Compose override (api + ui,
  no db, external network, secret mount).

## `scripts/deploy-production.sh`

Modeled on `local-symphony/deploy/update-production.sh`. Same safety spine:
SHA pinned to `origin/main`, one `flock`'d remote SSH session spanning all
mutation, secrets read on the remote (never over argv), audit records, and a
generated `rollback.md`. **No `--force`** — a failed gate stops the deploy.

### Usage

```
Usage: scripts/deploy-production.sh --ref <ref> [options]

Required:
  --ref <ref>            'main' (must equal origin/main) or an exact commit SHA
                         reachable from origin/main. Tags/branches rejected.

Options:
  --host <ssh-target>    default: infra-admin@10.0.0.106
  --repo-dir <path>      remote checkout; $HOME expanded on remote
                         (default: $HOME/git/bean-counter)
  --ui-port <port>       host port for the UI (default: 8088, bound 0.0.0.0)
  --image-api <tag>      default: bean-counter-api:prod
  --image-ui  <tag>      default: bean-counter-ui:prod
  --bn-project <prefix>  beans project prefix (default: local-symphony)
  --dsn-secret <path>    remote path to the bn DSN secret
                         (default: $HOME/symphony-secrets/bn_dsn... see note)
  --check                read-only local + remote preflight; no mutation
  --dry-run              resolve SHA + print planned phases; no tests, no remote
  --skip-integration     skip local integration gate (recorded in summary)
  --skip-local-build     skip local docker build gate (recorded in summary)
  --no-rebuild           skip remote image rebuild if the LIVE images were built
                         from the exact target SHA (see provenance note below)
  -h, --help

There is intentionally no --force.
```

> **DSN secret path note:** the symphony secrets dir is owned by `infra-admin`
> at `/home/infra-admin/symphony-secrets/bn_dsn`. Since the deploy SSHes in as
> `infra-admin`, `$HOME/symphony-secrets/bn_dsn` resolves to that path. The
> compose `BN_DSN_SECRET` value MUST be **derived from the resolved
> `--dsn-secret`** (not hardcoded to `/home/infra-admin/...`), so overriding
> `--host` to a non-`infra-admin` user does not silently point at the wrong
> home. Keep it overridable so a dedicated `bean-counter` secret can be
> substituted later.
>
> **Container-uid readability (not just presence):** the api image runs as the
> non-root user `bean-counter` (`Dockerfile`), but the host secret is owned by
> `infra-admin` and bind-mounted `:ro`. If the file is `0600 infra-admin`, the
> in-container uid **cannot read it** and the api crashes at startup. Preflight
> must assert readability *from the container's uid* (e.g. a throwaway
> `docker run --rm -u <uid> -v <secret>:/run/secrets/bn_dsn:ro <api-image> cat
> /run/secrets/bn_dsn >/dev/null`), not merely host-side `[ -f ]`. Remediation
> options: make the secret group/world-readable, run the api as a uid that can
> read it, or copy the secret to a bean-counter-owned path during deploy.

> **`--no-rebuild` provenance:** images are tagged with a moving tag
> (`bean-counter-api:prod`), so a tag alone does not prove which SHA built them.
> Builds MUST stamp `--label org.opencontainers.image.revision=<target-sha>`;
> `--no-rebuild` then skips the rebuild only when `docker image inspect` shows
> that label equals the target SHA. Without the label, `--no-rebuild` is unsafe
> and the script should ignore it (always rebuild).

### Modes

- `--dry-run`: `resolve_target_sha` + print plan + SSH reachability check only.
- `--check`: local preconditions + read-only remote preflight (docker, shared
  Postgres health, external network present, secret present, port free, compose
  config renders). No mutation.
- live (default): local gates → locked remote deploy.

### Structure (mirror the reference)

- Pure, sourceable helpers above a `BASH_SOURCE == $0` main-guard so
  `test/scripts/deploy-production_test.sh` can unit-test them without deploying.
  Candidate pure helpers:
  - `normalize_ui_port` — validate `--ui-port` is an integer in range.
  - `assert_dsn_container_host` — assert the DSN uses `@postgres:5432` (the
    container host form the external-network design requires); fail closed
    otherwise. (No rewrite — unlike local-symphony, we keep the container host.)
  - `extract_issue_count` — parse the smoke-test JSON issue count.
- `remote_exec` pipes a **self-contained** bash payload over SSH with
  positional args; secrets are read on the remote inside the payload, never
  interpolated into program text or passed as argv.
- `resolve_target_sha` enforces the pushed-ref policy exactly as the reference
  does (`git fetch origin`; `main` must equal `origin/main`; a SHA must be
  reachable from `origin/main`).

## `deploy/docker-compose.prod.yml`

A **self-contained** Compose file — it carries its own `build:` definitions and
defines ONLY `api` and `ui`. It must **not** be layered with
`docker-compose.stack.yml` (that file defines a `db` service, a
`bean-counter-postgres` volume, and `api.depends_on: db: service_healthy` —
layering would risk Compose starting a stray Postgres). Every compose command
pins `-p bean-counter`. Sketch (final values resolved during impl):

```yaml
services:
  api:
    build:
      context: .
      dockerfile: Dockerfile
    image: ${BEAN_COUNTER_API_IMAGE:-bean-counter-api:prod}
    environment:
      BN_DRIVER: postgres
      BN_DSN_FILE: /run/secrets/bn_dsn        # prep task: file-based DSN
      BN_PROJECT_PREFIX: ${BN_PROJECT_PREFIX:-local-symphony}
      BN_ACTOR: ${BN_ACTOR:-bean-counter-ui}
      BN_CORS_ORIGIN: ${BN_CORS_ORIGIN:-http://10.0.0.106:8088}
      BN_ADDR: :8080
    volumes:
      - ${BN_DSN_SECRET:-/home/infra-admin/symphony-secrets/bn_dsn}:/run/secrets/bn_dsn:ro
    restart: unless-stopped
    networks: [bean-counter-internal, symphony]
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://127.0.0.1:8080/api/v1/readyz >/dev/null"]
      interval: 10s
      timeout: 3s
      retries: 12
      start_period: 10s

  ui:
    build:
      context: ./frontend
      dockerfile: Dockerfile
    image: ${BEAN_COUNTER_UI_IMAGE:-bean-counter-ui:prod}
    environment:
      API_UPSTREAM: http://api:8080
      NGINX_RESOLVER: 127.0.0.11
    depends_on:
      api:
        condition: service_healthy
    restart: unless-stopped
    ports:
      - "0.0.0.0:${UI_PORT:-8088}:8080"
    networks: [bean-counter-internal]
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://127.0.0.1:8080/healthz >/dev/null"]
      interval: 10s
      timeout: 3s
      retries: 12

networks:
  bean-counter-internal:
    driver: bridge
  symphony:                      # the shared Postgres network
    external: true
    name: local-symphony_symphony-internal
```

### Flag → environment-variable → compose mapping

The script translates CLI flags into the env vars the compose file interpolates.
Keep this table authoritative so the two never drift (casing-boundary class):

| CLI flag | Env var | Compose use | Default |
|----------|---------|-------------|---------|
| `--ui-port` | `UI_PORT` | `ports: 0.0.0.0:${UI_PORT}:8080` | `8088` |
| `--image-api` | `BEAN_COUNTER_API_IMAGE` | `api.image` | `bean-counter-api:prod` |
| `--image-ui` | `BEAN_COUNTER_UI_IMAGE` | `ui.image` | `bean-counter-ui:prod` |
| `--bn-project` | `BN_PROJECT_PREFIX` | `api.environment` | `local-symphony` |
| `--dsn-secret` | `BN_DSN_SECRET` | `api.volumes` (mount source) | resolved `$HOME/symphony-secrets/bn_dsn` |

Open verification items for the impl (flagged for reviewers):

- Confirm the external network name on the host is exactly
  `local-symphony_symphony-internal` (`docker network ls`), since Compose
  derives it from the project dir name; if local-symphony was brought up with a
  custom `-p`, the prefix differs. The preflight should discover it, not assume.
- Confirm `BN_DSN_FILE` is implemented (prep task) before referencing it; until
  then use the option-2 stopgap from `01`.
- If any layering with `stack.yml` is unavoidable, the prod `api` MUST set
  `depends_on: !reset []` (Compose merges `depends_on` additively; selecting
  only `api ui` services does not reliably suppress a `service_healthy`
  dependency). Verify `db` is absent via `docker compose -p bean-counter ...
  config`. The self-contained file above avoids this entirely.
- Decide whether to commit `deploy/docker-compose.prod.yml` (yes — it carries no
  secrets) and whether the api should additionally publish `127.0.0.1:<port>`
  for debugging (default: no).

## Files this plan introduces

| Path | Purpose | Committed? |
|------|---------|-----------|
| `scripts/deploy-production.sh` | operator deploy entrypoint | yes |
| `deploy/docker-compose.prod.yml` | prod api+ui override, no db | yes |
| `deploy/README.md` | operator runbook (mirror local-symphony) | yes |
| `test/scripts/deploy-production_test.sh` | unit tests for pure helpers + arg parsing | yes |
| `internal/config` change | `BN_DSN_FILE` support | yes (prep) |
