#!/usr/bin/env bash
# scripts/deploy-production.sh — repeatable, audited deploy of bean-counter
# (Go API + Svelte UI) to the infra host, pointed at the EXISTING Postgres that
# the local-symphony orchestrator stack already runs there.
#
# Design and rationale: .agents/plans/deploy/ (00..07). This mirrors the proven
# local-symphony deploy/update-production.sh contract and safety model, adapted
# for a shared database that bean-counter does NOT own.
#
# Safety properties:
#   * Deploy only a SHA reachable from origin/main; never `git pull` after the
#     target SHA is resolved (no time-of-check/time-of-use race).
#   * Hold a single flock'd remote session across every mutating phase.
#   * Beans schema-version parity gate: abort if bean-counter's embedded beans
#     migrations are NEWER than the shared DB (it would migrate production).
#   * Mandatory consistent pg_dump of the shared DB before any mutation.
#   * Operate ONLY on the `bean-counter` compose project; never touch the
#     local-symphony project or the shared Postgres volume.
#   * Never print or persist the DSN; reuse the secret file form verbatim.
#   * A failed safety gate stops the deploy. There is no --force.
#
# Pure helpers (normalize_ui_port, assert_dsn_container_host,
# migration_max_from_dir, extract_issue_count) live above the main guard so
# test/scripts/deploy-production_test.sh can `source` this file and exercise
# them without launching a deploy.

set -euo pipefail

# --------------------------------------------------------------------------- #
# Defaults
# --------------------------------------------------------------------------- #

DEFAULT_HOST="infra-admin@10.0.0.106"
# Literal `$HOME` — expanded on the remote, never locally.
# shellcheck disable=SC2016
DEFAULT_REPO_DIR='$HOME/git/bean-counter'
DEFAULT_UI_PORT="8088"
DEFAULT_API_IMAGE="bean-counter-api:prod"
DEFAULT_UI_IMAGE="bean-counter-ui:prod"
DEFAULT_BN_PROJECT="local-symphony"
DEFAULT_BN_ACTOR="bean-counter-ui"
# shellcheck disable=SC2016
DEFAULT_DSN_SECRET='$HOME/symphony-secrets/bn_dsn'
DEFAULT_SYMPHONY_PROJECT="local-symphony"
DEFAULT_SYMPHONY_NETWORK="local-symphony_symphony-internal"
DEFAULT_PG_SERVICE="postgres"
DEFAULT_PG_USER="symphony"
DEFAULT_PG_DB="symphony"

COMPOSE_PROJECT="bean-counter"
COMPOSE_PROD="deploy/docker-compose.prod.yml"
BEANS_MODULE="github.com/mattsp1290/beans"
REVISION_LABEL="org.opencontainers.image.revision"

HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-180}"

# Runtime config (populated by parse_args).
REMOTE_HOST="$DEFAULT_HOST"
REMOTE_REPO_DIR="$DEFAULT_REPO_DIR"
UI_PORT="$DEFAULT_UI_PORT"
API_IMAGE="$DEFAULT_API_IMAGE"
UI_IMAGE="$DEFAULT_UI_IMAGE"
BN_PROJECT="$DEFAULT_BN_PROJECT"
BN_ACTOR="$DEFAULT_BN_ACTOR"
DSN_SECRET="$DEFAULT_DSN_SECRET"
SYMPHONY_PROJECT="$DEFAULT_SYMPHONY_PROJECT"
SYMPHONY_NETWORK="$DEFAULT_SYMPHONY_NETWORK"
PG_SERVICE="$DEFAULT_PG_SERVICE"
PG_USER="$DEFAULT_PG_USER"
PG_DB="$DEFAULT_PG_DB"
CORS_ORIGIN=""
REF=""
MODE="live"            # live | check | dry-run
SKIP_INTEGRATION=0
SKIP_LOCAL_BUILD=0
BUILD_SSH=0
NO_REBUILD=0

# --------------------------------------------------------------------------- #
# Output helpers
# --------------------------------------------------------------------------- #

log() { printf '[deploy-production] %s\n' "$*" >&2; }

