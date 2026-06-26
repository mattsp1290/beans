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

File: `schema/migrations/sqlite/0004_bn_repos.sql:10,24-25`

```sql
prefix TEXT NOT NULL REFERENCES bn_projects(prefix) ON DELETE CASCADE,
…
UNIQUE(prefix, slug),
CHECK (slug <> '' AND slug NOT GLOB '*[^a-z0-9._-]*' AND substr(slug, 1, 1) GLOB '[a-z0-9]'),
```

`bn_repos.prefix` references the project, and `(prefix, slug)` is UNIQUE.
Under topology (a) the prefix IS the slug, so the uniqueness of the project
primary key subsumes this constraint. The slug CHECK restricts slugs (and
therefore all prefixes) to lowercase alphanumeric characters, dots, underscores,
and hyphens, with the first character in `[a-z0-9]`. This constraint is enforced
only at the `bn_repos` row — `bn_projects.prefix` has no CHECK — so a malformed
value passes `EnsureProject` and only fails at the `CreateRepo` write. The slug
normalization step in the disambiguation algorithm below is what prevents this.

### ID generation

File: `store/store.go:1710-1716`

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
human-readable and immediately repo-scoped. Prefix length is intentionally
unbounded; step-3 and step-4 slugs produce longer IDs.

### Store query sites that filter on prefix

All issue list/read paths below pass `prefix` as a `WHERE` clause. No issue
query reads across prefixes without an explicit caller choice.

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
and `Limit`); there is no cross-prefix issue list path in existing code. Note:
memory queries (`SearchMemories`) use `prefix = ? OR prefix IS NULL` to include
global memories — that is an intentional exception for the memories subsystem,
not a pattern to replicate for issue queries.

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

### Step 0 — Slug normalization (runs before every candidate)

Before testing any candidate against `bn_projects`, normalize it to satisfy the
`bn_repos.slug` CHECK constraint. Apply this transform to every candidate string
(bare repo name, owner-qualified, host-qualified, numeric suffix base):

1. Lowercase the string.
2. Replace every run of characters outside `[a-z0-9._-]` with a single `-`.
3. Strip leading characters until the first character is in `[a-z0-9]`.
4. Strip trailing `-` characters.
5. If the result is empty, skip this candidate and move to the next step in the
   sequence.

This normalization runs on each candidate string *before* the `ProjectExists`
probe, so the value tested for collision is the exact value that will be written.
Normalization can itself create collisions (`My-App` and `my_app` both normalize
to `my-app`), which the numeric-suffix fallback absorbs.

**Worked example:** `github.com/MyOrg/My_App.git` → components: host=`github`,
owner=`myorg`, repo=`my_app` (`.git` suffix stripped per below). Candidate 1:
`my_app`. Candidate 2 if collision: `myorg-my_app`.

### Step 0a — `.git` suffix stripping

Strip the `.git` suffix from the last path component before deriving any slug
candidate. Responsibility: this stripping happens inside the auto-register
entry point (beans-qea) when parsing the normalized URL, not inside
`NormalizeRemoteURL` (beans-2uq), unless beans-2uq's spec is updated to
guarantee it. Without stripping, `github.com/alice/app.git` would produce
prefix `app.git` (valid per the slug CHECK since `.` is allowed) and issue IDs
like `app.git-a1b2c3` — unintended.

### Decided algorithm

The auto-register entry point (beans-qea) MUST attempt the following candidate
prefixes in order. Each candidate is normalized via Step 0 before being checked
or written. Stop at the first candidate that (a) normalizes to a non-empty
string and (b) is not already present in `bn_projects`.

1. **Bare repo name**: `{repo}` — e.g. `my_app`
2. **Owner-qualified**: `{owner}-{repo}` — e.g. `myorg-my_app`. If `{owner}` is
   absent (single-segment URL or bare `ssh://host/repo`), skip this step.
3. **Host-qualified**: `{host-shortname}-{owner}-{repo}`. If `{owner}` is absent,
   use `{host-shortname}-{repo}`. Host-shortname derivation:

   | Input | Shortname |
   |---|---|
   | `github.com` | `github` |
   | `gitlab.com` | `gitlab` |
   | `bitbucket.org` | `bitbucket` |
   | IP address / `localhost` | use literal (e.g. `10-0-0-1`, `localhost`) |
   | Host with port (e.g. `git.corp.com:2222`) | strip port, then first label (`git`) |
   | Any other hostname | first label (e.g. `git.corp.example.com → git`) |

   Note: two different hosts can produce the same shortname (e.g.
   `git.alice.com` and `git.bob.com` both yield `git`). Step 4 handles residual
   collisions.

