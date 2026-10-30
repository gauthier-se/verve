# The web app and the Go binary are built on the build platform, so that no target needs emulation:
#
#   docker buildx build --platform linux/amd64,linux/arm64 \
#     --build-arg VERSION=1.2.3 --build-arg COMMIT=abc1234 --build-arg BUILD_DATE=2026-01-02T03:04:05Z .
#
# Base images are pinned by digest: Dependabot has no version to compare for the distroless tag.

FROM --platform=$BUILDPLATFORM node:26-alpine@sha256:c6ad9e9c1d80f6eb607c26b4b0a132ff7a2344fad1c78ea3baa2a7d4c8de6d94 AS web
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
# Node.js images no longer ship corepack: install the pnpm version of the packageManager field.
RUN npm install --global "$(node --print 'require("./package.json").packageManager')"
RUN --mount=type=cache,id=pnpm-store,target=/pnpm/store \
    pnpm install --frozen-lockfile --store-dir /pnpm/store
COPY web/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /src/web/dist ./internal/webui/dist
RUN --mount=type=cache,id=go-mod,target=/go/pkg/mod \
    --mount=type=cache,id=go-build,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w \
    -X github.com/gauthier-se/verve/internal/version.version=$VERSION \
    -X github.com/gauthier-se/verve/internal/version.commit=$COMMIT \
    -X github.com/gauthier-se/verve/internal/version.date=$BUILD_DATE" \
    -o /out/verve ./cmd/verve
# Docker copies this directory, with its owner, into every new named volume.
RUN mkdir -m 0750 /out/data

FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.source="https://github.com/gauthier-se/verve" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$COMMIT" \
      org.opencontainers.image.created="$BUILD_DATE" \
      org.opencontainers.image.licenses="AGPL-3.0-only"
COPY --from=build /out/verve /verve
COPY --from=build --chown=65532:65532 /out/data /data
ENV VERVE_DATA_DIR=/data \
    VERVE_LISTEN_ADDR=:8080
USER nonroot:nonroot
EXPOSE 8080
VOLUME /data
# start-interval: report healthy within seconds of starting, instead of after the first interval.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --start-interval=2s --retries=3 \
    CMD ["/verve", "healthcheck"]
ENTRYPOINT ["/verve"]
CMD ["serve"]
