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
trap 'rm -rf "$tmp" "$gomod_tmp"' EXIT

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

# The real file must satisfy the gate, or a deploy from a clean checkout aborts
# on its own gate.
check_sanctioned_replace "$SCRIPT_DIR/go.mod" >/dev/null 2>&1
assert_rc "apps/bean-counter/go.mod satisfies the gate" 0 $?

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
