# syntax=docker/dockerfile:1

# natslens: the web UI is built with Node, embedded into the Go binary,
# which links DuckDB through cgo; the runtime is distroless with glibc and
# libstdc++, running as the nonroot user (65532). Base images are pinned
# by digest; update the tag and the digest together.
#
# The Node and Go stages run on the build machine's own platform and the Go
# stage cross-compiles for TARGETARCH with Debian's gcc cross toolchain, so
# an amd64 image builds on an arm64 Mac without emulation.

FROM --platform=$BUILDPLATFORM node:26.12.1-bookworm-slim@sha256:88e480776c6149ff1d098cd304f44bf18057fb5d284a3329b78162d29821e28c AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json web/.npmrc ./
RUN npm ci --no-audit --no-fund
# The whole context, not only web/: the type check covers tests that import
# files from elsewhere in the repository (boundary-sql.json, testdata).
COPY . /src/
RUN mkdir -p ../internal/webui/dist && npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.3-bookworm@sha256:dc0963f32053e4bc451d24d87e3892d42d01021b61f093e98eb95c0352bb84b0 AS build
ARG TARGETARCH
ARG BUILDARCH
# Bump when the native compiler/header setup changes without a base-image change.
ARG CGO_CACHE_EPOCH=1
# The C and C++ compilers for the target: the native ones when it is the
# build machine's architecture, else the cross compilers and the target's
# C library headers.
RUN set -eu; \
    case "$TARGETARCH" in \
      amd64) pkg=x86-64-linux-gnu ;; \
      arm64) pkg=aarch64-linux-gnu ;; \
      *) echo "unsupported TARGETARCH $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    if [ "$(dpkg --print-architecture)" = "$TARGETARCH" ]; then \
      pkgs="g++"; \
    else \
      pkgs="gcc-$pkg g++-$pkg libc6-dev-$TARGETARCH-cross"; \
    fi; \
    apt-get update; \
    apt-get install -y --no-install-recommends $pkgs; \
    rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,id=natslens-go-mod-1.27.3,target=/go/pkg/mod,sharing=shared go mod download
COPY . .
COPY --from=web /src/internal/webui/dist/ internal/webui/dist/
ARG VERSION=dev
RUN --mount=type=cache,id=natslens-go-mod-1.27.3,target=/go/pkg/mod,sharing=shared \
    --mount=type=cache,id=natslens-go-build-1.27.3-dc0963f3-${BUILDARCH}-${TARGETARCH}-cgo${CGO_CACHE_EPOCH},target=/root/.cache/go-build,sharing=locked \
    set -eu; \
    case "$TARGETARCH" in \
      amd64) triple=x86_64-linux-gnu ;; \
      arm64) triple=aarch64-linux-gnu ;; \
    esac; \
    CGO_ENABLED=1 GOOS=linux GOARCH="$TARGETARCH" CC="$triple-gcc" CXX="$triple-g++" \
      go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/natslens ./cmd/natslens

# Stages the release workflow exports with --output type=local: the built UI
# for the macOS binaries, and the Linux binaries the image ships.
FROM scratch AS webui
COPY --from=web /src/internal/webui/dist/ /

FROM scratch AS binary
COPY --from=build /out/natslens /natslens

# The runtime image; the last stage, so the default target.
FROM gcr.io/distroless/cc-debian12:nonroot@sha256:9dac0a79194e45a7da0158a9c6da57b217585af0786db3845d1f0ec1a0dd182f
LABEL org.opencontainers.image.title="nats-lens" \
      org.opencontainers.image.description="Observes a NATS deployment: snapshots, events, findings and a web UI" \
      org.opencontainers.image.source="https://github.com/shadone/nats-lens" \
      org.opencontainers.image.licenses="AGPL-3.0-only"
# natslens demo listens here unless -listen says otherwise: a published
# port cannot reach the container's loopback.
ENV NATSLENS_DEMO_LISTEN=0.0.0.0:8080
COPY --from=build /out/natslens /usr/local/bin/natslens
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/natslens"]
CMD ["run", "-config", "/etc/natslens/natslens.yaml"]
