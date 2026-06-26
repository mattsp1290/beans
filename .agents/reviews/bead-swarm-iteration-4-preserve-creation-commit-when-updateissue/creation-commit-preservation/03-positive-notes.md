# Positive Notes

- The implementation keeps repo replacement inside the existing `UpdateIssue` transaction.
- The tests exercise both preservation of existing immutable metadata and first-time creation of repo links through `UpdateIssue`.
- Invalid explicit `CreationCommit` values are rejected before the old link can be deleted.
