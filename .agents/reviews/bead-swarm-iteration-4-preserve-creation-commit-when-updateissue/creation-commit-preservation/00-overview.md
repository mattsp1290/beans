# Creation Commit Preservation Review

Reviewer: Creation Commit Preservation

Scope reviewed:
- `store/store.go` `UpdateIssue` handling for repo target delete-and-reinsert.
- `store/store_sqlite_contract_test.go` coverage for bead `beans-ceh.6`.

Result: approved. The branch preserves an existing `bn_issue_repos.creation_commit` before replacing the repo link and covers the requested retarget, ref/subdir, empty new-link, and invalid-input behavior.

Validation observed:
- `go test ./store`
- `go test ./store -run TestSQLiteStoreContractIssueRepoTarget -count=1`
