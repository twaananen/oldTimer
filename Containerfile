FROM docker.io/library/golang:1.27.1@sha256:512690a5660563b57d37ecc31129e7f136e831db2aed24a1dbeb8ad7380dc0fa AS runnerd-build
WORKDIR /src
COPY runnerd/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=0.1.0" -o /out/aeons-runnerd .

FROM scratch AS ctx
COPY build_files /
COPY runner-image /runner-image

FROM ghcr.io/ublue-os/bazzite:stable

RUN --mount=type=bind,from=ctx,source=/,target=/ctx \
    --mount=type=cache,dst=/var/cache \
    --mount=type=cache,dst=/var/log \
    --mount=type=tmpfs,dst=/tmp \
    /ctx/build.sh

COPY --from=runnerd-build /out/aeons-runnerd /usr/libexec/aeons-runnerd
COPY --from=ctx /runner-image /usr/share/aeons-runner-image

RUN systemd-analyze verify \
        aeons-ci.slice \
        aeons-ci-host-setup.service \
        aeons-ci-firewall.service \
        aeons-runner-image.service \
        aeons-runnerd.service \
    && rm -f /run/systemd/systemd-units-load \
    && bootc container lint
