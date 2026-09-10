# 08 — Execution handoff

Dependency-ordered work packages, verification per package, and the definition of done.

All work happens on one branch, `monorepo-consolidation`, cut from `main` in WP2. Each
work package is at least one commit. Do not squash WP2's commits together: the history
import, the library move, and the file hoist must stay separable so a reviewer can read
the rename commit's `--stat` and confirm it contains only renames.

## Order

```text
WP1  Pre-migration gate (G1)
 └── WP2  Import bean-counter history + move beans under libs/
      └── WP3  Rename modules, rewrite imports, add go.work + replace
           ├── WP4  Stale-reference sweep
           ├── WP5  Containers and compose
           │    └── WP7  Deploy script and its shell tests
           ├── WP6  Makefiles and CI workflows
           └── WP8  Beads tracker consolidation (G2)
                └── WP9  Root files: gitignore, AGENTS, CLAUDE, README, settings
                     └── WP10 Full verification and merge
```

WP4, WP5, WP6, and WP8 are independent of one another and may run in any order after
WP3. WP7 depends on WP5 because the deploy script's `docker build` arguments must match
the Dockerfile's new context. WP9 depends on WP8 because the root `.gitignore` merge and
the `AGENTS.md` merge both consume files that WP8 is still using.

Do not parallelize WP8 with anything else. It runs `bd` commands, and this repository's
Beads conventions require `bd` to run serially with no concurrent writer.

---

## WP1 — Pre-migration gate

**Goal.** Establish a recoverable starting point.

**Steps.** Execute gate G1 in [02-history-migration.md](02-history-migration.md) in full,
then run the remote-state check at the top of
[05-containers-and-deploy.md](05-containers-and-deploy.md), then install
`git-filter-repo`.

**Verification.**

- `git status --porcelain` is empty in both repositories.
- `git status -sb` shows both up to date with origin.
- `bd dolt push` succeeded in both, run serially.
- The backup directory exists and contains both `.git` and `.beads` trees.
- `command -v git-filter-repo` prints a path, or the subtree fallback is chosen and
  recorded.
- The infra-host check result is recorded as empty or non-empty.

**Acceptance.** The backup path and the infra-host result are written into the work
package notes. A later package can be reverted without them only by guesswork.

**Exclusions.** No file is modified in this package.

---

## WP2 — History import and library move

**Goal.** Both projects' files sit in their final directories with history intact.

**Changes.** No file contents. Directory placement only, per
[01-target-layout-and-module-graph.md](01-target-layout-and-module-graph.md).

**Steps.** [02-history-migration.md](02-history-migration.md) steps 2 through 5.

**Verification.**

```bash
cd "$HOME/git/beans"
git log --oneline -- apps/bean-counter/internal/server/app.go | tail -3
git log --follow --oneline -- libs/beans/store/store.go | tail -3
git show --stat <move-commit> | head -20      # renames only
git remote -v                                  # no bean-counter-rewrite
ls "$HOME/git/bean-counter/.git"               # source repo untouched
```

**Acceptance.** All six acceptance criteria in
[02-history-migration.md](02-history-migration.md).

**Risks.** Running `git filter-repo` in `$HOME/git/bean-counter` instead of a scratch
clone destroys that repository's remote link. The procedure clones first for this reason.

---

## WP3 — Module restructure

**Goal.** Both modules build and test under their new paths, with and without the
workspace.

**Changes.** `libs/beans/go.mod`, `apps/bean-counter/go.mod`, every `.go` file in both
modules, `libs/beans/Makefile`, `libs/beans/.golangci.yml`, `libs/beans/README.md`, root
`.gitignore`, new `go.work` and `go.work.sum`.

**Steps.** [03-go-module-restructure.md](03-go-module-restructure.md) steps 1 through 6.

**Verification.** Step 7 of that file, in full.

**Acceptance.** Its seven acceptance criteria.

**Risks.** The `LDFLAGS` version path is the silent failure. Its check —
`make build && ./bin/bn version` printing a non-empty string — is not optional.

---

## WP4 — Stale-reference sweep

**Goal.** No live file references the old module paths, the old repository name, or the
old directory layout.

**Steps.**

```bash
cd "$HOME/git/beans"

grep -rn 'github\.com/mattsp1290/beans[^/]' . \
  | grep -v '^\./\.git/' | grep -v '^\./\.agents/'

grep -rn 'github\.com/mattsp1290/bean-counter' . \
  | grep -v '^\./\.git/' | grep -v '^\./\.agents/'

grep -rn 'mattsp1290/bean-counter' --include='*.yml' --include='*.yaml' \
  --include='*.sh' --include='*.md' . | grep -v '^\./\.agents/'
```

Each must print nothing. Fix any hit at its source; do not add exclusions to the grep.

`.agents/` is excluded because it holds dated plans and review records — including this
plan's own quoted "before" values, which must stay as written. `.git/` is excluded
because it holds the remote URL.

