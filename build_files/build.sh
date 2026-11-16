#!/bin/bash
set -ouex pipefail

# Copy system overlay files into the image
cp -r /ctx/system_files/* /
chmod 0440 /etc/sudoers.d/99-password-feedback
visudo -c

# Homebrew zsh is installed persistently outside the OS image.
grep -qxF /home/linuxbrew/.linuxbrew/bin/zsh /etc/shells || \
    printf '\n%s\n' /home/linuxbrew/.linuxbrew/bin/zsh >> /etc/shells

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

# The workflow resolves and verifies the RPM before building the image.
(cd /ctx/mergerfs && sha256sum -c SHA256SUMS)
dnf5 install -y fuse /ctx/mergerfs/package.rpm
if ! rpm -q mergerfs || ! mergerfs --version; then
    echo "ERROR: mergerfs must be installed and runnable; refusing to build an image without the storage pool mount tool." >&2
    exit 1
fi

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
