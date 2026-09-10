## Action Items

### Critical

_None._

### Important

- [ ] [.github/workflows/ci-workspace.yml:36-38] The comment claims the porcelain check catches a `go.work.sum` that `go work sync` creates; `/go.work.sum` is now gitignored, so it cannot. Verified: the exact CI step passes rc=0 with a 3215-byte `go.work.sum` on disk. Rewrite the comment to say the file is ignored and to name the condition that would make it matter again. Documentation only — no behavior change, does not block merge.

### Suggestions

- [ ] [apps/bean-counter/scripts/deploy-production.sh:546-547] `awk`'s default field splitting truncates a symlink path at its first space, so the abort message names a path that does not exist (`apps/bean-counter/evil` for `apps/bean-counter/evil name.mk`). The gate still fires correctly. Fix with `awk -F'\t' 'substr($1, 1, 6) == "120000" { print $2 }'`, verified to print the full path in every fixture and nothing on the real repository.
- [ ] [apps/bean-counter/scripts/deploy-production.sh:535-544] Add one sentence to the comment block noting that the structural check is anchored at `libs/beans` and `apps/bean-counter` and therefore cannot see a tracked symlink at `libs` or `apps` themselves — the per-path loop is what covers those, so neither gate subsumes the other. Guards against a future cleanup deleting the entries the commit message calls redundant.
- [ ] [apps/bean-counter/test/scripts/deploy-production_test.sh:322-330] Mutating `print $4` to `print $3` leaves the suite at 62/62 — the new case asserts the return code only. If the S1 fix is applied, add an assertion on stderr that the reported path round-trips (a fixture symlink named `evil name.mk`); that case fails today and passes after the fix.
- [ ] [.gitignore:29-33] Add the revisit condition to the comment: if a future module bump ever makes `go work sync` produce a `go.work.sum`, neither the CI porcelain check nor the deploy's clean-worktree gate will flag it any more.