4. **Numeric suffix**: `{owner}-{repo}-2`, `{owner}-{repo}-3`, … If `{owner}` is
   absent, use `{repo}-2`, `{repo}-3`, …. Increment until a free slot is found.
   Cap at `-99` (i.e. try up to 98 numeric suffixes); if all 98 are taken, return
   a wrapped sentinel error (e.g. `ErrSlugExhausted`) that the caller surfaces as
   a human-readable message. The `-2` base starts from the owner-qualified form
   (step 2's candidate), not the step-3 form.

### Invariants

- **One repo per prefix:** Under topology (a), one `bn_projects` prefix contains
  exactly one `bn_repos` row. This is an explicit, testable post-condition of every
  auto-register call. A prefix found to be already occupied by a *different* remote
  URL always forces advancing to the next disambiguation step — it is never reused
  or shared.
- **Immutability:** A repo's prefix is assigned once at first registration and is
  immutable thereafter. Renaming the remote or re-running auto-register for the
  same normalized URL returns the existing row unchanged (idempotent by URL, not
  by slug).
- **Idempotency key:** The early-return check in step 2 of `beans-qea` MUST be
  keyed on the **normalized remote URL** (via `GetRepoByRemoteURL`), never on the
  derived slug. Two distinct remotes that happen to normalize to the same slug are
  distinct repos; only an exact URL match is a duplicate.
- **Creation commit is issue metadata, not registry state:** `creation_commit`
  lives on `bn_issue_repos`, where it snapshots the selected issue repo's cwd
  `HEAD` at issue creation time when the cwd git identity matches that selected
  repo. It is not part of the `bn_repos` row, so auto-register idempotency,
  slug disambiguation, and repo updates never mutate historical issue snapshots.
  The snapshot records only the exact commit object ID; dirty worktree state is
  intentionally outside topology (a) and outside repo registry semantics.

### Transaction safety

The `ProjectExists` probe is inherently outside the write transaction, creating a
check-then-act window: a concurrent auto-register for a different remote could
claim the same prefix between the probe and the write. The implementation MUST
handle this with bounded retry:

- If `CreateRepo` returns `ErrConflict` (prefix/slug already exists), treat it as
  a collision and advance to the next disambiguation candidate, then retry from
  that candidate.
- Cap total retries at 5 full-sequence attempts before surfacing an error to the
  caller.

Within a single attempt, `EnsureProject` and `CreateRepo` MUST share one
database transaction so a `CreateRepo` failure rolls back the project insert.
(Without this, a failed `CreateRepo` leaves an orphan row in `bn_projects`.)

### User-facing prefix (issue ID prefix)

Under topology (a), the prefix visible in every issue ID IS the computed slug.
If Alice registers `github.com/alice/app` first (gets prefix `app`) and Bob
later registers `github.com/bob/app` (gets prefix `bob-app`), Bob sees issue
IDs like `bob-app-a1b2c3`. This is intentional: the prefix is always traceable
back to the repo without a lookup.

### What beans-qea must implement

The auto-register entry point in `store/repo_store.go` (beans-qea) must:
1. Accept a normalized remote URL (output of `NormalizeRemoteURL` from
   beans-2uq). The assumed output shape: scheme-normalized URL string with
   `.git` suffix optionally still present (beans-qea strips it).
2. Check whether a `bn_repos` row already exists for that normalized URL via
   `GetRepoByRemoteURL`; if yes, return it immediately (idempotent by URL).
3. Parse URL path components; strip `.git` suffix; derive `{host}`, `{owner}`,
   `{repo}` per the rules above.
4. Run the four-step candidate sequence with Step 0 normalization and Step 4
   numeric cap, calling `ProjectExists` at each step.
5. On a candidate that passes: open a transaction, call `EnsureProject(ctx, tx,
   prefix)` then `CreateRepo(ctx, CreateRepoInput{Prefix: prefix, Slug: prefix,
   …})`. On `ErrConflict`, advance and retry (up to 5 full-sequence retries).
6. Return the new `Repo` struct (including the final `Prefix`/`Slug`), or a
   wrapped `ErrSlugExhausted` if all candidates are exhausted.

---

## No blockers found

No structural reason was found to deviate from topology (a). The collision
disambiguation algorithm above keeps prefix==slug while maintaining uniqueness.
Implementation proceeds.

---

## Related issues

- beans-2uq — `NormalizeRemoteURL` canonicalizer (unblocked by this record)
- beans-qea — auto-register entry point (must implement the full algorithm
  defined here, including normalization, `.git` stripping, transaction safety,
  and bounded retry)
- beans-934 — repo resolution precedence decision record
- beans-kf2 — injectable git-resolver seam
- beans-u79 — 0009 migration: unique index on normalized remote_url
