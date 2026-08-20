# PRD: The first release

## Goal

Publish `v0.1.0`: a git tag, a GitHub release carrying the binaries goreleaser
is already configured to produce, and a container image on ghcr.io. Today
installing Verve means cloning the repository and building it, which is a
developer's install path offered to a homelab audience. This is the only
remaining work that changes *who can run Verve at all*, which is why the
roadmap puts it ahead of every feature milestone.

Nothing here is a new capability. Everything the release publishes already
works and is already tested; what is missing is the machinery that turns a
green `main` into something a stranger can pull.

**This is explicitly not `v1.0.0`, and `v0.1.0` is not a rehearsal for it.**
More features land before the version number stops starting with a zero:
Annotations at least, and whatever the roadmap's "Later" list promotes. The
point of tagging now is that the release machinery gets *used* and therefore
gets debugged, on a version whose stakes are low, rather than being written
under pressure the day v1 is meant to ship. Every `0.x` between here and there
is a real release for anyone who wants to run Verve today, and a rehearsal for
the one that matters.

## The concept

**A release is a promise, and CI does not currently check the thing being
promised.** The CI workflow has one job. It runs `gofmt -l`, `go vet`,
`go build ./...` and `go test -race ./...`, and it never touches Node. That is
deliberate and it is documented: `internal/web/dist/.gitkeep` exists precisely
so `go:embed all:dist` compiles without a front-end build, and
`internal/web/web.go` serves an "has not been built" page for the case, with a
test holding it (`web_test.go:89`).

The consequence is that **no PR has ever had its TypeScript compiled**.
`npm run build` is `tsc --noEmit && vite build`. A type error, a bad import, an
unresolved `@/` alias: all of it merges green, and the *first* thing that would
discover it is `goreleaser release`, running its `before` hook against a tag
that has already been pushed. A tag is not a build environment. The web build
moves into CI before anything is tagged, or the first release is a coin flip.

**The version is already wired end to end; only the publishing is missing.**
`main.version` defaults to `"dev"` (`cmd/verve/version.go`), the Makefile
stamps `git describe`, the Dockerfile takes `ARG VERSION` defaulting to
`"docker"`, goreleaser stamps `{{ .Version }}`, and `cmd/verve/version_test.go`
asserts `verve version` prints it without touching the filesystem. There is
nothing to design here. There is a workflow to write.

**`v0.1.0`, not `v1.0.0`.** The README calls Verve pre-release and it is right
to: the API shape, the Catalog and the auth model are all things a second
connector or a first outside user could still move. A `0.x` tag says the data
is safe and the interface is not yet frozen, which is exactly true. Migrations
are forward-only and self-applying, so the guarantee the tag *does* make is the
one that matters: upgrading never asks you to do anything, and your database
comes with you.

## What this milestone does

### CI builds the SPA

A second job in `.github/workflows/ci.yml`, running on the same triggers:
`npm --prefix web ci` then `npm --prefix web run build`, which typechecks and
bundles. It runs beside the Go job rather than before it: the Go job's
Node-free path is a tested property, not an accident, and coupling the two
would delete the guarantee that a contributor with no Node installed can still
run the Go suite.

`make ci` gains the same step, because `CONTRIBUTING.md:23` states that a green
`make ci` means a green CI, and that sentence is about to stop being true.

### Tagged binaries

`.github/workflows/release.yml`, triggered on `push: tags: ['v*']`, with
`permissions: contents: write`, setting up Go 1.26 **and** Node 22 (goreleaser's
`before` hooks are npm commands), then `goreleaser release --clean`.

