# Creation Commit Implementation Guidance

Status: ready for implementation tasks

Related beads:
- `beans-ceh.1`: repo resolution and git detection audit
- `beans-ceh.2`: `bn_issue_repos` lifecycle and immutable metadata audit

## Scope

This note records the current repo-resolution and `bn_issue_repos` write/read
paths that future creation-commit implementation work must preserve.

The intended contract is still the one in
`.agents/plans/associate-the-commit-the-repo-was-at-when-a-bean-was-made.md`:
`creation_commit` is immutable creation-time metadata on the per-issue repo
link row, not mutable repo registry state.

## Repo Resolution Inputs

Current precedence must remain unchanged:

1. `--repo` flag
2. `.bn` marker `repo` field
3. cwd git auto-detect
4. no repo context

Current prefix precedence must also remain unchanged:

1. `--project`
2. `BN_PROJECT`
3. `.bn` marker `project`
4. cwd git auto-detect only when no prefix was found above
5. prefix-required error when a command requires it

The commit-capture decision belongs after repo resolution selects a concrete
`store.Repo`, but before `store.CreateIssue` is called. At that point command
code has both:

- the selected repo row (`ID`, `Slug`, `Prefix`, normalized `RemoteURL`)
- the cwd git identity (`Toplevel`, `RemoteURL` or synthesized `file://`, and
  future `HEAD` value)

Do not push this identity comparison into `store.CreateIssue`: the store does
not know cwd, the active `.bn` marker, or whether a `--repo` value was selected
from the current checkout or from another repo.

## Cwd Identity Comparison Matrix

The cwd HEAD should be stored only when the cwd git identity matches the repo
being linked. If it cannot be proven, create the issue normally and leave
`creation_commit` empty.

| Case | Selected repo source | Cwd identity input | Match rule | Commit result |
| --- | --- | --- | --- | --- |
| Plain auto-detect | `tryGitAutoDetect` registers or resolves cwd repo | same `Toplevel` plus `remote.origin.url`, or synthesized `file:///abs/toplevel` when no remote exists | true by construction after `AutoRegisterRepo` returns | capture cwd `HEAD` if `git rev-parse HEAD` succeeds |
| `.bn` marker repo | marker `repo` slug, looked up with current prefix | cwd normalized `remote.origin.url`, or synthesized `file:///abs/toplevel` | compare normalized cwd URL to selected repo `RemoteURL` | capture only on URL match |
| Explicit `--repo <slug>` | `GetRepoBySlug(arg, arg)` today for repo context, or command-local slug path for create/update | cwd normalized `remote.origin.url`, or synthesized `file:///abs/toplevel` | compare normalized cwd URL to selected repo `RemoteURL`; do not assume slug equality is enough | capture only on URL match |
| Explicit `--repo <url>` | `AutoRegisterRepo(RemoteURL: arg)` | cwd normalized `remote.origin.url`, or synthesized `file:///abs/toplevel` | compare normalized cwd URL to selected repo `RemoteURL` | capture only on URL match |
| Local-only cwd repo | auto-detect synthesizes `file:///abs/toplevel` | resolved absolute git toplevel converted to `file://` | compare synthesized URL to selected repo `RemoteURL` | capture cwd `HEAD` on match |
| Prefix mismatch | `--project` or `BN_PROJECT` differs from auto-detected repo prefix | selected repo may be resolved, but `cmd_create.go` only links it when `repo.Prefix == rs.prefix` | no repo link is created by current create behavior | do not capture a commit without a repo link |

The comparison input should be the normalized URL used by `bn_repos.remote_url`,
not the displayed slug. Slug equality is insufficient because distinct repos
can collide before disambiguation, and explicit slug selection can point at a
repo different from cwd.

## Git Resolver Surface

`cmd/bn/git_resolver.go` currently exposes:

- `Toplevel(dir string) (root string, ok bool, err error)`
- `RemoteURL(root string) (url string, ok bool, err error)`

Creation commit capture needs one additional best-effort method, for example:

```go
HEAD(root string) (commit string, ok bool, err error)
```

The production implementation should run `git rev-parse HEAD` with `cmd.Dir =
root`. It should return `ok == false` and no fatal command error for unborn
repos, missing git, permission errors, or any other git failure that should not
change create behavior. The accepted value must be a full lowercase
40-character hex object ID.

## Current Store Lifecycle

`store/store.go` and `store/gorm_models.go` have one physical issue-repo link
table model today:

- `gormIssueRepo` maps `bn_issue_repos`.
- The current columns are `issue_id`, `repo_id`, `requested_ref`, `base_ref`,
  `work_branch`, `worktree_subdir`, `metadata`, `created_at`, and `updated_at`.
- There is no `creation_commit` field yet.

Future implementation should add:

- `IssueRepoInput.CreationCommit`
- `model.RepoTarget.CreationCommit`
- `gormIssueRepo.CreationCommit`
- migration `0011` adding `bn_issue_repos.creation_commit TEXT NOT NULL DEFAULT ''`
- CLI JSON nested repo field `creation_commit,omitempty`

Validation belongs at the store boundary that accepts `IssueRepoInput`, so both
CLI create and import use the same rule. A non-empty value must be the full
lowercase 40-character hex object ID returned by git.

