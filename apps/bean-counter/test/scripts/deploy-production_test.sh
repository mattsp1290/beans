#!/usr/bin/env bash
# Unit tests for scripts/deploy-production.sh pure helpers and argument parsing.
# Hermetic: sources the script (the main-guard prevents a deploy from running)
# and exercises the sourceable helpers. No network, no Docker, no SSH.
#
#   bash apps/bean-counter/test/scripts/deploy-production_test.sh
#
# SC1090: every `source "$SCRIPT"` below resolves at runtime from SCRIPT_DIR,
# so shellcheck cannot follow it statically. The single-directive form only
# covers the next source, and this file sources in nine places.
# shellcheck disable=SC1090

# Intentionally NOT `set -e` in the harness: helpers return non-zero as part of
# their contract and we assert on that.
# A GIT_DIR or GIT_WORK_TREE inherited from the caller would redirect every git
# command below at the caller's repository, silently making the require_repo_root
# fixture cases vacuous. Unset them for the whole run.
unset GIT_DIR GIT_WORK_TREE

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$SCRIPT_DIR/scripts/deploy-production.sh"

source "$SCRIPT"
# The sourced script enables strict mode for live runs; disable it here so the
# harness can drive helpers that intentionally fail.
set +e +u +o pipefail

PASS=0
FAIL=0

ok()   { PASS=$((PASS+1)); printf 'ok   - %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf 'FAIL - %s\n' "$1"; }

# assert_eq <label> <expected> <actual>
assert_eq() {
  if [ "$2" = "$3" ]; then ok "$1"; else bad "$1 (expected '$2', got '$3')"; fi
}
# assert_rc <label> <expected-rc> <actual-rc>
assert_rc() {
  if [ "$2" -eq "$3" ]; then ok "$1"; else bad "$1 (expected rc $2, got $3)"; fi
}

# ----- normalize_ui_port --------------------------------------------------- #
out="$(normalize_ui_port 8088)"; assert_eq "normalize_ui_port 8088 -> 8088" "8088" "$out"
normalize_ui_port 0 >/dev/null 2>&1;     assert_rc "normalize_ui_port 0 rejected" 1 $?
normalize_ui_port 99999 >/dev/null 2>&1; assert_rc "normalize_ui_port 99999 rejected" 1 $?
normalize_ui_port abc >/dev/null 2>&1;   assert_rc "normalize_ui_port abc rejected" 1 $?
normalize_ui_port 80a >/dev/null 2>&1;   assert_rc "normalize_ui_port 80a rejected" 1 $?

# ----- assert_dsn_container_host ------------------------------------------- #
assert_dsn_container_host "postgres://u:p@postgres:5432/beans"; assert_rc "dsn uri @postgres:5432 ok" 0 $?
assert_dsn_container_host "host=postgres dbname=beans";          assert_rc "dsn libpq host=postgres ok" 0 $?
assert_dsn_container_host "host=postgres";                       assert_rc "dsn libpq host=postgres (eol) ok" 0 $?
assert_dsn_container_host "postgres://u:p@127.0.0.1:5432/beans"; assert_rc "dsn host 127.0.0.1 rejected" 1 $?
assert_dsn_container_host "host=postgresx dbname=beans";         assert_rc "dsn host=postgresx rejected" 1 $?

# ----- migration_max_from_dir ---------------------------------------------- #
tmp="$(mktemp -d)"
: > "$tmp/0001_init.sql"; : > "$tmp/0007_guards.sql"; : > "$tmp/0003_mid.sql"
: > "$tmp/notes.txt"; : > "$tmp/readme_0099.sql"   # leading non-digit -> ignored
out="$(migration_max_from_dir "$tmp")"; assert_eq "migration_max_from_dir picks max 7" "7" "$out"
empty="$(mktemp -d)"
out="$(migration_max_from_dir "$empty")"; assert_eq "migration_max_from_dir empty -> 0" "0" "$out"
rm -rf "$empty"

# ----- extract_issue_count ------------------------------------------------- #
out="$(extract_issue_count '{"issues":[{"identifier":"x-1"},{"identifier":"x-2"}]}')"
assert_eq "extract_issue_count two issues" "2" "$out"
out="$(extract_issue_count '{"issues":[]}')"
assert_eq "extract_issue_count empty array -> 0" "0" "$out"
extract_issue_count 'not-json' >/dev/null 2>&1; assert_rc "extract_issue_count rejects non-issues body" 1 $?

# ----- check_sanctioned_replace -------------------------------------------- #
# This gate replaced the old "reject any beans replace" check. In the monorepo
# the replace is mandatory, so the property being defended changed: the deploy
# must still abort when apps/bean-counter's go.mod points the library anywhere
# other than the in-repo copy. Without these cases that swap is asserted only
# in prose.
#
# The prefix and comment cases are the ones a substring test gets wrong, and a
# substring test is strictly weaker than the gate this replaced. Keep them.
gomod_tmp="$(mktemp -d)"

SANCTIONED_LINE='replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans'
HEADER='module github.com/mattsp1290/beans/apps/bean-counter

go 1.25.7

require github.com/gofiber/fiber/v3 v3.3.0
'

write_gomod() { printf '%s\n%s\n' "$HEADER" "$2" > "$gomod_tmp/$1"; }

# accepts <label> <fixture-body>
accepts() {
  write_gomod "case.mod" "$2"
  check_sanctioned_replace "$gomod_tmp/case.mod" >/dev/null 2>&1
  assert_rc "$1" 0 $?
}
# rejects <label> <fixture-body>
rejects() {
  write_gomod "case.mod" "$2"
  check_sanctioned_replace "$gomod_tmp/case.mod" >/dev/null 2>&1
  assert_rc "$1" 1 $?
}

accepts "sanctioned replace accepted" "$SANCTIONED_LINE"
accepts "sanctioned replace with odd spacing accepted" \
  "replace   github.com/mattsp1290/beans/libs/beans   =>   ../../libs/beans"
accepts "sanctioned replace with a trailing comment accepted" \
  "$SANCTIONED_LINE // in-repo library"
accepts "block form holding only the sanctioned entry accepted" \
  "replace (
	github.com/mattsp1290/beans/libs/beans => ../../libs/beans
)"

