#!/usr/bin/env bash

set -euo pipefail

fail() {
  printf '%s\n' "$1" >&2
  exit 1
}

[[ "${EXPECTED_APPLE_TEAM_ID:-}" =~ ^[A-Z0-9]{10}$ ]] ||
  fail "expected Apple Team ID is missing or invalid"
[[ "${GORELEASER_CURRENT_TAG:-}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] ||
  fail "GoReleaser release tag is missing or invalid"

verify_dir="$(mktemp -d)"
trap 'rm -rf "$verify_dir"' EXIT

for arch in amd64 arm64; do
  archive="dist/autoreview_${GORELEASER_CURRENT_TAG}_darwin_${arch}.tar.gz"
  target_dir="$verify_dir/$arch"
  mkdir "$target_dir"
  tar -xzf "$archive" -C "$target_dir"

  binary="$target_dir/autoreview"
  codesign --verify --strict "$binary"
  details="$(codesign --display --verbose=4 "$binary" 2>&1)"
  grep -q '^Identifier=autoreview$' <<<"$details"
  grep -q "^TeamIdentifier=${EXPECTED_APPLE_TEAM_ID}$" <<<"$details"
  grep -Eq '^CodeDirectory .*flags=.*runtime' <<<"$details"
  grep -q '^Timestamp=' <<<"$details"
  grep -q '^Authority=Developer ID Application:' <<<"$details"
done
