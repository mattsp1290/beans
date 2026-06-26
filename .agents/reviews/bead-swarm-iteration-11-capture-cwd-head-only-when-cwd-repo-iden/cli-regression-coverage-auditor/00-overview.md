# CLI Regression Coverage Auditor

Branch: bead-swarm/iteration-11-capture-cwd-head-only-when-cwd-repo-iden
Date: 2026-06-26
Reviewer slug: cli-regression-coverage-auditor
Role: Checks command behavior, edge-case coverage, and regression risk around create-time repo routing.

Summary: The create command now resolves the selected repo row before constructing the store input and performs a best-effort cwd identity/HEAD capture after repo selection. The tests cover the acceptance matrix: auto-detect captures HEAD, marker and explicit slug capture only when cwd identity matches, mismatch keeps creation_commit empty, local-only auto-detect captures via file URL, and best-effort git failures do not block issue creation.

Overall verdict: APPROVE

Stats: 5 files changed, 287 insertions, 16 deletions, 2 commits.
