#!/usr/bin/env bash
set -euo pipefail
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
system_root="$repo_root/build_files/system_files"
unit_root="$system_root/usr/lib/systemd/system"
host_setup_unit="$unit_root/aeons-ci-host-setup.service"
host_setup="$system_root/usr/libexec/aeons-ci-host-setup"
shadow_lock="$system_root/usr/libexec/aeons-shadow-lock"
host_isolation="$system_root/usr/lib/aeons-ci/host-isolation.sh"
firewall="$system_root/usr/lib/aeons-ci/network.nft"

# The OS supplies bootstrap only. Runtime releases belong to Aeons.
for retired in runnerd runner-image \
  build_files/system_files/usr/lib/systemd/system/aeons-runnerd.service \
  build_files/system_files/usr/lib/systemd/system/aeons-runner-image.service \
  build_files/system_files/usr/lib/aeons-ci/runner.env \
  build_files/system_files/usr/libexec/aeons-runner-image-ensure \
  build_files/system_files/usr/libexec/aeons-runner-host-acceptance \
  build_files/system_files/usr/libexec/aeons-runner-host-acceptance-worker \
  build_files/system_files/usr/libexec/aeons-cgroup-supervisor-launch; do
  if [[ -e "$repo_root/$retired" ]]; then
    echo "OS still owns retired runner runtime: $retired" >&2
    exit 1
  fi
done
for setting in 'CPUQuota=1200%' 'MemoryHigh=48G' 'MemoryMax=64G' 'TasksMax=27000'; do
  grep -qxF "$setting" "$unit_root/aeons-ci.slice"
done
grep -qxF 'Requires=aeons-ci-host-setup.service' "$unit_root/aeons-ci-firewall.service"
grep -qxF 'cgroup_manager = "cgroupfs"' "$system_root/usr/lib/aeons-ci/containers.conf"
for resolver in 1.1.1.1 9.9.9.9; do
  grep -qxF "nameserver $resolver" "$system_root/usr/lib/aeons-ci/resolv.conf"
done
bash -n "$host_setup" "$host_isolation"
python3 -c 'import ast, pathlib, sys; ast.parse(pathlib.Path(sys.argv[1]).read_text())' "$shadow_lock"
grep -qxF 'ProtectSystem=strict' "$host_setup_unit"
grep -qxF 'ReadWritePaths=/etc /run/lock' "$host_setup_unit"
for command in \
  'cp --attributes-only --preserve=all -- "$file" "$temporary"' \
  'sync -f "$temporary"' \
  'mv -fT -- "$temporary" "$file"'; do
  grep -qxF "    $command" "$host_setup"
done
grep -qxF 'shadow_lock_helper=${AEONS_SHADOW_LOCK_HELPER:-/usr/libexec/aeons-shadow-lock}' "$host_setup"
grep -qxF '    exec "$shadow_lock_helper" "$0" "$@"' "$host_setup"
grep -qF 'libc.lckpwdf()' "$shadow_lock"

grep -qF 'meta skuid "aeons-ci" fib daddr type local counter reject' "$firewall"
for network in 10.0.0.0/8 100.64.0.0/10 127.0.0.0/8 169.254.0.0/16 172.16.0.0/12 192.168.0.0/16 224.0.0.0/4 240.0.0.0/4; do
  grep -qF "$network" "$firewall"
done
grep -qF 'meta skuid "aeons-ci" meta nfproto ipv6 counter reject' "$firewall"
for marker in \
  aeons-ci-probe-host-local \
  aeons-ci-probe-lan-gateway \
  aeons-ci-probe-docker-bridge \
  aeons-ci-probe-carrier-grade \
  aeons-ci-probe-link-local \
  aeons-ci-probe-ipv6; do
  grep -qF "comment \"$marker\"" "$firewall"
done
subid_test_root=$(mktemp -d)
trap 'rm -rf -- "$subid_test_root"' EXIT
subuid_file="$subid_test_root/subuid"
subgid_file="$subid_test_root/subgid"
lock_file="$subid_test_root/lock"
export AEONS_SHADOW_LOCK_HELPER="$shadow_lock"
export AEONS_PWD_LOCK_FILE="$subid_test_root/pwd.lock"
printf 'tommi:524288:65536\n' >"$subuid_file"
printf 'tommi:524288:65536\n' >"$subgid_file"
for attempt in 1 2; do
  AEONS_SUBUID_FILE="$subuid_file" \
  AEONS_SUBGID_FILE="$subgid_file" \
  AEONS_SUBID_LOCK_FILE="$lock_file" \
    "$host_setup"
done
test "$(grep -cxF 'aeons-ci:589824:98304' "$subuid_file")" = 1
test "$(grep -cxF 'aeons-ci:589824:98304' "$subgid_file")" = 1

