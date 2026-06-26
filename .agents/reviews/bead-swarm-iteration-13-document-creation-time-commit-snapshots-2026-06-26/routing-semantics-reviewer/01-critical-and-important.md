# Critical And Important Findings

## Critical

No Critical findings.

## Important

### Prefix mismatch paragraph contradicted implementation

- Severity: Important
- File: `docs/specs/repo-resolution-precedence.md:45`
- Problem: The earlier prefix-chain paragraph said an auto-detected repo slug is attached when an explicit project prefix disagrees with the auto-detected prefix. The new snapshot section and implementation behavior say the prefix guard rejects that repo link, leaving no attached repo and no `creation_commit`.
- Suggested fix: Update the earlier paragraph to say the explicit prefix remains in effect and the mismatched auto-detected repo is not attached to the issue.
