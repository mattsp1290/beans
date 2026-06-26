# Suggestions

### Add explicit update-retarget coverage when implementing the follow-up bead

- File: `store/store_sqlite_contract_test.go:704`

The shared insert helper is exercised, but an explicit `UpdateIssueInput.Repo.CreationCommit` retarget assertion would better pin the follow-up immutability contract. This belongs with `beans-ceh.6`.

### Consider operation-neutral validation wording for the shared helper

- File: `store/store.go:1547`

`insertIssueRepoGORM` is shared by create and update paths, but its error prefix still says `CreateIssue`. Consider a future wording cleanup such as `store: issue repo creation_commit` if this becomes confusing in update/import callers.

