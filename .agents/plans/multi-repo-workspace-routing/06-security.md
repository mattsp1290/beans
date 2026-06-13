# Security and Trust Boundaries

## Threat Model

The agent will run shell commands inside cloned repositories. Repo onboarding
therefore expands the blast radius from "one known workspace" to "any repo a
user registers". The design must make the target repo explicit, keep credentials
out of tracker data, and prevent path traversal or accidental host filesystem
access.

## Boundaries

### Tracker Database

Trusted for routing metadata, not for secrets.

Allowed:

- Repo slug.
- Remote URL without embedded credentials.
- Default branch.
- Worktree subdir.
- Logical auth reference.

Forbidden:

- Private key material.
- HTTPS tokens.
- Full credential-bearing URLs.

### Orchestrator Container

Trusted to read deployment secrets and clone repos. It must not receive secrets
through issue descriptions or `bn` flags.

### Local Operator Machine

May have local repo paths, but those paths are advisory. The orchestrator host
cannot and should not dereference `/Users/...` paths.

## Remote URL Policy

Validate remote URLs at `bn repo add` time and again at runtime:

- Allow `git@github.com:owner/repo.git`.
- Allow `ssh://git@host/owner/repo.git`.
- Allow `https://host/owner/repo.git`.
- Reject URLs with username/password components in HTTPS.
- Reject local absolute paths until host-mounted mode is deliberately added.
- Reject `file://` by default.

Runtime should have an allowlist of git hosts, especially when running behind
Squid or strict egress.

This is not hardening to defer. Host allowlisting and runtime revalidation are
part of phase-one repo routing: `bn repo add` validates operator input, and the
workspace router validates the same URL, host, enabled state, and auth policy
again before checkout.

## Path Policy

For `worktree_subdir`:

- Must be relative.
- Must clean to itself.
- Must not contain `..`.
- Must not be absolute.
- Must exist after checkout before running hooks.

For workspace paths:

- All derived paths must remain under `workspace.root`.
- Use existing workspace sanitization helpers where possible.

## Git SSH

Use `BatchMode=yes` for all git SSH commands. No interactive prompts in
long-running orchestrator processes.

Known hosts must be pinned. Do not set `StrictHostKeyChecking=no` in production.

The workspace router resolves `auth_ref` for each git command. A single process
wide `GIT_SSH_COMMAND` cannot represent multiple auth refs safely.

## Egress

Repo onboarding may require extra domains in Squid policy:

- GitHub SSH over `22` or HTTPS over `443`.
- GitHub tarball/CDN hosts if using HTTPS.
- Other forge domains as repos are added.

This needs an operator command that can preflight:

```bash
bn repo doctor boxy
```

The doctor should check that the orchestrator can reach and authenticate to the
remote without running an agent.

## Audit

Every repo registry mutation should create an audit note or dedicated audit row:

- Actor.
- Old values and new values.
- Timestamp.
- Source command.

Run attempts should snapshot repo identity and revision so historical records
remain meaningful even if a repo slug or remote changes later.

## Authorization

Possession of `BN_DSN` is not enough to mutate repo routing. Repo add/update,
alias changes, auth-ref changes, enable/disable, and hard delete require a
project-admin actor. The initial implementation can use a simple
`bn_project_admins` table keyed by `(prefix, actor)` and a local-dev escape
hatch, but production docs should treat repo registry mutation as an
administrative action.
