#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
system_root="$repo_root/build_files/system_files"
unit_root="$system_root/usr/lib/systemd/system"
runner_unit="$unit_root/aeons-runnerd.service"
runner_slice="$unit_root/aeons-ci.slice"
image_unit="$unit_root/aeons-runner-image.service"
firewall="$system_root/usr/lib/aeons-ci/network.nft"
runner_env="$system_root/usr/lib/aeons-ci/runner.env"
acceptance="$system_root/usr/libexec/aeons-runner-host-acceptance"
acceptance_worker="$system_root/usr/libexec/aeons-runner-host-acceptance-worker"

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
grep -qF 'EnvironmentFile=/usr/lib/aeons-ci/runner.env' "$runner_unit"
grep -qF 'EnvironmentFile=/usr/lib/aeons-ci/runner.env' "$image_unit"
grep -qxF 'AEONS_RUNNERD_IMAGE_TAG=localhost/aeons-actions-runner:oldtimer' "$runner_env"
grep -qF 'nameserver 1.1.1.1' "$system_root/usr/lib/aeons-ci/resolv.conf"
grep -qF 'nameserver 9.9.9.9' "$system_root/usr/lib/aeons-ci/resolv.conf"

bash -n "$system_root/usr/libexec/aeons-runner-image-ensure"
bash -n "$acceptance"
bash -n "$acceptance_worker"
grep -qF 'systemctl is-active --quiet aeons-runnerd.service' "$acceptance"
for property in \
  '--property=User=aeons-ci' \
  '--property=Group=aeons-ci' \
  '--property=Slice=aeons-ci.slice' \
  '--property=Delegate=yes'; do
  grep -qF -- "$property" "$acceptance"
done
for flag in \
  '--userns=auto:size=8192' \
  '--network=pasta:--ipv4-only,--no-map-gw' \
  '--read-only' \
  '--cap-drop=all' \
  '--security-opt=no-new-privileges'; do
  grep -qF -- "$flag" "$acceptance_worker"
done
grep -qF 'for suffix in 000000000001 000000000002 000000000003 000000000004' \
  "$acceptance_worker"
grep -qF 'expected_cgroup=/aeons-ci.slice/aeons-runner-host-acceptance-worker.service' \
  "$acceptance_worker"
grep -qF 'before_ids=' "$repo_root/README.md"
grep -qF 'grep -qxF "$run_id" <<<"$before_ids"' "$repo_root/README.md"
grep -qF 'assert_runner_cleanup()' "$repo_root/README.md"
if (( $(grep -c '^assert_runner_cleanup$' "$repo_root/README.md") < 7 )); then
  echo "every canary and real-workflow stage must prove exact cleanup" >&2
  exit 1
fi
timeout_boundary_check='test "$(gh run view "$run_id" --repo "$repo" --json jobs --jq '\''[.jobs[].steps[] | select(.name == "Verify qualified toolchain and container boundary") | .conclusion] | unique | .[]'\'')" = success'
grep -qF "$timeout_boundary_check" "$repo_root/README.md"

diagnostics_dispatch_line=$(grep -nF 'diagnostics_id=$(dispatch_run gd-diagnostics.yml -f runner="$runner")' "$repo_root/README.md" | cut -d: -f1)
client_dispatch_line=$(grep -nF 'client_id=$(dispatch_run client-gdunit.yml -f runner="$runner")' "$repo_root/README.md" | cut -d: -f1)
cleanup_between=$(sed -n "$((diagnostics_dispatch_line + 1)),$((client_dispatch_line - 1))p" "$repo_root/README.md" | grep -c '^assert_runner_cleanup$')
if (( diagnostics_dispatch_line >= client_dispatch_line || cleanup_between != 1 )); then
  echo "qualified workflows must run and clean up sequentially" >&2
  exit 1
fi
grep -qF ': "${AEONS_RUNNERD_IMAGE_TAG:?}"' \
  "$system_root/usr/libexec/aeons-runner-image-ensure"
if grep -qF 'podman image exists' "$system_root/usr/libexec/aeons-runner-image-ensure"; then
  echo "runner image service must rebuild the embedded recipe" >&2
  exit 1
fi
grep -qE '^FROM ghcr.io/actions/actions-runner:[^ ]+@sha256:[0-9a-f]{64}$' \
  "$repo_root/runner-image/Containerfile"
grep -qF 'COPY entrypoint.sh /opt/actions-runner/entrypoint.sh' "$repo_root/runner-image/Containerfile"
grep -qF 'ln -s "/opt/actions-runner/runtime/$directory"' \
  "$repo_root/runner-image/entrypoint.sh"
if grep -qF 'runtime/. /runner/' "$repo_root/runner-image/entrypoint.sh"; then
  echo "runner entrypoint must not copy the immutable payload per job" >&2
  exit 1
fi
grep -qF 'RUN systemd-analyze verify' "$repo_root/Containerfile"

if grep -qE 'systemctl enable (aeons-runnerd|aeons-ci-firewall|aeons-runner-image)' "$repo_root/build_files/build.sh"; then
  echo "runner services must remain disabled until live acceptance passes" >&2
  exit 1
fi
