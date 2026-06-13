# Data Model

## Requirements

The database must answer these questions without parsing issue text:

- Which repo does this issue target?
- Where should the orchestrator fetch that repo from?
- Which ref should this run start from?
- What subdirectory should be the agent cwd?
- What revision did the agent actually run against?
- Who onboarded or changed repo configuration?

## Tables

### `bn_repos`

```sql
CREATE TABLE bn_repos (
    id              TEXT PRIMARY KEY,
    prefix          TEXT NOT NULL REFERENCES bn_projects(prefix),
    slug            TEXT NOT NULL,
    display_name    TEXT NOT NULL,
    remote_url      TEXT NOT NULL,
    default_branch  TEXT NOT NULL DEFAULT 'main',
    worktree_subdir TEXT NOT NULL DEFAULT '',
    clone_strategy  TEXT NOT NULL DEFAULT 'mirror-cache',
    auth_ref        TEXT NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      TEXT NOT NULL DEFAULT '',
    updated_by      TEXT NOT NULL DEFAULT '',
    UNIQUE(prefix, slug)
);
```

`remote_url` should be a normalized git URL. It may be HTTPS or SSH depending
on operator policy.

`auth_ref` is a logical reference such as `ssh-key:github-default` or
`token:github-readonly`; it is not the secret itself.

`auth_ref` is required unless the deployment explicitly configures a default
auth reference. The CLI should make `--auth` required in normal repo onboarding
because a missing auth reference usually produces a delayed runtime failure for
private repositories.

### `bn_repo_aliases`

```sql
CREATE TABLE bn_repo_aliases (
    prefix      TEXT NOT NULL REFERENCES bn_projects(prefix),
    alias       TEXT NOT NULL,
    repo_id     TEXT NOT NULL REFERENCES bn_repos(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(prefix, alias)
);
```

Aliases let `clckr`, `birbparty/clckr`, and `github.com/punk1290/clckr` resolve
to the same repo.

### `bn_issue_repos`

Keep issue-to-repo routing separate from `bn_issues` at first to avoid a risky
wide migration of the core issue table.

```sql
CREATE TABLE bn_issue_repos (
    issue_id         TEXT PRIMARY KEY REFERENCES bn_issues(id) ON DELETE CASCADE,
    repo_id          TEXT NOT NULL REFERENCES bn_repos(id),
    requested_ref    TEXT NOT NULL DEFAULT '',
    base_ref         TEXT NOT NULL DEFAULT '',
    work_branch      TEXT NOT NULL DEFAULT '',
    worktree_subdir  TEXT NOT NULL DEFAULT '',
    metadata         JSONB NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

`requested_ref` is what the user asked for. `base_ref` is the normalized branch,
tag, or SHA chosen by the router. `work_branch` is the generated branch name if
the run creates one.

Issue creation with repo metadata must be atomic: `bn create --repo` inserts
`bn_issues`, the optional initial note, `bn_issue_repos`, and the repo-audit row
in one transaction. A repo-link failure must not leave an unroutable issue.

### `bn_repo_audit`

```sql
CREATE TABLE bn_repo_audit (
    id          BIGSERIAL PRIMARY KEY,
    prefix      TEXT NOT NULL REFERENCES bn_projects(prefix),
    repo_id     TEXT NULL REFERENCES bn_repos(id) ON DELETE SET NULL,
    action      TEXT NOT NULL,
    actor       TEXT NOT NULL,
    old_values  JSONB NOT NULL DEFAULT '{}',
    new_values  JSONB NOT NULL DEFAULT '{}',
    command     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Audit JSON must be redacted before insert. It can include remote URL, branch,
auth reference name, aliases, and enabled state; it must not include private key
material, HTTPS tokens, or raw credential-helper output.

### Repo Admins

Add a simple project-scoped authorization table before repo mutation commands:

```sql
CREATE TABLE bn_project_admins (
    prefix     TEXT NOT NULL REFERENCES bn_projects(prefix),
    actor      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(prefix, actor)
);
```

Repo registry mutations require the current `BN_ACTOR` to be a project admin
unless an explicit local-dev escape hatch is enabled. Issue creation can remain
available to non-admin actors.

### `run_attempts` additions

Add nullable repo snapshot columns to the persistence schema:

```sql
repo_id        TEXT NOT NULL DEFAULT '',
repo_slug      TEXT NOT NULL DEFAULT '',
repo_remote    TEXT NOT NULL DEFAULT '',
repo_ref       TEXT NOT NULL DEFAULT '',
repo_revision  TEXT NOT NULL DEFAULT '',
workspace_path TEXT NOT NULL DEFAULT ''
```

These should be a snapshot, not foreign-key dependent audit data. If a repo is
renamed later, historical runs should still say what they used.

Use application-generated text IDs for repos rather than database UUID defaults.
The existing bn migrations do not enable `pgcrypto`, and keeping repo IDs in the
same style as issue IDs avoids adding an extension dependency just for registry
rows.

## Domain Types

Add core types:

```go
type RepoTarget struct {
    ID             string
    Slug           string
    RemoteURL      string
    DefaultBranch  string
    RequestedRef   string
    BaseRef        string
    WorkBranch     string
    WorktreeSubdir string
    CloneStrategy  string
    AuthRef        string
}

type Issue struct {
    ...
    Repo *RepoTarget
}
```

If adding `Repo` directly to `core.Issue` creates too much blast radius, use a
parallel `IssueAssignment` field first and migrate later.

Prompt rendering also needs a workspace context type if templates expose
workspace fields:

```go
type WorkspaceContext struct {
    Root     string
    Cwd      string
    Revision string
}
```

Adding this requires updating `core.RenderContext`, its synthetic validation
context, and every render construction call.

## Validation Rules

- `slug` must be lowercase URL-safe: `[a-z0-9][a-z0-9._-]*`.
- `remote_url` must match allowed schemes: initially `git@...` SSH and
  `https://...`.
- `worktree_subdir` must be relative and must not contain `..`.
- Disabled repos cannot receive new issues unless `--force` is provided.
- `bn create --repo` should fail if the repo slug is unknown.
- `bn update --repo` should be allowed only for non-terminal issues unless
  `--force` is passed.
- Repo registry mutations require project-admin authorization.
- Runtime revalidates repo URL scheme, allowed host, enabled state, and
  `auth_ref` compatibility before every checkout.

## Migrations

Add one migration for registry tables, and a later migration for run-attempt
snapshot columns if keeping persistence schema changes separate is safer.

Migration order:

1. `0004_bn_repos.sql`
2. `0005_bn_issue_repos.sql`
3. `0006_bn_repo_audit_and_admins.sql`
4. Persistence migration for run-attempt repo snapshot fields.

Repo registry tables use the `bn_` namespace and should live under
`internal/tracker/postgres/migrations` initially, so `pgtracker.New` migrates
them for both `bn` and the Postgres tracker adapter. Higher-level domain code
can still live in `internal/repository`; it should use the migrated `bn_`
tables rather than owning a second migration path. The runtime's persistence
migrator separately handles run-attempt schema changes.
