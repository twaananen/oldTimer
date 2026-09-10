# oldtimer

oldtimer is an OCI image building on top of Bazzite/ublue-os images.

## Aeons CI host bootstrap

Aeons owns its runner image, supervisor, runtime unit, acceptance checks and
release operator in [tools/local-runner](https://github.com/FullPotatoStudios/Aeons/tree/main/tools/local-runner).
Runner changes no longer require rebuilding this OS or rebooting the machine.

This image supplies the stable host foundation: the dedicated `aeons-ci` user,
subordinate UID/GID reconciliation, rootless Podman configuration, public DNS
resolver, private-network firewall, isolation helpers and aggregate systemd
slice (12 CPU equivalents, MemoryHigh 48 GiB, MemoryMax 64 GiB). These must
remain installed while the extracted runner pool uses them.

The firewall pulls in host setup, which reconciles the allocation in persistent
`/etc` under the system account lock. It does not change other users' Podman
configuration. Bootstrap units stay opt-in; on an already provisioned host,
runtime updates do not restart them.

Removing the retired OS runtime files does not remove `/etc/systemd/system`'s
Aeons-owned unit or `/var/lib/aeons-ci-releases`. Apply this cleanup with a normal
future OS update; no immediate update or reboot is needed. Old deployments still
contain their historical binaries, but the installed runtime override and
image-builder mask keep the Aeons release in control.

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

### Install or update the runtime

On a newly bootstrapped host, start the firewall with
`sudo systemctl start aeons-ci-firewall.service`, then follow the Aeons runner
runbook for staging, qualification and activation. Keep credentials in the
system encrypted credential store, outside both repositories and images.

Do not re-enable the retired `aeons-runner-image.service`. Runtime rollback,
resource settings, caches and workflow qualification are documented in Aeons;
this repository does not carry a second copy of that procedure.