rejects "no replace at all rejected" ""
rejects "commented-out replace rejected (declares nothing)" \
  "// $SANCTIONED_LINE"
rejects "replace pointing at a local experiment rejected" \
  "replace github.com/mattsp1290/beans/libs/beans => /Users/dev/experiment/beans"
# The sanctioned text is a PREFIX of these two. A substring test accepts both.
rejects "sibling path with the sanctioned text as a prefix rejected" \
  "replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans-attacker-fork"
rejects "deeper path with the sanctioned text as a prefix rejected" \
  "replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans/vendored-fork"
rejects "unexpected second replace rejected" \
  "$SANCTIONED_LINE
replace github.com/gofiber/fiber/v3 => ../../../fiber-fork"
rejects "second replace hidden behind a repeated sanctioned comment rejected" \
  "$SANCTIONED_LINE
replace github.com/gofiber/fiber/v3 => ../../../fiber-fork // $SANCTIONED_LINE"
rejects "block form holding an extra entry rejected" \
  "replace (
	github.com/mattsp1290/beans/libs/beans => ../../libs/beans
	github.com/gofiber/fiber/v3 => ../../../fiber-fork
)"

check_sanctioned_replace "$gomod_tmp/does-not-exist.mod" >/dev/null 2>&1
assert_rc "missing go.mod rejected" 1 $?

# The parser must see EVERY directive, not merely reject the file. Asserting the
# count is what catches a parser that stops early: on BSD sed under a UTF-8
# locale, one invalid byte aborts the stream after emitting what it had already
# processed, hiding every replace below it. This suite runs on GNU sed in CI,
# where that abort does not occur, so a rejection-only assertion would pass on
# both platforms while the gate was broken on the one the deploy runs from.
directive_count() { replace_directives "$1" | sed '/^[[:space:]]*$/d' | grep -c . ; }

write_gomod "two.mod" "$SANCTIONED_LINE
replace github.com/mattsp1290/beans/libs/other => ../../libs/evil"
out="$(directive_count "$gomod_tmp/two.mod")"
assert_eq "parser sees both directives" "2" "$out"

# Same file with one Latin-1 byte between the two directives.
printf 'module m\n\ngo 1.25.7\n\n%s\n// caf\xe9 latin-1 comment\nreplace github.com/mattsp1290/beans/libs/other => ../../libs/evil\n' \
  "$SANCTIONED_LINE" > "$gomod_tmp/latin1.mod"
out="$(directive_count "$gomod_tmp/latin1.mod")"
assert_eq "parser sees both directives past an invalid UTF-8 byte" "2" "$out"
check_sanctioned_replace "$gomod_tmp/latin1.mod" >/dev/null 2>&1
assert_rc "replace hidden after an invalid UTF-8 byte rejected" 1 $?

