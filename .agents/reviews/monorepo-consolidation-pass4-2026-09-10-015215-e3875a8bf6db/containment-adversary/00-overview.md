# Review Overview

- **Branch:** `monorepo-consolidation`
- **HEAD reviewed:** `e3875a8` (`e3875a8bf6db5001c286421852bcc219e52f61e5`)
- **Date:** 2026-09-10
- **Reviewer:** Containment Adversary (`containment-adversary`)
- **Role:** Attack `require_in_repo` and its call sites — the control introduced by the previous pass's own fix.
- **Stats (`git show --shortstat e3875a8`):** 40 files changed, 4148 insertions(+), 15 deletions(-)

## Summary

I could not break `require_in_repo` itself. I threw twenty input shapes at the
real function sourced from the real script — out-of-tree symlinks plain and with
trailing slashes, `/.`, in-tree symlinks, dangling symlinks, `../../etc/passwd`,
absolute paths, the empty string, `.`, `..`, the repo root, double slashes,
spaces, embedded newlines, glob stars, brackets, and a FIFO — and every escape
attempt was rejected, with the containment `case` catching the two shapes that
slip past the `-L` refusal. The helper's logic is correct, including the file
branch's un-resolved `basename` re-append, which is safe precisely because `-L`
has already excluded a symlinked leaf and the dirname is resolved physically.
The weakness is on the two sides of the helper. First, **its wiring is
untested**: I mutated the entire `for rel in …; do require_in_repo …; done` loop
in `require_repo_root` into `true` and the suite still reported **52 passed, 0
failed** — the same "delete the check, suite stays green" failure mode the test
file's own comment claims these ten cases make impossible. Dropping only
`libs/beans/schema/migrations/postgres` from the list also survives, and so does
removing the `/` separator from the containment prefix, because the case named
"sibling path sharing the root prefix rejected" is short-circuited by the `-L`
check and never reaches the `case` at all. Second, **the list of guarded paths
omits paths the deploy trusts to decide what ships**: I built a git repo whose
`apps/bean-counter/Dockerfile`, `apps/bean-counter/frontend` and
`apps/bean-counter/deploy/docker-compose.prod.yml` are all committed symlinks
(mode `120000`) pointing outside the tree, and `require_repo_root` returned `0`
— while `require_in_repo` rejects all three when actually called on them. Given
this branch's history of each pass finding a hole in the previous pass's fix, an
untested control is the finding that matters most.

## Verdict

`REQUEST_CHANGES`
