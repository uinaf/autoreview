#!/usr/bin/env bash

set -euo pipefail

fail() {
  printf '%s\n' "$1" >&2
  exit 1
}

[[ "${EXPECTED_APPLE_TEAM_ID:-}" =~ ^[A-Z0-9]{10}$ ]] ||
  fail "expected Apple Team ID is missing or invalid"
[[ "${APPLE_TEAM_ID:-}" == "$EXPECTED_APPLE_TEAM_ID" ]] ||
  fail "signing payload Apple Team ID does not match release policy"
test -n "${APPLE_DEVELOPER_ID_CERTIFICATE_P12_BASE64:-}" ||
  fail "signing payload certificate is missing"
test -n "${APPLE_DEVELOPER_ID_CERTIFICATE_PASSWORD:-}" ||
  fail "signing payload certificate password is missing"

verify_dir="$(mktemp -d)"
keychain="$verify_dir/release-signing.keychain-db"
keychain_password="$(uuidgen)"

cleanup() {
  security delete-keychain "$keychain" >/dev/null 2>&1 || true
  rm -rf "$verify_dir"
}
trap cleanup EXIT

certificate="$verify_dir/developer-id.p12"
certificate_pem="$verify_dir/developer-id.pem"
if ! printf '%s' "$APPLE_DEVELOPER_ID_CERTIFICATE_P12_BASE64" |
  base64 --decode >"$certificate"; then
  fail "signing payload certificate is not valid base64"
fi

security create-keychain -p "$keychain_password" "$keychain" ||
  fail "could not create temporary signing keychain"
security unlock-keychain -p "$keychain_password" "$keychain" ||
  fail "could not unlock temporary signing keychain"
security import "$certificate" \
  -P "$APPLE_DEVELOPER_ID_CERTIFICATE_PASSWORD" \
  -k "$keychain" \
  -T /usr/bin/codesign >/dev/null ||
  fail "could not import Developer ID certificate"
if ! security find-certificate -c 'Developer ID Application' -p "$keychain" \
  >"$certificate_pem"; then
  fail "Developer ID Application certificate was not found after import"
fi
security verify-cert -c "$certificate_pem" -p codeSign -q ||
  fail "Developer ID Application certificate is not valid for code signing"

subject="$(openssl x509 -in "$certificate_pem" -noout -subject -nameopt RFC2253)"
grep -q "OU=${EXPECTED_APPLE_TEAM_ID}" <<<"$subject" ||
  fail "Developer ID certificate Team ID does not match release policy"