**Acceptance.** All three greps are silent, and every fix made here is in a commit whose
message names the file and why the reference was stale.

**Exclusions.** Do not rewrite historical documents under `.agents/`.

---

## WP5 — Containers and compose

**Goal.** Both images build from the monorepo, without `go.work`.

**Changes.** `apps/bean-counter/Dockerfile`, new root `.dockerignore`, deletion of
`apps/bean-counter/.dockerignore`, `apps/bean-counter/docker-compose.stack.yml`,
`apps/bean-counter/deploy/docker-compose.prod.yml` including its header comment block.

**Steps.** [05-containers-and-deploy.md](05-containers-and-deploy.md), sections "API
image" through "Compose files".

**Verification.**

```bash
cd "$HOME/git/beans"
docker build -f apps/bean-counter/Dockerfile -t bean-counter-api:monorepo-test .
docker build -t bean-counter-ui:monorepo-test ./apps/bean-counter/frontend
docker compose -f apps/bean-counter/docker-compose.stack.yml config | grep -A3 'context:'
docker compose -f apps/bean-counter/deploy/docker-compose.prod.yml config >/dev/null
```

**Acceptance.** Criteria 1 through 3 in
[05-containers-and-deploy.md](05-containers-and-deploy.md). The API image must contain no
`go.work`; the build sets `GOWORK=off` and never copies it.

**Prerequisites.** Docker daemon running.

---

## WP6 — Makefiles and CI

**Goal.** `make ci` at the root runs every gate, and GitHub Actions runs the right jobs.

**Changes.** New root `Makefile`, `GOWORK=off` added to both `tidy-check` targets, a new
`tidy-check` in `apps/bean-counter/Makefile`, the `VERSION` match pattern in
`libs/beans/Makefile`, deletion of `.github/workflows/ci.yml`, three new workflow files.

**Steps.** [04-build-lint-and-ci.md](04-build-lint-and-ci.md).

**Verification.**

```bash
cd "$HOME/git/beans"
make ci
make ci-integration          # requires Docker
make beans TARGET=build
make bean-counter TARGET=test
gh api repos/mattsp1290/beans/branches/main/protection 2>&1 | head -20
```

**Acceptance.** Its eight acceptance criteria. Criterion 8 — the branch-protection check
was run and its outcome recorded — is a prerequisite for enabling path filters, not a
formality.

**Risks.** Path-filtered workflows plus required status checks produce pull requests that
can never merge. The `gh api` check is what prevents this.

---

## WP7 — Deploy script

**Goal.** The production deploy script targets the monorepo and keeps its safety
properties.

**Changes.** `apps/bean-counter/scripts/deploy-production.sh` and
`apps/bean-counter/test/scripts/deploy-production_test.sh`.

**Steps.** [05-containers-and-deploy.md](05-containers-and-deploy.md), the
`deploy-production.sh` section, changes 1 through 6, plus the widened clean-worktree note
and the shell-test additions.

**Verification.**

```bash
cd "$HOME/git/beans"
shellcheck apps/bean-counter/scripts/deploy-production.sh \
           apps/bean-counter/test/scripts/deploy-production_test.sh
bash apps/bean-counter/test/scripts/deploy-production_test.sh
grep -n '\./frontend\|"deploy/docker-compose' apps/bean-counter/scripts/deploy-production.sh
./apps/bean-counter/scripts/deploy-production.sh --dry-run 2>&1 | head -40
```

**Acceptance.** Criteria 4 through 7 in
[05-containers-and-deploy.md](05-containers-and-deploy.md). The shell test must include
the two new replace-gate cases; without them, change 5 ships unverified.

**Risks.** Change 5 replaces a safety gate. An implementer who deletes the failing check
instead of replacing it removes a real protection and the deploy still appears to work.
The new test cases are the only thing that catches this.

**Exclusions.** No deploy is performed. `bean-counter-m0p` stays open.

---

## WP8 — Beads tracker consolidation

**Goal.** One tracker at the root holding both projects' issues.

**Changes.** The root Dolt database gains 54 issues; a JSONL archive is committed;
`apps/bean-counter/.beads/` is deleted.

**Steps.** [06-beads-and-agent-config.md](06-beads-and-agent-config.md) steps 1 through 4.

**Verification.** Gate G2 in that file, all six checks.

**Acceptance.** Criteria 1 through 5 in
[06-beads-and-agent-config.md](06-beads-and-agent-config.md).

**Prerequisites.** No other agent or session may hold the Beads lock. Run every `bd`
command serially. On an embedded-Dolt exclusive-lock error, wait and retry a bounded
number of times before treating it as a real conflict.

**Risks.** A partial import that passes a count check but loses dependency edges. G2
check 5 compares the dependency trees for this reason.

---

## WP9 — Root files

**Goal.** One `.gitignore`, one `AGENTS.md`, one `CLAUDE.md`, one
`.claude/settings.json`, and a monorepo `README.md`.

