# Repo Identity Correctness Auditor

Branch: bead-swarm/iteration-11-capture-cwd-head-only-when-cwd-repo-iden
Date: 2026-06-26
Reviewer slug: repo-identity-correctness-auditor
Role: Checks whether creation_commit capture is gated by the same normalized repo identity that create links.

Summary: The change adds a command-layer `cwdCreationCommitForRepo` helper that resolves cwd git identity through the store by remote URL, falling back to the same synthesized file URL format as git auto-detect, and only returns HEAD when that resolved repo row ID matches the selected issue repo. `bn create` now keeps track of the selected repo row for marker, explicit slug, and auto-detect paths and passes the captured commit into `IssueRepoInput` only when a repo link is actually being created.

Overall verdict: APPROVE

Stats: 5 files changed, 287 insertions, 16 deletions, 2 commits.
