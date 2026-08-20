Status: done

# 03: ci, packaging: an image on ghcr, so installing is a pull

Depends on 01. Independent of 02, but land it second.

## What

- **A `docker` job in `.github/workflows/release.yml`**, on the same `v*` tag
  push, with `permissions: packages: write` alongside `contents: write`.
  `docker/login-action` against `ghcr.io` with `GITHUB_TOKEN`,
  `docker/metadata-action` for the tags, `docker/build-push-action` to build
  and push.

  Tags from `metadata-action`: the full version (`0.1.0`), the major.minor
  (`0.1`), and `latest`. `latest` points at the newest *tag*, never at `main`:
  an image that moves under a `docker compose pull` is how a self-hosted app
  breaks on a Tuesday for no stated reason.

  `--build-arg VERSION=${{ github.ref_name }}` so `verve version` inside the
  image reports the tag rather than the `docker` default from `Dockerfile:31`.

- **`linux/amd64` only.** No `platforms:` list, no QEMU setup, no
  `TARGETARCH`. The Dockerfile is left exactly as it is. See the PRD: arm64 is
  wanted, is cheap because `CGO_ENABLED=0` makes the Go build cross-compile
  natively, and is deliberately not in this milestone. What is being
  established here is that pushing a tag yields a pullable image, and a second
  platform is a second way for that to fail before it has ever worked once.

- **ghcr and not Docker Hub.** One registry, tied to the repository that
  produces it, authenticated by the `GITHUB_TOKEN` the workflow already has.
  Docker Hub means a second account, a `DOCKERHUB_TOKEN` secret to create and
  rotate by hand, and a namespace that can be squatted. If it is wanted later
  it is a second `login-action` and a second tag list, not a redesign. Note
  that a package pushed to ghcr is **private by default**: its visibility has
  to be set to public once, in the repository's package settings, or an
  anonymous `docker pull` fails with a 403 that reads like a typo.

- **`compose.yml` switches to the published image.** The file already carries
  the answer as a comment:

  ```yaml
  # build: .
  image: ghcr.io/gauthier-se/verve:latest
  ```

  Keep `build: .` present but commented, with the note that it is the path for
  running an unreleased `main`. The surrounding comment block ("Build the image
  locally so `docker compose up` works with no prerequisites. Once a published
  image exists, swap in `image:`") is now describing history and gets rewritten.

- **The README's install path drops the clone.** "Docker Compose, recommended
  for a homelab" currently opens with `git clone`, `cd verve`,
  `docker compose up -d --build`. It becomes: save a short compose file, then
  `docker compose up -d`. The `--build` flag goes. Add the one-liner for
  someone who wants to look before committing:

  ```sh
  docker run --rm -p 8080:8080 -v verve-data:/data \
      ghcr.io/gauthier-se/verve:latest serve --addr=:8080 --secure-cookie=false
  ```

  and keep the `--secure-cookie=false` explanation attached to it, because a
  first-time user on plain HTTP who cannot log in will not guess that a cookie
  attribute is why.

- **`docs/deployment.md`** gains the image reference, the tag policy above,
  and the sentence about upgrades: pull the new tag, restart, migrations apply
  themselves on startup, and there is no downgrade. That last clause is the
  honest statement of a forward-only migration set, and it belongs beside the
  backup instructions rather than in a release note nobody re-reads. It also
  names the amd64-only limitation, because an operator on a Pi should read it
  in the docs rather than discover it from `exec format error`.

## Why here

The image matters more than the binaries for the same reason `compose.yml`
exists: the audience is people running a homelab, and their install is a
compose file, not a tarball on a path. The binaries are for everyone else.

It is also the half of the release that is hardest to test without doing it.
`goreleaser build --snapshot` proves the binaries locally; nothing proves that
a workflow can authenticate to a registry and push under a tag except pushing
under a tag. That is an argument for doing it now, at `0.1.0`, rather than on
the day v1 is meant to ship.

## Done when

- `docker pull ghcr.io/gauthier-se/verve:0.1.0` works from a machine that has
  never authenticated to ghcr.
- `docker run --rm ghcr.io/gauthier-se/verve:0.1.0 version` prints `0.1.0`.
- A `compose.yml` copied out of the README, with no repository checked out,
  brings up a working instance that reaches the first-run account screen.

## Comments

Landed, with one deliberate departure from the spec above.

**`build-args` reads `steps.meta.outputs.version`, not `github.ref_name`.**
Writing it as specified would have stamped `v0.1.0` into the image while
goreleaser stamped `0.1.0` into the binaries, because goreleaser strips the
leading `v` and `github.ref_name` does not. Two artifacts from one tag
disagreeing about their own version number is exactly the kind of thing nobody
notices until someone pastes both into a bug report.

Smoke-tested locally against a real image built from this Dockerfile, since the
push itself cannot be tested without a tag:

- `verve version` inside the image reports the stamped version.
- The container starts, applies all eleven migrations including
  `0011_session_stats.sql`, and serves the embedded SPA on `/`.
- `GET /v1/auth/state` returns `needs_bootstrap: true`, `POST
  /v1/auth/register` creates the account, and the state then flips to
  `needs_bootstrap: false`. The first-run path works from a bare image and an
  empty volume, which is the README's install path end to end.
- The compose snippet in the README was extracted verbatim and passes
  `docker compose config`, as does `compose.yml` itself.

**Two manual steps remain and cannot be done from here.** After the first tag,
the ghcr package is private by default and its visibility must be set to public
once in the repository's package settings, or an anonymous `docker pull` fails
with a 403. And nothing proves the registry push works until a tag is pushed.
