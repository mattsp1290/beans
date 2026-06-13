# Workspace Routing

## Desired Behavior

Every run gets an isolated checkout derived from a repo registry entry. The
agent should begin in the correct repo directory with no prompt-level setup
instructions required.

## Workspace Layout

Use a stable cache plus per-attempt worktrees:

```text
/workspace/
  mirrors/
    boxy.git/
    shady.git/
    clckr.git/
  repos/
    boxy/
      issues/
        agent-work-abc/
          attempts/
            1/
              checkout/
    shady/
      issues/
        agent-work-def/
          attempts/
            1/
              checkout/
```

The mirror cache is shared across attempts for fetch efficiency. The checkout
directory is per attempt, so failed runs can be preserved for forensics and
retries do not inherit dirty state.

This layout is canonical for the plan. Other documents should refer to this
shape when discussing snapshots, cleanup, and host smoke tests.

## Checkout Strategies

### `mirror-cache`

Default.

1. Lock `/workspace/mirrors/<slug>.git`.
2. If absent, `git clone --mirror <remote>`.
3. If present, `git remote update --prune`.
4. Create worktree or clone from mirror into attempt checkout.
5. Checkout requested ref or default branch.
6. Create work branch if configured.

### `fresh-clone`

Useful for debugging or repos with weird mirror behavior.

1. `git clone <remote> checkout`.
2. Checkout requested ref/default branch.

### `host-mounted`

Later option for air-gapped or pre-seeded repos. It should still copy or
worktree into a per-attempt checkout rather than mutating the source mount.

## Locking

Locks needed:

- Per-repo mirror lock for fetch/update.
- Per-attempt workspace lock to prevent duplicate preparation after restart.

Use file locks inside `/workspace/.locks` or DB advisory locks keyed by repo ID.
DB locks are easier to coordinate across multiple orchestrator replicas later;
file locks are simpler for the current single-host runtime. Prefer DB advisory
locks because repo registry already lives in Postgres.

Checkout preparation must be safe when two issues for the same repo dispatch at
the same time. The mirror update happens under the repo lock; per-attempt
checkout directories are independent and must never share a mutable worktree.

## Branch and Ref Rules

Resolution order:

1. Issue `requested_ref`.
2. Repo `default_branch`.
3. Runtime fallback default, if set.

Work branch generation:

```text
symphony/<issue-id>/attempt-<n>
```

The first implementation does not have to push this branch. It should create
the branch locally so commits have a meaningful ref if later PR automation is
added.

## Cwd Selection

Prepared workspace should expose:

```go
type PreparedWorkspace struct {
    Root          string // attempt checkout root
    Cwd           string // root + worktree_subdir
    RepoSlug      string
    RemoteURL     string
    Ref           string
    Revision      string
    WorkBranch    string
    AttemptNumber int
}
```

The worker currently receives a single `IssueAssignment.WorkspacePath`, and
hooks plus shell/file/search tools use that value as their cwd. Supporting
`worktree_subdir` requires either:

1. Extending `IssueAssignment` to carry both `WorkspaceRoot` and `Cwd`, then
   updating hooks and tools to use `Cwd`; or
2. Setting `WorkspacePath` to the subdir cwd and storing the attempt checkout
   root separately in run-attempt metadata.

Prefer option 1 because cleanup, retention, and snapshots need the checkout
root, while command execution needs the cwd.

## Cleanup

Terminal issue cleanup must become repo-aware:

- Terminal states can remove attempt workspaces according to retention policy.
- Mirrors should not be removed automatically.
- Add later `symphony workspace gc` or `bn repo gc` to prune old attempts.
- The existing `workspace.Manager.Remove(issue.Identifier)` path must be
  replaced or bypassed for repo-routed issues; it deletes by issue leaf and
  cannot implement per-attempt retention.

Initial retention policy:

- Keep all failed attempts.
- Keep last successful attempt.
- Never delete mirror cache automatically.

## Prompt Context

The prompt template should get repo fields:

```text
{{ .Issue.Repo.Slug }}
{{ .Issue.Repo.RemoteURL }}
{{ .Workspace.Cwd }}
{{ .Workspace.Revision }}
```

Even though routing is not prompt-driven, exposing this context helps the agent
explain what it did and avoid confusion.

This is a schema change. `core.RenderContext`, the synthetic template-validation
context, and render construction must all gain `Workspace`; otherwise strict
template validation will reject `{{ .Workspace.* }}`.

## Error Categories

Map workspace routing failures into explicit categories:

- `repo_not_found`
- `repo_disabled`
- `repo_auth_failed`
- `repo_fetch_failed`
- `repo_ref_not_found`
- `workspace_prepare_failed`

These should appear in run events and OTel attributes. Avoid using raw git
stderr as a high-cardinality metric label.

Because these errors should be durable, run-attempt creation must precede
checkout preparation. A failed checkout should finalize the attempt with a
workspace/repo failure status rather than disappearing as a pre-dispatch error.
