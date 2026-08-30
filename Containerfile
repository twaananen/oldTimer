FROM docker.io/library/golang:1.27.0@sha256:0ecdc2a9f6156af6451080bfe3d8382a662fcc4e209608c6f919e643453514c1 AS runnerd-build
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
        aeons-ci-firewall.service \
        aeons-runner-image.service \
        aeons-runnerd.service \
    && rm -f /run/systemd/systemd-units-load \
    && bootc container lint
