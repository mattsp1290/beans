## Action Items

### Critical
- [ ] [apps/bean-counter/scripts/deploy-production.sh:223] Run the parser under `LC_ALL=C` — BSD `sed` aborts the whole stream on a byte that is not valid UTF-8, hiding every `replace` directive after it and making the gate accept a hostile go.mod
- [ ] [apps/bean-counter/scripts/deploy-production.sh:261] Check the parser's exit status and fail closed; today the status is discarded and `set -e` is suspended because the only caller is `check_sanctioned_replace … || fatal`, so a dead parser is indistinguishable from "no more directives"

### Important
- [ ] [apps/bean-counter/scripts/deploy-production.sh:414] Compare `pwd -P` to `git rev-parse --show-toplevel`, not `$PWD` — a checkout reached through any symlinked path component (symlinked `~/git`, `/tmp` on macOS) can never satisfy the guard and there is no `--force`
- [ ] [apps/bean-counter/scripts/deploy-production.sh:416] Assert `libs/beans` is not a symlink and resolves inside the repository root — the gate checks the directive text, and a committed symlink out of the tree passes every guard and feeds `EMBEDDED_MAX` from outside the repo
- [ ] [apps/bean-counter/test/scripts/deploy-production_test.sh:103] Add a regression case for the non-UTF-8 truncation that asserts the *parsed directive count* (2), not just rejection — CI runs on ubuntu/GNU sed where the bypass does not reproduce, so a rejection-only test would be green either way

### Suggestions
- [ ] [apps/bean-counter/scripts/deploy-production.sh:278] Accept a fully-quoted replace target (`=> "../../libs/beans"`), which is legal go.mod and resolves correctly, or document that only the unquoted spelling is supported
- [ ] [apps/bean-counter/scripts/deploy-production.sh:1006] Run `check_sanctioned_replace` before `resolve_embedded_migration_max`, so the parity gate's input is not derived through an unvalidated `replace`
- [ ] [apps/bean-counter/scripts/deploy-production.sh:223] Add `--` before the path argument to `sed` so a path beginning with `-` cannot be read as an option
- [ ] [apps/bean-counter/scripts/deploy-production.sh:963] Say in the dry-run output that the local gates were not run, or run them — `do_dry_run` skips `require_clean_local_ref` entirely, so it is not a rehearsal of `--check`
- [ ] [apps/bean-counter/test/scripts/deploy-production_test.sh:113] Add fixtures for CRLF line endings and a versioned left-hand side (`replace mod v0.1.0 => …`); both behave correctly today but neither is asserted
- [ ] [apps/bean-counter/test/scripts/deploy-production_test.sh:20] If any fix relies on `pipefail` propagating out of `replace_directives`, re-enable `set -o pipefail` around the replace-gate block — the harness disables it, so such a fix would be untested
