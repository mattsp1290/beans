# Positive Notes

- `model/issue.go:103` uses `json:"creation_commit,omitempty"`, preserving empty-string compatibility for existing and non-git cases.
- `store/store.go:1623` adds the column to the shared hydration select instead of patching individual read APIs, which keeps `GetIssue`, `ListIssues`, and `ReadyIssues` aligned.
- `store/store_sqlite_contract_test.go:655`, `store/store_sqlite_contract_test.go:680`, and `store/store_sqlite_contract_test.go:688` directly assert the create, list, and ready hydration behavior requested by the bead.

