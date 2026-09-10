# 01 — Architecture & topology on 10.0.0.106

## What already runs on the host

The `local-symphony` stack runs via Compose with a production override
(`~/git/local-symphony/docker-compose.yml` +
`docker-compose.infra-postgres-beans.yml`, profile `default`):

- `postgres` — `postgres:16-alpine`, on networks `symphony-internal` /
  `symphony-egress`, volume `postgres-data`, published by the infra override to
  `127.0.0.1:5432`. **This is the production beans database.** Compose project
  name is `local-symphony`, so the real Docker object names are
  `local-symphony_symphony-internal` (network) and
  `local-symphony_postgres-data` (volume).
- `symphony` (orchestrator), `otel-collector`, `squid`.

Secrets live on the host, owned by `infra-admin`, mounted read-only into the
symphony container:

- `/home/infra-admin/symphony-secrets/bn_dsn` — the beans DSN. Uses the
  **container** host form (`...@postgres:5432/...`), since symphony reaches
  Postgres over the `symphony-internal` network.
- `/home/infra-admin/symphony-secrets/postgres_dsn`, Codex auth — not needed by
  bean-counter.

## Where bean-counter sits

bean-counter is deployed as its **own Compose project** (`bean-counter`) on the
same host, **without a database service**. Two services:

```
                  ┌─────────────────────── bean-counter project ──────────────┐
LAN :8088 ──────► │  ui (nginx)  ──/api/──►  api (fiber)                        │
http://10.0.0.106 │      │                      │                              │
                  └──────┼──────────────────────┼──────────────────────────────┘
                         │                       │  joins external network
                  bean-counter-internal   local-symphony_symphony-internal
                                                 │
                                                 ▼
                                    postgres (local-symphony stack)
                                       resolves as host `postgres:5432`
```

- **api** joins **two** networks: `bean-counter-internal` (so the UI can reach
  it) and the **external** `local-symphony_symphony-internal` (so it can reach
  the shared Postgres by its service name `postgres`).
- **ui** joins only `bean-counter-internal` and proxies `/api/` to
  `http://api:8080` (matches `frontend/nginx.conf` `API_UPSTREAM`).

### Why join the external network instead of `127.0.0.1:5432`

The `bn_dsn` secret already encodes host `postgres`. If the api container is on
`local-symphony_symphony-internal`, `postgres` resolves with **no DSN rewrite**
— simpler and lower-risk than local-symphony's host-rewrite path (that script
rewrites `@postgres:5432`→`@127.0.0.1:5432` only because `bn` runs on the host,
not in a container). Reusing the secret verbatim avoids a transform bug class.

Fallback if joining the external network is undesirable: publish nothing new,
keep Postgres on `127.0.0.1:5432`, set the api to host networking or
`extra_hosts`, and rewrite the DSN host to `127.0.0.1`. Documented as a
fallback only; the external-network path is the recommendation.

## DB connection config for the api

| Var | Value | Why |
|-----|-------|-----|
| `BN_DRIVER` | `postgres` | shared DB is Postgres 16 |
| `BN_DSN` | from secret file (see below) | reuse `bn_dsn` verbatim (`@postgres:5432`) |
| `BN_PROJECT_PREFIX` | `local-symphony` | so bean-counter shows the orchestrator's tracker, not an empty `bean-counter` project |
| `BN_ACTOR` | `bean-counter-ui` | attribution for writes that originate in the UI |
| `BN_CORS_ORIGIN` | `http://10.0.0.106:8088` | the LAN origin serving the UI |
| `BN_ADDR` | `:8080` | container-internal |

`BN_PROJECT_PREFIX=local-symphony` is essential: the prefix scopes every store
query (`internal/store/adapter.go`). With the default `bean-counter` prefix the
UI would show nothing.

## Secret handling — `BN_DSN` must never hit disk or argv

**Grounded gap:** `internal/config/config.go` reads `BN_DSN` from the
environment only — there is **no** file-based DSN support today. Three ways to
get the secret into the api without leaking it into the rendered compose config
(`docker compose config` interpolates `${BN_DSN}` and the deploy record scans
for `postgres://`):

1. **Recommended — add `BN_DSN_FILE` support to bean-counter** (small prep
   task). If `BN_DSN_FILE` is set, `config.Load` reads the DSN from that path.
   The compose api service then sets `BN_DSN_FILE=/run/secrets/bn_dsn` and
   bind-mounts `/home/infra-admin/symphony-secrets/bn_dsn:/run/secrets/bn_dsn:ro`.
   The secret never appears in env, argv, or rendered config. Mirrors how
   local-symphony loads DSNs from `/run/secrets`. → prep task in `06`.
2. Interim fallback (no code change): export `BN_DSN` only in the shell that
   runs `docker compose up` on the remote (read from the secret file in the
   same locked session, never written to disk), and **skip rendering compose
   config with the secret present** / scan a secret-free render. Weaker: any
   `docker compose config` by an operator would expose it.
3. Rejected: writing a `.env` with the DSN — it persists the secret on disk.

The plan adopts option 1 as a prerequisite task; option 2 is the documented
stopgap if the code change slips.

**Readability, not just presence:** the api image runs as the **non-root** user
`bean-counter` (`Dockerfile`), while the host secret is owned by `infra-admin`
and mounted `:ro`. A `0600 infra-admin` secret is unreadable by the container
uid and the api crashes at startup. Preflight must verify the secret is readable
**from the container's uid** (see `02` DSN note), not merely that the host file
exists.

## Ports & conflicts

- UI host port default `8088` (override `--ui-port`). Preflight must verify the
  port is free on the host (`ss -ltn` / `docker ps` check) before `up`.
- The api is **not** published to the host (UI proxies to it over the internal
  network). If direct API access is wanted, publish on `127.0.0.1:<port>` only.
- Confirm `8088` does not collide with anything the symphony stack exposes
  (symphony stack publishes only `127.0.0.1:5432` for Postgres today).

## Remote checkout

bean-counter source must exist at `infra-admin@10.0.0.106:~/git/bean-counter`.
The deploy resolves a target SHA locally, then on the remote `git fetch` +
`git checkout --detach <sha>` and builds images from that checkout (same model
as local-symphony). A **bootstrap** step (clone if missing) is part of the task
sequence; steady-state deploys assume the checkout exists.
