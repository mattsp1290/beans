## Action Items

### Critical

_None._

### Important

- [ ] [apps/bean-counter/deploy/README.md:5] Broken link `../.agents/plans/deploy/` — the plans were hoisted and renamed by the migration; point it at `../../../.agents/plans/bean-counter-deploy/`. Only broken relative markdown link on the branch.
- [ ] [README.md:9] `.dockerignore  shared by every image build in the repository` is false — the UI image's context is `apps/bean-counter/frontend` and uses that directory's own `.dockerignore`. Reword to "every image built from the repository root".
- [ ] [AGENTS.md:14] Same false claim as above; reword identically.
- [ ] [.agents/plans/monorepo-consolidation/01-target-layout-and-module-graph.md:acceptance criterion 2] Amend the root depth-1 file list: add `.dockerignore` (required since `a73abfe` moved the API build context to the root) and drop `go.work.sum` (`go work sync` produces none here). Also update the mapping table row that still lists `.dockerignore` under "Kept at `apps/bean-counter/` unchanged".
- [ ] [.agents/plans/monorepo-consolidation/01-target-layout-and-module-graph.md:acceptance criterion 4] Widen the exception clause to cover `deploy/docker-compose.prod.yml`'s `context: ../../..` (correct — resolves to the repository root) and the `$HOME` remote-host path in the `deploy/README.md` rollback snippet.
- [ ] [.agents/plans/monorepo-consolidation/00-overview.md:success criterion 9] Path is pre-migration: `scripts/deploy-production.sh` does not exist at the root. Change to `apps/bean-counter/scripts/deploy-production.sh`, which is what the same plan's layout mapping mandates and what the criterion verifies successfully.

### Suggestions

- [ ] [apps/bean-counter/internal/config/config.go:107] Comment names `.agents/plans/deploy/`; update to `.agents/plans/bean-counter-deploy/`. Last surviving stale plan reference outside `.agents/`.
- [ ] [apps/bean-counter/Makefile:33] `gofmt -l ./cmd ./internal ./test` silently skips any future top-level Go directory, and goes green if one of the three paths disappears (gofmt's error goes to stderr; `test -z "$( )"` sees empty). Prefer `gofmt -l $$(git ls-files '*.go')` — same node_modules fix, no coverage cliff.
- [ ] [Makefile:24] `fmt-check` is the only root target that does not fan out, and is excluded from both root `ci` and `ci-workspace`'s fan-out gate list — the third application will be silently skipped. Fan out with an opt-out, or note the decision at the `MODULES` line.
- [ ] [.github/workflows/ci-libs-beans.yml:66] Integration step has no `docker version` preflight, unlike the `ci-apps-bean-counter` integration job; a daemon-less runner fails opaquely.
- [ ] [.github/workflows/ci-workspace.yml:4] Header says "Unfiltered on purpose" but the trigger now carries `push.branches: [main]`; say "path-unfiltered" (the property `CLAUDE.md` relies on is still intact).
- [ ] [.github/workflows/ci-apps-bean-counter.yml:8] All three workflows filter on `go.work.sum`, which is untracked, has never been tracked, and is not produced by `go work sync`. Drop it or annotate it as future-proofing.
- [ ] [bd:bean-counter-m0p] Imported issue descriptions still name `scripts/deploy-production.sh` and `.agents/plans/deploy/05-validation.md`. Not a violation — plan 06 forbade a triage pass during the import — but file a follow-up to re-path them before the first production deploy.
- [ ] [.git/info/exclude:7] `.agents/reviews/` is excluded machine-locally, so new review records never appear in `git status` and this pass's five files need `git add -f` to be committed. Decide whether review output is tracked (plan 01 implies yes) or local-only, and record the decision.
