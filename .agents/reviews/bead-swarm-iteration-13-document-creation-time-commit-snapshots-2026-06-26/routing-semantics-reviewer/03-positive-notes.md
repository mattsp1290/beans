# Positive Notes

- `docs/specs/repo-resolution-precedence.md` clearly separates repo selection from commit capture and documents the identity-match requirement before reading HEAD.
- `docs/specs/repo-resolution-precedence.md` explicitly states dirty state is not recorded, which avoids implying unstaged or staged changes are recoverable from `creation_commit`.
- `docs/specs/topology-a-prefix-equals-slug.md` places `creation_commit` in the invariants section as issue routing metadata, not mutable repo registry state.
- `README.md` gives users a concise explanation of when `creation_commit` is filled versus left empty.
