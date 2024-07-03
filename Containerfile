ARG SOURCE_IMAGE="${SOURCE_IMAGE:-bazzite}"
ARG BASE_IMAGE="ghcr.io/ublue-os/${SOURCE_IMAGE}"
ARG FEDORA_MAJOR_VERSION="${FEDORA_MAJOR_VERSION:-39}"

FROM ${BASE_IMAGE}:${FEDORA_MAJOR_VERSION} AS oldtimer

ARG IMAGE_NAME="${IMAGE_NAME}"
ARG IMAGE_VENDOR="${IMAGE_VENDOR}"
ARG IMAGE_FLAVOR="${IMAGE_FLAVOR}"
ARG BASE_IMAGE_NAME="${BASE_IMAGE_NAME}"
ARG FEDORA_MAJOR_VERSION="${FEDORA_MAJOR_VERSION}"

# Fetch ublue-os-staging-fedora-${FEDORA_MAJOR_VERSION}.repo
RUN wget https://copr.fedorainfracloud.org/coprs/ublue-os/staging/repo/fedora-"${FEDORA_MAJOR_VERSION}"/ublue-os-staging-fedora-"${FEDORA_MAJOR_VERSION}".repo -O /etc/yum.repos.d/ublue-os-staging-fedora-"${FEDORA_MAJOR_VERSION}".repo

# Copy static configurations and component files.
COPY system_files/shared system_files/dx /

# Apply IP Forwarding before installing Docker to prevent messing with LXC networking
RUN sysctl -p

# Install snapraid, mergerfs, container packages
RUN chmod +x /tmp/github-release-install.sh && \
    rpm-ostree install \
    snapraid git-lfs \
    docker-ce docker-ce-cli docker-buildx-plugin docker-compose-plugin \
    podman-compose podman-tui \
    code devpod && \
    /tmp/github-release-install.sh trapexit/mergerfs fc40.x86_64

# Set up services
RUN systemctl enable docker.socket && \
    systemctl enable podman.socket && \
    systemctl enable bluefin-dx-groups.service && \
    systemctl enable --global bluefin-dx-user-vscode.service

# Run the build script, then clean up temp files and finalize container build.
RUN chmod +x /tmp/image-info.sh && \
    /tmp/image-info.sh && \
    rm -rf /tmp/* /var/* && \
    mkdir -p /var/tmp && \
    chmod -R 1777 /var/tmp && \
    ostree container commit
