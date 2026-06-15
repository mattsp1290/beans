# Plan (FINAL v1): `/forgejo` skill — create or migrate a git repo to git.birb.homes

Decisions locked after dual-subagent review + Socratic pass with the user.

## Scope (updated — local push promoted into v1)
- **migrate** an existing GitHub repo → remote-exec the validated host helper.
- **create** a fresh empty repo → host API.
- **push** a local working tree → create empty remote, pin host key, wire `forgejo`
  remote, ensure SSH auth, push the current branch. (Promoted from v2 after the first
  live run: on a LAN-only, fixed-port instance the host-key pin and key registration
  proved cheap to automate.) Default for a local clone of a GitHub repo = push-local-tree.

## Locked policy
- **Owner:** infer from source owner; fall back to `birbparty` if absent. (Matches the
  helper's own `TARGET_OWNER=${TARGET_OWNER:-$SOURCE_OWNER}` default.)
- **Visibility:** private by default.
- **Existing non-empty target:** HARD STOP. No `--force-mirror` / `--prune` reachable in v1.
- **Org creation:** opt-in only; default fail-if-missing (do NOT pass `--create-org` unless the user explicitly confirms creating the org).

## Grounded facts
- Base URL `https://git.birb.homes`; SSH remote `ssh://git@git.birb.homes:2222/<owner>/<repo>.git`.
- `FORGEJO_TOKEN` is ONLY in `~/gitea/.env.forgejo` on `infra-admin@10.0.0.106`.
- Helper `~/scripts/github-to-forgejo-sync` is dry-run by default, `--apply` writes; it
  ensures org/repo via API and mirror-pushes heads+tags over HTTPS. Verified: it
  **redacts the token in dry-run output** (`[redacted]` for the push header; literal
  unexpanded `$FORGEJO_TOKEN` in the api dry-run line).
- Local machine reaches the instance (HTTPS API + SSH 2222 both open).
- Skills live at `dotfiles/.agents/skills/<name>/SKILL.md`, symlinked into `~/.claude/skills/`.

## Skill flow
1. **Parse `$ARGUMENTS`** → classify source: GitHub ref/URL → migrate; local dir → inspect
   `origin` (GitHub → migrate-from-origin + v2 note); empty / `--create <name>` → create.
2. **Universal preflight** (read-only, collect all failures): host SSH reachable with a
   pinned `known_hosts` entry (no auto-accept on the token-bearing session); `~/gitea/.env.forgejo`
   present on host (never print it).
3. **Determine mode + confirm target**: owner (inferred), repo name (source basename), visibility (private).
4. **Target existence check** via host API `GET /api/v1/repos/<owner>/<repo>` (unauthenticated read
   where possible): absent/empty → proceed; **non-empty → hard stop & report**.
5. **Dry-run**: migrate → run helper without `--apply`; create → print the exact planned `POST`.
   Pipe all output through a token redactor before showing it.
6. **Confirm → apply**: migrate → re-run helper with `--apply`, sourcing `.env.forgejo` inside the
   SSH session; create → `POST` repo via host (token from sourced env, header not URL, no `-v`/`-x`).
7. **Verify**: `git ls-remote ssh://git@git.birb.homes:2222/<owner>/<repo>.git` from local (primary
   post-condition) + host API repo GET (secondary). Print HTTPS + SSH clone URLs.
8. **Report** + follow-ons: CI runner registration (`forgejo-add-runner-host`) and local-tree push (v2).

## Secret & destructive-op handling (from security review)
- Token use stays inside the remote SSH session sourcing `.env.forgejo`; never echoed; output redacted.
- No `--force-mirror`/`--prune` in v1 → the named-ref-destruction scenario is structurally prevented.
- Distinct git remote name if/when local push lands (v2): `forgejo`, error rather than clobber `origin`.
- `auto_init=false` on create; set `default_branch` post-push (relevant to v2 push path).

## Out of scope (v1)
CI runner registration; LFS/issues/PR/wiki migration; bulk multi-repo; local-tree push.
