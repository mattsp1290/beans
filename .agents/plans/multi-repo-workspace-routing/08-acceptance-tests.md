# Acceptance Tests

## Unit Tests

### Repo Validation

- Reject empty slug.
- Reject uppercase slug.
- Reject slug with spaces.
- Reject HTTPS URL with embedded password.
- Reject absolute `worktree_subdir`.
- Reject `../` subdir.
- Accept SSH GitHub shorthand URL.
- Accept HTTPS GitHub URL without credentials.
- Reject a remote host outside the deployment allowlist.
- Reject missing `auth_ref` unless a default auth ref is explicitly configured.

### Marker Parsing

- Existing `.bn` with only `project=` still parses.
- Extended marker with `project=` and `repo=` parses.
- Unknown marker keys are ignored.
- Empty `repo=` is ignored or rejected consistently.

### Repo Resolution

- Resolve by slug.
- Resolve by alias.
- Disabled repo cannot receive new issues without force.
- Unknown repo returns typed validation error.

### Authorization

- `bn init --admin=<actor>` or admin bootstrap creates the first project admin.
- Non-admin actor cannot run `bn repo add`.
- Non-admin actor cannot change remote URL, aliases, auth ref, or enabled state.
- Admin actor can mutate repo records and each mutation writes a redacted audit
  row.

## Postgres Integration Tests

Use testcontainers Postgres.

- Migrations create `bn_repos`, `bn_repo_aliases`, and `bn_issue_repos`.
- `bn repo add` is idempotent.
- `bn create --repo boxy` writes issue and repo link in one transaction.
- `bn ready --json` includes repo metadata.
- `bn update <id> --repo shady` moves a non-terminal issue.
- Terminal issue repo update fails without `--force`.
- Failed repo-link insert rolls back the whole `bn create --repo` transaction.
- Repo audit rows include actor, action, old values, and new values without
  credential material.

## Workspace Router Tests

Use local bare git repos as remotes.

- Prepare checkout from mirror cache.
- Second issue for same repo reuses mirror.
- Two repos route to different checkout paths.
- Requested branch is checked out.
- Missing branch returns `repo_ref_not_found`.
- Dirty failed attempt does not affect retry attempt.
- `worktree_subdir` sets cwd below checkout root.
- Two concurrent checkouts for the same repo serialize mirror updates and do not
  corrupt the mirror or share mutable worktrees.
- Runtime rejects a repo host outside the allowlist even if the DB row exists.
- Missing or wrong known-hosts data produces `repo_auth_failed` or
  `repo_fetch_failed` without leaking secrets.

## Runtime Integration Test

Create two fixture repos:

- `repo-a` contains `repo.txt` with `a`.
- `repo-b` contains `repo.txt` with `b`.

Seed two `bn` issues:

- Issue A targets `repo-a` and asks agent to append `done-a`.
- Issue B targets `repo-b` and asks agent to append `done-b`.

Run orchestrator with a deterministic fake provider or replay provider.

Assert:

- Both issues close.
- Each run attempt has correct `repo_slug`.
- Each run attempt has non-empty `repo_revision`.
- Repo checkout failures produce durable run-attempt rows and run events.
- `repo-a` checkout contains only `done-a`.
- `repo-b` checkout contains only `done-b`.
- No prompt text was needed to tell the agent where to clone.

## Host Smoke Test

Against the real hosts:

1. Start infra compose with Postgres tracker mode.
2. Verify Spark model endpoint from infra.
3. Create SSH tunnel from Mac to infra Postgres.
4. Run:

   ```bash
   cd ~/git/boxy
   bn repo add boxy --path "$PWD" --remote git@github.com:punk1290/boxy.git --auth ssh-key:github-default
   bn repo doctor boxy --from-orchestrator
   bn create "Smoke: inspect repo name" -d "Read the repo and close with a note."
   ```

5. Confirm orchestrator prepares `/workspace/repos/boxy/...`.
6. Confirm issue closes.
7. Confirm run attempt stores repo snapshot.

## Done Criteria

- Adding a new repo does not require editing `WORKFLOW.md`.
- Creating an issue from an onboarded local repo automatically routes it.
- Orchestrator can work on at least three repos under one project prefix.
- Repo credentials are not stored in tracker rows.
- Historical run attempts preserve repo slug, remote, ref, and revision.
- Existing repo-less `bn` issues and single-repo workflows keep working.
