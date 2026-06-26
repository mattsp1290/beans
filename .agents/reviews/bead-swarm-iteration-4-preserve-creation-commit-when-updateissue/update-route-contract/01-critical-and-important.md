# Critical And Important Findings

No Critical findings.

No Important findings.

Key checks:
- `store/store.go:599` keeps `UpdateIssue` repo replacement inside one transaction.
- `store/store.go:624` validates explicit `creation_commit` input before deleting the old repo target.
- `store/store.go:629` reads the existing `creation_commit` before replacement.
- `store/store_sqlite_contract_test.go:702` covers retarget preservation.
- `store/store_sqlite_contract_test.go:714` covers ref/subdir preservation.
- `store/store_sqlite_contract_test.go:731` covers explicit replacement attempts preserving the original value.
- `store/store_sqlite_contract_test.go:783` and `store/store_sqlite_contract_test.go:800` cover adding a repo target to an issue without one.
