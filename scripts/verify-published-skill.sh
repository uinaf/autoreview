#!/usr/bin/env bash

set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
manifest=$repository_root/skills/autoreview/.tessl-plugin/plugin.json
requested_version=${1:-}

if [ -z "$requested_version" ]; then
  printf 'usage: %s VERSION\n' "${0##*/}" >&2
  exit 2
fi

for required_command in cat grep jq mktemp sleep tessl; do
  command -v "$required_command" >/dev/null 2>&1 || {
    printf 'required command not found: %s\n' "$required_command" >&2
    exit 1
  }
done

name=$(jq -er '.name | select(type == "string" and length > 0)' "$manifest")
version=$(jq -er '.version | select(test("^[0-9]+\\.[0-9]+\\.[0-9]+$"))' "$manifest")
install_attempts=${TESSL_VERIFY_INSTALL_ATTEMPTS:-30}
install_interval_seconds=${TESSL_VERIFY_INSTALL_INTERVAL_SECONDS:-10}
if ! [[ "$install_attempts" =~ ^[1-9][0-9]*$ ]] || \
    ! [[ "$install_interval_seconds" =~ ^[0-9]+$ ]]; then
  printf 'install retry settings must be positive attempts and non-negative seconds\n' >&2
  exit 1
fi
if [ "$requested_version" != "$version" ]; then
  printf 'requested version %s does not match manifest version %s\n' \
    "$requested_version" "$version" >&2
  exit 1
fi

verification_root=$(mktemp -d "${TMPDIR:-/tmp}/autoreview-skill-release.XXXXXX")
cleanup() {
  rm -rf -- "$verification_root"
}
trap cleanup EXIT

install_log=$verification_root/install.log
installed=false
for ((attempt = 1; attempt <= install_attempts; attempt += 1)); do
  if (
    cd "$verification_root"
    tessl install --yes --strict --agent codex "$name@$version"
  ) > "$install_log" 2>&1; then
    cat "$install_log"
    installed=true
    break
  fi

  if ! grep -F 'moderation did not pass' "$install_log" >/dev/null || \
      [ "$attempt" -eq "$install_attempts" ]; then
    cat "$install_log" >&2
    printf 'published skill did not become installable after %d attempt(s)\n' \
      "$attempt" >&2
    exit 1
  fi

  printf 'published skill is awaiting moderation; retrying install (%d/%d)\n' \
    "$attempt" "$install_attempts"
  sleep "$install_interval_seconds"
done

if [ "$installed" != true ]; then
  printf 'published skill installation did not complete\n' >&2
  exit 1
fi

skill_name=${name##*/}
codex_skill=$verification_root/.codex/skills/tessl__$skill_name/SKILL.md
if [ ! -s "$codex_skill" ] || ! grep -Fx "name: $skill_name" "$codex_skill" >/dev/null; then
  printf 'published skill is missing from the Codex discovery surface: %s\n' \
    "$codex_skill" >&2
  exit 1
fi

printf 'smoked published skill %s@%s through Codex discovery\n' "$name" "$version"
