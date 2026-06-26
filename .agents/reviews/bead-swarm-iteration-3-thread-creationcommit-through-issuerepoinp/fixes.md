# Review Fixes

- Fixed `Store Boundary Correctness` important finding: `CreateIssue` now validates `IssueRepoInput.CreationCommit` before `Repo.RemoteURL` auto-registration can write repo/project rows. Validation: `go test ./store -run TestSQLiteStoreContractIssueRepoTarget -count=1` and `make test`.
- Justified/deferred `Store Boundary Correctness` important finding: preserving existing `creation_commit` during `UpdateIssue` repo retargeting is the explicit scope of dependent bead `beans-ceh.6`, so it is not merged into this `beans-ceh.5` iteration.
- No critical findings were reported.
- `Hydration Contract Coverage` approved the diff with suggestions only.

findings_fixed_re_reviewed: false
