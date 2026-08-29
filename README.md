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

After an explicitly approved reboot into the new oldtimer image:

```bash
sudo systemctl start aeons-ci-firewall.service aeons-runner-image.service
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

Before enabling any PR route, prove all of the following from containers run as
`aeons-ci` with the same flags emitted by `runnerd`:

- Four concurrent containers enforce 2 CPUs, 8 GiB, 2,048 PIDs, and the
  aggregate slice remains below 8 CPUs/40 GiB.
- Public DNS and HTTPS work; container-local loopback works.
- nft counters prove rejection of `192.168.0.11`, `192.168.0.1`, Docker/k3d
  bridge ranges, `100.64.0.0/10`, link-local, host-local, and all IPv6 traffic.
- `/etc` is read-only; host homes, service data, sockets, ports, devices, and
  host UID 1000 are absent; `CapEff` is zero and `NoNewPrivs` is one.
- Killing `aeons-runnerd` removes only `io.aeons.runnerd.owner=oldtimer`
  containers and server runner registrations while preserving the scale set.

List the exact-owned local state without mutating it:

```bash
sudo -u aeons-ci env HOME=/var/lib/aeons-ci XDG_RUNTIME_DIR=/run/aeons-ci \
  podman ps --all --filter label=io.aeons.runnerd.owner=oldtimer
sudo nft list table inet aeons_ci
```

Then manually dispatch Aeons' diagnostics and client workflows with runner input
`aeons-oldtimer-linux-x64`. Exercise success, intentional failure, cancellation,
and daemon restart. Only after both real workflows and cleanup pass, enable the
units persistently and set the Aeons repository variable:

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
kills the attached jobs, GitHub requeues them, and startup performs exact-owned
cleanup rather than implementing a second drain protocol.
