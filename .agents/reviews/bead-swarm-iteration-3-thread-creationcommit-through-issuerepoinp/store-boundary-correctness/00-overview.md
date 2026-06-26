# Store Boundary Correctness Review

- Branch: `bead-swarm/iteration-3-thread-creationcommit-through-issuerepoinp`
- Date: 2026-06-26
- Reviewer: Store Boundary Correctness
- Reviewer slug: `store-boundary-correctness`
- Role: Checks validation, persistence, and transactional behavior at the store API boundary.
- Overall verdict: REQUEST_CHANGES

The branch threads `CreationCommit` through the issue-repo store input/model/GORM surfaces, persists the value on `bn_issue_repos`, hydrates it through the common issue repo population path, and adds SQLite contract coverage for create/get/list/ready plus invalid values. The first review pass found two important store-boundary concerns: invalid `CreationCommit` with `Repo.RemoteURL` was validated after auto-registration side effects, and normal `UpdateIssue` repo retargeting can still clear creation commits. The first item was fixed in commit `968d8e2`; the second is intentionally deferred to the already-created dependent bead `beans-ceh.6`.

Stats:
- Files changed at review: 5
- Lines added/removed at review: 99 insertions, 3 deletions
- Commits at review: 2