## Store Paths That Write Or Replace `bn_issue_repos`

### `CreateIssue`

`CreateIssue` resolves `IssueRepoInput.RemoteURL` before the issue transaction.
When `RemoteURL` is set, `AutoRegisterRepo` decides the effective issue prefix
and supplies a repo slug if `RepoSlug` was empty. When only `RepoSlug` is set,
the issue prefix comes from `CreateIssueInput.Prefix`.

Guidance:

- Pass the command-layer captured commit through `IssueRepoInput.CreationCommit`.
- Do not let `AutoRegisterRepo` or repo registry updates supply this value.
- `insertIssueRepoGORM` should persist it on the new link row.
- If no commit was captured, store `''`.

### `insertIssueRepoGORM`

This is the single helper that currently creates `bn_issue_repos` rows. It is
called from both `CreateIssue` and `UpdateIssue`.

Guidance:

- Trim and validate `IssueRepoInput.CreationCommit`.
- Persist it to `gormIssueRepo.CreationCommit`.
- Include it in the returned `RepoTarget`.
- Keep existing ref/subdir/default-branch behavior unchanged.

### `UpdateIssue`

`UpdateIssue` currently deletes the existing `bn_issue_repos` row whenever
`UpdateIssueInput.Repo != nil`, then reinserts via `insertIssueRepoGORM`.
That delete-and-reinsert path will clear immutable metadata unless it reads and
reuses the old value.

Guidance:

- Before deleting, read the existing link row's `creation_commit`.
- If the update input has an explicit non-empty `CreationCommit`, use it only
  for import or a dedicated store-internal create/restore path, not normal CLI
  retargeting.
- For normal `bn update --repo`, `--ref`, or `--subdir`, carry forward the old
  `creation_commit` into the replacement row.
- If there was no old row, store `''` unless the caller explicitly supplied a
  validated import value.

The CLI update path (`cmd/bn/cmd_update.go`) currently only passes repo slug,
requested ref, and subdir. That is good: normal update should not recapture or
replace a creation commit.

## Store Paths That Read `bn_issue_repos`

### `populateIssueRepos`

Every read path that hydrates issues calls `populateIssueRepos` after loading
issues, including `ListIssues`, `GetIssue`, `ReadyIssues`, `ListMembers`, and
other list-style queries.

Guidance:

- Select `ir.creation_commit` in the join.
- Populate `model.RepoTarget.CreationCommit`.
- Keep empty strings as empty strings in Go; JSON `omitempty` handles display.

### `repoTargetFromIssueRepo`

This helper builds the create/update return value after insertion. It must stay
aligned with `populateIssueRepos`.

Guidance:

- Accept and set `creation_commit`.
- Return the same value that was persisted, after validation/normalization.

## Import And Export Paths

### Current import

`ImportInput` does not contain repo routing fields. `ImportIssuesFull` creates
or updates only `bn_issues` and dependency rows. It never writes
`bn_issue_repos` today.

Guidance:

- Extend `ImportInput` with an optional repo payload that can carry slug,
  remote URL, refs/subdir/metadata, and `creation_commit`.
- On create, if `repo.remote_url` is present, resolve through the existing
  `AutoRegisterRepo` path and insert the issue-repo link with the imported
  commit.
- Otherwise, if `repo.slug` is present, resolve it under the destination prefix
  and insert the link with the imported commit.
- If `creation_commit` is present but the repo payload cannot be resolved,
  reject that import line clearly; do not silently drop immutable metadata.
- Older imports without a repo payload must keep today's behavior.
- Merge-mode updates to an existing issue should preserve any existing
  `creation_commit` unless the implementation explicitly defines a restore
  mode for missing repo links.

### Current export and JSON output

`toIssueJSON` already emits the nested repo object used by `bn show --json`,
`bn list --json`, and related JSON output. `cmd/bn/cmd_export.go` currently
emits a bd-compatible line without repo routing metadata.

Guidance:

- Add `creation_commit,omitempty` to `repoTargetJSON`.
- Extend export with an optional nested `repo` object when an issue has repo
  routing metadata.
- Keep old exports readable by leaving the repo object optional.
- Include non-empty `creation_commit` in export so import can preserve it.

## Implementation Order

1. Add characterization tests for current repo resolution and `UpdateIssue`
   repo replacement behavior before changing logic.
2. Add migration/model/input/output fields.
3. Implement store validation, write, preserve, and hydration behavior.
4. Extend CLI resolver and create path to capture cwd HEAD only after identity
   comparison succeeds.
5. Extend JSON/export/import round-trip behavior.
6. Update docs/specs once behavior is implemented.

## Acceptance Checklist

- Repo identity comparison inputs are documented for plain auto-detect, `.bn`
  marker repo, explicit slug, explicit URL, local-only `file://` repos, and
  prefix mismatch cases.
- Existing repo and prefix precedence are unchanged.
- Every current store path that writes, replaces, reads, or exports
  `bn_issue_repos` has implementation guidance for supplying or preserving
  `creation_commit`.
- Normal issue updates preserve the original creation commit.
- Import/export behavior has an explicit rule for preserving non-empty commits
  without breaking older JSON.
