#!/usr/bin/env bash

set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
installer=$repository_root/install.sh
scratch=$(mktemp -d "${TMPDIR:-/tmp}/autoreview-installer-test.XXXXXX")
trap 'rm -rf "$scratch"' EXIT

grep -F 'name_template: "{{ .ProjectName }}_{{ .Tag }}_{{ .Os }}_{{ .Arch }}"' \
  "$repository_root/.goreleaser.yaml" >/dev/null
grep -F 'name_template: checksums.txt' "$repository_root/.goreleaser.yaml" >/dev/null
grep -F "archive=autoreview_\${release_tag}_linux_\${architecture}.tar.gz" \
  "$installer" >/dev/null

fixtures=$scratch/fixtures
fake_bin=$scratch/bin
fixture_sha256sum=$(command -v sha256sum || true)
fixture_shasum=$(command -v shasum || true)
mkdir -p "$fixtures" "$fake_bin"

make_archive() {
  local architecture=$1
  local payload=$scratch/payload-$architecture
  mkdir -p "$payload"
  printf '#!/bin/sh\nprintf '\''autoreview v1.2.3 (fixture-%s)\\n'\''\n' \
    "$architecture" > "$payload/autoreview"
  chmod 755 "$payload/autoreview"
  printf 'fixture license\n' > "$payload/LICENSE"
  printf 'fixture readme\n' > "$payload/README.md"
  tar -czf "$fixtures/autoreview_v1.2.3_linux_${architecture}.tar.gz" \
    -C "$payload" autoreview LICENSE README.md
}

make_archive amd64
make_archive arm64
(
  cd "$fixtures"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum autoreview_v1.2.3_linux_amd64.tar.gz \
      autoreview_v1.2.3_linux_arm64.tar.gz > checksums.txt
  else
    shasum -a 256 autoreview_v1.2.3_linux_amd64.tar.gz \
      autoreview_v1.2.3_linux_arm64.tar.gz > checksums.txt
  fi
)

cat > "$fake_bin/uname" <<'EOF'
#!/bin/sh
case "$1" in
  -s) printf '%s\n' "${TEST_UNAME_S:-Linux}" ;;
  -m) printf '%s\n' "${TEST_UNAME_M:-x86_64}" ;;
  *) exit 2 ;;
esac
EOF

cat > "$fake_bin/sha256sum" <<'EOF'
#!/bin/sh
if [ -n "${FIXTURE_SHA256SUM:-}" ]; then
  exec "$FIXTURE_SHA256SUM" "$@"
fi
exec "$FIXTURE_SHASUM" -a 256 "$@"
EOF

cat > "$fake_bin/curl" <<'EOF'
#!/bin/sh
set -eu

output=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      output=$2
      shift 2
      ;;
    -w|--proto)
      shift 2
      ;;
    --tlsv1.2|-fsSL)
      shift
      ;;
    *)
      url=$1
      shift
      ;;
  esac
done

case "$url" in
  */releases/latest)
    if [ "${FAKE_CURL_MODE:-}" = malformed-latest ]; then
      printf '%s' 'https://github.com/uinaf/autoreview/issues/61'
    else
      printf '%s' 'https://fixture.invalid/releases/tag/v1.2.3'
    fi
    ;;
  *)
    asset=${url##*/}
    printf '%s\n' "$url" >> "$FAKE_CURL_LOG"
    case "${FAKE_CURL_MODE:-}" in
      fail-download) exit 22 ;;
      interrupt)
        kill -TERM "$PPID"
        exit 143
        ;;
    esac
    cp "$FIXTURE_DIR/$asset" "$output"
    ;;
esac
EOF

chmod 755 "$fake_bin/uname" "$fake_bin/sha256sum" "$fake_bin/curl"

