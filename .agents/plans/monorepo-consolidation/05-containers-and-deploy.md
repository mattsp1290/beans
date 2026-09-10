# 05 — Containers and deploy

Goal: the bean-counter API image, the UI image, and the production deploy script all
work from the monorepo layout, and the deploy script's safety properties survive the
change to a filesystem `replace` dependency.

Prerequisite state: [03-go-module-restructure.md](03-go-module-restructure.md) complete.

This is the highest-risk work package in the plan. The deploy script encodes safety
gates that were designed around bean-counter consuming a *published* beans module. That
premise no longer holds.

## Finding: bean-counter is not yet deployed

`bd list --status=open` in bean-counter reports `bean-counter-m0p` ("First live
production deploy of bean-counter") and `bean-counter-log` ("Bootstrap remote
bean-counter checkout + DSN secret on infra host") both open, and `bean-counter-am5`
("Run `--check` against host") open. The reasonable reading is that no remote checkout
exists at `$HOME/git/bean-counter` on the infra host and no production stack is running.

Verify before assuming it. From a shell with access to the infra host:

```bash
ssh -o BatchMode=yes infra-admin@10.0.0.106 \
  'ls -d "$HOME/git/bean-counter" 2>/dev/null; docker compose -p bean-counter ps 2>/dev/null'
```

- **Empty output**: no remote state exists. The deploy-script edits below are a pure
  source change with no migration.
- **Non-empty output**: a remote checkout or a running stack exists. Stop and treat the
  remote-side move as its own work package with a rollback path. Do not proceed on the
  assumption in this section.

If the infra host is unreachable from the implementing environment, record that the
check could not be run and treat the remote as possibly-existing. Do not record an
unverified "nothing is deployed" as a fact.

## API image — `apps/bean-counter/Dockerfile`

The current build stage is:

```dockerfile
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bean-counter ./cmd/bean-counter
```

This breaks in the monorepo: `go.mod` now has a `replace` pointing at `../../libs/beans`,
which is outside the build context and is not copied.

Replace the build stage with a root-context build:

```dockerfile
FROM golang:1.25.7-alpine AS build

WORKDIR /src

COPY libs/beans/go.mod libs/beans/go.sum ./libs/beans/
COPY apps/bean-counter/go.mod apps/bean-counter/go.sum ./apps/bean-counter/

WORKDIR /src/apps/bean-counter
RUN go mod download

WORKDIR /src
COPY libs/beans ./libs/beans
COPY apps/bean-counter/cmd ./apps/bean-counter/cmd
COPY apps/bean-counter/internal ./apps/bean-counter/internal

WORKDIR /src/apps/bean-counter
ENV CGO_ENABLED=0
ENV GOWORK=off
RUN GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bean-counter ./cmd/bean-counter
```

Notes on this shape:

- The build context becomes the repository root. Every `docker build` invocation must
  change accordingly; see the table below.
- `go.work` is deliberately not copied, and `GOWORK=off` is set. The image builds through
  the `replace` directive alone. This is decision D3 in [00-overview.md](00-overview.md).
- `go mod download` runs before the source copy so the dependency layer still caches. It
  runs from `apps/bean-counter`, and it needs `libs/beans/go.mod` present because the
  `replace` target's `go.mod` is read during module-graph loading. Its source is not
  needed yet, which is why only the two `go.mod`/`go.sum` pairs are copied first.
- `COPY libs/beans ./libs/beans` copies the whole library, including
  `schema/migrations/*/*.sql`, which `libs/beans/schema/schema.go` embeds. Copying only
  `libs/beans/model`, `repo`, and `store` would break the embed at build time.

The runtime stage is unchanged. All `BN_*` environment defaults, the `bean-counter`
user, and `/data` stay exactly as they are.

## `.dockerignore`

`apps/bean-counter/.dockerignore` no longer applies, because Docker reads
`.dockerignore` from the build-context root. Create a root `.dockerignore` carrying the
same intent, widened to the monorepo:

```text
.git
.github
.agents
.claude
.beads
**/.beads
**/bin
**/node_modules
**/frontend/dist
**/*.db
**/*.db-wal
**/*.db-shm
**/*.sqlite
**/*.sqlite-wal
**/*.sqlite-shm
**/.env
**/.env.*
**/.envrc
**/.npmrc
**/.yarnrc*
**/.DS_Store
```

The original excluded `frontend` wholesale because the API image never needed it. The
root file excludes `**/node_modules` and `**/frontend/dist` instead, because the UI
image's context is still the frontend directory and a blanket `frontend` exclusion at
the root would be wrong once the third application ships a UI.

`Dockerfile` and `.dockerignore` were themselves excluded in the original. Leave them
out of the root file; excluding Dockerfiles from a multi-app context is more likely to
confuse than to help.

Delete `apps/bean-counter/.dockerignore` after the root file is in place, so there is one
authority. `apps/bean-counter/frontend/.dockerignore` stays, because the UI image keeps
its own narrower context.

## UI image

`apps/bean-counter/frontend/Dockerfile` and `nginx.conf` need no content change. Only
the context path in each invocation changes, from `./frontend` to
`./apps/bean-counter/frontend`.

## Compose files

| File | `build:` stanza | Change |
| --- | --- | --- |
| `apps/bean-counter/docker-compose.yml` | none | No change. It defines only the `postgres` and `mysql` dev services with named volumes. |
| `apps/bean-counter/docker-compose.stack.yml` | `api` and `ui` | `api.build.context: .` becomes `../..`, and `api.build.dockerfile: Dockerfile` becomes `apps/bean-counter/Dockerfile`. `ui.build.context: ./frontend` is unchanged. |
| `apps/bean-counter/deploy/docker-compose.prod.yml` | `api` and `ui` | Identical two-line change to the `api` service. `ui.build.context: ./frontend` is unchanged. |

`deploy/docker-compose.prod.yml` also carries a header comment block that documents its
own invocation as `docker compose -p bean-counter -f deploy/docker-compose.prod.yml ...`,
repeated on three lines. Update those to
`-f apps/bean-counter/deploy/docker-compose.prod.yml`. The header is operator-facing
documentation that the deploy script's failure messages point at; a stale path there
sends an operator to a file that does not exist at that path.

Nothing else in the prod compose file changes. The `symphony` external network, the
`BN_DSN_FILE` secret bind mount, the published port bindings, and the healthchecks are
all independent of repository layout.

Compose resolves relative `build.context` against the directory containing the compose
file, not the invocation directory. Since the compose files stay under
`apps/bean-counter/`, `../..` resolves to the repository root, and `./frontend` still
resolves to `apps/bean-counter/frontend`. Both are correct regardless of where
`docker compose` is run from, as long as `-f` names the file.

Verify by rendering rather than by reading:

```bash
cd "$HOME/git/beans"
docker compose -f apps/bean-counter/docker-compose.stack.yml config \
  | grep -A3 'context:'
```

The `api` context must render as the repository root and the `ui` context as
`apps/bean-counter/frontend`.

## `apps/bean-counter/scripts/deploy-production.sh`

Six changes. The first four are mechanical; the fifth is a real safety redesign.

### 1. Default remote repository directory

```bash
DEFAULT_REPO_DIR='$HOME/git/bean-counter'   →   DEFAULT_REPO_DIR='$HOME/git/beans'
```

The single quotes are intentional in the original: `$HOME` is expanded on the remote
host, never locally. Preserve that, including the `# shellcheck disable=SC2016` comment
above it.

### 2. Compose path

```bash
COMPOSE_PROD="deploy/docker-compose.prod.yml"
→
COMPOSE_PROD="apps/bean-counter/deploy/docker-compose.prod.yml"
```

This path is used on the remote host relative to `$REPO_DIR`, which is now the monorepo
root.

### 3. Beans module identifier

```bash
BEANS_MODULE="github.com/mattsp1290/beans"
→
BEANS_MODULE="github.com/mattsp1290/beans/libs/beans"
```

### 4. Frontend paths in the local gates

Two occurrences, both currently assuming the script runs from the bean-counter root:

```bash
( cd frontend && run npm ci && ... )
→
( cd apps/bean-counter/frontend && run npm ci && ... )

docker build --label "$REVISION_LABEL=$TARGET_SHA" -t "$UI_IMAGE" ./frontend
→
docker build --label "$REVISION_LABEL=$TARGET_SHA" -t "$UI_IMAGE" ./apps/bean-counter/frontend
```

There is a second `docker build ... ./frontend` for the UI image further down the file.
Both must change. Grep for `./frontend` and fix every hit.

Any `docker build` for the **API** image must additionally gain
`-f apps/bean-counter/Dockerfile` and take `.` as its context, matching the new
Dockerfile shape.

The script's local gates run `go test`, vet, lint, and fmt. Each of those must now run
with `apps/bean-counter` as the working directory, or be invoked as
`make -C apps/bean-counter <target>`. Prefer the `make -C` form; it survives future
Makefile changes.

### 5. Replace the `replace`-forbidding gate

`require_clean_local_ref` currently contains:

```bash
if grep -nE 'replace[[:space:]]+.*github\.com/mattsp1290/beans' go.mod >/dev/null 2>&1; then
  fatal "go.mod contains a local replace for $BEANS_MODULE; remove it before deploying"
fi
```

Its purpose was to refuse to deploy an image built against unpublished local library
code. In the monorepo the `replace` is mandatory, so this gate would abort every deploy.

Deleting it would silently drop a real safety property. Replace it with a gate that
asserts the same intent under the new premise — the library code being built is exactly
the library code at the target commit:

```bash
# In the monorepo, apps/bean-counter MUST replace the beans library with the
# in-repo copy. What used to be guarded (deploying unpublished library code) is
# now guaranteed by the single target SHA that pins both modules. What must be
# guarded instead is a replace pointing anywhere other than ../../libs/beans.
expected_replace='replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans'
if ! grep -qF "$expected_replace" apps/bean-counter/go.mod; then
  fatal "apps/bean-counter/go.mod must contain: $expected_replace"
fi
if grep -nE '^[[:space:]]*replace[[:space:]]' apps/bean-counter/go.mod \
   | grep -v -F "$expected_replace" >/dev/null 2>&1; then
  fatal "apps/bean-counter/go.mod contains an unexpected replace directive"
fi
```

The second check is what preserves the original property: any replace other than the
one sanctioned by the monorepo layout — a developer pointing at a local experiment, for
example — still aborts the deploy.

The adjacent `go list -m -json "$BEANS_MODULE"` check keeps working and should be kept.
Run it from `apps/bean-counter`.

### 6. `resolve_embedded_migration_max`

```bash
beans_dir="$(go list -m -f '{{.Dir}}' "$BEANS_MODULE" 2>/dev/null)"
EMBEDDED_MAX="$(migration_max_from_dir "$beans_dir/schema/migrations/postgres")"
```

With a filesystem `replace`, `go list -m -f '{{.Dir}}'` returns the absolute path of
`libs/beans` in the worktree instead of a module-cache path. The subsequent
`$beans_dir/schema/migrations/postgres` still resolves, because
[01-target-layout-and-module-graph.md](01-target-layout-and-module-graph.md) keeps
`schema/migrations/` intact inside `libs/beans`.

This is a semantic improvement: the parity gate now reads the migrations that are
actually compiled into the image, rather than a cached copy of a pinned version.

Two adjustments are required:

- `go list -m` must run with `apps/bean-counter` as the working directory. Wrap the call
  in `( cd apps/bean-counter && go list -m ... )`.
- The existing `[ "$EMBEDDED_MAX" -gt 0 ] || fatal "... module layout unexpected"` check
  is the safety net for a wrong `beans_dir`. Keep it. It is the reason a mistaken path
  fails loudly instead of reporting a parity max of zero.

### Widened clean-worktree gate

`require_clean_local_ref` runs `git status --porcelain` and aborts on any output. In the
monorepo that covers the library and every other application, not just bean-counter.
This is stricter than before. Keep it strict: the deploy records a single monorepo SHA,
so uncommitted changes anywhere make that SHA an incomplete description of what was
tested.

Note this in the script's header comment block, which already documents the safety
properties.

## `apps/bean-counter/test/scripts/deploy-production_test.sh`

This file `source`s the deploy script to unit-test its pure helpers
(`normalize_ui_port`, `assert_dsn_container_host`, `migration_max_from_dir`,
`extract_issue_count`). Update the `source` path for the new location.

Add one test case for the new replace gate: a `go.mod` fixture containing an unexpected
`replace` line must be rejected, and one containing exactly the sanctioned line must be
accepted. Without that case, change 5 above is untested and the safety property is
asserted only in prose.

Keep the helpers above the main guard so `source` continues to work without launching a
deploy. That structure is documented in the script's header and must not be broken.

## Verification

```bash
cd "$HOME/git/beans"

# API image builds from the repository root, without go.work.
docker build -f apps/bean-counter/Dockerfile -t bean-counter-api:monorepo-test .

# The binary runs.
docker run --rm bean-counter-api:monorepo-test --help 2>&1 | head -5

# UI image builds.
docker build -t bean-counter-ui:monorepo-test ./apps/bean-counter/frontend

# Compose files render with correct contexts.
docker compose -f apps/bean-counter/docker-compose.stack.yml config | grep -A3 'context:'
docker compose -f apps/bean-counter/deploy/docker-compose.prod.yml config >/dev/null

# Shell gates.
shellcheck apps/bean-counter/scripts/deploy-production.sh \
           apps/bean-counter/test/scripts/deploy-production_test.sh
bash apps/bean-counter/test/scripts/deploy-production_test.sh

# No stale ./frontend or bare deploy/ path remains.
grep -n '\./frontend\|"deploy/docker-compose' apps/bean-counter/scripts/deploy-production.sh
# must print nothing

# Dry run reflects the new layout.
./apps/bean-counter/scripts/deploy-production.sh --dry-run 2>&1 | head -40
```

The `--help` invocation assumes the `bean-counter` binary accepts `--help` and exits
zero. Confirm against `apps/bean-counter/cmd/bean-counter/main.go` before relying on it;
if it does not, substitute a check that the entrypoint starts and fails on a missing DSN
rather than on a missing binary.

The dry run must show `$HOME/git/beans` as the repo directory, the
`apps/bean-counter/deploy/docker-compose.prod.yml` compose path, and image build
contexts consistent with this document.

## Acceptance criteria

1. `docker build -f apps/bean-counter/Dockerfile .` from the repository root succeeds
   with no `go.work` present in the image.
2. `docker build ./apps/bean-counter/frontend` succeeds.
3. Both compose files render with the contexts described above.
4. `shellcheck` is clean on both shell files.
5. `bash apps/bean-counter/test/scripts/deploy-production_test.sh` passes, including the
   new replace-gate cases.
6. `--dry-run` prints the monorepo paths.
7. The remote-state check at the top of this file was run, and its result — empty or
   non-empty — is recorded in the work-package notes.

## Risks

- **The replace gate is weakened rather than replaced.** An implementer under time
  pressure may simply delete the failing `grep` and move on. The new test cases in the
  shell unit test are the defense; treat their absence as a failed work package.
- **A second `docker build ./frontend` is missed.** There are at least two occurrences.
  The verification grep catches this.
- **The remote host already has a `bean-counter` checkout.** Covered by the
  prerequisite check; it converts this work package's assumption into a separate,
  explicitly scoped migration.

## Exclusions

- No change to bean-counter's runtime configuration surface. Every `BN_*` variable keeps
  its name, default, and meaning.
- No first production deploy. `bean-counter-m0p` remains open and is out of scope.
- No change to `deploy/k8s/bean-counter-ingress.yaml`, which contains no repository
  paths.
