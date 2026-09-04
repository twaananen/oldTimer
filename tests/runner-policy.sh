#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
system_root="$repo_root/build_files/system_files"
unit_root="$system_root/usr/lib/systemd/system"
runner_unit="$unit_root/aeons-runnerd.service"
runner_slice="$unit_root/aeons-ci.slice"
image_unit="$unit_root/aeons-runner-image.service"
host_setup_unit="$unit_root/aeons-ci-host-setup.service"
firewall="$system_root/usr/lib/aeons-ci/network.nft"
runner_env="$system_root/usr/lib/aeons-ci/runner.env"
containers_conf="$system_root/usr/lib/aeons-ci/containers.conf"
acceptance="$system_root/usr/libexec/aeons-runner-host-acceptance"
acceptance_worker="$system_root/usr/libexec/aeons-runner-host-acceptance-worker"
cgroup_launcher="$system_root/usr/libexec/aeons-cgroup-supervisor-launch"
host_setup="$system_root/usr/libexec/aeons-ci-host-setup"
host_isolation="$system_root/usr/lib/aeons-ci/host-isolation.sh"

for setting in \
  'User=aeons-ci' \
  'Delegate=yes' \
  'KillMode=control-group' \
  'Requires=aeons-ci-firewall.service aeons-runner-image.service'; do
  grep -qxF "$setting" "$runner_unit"
done

# A network outage must not permanently trip systemd's start limiter. The
# daemon is unattended and may boot before the uplink is usable.
grep -qxF 'StartLimitIntervalSec=0' "$runner_unit"
grep -qxF 'Restart=on-failure' "$runner_unit"

for setting in 'CPUQuota=1200%' 'MemoryHigh=48G' 'MemoryMax=64G' 'TasksMax=27000'; do
  grep -qxF "$setting" "$runner_slice"
done
for setting in \
  'expected_slice_cpu_max="1200000 100000"' \
  'expected_slice_memory_high=51539607552' \
  'expected_slice_memory_max=68719476736' \
  'expected_slice_tasks_max=27000' \
  'runner_memory=6g' \
  'runner_memory_bytes=6442450944'; do
  grep -qxF "$setting" "$acceptance_worker"
done

for forbidden in \
  'NoNewPrivileges=yes' \
  'RestrictSUIDSGID=yes' \
  'ProtectControlGroups=yes' \
  'PrivateDevices=yes' \
  'ProtectKernelTunables=yes' \
  'ProtectKernelLogs=yes'; do
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
for marker in \
  aeons-ci-probe-host-local \
  aeons-ci-probe-lan-gateway \
  aeons-ci-probe-docker-bridge \
  aeons-ci-probe-carrier-grade \
  aeons-ci-probe-link-local \
  aeons-ci-probe-ipv6; do
  grep -qF "comment \"$marker\"" "$firewall"
done
grep -qF 'counter_before[$marker]=$(counter_value "$marker")' "$acceptance"
grep -qF 'counter_value()' "$acceptance"
grep -qF 'rule_is_reject()' "$acceptance"
grep -qF 'if ip -6 route get "$ipv6_target"' "$acceptance"
grep -qF 'SKIP: no host IPv6 route; live reject and container IPv4-only boundary proved' "$acceptance"
if grep -qF 'counter_total()' "$acceptance"; then
  echo "host acceptance must prove every documented reject independently" >&2
  exit 1
fi

grep -qF 'LoadCredentialEncrypted=github-app-key' "$runner_unit"
grep -qxF 'ExecStart=/usr/libexec/aeons-cgroup-supervisor-launch /usr/libexec/aeons-runnerd' "$runner_unit"
grep -qxF 'Requires=aeons-ci-host-setup.service' "$system_root/usr/lib/systemd/system/aeons-ci-firewall.service"
grep -qxF 'ExecStart=/usr/libexec/aeons-ci-host-setup' "$host_setup_unit"
grep -qF 'BindReadOnlyPaths=/usr/lib/aeons-ci/resolv.conf:/etc/resolv.conf' "$runner_unit"
for unit in "$runner_unit" "$image_unit"; do
  grep -qF 'EnvironmentFile=/usr/lib/aeons-ci/runner.env' "$unit"
  if grep -qF 'Environment=CONTAINERS_CONF=' "$unit"; then
    echo "runner units must take shared Podman configuration from runner.env" >&2
    exit 1
  fi
done
grep -qxF 'cgroup_manager = "cgroupfs"' "$containers_conf"
grep -qxF 'AEONS_RUNNERD_IMAGE_TAG=localhost/aeons-actions-runner:oldtimer' "$runner_env"
grep -qxF 'CONTAINERS_CONF=/usr/lib/aeons-ci/containers.conf' "$runner_env"
grep -qF 'nameserver 1.1.1.1' "$system_root/usr/lib/aeons-ci/resolv.conf"
grep -qF 'nameserver 9.9.9.9' "$system_root/usr/lib/aeons-ci/resolv.conf"

bash -n "$system_root/usr/libexec/aeons-runner-image-ensure"
bash -n "$host_setup"
bash -n "$acceptance"
bash -n "$acceptance_worker"
bash -n "$cgroup_launcher"
bash -n "$host_isolation"
grep -qF 'systemctl is-active --quiet aeons-runnerd.service' "$acceptance"
grep -qF 'containers_conf=${AEONS_CONTAINERS_CONF:-/usr/lib/aeons-ci/containers.conf}' "$acceptance"
grep -qF 'acceptance_worker=${AEONS_ACCEPTANCE_WORKER:-/usr/libexec/aeons-runner-host-acceptance-worker}' "$acceptance"
grep -qF 'cgroup_launcher=${AEONS_CGROUP_LAUNCHER:-/usr/libexec/aeons-cgroup-supervisor-launch}' "$acceptance"
if grep -qF -- '    --quiet' "$acceptance"; then
  echo "host acceptance must surface transient unit failures" >&2
  exit 1
