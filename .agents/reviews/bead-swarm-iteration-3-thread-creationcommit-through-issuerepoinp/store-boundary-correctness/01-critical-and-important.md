# Critical And Important

## Critical

No critical issues found.

## Important

### Fixed: remote repo auto-registration happened before creation_commit validation

- Severity: Important
- File: `store/store.go:242`
- File: `store/store.go:296`
- File: `store/store.go:1545`

`CreateIssue` resolved and auto-registered `IssueRepoInput.RemoteURL` before validating `IssueRepoInput.CreationCommit`. Invalid non-empty commits therefore returned a validation error only after repo/project side effects had already been written.

Suggested fix:

```go
if in.Repo != nil {
    if _, err := validateCreationCommit(in.Repo.CreationCommit); err != nil {
        return Issue{}, fmt.Errorf("store: CreateIssue repo creation_commit: %w", err)
    }
    repoSlug = in.Repo.RepoSlug
}
```

Resolution: fixed in commit `968d8e2`, with coverage asserting no repo or issue exists after invalid remote creation commit input.

### Deferred: UpdateIssue retargeting still clears existing creation_commit

- Severity: Important
- File: `store/store.go:621`
- File: `store/store.go:625`
- File: `store/store.go:1562`

`UpdateIssue` deletes and reinserts `bn_issue_repos`. Because ordinary update callers do not supply `IssueRepoInput.CreationCommit`, an existing non-empty `creation_commit` can be replaced by an empty value during repo/ref/subdir retargeting.

Suggested fix:

```go
// Before deleting the existing link, read its creation_commit and carry it into
// the replacement row unless the caller explicitly supplied a valid value.
```

Resolution: not fixed in this bead by scope. This is the explicit acceptance of dependent bead `beans-ceh.6` ("Preserve creation_commit when UpdateIssue retargets repo routing"), which is blocked by this bead and should be handled next.