printf 'tommi:524288:65536\naeons-ci:589824:65536\n' >"$subuid_file"
printf 'tommi:524288:65536\naeons-ci:589824:65536\n' >"$subgid_file"
chmod 0640 "$subuid_file"
chmod 0600 "$subgid_file"
subuid_inode=$(stat -c %i "$subuid_file")
subgid_inode=$(stat -c %i "$subgid_file")
AEONS_SUBUID_FILE="$subuid_file" \
AEONS_SUBGID_FILE="$subgid_file" \
AEONS_SUBID_LOCK_FILE="$lock_file" \
  "$host_setup"
test "$(grep -cxF 'aeons-ci:589824:98304' "$subuid_file")" = 1
test "$(grep -cxF 'aeons-ci:589824:98304' "$subgid_file")" = 1
test "$(stat -c %i "$subuid_file")" != "$subuid_inode"
test "$(stat -c %i "$subgid_file")" != "$subgid_inode"
test "$(stat -c %a "$subuid_file")" = 640
test "$(stat -c %a "$subgid_file")" = 600

printf 'tommi:524288:65536\naeons-ci:589824:65536\n' >"$subuid_file"
printf 'tommi:524288:65536\naeons-ci:589824:65536\n' >"$subgid_file"
pwd_lock=$AEONS_PWD_LOCK_FILE
lock_ready="$subid_test_root/lock-ready"
lock_release="$subid_test_root/lock-release"
timeout 5 python3 - "$pwd_lock" "$lock_ready" "$lock_release" <<'PY' &
import fcntl
import os
from pathlib import Path
import sys
import time

fd = os.open(sys.argv[1], os.O_WRONLY | os.O_CREAT, 0o600)
fcntl.lockf(fd, fcntl.LOCK_EX)
Path(sys.argv[2]).touch()
while not Path(sys.argv[3]).exists():
    time.sleep(0.01)
PY
lock_holder_pid=$!
while [[ ! -e $lock_ready ]]; do sleep 0.01; done
AEONS_SUBUID_FILE="$subuid_file" \
AEONS_SUBGID_FILE="$subgid_file" \
AEONS_SUBID_LOCK_FILE="$lock_file" \
AEONS_PWD_LOCK_FILE="$pwd_lock" \
AEONS_SHADOW_LOCK_HELPER="$shadow_lock" \
  "$host_setup" &
host_setup_pid=$!
sleep 0.2
kill -0 "$host_setup_pid"
grep -qxF 'aeons-ci:589824:65536' "$subuid_file"
touch "$lock_release"
wait "$lock_holder_pid"
wait "$host_setup_pid"
test "$(grep -cxF 'aeons-ci:589824:98304' "$subuid_file")" = 1
test "$(grep -cxF 'aeons-ci:589824:98304' "$subgid_file")" = 1

printf 'other:600000:1024\n' >"$subuid_file"
printf 'other:600000:1024\n' >"$subgid_file"
if AEONS_SUBUID_FILE="$subuid_file" \
  AEONS_SUBGID_FILE="$subgid_file" \
  AEONS_SUBID_LOCK_FILE="$lock_file" \
    "$host_setup" 2>/dev/null; then
  echo "host setup accepted an overlapping subordinate-ID range" >&2
  exit 1
fi
test "$(wc -l <"$subuid_file")" = 1
test "$(wc -l <"$subgid_file")" = 1

printf 'aeons-ci:589824:65536\nother:670000:1024\n' >"$subuid_file"
printf 'aeons-ci:589824:65536\nother:670000:1024\n' >"$subgid_file"
if AEONS_SUBUID_FILE="$subuid_file" \
  AEONS_SUBGID_FILE="$subgid_file" \
  AEONS_SUBID_LOCK_FILE="$lock_file" \
    "$host_setup" 2>/dev/null; then
  echo "host setup accepted an overlap beside the expected allocation" >&2
  exit 1
fi

uid_map_file="$subid_test_root/uid_map"
printf '0 589824 8192\n' >"$uid_map_file"
source "$host_isolation"
payload_parent=/aeons.slice/aeons-ci.slice/aeons-runnerd.service
payload_id=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
aeons_is_direct_runner_cgroup "$payload_parent" "$payload_parent/libpod-$payload_id"
for invalid_cgroup in \
  "$payload_parent/libpod-$payload_id/runtime" \
  "$payload_parent/runtime/libpod-$payload_id" \
  "$payload_parent/libpod-short"; do
  if aeons_is_direct_runner_cgroup "$payload_parent" "$invalid_cgroup"; then
    echo "runner cgroup predicate accepted an invalid topology: $invalid_cgroup" >&2
    exit 1
  fi
done
if aeons_uid_map_contains 1000 "$uid_map_file"; then
  echo "host UID predicate rejected a subordinate-only mapping" >&2
  exit 1
fi
printf '0 1000 8192\n' >"$uid_map_file"
aeons_uid_map_contains 1000 "$uid_map_file"
