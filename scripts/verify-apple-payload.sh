#!/usr/bin/env bash

set -euo pipefail

payload="${1:-}"
if [[ ! -f "$payload" ]]; then
  printf '%s\n' "encrypted Apple signing payload was not found" >&2
  exit 1
fi

jq -e '
  type == "object" and
  (.sops | type == "object") and
  ((keys - ["sops"]) | sort) == ([
    "APPLE_DEVELOPER_ID_CERTIFICATE_P12_BASE64",
    "APPLE_DEVELOPER_ID_CERTIFICATE_PASSWORD",
    "APPLE_NOTARY_API_ISSUER_ID",
    "APPLE_NOTARY_API_KEY_ID",
    "APPLE_NOTARY_API_KEY_P8_BASE64",
    "APPLE_TEAM_ID"
  ] | sort)
' "$payload" >/dev/null || {
  printf '%s\n' "encrypted Apple signing payload has unexpected keys" >&2
  exit 1
}
