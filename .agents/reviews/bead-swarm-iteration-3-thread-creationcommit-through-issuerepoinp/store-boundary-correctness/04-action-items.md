## Action Items

### Critical

- [x] No critical action items.

### Important

- [x] `store/store.go:242` Validate `IssueRepoInput.CreationCommit` before `CreateIssue` auto-registers `Repo.RemoteURL`, so invalid commits return before repo/project writes.
- [x] `store/store.go:621` Preserve existing `creation_commit` when `UpdateIssue` retargets repo routing. Deferred to scoped follow-up bead `beans-ceh.6`, not blocking `beans-ceh.5`.

### Suggestions

- [x] No suggestion action items.

