# Review Fixes

Validation after fixes:

- `go test ./store -run 'TestSQLiteStoreContractIssueRepoTarget'`: pass
- `go test ./schema -run 'TestMigrationRequiredObjects|TestDialectSpecificDDL|TestMigrateSQLiteBackfillsIssueRepoCreationCommit'`: pass
- `go test -tags=integration ./store -run 'TestStoreContractAcrossDialects/.*/repos_audit_and_issue_targets' -count=1`: pass
- `make test`: pass

Resolved items:

- Dialect Contract Auditor Important item for explicit valid replacement: fixed by adding a cross-dialect `UpdateIssue` assertion that supplies a different valid 40-character commit and verifies the original `creation_commit` remains unchanged.
- Dialect Contract Auditor Important item for create-time invalid validation: fixed by adding a cross-dialect invalid `CreateIssue` attempt and verifying only the original issue remains persisted.
- Regression Safety Auditor Important item for invalid create-time values: fixed by the same invalid `CreateIssue` regression assertion.
- Regression Safety Auditor Important item for empty-string compatibility: fixed by adding a cross-dialect `CreateIssue` assertion that accepts empty `CreationCommit` and hydrates an empty string.
- Regression Safety Auditor suggestion for route-field semantics: addressed by asserting retargeted `BaseRef` and `WorkBranch` values alongside ref/subdir and `creation_commit`.

Findings fixed re-reviewed by independent Codex subagents: true.
