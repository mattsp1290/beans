# Positive Notes

- `cmd/bn/repo_resolve.go`: The comparison is by resolved repo row ID, not slug, which preserves the guidance that slug equality is insufficient.
- `cmd/bn/repo_resolve.go`: `fileURLForGitRoot` centralizes the local-only URL synthesis and keeps auto-detect and capture comparison aligned.
- `cmd/bn/cmd_create.go`: The code only asks for a creation commit when `repoInput` is present, so prefix-mismatch cases that create an unlinked issue do not accidentally capture a commit.
