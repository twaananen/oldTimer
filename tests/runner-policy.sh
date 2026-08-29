#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
system_root="$repo_root/build_files/system_files"
unit_root="$system_root/usr/lib/systemd/system"
runner_unit="$unit_root/aeons-runnerd.service"
runner_slice="$unit_root/aeons-ci.slice"
firewall="$system_root/usr/lib/aeons-ci/network.nft"

for setting in \
  'User=aeons-ci' \
  'Delegate=yes' \
  'KillMode=control-group' \
  'Requires=aeons-ci-firewall.service aeons-runner-image.service'; do
  grep -qxF "$setting" "$runner_unit"
done

for setting in 'CPUQuota=800%' 'MemoryHigh=32G' 'MemoryMax=40G'; do
  grep -qxF "$setting" "$runner_slice"
done

for forbidden in \
  'NoNewPrivileges=yes' \
  'RestrictSUIDSGID=yes' \
  'ProtectControlGroups=yes' \
  'PrivateDevices=yes'; do
  if grep -qxF "$forbidden" "$runner_unit"; then
    echo "runnerd unit blocks rootless Podman: $forbidden" >&2
    exit 1
  fi
done

grep -qF 'meta skuid "aeons-ci" fib daddr type local counter reject' "$firewall"
for network in 10.0.0.0/8 100.64.0.0/10 127.0.0.0/8 169.254.0.0/16 172.16.0.0/12 192.168.0.0/16 224.0.0.0/4 240.0.0.0/4; do
  grep -qF "$network" "$firewall"
done
grep -qF 'meta skuid "aeons-ci" meta nfproto ipv6 counter reject' "$firewall"

grep -qF 'LoadCredentialEncrypted=github-app-key' "$runner_unit"
grep -qF 'BindReadOnlyPaths=/usr/lib/aeons-ci/resolv.conf:/etc/resolv.conf' "$runner_unit"
grep -qF 'nameserver 1.1.1.1' "$system_root/usr/lib/aeons-ci/resolv.conf"
grep -qF 'nameserver 9.9.9.9' "$system_root/usr/lib/aeons-ci/resolv.conf"

bash -n "$system_root/usr/libexec/aeons-runner-image-ensure"
grep -qE '^FROM ghcr.io/actions/actions-runner:[^ ]+@sha256:[0-9a-f]{64}$' \
  "$repo_root/runner-image/Containerfile"
grep -qF 'COPY entrypoint.sh /opt/actions-runner/entrypoint.sh' "$repo_root/runner-image/Containerfile"
grep -qF 'RUN systemd-analyze verify' "$repo_root/Containerfile"

if grep -qE 'systemctl enable (aeons-runnerd|aeons-ci-firewall|aeons-runner-image)' "$repo_root/build_files/build.sh"; then
  echo "runner services must remain disabled until live acceptance passes" >&2
  exit 1
fi
