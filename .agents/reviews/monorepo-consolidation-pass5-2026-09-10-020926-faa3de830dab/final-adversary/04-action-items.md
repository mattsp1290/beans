## Action Items

### Critical

_None._

### Important

- [ ] [apps/bean-counter/scripts/deploy-production.sh:522-533] `require_repo_root` returns 0 with `apps/bean-counter/Makefile`, `apps/bean-counter/frontend/Dockerfile` and `apps/bean-counter/frontend/package.json` as committed out-of-tree symlinks (reproduced). Close it structurally: reject any tracked mode-`120000` object under the deployed paths — `git ls-files -s -- libs/beans apps/bean-counter | awk '$1=="120000"{print $4}'` must be empty — keeping the existing loop for uncommitted symlinks in `--check`/`--dry-run`.

### Suggestions

- [ ] [apps/bean-counter/test/scripts/deploy-production_test.sh:271-288] Four list entries (`libs/beans`, `libs/beans/schema`, `libs/beans/schema/migrations`, `apps/bean-counter`) survive removal at 60/60; add a case for the one property only they provide — an intermediate component symlinked to an *in-tree* location — and correct the "every path is individually mutation-covered" claim.
- [ ] [apps/bean-counter/test/scripts/deploy-production_test.sh:266-269] Add a case that `cd`s into the fixture through a symlink and asserts rc 0, so round 2's `pwd -P` fix is covered (reverting it to `$PWD` currently leaves the suite green).
- [ ] [apps/bean-counter/scripts/deploy-production.sh:467-469] Pin the `REPO_ROOT_PHYS` guard and the `root_phys` reuse with a case that shadows `go` in a subshell; both mutations currently survive at 60/60.
- [ ] [apps/bean-counter/test/scripts/deploy-production_test.sh:255-265] `unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE` (or `env -u …`) around the fixture's `git init` — with `GIT_DIR` inherited it re-initializes the caller's repository and makes the six victim cases vacuous.
- [ ] [apps/bean-counter/test/scripts/deploy-production_test.sh:302] Remove the trailing dead `build_fixture_repo` call.
- [ ] [apps/bean-counter/scripts/deploy-production.sh:606-621] `"${ssh_args[@]}"` on an empty array aborts under bash 3.2 (`set -u`), i.e. stock macOS `/bin/bash`; fails closed but with a misleading message — use the `${a[@]+"${a[@]}"}` form or assert a minimum bash version.
- [ ] [apps/bean-counter/test/scripts/deploy-production_test.sh:255-265] `git init -q -b main .` (or redirect the fixture subshell) to keep git's default-branch advice out of CI output on runners without `init.defaultBranch`.
