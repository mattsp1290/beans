# Critical and Important

## Critical

None.

---

## Important

### I1. The containment list still omits three trusted worktree paths: `apps/bean-counter/Makefile`, `apps/bean-counter/frontend/Dockerfile`, `apps/bean-counter/frontend/package.json`

**File:** `/Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh:522-533`

Round 4 added `apps/bean-counter/Dockerfile`, `apps/bean-counter/frontend` and
`"$COMPOSE_PROD"` on this stated criterion, quoted from the code:

> Every path below is read from the worktree and decides either what ships or
> what a safety gate concludes, so each must be genuinely in-tree.

Three paths that meet that criterion exactly are still absent. Adding
`apps/bean-counter/frontend` (the directory) does **not** cover files inside it:
`require_in_repo` tests `-L` only on the final component of each listed path.

**What each omitted path controls:**

| Path | Read by | What it decides |
|---|---|---|
| `apps/bean-counter/Makefile` | `local_gates` — `make -C apps/bean-counter test`, `test-integration`, `vet`, `lint`, `fmt-check` (`deploy-production.sh:578,588,592-594`) | Every local gate, and the `PASS` lines written into `LOCAL_PREFLIGHT` and recorded remotely as `$run_dir/local-preflight.txt` |
| `apps/bean-counter/frontend/Dockerfile` | `docker build … ./apps/bean-counter/frontend` (`:621`, and remotely `:940`) | The recipe for the UI image that ships |
| `apps/bean-counter/frontend/package.json` | `npm ci && npm run check && npm test && npm run build` (`:597`) | The frontend test/build gate |

**Reproduction (run, not asserted).** Built a fixture matching the shape the new
tests use, sourced the real script, called the real function:

```
mkdir -p repo && cd repo && git init -q .
mkdir -p libs/beans/schema/migrations/postgres apps/bean-counter/frontend apps/bean-counter/deploy
: > libs/beans/schema/migrations/postgres/0001_init.sql
: > apps/bean-counter/go.mod ; : > apps/bean-counter/Dockerfile
: > apps/bean-counter/deploy/docker-compose.prod.yml
ln -s "$OUTSIDE/evil/Makefile"     apps/bean-counter/Makefile
ln -s "$OUTSIDE/evil/Dockerfile"   apps/bean-counter/frontend/Dockerfile
ln -s "$OUTSIDE/evil/package.json" apps/bean-counter/frontend/package.json
source /Users/punk1290/git/beans/apps/bean-counter/scripts/deploy-production.sh
( require_repo_root ); echo "rc=$?"
```

Result:

```
require_repo_root rc=0
lrwxr-xr-x  apps/bean-counter/frontend/Dockerfile -> /private/var/.../evil/Dockerfile
lrwxr-xr-x  apps/bean-counter/Makefile            -> /private/var/.../evil/Makefile
```

All three out-of-tree symlinks pass the gate.

**Why this is not "an attacker with commit access can just edit the Makefile."**
That is true but is a different property. The invariant this control exists to
protect is *the recorded SHA describes what was tested*. A malicious Makefile
committed as content is in the tree and shows up in the diff for that SHA. A
committed **symlink** points at content that is not in the repository at all, is
invisible to `git status` (the symlink is the committed object), and cannot be
recovered by anyone auditing the recorded SHA later. That is exactly the
distinction round 3 and round 4 used to justify the control, and it applies to
the Makefile at least as strongly as to `apps/bean-counter/Dockerfile`.

**Recommended fix — structural, not another list entry.** The enumeration cannot
be completed by hand; `go.sum`, `frontend/nginx.conf`, `frontend/vite.config.ts`
and every future gate input have the same exposure. One check subsumes all of
them, and it is the exact threat model as stated ("a committed symlink is
invisible to `git status`"):

```bash
local committed_links
committed_links="$(git ls-files -s -- libs/beans apps/bean-counter \
                   | awk '$1=="120000"{print $4}')"
[ -z "$committed_links" ] || {
  printf '%s\n' "$committed_links" >&2
  fatal "committed symlinks under the deployed paths; every deployed path must be a real in-tree object"
}
```

Verified against the real repository: `git ls-files -s -- libs/beans
apps/bean-counter | awk '$1=="120000"{print $4}'` returns empty today (no
committed symlinks), and field 4 is the path (`100644 <sha> 0<TAB>path`), so the
`awk` form is correct.

Keep the existing loop alongside it — `git ls-files` sees only tracked objects,
and `require_in_repo` additionally catches an *uncommitted* symlink in modes
(`--check`, `--dry-run`) that never reach `require_clean_local_ref`.

If the structural fix is judged out of scope for this branch, the minimum is to
add the three paths above to the list and a matching victim case for each in
`deploy-production_test.sh` (the fixture must then create them, since it
currently creates only the nine paths the list already checks — see S1).
