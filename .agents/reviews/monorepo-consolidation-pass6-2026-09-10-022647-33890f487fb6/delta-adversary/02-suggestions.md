# Suggestions

## S1. `apps/bean-counter/scripts/deploy-production.sh:546-547` — the diagnostic truncates a symlink path at its first space

`awk`'s default field splitting treats a run of spaces *or tabs* as one
separator, but `git ls-files -s` emits `<mode> <object> <stage>\t<path>` — the
path is a single field only because of the tab, and `$4` therefore stops at the
first space *inside* the path. Git C-quotes paths containing newlines, tabs and
control characters (so there is no injection hazard and no multi-line record),
but it does **not** quote a plain space.

Reproduced with the real function against a fixture whose only tracked symlink is
`apps/bean-counter/evil name.mk`:

```
=== real require_repo_root, symlink path contains a space ===
  apps/bean-counter/evil
[deploy-production] ERROR: the deployed trees contain tracked symlinks (listed above); they must be real files
rc=1 (1 = gate fired)
actual path in index: apps/bean-counter/evil name.mk
```

**The gate is not bypassed.** `print $4` is non-empty for every possible
`ls-files -s` record — a path always follows the tab, and awk skips leading
separators, so there is no input for which a `120000` row yields an empty `$4`
and slips past `[ -n "$tracked_links" ]`. I looked for one specifically
(trailing-space filenames, a filename that is a single space, quoted paths) and
could not construct it. The impact is confined to the operator-facing listing:
the deploy correctly aborts, but points the operator at a path that does not
exist, which costs a round of confusion in exactly the situation where the gate
just told them something is seriously wrong with the tree.

Split on the tab instead, which makes the path field exact:

```sh
  tracked_links="$(git ls-files -s -- libs/beans apps/bean-counter \
    | awk -F'\t' 'substr($1, 1, 6) == "120000" { print $2 }')" \
    || fatal "could not enumerate tracked objects under the deployed trees"
```

Verified this variant prints the full path in every fixture I built:

```
apps/bean-counter/deep/nest/nested-link
"apps/bean-counter/link\nwith-newline"
apps/bean-counter/link with spaces
apps/bean-counter/staged-link
libs/beans/committed-link
```

and prints nothing on the real repository. `substr($1, 1, 6)` is the mode; an
equality test against the whole of `$1` would not work under `-F'\t'` because
`$1` is then `"120000 <sha> 0"`.

Optional companion, if the message should also survive a C-quoted path: run the
listing through `git ls-files -sz ... | tr '\0' '\n'` (`-z` disables quoting and
NUL-delimits), or set `-c core.quotePath=false`. Not necessary — a quoted path is
unambiguous, just ugly.

## S2. `apps/bean-counter/scripts/deploy-production.sh:546` — the pathspec anchors are themselves outside the structural check

`git ls-files -s -- libs/beans apps/bean-counter` matches index entries whose
path *is* or is *under* those prefixes. If `libs` or `apps` is itself a tracked
symlink, no index entry begins with `libs/beans` or `apps/bean-counter`, and the
new check sees nothing:

```
index entries:
100644 e69de29... 0	apps/bean-counter/go.mod
120000 2980104... 0	libs
--- new check on this tree ---
(blank above = new check sees nothing)
```

**There is no live hole.** `require_in_repo` resolves each path physically with
every intermediate component followed, so the per-path loop catches it two lines
earlier. Confirmed end to end against the real function on that fixture:

```
libs/beans resolves to .../scratchpad/out2/beans, outside the repository at .../scratchpad/anc2
[deploy-production] ERROR: libs/beans failed the in-repo containment check
rc=1
```

The suggestion is documentation, not code. The commit message correctly notes
that four of the nine per-path entries survive individual removal at 60/60 and
calls the redundancy "genuine". That is true, but it invites a future cleanup to
delete the "redundant" entries — and the ancestor components `libs` and `apps`
are covered *only* by that loop, never by the structural check. One sentence in
the comment block at line 535 would inoculate against that:

> The structural check below is anchored at `libs/beans` and `apps/bean-counter`,
> so it cannot see a tracked symlink at `libs` or `apps` themselves. The per-path
> loop above is what covers those, because it resolves every intermediate
> component. Neither gate subsumes the other.

## S3. `apps/bean-counter/test/scripts/deploy-production_test.sh:322-330` — the new case asserts only the return code, so the awk field index is not oracle-covered

Mutating `print $4` to `print $3` (which yields the stage number, `0` — non-empty,
so the gate still fires) leaves the suite at **62/62**. The other three mutations
I ran each failed exactly one case, and the right one:

| Mutation | Result |
| --- | --- |
| delete the whole tracked-symlink block | 61/62 — fails `rejects a TRACKED symlink not in the path list` |
| `here="$(pwd -P)"` → `here="$PWD"` | 61/62 — fails `accepts a repo reached through a symlinked path` |
| `"120000"` → `"120001"` | 61/62 — fails `rejects a TRACKED symlink not in the path list` |
| `print $4` → `print $3` | **62/62 — survives** |

The detection logic is well covered; only the *reporting* is not. If S1 is
applied, an assertion on the message would lock it in — capture stderr rather
than discarding it, and check the listed path round-trips:

```sh
build_fixture_repo
( cd "$repo_root_tmp/repo" || exit 1
  ln -s "$outside2_tmp/file" "apps/bean-counter/evil name.mk"
  env -u GIT_DIR -u GIT_WORK_TREE git add -A >/dev/null 2>&1
) || true
out="$( ( cd "$repo_root_tmp/repo" && require_repo_root ) 2>&1 >/dev/null )"
case "$out" in
  *"apps/bean-counter/evil name.mk"*) ok "tracked-symlink report names the whole path" ;;
  *) bad "tracked-symlink report names the whole path (got: $out)" ;;
esac
```

This case fails today (it prints `apps/bean-counter/evil`) and passes with the
S1 fix, so it is a genuine regression test rather than a restatement.

## S4. `.gitignore:29-33` — note the condition under which the ignore should be revisited

The comment is accurate about *why* the entry exists. It is worth adding the
condition that would invalidate it, since it is the same information I1 wants in
the workflow: if a future module bump makes workspace-wide MVS select a version
whose hash is in neither module's `go.sum`, `go work sync` will start producing a
`go.work.sum`, and at that point the file becomes a real checksum-pinning
artifact that arguably belongs in the tree — and nothing will flag the change,
because both the CI porcelain check and the deploy's clean-worktree gate are now
blind to it. A one-line "revisit if `go work sync` ever writes one" is enough.

Not a defect: `go work sync` writes none today (verified in an isolated copy of
HEAD, rc=0, no file, clean tree), and a `go.work.sum` that only read commands
generate is correctly ignored.
