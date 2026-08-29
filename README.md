# oldtimer
oldtimer is an OCI image building on top of Bazzite/ublue-os images.

## Aeons GitHub Actions runners

The image contains an opt-in pool of four ephemeral, repository-scoped Aeons
runners. `aeons-runnerd` uses the official GitHub scale-set client and launches
one attached rootless Podman container per job under the dedicated `aeons-ci`
account. It never uses Tommi's Podman storage, the host Docker daemon, k3d, or a
container-engine socket.

The units are intentionally disabled in the image. Activate them only after the
new deployment is booted and the following gate is completed.

### Provision the GitHub App

Create a GitHub App installed only on `FullPotatoStudios/Aeons`. Grant repository
`Administration: read and write`; grant no Contents, Actions, Secrets, or
organization permissions. Record its client ID and installation ID and download
one private key.

On oldtimer, create the non-secret configuration:

```bash
sudo install -o root -g root -m 0600 /dev/null /etc/aeons-runnerd.env
sudoedit /etc/aeons-runnerd.env
```

The file contains only:

```text
AEONS_RUNNERD_APP_CLIENT_ID=Iv23.example
AEONS_RUNNERD_APP_INSTALLATION_ID=12345678
```

Encrypt the downloaded PEM for the system unit, then remove the plaintext PEM
from the download location:

```bash
sudo install -d -o root -g root -m 0700 /etc/credstore.encrypted
sudo systemd-creds encrypt --name=github-app-key github-app.pem \
  /etc/credstore.encrypted/github-app-key
```

### Build, start, and inspect

After an explicitly approved reboot into the new oldtimer image, start only the
firewall and image builder, run the image-owned host acceptance program with the
daemon stopped, then start the daemon:

```bash
sudo systemctl start aeons-ci-firewall.service aeons-runner-image.service
sudo /usr/libexec/aeons-runner-host-acceptance
sudo systemctl start aeons-runnerd.service
sudo systemctl status aeons-ci-firewall.service aeons-runner-image.service aeons-runnerd.service
sudo journalctl -u aeons-runnerd.service -f
```

The image service builds the embedded recipe from a digest-pinned official
runner and installs Ruby, Python, Git LFS, and the libraries used by headless
Godot. It reevaluates that recipe on each service start, using Podman's layer
cache while ensuring a base-image update cannot leave a stale tag in service.
Runner jobs use a read-only root and bounded tmpfs, so their checkout, tools,
and diagnostics disappear at exit without growing persistent container storage.

The host acceptance program refuses to run while `aeons-runnerd` is active or
when any owner-labelled container already exists. It creates exactly four
fixed-name probes with the production isolation and resource flags, checks the
following contract, then removes only those names:

- Four concurrent containers enforce 2 CPUs, 8 GiB, 2,048 PIDs, and the
  aggregate slice remains below 8 CPUs/40 GiB.
- Public DNS and HTTPS work; container-local loopback works.
- nft counters prove rejection of `192.168.0.11`, `192.168.0.1`, Docker/k3d
  bridge ranges, `100.64.0.0/10`, link-local, host-local, and all IPv6 traffic.
- `/etc` is read-only; host homes, service data, engine sockets, published
  ports, host devices, and host UID 1000 are absent; `CapEff` is zero and
  `NoNewPrivs` is one.
- The `aeons_ci` nft counter increases during its private-address probes.

List the exact-owned local state without mutating it:

```bash
sudo -u aeons-ci env HOME=/var/lib/aeons-ci XDG_RUNTIME_DIR=/run/aeons-ci \
  podman ps --all --filter label=io.aeons.runnerd.owner=oldtimer
sudo nft list table inet aeons_ci
```

### Exercise the real GitHub lifecycle

Run these from an Aeons checkout after both PRs are merged. Keep
`AEONS_PR_LINUX_RUNNER` unset throughout qualification. The manual-only canary
uses no repository secret and accepts the label explicitly.

Define a helper that dispatches one mode and returns its newest matching run ID:

```bash
repo=FullPotatoStudios/Aeons
runner=aeons-oldtimer-linux-x64
start_canary() {
  local mode=$1 run_id=
  gh workflow run local-runner-acceptance.yml --repo "$repo" --ref main \
    -f runner="$runner" -f mode="$mode"
  for attempt in {1..15}; do
    run_id=$(gh run list --repo "$repo" --workflow local-runner-acceptance.yml \
      --event workflow_dispatch --limit 20 --json databaseId,displayTitle \
      --jq ".[] | select(.displayTitle == \"Local runner acceptance ($mode)\") | .databaseId" |
      head -n1)
    [[ -n $run_id ]] && break
    sleep 2
  done
  [[ -n $run_id ]] || return 1
  printf '%s\n' "$run_id"
}
```