case_number=0
new_case() {
  case_number=$((case_number + 1))
  case_root=$scratch/case-$case_number
  case_fixtures=$case_root/fixtures
  case_home=$case_root/home
  case_tmp=$case_root/tmp
  case_log=$case_root/curl.log
  mkdir -p "$case_fixtures" "$case_home" "$case_tmp"
  cp "$fixtures"/* "$case_fixtures/"
  : > "$case_log"
}

run_installer() {
  PATH="$fake_bin:$PATH" \
    HOME="$case_home" \
    TMPDIR="$case_tmp" \
    FIXTURE_DIR="$case_fixtures" \
    FIXTURE_SHA256SUM="$fixture_sha256sum" \
    FIXTURE_SHASUM="$fixture_shasum" \
    FAKE_CURL_LOG="$case_log" \
    AUTOREVIEW_INSTALL_REPOSITORY_URL="${TEST_REPOSITORY_URL:-https://fixture.invalid}" \
    TEST_UNAME_S="${TEST_UNAME_S:-Linux}" \
    TEST_UNAME_M="${TEST_UNAME_M:-x86_64}" \
    FAKE_CURL_MODE="${FAKE_CURL_MODE:-}" \
    /bin/sh "$installer" "$@"
}

run_installer_without_home() {
  env -u HOME \
    PATH="$fake_bin:$PATH" \
    TMPDIR="$case_tmp" \
    FIXTURE_DIR="$case_fixtures" \
    FIXTURE_SHA256SUM="$fixture_sha256sum" \
    FIXTURE_SHASUM="$fixture_shasum" \
    FAKE_CURL_LOG="$case_log" \
    AUTOREVIEW_INSTALL_REPOSITORY_URL="${TEST_REPOSITORY_URL:-https://fixture.invalid}" \
    TEST_UNAME_S="${TEST_UNAME_S:-Linux}" \
    TEST_UNAME_M="${TEST_UNAME_M:-x86_64}" \
    FAKE_CURL_MODE="${FAKE_CURL_MODE:-}" \
    /bin/sh "$installer" "$@"
}

expect_failure() {
  local expected=$1
  shift
  local output
  if output=$("$@" 2>&1); then
    printf 'expected command to fail: %s\n' "$*" >&2
    exit 1
  fi
  case "$output" in
    *"$expected"*) ;;
    *)
      printf 'expected failure containing %q, got:\n%s\n' "$expected" "$output" >&2
      exit 1
      ;;
  esac
}

assert_no_residue() {
  if find "$case_tmp" "$case_home" \
      \( -name 'autoreview-install.*' -o -name '.autoreview.*' \) \
      -print -quit | grep -q .; then
    printf 'installer left temporary files behind\n' >&2
    find "$case_tmp" "$case_home" \
      \( -name 'autoreview-install.*' -o -name '.autoreview.*' \) >&2
    exit 1
  fi
}

env -u HOME /bin/sh "$installer" --help >/dev/null

new_case
destination_without_home=$case_root/no-home-bin
run_installer_without_home --dest "$destination_without_home" >/dev/null
test "$("$destination_without_home/autoreview" --version)" = \
  'autoreview v1.2.3 (fixture-amd64)'
assert_no_residue

new_case
run_installer >/dev/null
test "$("$case_home/.local/bin/autoreview" --version)" = \
  'autoreview v1.2.3 (fixture-amd64)'
grep -Fx \
  'https://fixture.invalid/releases/download/v1.2.3/autoreview_v1.2.3_linux_amd64.tar.gz' \
  "$case_log" >/dev/null
grep -Fx \
  'https://fixture.invalid/releases/download/v1.2.3/checksums.txt' \
  "$case_log" >/dev/null
assert_no_residue

new_case
TEST_REPOSITORY_URL=https://fixture.invalid/ run_installer >/dev/null
grep -Fx \
  'https://fixture.invalid/releases/download/v1.2.3/autoreview_v1.2.3_linux_amd64.tar.gz' \
  "$case_log" >/dev/null
assert_no_residue

new_case
custom_destination=$case_root/custom-bin-with-space/'chosen bin'
TEST_UNAME_M=aarch64 run_installer --version 1.2.3 \
  --dest "$custom_destination" >/dev/null
test "$("$custom_destination/autoreview" --version)" = \
  'autoreview v1.2.3 (fixture-arm64)'
grep -Fx \
  'https://fixture.invalid/releases/download/v1.2.3/autoreview_v1.2.3_linux_arm64.tar.gz' \
  "$case_log" >/dev/null
assert_no_residue

new_case
TEST_UNAME_S=Darwin expect_failure 'unsupported operating system: Darwin' \
  run_installer
assert_no_residue

new_case
TEST_UNAME_M=riscv64 expect_failure 'unsupported architecture: riscv64' \
  run_installer
assert_no_residue

new_case
FAKE_CURL_MODE=malformed-latest expect_failure \
  'latest release returned malformed metadata' run_installer
assert_no_residue

new_case
expect_failure 'invalid release version: vnightly' \
  run_installer --version nightly
assert_no_residue

new_case
awk '$2 != "autoreview_v1.2.3_linux_amd64.tar.gz"' \
  "$case_fixtures/checksums.txt" > "$case_fixtures/checksums.new"
mv "$case_fixtures/checksums.new" "$case_fixtures/checksums.txt"
expect_failure 'checksums.txt must contain exactly one entry' run_installer
assert_no_residue

new_case
awk '{ if ($2 == "autoreview_v1.2.3_linux_amd64.tar.gz") $1 = sprintf("%064d", 0); print }' \
  "$case_fixtures/checksums.txt" > "$case_fixtures/checksums.new"
mv "$case_fixtures/checksums.new" "$case_fixtures/checksums.txt"
expect_failure 'checksum mismatch' run_installer
assert_no_residue

new_case
mkdir -p "$case_home/.local/bin"
printf '#!/bin/sh\nprintf '\''old autoreview\\n'\''\n' > \
  "$case_home/.local/bin/autoreview"
chmod 755 "$case_home/.local/bin/autoreview"
FAKE_CURL_MODE=fail-download expect_failure 'failed to download' run_installer
test "$("$case_home/.local/bin/autoreview")" = 'old autoreview'
assert_no_residue

new_case
mkdir -p "$case_home/.local/bin/autoreview"
expect_failure 'destination path is a directory' run_installer
test -d "$case_home/.local/bin/autoreview"
assert_no_residue

new_case
mkdir -p "$case_home/.local/bin"
printf '#!/bin/sh\nprintf '\''old autoreview\\n'\''\n' > \
  "$case_home/.local/bin/autoreview"
chmod 755 "$case_home/.local/bin/autoreview"
if FAKE_CURL_MODE=interrupt run_installer >/dev/null 2>&1; then
  printf 'expected interrupted installer to fail\n' >&2
  exit 1
else
  interrupt_status=$?
fi
test "$interrupt_status" -eq 143
test "$("$case_home/.local/bin/autoreview")" = 'old autoreview'
assert_no_residue

printf 'installer tests passed\n'