write_gomod "one.mod" "$SANCTIONED_LINE"
out="$(directive_count "$gomod_tmp/one.mod")"
assert_eq "parser sees exactly one directive in the sanctioned file" "1" "$out"

# The real file must satisfy the gate, or a deploy from a clean checkout aborts
# on its own gate.
check_sanctioned_replace "$SCRIPT_DIR/go.mod" >/dev/null 2>&1
assert_rc "apps/bean-counter/go.mod satisfies the gate" 0 $?

# ----- require_in_repo ----------------------------------------------------- #
# The deploy's safety rests on "the recorded SHA describes what was tested", and
# any component of a path can be a committed symlink pointing out of the tree.
# The concrete hazard is libs/beans/schema/migrations, which feeds EMBEDDED_MAX:
# understating it is exactly the direction that makes the parity gate pass when
# it should abort. A previous round fixed this with no test at all - deleting
# the check left the suite green - so these cases exist to make that impossible.
repo_tmp="$(mktemp -d)"
outside_tmp="$(mktemp -d)"

# Physical paths: mktemp -d hands back /var/... on macOS, which is a symlink to
# /private/var, and the helper compares physical paths.
repo_tmp="$(cd "$repo_tmp" && pwd -P)"
outside_tmp="$(cd "$outside_tmp" && pwd -P)"

mkdir -p "$repo_tmp/libs/beans/schema/migrations/postgres" "$repo_tmp/apps/bean-counter"
: > "$repo_tmp/apps/bean-counter/go.mod"
mkdir -p "$outside_tmp/postgres"

REPO_ROOT_PHYS="$repo_tmp"

in_repo_rc() { ( cd "$repo_tmp" && require_in_repo "$1" >/dev/null 2>&1; echo $? ); }

assert_eq "real directory inside the repo accepted"    "0" "$(in_repo_rc libs/beans)"
assert_eq "real file inside the repo accepted"         "0" "$(in_repo_rc apps/bean-counter/go.mod)"
assert_eq "migrations directory inside the repo accepted" "0" \
  "$(in_repo_rc libs/beans/schema/migrations/postgres)"
assert_eq "missing path rejected"                      "1" "$(in_repo_rc libs/nope)"

# Final component is a symlink out of the tree.
ln -s "$outside_tmp" "$repo_tmp/libs/beans/schema/migrations/evil"
assert_eq "symlinked leaf rejected" "1" "$(in_repo_rc libs/beans/schema/migrations/evil)"

# An INTERMEDIATE component is a symlink out of the tree: the leaf itself is a
# real directory, so a check that only tested the last component would pass.
mkdir -p "$outside_tmp/real/postgres"
mv "$repo_tmp/libs/beans/schema/migrations" "$repo_tmp/libs/beans/schema/migrations.orig"
ln -s "$outside_tmp/real" "$repo_tmp/libs/beans/schema/migrations"
assert_eq "symlinked intermediate component rejected" "1" \
  "$(in_repo_rc libs/beans/schema/migrations/postgres)"
rm -f "$repo_tmp/libs/beans/schema/migrations"
mv "$repo_tmp/libs/beans/schema/migrations.orig" "$repo_tmp/libs/beans/schema/migrations"

# The go.mod the replace gate reads, behind a symlinked parent.
mv "$repo_tmp/apps/bean-counter" "$repo_tmp/apps/bean-counter.orig"
mkdir -p "$outside_tmp/bc"; : > "$outside_tmp/bc/go.mod"
ln -s "$outside_tmp/bc" "$repo_tmp/apps/bean-counter"
assert_eq "go.mod behind a symlinked parent rejected" "1" "$(in_repo_rc apps/bean-counter/go.mod)"
rm -f "$repo_tmp/apps/bean-counter"
mv "$repo_tmp/apps/bean-counter.orig" "$repo_tmp/apps/bean-counter"

# An IN-TREE symlink. Containment alone accepts this (it resolves to a path
# inside the repo), so this case is what gives the explicit -L refusal teeth:
# without it, dropping that check leaves the suite green.
mkdir -p "$repo_tmp/libs/beans-fork"
ln -s "$repo_tmp/libs/beans-fork" "$repo_tmp/libs/beans-intree-link"
assert_eq "in-tree symlink rejected" "1" "$(in_repo_rc libs/beans-intree-link)"