Prove success, intentional failure, timeout, and cancellation. The non-success
modes are passing acceptance evidence only when their recorded conclusion is the
one requested:

```bash
run_id=$(start_canary success)
gh run watch "$run_id" --repo "$repo" --exit-status

run_id=$(start_canary failure)
! gh run watch "$run_id" --repo "$repo" --exit-status
test "$(gh run view "$run_id" --repo "$repo" --json conclusion --jq .conclusion)" = failure

run_id=$(start_canary timeout)
! gh run watch "$run_id" --repo "$repo" --exit-status
test "$(gh run view "$run_id" --repo "$repo" --json conclusion --jq .conclusion)" = failure

run_id=$(start_canary hold)
until [[ $(gh run view "$run_id" --repo "$repo" --json status --jq .status) = in_progress ]]; do sleep 2; done
gh run cancel "$run_id" --repo "$repo"
gh run watch "$run_id" --repo "$repo" || true
test "$(gh run view "$run_id" --repo "$repo" --json conclusion --jq .conclusion)" = cancelled
```

The restart canary is the only planned exception to “never restart with an
active runner.” It is safe only while routing is unset and the hold canary is the
sole owner-labelled container. An interrupted in-progress job is not promised to
requeue; this procedure expects it to end non-successfully, proves exact cleanup,
then reruns it.

```bash
if gh variable get AEONS_PR_LINUX_RUNNER --repo "$repo" >/dev/null 2>&1; then
  echo 'routing must be unset during restart qualification' >&2
  false
fi
run_id=$(start_canary hold)
until [[ $(gh run view "$run_id" --repo "$repo" --json status --jq .status) = in_progress ]]; do sleep 2; done
test "$(sudo -u aeons-ci env HOME=/var/lib/aeons-ci XDG_RUNTIME_DIR=/run/aeons-ci \
  podman ps --filter label=io.aeons.runnerd.owner=oldtimer --format '{{.Names}}' | wc -l)" = 1
sudo systemctl restart aeons-runnerd.service
gh run watch "$run_id" --repo "$repo" || true
test -z "$(sudo -u aeons-ci env HOME=/var/lib/aeons-ci XDG_RUNTIME_DIR=/run/aeons-ci \
  podman ps --all --filter label=io.aeons.runnerd.owner=oldtimer --format '{{.Names}}')"
test "$(gh api "repos/$repo/actions/runners" --jq \
  '[.runners[] | select(.name | startswith("aeons-oldtimer-"))] | length')" = 0
run_id=$(start_canary success)
gh run watch "$run_id" --repo "$repo" --exit-status
```

Finally dispatch the actual qualified workflows and require both to pass:

```bash
gh workflow run gd-diagnostics.yml --repo "$repo" --ref main -f runner="$runner"
gh workflow run client-gdunit.yml --repo "$repo" --ref main -f runner="$runner"
diagnostics_id=$(gh run list --repo "$repo" --workflow gd-diagnostics.yml \
  --event workflow_dispatch --limit 1 --json databaseId --jq '.[0].databaseId')
client_id=$(gh run list --repo "$repo" --workflow client-gdunit.yml \
  --event workflow_dispatch --limit 1 --json databaseId --jq '.[0].databaseId')
gh run watch "$diagnostics_id" --repo "$repo" --exit-status
gh run watch "$client_id" --repo "$repo" --exit-status
```

Only after every step above passes, enable the units persistently and set the
Aeons repository variable:

```bash
sudo systemctl enable aeons-ci-firewall.service aeons-runner-image.service aeons-runnerd.service
gh variable set AEONS_PR_LINUX_RUNNER --repo FullPotatoStudios/Aeons \
  --body aeons-oldtimer-linux-x64
```

Rollback is immediate for newly evaluated jobs:

```bash
gh variable delete AEONS_PR_LINUX_RUNNER --repo FullPotatoStudios/Aeons
sudo systemctl disable --now aeons-runnerd.service
```

Do not restart the daemon while `podman ps` shows an active runner during a
planned operation. An unexpected restart is deliberately fail-fast: systemd
kills the attached jobs, the run may fail and need a manual rerun, and startup
performs exact-owned cleanup rather than implementing a second drain protocol.
