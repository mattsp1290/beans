# SQLite Rebuild Guardian Review

- Branch: `bead-swarm/iteration-19-migration-0010-drop-bn-issues-state-check`
- Date: 2026-06-26
- Reviewer: SQLite Rebuild Guardian
- Reviewer slug: `sqlite-rebuild-guardian`
- Role: Audits the SQLite table rebuild for data preservation, schema fidelity, and migration edge cases.
- Overall verdict: REQUEST_CHANGES

The branch adds focused schema coverage for SQLite migration 0010, proving the legacy state CHECK rejects `ready_for_review` before v10, the v9-to-v10 migration permits `ready_for_*` states afterward, the `bn_issues` columns and `bn_issues_prefix_state_idx` survive the rebuild, and child rows in `bn_issue_repos`, `bn_issue_deps`, and `bn_issue_notes` are preserved. The remaining concern is that the initial test did not prove the rebuilt table retained live foreign-key behavior and non-state constraints/defaults after `PRAGMA foreign_keys` was toggled during the rebuild.

Stats: 2 files changed, 188 lines added, 0 removed, 2 commits at review time.
