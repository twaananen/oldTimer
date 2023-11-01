ARG SOURCE_IMAGE="${SOURCE_IMAGE:-bazzite}"
ARG BASE_IMAGE="ghcr.io/ublue-os/${SOURCE_IMAGE}"
ARG FEDORA_MAJOR_VERSION="${FEDORA_MAJOR_VERSION:-39}"

FROM ${BASE_IMAGE}:${FEDORA_MAJOR_VERSION} AS oldtimer

ARG IMAGE_NAME="${IMAGE_NAME}"
ARG IMAGE_VENDOR="${IMAGE_VENDOR}"
ARG IMAGE_FLAVOR="${IMAGE_FLAVOR}"
ARG BASE_IMAGE_NAME="${BASE_IMAGE_NAME}"
ARG FEDORA_MAJOR_VERSION="${FEDORA_MAJOR_VERSION}"

# Copy static configurations and component files.
COPY system_files/shared /

# Install snapraid, mergerfs
RUN chmod +x /tmp/github-release-install.sh && \
    rpm-ostree install \
    snapraid && \
    /tmp/github-release-install.sh trapexit/mergerfs fc38.x86_64

# Run the build script, then clean up temp files and finalize container build.
RUN chmod +x /tmp/image-info.sh && \
    /tmp/image-info.sh && \
    rm -rf /tmp/* /var/* && \
    mkdir -p /var/tmp && \
    chmod -R 1777 /var/tmp && \
    ostree container commit