# A sibling whose name merely starts with the repo root must not satisfy the
# containment prefix match. The path under test must NOT itself be a symlink,
# or -L fires first and the containment branch is never reached - which is what
# made the previous version of this case pass for the wrong reason. Here the
# symlink is an intermediate component, so `rel` is a real directory reached
# through it.
sibling="${repo_tmp}-evil"
mkdir -p "$sibling/libs/beans"
ln -s "$sibling/libs" "$repo_tmp/libs/evil-parent"
assert_eq "sibling path sharing the root prefix rejected" "1" \
  "$(in_repo_rc libs/evil-parent/beans)"
rm -f "$repo_tmp/libs/evil-parent"
rm -rf "$sibling"

# Refuses to run at all without a root, rather than defaulting to permissive.
saved_root="$REPO_ROOT_PHYS"
REPO_ROOT_PHYS=""
assert_eq "unset REPO_ROOT_PHYS rejected" "1" "$(in_repo_rc libs/beans)"
REPO_ROOT_PHYS="$saved_root"

# ----- require_repo_root wiring -------------------------------------------- #
# The cases above pin require_in_repo's internals, but nothing called
# require_repo_root - so deleting its containment loop entirely, or dropping the
# migrations path that feeds EMBEDDED_MAX, left the suite green. These cases
# exercise the wiring. They use a local `git init` only; no network, no daemon.
repo_root_tmp="$(mktemp -d)"
repo_root_tmp="$(cd "$repo_root_tmp" && pwd -P)"
outside2_tmp="$(mktemp -d)"
outside2_tmp="$(cd "$outside2_tmp" && pwd -P)"
trap 'rm -rf "$tmp" "$gomod_tmp" "$repo_tmp" "$outside_tmp" "$repo_root_tmp" "$outside2_tmp"' EXIT

build_fixture_repo() {
  rm -rf "${repo_root_tmp:?}/repo"
  mkdir -p "$repo_root_tmp/repo"
  ( cd "$repo_root_tmp/repo" || exit 1
    # env -u: an inherited GIT_DIR/GIT_WORK_TREE would make `git init`
    # re-initialize the CALLER's repository and leave this fixture without a
    # .git at all, which silently makes every case below vacuous.
    env -u GIT_DIR -u GIT_WORK_TREE git init -q .
    mkdir -p libs/beans/schema/migrations/postgres apps/bean-counter/frontend
    : > libs/beans/schema/migrations/postgres/0001_init.sql
    : > apps/bean-counter/go.mod
    : > apps/bean-counter/Dockerfile
    mkdir -p apps/bean-counter/deploy
    : > apps/bean-counter/deploy/docker-compose.prod.yml
  )
}

# require_repo_root calls fatal, which exits. Take the SUBSHELL's status - an
# `echo $?` inside it would never run.
repo_root_rc() { ( cd "$repo_root_tmp/repo" && require_repo_root ) >/dev/null 2>&1; echo $?; }

build_fixture_repo
assert_eq "require_repo_root accepts a well-formed tree" "0" "$(repo_root_rc)"

build_fixture_repo
( cd "$repo_root_tmp/repo/apps/bean-counter" && require_repo_root ) >/dev/null 2>&1
assert_rc "require_repo_root refuses a cwd below the root" 1 $?

# Each trusted path, replaced by an out-of-tree symlink in turn. Any one of
# these passing means the containment loop no longer covers that path.
mkdir -p "$outside2_tmp/dir"; : > "$outside2_tmp/file"
for victim in libs/beans \
              libs/beans/schema/migrations/postgres \
              apps/bean-counter/frontend; do
  build_fixture_repo
  rm -rf "${repo_root_tmp:?}/repo/$victim"
  ln -s "$outside2_tmp/dir" "$repo_root_tmp/repo/$victim"
  assert_eq "require_repo_root rejects a symlinked $victim" "1" "$(repo_root_rc)"
done

for victim in apps/bean-counter/go.mod \
              apps/bean-counter/Dockerfile \
              apps/bean-counter/deploy/docker-compose.prod.yml; do
  build_fixture_repo
  rm -f "${repo_root_tmp:?}/repo/$victim"
  ln -s "$outside2_tmp/file" "$repo_root_tmp/repo/$victim"
  assert_eq "require_repo_root rejects a symlinked $victim" "1" "$(repo_root_rc)"
done