fatal() {
  printf '[deploy-production] ERROR: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'USAGE'
Usage: scripts/deploy-production.sh --ref <ref> [options]

Deploy bean-counter (API + UI) to the infra host, pointed at the shared
local-symphony Postgres. See .agents/plans/deploy/.

Required:
  --ref <ref>            'main' (must equal origin/main) or an exact commit SHA
                         reachable from origin/main. Tags/branches are rejected.

Options:
  --host <ssh-target>    Production SSH target (default: infra-admin@10.0.0.106).
  --repo-dir <path>      Remote checkout. $HOME expanded on the remote
                         (default: $HOME/git/bean-counter).
  --ui-port <port>       Host port for the UI, bound 0.0.0.0 (default: 8088).
  --image-api <tag>      API image tag (default: bean-counter-api:prod).
  --image-ui <tag>       UI image tag (default: bean-counter-ui:prod).
  --bn-project <prefix>  Beans project prefix to view (default: local-symphony).
  --bn-actor <name>      Write attribution (default: bean-counter-ui).
  --cors-origin <url>    CORS origin (default: http://<host-ip>:<ui-port>).
  --dsn-secret <path>    Remote DSN secret file. $HOME expanded on the remote
                         (default: $HOME/symphony-secrets/bn_dsn).
  --symphony-project <n> Compose project owning Postgres (default: local-symphony).
  --symphony-network <n> External Postgres network
                         (default: local-symphony_symphony-internal).
  --pg-user <user>       Shared Postgres user (default: symphony).
  --pg-db <db>           Shared Postgres database (default: symphony).
  --check                Read-only local + remote preflight. No mutation.
  --dry-run              Resolve the target SHA and print the planned phases.
  --skip-integration     Skip the local integration gate (recorded in summary).
  --skip-local-build     Skip the local Docker build gate (recorded in summary).
  --build-ssh            Add `--ssh default` to Docker builds; fails if
                         SSH_AUTH_SOCK is unset.
  --no-rebuild           Skip the remote image rebuild when the live images were
                         built from the exact target SHA (revision label match).
  -h, --help             Show this help.

There is intentionally no --force. A failed safety gate stops the deploy.
USAGE
}

# --------------------------------------------------------------------------- #
# Pure helpers (sourceable; some are mirrored inside the remote payloads)
# --------------------------------------------------------------------------- #

# normalize_ui_port <value> -> echoes the port if it is an integer in [1,65535],
# else returns non-zero. Leading zeros and whitespace are rejected.
normalize_ui_port() {
  local raw="$1"
  case "$raw" in
    ''|*[!0-9]*) return 1 ;;
  esac
  local n=$((10#$raw))
  if [ "$n" -lt 1 ] || [ "$n" -gt 65535 ]; then
    return 1
  fi
  printf '%s\n' "$n"
}

# assert_dsn_container_host <dsn> -> returns 0 if the DSN uses the container
# Postgres host form required by the external-network design (URI `@postgres:5432`
# or libpq `host=postgres`). Returns non-zero otherwise. Never rewrites — unlike
# local-symphony, the api joins the symphony network so `postgres` resolves.
assert_dsn_container_host() {
  local dsn="$1"
  case "$dsn" in
    *'@postgres:5432'*) return 0 ;;
    *'host=postgres '*|*'host=postgres') return 0 ;;
    *) return 1 ;;
  esac
}

