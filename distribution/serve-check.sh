#!/bin/sh
# Check that a bn binary initializes a hub and serves the complete embedded UI.
#
# Usage: serve-check.sh <path-to-bn>
set -eu

work=
pid=

fail() {
  printf 'serve-check.sh: %s\n' "$1" >&2
  exit 1
}

cleanup() {
  if [ -n "$pid" ]; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  if [ -n "$work" ]; then
    rm -rf "$work"
  fi
}

main() {
  [ $# -eq 1 ] || fail "usage: serve-check.sh <path-to-bn>"
  case $1 in
    /*) bn=$1 ;;
    *) bn=$PWD/$1 ;;
  esac
  [ -x "$bn" ] || fail "$bn is not executable"
  command -v curl >/dev/null 2>&1 || fail "curl is required"
  command -v git >/dev/null 2>&1 || fail "git is required"

  trap cleanup EXIT
  trap 'exit 1' INT TERM HUP
  work=$(mktemp -d)

  mkdir "$work/home"
  export HOME="$work/home"
  export BEANS_HOME="$work/beans"
  export GIT_CONFIG_GLOBAL=/dev/null
  export GIT_AUTHOR_NAME=serve-check GIT_AUTHOR_EMAIL=serve-check@example.invalid
  export GIT_COMMITTER_NAME=serve-check GIT_COMMITTER_EMAIL=serve-check@example.invalid
  unset BEANS_HUB BN_CONFIG BEANS_PROJECT BN_ACTOR

  git init --quiet --bare --initial-branch=main "$work/remote.git"
  git init --quiet --initial-branch=main "$work/seed"
  printf 'base\n' >"$work/seed/data.txt"
  git -C "$work/seed" add -A
  git -C "$work/seed" commit --quiet -m seed
  git -C "$work/seed" push --quiet "$work/remote.git" main

  cd "$work"
  "$bn" init "$work/remote.git" >/dev/null || fail "bn init failed"

  # Port 0 lets the kernel choose; bn prints the bound address on stderr.
  "$bn" serve --port 0 2>"$work/serve.log" &
  pid=$!
  body=
  tries=0
  while [ "$tries" -lt 20 ]; do
    url=$(sed -n 's/^Serving //p' "$work/serve.log")
    if [ -n "$url" ] && body=$(curl -fsS --max-time 5 --url "$url/"); then
      break
    fi
    kill -0 "$pid" 2>/dev/null || fail "bn serve exited: $(cat "$work/serve.log")"
    tries=$((tries + 1))
    sleep 1
  done

  [ -n "$body" ] || fail "bn serve did not answer within 20 seconds: $(cat "$work/serve.log")"
  case $body in
    *"The UI has not been built into this binary"*)
      fail "the binary embeds the placeholder UI, not the built app"
      ;;
  esac
  printf 'serve-check.sh: %s serves the embedded UI\n' "$bn"
}

main "$@"
