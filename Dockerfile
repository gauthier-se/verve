# Verve ships as a single static binary with the React SPA embedded (ADR 0005).
# This build has three stages: build the SPA, embed it into a CGo-free Go binary
# (ADR 0004 — pure-Go SQLite means CGO_ENABLED=0), then copy that one binary into
# a distroless image. The result is a small, rootless image with a single
# writable volume for VERVE_DATA_DIR.

# --- Stage 1: build the React SPA ---
# Vite writes its output into internal/web/dist (see web/vite.config.ts), the
# directory the Go `web` package embeds. package-lock.json is copied first so the
# dependency layer caches across source-only changes.
FROM node:22-alpine AS web
WORKDIR /src
COPY web/package.json web/package-lock.json ./web/
RUN npm --prefix web ci
COPY web ./web
RUN npm --prefix web run build

# --- Stage 2: build the Go binary ---
# CGO_ENABLED=0 gives a fully static binary (pure-Go SQLite driver, no libc),
# which is what makes the distroless/scratch final image clean. go.mod/go.sum are
# copied first so the module-download layer caches independently of source edits.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Overlay the freshly built SPA (dist/ is git-ignored, so `COPY . .` only brought
# in the .gitkeep placeholder; this replaces it with the real index.html+assets).
COPY --from=web /src/internal/web/dist ./internal/web/dist
# version defaults to "docker"; a release build overrides it with the git tag.
ARG VERSION=docker
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /verve ./cmd/verve
# The data dir is baked in with nonroot ownership so a fresh named volume mounted
# over it inherits writable permissions for the unprivileged runtime user.
RUN mkdir -p /data && chown 65532:65532 /data

# --- Stage 3: minimal runtime ---
# distroless/static has no shell or package manager — just CA certs and the
# nonroot (uid 65532) user. Nothing to attack, nothing to patch.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /verve /verve
COPY --from=build --chown=65532:65532 /data /data
ENV VERVE_DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/verve"]
# Default to the server with a secure session cookie (assumes an HTTPS reverse
# proxy in front). Over plain HTTP — a LAN, or `docker run -p 8080:8080` — logins
# need `serve --addr=:8080 --secure-cookie=false` (compose.yml sets this).
# Override the command for CLI use, e.g.
#   docker run --rm -it -v verve-data:/data verve account create --email=you@example.com
CMD ["serve", "--addr=:8080"]
