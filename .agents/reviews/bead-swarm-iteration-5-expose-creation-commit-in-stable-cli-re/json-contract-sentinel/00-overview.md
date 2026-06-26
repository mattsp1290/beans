# JSON Contract Sentinel Review Overview

Branch: bead-swarm/iteration-5-expose-creation-commit-in-stable-cli-re
Date: 2026-06-26
Reviewer: JSON Contract Sentinel
Reviewer slug: json-contract-sentinel
Reviewer role: Reviews stable CLI JSON shape, omitempty behavior, and command coverage.

The branch cleanly extends the stable CLI repo JSON contract by adding `repo.creation_commit` with `omitempty`, maps the value from `store.Issue.Repo.CreationCommit`, and covers `show`, `list`, and `ready` JSON command output without changing the table-output branches. The tests exercise both populated and legacy-empty repo targets, including absence of the field when empty, and focused package tests pass.

Overall verdict: APPROVE

Stats: 6 files changed, 147 insertions, 5 deletions. Verification run: `go test ./cmd/bn` passed.
