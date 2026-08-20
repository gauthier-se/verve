Status: done

# 02: ci: a tag produces binaries, and goreleaser stops embedding last month's assets

Depends on 01.

## What

- **`.github/workflows/release.yml`**, new:

  ```yaml
  on:
    push:
      tags: ['v*']

  permissions:
    contents: write   # goreleaser creates the release and uploads assets
  ```

  Steps: `actions/checkout@v4` with `fetch-depth: 0` (goreleaser derives the
  version and the changelog from git history and fails on a shallow clone),
  `actions/setup-go@v5` at 1.26, `actions/setup-node@v4` at 22, then
  `goreleaser/goreleaser-action@v6` with `args: release --clean` and
  `GITHUB_TOKEN` from `secrets.GITHUB_TOKEN`.

  Node is in the runner because `.goreleaser.yaml`'s `before` hooks are
  `npm --prefix web ci` and `npm --prefix web run build`. Without it the
  release fails on its first hook, after the tag exists.

- **The stale-assets fix in `.goreleaser.yaml`.** Its `before` hooks build the
  SPA but never clear `internal/web/dist/assets`, and `web/vite.config.ts` sets
  `emptyOutDir: false` deliberately, to protect the committed `.gitkeep` that
  keeps `go:embed` compiling. `make ui` covers the gap with an explicit
  `rm -rf internal/web/dist/assets`; goreleaser has no equivalent. Add it as
  the first hook:

  ```yaml
  before:
    hooks:
      - rm -rf internal/web/dist/assets
      - npm --prefix web ci
      - npm --prefix web run build
  ```

  On a CI runner the directory holds only the placeholder, so this changes
  nothing there. It matters on a maintainer's laptop, where `--clean` cleans
  goreleaser's own `dist/` and not the SPA's, and where a binary can therefore
  ship with hashed chunks from an older build sitting beside the current ones.
  Vite's `manualChunks` split makes that more likely, not less: two hashed
  files per build, both orphaned on the next one.

- **Verify the dry run before tagging anything.**
  `goreleaser build --snapshot --clean` is already documented at the top of
  `.goreleaser.yaml` and is the gate: it must produce six binaries (linux,
  darwin, windows, times amd64 and arm64), and the linux/amd64 one must answer
  `verve version` with the snapshot version, not `dev`. Stamping is
  `-X main.version={{ .Version }}` against `cmd/verve/version.go`'s
  `var version = "dev"`, and `cmd/verve/version_test.go` already holds the
  command's behaviour, so the only thing a smoke run adds is proof that the
  ldflag path matches the variable path after `-trimpath`.

- **Nothing about the version is edited in a file.** `git tag v0.1.0` is the
  entire input. The Makefile's `git describe`, the Dockerfile's `ARG VERSION`
  and goreleaser's `{{ .Version }}` all read from the tag or default to a
  clearly-not-a-release string (`dev`, `docker`). No `version.txt`, no bump
  commit, no `package.json` version to keep in sync: `web/package.json` says
  `0.1.0` and is `private: true`, so it is never published and is not the
  version of anything.

## Why here

goreleaser has been configured since the core milestone and has never run. The
config is good, its `archives`, `checksum` and `changelog` blocks need no
change, and its `snapshot` template already produces something sane. The work
is the trigger and one missing `rm`.

The `rm` is worth the paragraph it costs because of *how* it fails: silently,
locally, and only for a maintainer who has run `make ui` before. The binary
builds, the app runs, the extra assets are simply never requested, and the
release is a few hundred kilobytes heavier for no reason. Nothing points at it.
The same class of bug as the map config landing on the wrong endpoint in the
workouts milestone: green CI, working page, wrong artifact.

## Done when

- `goreleaser build --snapshot --clean` produces all six archives locally and
  the linux binary reports the snapshot version.
- Pushing `v0.1.0` produces a GitHub release with six archives and
  `checksums.txt`.
- Deleting `internal/web/dist/assets` by hand and re-running the snapshot
  yields a binary of the same size as one built over a dirty `dist/`.

## Comments

Landed as specified: `release.yml` on `v*`, `fetch-depth: 0`, Go 1.26 and Node
22 in the runner, `goreleaser-action@v6` pinned to `~> v2` with
`release --clean`, and `contents: write` scoped to that job alone rather than to
the workflow.

The `rm -rf internal/web/dist/assets` hook is in. Not runnable here (no
goreleaser binary on this machine), so the dry run named in "Done when" is still
outstanding and is the one thing to do before pushing a tag.

What was verified instead, because it exercises the same stamping path: a
`docker build --build-arg VERSION=v0.1.0-test` produced an image whose
`verve version` prints `v0.1.0-test`, and whose server logs
`version=v0.1.0-test` on startup. That confirms `-X main.version` reaches
`cmd/verve/version.go` through `-trimpath` and a stripped build, which is the
part of the goreleaser ldflags that could silently produce `dev`.
