# Suggestions (non-blocking)

## S1 — Root `Makefile:5`: `tidy-check` is missing from `.PHONY`

Every other target in the file is declared. `tidy-check` (`Makefile:24`) is not, so a stray
file or directory named `tidy-check` at the repo root would silently make the target a no-op.

```make
.PHONY: build test vet lint fmt-check tidy-check ci ci-integration clean beans bean-counter
```

## S2 — Root `Makefile:32-34`: `ci-integration` fans out two different ways

```make
ci-integration:
	cd libs/beans && go test -tags=integration ./...
	$(MAKE) -C apps/bean-counter test-integration
```

The file's own header says "each module owns its rules", and every other target honours that
with `$(MAKE) -C`. This one reaches into `libs/beans` and runs `go test` directly, because
`libs/beans/Makefile` has no `test-integration` target. Add one and make the fan-out uniform
— it also gives `libs/beans` the same escape hatch the app has
(`make beans TARGET=test-integration`):

```make
# libs/beans/Makefile
.PHONY: build test test-integration vet lint tidy-check ci install clean

test-integration:
	go test -tags=integration ./...
```
```make
# Makefile
ci-integration:
	@for m in $(MODULES); do $(MAKE) -C $$m test-integration || exit 1; done
```

## S3 — `apps/bean-counter/deploy/README.md:4-5`: broken relative link to the design plans

```markdown
Full design: [`../.agents/plans/deploy/`](../.agents/plans/deploy/).
```

From `apps/bean-counter/deploy/`, `../.agents/...` resolves to
`apps/bean-counter/.agents/plans/deploy/`, which does not exist (there is no
`apps/bean-counter/.agents` at all). The plans were hoisted and renamed; the deploy script's
own header was updated to `.agents/plans/bean-counter-deploy/` in the same commit, but this
link was not.

```markdown
Full design: [`.agents/plans/bean-counter-deploy/`](../../../.agents/plans/bean-counter-deploy/).
```

## S4 — Stale `deploy/k8s/...` path in two places

`apps/bean-counter/deploy/README.md:37` and
`apps/bean-counter/deploy/docker-compose.prod.yml:59` both say
`deploy/k8s/bean-counter-ingress.yaml`. That is wrong from the repository root (it is now
`apps/bean-counter/deploy/k8s/...`) and wrong relative to each file's own directory (it is
just `k8s/...`). The file exists; only the reference is stale. Use the repo-root form in both,
matching how the rest of the migrated docs anchor paths.

## S5 — `.dockerignore:11-14`: the comment's reasoning about the frontend is inverted

```
# Not a blanket `frontend` exclusion: the UI image's context is still the
# frontend directory, and a root-level exclusion would be wrong the moment a
# second application ships a UI.
```

Docker reads `.dockerignore` from the **build-context root**, so this file has no effect on
the UI build at all — that build's context is `apps/bean-counter/frontend`, which has its own
`.dockerignore` (I confirmed it exists and covers `node_modules`, `dist`, `.env*`). A
`**/frontend` entry here would therefore be entirely safe, and would shrink the API build
context by the whole Svelte source tree. Either add it and correct the comment, or keep the
current entries and reword the comment so it does not teach a future reader the wrong rule.

## S6 — `.github/workflows/ci-libs-beans.yml:20-22`: `defaults` is at workflow level, not job level

```yaml
defaults:
  run:
    working-directory: libs/beans

jobs:
  gates:
```

Harmless today (one job), but it is a latent trap: any second job added to this workflow —
a shellcheck job, a docs job, an image job — silently inherits `working-directory: libs/beans`
and every repo-root-relative command in it breaks in a confusing way.
`ci-apps-bean-counter.yml` already puts `defaults` inside each job, including deliberately
omitting it from `deploy-scripts`. Move it into `gates` for symmetry.

## S7 — `.github/workflows/ci-workspace.yml:38-41`: the sync gate cannot see new untracked files

```yaml
        run: |
          go work sync
          git diff --exit-code
```

`git diff --exit-code` reports tracked modifications only. `.gitignore` no longer ignores
`go.work.sum` (correctly — `go.work` had to become trackable), so if `go work sync` ever
starts emitting one it lands as an untracked file and this gate stays green. I ran
`go work sync` on the current tree: it is idempotent and produces no `go.work.sum` today, so
this is prophylactic.

```yaml
        run: |
          go work sync
          git status --porcelain
          test -z "$(git status --porcelain)"
```

## S8 — Trigger filters are inconsistent across the three workflows

- `ci-workspace`: `on: push` / `on: pull_request`, no branch filter — runs twice for every PR branch push.
- `ci-libs-beans`: `on: push` with `paths` but no `branches`.
- `ci-apps-bean-counter`: `push` restricted to `branches: [main]`.

Pick one convention. `push: branches: [main]` plus unrestricted `pull_request` (what
`ci-apps-bean-counter` does) avoids the duplicate-run cost on PR branches.

## S9 — Path filters plus required status checks can deadlock a root-only PR

