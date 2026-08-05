#!/usr/bin/env bash

set -euo pipefail

fail() {
  printf '%s\n' "$1" >&2
  exit 1
}

expected_team_id="${1:-}"

[[ "$expected_team_id" =~ ^[A-Z0-9]{10}$ ]] ||
  fail "expected Apple Team ID is missing or invalid"
test -n "${APPLE_DEVELOPER_ID_CERTIFICATE_P12_BASE64:-}" ||
  fail "Developer ID certificate is missing"
test -n "${APPLE_DEVELOPER_ID_CERTIFICATE_PASSWORD:-}" ||
  fail "Developer ID certificate password is missing"
[[ "${APPLE_NOTARY_API_ISSUER_ID:-}" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]] ||
  fail "Apple notarization issuer ID is missing or invalid"
[[ "${APPLE_NOTARY_API_KEY_ID:-}" =~ ^[A-Z0-9]{10}$ ]] ||
  fail "Apple notarization key ID is missing or invalid"
test -n "${APPLE_NOTARY_API_KEY_P8_BASE64:-}" ||
  fail "Apple notarization private key is missing"

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
notary_key="$verify_dir/notary-api-key.p8"
if ! printf '%s' "$APPLE_DEVELOPER_ID_CERTIFICATE_P12_BASE64" |
  /usr/bin/base64 -D >"$certificate"; then
  fail "Developer ID certificate is not valid base64"
fi
if ! printf '%s' "$APPLE_NOTARY_API_KEY_P8_BASE64" |
  /usr/bin/base64 -D >"$notary_key"; then
  fail "Apple notarization private key is not valid base64"
fi
/usr/bin/openssl pkey -in "$notary_key" -noout >/dev/null 2>&1 ||
  fail "Apple notarization private key is invalid"

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

subject="$(/usr/bin/openssl x509 -in "$certificate_pem" -noout -subject -nameopt RFC2253)"
grep -q "OU=${expected_team_id}" <<<"$subject" ||
  fail "Developer ID certificate Team ID does not match release policy"
