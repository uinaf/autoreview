#!/usr/bin/env bash

set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
skill_directory=$repository_root/skills/autoreview
verifier=$repository_root/scripts/verify-published-skill.sh
workflow=$repository_root/.github/workflows/publish-skill.yml
scratch=$(mktemp -d "${TMPDIR:-/tmp}/autoreview-skill-release-test.XXXXXX")
trap 'rm -rf -- "$scratch"' EXIT

grep -F 'name: skill-release' "$workflow" >/dev/null
grep -F "token: \${{ secrets.TESSL_TOKEN }}" "$workflow" >/dev/null
grep -F 'version: "0.94.0"' "$workflow" >/dev/null
grep -F 'tesslio/setup-tessl@33e1c9253e3673f28e1b8949475b250dbd57918e' \
  "$workflow" >/dev/null
grep -F 'uinaf/tessl-publish-action@20580b9b8ecdb6d958ba583e37078541128aeb76' \
  "$workflow" >/dev/null
grep -F 'review-threshold: "100"' "$workflow" >/dev/null
grep -F 'publish-all: "true"' "$workflow" >/dev/null
grep -F 'version-strategy: manifest' "$workflow" >/dev/null
grep -F 'commit-version-bumps: "false"' "$workflow" >/dev/null
grep -F 'persist-credentials: false' "$workflow" >/dev/null
grep -F "if: \${{ github.ref == 'refs/heads/main' }}" "$workflow" >/dev/null
if grep -F 'id-token: write' "$workflow" >/dev/null || \
    grep -F 'contents: write' "$workflow" >/dev/null || \
    grep -F '  pull_request:' "$workflow" >/dev/null; then
  printf 'skill publication workflow broadens secret, write, or event access\n' >&2
  exit 1
fi

fake_bin=$scratch/bin
mkdir -p "$fake_bin"
cat > "$fake_bin/tessl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

if [ "$1" != install ]; then
  exit 2
fi

pending_failures=${FAKE_TESSL_PENDING_FAILURES:-0}
pending_count=0
if [ -n "${FAKE_TESSL_PENDING_FILE:-}" ] && [ -f "$FAKE_TESSL_PENDING_FILE" ]; then
  pending_count=$(cat "$FAKE_TESSL_PENDING_FILE")
fi
if [ "$pending_count" -lt "$pending_failures" ]; then
  printf '%d\n' "$((pending_count + 1))" > "$FAKE_TESSL_PENDING_FILE"
  printf '403 Forbidden\nTile version contents are hidden because moderation did not pass.\n' >&2
  exit 1
fi

installed=.tessl/plugins/uinaf/autoreview
mkdir -p "$installed/.tessl-plugin" "$installed/agents" "$installed/references" \
  .agents/skills .codex/skills
cp "$FAKE_TESSL_SOURCE/.tessl-plugin/plugin.json" \
  "$installed/.tessl-plugin/plugin.json"
cp "$FAKE_TESSL_SOURCE/SKILL.md" "$installed/SKILL.md"
cp "$FAKE_TESSL_SOURCE/agents/openai.yaml" "$installed/agents/openai.yaml"
cp "$FAKE_TESSL_SOURCE/references/"*.md "$installed/references/"
name=$(jq -r .name "$FAKE_TESSL_SOURCE/.tessl-plugin/plugin.json")
version=$(jq -r .version "$FAKE_TESSL_SOURCE/.tessl-plugin/plugin.json")
summary=$(jq -r .description "$FAKE_TESSL_SOURCE/.tessl-plugin/plugin.json")
jq -n --arg name "$name" --arg version "$version" \
  '{name: $name, version: $version}' > "$installed/tessl-package.json"
jq -n --arg name "$name" --arg version "$version" --arg summary "$summary" \
  '{name: $name, version: $version, summary: $summary, skills: {autoreview: {path: "SKILL.md"}}, private: false}' \
  > "$installed/tile.json"
ln -s ../../.tessl/plugins/uinaf/autoreview .agents/skills/tessl__autoreview
ln -s ../../.tessl/plugins/uinaf/autoreview .codex/skills/tessl__autoreview
EOF
chmod 755 "$fake_bin/tessl"

run_verifier() {
  PATH="$fake_bin:$PATH" \
    FAKE_TESSL_SOURCE="$1" \
    FAKE_TESSL_PENDING_FAILURES="${2:-0}" \
    FAKE_TESSL_PENDING_FILE="$scratch/pending-count" \
    TESSL_VERIFY_INSTALL_ATTEMPTS=3 \
    TESSL_VERIFY_INSTALL_INTERVAL_SECONDS=0 \
    "$verifier" 2.1.1
}

run_verifier "$skill_directory" >/dev/null
printf '0\n' > "$scratch/pending-count"
run_verifier "$skill_directory" 1 > "$scratch/pending.out"
grep -F 'awaiting moderation; retrying install (1/3)' "$scratch/pending.out" >/dev/null
test "$(cat "$scratch/pending-count")" = 1

assert_stale_rejected() {
  relative_path=$1
  stale_skill=$scratch/stale-${relative_path//\//-}
  mkdir -p "$stale_skill"
  cp -R "$skill_directory/." "$stale_skill/"
  printf '\nstale registry content\n' >> "$stale_skill/$relative_path"
  if run_verifier "$stale_skill" > "$scratch/stale.out" 2>&1; then
    printf 'stale published content passed verification: %s\n' "$relative_path" >&2
    exit 1
  fi
  grep -F "published content differs: $relative_path" "$scratch/stale.out" >/dev/null
}

assert_stale_rejected SKILL.md
assert_stale_rejected agents/openai.yaml

printf 'skill release tests passed\n'