**Changes.** Root `.gitignore`, `AGENTS.md`, `CLAUDE.md`, `.claude/settings.json`, a new
root `README.md`; deletion of the `apps/bean-counter/` copies.

**Steps.** [06-beads-and-agent-config.md](06-beads-and-agent-config.md), sections "Root
`.gitignore`" through "`setup-beads.sh`". The new root `README.md` reproduces the target
tree from [00-overview.md](00-overview.md), names each component in one line, and points
at `libs/beans/README.md` and `apps/bean-counter/README.md` for detail.

Also file the deferred-work Beads issues listed at the end of this document.

**Verification.**

```bash
cd "$HOME/git/beans"
git status --porcelain                       # empty
git check-ignore -v go.work                  # exits non-zero
git check-ignore -v bn                       # exits zero
grep -c 'BEGIN BEADS INTEGRATION' AGENTS.md  # exactly 1
find . -name AGENTS.md -not -path './.git/*' # exactly one result, at the root
find . -name CLAUDE.md -not -path './.git/*' # exactly one result, at the root
```

**Acceptance.** Criteria 6 through 9 in
[06-beads-and-agent-config.md](06-beads-and-agent-config.md).

---

## WP10 — Full verification and merge

**Goal.** Prove the migration against the success criteria in
[00-overview.md](00-overview.md) and land it.

**Steps.**

```bash
cd "$HOME/git/beans"

# 1-3: builds, tests, vet, lint, both modes.
go build ./libs/beans/... ./apps/bean-counter/...
( cd libs/beans        && GOWORK=off go build ./... && GOWORK=off go test ./... )
( cd apps/bean-counter && GOWORK=off go build ./... && GOWORK=off go test ./... )
make ci
make ci-integration

# 4-5: history.
git log --follow --oneline -- libs/beans/store/store.go | tail -3
git log --oneline -- apps/bean-counter/internal/server/app.go | tail -3

# 6: image.
docker build -f apps/bean-counter/Dockerfile -t bean-counter-api:verify .

# 7: tracker.
bd stats
bd show bean-counter-m0p

# 8-9: deploy surface.
shellcheck apps/bean-counter/scripts/deploy-production.sh \
           apps/bean-counter/test/scripts/deploy-production_test.sh
bash apps/bean-counter/test/scripts/deploy-production_test.sh
./apps/bean-counter/scripts/deploy-production.sh --dry-run 2>&1 | head -40

# 10: clean tree.
git status --porcelain
```

Then push the branch, let all three workflows run green, and merge.

```bash
git push -u origin monorepo-consolidation
gh pr create --fill
# after CI is green and the PR is reviewed:
gh pr merge --merge          # not --squash: WP2's separable commits are the point
bd dolt push
git checkout main && git pull --rebase && git status -sb
```

Use a merge commit, not a squash. Squashing collapses the imported bean-counter history
into a single commit and undoes WP2.

**Acceptance.** All ten success criteria in [00-overview.md](00-overview.md) pass, and
`git status -sb` on `main` reports up to date with origin.

---

## Definition of done

1. Every success criterion in [00-overview.md](00-overview.md) is met and its command
   output was observed, not assumed.
2. `monorepo-consolidation` is merged to `main` with a merge commit, and `main` is pushed.
3. `bd dolt push` succeeded after the merge.
4. `$HOME/git/bean-counter` is unchanged from its WP1 state and still has its `origin`
   remote.
5. The G1 backup directory still exists and its path is recorded.
6. The deferred-work issues below exist in the root tracker.
7. Gate G3 has not been executed. The `mattsp1290/bean-counter` GitHub repository is
   untouched.

## Deferred work

File each of these as a Beads issue in the root tracker during WP9.

| Item | Type | Priority | Note |
| --- | --- | --- | --- |
| Decide whether to archive `mattsp1290/bean-counter` | task | 1 | Gate G3. Outward-facing; needs explicit user approval. Do not act without it. |
| Update the remote infra-host checkout from `$HOME/git/bean-counter` to `$HOME/git/beans` | task | 1 | Only applies if WP1's remote-state check returned non-empty. Blocks `bean-counter-m0p`. |
| Unify the two `.golangci.yml` policies and the golangci-lint invocation style | chore | 3 | Decision D7 deferred this deliberately. |
| Create the first `libs/beans/v*` tag and document the release procedure | task | 3 | The `v0.1.0` and `v0.1.1` tags no longer describe a resolvable module. |
| Delete the `$HOME/git/bean-counter` local clone | chore | 3 | Only after gate G3 and after the backup is confirmed. |
| Consider a shared composite GitHub Action for the repeated Go setup steps | chore | 4 | Worth doing once the third application lands. |

`bean-counter-m0p`, `bean-counter-log`, and `bean-counter-am5` carry over from the
bean-counter tracker unchanged. They describe the first production deploy and are
consumers of this migration, not part of it.
