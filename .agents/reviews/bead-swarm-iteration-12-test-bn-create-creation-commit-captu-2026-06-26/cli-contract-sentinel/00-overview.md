# Overview

- Branch: `bead-swarm/iteration-12-test-bn-create-creation-commit-captu`
- Date: `2026-06-26`
- Reviewer: CLI Contract Sentinel
- Reviewer slug: `cli-contract-sentinel`
- Reviewer role: Focuses on CLI JSON/output contracts, command-output testability, omitted JSON fields, test hermeticity, and create-behavior regression risk.
- Stats: 7 files changed, 351 insertions, 9 deletions, 2 commits

The branch adds regression coverage around `bn create` recording `creation_commit` only when the selected issue repo matches the current working git identity, rejects invalid HEAD strings before they reach store validation, verifies create JSON includes or omits `repo.creation_commit` correctly, and fixes create JSON output to honor `cmd.OutOrStdout()` instead of process stdout. The tests also clarify that git auto-detect should not read HEAD before create has selected a repo.

VERDICT: APPROVE