# Round 2 fixed require_repo_root comparing logical $PWD against git's physical
# toplevel, which aborted a legitimate deploy from a symlinked path. Nothing
# covered it: reverting the fix left the suite green.
build_fixture_repo
ln -s "$repo_root_tmp/repo" "$repo_root_tmp/repo-link"
( cd "$repo_root_tmp/repo-link" && require_repo_root ) >/dev/null 2>&1
assert_rc "require_repo_root accepts a repo reached through a symlinked path" 0 $?
rm -f "$repo_root_tmp/repo-link"

# A committed symlink is invisible to `git status` - the symlink IS the tracked
# object - so the clean-worktree gate cannot catch one. The per-path list cannot
# be completed by hand either, so the whole class is rejected structurally.
build_fixture_repo
( cd "$repo_root_tmp/repo" || exit 1
  ln -s "$outside2_tmp/file" apps/bean-counter/Makefile
  env -u GIT_DIR -u GIT_WORK_TREE git add -A >/dev/null 2>&1
) || true
assert_eq "require_repo_root rejects a TRACKED symlink not in the path list" "1" "$(repo_root_rc)"

# The reported path must be the real one. Default awk splitting truncates at the
# first space, and the wrong field number would name a git object id - both
# leave the gate firing while the message sends an operator to a file that does
# not exist, so assert the message, not just the status.
build_fixture_repo
( cd "$repo_root_tmp/repo" || exit 1
  ln -s "$outside2_tmp/file" "apps/bean-counter/evil name.mk"
  env -u GIT_DIR -u GIT_WORK_TREE git add -A >/dev/null 2>&1
) || true
out="$( cd "$repo_root_tmp/repo" && require_repo_root 2>&1 >/dev/null; : )"
case "$out" in
  *"apps/bean-counter/evil name.mk"*) ok "tracked-symlink abort names the full path" ;;
  *) bad "tracked-symlink abort names the full path (got: $(printf '%s' "$out" | tr '\n' ' '))" ;;
esac


build_fixture_repo

# ----- argument parsing ---------------------------------------------------- #
# parse_args mutates globals and may call fatal (exit); run in subshells.
( source "$SCRIPT" >/dev/null 2>&1; set +e; parse_args ) >/dev/null 2>&1
assert_rc "no args -> error (missing --ref)" 1 $?

( source "$SCRIPT" >/dev/null 2>&1; set +e; parse_args --ref main --force ) >/dev/null 2>&1
assert_rc "--force rejected" 1 $?

( source "$SCRIPT" >/dev/null 2>&1; set +e; parse_args --ref main --skip-smoke ) >/dev/null 2>&1
assert_rc "--skip-smoke rejected" 1 $?

( source "$SCRIPT" >/dev/null 2>&1; set +e; parse_args --ref main --bogus ) >/dev/null 2>&1
assert_rc "unknown arg rejected" 1 $?

( source "$SCRIPT" >/dev/null 2>&1; set +e; parse_args --ref main --ui-port 70000 ) >/dev/null 2>&1
assert_rc "invalid --ui-port rejected" 1 $?

out="$( source "$SCRIPT" >/dev/null 2>&1; set +eu +o pipefail
        parse_args --ref main >/dev/null 2>&1; printf '%s|%s|%s|%s|%s' "$REF" "$MODE" "$UI_PORT" "$API_PORT" "$CORS_ORIGIN" )"
assert_eq "valid parse: ref/mode/ui-port/api-port/cors" "main|live|8088|8081|https://counter.birb.homes" "$out"

out="$( source "$SCRIPT" >/dev/null 2>&1; set +eu +o pipefail
        parse_args --ref main --check >/dev/null 2>&1; printf '%s' "$MODE" )"
assert_eq "--check sets MODE=check" "check" "$out"

out="$( source "$SCRIPT" >/dev/null 2>&1; set +eu +o pipefail
        parse_args --ref main --dry-run --ui-port 9090 --cors-origin https://x.example >/dev/null 2>&1
        printf '%s|%s|%s' "$MODE" "$UI_PORT" "$CORS_ORIGIN" )"
assert_eq "--dry-run + custom port + explicit cors override" "dry-run|9090|https://x.example" "$out"

# --api-port must differ from --ui-port
( source "$SCRIPT" >/dev/null 2>&1; set +e; parse_args --ref main --api-port 8088 ) >/dev/null 2>&1
assert_rc "--api-port == --ui-port rejected" 1 $?

# ----- summary ------------------------------------------------------------- #
printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
