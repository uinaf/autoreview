#!/usr/bin/env bash

set -euo pipefail

test -n "${APPLE_TEAM_ID:-}"
[[ "${GORELEASER_CURRENT_TAG:-}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]

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
  grep -q "^TeamIdentifier=${APPLE_TEAM_ID}$" <<<"$details"
  grep -Eq '^CodeDirectory .*flags=.*runtime' <<<"$details"
  grep -q '^Timestamp=' <<<"$details"
  grep -q '^Authority=Developer ID Application:' <<<"$details"
done
