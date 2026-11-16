#!/bin/bash
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT
export TEST_DIR
export PATH="$TEST_DIR:$PATH"

cat > "$TEST_DIR/curl" <<'EOF'
#!/bin/bash
printf '%s\n' '{"assets":[{"name":"mergerfs-2.42.0-1.fc44.x86_64.rpm","created_at":"2026-05-08","browser_download_url":"https://example.com/mergerfs.rpm"}]}'
exit "${CURL_STATUS:-0}"
EOF
cat > "$TEST_DIR/dnf5" <<'EOF'
#!/bin/bash
printf '%s\n' "$*" > "$TEST_DIR/install"
exit "${DNF_STATUS:-0}"
EOF
chmod +x "$TEST_DIR/curl" "$TEST_DIR/dnf5"

bash "$REPO/build_files/github-release-install.sh" trapexit/mergerfs 'fc44\.x86_64' > "$TEST_DIR/log" 2>&1
test "$(cat "$TEST_DIR/install")" = 'install -y https://example.com/mergerfs.rpm'
rm "$TEST_DIR/install"

if bash "$REPO/build_files/github-release-install.sh" trapexit/mergerfs 'fc45\.x86_64' > "$TEST_DIR/log" 2>&1; then
    echo 'FAIL: missing RPM was accepted' >&2
    exit 1
fi
grep -Fq 'ERROR: No RPM matching' "$TEST_DIR/log"
test ! -e "$TEST_DIR/install"

if CURL_STATUS=22 bash "$REPO/build_files/github-release-install.sh" trapexit/mergerfs 'fc44\.x86_64' > "$TEST_DIR/log" 2>&1; then
    echo 'FAIL: failed download was accepted' >&2
    exit 1
fi
test ! -e "$TEST_DIR/install"

if DNF_STATUS=1 bash "$REPO/build_files/github-release-install.sh" trapexit/mergerfs 'fc44\.x86_64' > "$TEST_DIR/log" 2>&1; then
    echo 'FAIL: failed installation was accepted' >&2
    exit 1
fi
echo 'PASS: RPM selection, missing RPM, download failure, installation failure'
