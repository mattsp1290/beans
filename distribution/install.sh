#!/bin/sh
# Install the bn release binary into $BN_INSTALL_DIR (default $HOME/.local/bin).
#
# Environment:
#   BN_INSTALL_DIR        destination directory
#   BN_INSTALL_VERSION    release tag such as v0.3.0 (default: latest)
#   BN_RELEASE_BASE_URL   release base URL, https:// or file://
set -eu

default_base="https://github.com/mattsp1290/beans/releases"
tmp=

fail() {
  printf 'install.sh: %s\n' "$1" >&2
  exit 1
}

cleanup() {
  if [ -n "$tmp" ]; then
    rm -f "$tmp"
  fi
}

valid_tag() {
  case $1 in
    '' | *[!0-9.v]*) return 1 ;;
  esac
  printf '%s\n' "$1" | awk '/^v[0-9]+\.[0-9]+\.[0-9]+$/ { ok = 1 } END { exit !ok }'
}

# fetch <max-seconds> <url> <output|->
fetch() {
  if [ "$secure" = 1 ]; then
    curl -fsSL --retry 3 --connect-timeout 10 --max-time "$1" \
      --proto '=https' --proto-redir '=https' --url "$2" -o "$3"
  else
    curl -fsSL --retry 3 --connect-timeout 10 --max-time "$1" --url "$2" -o "$3"
  fi
}

# field <block> <key>: print a value from the manifest in $manifest. An empty
# block selects top-level keys; otherwise the key is read from that object.
field() {
  printf '%s\n' "$manifest" | awk -v blk="$1" -v key="$2" '
    blk != "" && $0 == "  \"" blk "\": {" { inb = 1; next }
    inb && /^  },?$/ { inb = 0; next }
    {
      if (blk != "") {
        if (!inb) next
        prefix = "    \"" key "\": "
      } else {
        prefix = "  \"" key "\": "
      }
      if (index($0, prefix) != 1) next
      v = substr($0, length(prefix) + 1)
      sub(/,$/, "", v)
      gsub(/"/, "", v)
      print v
      exit
    }'
}

sha256_of() {
  case $hasher in
    sha256sum) sha256sum | awk '{ print $1 }' ;;
    shasum) shasum -a 256 | awk '{ print $1 }' ;;
    openssl) openssl dgst -sha256 | awk '{ print $NF }' ;;
  esac
}

main() {
  case $(uname -s) in
    Linux) os=linux ;;
    Darwin) os=macos ;;
    *) fail "unsupported OS: $(uname -s)" ;;
  esac
  case $(uname -m) in
    x86_64 | amd64) arch=x86_64 ;;
    aarch64 | arm64) arch=aarch64 ;;
    *) fail "unsupported architecture: $(uname -m)" ;;
  esac
  target="$os-$arch"

  command -v curl >/dev/null 2>&1 || fail "curl is required"
  command -v awk >/dev/null 2>&1 || fail "awk is required"
  if command -v sha256sum >/dev/null 2>&1; then
    hasher=sha256sum
  elif command -v shasum >/dev/null 2>&1; then
    hasher=shasum
  elif command -v openssl >/dev/null 2>&1; then
    hasher=openssl
  else
    fail "sha256sum, shasum or openssl is required"
  fi

  want=${BN_INSTALL_VERSION:-}
  if [ -n "$want" ] && ! valid_tag "$want"; then
    fail "BN_INSTALL_VERSION must look like v1.2.3"
  fi
  base=${BN_RELEASE_BASE_URL:-$default_base}
  while :; do
    case $base in
      */) base=${base%/} ;;
      *) break ;;
    esac
  done
  case $base in
    https://*) secure=1 ;;
    file://*) secure=0 ;;
    *) fail "BN_RELEASE_BASE_URL must start with https:// or file://" ;;
  esac

  if [ -n "$want" ]; then
    manifest_url="$base/download/$want/bn-manifest.json"
  else
    manifest_url="$base/latest/download/bn-manifest.json"
  fi
  manifest=$(fetch 20 "$manifest_url" -) || fail "could not download $manifest_url"

  [ "$(field '' schema)" = 1 ] || fail "unsupported manifest schema"
  version=$(field '' version)
  valid_tag "v$version" || fail "manifest has an invalid version"
  [ "$(field '' tag)" = "v$version" ] || fail "manifest tag does not match its version"
  if [ -n "$want" ] && [ "v$version" != "$want" ]; then
    fail "manifest version v$version does not match requested $want"
  fi
  url=$(field assets "$target")
  digest=$(field sha256 "$target")
  [ -n "$url" ] || fail "target $target not found in manifest"
  [ "$url" = "$base/download/v$version/bn-$target" ] || fail "unexpected asset URL for $target"
  case $digest in
    '' | *[!0-9a-f]*) fail "invalid sha256 for $target" ;;
  esac
  [ "${#digest}" -eq 64 ] || fail "invalid sha256 for $target"

  dir=${BN_INSTALL_DIR:-${HOME:?HOME is not set}/.local/bin}
  mkdir -p "$dir" || fail "could not create $dir"
  trap cleanup EXIT
  trap 'cleanup; exit 1' INT TERM HUP
  tmp=$(mktemp "$dir/.bn-install.XXXXXX") || fail "could not create a temporary file in $dir"

  fetch 300 "$url" "$tmp" || fail "could not download $url"
  actual=$(sha256_of <"$tmp")
  [ "$actual" = "$digest" ] || fail "checksum mismatch for $target"

  chmod 0755 "$tmp"
  reported=$("$tmp" --version 2>/dev/null) || fail "downloaded binary does not run on this system"
  [ "$reported" = "bn v$version" ] || fail "downloaded binary reports '$reported', expected 'bn v$version'"

  mv -f "$tmp" "$dir/bn"
  tmp=

  if [ -t 1 ]; then
    printf '\033[32mInstalled\033[0m bn v%s to %s/bn\n' "$version" "$dir"
  else
    printf 'Installed bn v%s to %s/bn\n' "$version" "$dir"
  fi

  case ":$PATH:" in
    *":$dir:"*) ;;
    *)
      printf 'warning: %s is not on your PATH. Add it with:\n' "$dir" >&2
      # shellcheck disable=SC2016 # $PATH is printed for the user to paste
      printf '  export PATH="%s:$PATH"\n' "$dir" >&2
      ;;
  esac
  command -v git >/dev/null 2>&1 ||
    printf 'warning: git was not found on PATH; bn needs system Git at runtime\n' >&2
  found=$(command -v bn 2>/dev/null || true)
  if [ -n "$found" ] && [ "$found" != "$dir/bn" ]; then
    printf 'warning: another bn shadows this install: %s\n' "$found" >&2
  fi
}

main "$@"