# migration_max_from_dir <dir> -> prints the largest leading numeric prefix among
# *.sql files (e.g. 0007_x.sql -> 7). Prints 0 when the dir has no such files.
migration_max_from_dir() {
  local dir="$1" max=0 f base num
  for f in "$dir"/*.sql; do
    [ -e "$f" ] || continue
    base="$(basename "$f")"
    num="${base%%_*}"
    case "$num" in
      ''|*[!0-9]*) continue ;;
    esac
    num=$((10#$num))
    [ "$num" -gt "$max" ] && max="$num"
  done
  printf '%s\n' "$max"
}

# extract_issue_count <issues-json> -> prints the number of issues in a
# GET /api/v1/issues response ({"issues":[...]}). Fails (non-zero) if the body
# does not look like that response (missing the "issues" key).
extract_issue_count() {
  local body="$1"
  case "$body" in
    *'"issues"'*) : ;;
    *) return 1 ;;
  esac
  # Count issue objects by their "identifier" field; an empty array yields 0.
  printf '%s' "$body" | grep -o '"identifier"' | grep -c . || true
}

# --------------------------------------------------------------------------- #
# Local command runner / remote exec
# --------------------------------------------------------------------------- #

# run <cmd...> — echo then execute a local command under strict mode.
run() {
  log "+ $*"
  "$@"
}

# remote_exec <payload> <args...> — pipe a self-contained bash program to the
# infra host over SSH, passing positional arguments safely (never interpolated
# into the program text). Secrets are read on the remote, never passed as argv.
remote_exec() {
  local payload="$1"; shift
  printf '%s\n' "$payload" \
    | ssh -o BatchMode=yes -o ConnectTimeout=10 "$REMOTE_HOST" bash -euo pipefail -s -- "$@"
}

# --------------------------------------------------------------------------- #
# Argument parsing
# --------------------------------------------------------------------------- #

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --ref)              REF="${2:-}"; shift 2 ;;
      --host)             REMOTE_HOST="${2:-}"; shift 2 ;;
      --repo-dir)         REMOTE_REPO_DIR="${2:-}"; shift 2 ;;
      --ui-port)          UI_PORT="${2:-}"; shift 2 ;;
      --image-api)        API_IMAGE="${2:-}"; shift 2 ;;
      --image-ui)         UI_IMAGE="${2:-}"; shift 2 ;;
      --bn-project)       BN_PROJECT="${2:-}"; shift 2 ;;
      --bn-actor)         BN_ACTOR="${2:-}"; shift 2 ;;
      --cors-origin)      CORS_ORIGIN="${2:-}"; shift 2 ;;
      --dsn-secret)       DSN_SECRET="${2:-}"; shift 2 ;;
      --symphony-project) SYMPHONY_PROJECT="${2:-}"; shift 2 ;;
      --symphony-network) SYMPHONY_NETWORK="${2:-}"; shift 2 ;;
      --pg-user)          PG_USER="${2:-}"; shift 2 ;;
      --pg-db)            PG_DB="${2:-}"; shift 2 ;;
      --check)            MODE="check"; shift ;;
      --dry-run)          MODE="dry-run"; shift ;;
      --skip-integration) SKIP_INTEGRATION=1; shift ;;
      --skip-local-build) SKIP_LOCAL_BUILD=1; shift ;;
      --build-ssh)        BUILD_SSH=1; shift ;;
      --no-rebuild)       NO_REBUILD=1; shift ;;
      --skip-smoke)       fatal "--skip-smoke is not supported: the smoke gate is mandatory." ;;
      --force)            fatal "--force is not supported: a failed safety gate must stop the deploy." ;;
      -h|--help)          usage; exit 0 ;;
      *)                  fatal "unknown argument: $1 (try --help)" ;;
    esac
  done

  [ -n "$REF" ] || { usage >&2; fatal "--ref is required"; }

  UI_PORT="$(normalize_ui_port "$UI_PORT")" \
    || fatal "--ui-port must be an integer in [1,65535]"

  if [ -z "$CORS_ORIGIN" ]; then
    local host_part="${REMOTE_HOST##*@}"
    CORS_ORIGIN="http://${host_part}:${UI_PORT}"
  fi
}

# --------------------------------------------------------------------------- #
# Local target resolution and preconditions
# --------------------------------------------------------------------------- #

TARGET_SHA=""
SHORT_SHA=""
EMBEDDED_MAX=""

resolve_target_sha() {
  run git fetch origin

  local origin_main
  origin_main="$(git rev-parse --verify origin/main)" \
    || fatal "could not resolve origin/main; is origin reachable?"

  if [ "$REF" = "main" ]; then
    TARGET_SHA="$(git rev-parse --verify 'main^{commit}')" \
      || fatal "could not resolve local 'main'"
    if [ "$TARGET_SHA" != "$origin_main" ]; then
      fatal "local main ($TARGET_SHA) does not match origin/main ($origin_main); pull/rebase first"
    fi
  elif printf '%s' "$REF" | grep -qE '^[0-9a-f]{7,40}$'; then
    TARGET_SHA="$(git rev-parse --verify "${REF}^{commit}" 2>/dev/null)" \
      || fatal "commit $REF does not resolve locally"
    if ! git branch -r --contains "$TARGET_SHA" 2>/dev/null | grep -qE '(^|[[:space:]])origin/main$'; then
      fatal "commit $TARGET_SHA is not reachable from origin/main; push it to main first"
    fi
  else
    fatal "unsupported ref '$REF': only 'main' or an exact commit SHA reachable from origin/main are allowed"
  fi

  SHORT_SHA="$(git rev-parse --short=12 "$TARGET_SHA")"
  log "resolved ref '$REF' -> $TARGET_SHA (origin/main=$origin_main)"
}

# Compute bean-counter's embedded beans migration max from the pinned module so
# the remote parity gate compares against the version actually shipping.
resolve_embedded_migration_max() {
  local beans_dir
  beans_dir="$(go list -m -f '{{.Dir}}' "$BEANS_MODULE" 2>/dev/null)" \
    || fatal "go list -m $BEANS_MODULE failed; module graph is not deployable"
  [ -n "$beans_dir" ] || fatal "could not locate $BEANS_MODULE in the module cache"
  EMBEDDED_MAX="$(migration_max_from_dir "$beans_dir/schema/migrations/postgres")"
  case "$EMBEDDED_MAX" in
    ''|*[!0-9]*) fatal "could not determine embedded beans migration max (got '$EMBEDDED_MAX')" ;;
  esac
  [ "$EMBEDDED_MAX" -gt 0 ] || fatal "embedded beans migration max is 0; module layout unexpected"
  log "embedded beans postgres migration max = $EMBEDDED_MAX"
}

require_clean_local_ref() {
  local porcelain
  porcelain="$(git status --porcelain)"
  if [ -n "$porcelain" ]; then
    printf '%s\n' "$porcelain" >&2
    fatal "local worktree is not clean (tracked changes or untracked files); commit/stash first"
  fi

  if grep -nE 'replace[[:space:]]+.*github\.com/mattsp1290/beans' go.mod >/dev/null 2>&1; then
    fatal "go.mod contains a local replace for $BEANS_MODULE; remove it before deploying"
  fi

  if ! go list -m -json "$BEANS_MODULE" >/dev/null 2>&1; then
    fatal "go list -m -json $BEANS_MODULE failed; module graph is not deployable"
  fi
}

# Heavy local gates: unit+e2e, integration, frontend, and a labeled Docker build.
LOCAL_PREFLIGHT=""

local_gates() {
  LOCAL_PREFLIGHT="ref=$REF target_sha=$TARGET_SHA host=$(hostname 2>/dev/null || echo '?')"

  run go test ./...
  LOCAL_PREFLIGHT="$LOCAL_PREFLIGHT
go test ./... (incl. e2e): PASS"

  if [ "$SKIP_INTEGRATION" -eq 1 ]; then
    log "skipping integration gate (--skip-integration)"
    LOCAL_PREFLIGHT="$LOCAL_PREFLIGHT
integration: SKIPPED (--skip-integration)"
  else
    run go test -tags=integration ./...
    LOCAL_PREFLIGHT="$LOCAL_PREFLIGHT
integration: PASS"
  fi

  run make vet
  run make lint
  run make fmt-check
  LOCAL_PREFLIGHT="$LOCAL_PREFLIGHT
vet/lint/fmt-check: PASS"

  ( cd frontend && run npm ci && run npm run check && run npm test && run npm run build )
  LOCAL_PREFLIGHT="$LOCAL_PREFLIGHT
frontend check/test/build: PASS"

  if [ "$SKIP_LOCAL_BUILD" -eq 1 ]; then
    log "skipping local Docker build gate (--skip-local-build)"
    log "WARNING: image-build failures will first surface on the remote, after the pg_dump"
    LOCAL_PREFLIGHT="$LOCAL_PREFLIGHT
local docker build: SKIPPED (--skip-local-build)"
  else
    local -a ssh_args=()
    if [ "$BUILD_SSH" -eq 1 ]; then
      [ -n "${SSH_AUTH_SOCK:-}" ] || fatal "--build-ssh set but SSH_AUTH_SOCK is empty; start an ssh-agent"
      ssh_args=(--ssh default)
    fi
    DOCKER_BUILDKIT=1 run docker build "${ssh_args[@]}" \
      --label "$REVISION_LABEL=$TARGET_SHA" -t "$API_IMAGE" .
    DOCKER_BUILDKIT=1 run docker build "${ssh_args[@]}" \
      --label "$REVISION_LABEL=$TARGET_SHA" -t "$UI_IMAGE" ./frontend
    LOCAL_PREFLIGHT="$LOCAL_PREFLIGHT
local docker build: PASS$( [ "$BUILD_SSH" -eq 1 ] && echo ' (--ssh default)')"
  fi
}

# --------------------------------------------------------------------------- #
# Remote payloads
# --------------------------------------------------------------------------- #

# Read-only remote preflight for --check. No lock, no mutation.
#   $1 repo_dir $2 compose_project $3 compose_prod $4 symphony_project
#   $5 symphony_network $6 pg_service $7 pg_user $8 pg_db $9 dsn_secret
#   $10 ui_port $11 embedded_max
remote_check_payload() {
  cat <<'PAYLOAD'
repo_dir="$1"; compose_project="$2"; compose_prod="$3"; symphony_project="$4"
symphony_network="$5"; pg_service="$6"; pg_user="$7"; pg_db="$8"; dsn_secret="$9"
ui_port="${10}"; embedded_max="${11}"

expand_home() { case "$1" in '$HOME'/*) printf '%s\n' "$HOME/${1#'$HOME'/}";; '~'/*) printf '%s\n' "$HOME/${1#'~'/}";; *) printf '%s\n' "$1";; esac; }
repo_dir="$(expand_home "$repo_dir")"
dsn_secret="$(expand_home "$dsn_secret")"
say() { printf '%s\n' "$*"; }

say "== host =="
hostname; uname -a; docker --version; docker compose version

[ -d "$repo_dir" ] || { echo "FAIL: repo dir missing: $repo_dir" >&2; exit 1; }
cd "$repo_dir" || exit 1
say "== git =="
git status --short --branch; git log --oneline -3

say "== shared Postgres container =="
pg_id="$(docker ps -q -f "label=com.docker.compose.project=$symphony_project" -f "label=com.docker.compose.service=$pg_service")"
[ -n "$pg_id" ] || { echo "FAIL: no running $symphony_project/$pg_service container" >&2; exit 1; }
[ "$(printf '%s\n' "$pg_id" | grep -c .)" -eq 1 ] || { echo "FAIL: multiple $pg_service containers matched" >&2; exit 1; }
health="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$pg_id")"
say "postgres container $pg_id health=$health"

say "== external network =="
docker network inspect "$symphony_network" >/dev/null 2>&1 \
  || { echo "FAIL: external network missing: $symphony_network" >&2; exit 1; }
say "network present: $symphony_network"

say "== schema-version parity =="
tbl="$(docker exec "$pg_id" psql -U "$pg_user" -d "$pg_db" -tAc "select to_regclass('bn_schema_versions') is not null" </dev/null 2>/dev/null || true)"
[ "$tbl" = "t" ] || { echo "FAIL: bn_schema_versions not found in $pg_db; not a beans DB?" >&2; exit 1; }
db_max="$(docker exec "$pg_id" psql -U "$pg_user" -d "$pg_db" -tAc "select coalesce(max(version_id),0) from bn_schema_versions" </dev/null 2>/dev/null | tr -dc '0-9')"
db_max="${db_max:-0}"
say "embedded_max=$embedded_max db_max=$db_max"
if [ "$embedded_max" -gt "$db_max" ]; then
  echo "FAIL: bean-counter embedded migrations ($embedded_max) are NEWER than prod ($db_max); deploying would migrate the shared schema" >&2
  exit 1
fi

say "== DSN secret (presence + container-host form; contents never printed) =="
[ -f "$dsn_secret" ] || { echo "FAIL: DSN secret missing: $dsn_secret" >&2; exit 1; }
if ! grep -qE '@postgres:5432|host=postgres([[:space:]]|$)' "$dsn_secret"; then
  echo "FAIL: DSN secret does not use the container host form (@postgres:5432 / host=postgres)" >&2
  exit 1
fi
say "dsn secret present and container-host form ok"

say "== UI port free =="
if (command -v ss >/dev/null 2>&1 && ss -ltn 2>/dev/null | grep -qE "[:.]${ui_port}\\b") \
   || (command -v netstat >/dev/null 2>&1 && netstat -ltn 2>/dev/null | grep -qE "[:.]${ui_port}\\b"); then
  echo "FAIL: UI port $ui_port already in use on host" >&2; exit 1
fi
say "ui port $ui_port free"

say "== compose config render + secret scan + db-service check =="
if ! UI_PORT="$ui_port" SYMPHONY_NETWORK="$symphony_network" BN_DSN_SECRET="$dsn_secret" \
     docker compose -p "$compose_project" -f "$compose_prod" config > /tmp/bc-check-config.$$ 2>/tmp/bc-check-config.err.$$; then
  echo "FAIL: docker compose config did not render" >&2
  sed -E 's#postgres://[^[:space:]"]*#postgres://REDACTED#g' /tmp/bc-check-config.err.$$ >&2 || true
  rm -f /tmp/bc-check-config.$$ /tmp/bc-check-config.err.$$
  exit 1
fi
if grep -Eiq 'postgres://|password=' /tmp/bc-check-config.$$; then
  echo "FAIL: rendered compose config may contain secret values" >&2
  rm -f /tmp/bc-check-config.$$ /tmp/bc-check-config.err.$$; exit 1
fi
if grep -qE '^\s{2,}db:\s*$' /tmp/bc-check-config.$$; then
  echo "FAIL: rendered compose unexpectedly contains a db service" >&2
  rm -f /tmp/bc-check-config.$$ /tmp/bc-check-config.err.$$; exit 1
fi
rm -f /tmp/bc-check-config.$$ /tmp/bc-check-config.err.$$
say "compose config renders, secret-free, no db service"
say "CHECK OK"
PAYLOAD
}

# Full live deploy program. Runs in ONE locked remote session.
#   $1 target_sha $2 short_sha $3 repo_dir $4 api_image $5 ui_image
#   $6 compose_project $7 compose_prod $8 bn_project $9 bn_actor $10 cors_origin
#   $11 ui_port $12 dsn_secret $13 symphony_project $14 symphony_network
#   $15 pg_service $16 pg_user $17 pg_db $18 embedded_max $19 no_rebuild
#   $20 user_ref $21 local_preflight $22 health_timeout $23 revision_label
remote_deploy_payload() {
  cat <<'PAYLOAD'
target_sha="$1"; short_sha="$2"; repo_dir="$3"; api_image="$4"; ui_image="$5"
compose_project="$6"; compose_prod="$7"; bn_project="$8"; bn_actor="$9"; cors_origin="${10}"
ui_port="${11}"; dsn_secret="${12}"; symphony_project="${13}"; symphony_network="${14}"
pg_service="${15}"; pg_user="${16}"; pg_db="${17}"; embedded_max="${18}"; no_rebuild="${19}"
user_ref="${20}"; local_preflight="${21}"; health_timeout="${22}"; revision_label="${23}"

expand_home() { case "$1" in '$HOME'/*) printf '%s\n' "$HOME/${1#'$HOME'/}";; '~'/*) printf '%s\n' "$HOME/${1#'~'/}";; *) printf '%s\n' "$1";; esac; }
repo_dir="$(expand_home "$repo_dir")"
dsn_secret="$(expand_home "$dsn_secret")"

current_phase="init"
run_dir=""
say() { printf '%s\n' "$*"; }

redact() {
  sed -E \
    -e 's#postgres://[^[:space:]"'"'"']*#postgres://REDACTED#g' \
    -e 's#([Pp][Aa][Ss][Ss][Ww][Oo][Rr][Dd]=)[^[:space:]"'"'"']+#\1REDACTED#g'
}

dcp() { docker compose -p "$compose_project" -f "$compose_prod" "$@"; }

on_exit() {
  local ec=$?
  [ "$ec" -eq 0 ] && return 0
  echo "" >&2
  echo "DEPLOY FAILED in phase '${current_phase}' (exit ${ec})" >&2
  if [ -n "$run_dir" ] && [ -d "$run_dir" ]; then
    echo "deploy-run dir: $run_dir" >&2
    echo "FAILED phase: $current_phase (exit $ec)" >> "$run_dir/FAILED.txt" 2>/dev/null || true
    dcp ps > "$run_dir/docker-after.txt" 2>&1 || true
    for svc in api ui; do
      cid="$(dcp ps -q "$svc" 2>/dev/null || true)"
      [ -n "$cid" ] && docker logs --tail=200 "$cid" 2>&1 | redact > "$run_dir/$svc-logs.txt" || true
    done
    [ -f "$run_dir/rollback.md" ] && echo "rollback: see $run_dir/rollback.md" >&2
  fi
}
trap on_exit EXIT

# ----- lock ---------------------------------------------------------------- #
current_phase="lock"
lock_dir="$HOME/.agents/deploy-runs/$compose_project"
mkdir -p "$lock_dir"
lock_file="$lock_dir/deploy.lock"
exec 9>"$lock_file"
if ! flock -n 9; then
  echo "another deploy holds $lock_file" >&2
  exit 1
fi

# ----- deploy-run directory ------------------------------------------------ #
current_phase="run-dir"
stamp="$(date +%Y%m%d-%H%M%S)"
run_dir="$lock_dir/$stamp-$short_sha"
mkdir -p "$run_dir"
{
  echo "pid=$$"; echo "ref=$user_ref"; echo "target_sha=$target_sha"
  echo "start=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
} > "$lock_dir/deploy.lock.meta"
say "deploy-run dir: $run_dir"
printf '%s\n' "$local_preflight" > "$run_dir/local-preflight.txt"

[ -d "$repo_dir" ] || { echo "FAIL: repo dir missing: $repo_dir" >&2; exit 1; }
cd "$repo_dir" || exit 1

# ----- remote preflight ---------------------------------------------------- #
current_phase="remote-preflight"
{
  echo "== host =="; hostname; uname -a; docker --version; docker compose version
  echo "== docker =="; docker ps; docker network ls
} > "$run_dir/remote-preflight.txt" 2>&1

{ echo "== git before =="; git status --short --branch; git log --oneline -5; } > "$run_dir/git-before.txt" 2>&1
if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
  echo "FAIL: remote checkout has tracked changes" >&2
  git status --porcelain --untracked-files=no >&2; exit 1
fi

# Shared Postgres container (by compose labels — no container_name to rely on).
pg_id="$(docker ps -q -f "label=com.docker.compose.project=$symphony_project" -f "label=com.docker.compose.service=$pg_service")"
[ -n "$pg_id" ] || { echo "FAIL: no running $symphony_project/$pg_service container" >&2; exit 1; }
[ "$(printf '%s\n' "$pg_id" | grep -c .)" -eq 1 ] || { echo "FAIL: multiple $pg_service containers matched" >&2; exit 1; }

docker network inspect "$symphony_network" >/dev/null 2>&1 \
  || { echo "FAIL: external network missing: $symphony_network" >&2; exit 1; }

[ -f "$dsn_secret" ] || { echo "FAIL: DSN secret missing: $dsn_secret" >&2; exit 1; }
if ! grep -qE '@postgres:5432|host=postgres([[:space:]]|$)' "$dsn_secret"; then
  echo "FAIL: DSN secret does not use the container host form" >&2; exit 1
fi

if (command -v ss >/dev/null 2>&1 && ss -ltn 2>/dev/null | grep -qE "[:.]${ui_port}\\b") \
   || (command -v netstat >/dev/null 2>&1 && netstat -ltn 2>/dev/null | grep -qE "[:.]${ui_port}\\b"); then
  echo "FAIL: UI port $ui_port already in use on host" >&2; exit 1
fi

# ----- version-parity gate (critical) -------------------------------------- #
current_phase="version-parity"
tbl="$(docker exec "$pg_id" psql -U "$pg_user" -d "$pg_db" -tAc "select to_regclass('bn_schema_versions') is not null" </dev/null 2>/dev/null || true)"
[ "$tbl" = "t" ] || { echo "FAIL: bn_schema_versions not found in $pg_db; not a beans DB?" >&2; exit 1; }
db_max="$(docker exec "$pg_id" psql -U "$pg_user" -d "$pg_db" -tAc "select coalesce(max(version_id),0) from bn_schema_versions" </dev/null 2>/dev/null | tr -dc '0-9')"
db_max="${db_max:-0}"
{ echo "embedded_max=$embedded_max"; echo "db_max=$db_max"; } > "$run_dir/version-parity.txt"
if [ "$embedded_max" -gt "$db_max" ]; then
  echo "FAIL: bean-counter embedded migrations ($embedded_max) NEWER than prod ($db_max); would migrate shared schema" >&2
  exit 1
fi
say "version parity ok (embedded=$embedded_max <= db=$db_max)"

# ----- compose config render + secret scan --------------------------------- #
current_phase="compose-config"
export UI_PORT="$ui_port" SYMPHONY_NETWORK="$symphony_network" BN_DSN_SECRET="$dsn_secret"
export BEAN_COUNTER_API_IMAGE="$api_image" BEAN_COUNTER_UI_IMAGE="$ui_image"
export BN_PROJECT_PREFIX="$bn_project" BN_ACTOR="$bn_actor" BN_CORS_ORIGIN="$cors_origin"
if ! dcp config > "$run_dir/compose-config.yml" 2>"$run_dir/compose-config.err.raw"; then
  redact < "$run_dir/compose-config.err.raw" > "$run_dir/compose-config.err"
  rm -f "$run_dir/compose-config.err.raw" "$run_dir/compose-config.yml"
  echo "FAIL: docker compose config did not render" >&2
  cat "$run_dir/compose-config.err" >&2; exit 1
fi
rm -f "$run_dir/compose-config.err.raw"
if grep -Eiq 'postgres://|password=' "$run_dir/compose-config.yml"; then
  echo "FAIL: rendered compose config may contain secret values" >&2
  rm -f "$run_dir/compose-config.yml"; exit 1
fi
if grep -qE '^\s{2,}db:\s*$' "$run_dir/compose-config.yml"; then
  echo "FAIL: rendered compose unexpectedly contains a db service" >&2; exit 1
fi

# ----- backup (mandatory, before any mutation) ----------------------------- #
current_phase="backup"
previous_sha="$(git rev-parse HEAD)"
previous_api_id="$(docker image inspect "$api_image" --format '{{.Id}}' 2>/dev/null || true)"
previous_ui_id="$(docker image inspect "$ui_image" --format '{{.Id}}' 2>/dev/null || true)"

tmp_dump="$run_dir/symphony.sql.partial"
if docker exec "$pg_id" pg_dump -Fc -U "$pg_user" -d "$pg_db" </dev/null > "$tmp_dump" && [ -s "$tmp_dump" ]; then
  mv -f "$tmp_dump" "$run_dir/symphony.dump"
  wc -c "$run_dir/symphony.dump" > "$run_dir/symphony.dump.size"
else
  rm -f "$tmp_dump"
  echo "FAIL: mandatory pg_dump failed or empty; aborting before mutation" >&2
  exit 1
fi

current_phase="rollback-doc"
{
  echo "# Rollback for deploy $stamp-$short_sha"
  echo
  echo "- previous_sha: \`$previous_sha\`"
  echo "- previous_api_image_id: \`${previous_api_id:-<none>}\`"
  echo "- previous_ui_image_id: \`${previous_ui_id:-<none>}\`"
  echo "- target_sha: \`$target_sha\`"
  echo "- shared-DB dump (forensic; restore is a COORDINATED manual op): \`$run_dir/symphony.dump\`"
  echo
  echo "## Preferred rollback: retag the previous images (no data touched)"
  echo
  echo '```bash'
  echo "cd $repo_dir"
  echo "docker compose -p $compose_project -f $compose_prod stop api ui"
  if [ -n "$previous_api_id" ]; then echo "docker tag $previous_api_id $api_image"; else echo "# no previous api image id; rebuild from $previous_sha"; fi
  if [ -n "$previous_ui_id" ];  then echo "docker tag $previous_ui_id $ui_image";  else echo "# no previous ui image id; rebuild from $previous_sha"; fi
  echo "UI_PORT=$ui_port SYMPHONY_NETWORK=$symphony_network BN_DSN_SECRET=$dsn_secret \\"
  echo "  BEAN_COUNTER_API_IMAGE=$api_image BEAN_COUNTER_UI_IMAGE=$ui_image \\"
  echo "  BN_PROJECT_PREFIX=$bn_project BN_ACTOR=$bn_actor BN_CORS_ORIGIN=$cors_origin \\"
  echo "  docker compose -p $compose_project -f $compose_prod up -d --no-build api ui"
  echo '```'
  echo
  echo "## Back out entirely (orchestrator untouched)"
  echo
  echo '```bash'
  echo "docker compose -p $compose_project -f $compose_prod down   # NEVER -v: shared volume is owned by $symphony_project"
  echo '```'
  echo
  echo "Data rollback (shared DB) is a coordinated manual operation — stop the"
  echo "orchestrator first, then pg_restore $run_dir/symphony.dump. Never automated."
} > "$run_dir/rollback.md"

# ----- checkout + build ---------------------------------------------------- #
current_phase="checkout"
git fetch origin
git checkout --detach "$target_sha"
[ "$(git rev-parse HEAD)" = "$target_sha" ] || { echo "FAIL: checkout did not land target SHA" >&2; exit 1; }
{ echo "== git after =="; git status --short --branch; git log --oneline -5; } > "$run_dir/git-after.txt" 2>&1

current_phase="build"
rebuilt="yes"
img_rev() { docker image inspect "$1" --format "{{index .Config.Labels \"$revision_label\"}}" 2>/dev/null || true; }
if [ "$no_rebuild" -eq 1 ] && [ "$(img_rev "$api_image")" = "$target_sha" ] && [ "$(img_rev "$ui_image")" = "$target_sha" ]; then
  say "skipping rebuild (--no-rebuild; live images already built from $short_sha)"
  rebuilt="no (--no-rebuild; revision label matched)"
else
  DOCKER_BUILDKIT=1 docker build --label "$revision_label=$target_sha" -t "$api_image" . 2>&1 | tee "$run_dir/build-api.txt"
  DOCKER_BUILDKIT=1 docker build --label "$revision_label=$target_sha" -t "$ui_image" ./frontend 2>&1 | tee "$run_dir/build-ui.txt"
fi
new_api_id="$(docker image inspect "$api_image" --format '{{.Id}}')"
new_ui_id="$(docker image inspect "$ui_image" --format '{{.Id}}')"
{ echo "api=$new_api_id"; echo "ui=$new_ui_id"; } > "$run_dir/image-ids.txt"

# ----- secret readability from the api container uid ----------------------- #
current_phase="secret-readability"
if ! docker run --rm --entrypoint sh \
       -v "$dsn_secret":/run/secrets/bn_dsn:ro "$api_image" \
       -c 'test -r /run/secrets/bn_dsn && head -c1 /run/secrets/bn_dsn >/dev/null' </dev/null >/dev/null 2>&1; then
  echo "FAIL: api container uid cannot read the mounted DSN secret ($dsn_secret)" >&2
  echo "       make it readable by the image user, or copy it to a bean-counter-owned path" >&2
  exit 1
fi
say "secret readable by api container uid"

# ----- compose up + health ------------------------------------------------- #
current_phase="compose-up"
{ echo "== docker before =="; dcp ps; } > "$run_dir/docker-before.txt" 2>&1
dcp up -d --no-build api ui

health_of() { docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$1"; }
wait_health() {
  local id="$1" timeout="$2" label="$3" deadline val
  deadline=$(( $(date +%s) + timeout ))
  while :; do
    val="$(health_of "$id" 2>/dev/null || true)"
    [ "$val" = "healthy" ] && return 0
    if [ "$(date +%s)" -ge "$deadline" ]; then
      echo "FAIL: $label not healthy within ${timeout}s (last: '${val:-<none>}')" >&2
      return 1
    fi
    sleep 3
  done
}

api_id="$(dcp ps -q api)"; [ -n "$api_id" ] || { echo "FAIL: api container not found" >&2; exit 1; }
ui_id="$(dcp ps -q ui)";   [ -n "$ui_id" ] || { echo "FAIL: ui container not found" >&2; exit 1; }
wait_health "$api_id" "$health_timeout" "api"
wait_health "$ui_id"  "$health_timeout" "ui"

docker logs --tail=200 "$api_id" 2>&1 | redact > "$run_dir/api-startup.txt"
if docker logs --tail=200 "$api_id" 2>&1 | grep -Eiq 'ensure project|new tracker|store: |migrate|ErrEmptyDSN'; then
  echo "FAIL: api startup logs report a store/DSN/migration error" >&2
  exit 1
fi
{ echo "== docker after up =="; dcp ps; } > "$run_dir/docker-after.txt" 2>&1

# ----- smoke gate (read-only) ---------------------------------------------- #
current_phase="smoke"
base="http://127.0.0.1:$ui_port"
get() { wget -qO- --timeout=10 "$1"; }

hz="$(get "$base/healthz" || true)"
case "$hz" in *ok*) : ;; *) echo "FAIL: UI /healthz did not return ok (got: ${hz:-<none>})" >&2; exit 1;; esac

rz="$(get "$base/api/v1/readyz" || true)"
[ -n "$rz" ] || { echo "FAIL: /api/v1/readyz returned empty" >&2; exit 1; }

issues="$(get "$base/api/v1/issues?limit=1" || true)"
case "$issues" in
  *'"issues"'*) : ;;
  *) echo "FAIL: /api/v1/issues did not return a valid issues response" >&2; exit 1 ;;
esac
issue_hits="$(printf '%s' "$issues" | grep -o '"identifier"' | grep -c . || true)"
prefix_seen="no"
printf '%s' "$issues" | grep -q "\"${bn_project}-" && prefix_seen="yes"
{
  echo "healthz=ok"; echo "readyz=ok"
  echo "issues_response_valid=yes issues_in_first_page=$issue_hits"
  echo "project_prefix_${bn_project}_seen=$prefix_seen (informational; empty tracker is a valid pass)"
} > "$run_dir/smoke.txt"
say "smoke ok (issues_in_first_page=$issue_hits, prefix_seen=$prefix_seen)"

# ----- summary ------------------------------------------------------------- #
current_phase="summary"
{
  echo "# Deploy summary $stamp-$short_sha"
  echo
  echo "- user_ref: \`$user_ref\`"
  echo "- target_sha: \`$target_sha\`"
  echo "- previous_sha: \`$previous_sha\`"
  echo "- api_image: \`$api_image\` (\`$new_api_id\`)"
  echo "- ui_image: \`$ui_image\` (\`$new_ui_id\`)"
  echo "- rebuilt: $rebuilt"
  echo "- ui_port: $ui_port  cors_origin: $cors_origin"
  echo "- bn_project: \`$bn_project\`  bn_actor: \`$bn_actor\`"
  echo "- version parity: embedded=$embedded_max db=$db_max"
  echo "- shared-DB dump: \`$run_dir/symphony.dump\`"
  echo "- smoke: issues_in_first_page=$issue_hits prefix_seen=$prefix_seen"
  echo
  echo "## Local preflight"
  echo '```'
  printf '%s\n' "$local_preflight"
  echo '```'
} > "$run_dir/summary.md"

say "DEPLOY OK"
say "run_dir=$run_dir"
PAYLOAD
}

# --------------------------------------------------------------------------- #
# Mode drivers
# --------------------------------------------------------------------------- #

print_plan() {
  cat >&2 <<PLAN
Planned deploy:
  mode:          $MODE
  host:          $REMOTE_HOST
  repo-dir:      $REMOTE_REPO_DIR
  ref:           $REF
  target_sha:    $TARGET_SHA
  short_sha:     $SHORT_SHA
  ui-port:       $UI_PORT   cors: $CORS_ORIGIN
  images:        $API_IMAGE , $UI_IMAGE
  compose:       -p $COMPOSE_PROJECT -f $COMPOSE_PROD
  bn project:    $BN_PROJECT   actor: $BN_ACTOR
  shared pg:     project=$SYMPHONY_PROJECT service=$PG_SERVICE db=$PG_DB user=$PG_USER
  network:       $SYMPHONY_NETWORK
  embedded_max:  $EMBEDDED_MAX (beans migrations shipping in bean-counter)

Phases:
  1. local: clean worktree, pushed ref, go.mod (no beans replace), embedded max
  2. local gates: go test ./... ; integration$( [ "$SKIP_INTEGRATION" -eq 1 ] && echo " [SKIPPED]") ; vet/lint/fmt ; frontend ; docker build$( [ "$SKIP_LOCAL_BUILD" -eq 1 ] && echo " [SKIPPED]")
  3. remote (one flock'd session): preflight -> version-parity gate ->
     compose config (secret scan, no db) -> pg_dump backup + rollback.md ->
     checkout $SHORT_SHA -> build$( [ "$NO_REBUILD" -eq 1 ] && echo " [skip if revision matches]") -> secret-readability ->
     compose up api ui -> health -> read-only smoke
  4. records under ~/.agents/deploy-runs/$COMPOSE_PROJECT/<stamp>-$SHORT_SHA/
PLAN
}

do_dry_run() {
  print_plan
  log "dry-run: checking SSH connectivity only (no remote mutation)"
  if ssh -o BatchMode=yes -o ConnectTimeout=10 "$REMOTE_HOST" true 2>/dev/null; then
    log "ssh connectivity: ok"
  else
    log "ssh connectivity: FAILED (BatchMode); live deploy would abort"
  fi
  log "dry-run complete: no production state was changed"
}

do_check() {
  require_clean_local_ref
  log "local preconditions ok; running read-only remote preflight"
  remote_exec "$(remote_check_payload)" \
    "$REMOTE_REPO_DIR" "$COMPOSE_PROJECT" "$COMPOSE_PROD" "$SYMPHONY_PROJECT" \
    "$SYMPHONY_NETWORK" "$PG_SERVICE" "$PG_USER" "$PG_DB" "$DSN_SECRET" \
    "$UI_PORT" "$EMBEDDED_MAX"
  log "check complete: no production state was changed"
}

do_live() {
  require_clean_local_ref
  local_gates
  log "local gates passed; starting locked remote deploy"
  remote_exec "$(remote_deploy_payload)" \
    "$TARGET_SHA" "$SHORT_SHA" "$REMOTE_REPO_DIR" "$API_IMAGE" "$UI_IMAGE" \
    "$COMPOSE_PROJECT" "$COMPOSE_PROD" "$BN_PROJECT" "$BN_ACTOR" "$CORS_ORIGIN" \
    "$UI_PORT" "$DSN_SECRET" "$SYMPHONY_PROJECT" "$SYMPHONY_NETWORK" \
    "$PG_SERVICE" "$PG_USER" "$PG_DB" "$EMBEDDED_MAX" "$NO_REBUILD" \
    "$REF" "$LOCAL_PREFLIGHT" "$HEALTH_TIMEOUT" "$REVISION_LABEL"
  log "live deploy complete"
}

main() {
  parse_args "$@"
  resolve_target_sha
  resolve_embedded_migration_max

  case "$MODE" in
    dry-run) do_dry_run ;;
    check)   do_check ;;
    live)    do_live ;;
    *)       fatal "internal: unknown mode $MODE" ;;
  esac
}

# --------------------------------------------------------------------------- #
# Main guard — lets tests source pure helpers without running a deploy.
# --------------------------------------------------------------------------- #

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi
