#!/usr/bin/env bash

set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
publisher=$repository_root/scripts/publish-skill.sh
skill_directory=$repository_root/skills/autoreview
workflow=$repository_root/.github/workflows/publish-skill.yml
scratch=$(mktemp -d "${TMPDIR:-/tmp}/autoreview-skill-release-test.XXXXXX")
trap 'rm -rf -- "$scratch"' EXIT

grep -F 'name: skill-release' "$workflow" >/dev/null
grep -F "token: \${{ secrets.TESSL_TOKEN }}" "$workflow" >/dev/null
grep -F 'version: "0.94.0"' "$workflow" >/dev/null
grep -F 'tesslio/setup-tessl@33e1c9253e3673f28e1b8949475b250dbd57918e' \
  "$workflow" >/dev/null
grep -F 'persist-credentials: false' "$workflow" >/dev/null
grep -F "if: \${{ github.ref == 'refs/heads/main' }}" "$workflow" >/dev/null
if grep -F 'id-token: write' "$workflow" >/dev/null || \
    grep -F '  pull_request:' "$workflow" >/dev/null; then
  printf 'skill publication workflow broadens secret or event access\n' >&2
  exit 1
fi

fake_bin=$scratch/bin
fake_log=$scratch/tessl.log
mkdir -p "$fake_bin"
: > "$fake_log"
cat > "$fake_bin/tessl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "$*" >> "$FAKE_TESSL_LOG"
case "$1" in
  search)
    if [ "${FAKE_TESSL_EXISTS:-false}" = true ]; then
      printf '{"results":[{"type":"tile","fullName":"uinaf/autoreview","versions":["2.1.0"]}]}\n'
    else
      printf '{"results":[]}\n'
    fi
    ;;
  plugin)
    case "$2" in
      lint|publish) ;;
      *) exit 2 ;;
    esac
    ;;
  install)
    installed=.tessl/plugins/uinaf/autoreview
    mkdir -p "$installed/.tessl-plugin" "$installed/references" \
      .agents/skills .codex/skills
    cp "$FAKE_TESSL_SOURCE/.tessl-plugin/plugin.json" \
      "$installed/.tessl-plugin/plugin.json"
    cp "$FAKE_TESSL_SOURCE/SKILL.md" "$installed/SKILL.md"
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
    ;;
  *) exit 2 ;;
esac
EOF
chmod 755 "$fake_bin/tessl"

run_publisher() {
  PATH="$fake_bin:$PATH" \
    TESSL_SKIP_QUALITY=true \
    FAKE_TESSL_LOG="$fake_log" \
    FAKE_TESSL_SOURCE="$1" \
    FAKE_TESSL_EXISTS="$2" \
    "$publisher"
}

run_publisher "$skill_directory" false >/dev/null
grep -F 'plugin publish --workspace uinaf' "$fake_log" >/dev/null

: > "$fake_log"
run_publisher "$skill_directory" true >/dev/null
if grep -F 'plugin publish' "$fake_log" >/dev/null; then
  printf 'existing skill version was republished\n' >&2
  exit 1
fi

stale_skill=$scratch/stale-skill
mkdir -p "$stale_skill"
cp -R "$skill_directory/." "$stale_skill/"
printf '\nstale registry content\n' >> "$stale_skill/SKILL.md"
: > "$fake_log"
if run_publisher "$stale_skill" true > "$scratch/stale.out" 2>&1; then
  printf 'stale published content passed verification\n' >&2
  exit 1
fi
grep -F 'published content differs: SKILL.md' "$scratch/stale.out" >/dev/null
if grep -F 'plugin publish' "$fake_log" >/dev/null; then
  printf 'stale existing version was mutated\n' >&2
  exit 1
fi

printf 'skill release tests passed\n'
