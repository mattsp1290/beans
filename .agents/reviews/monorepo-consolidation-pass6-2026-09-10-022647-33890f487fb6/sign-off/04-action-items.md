## Action Items

### Critical

_None._

### Important

_None that block the merge._

### Suggestions

- [ ] [merge commit body] State the corrected figure: the four redundant containment entries survive removal at **62/62**, not the "60/60" in `33890f4`'s message. The finding is right; only the count is stale, and it cannot be fixed in place without rewriting a published commit. The proposed body in `03-positive-notes.md` already carries the correction.
- [ ] [.github/workflows/ci-workspace.yml:39-42] Reword the comment above the "Workspace is in sync" step. It says the `git status --porcelain` check catches a `go.work.sum` that `go work sync` might create, but round 5's anchored `/go.work.sum` in `.gitignore` makes that file invisible to `git status` by design. The step still usefully catches other untracked artefacts; only the `go.work.sum` rationale is now false. Cross-reference `IMPLEMENTATION-NOTES.md`'s "`go.work.sum` is ignored, not tracked".
- [ ] [.github/workflows/ci-libs-beans.yml:10,17] Remove the `'go.work.sum'` path-filter entries, or comment them as vestigial — the file is gitignored and can never appear in a diff.
- [ ] [.github/workflows/ci-apps-bean-counter.yml:12,23] Same as above: two dead `'go.work.sum'` path-filter entries.
- [ ] [.agents/plans/monorepo-consolidation/IMPLEMENTATION-NOTES.md:62-79] Optionally extend the `go.work.sum` section to note the two follow-on effects of that fix — the stale `ci-workspace.yml` comment and the four dead workflow path filters — so the next reader sees the full blast radius of the ignore rule.
- [ ] [.agents/reviews/monorepo-consolidation-2026-09-10-005745-40d8e6475e51/build-and-deploy-integrity/02-suggestions.md] Three broken relative links across this file and `.agents/reviews/monorepo-consolidation-pass3-2026-09-10-013439-5bd9e07513ec/regression-auditor/01-critical-and-important.md` (`../.agents/plans/deploy/`, `../../../.agents/plans/bean-counter-deploy/`). Lowest priority — these are point-in-time review records, and all 107 relative links in the 89 delivered markdown files resolve. Worth noting only because a future repo-wide link checker must exclude `.agents/reviews/`.

### Not action items — recorded so they are not re-litigated

- `go build ./...` from the repository root fails with `directory prefix . does not contain modules listed in go.work`. This is standard Go workspace behaviour, reproduced in a clean scratch workspace unrelated to this repo; the workspace root is not a module. No documentation instructs anyone to run it, and `ci-workspace.yml` uses the correct `go build ./libs/beans/... ./apps/bean-counter/...`. **No change needed.**
- `setup-beads.sh` and `setup-multi-repo-beads.sh` emit info-level SC2016/SC2086 under shellcheck. Counts are identical to `main` (4 and 100), the branch appends only 6 lines to each, and CI's shellcheck job intentionally scopes to the two deploy scripts. **Not a regression, out of scope.**

### Follow-up work already tracked in Beads — do not re-file

- `beans-oba` (P1) — verify both bean-counter images build from the monorepo root; blocked here only by the local Docker engine being down.
- `beans-ued` (P0) — resolve embedded schema 11 vs production 8 before the next deploy.
- `beans-nlc` (P0) — migrate the live infra-host deploy from `git/bean-counter` to `git/beans`.
- `beans-ad3` (P1) — decide whether to archive `github.com/mattsp1290/bean-counter`; keep it intact until `beans-nlc` lands, as it is the recovery source.
