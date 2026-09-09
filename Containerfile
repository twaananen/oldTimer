FROM scratch AS ctx
COPY build_files /

FROM ghcr.io/ublue-os/bazzite:stable

RUN --mount=type=bind,from=ctx,source=/,target=/ctx \
    --mount=type=cache,dst=/var/cache \
    --mount=type=cache,dst=/var/log \
    --mount=type=tmpfs,dst=/tmp \
    /ctx/build.sh

RUN systemd-analyze verify \
        aeons-ci.slice \
        aeons-ci-host-setup.service \
        aeons-ci-firewall.service \
    && rm -f /run/systemd/systemd-units-load \
    && bootc container lint
