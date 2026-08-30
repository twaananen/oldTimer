#!/bin/bash
set -ouex pipefail

# Copy system overlay files into the image
cp -r /ctx/system_files/* /

# Dedicated subordinate IDs keep untrusted runner user namespaces away from
# the workstation user's rootless Podman allocation.
grep -q '^aeons-ci:' /etc/subuid || echo 'aeons-ci:589824:65536' >> /etc/subuid
grep -q '^aeons-ci:' /etc/subgid || echo 'aeons-ci:589824:65536' >> /etc/subgid

# Install packages
dnf5 install -y \
    snapraid \
    git-lfs \
    docker-ce \
    docker-ce-cli \
    docker-buildx-plugin \
    docker-compose-plugin \
    podman-compose \
    podman-tui \
    code

# Install mergerfs from GitHub release (derive Fedora version dynamically)
FEDORA_VERSION=$(rpm -E %fedora)
/ctx/github-release-install.sh trapexit/mergerfs "fc${FEDORA_VERSION}.x86_64"

# Disable third-party repos in the final image
dnf5 config-manager setopt docker-ce-stable.enabled=0
dnf5 config-manager setopt code.enabled=0

# Clean up dnf artifacts that trigger bootc lint warnings
dnf5 clean all
rm -rf /run/dnf /var/lib/dnf/repos

# Enable services
systemctl enable docker.socket
systemctl enable podman.socket
systemctl enable bluefin-dx-groups.service
systemctl enable --global bluefin-dx-user-vscode.service
