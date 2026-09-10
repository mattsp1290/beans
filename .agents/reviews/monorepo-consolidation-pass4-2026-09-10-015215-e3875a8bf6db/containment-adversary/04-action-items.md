## Action Items

### Critical

_None. No bypass of `require_in_repo` was found. Everything attempted is listed in `03-positive-notes.md`._

### Important

- [ ] [apps/bean-counter/test/scripts/deploy-production_test.sh:167-238] Add a case that calls `require_repo_root` end to end against a real `git init` fixture, looping over each guarded path — today, replacing `deploy-production.sh:524` with `true` (deleting the entire containment wiring) leaves the suite at 52 passed, 0 failed, and so does dropping `libs/beans/schema/migrations/postgres` from the list at line 521.
- [ ] [apps/bean-counter/scripts/deploy-production.sh:518-525] Add `apps/bean-counter/Dockerfile`, `apps/bean-counter/frontend`, `apps/bean-counter/Makefile` and `$COMPOSE_PROD` to the guarded list — `require_repo_root` returns 0 on a tree where the first three are committed symlinks (mode 120000) out of the repo, while `require_in_repo` rejects each of them when called; those paths feed the API image (line 608/931), the UI build context (589/611/932), every local gate recipe (570-585) and the remote compose (698/748/830).
- [ ] [apps/bean-counter/test/scripts/deploy-production_test.sh:225-232] Rewrite the "sibling path sharing the root prefix rejected" case to use a non-symlink path — as written, `-L` fires first and the containment `case` is never reached, so removing the `/` separator from `deploy-production.sh:252` survives with 52 passed, 0 failed.

### Suggestions

- [ ] [apps/bean-counter/scripts/deploy-production.sh:466-468] Reuse `REPO_ROOT_PHYS` instead of recomputing the root; the nested `git rev-parse` swallows its own failure and `cd ""` succeeds, so the `|| fatal` on line 468 can never fire and `root_phys` silently degrades to `$PWD`.
- [ ] [apps/bean-counter/scripts/deploy-production.sh:466-484] Fold this second, weaker containment implementation (no `-L` refusal) into `require_in_repo`, and assert `beans_phys` equals `$REPO_ROOT_PHYS/libs/beans` rather than merely being contained by the root.
- [ ] [apps/bean-counter/scripts/deploy-production.sh:239-242] Strip a trailing slash from `$rel` before the `-L` test — `libs/inlink/` is accepted where `libs/inlink` is refused (containment still catches the out-of-tree case, so this is contract drift, not a hole).
- [ ] [apps/bean-counter/scripts/deploy-production.sh:252] Decide deliberately whether the repository root itself should satisfy containment and pin it with a test; widening the pattern to accept it currently survives mutation.
- [ ] [apps/bean-counter/scripts/deploy-production.sh:1112-1116] Run `check_sanctioned_replace` before `resolve_embedded_migration_max` so `EMBEDDED_MAX` is never derived through an unvetted replace target — fail-closed today for `check`/`live`, but `print_plan`'s `embedded_max:` line can report an unvetted number in dry-run.
