# `bn` CLI Plan

## Principles

- Repo onboarding is explicit and durable.
- Issue authoring from inside an onboarded repo should be low-friction.
- `bn` should never put secrets on argv or in issue rows.
- Repo registry mutation is an admin operation, not merely possession of
  `BN_DSN`.
- Existing commands keep working for repo-less projects.

## New Commands

### `bn repo admin`

Bootstrap and inspect repo administrators:

```bash
bn repo admin add punk1290
bn repo admin list
bn repo admin remove old-actor
```

The first admin bootstrap needs a guarded path. Acceptable options:

- `bn init --prefix=<proj> --admin=<actor>` seeds the initial admin.
- `bn repo admin add --bootstrap <actor>` works only when the project has no
  admin rows.

After the first admin exists, admin changes require admin authorization.

### `bn repo add`

```bash
bn repo add <slug> \
  --remote git@github.com:punk1290/boxy.git \
  --default-branch main \
  --path ~/git/boxy \
  --auth ssh-key:github-default
```

Effects:

- Ensures project exists.
- Inserts or updates `bn_repos`.
- Adds common aliases.
- Writes or updates repo-local marker data in `.bn`.
- Writes a redacted repo-audit row.

`--path` is local-machine metadata only. It helps `bn` infer repo identity when
run from that checkout, but the orchestrator must not assume it can access that
path.

`--auth` is required unless `BN_DEFAULT_REPO_AUTH_REF` or a project-level
default is configured. The CLI validates that the auth reference is syntactically
compatible with the remote URL scheme, but the full connectivity check belongs
to `bn repo doctor`.

### `bn repo list`

```bash
bn repo list
bn repo list --json
```

Shows slug, enabled state, default branch, remote URL, and optional local path
from marker data.

### `bn repo show`

```bash
bn repo show boxy
bn repo show --json boxy
```

Includes aliases and recent issue counts.

### `bn repo update`

```bash
bn repo update boxy --remote git@github.com:punk1290/boxy.git
bn repo update boxy --disable
bn repo update boxy --enable
bn repo update boxy --worktree-subdir packages/api
```

### `bn repo remove`

Prefer soft-disable over hard delete:

```bash
bn repo remove boxy
bn repo remove boxy --hard
```

Hard delete should fail if issues reference the repo unless `--force` is used.

### `bn repo doctor`

```bash
bn repo doctor boxy
bn repo doctor boxy --from-orchestrator
```

Checks:

- Repo exists and is enabled.
- Remote URL scheme and host are allowed by policy.
- `auth_ref` resolves to a configured secret reference.
- Known-hosts entry exists for SSH remotes.
- The orchestrator context can run `git ls-remote` non-interactively.

The first implementation can execute the same checks from the local CLI
environment. `--from-orchestrator` is the target operator UX and may initially
shell through `docker compose exec symphony` or a small diagnostic command.

## Issue Authoring Changes

### `bn create`

Add flags:

```bash
bn create "Fix flaky timer test" --repo boxy
bn create "Fix release script" --ref release/2026-06 --repo boxy
bn create "Fix import path" --repo shady --subdir packages/server
```

Default inference order:

1. `--repo`.
2. Repo marker in nearest `.bn`.
3. `BN_REPO`.
4. Project default repo, if configured.
5. No repo target.

If the project has more than one repo and no repo can be inferred, `bn create`
should fail with a clear message:

```text
repo required: use --repo, set BN_REPO, or run from a repo with a .bn marker
```

`bn create --repo` must use one transaction for issue insert, repo-link insert,
initial note, and audit write. If any repo-routing write fails, the issue should
not be created.

### `bn show`

Show repo routing:

```text
Repo: boxy
Remote: git@github.com:punk1290/boxy.git
Ref: main
Subdir: .
```

### `bn update`

Add:

```bash
bn update <id> --repo boxy
bn update <id> --ref main
bn update <id> --subdir packages/server
```

Changing repo/ref on an `in_progress` issue should fail unless `--force` is
provided. The dispatcher may already have prepared a workspace for the old
target.

Changing repo registry records (`bn repo add/update/remove`) requires project
admin authorization. Changing an individual issue's repo target follows the
normal issue update permission model, but should still be blocked for terminal
or in-progress issues unless `--force` is passed.

## `.bn` Marker

The existing `.bn` marker stores the project prefix. Extend it without breaking
old readers:

```ini
project=agent-work
repo=boxy
remote=git@github.com:punk1290/boxy.git
```

Parsing rules:

- Unknown keys ignored.
- `project` remains required for project inference.
- `repo` is optional.
- `remote` is advisory and can be used to warn when marker and registry differ.

## Imports

`bn import` should accept optional mapping flags:

```bash
bn import issues.jsonl --repo boxy
bn import issues.jsonl --repo-map oldprefix=boxy
```

For bd migration, a single `--repo` is enough for most projects. More complex
imports can map by label or source prefix later.

## JSON Output

All issue JSON should include a nullable `repo` object:

```json
{
  "id": "agent-work-abc",
  "title": "Fix flaky timer test",
  "repo": {
    "slug": "boxy",
    "requested_ref": "",
    "worktree_subdir": ""
  }
}
```
