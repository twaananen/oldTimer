#!/bin/bash
set -ouex pipefail

# Copy system overlay files into the image
cp -r /ctx/system_files/* /

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

# Install mergerfs from GitHub release
/ctx/github-release-install.sh trapexit/mergerfs fc43.x86_64

# Disable third-party repos in the final image
dnf5 config-manager setopt docker-ce-stable.enabled=0
dnf5 config-manager setopt code.enabled=0

# Enable services
systemctl enable docker.socket
systemctl enable podman.socket
systemctl enable bluefin-dx-groups.service
systemctl enable --global bluefin-dx-user-vscode.service
