#!/usr/bin/env bash
# Unit tests for scripts/deploy-production.sh pure helpers and argument parsing.
# Hermetic: sources the script (the main-guard prevents a deploy from running)
# and exercises the sourceable helpers. No network, no Docker, no SSH.
#
#   bash test/scripts/deploy-production_test.sh

# Intentionally NOT `set -e` in the harness: helpers return non-zero as part of
# their contract and we assert on that.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$SCRIPT_DIR/scripts/deploy-production.sh"

# shellcheck source=/dev/null
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
trap 'rm -rf "$tmp"' EXIT
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
        parse_args --ref main >/dev/null 2>&1; printf '%s|%s|%s|%s' "$REF" "$MODE" "$UI_PORT" "$CORS_ORIGIN" )"
assert_eq "valid parse: ref/mode/ui-port/cors" "main|live|8088|http://10.0.0.106:8088" "$out"

out="$( source "$SCRIPT" >/dev/null 2>&1; set +eu +o pipefail
        parse_args --ref main --check >/dev/null 2>&1; printf '%s' "$MODE" )"
assert_eq "--check sets MODE=check" "check" "$out"

out="$( source "$SCRIPT" >/dev/null 2>&1; set +eu +o pipefail
        parse_args --ref main --dry-run --ui-port 9090 --host me@host >/dev/null 2>&1
        printf '%s|%s|%s' "$MODE" "$UI_PORT" "$CORS_ORIGIN" )"
assert_eq "--dry-run + custom port/host derives cors" "dry-run|9090|http://host:9090" "$out"

# ----- summary ------------------------------------------------------------- #
printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
