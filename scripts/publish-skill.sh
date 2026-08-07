#!/usr/bin/env bash

set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
skill_directory=$repository_root/skills/autoreview
manifest=$skill_directory/.tessl-plugin/plugin.json
workspace=${TESSL_WORKSPACE:-uinaf}

for required_command in jq mise tessl; do
  command -v "$required_command" >/dev/null 2>&1 || {
    printf 'required command not found: %s\n' "$required_command" >&2
    exit 1
  }
done

name=$(jq -er '.name | select(type == "string" and length > 0)' "$manifest")
version=$(jq -er '.version | select(test("^[0-9]+\\.[0-9]+\\.[0-9]+$"))' "$manifest")
case "$name" in
  "$workspace"/*) ;;
  *)
    printf 'skill %s does not belong to Tessl workspace %s\n' "$name" "$workspace" >&2
    exit 1
    ;;
esac

if [ "${TESSL_SKIP_QUALITY:-false}" != true ]; then
  mise run skill:package
  mise run skill:review
fi
tessl plugin lint "$skill_directory"

registry=$(tessl search --json "$name")
if jq -e --arg name "$name" --arg version "$version" '
  any(.results[]?;
    .type == "tile" and
    .fullName == $name and
    any(.versions[]?; . == $version)
  )
' >/dev/null <<<"$registry"; then
  printf '%s@%s already exists; verifying immutable content\n' "$name" "$version"
else
  tessl plugin publish --workspace "$workspace" "$skill_directory"
fi

exec "$repository_root/scripts/verify-published-skill.sh" "$version"