If `Backend`, `Gates` or any path-filtered job is configured as a required check in branch
protection, a PR that touches only root files (`Makefile`, `.dockerignore`, `README.md`,
`AGENTS.md`) will never report those checks and can never merge. I cannot see the branch
protection settings from the repo, so this is a "confirm before enabling" note: either keep
only `Workspace` (from the always-running `ci-workspace`) as required, or add a skip-shim job
per filtered workflow.

## S10 — `libs/beans/.golangci.yml:23`: empty `settings:` key fails `golangci-lint config verify`

```yaml
linters:
  default: none
  enable:
    ...
  settings:          # <- parses as null
```

Verified against v2.12.2:

```
jsonschema: "linters.settings" does not validate with ".../settings/type": got null, want object
```

`golangci-lint run` tolerates it (I confirmed `0 issues.`), so this is hygiene rather than a
break — but it means `golangci-lint config verify` can never be added as a gate. The key is
inherited unchanged from `main`'s root config. Just delete the line.

## S11 — `deploy-production.sh` has no working-directory guard

Every relative path in the script is now repository-root-relative, and the header says so —
but nothing enforces it. The old invocation was `scripts/deploy-production.sh` from the app
repo root; the natural muscle-memory equivalent, `./scripts/deploy-production.sh` from
`apps/bean-counter/`, now fails at `resolve_embedded_migration_max` with
`go list -m ... failed; module graph is not deployable`, which points an operator at the
module graph rather than at their `cd`. It fails safe, but unhelpfully. One line near the top
of `main()`:

```bash
main() {
  parse_args "$@"
  # Every relative path below is repository-root-relative.
  cd "$(git rev-parse --show-toplevel)" \
    || fatal "not inside a git worktree; run this from the monorepo root"
  [ -f apps/bean-counter/go.mod ] || fatal "repository root does not contain apps/bean-counter/go.mod"
  resolve_target_sha
```

## S12 — `deploy-production_test.sh:53` vs `:75`: the first temp dir is unprotected for 22 lines

```bash
tmp="$(mktemp -d)"          # :53
...
gomod_tmp="$(mktemp -d)"    # :74
trap 'rm -rf "$tmp" "$gomod_tmp"' EXIT   # :75
```

The trap covers both directories once installed — that part is right. But `$tmp` is created
22 lines earlier, and anything that terminates the harness in between (a `mktemp` failure, an
interrupt) leaks it. Install the trap immediately after the first `mktemp -d`; `$gomod_tmp`
is empty at that point and `rm -rf ""` is harmless because the variable is quoted and unset
expands to nothing under the harness's `set +u`. Cleanest is to declare both up front:

```bash
tmp="$(mktemp -d)"
gomod_tmp="$(mktemp -d)"
trap 'rm -rf "$tmp" "$gomod_tmp"' EXIT
```

## S13 — Both `tidy-check` targets diff against the index, not `HEAD`

`libs/beans/Makefile:29` and `apps/bean-counter/Makefile:40`:

```make
	git diff --exit-code go.mod go.sum
```

`git diff` with no commit argument compares the worktree to the **index**, so a `go.mod`
that was `git add`ed before `go mod tidy` ran passes the check. The cwd handling is correct
(pathspecs resolve relative to the module directory under `make -C`, which I confirmed), it
is only the ref that is loose. Pre-existing, inherited from the old root Makefile:

```make
	git diff --exit-code HEAD -- go.mod go.sum
```

## S14 — Root `.gitignore` lost `.agents/reviews/` while the clean gate got stricter

The dropped `apps/bean-counter/.gitignore` carried an `# ai litter` / `.agents/reviews/`
entry; the root `.gitignore` has no equivalent. Meanwhile `require_clean_local_ref`
(`deploy-production.sh:379-385`) fails on **any** untracked file anywhere in the monorepo —
deliberately, and I agree with the reasoning. The combination means agent/review output left
in the tree now aborts a deploy with "local worktree is not clean".

Worth knowing while deciding: on this machine the gate is currently only quiet by accident.
`git check-ignore -v` shows this review directory is matched by
`/Users/punk1290/.gitignore_global:1:reviews/` — a **global** ignore outside the repository.
Another operator, or CI, or a fresh clone would see those files as untracked and the deploy
would abort. Either re-add repo-level ignores for the agent/review output directories, or
note in `deploy/README.md` that the tree must be swept first.

## S15 — `apps/bean-counter/README.md` does not say which directory its commands run from

`make run`, `npm --prefix frontend install`, `docker compose up -d postgres` and
`docker compose -f docker-compose.stack.yml up --build` (line 68) all require
cwd = `apps/bean-counter/`. That is self-consistent and the compose contexts resolve
correctly from there (I rendered them), but `AGENTS.md`, `CLAUDE.md` and the root `README.md`
all say "run from the repository root", so a reader arriving from those will get
`no configuration file provided`. One line under `## Run Locally`:

```markdown
Run every command in this document from `apps/bean-counter/`. Root-level `make` targets
(`make ci`, `make bean-counter TARGET=test`) run from the repository root instead.
```