fi
for property in \
  '--property=User=aeons-ci' \
  '--property=Group=aeons-ci' \
  '--property=Slice=aeons-ci.slice' \
  '--property=Delegate=yes' \
  '--property=StateDirectory=aeons-ci' \
  '--property=StateDirectoryMode=0700' \
  '--property=RuntimeDirectory=aeons-ci' \
  '--property=RuntimeDirectoryMode=0700' \
  '--property=PrivateTmp=yes' \
  '--property=ProtectSystem=strict' \
  '--property=ProtectKernelModules=yes' \
  '--property=LockPersonality=yes' \
  '--property=RestrictRealtime=yes' \
  '--setenv=CONTAINERS_CONF="$containers_conf"'; do
  grep -qF -- "$property" "$acceptance"
done
grep -qxF '    "$cgroup_launcher" "$acceptance_worker"' "$acceptance"
for flag in \
  '--userns=auto:size=8192' \
  '--cgroups=enabled' \
  '--cgroup-parent="$AEONS_CGROUP_PARENT"' \
  '--network=pasta:--ipv4-only,--no-map-gw' \
  '--read-only' \
  '--cap-drop=all' \
  '--security-opt=no-new-privileges'; do
  grep -qF -- "$flag" "$acceptance_worker"
done
grep -qF 'for suffix in 000000000001 000000000002 000000000003 000000000004' \
  "$acceptance_worker"
grep -qF 'slice_cgroup=$(systemctl show --property=ControlGroup --value aeons-ci.slice)' \
  "$acceptance_worker"
grep -qF 'expected_cgroup="$slice_cgroup/aeons-runner-host-acceptance-worker.service"' \
  "$acceptance_worker"
grep -qF 'expected_supervisor="$expected_cgroup/supervisor"' "$acceptance_worker"
grep -qF 'declare -A seen_container_cgroups=()' "$acceptance_worker"
grep -qF 'leftover_payload_cgroups=("$unit_root"/libpod-*)' "$acceptance_worker"
grep -qF 'aeons_uid_map_contains "$forbidden_uid" "/proc/$pid/uid_map"' \
  "$acceptance_worker"
grep -qF 'private_probe_addresses=(' "$acceptance_worker"
test "$(grep -cF 'private_probe_addresses=(' "$acceptance_worker")" = 1
grep -qF 'host_probe_pids+=("$!")' "$acceptance_worker"
grep -qF 'wait "${host_probe_pids[$index]}"' "$acceptance_worker"
grep -qF 'host-side private address was reachable: ${private_probe_addresses[$index]}' \
  "$acceptance_worker"
grep -qF 'podman rm --force --time=0 --ignore "$name"' "$acceptance_worker"
if grep -qF '/proc/self/uid_map' "$acceptance_worker"; then
  echo "acceptance must inspect UID mappings from the host namespace" >&2
  exit 1
fi
if grep -qF '/sys/fs/cgroup/aeons-ci.slice' "$acceptance_worker"; then
  echo "acceptance must resolve systemd's hierarchical slice path dynamically" >&2
  exit 1
fi
grep -qF 'before_ids=' "$repo_root/README.md"
grep -qF 'grep -qxF "$run_id" <<<"$before_ids"' "$repo_root/README.md"
grep -qF 'assert_runner_cleanup()' "$repo_root/README.md"
if (( $(grep -c '^assert_runner_cleanup$' "$repo_root/README.md") < 7 )); then
  echo "every canary and real-workflow stage must prove exact cleanup" >&2
  exit 1
fi
timeout_boundary_check='test "$(gh run view "$run_id" --repo "$repo" --json jobs --jq '\''[.jobs[].steps[] | select(.name == "Verify qualified toolchain and container boundary") | .conclusion] | unique | .[]'\'')" = success'
grep -qF "$timeout_boundary_check" "$repo_root/README.md"
timeout_conclusion_check='test "$(gh run view "$run_id" --repo "$repo" --json conclusion --jq .conclusion)" = cancelled'
test "$(grep -cF "$timeout_conclusion_check" "$repo_root/README.md")" = 2

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

if grep -qE '/etc/sub(uid|gid)' "$repo_root/build_files/build.sh"; then
  echo "subordinate IDs must be reconciled on the live host, not baked into /etc" >&2
  exit 1
fi

subid_test_root=$(mktemp -d)
trap 'rm -rf -- "$subid_test_root"' EXIT
subuid_file="$subid_test_root/subuid"
subgid_file="$subid_test_root/subgid"
lock_file="$subid_test_root/lock"
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
AEONS_SUBUID_FILE="$subuid_file" \
AEONS_SUBGID_FILE="$subgid_file" \
AEONS_SUBID_LOCK_FILE="$lock_file" \
  "$host_setup"
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

if grep -qE 'systemctl enable (aeons-runnerd|aeons-ci-firewall|aeons-runner-image)' "$repo_root/build_files/build.sh"; then
  echo "runner services must remain disabled until live acceptance passes" >&2
  exit 1
fi
