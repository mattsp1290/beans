# CLI Regression Analyst Review Overview

Branch: bead-swarm/iteration-5-expose-creation-commit-in-stable-cli-re
Date: 2026-06-26
Reviewer: CLI Regression Analyst
Reviewer slug: cli-regression-analyst
Reviewer role: Reviews command behavior, regression risk, and test isolation for CLI output paths.

The branch adds `creation_commit` only to the issue repo JSON DTO with `omitempty`, maps it from the hydrated repo target, and switches the affected `show`, `list`, and `ready` JSON paths to the command output writer so tests can capture the actual command output. The non-JSON branches still call the existing detail/table renderers, so table output behavior is not changed by this diff.

Overall verdict: APPROVE

Stats: 6 files changed, 147 insertions, 5 deletions.
