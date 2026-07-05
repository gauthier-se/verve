# 07 — Packaging: Docker, goreleaser, compose, docs

Status: ready-for-agent
Blocked by: 06

## Goal

Make Verve trivially self-hostable: one image, one binary, one data volume.

## Scope

- **Two-stage build**: `vite build` → static assets embedded → Go build. A
  Makefile target wires it (`make build`).
- **Dockerfile**: distroless/scratch final image (CGo-free build makes this
  clean). Single volume for `VERVE_DATA_DIR`.
- **`compose.yml`**: a minimal homelab example (image + volume + port + env).
- **goreleaser**: static per-OS/arch binary releases.
- **Config docs**: `VERVE_DATA_DIR`, flags/env, first-run (`account create`,
  `import`), backup = copy the data dir.
- **README**: what Verve is, Apache-2.0, quickstart (Docker + CLI import).

## Out of scope

CI/CD pipeline setup (separate), forward-auth/SSO deployment guide (v1.x),
Helm/k8s.

## Acceptance

- `docker compose up` starts Verve; migrations auto-apply; the UI is reachable.
- A released binary runs standalone with just `VERVE_DATA_DIR` set.
- Docs cover install → create account → import → view.

## Refs

ADR 0004 (CGo-free), 0005 (single binary), 0010 (Apache-2.0). `good_practices.md` §9.
