# Decision Record: Topology Option (a) — prefix == repo-slug

**Status:** Confirmed  
**Date:** 2026-06-15  
**Closes:** beans-6zn

---

## Decision

Adopt topology option (a): each auto-registered git repository receives its own
`bn_projects` row where **prefix == slug**. This means a repo whose computed
slug is `myapp` gets `bn_projects.prefix = "myapp"` and
`bn_repos.slug = "myapp"` within that prefix.

Option (b) (shared-prefix, multi-repo-per-prefix) is rejected unless a named
blocker is found below; none was found.

---

## Source confirmation

Every fact below was verified against the live source tree.

### Schema constraints

File: `schema/migrations/sqlite/0001_bn_init.sql:15`

```sql
prefix TEXT NOT NULL REFERENCES bn_projects(prefix),
```

`bn_issues.prefix` is NOT NULL and foreign-keyed to `bn_projects.prefix`.  
`bn_projects.prefix` is a `PRIMARY KEY` (line 9), so all prefix values must be
globally unique — there can be no two projects with the same prefix string.

File: `schema/migrations/sqlite/0004_bn_repos.sql:10,24`

```sql
prefix TEXT NOT NULL REFERENCES bn_projects(prefix) ON DELETE CASCADE,
…
UNIQUE(prefix, slug),
```

`bn_repos.prefix` references the project, and `(prefix, slug)` is UNIQUE, so
within any project the slug is distinct. Under topology (a) the prefix IS the
slug, so the uniqueness of the project primary key subsumes this constraint.

### ID generation

File: `store/store.go:1708-1716`

```go
func generateID(prefix string) (string, error) {
    b := make([]byte, 3)
    if _, err := rand.Read(b); err != nil {
        return "", err
    }
    return prefix + "-" + hex.EncodeToString(b), nil
}
```

All issue IDs are `"{prefix}-{6-hex-chars}"`. Under topology (a) the prefix
visible in every issue ID is the repo slug, so IDs read `myapp-a1b2c3` — both
human-readable and immediately repo-scoped.

### Store query sites that filter on prefix

All list/read paths below pass `prefix` as a `WHERE` clause. No query reads
across prefixes without an explicit caller choice.

| Function | File:line | Predicate |
|---|---|---|
| `ListFilter` struct | store.go:281 | `Prefix string` field; `ListIssues` applies `WHERE prefix = ?` |
| `ListIssues` | store.go:288 | `Where("prefix = ?", f.Prefix)` |
| `ReadyIssues` | store.go:333 | `Where("prefix = ?", prefix)` |
| `ListDeps` | store.go:687 | `JOIN bn_issues i ON i.id = d.issue_id` + `Where("i.prefix = ?", prefix)` |
| `ListBlockingDeps` | store.go:712 | same join + `Where("i.prefix = ? AND d.dep_type = ?", prefix, DepTypeBlocks)` |
| `ListMembers` | store.go:739 | `Where("i.prefix = ? AND d.blocked_by_id = ? AND d.dep_type = ?", prefix, parentID, DepTypeParentChild)` |
| `ListParents` | store.go:773 | `Where("i.prefix = ? AND d.issue_id = ? AND d.dep_type = ?", prefix, childID, DepTypeParentChild)` |

`ListFilter.Prefix` is the only filter field on the struct (besides `States`
and `Limit`); there is no cross-prefix list path in existing code.

`EnsureProject` (`store.go:76`) is idempotent and safe to call on the computed
prefix before auto-registering a new repo.

### Existing `bn_repos` functions (pre-multi-repo)

`GetRepoByRemoteURL` does **not exist** yet — it is a planned addition
(beans-2uq / beans-qea). The existing entry points are `GetRepoBySlug`,
`ResolveRepoAlias`, and `ListRepos`. The auto-register entry point to be
created in `store/repo_store.go` (beans-qea) will need `GetRepoByRemoteURL`
internally or will call through a `NormalizeRemoteURL` canonicalizer
(beans-2uq) before matching.

---

## Slug-collision disambiguation (critical gap — resolved here)

### The problem

`bn_projects.prefix` is a PRIMARY KEY. Two distinct git remotes that both
derive slug `app` (e.g. `github.com/alice/app` and `github.com/bob/app`)
cannot both use `prefix = "app"`. The auto-register path must produce a unique
prefix for every remote URL without manual intervention.

### Decided algorithm

The auto-register entry point (beans-qea) MUST attempt prefixes in this order,
stopping at the first value that is not already in `bn_projects`:

1. **Bare repo name**: `{repo}` — e.g. `app`
2. **Owner-qualified**: `{owner}-{repo}` — e.g. `alice-app`
3. **Host-qualified**: `{host-shortname}-{owner}-{repo}` where `host-shortname`
   is derived by stripping the TLD and common suffixes:  
   `github.com → github`, `gitlab.com → gitlab`, `bitbucket.org → bitbucket`,
   any other host → first label of the hostname (e.g. `git.corp.example.com → git`).
4. **Numeric suffix**: `{owner}-{repo}-2`, `{owner}-{repo}-3`, … (incrementing
   until a free slot is found, capped at `-99` before hard-failing).

The derived slug stored in `bn_repos.slug` is always the same as the prefix
chosen from this sequence. Both are written in a single transaction:
`EnsureProject(ctx, prefix)` followed by `CreateRepo(ctx, CreateRepoInput{Prefix: prefix, Slug: prefix, …})`.

**Rationale for this ordering:**
- Step 1 keeps IDs short in the common case (most users have at most one repo
  named `app`).
- Step 2 handles the most common collision (same name, different owners).
- Step 3 handles the rare case of identical owner+repo on two hosts.
- Step 4 is a deterministic but opaque fallback that can handle any remaining
  collision without blocking the user.

### User-facing prefix (issue ID prefix)

Under topology (a), the prefix visible in every issue ID IS the computed slug.
If Alice registers `github.com/alice/app` first (gets prefix `app`) and Bob
later registers `github.com/bob/app` (gets prefix `bob-app`), Bob sees issue
IDs like `bob-app-a1b2c3`. This is intentional: it's always traceable back to
the repo without looking anything up.

### What beans-qea must implement

The auto-register entry point in `store/repo_store.go` (beans-qea) must:
1. Accept a normalized remote URL (output of `NormalizeRemoteURL` from
   beans-2uq).
2. Check whether a `bn_repos` row already exists for that URL; if yes, return
   it immediately (idempotent).
3. Derive the base slug from the URL path components using the four-step
   sequence above, checking `ProjectExists` at each step.
4. Run the two writes (`EnsureProject` + `CreateRepo`) in a transaction.
5. Return the new `Repo` struct (including the final `Prefix`/`Slug`).

---

## No blockers found

No structural reason was found to deviate from topology (a). The collision
disambiguation algorithm above keeps prefix==slug while maintaining uniqueness.
Implementation proceeds.

---

## Related issues

- beans-2uq — `NormalizeRemoteURL` canonicalizer (unblocked by this record)
- beans-qea — auto-register entry point (must implement the disambiguation
  algorithm defined here)
- beans-934 — repo resolution precedence decision record
- beans-kf2 — injectable git-resolver seam
- beans-u79 — 0009 migration: unique index on normalized remote_url