One fix to `.goreleaser.yaml` while there: its `before` hooks run
`npm run build` without clearing `internal/web/dist/assets` first, and
`vite.config.ts` sets `emptyOutDir: false` on purpose to protect the `.gitkeep`
placeholder. `make ui` handles this with an explicit `rm -rf`; goreleaser does
not. On a fresh CI checkout `dist/` holds only the placeholder, so the bug is
invisible there and fires on a maintainer's laptop, where `goreleaser release
--clean` cleans goreleaser's own `dist/` and leaves stale hashed assets from
last month's build inside the binary. The `rm -rf` becomes the first hook.

### A published image

The same tag push builds and pushes `ghcr.io/gauthier-se/verve`, tagged with
the full version, the major.minor, and `latest`. `latest` points at the newest
tag and never at `main`: an image that moves under a `docker compose pull` is
how a self-hosted app breaks on a Tuesday for no stated reason.

`linux/amd64` only, for now. arm64 is the obvious next step, since a
self-hosted health warehouse on a Raspberry Pi is a plausible median
deployment, and it is cheap when it comes: `CGO_ENABLED=0` is already set (ADR
0004, pure-Go SQLite), so the Go stage only needs `TARGETARCH` from buildx to
cross-compile natively instead of running under QEMU. It is deferred rather
than done because the first thing to establish is that a tag produces a
pullable image at all, and a second platform doubles the surface on which that
can fail. It is recorded in "What this milestone does NOT do" so it is a
decision and not an oversight.

Then `compose.yml` swaps `build: .` for the `image:` line it already carries
commented out, and the README's install path stops with a `git clone`. Pulling
an image is the whole install.

### What a published version changes in the docs

* `SECURITY.md` currently says "Verve has not reached a tagged release [...]
  Once versions are published this section will say which ones receive fixes."
  It gets its answer: on `0.x`, the latest minor is the supported one.
* The README's `> **Status: pre-release.**` block goes away, replaced by the
  install line for the image, and the "From source" section stays as the second
  path rather than the first.
* `CONTRIBUTING.md` gains a short release section: how a tag is cut, what it
  triggers, and the fact that the version is never edited in a file.
* `ROADMAP.md` moves the release out of "Next", leaving Annotations alone
  there.

## What this milestone does NOT do

* **No committed `CHANGELOG.md`.** goreleaser already generates release notes
  from commit subjects and already filters `docs:`, `test:` and `chore:`. This
  repository's commit *bodies* are essays explaining the reasoning, and they
  are the real changelog; a hand-maintained file would either duplicate them or
  rot beside them. The release page is the changelog. Recorded as a decision so
  it is not re-litigated at every tag.
* **No arm64 image.** `linux/amd64` only. See above: the milestone establishes
  that a tag publishes a working image, and adding a platform is a two-line
  change to `platforms:` plus the `TARGETARCH` edit in the Dockerfile, worth
  doing once the pipeline has run green at least once.
* **No signing, SBOM or build provenance.** Cosign signatures and SLSA
  attestation are wanted and are a later, self-contained piece of work. Their
  absence does not block a first tag, and adding them under time pressure at
  tag time is how they get done badly.
* **No package managers.** No Homebrew tap, no AUR, no nix flake output for the
  binary. goreleaser produces the tap in a few lines whenever someone asks for
  it.
* **No downgrade path.** Migrations are forward-only and apply on startup. That
  is the existing behaviour, it stays, and it gets written down in
  `docs/deployment.md` instead of engineered around.
* **No release cadence.** One person's side project. Tags happen when a
  milestone lands.

## Ordering

Strict. `01` is a prerequisite for trusting anything after it: tagging before
the SPA is built in CI means the tag is the build test. `02` and `03` both hang
off the same tag push and can land in either order, but `02` first keeps the
first tag's blast radius to a GitHub release page. `04` lands last, in the same
PR as the tag, because every sentence it writes is false until the tag exists.

## Docs

* **ADR 0029**, on what a `0.x` tag promises: forward-only migrations and a
  stable data directory, against an interface and an API that are not frozen.
  Rejected alternatives to record: `v1.0.0` for a first tag; a committed
  `CHANGELOG.md`; publishing to Docker Hub as well as ghcr; `latest` pointing
  at `main` rather than at the newest tag.
* **CONTEXT.md** needs no new entry. A release is not a domain concept.

## Issues

1. `01-ci-builds-the-spa`, ci: the web job in `ci.yml`, the matching step in
   `make ci`, and the CONTRIBUTING line that claims the two agree.
2. `02-tagged-binaries`, ci: `release.yml` on `v*`, Go and Node in the runner,
   `goreleaser release --clean`, and the stale-assets fix in the `before`
   hooks.
3. `03-published-image`, ci and packaging: multi-arch build and push to ghcr on
   the same tag, `TARGETARCH` cross-compilation in the Dockerfile, `compose.yml`
   on the published image, README install path without a clone.
4. `04-what-a-published-version-changes`, docs: SECURITY supported versions,
   the README status block, the CONTRIBUTING release section, ROADMAP, ADR 0029,
   and the forward-only note in deployment.md. Lands with the tag.
