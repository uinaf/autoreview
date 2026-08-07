#!/usr/bin/env bash

set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
skill_directory=$repository_root/skills/autoreview
manifest=$skill_directory/.tessl-plugin/plugin.json
requested_version=${1:-}

if [ -z "$requested_version" ]; then
  printf 'usage: %s VERSION\n' "${0##*/}" >&2
  exit 2
fi

for required_command in cmp diff find jq mktemp sed sort tessl; do
  command -v "$required_command" >/dev/null 2>&1 || {
    printf 'required command not found: %s\n' "$required_command" >&2
    exit 1
  }
done

name=$(jq -er '.name | select(type == "string" and length > 0)' "$manifest")
version=$(jq -er '.version | select(test("^[0-9]+\\.[0-9]+\\.[0-9]+$"))' "$manifest")
description=$(jq -er '.description | select(type == "string" and length > 0)' "$manifest")
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

(
  cd "$verification_root"
  tessl install --yes --strict --agent codex "$name@$version"
)

installed_directory=$verification_root/.tessl/plugins/$name
if [ ! -d "$installed_directory" ]; then
  printf 'installed package directory not found: %s\n' "$installed_directory" >&2
  exit 1
fi

expected_files=$verification_root/expected-files.txt
actual_files=$verification_root/actual-files.txt
printf '%s\n' \
  '.tessl-plugin/plugin.json' \
  'SKILL.md' \
  'references/configuration.md' \
  'references/providers.md' \
  'references/results.md' \
  'references/security.md' > "$expected_files"
find "$installed_directory" -type f \
  ! -path "$installed_directory/tessl-package.json" \
  ! -path "$installed_directory/tile.json" \
  -print | sed "s#^$installed_directory/##" | LC_ALL=C sort > "$actual_files"
if ! diff -u "$expected_files" "$actual_files"; then
  printf 'published file set differs from the source package contract\n' >&2
  exit 1
fi

while IFS= read -r relative_path; do
  if ! cmp -s "$skill_directory/$relative_path" "$installed_directory/$relative_path"; then
    diff -u "$skill_directory/$relative_path" "$installed_directory/$relative_path" || true
    printf 'published content differs: %s\n' "$relative_path" >&2
    exit 1
  fi
done < "$expected_files"

jq -e --arg name "$name" --arg version "$version" '
  .name == $name and .version == $version
' "$installed_directory/tessl-package.json" >/dev/null
jq -e --arg name "$name" --arg version "$version" --arg summary "$description" '
  .name == $name and
  .version == $version and
  .summary == $summary and
  .private == false and
  .skills.autoreview.path == "SKILL.md"
' "$installed_directory/tile.json" >/dev/null

for agent_skill in \
  "$verification_root/.agents/skills/tessl__autoreview" \
  "$verification_root/.codex/skills/tessl__autoreview"; do
  if [ ! -f "$agent_skill/SKILL.md" ] || \
      ! cmp -s "$skill_directory/SKILL.md" "$agent_skill/SKILL.md"; then
    printf 'installed Codex discovery surface differs: %s\n' "$agent_skill" >&2
    exit 1
  fi
done

printf 'verified published skill %s@%s\n' "$name" "$version"
