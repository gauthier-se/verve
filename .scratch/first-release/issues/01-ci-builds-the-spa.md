Status: done

# 01: ci: CI builds the SPA, so a tag is not the first TypeScript compile

## What

- **A second job in `.github/workflows/ci.yml`**, `web`, on the same `push` to
  `main` and `pull_request` triggers as the existing `go` job:

  ```yaml
  web:
    name: Web typecheck and build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '22'
          cache: npm
          cache-dependency-path: web/package-lock.json
      - run: npm --prefix web ci
      - run: npm --prefix web run build
  ```

  Node 22 matches the Dockerfile's `node:22-alpine` build stage; the two must
  not drift, and a comment says so in both places.

  `npm run build` is `tsc --noEmit && vite build` (`web/package.json`), so this
  one step is both the typecheck and the bundle. No separate `typecheck` step:
  running `tsc` twice per PR to get a marginally better failure label is not
  worth the minute.

- **The job runs beside the Go job, not before it.** No `needs:`. The Go job's
  ability to build and test with no Node installed is a *property*, held by
  `internal/web/dist/.gitkeep`, by `go:embed all:dist`, and by two tests
  (`web_test.go` on the unbuilt page and `TestEmbeddedHandlerConstructs`).
  Making the Go job depend on a built `dist/` would quietly delete it and would
  make every Go-only contributor wait on npm.

- **`make ci` gains the same build**, as a new `ui-check` prerequisite or an
  inline step, whichever keeps the target readable. It must be the *same*
  command CI runs. Note that `make ui` additionally does
  `rm -rf internal/web/dist/assets`; keep that in the make path, since a
  developer's checkout is exactly where stale hashed assets accumulate and CI's
  is exactly where they cannot.

- **`CONTRIBUTING.md:23`** claims "A green `make ci` locally means a green CI,
  because the `ci` target mirrors `.github/workflows/ci.yml` exactly." That
  sentence is the reason this issue touches the Makefile at all. It stays true,
  and it gains the caveat that `make ci` now needs Node, with the Go-only
  fallback named (`make test`, `make vet`) for a contributor who has none.

## Why here

This is first in the milestone and prerequisite to the rest of it. Today the
front end is compiled by exactly two things: a developer running `make ui`, and
`goreleaser`'s `before` hook. The second of those runs *against a tag that has
already been pushed*. A tag is immutable in every practical sense, so a
TypeScript error would mean either a deleted tag or a `v0.1.1` whose only
content is an apology.

Concretely, what merges green today and should not: any type error, any import
of a file that no longer exists, any `@/` alias that does not resolve, any
Vite-level failure. The web tests this repo does have are Go tests reading the
`.tsx` as *text* (`internal/web/workouts_test.go`, and the palette and
sleep-stage contracts before it). They are deliberate and they are good at what
they do, and none of them can tell whether the file compiles.

Cost of the job: one `npm ci` against a cached lockfile plus a Vite build, call
it under two minutes. Cost of not having it: the first release.

## Done when

- A PR that introduces a deliberate type error in `web/src` fails CI.
- A PR touching only Go still passes without the web job having built anything
  the Go job needs.
- `make ci` runs the web build and fails on the same error.

## Comments

Landed as specified. The `web` job runs beside `go` with no `needs:`, Node 22
with an npm cache keyed on `web/package-lock.json`, and one `npm run build` step
that is both the typecheck and the bundle.

`make ci` gained `$(MAKE) ui` rather than a bare npm call, so the local path
keeps the `rm -rf internal/web/dist/assets` that `ui` already does and CI does
not need. `make ci-go` is the Go-only subset named in CONTRIBUTING.

Mutation-checked rather than assumed. With a deliberate `const x: number =
"nope"` appended to `web/src/lib/format.ts`:

```
npm run build   exit 2   (error TS2322)
make ci         exit 2
make ci-go      exit 0
```

The third line is the point: `make ci-go` passing on a broken front end is the
tested property, not a gap. Restored, and `git diff` on the file is empty.

Also verified that `rm -rf internal/web/dist/assets` leaves `dist/.gitkeep`
alone and that `go build ./...` still succeeds against the bare placeholder,
which is the invariant the whole two-job split rests on.
