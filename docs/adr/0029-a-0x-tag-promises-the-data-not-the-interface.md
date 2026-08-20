# A `0.x` tag promises the data, not the interface

## Context

Verve has been buildable and usable for every milestone in the roadmap's
Shipped table, and installable by nobody who was not willing to clone it and
run `make dist`. goreleaser has been configured since the core milestone and
has never run. The distroless image has been buildable since then and has never
been pushed. The README carried a "no version has been tagged yet" banner as
its second paragraph.

Publishing a first version raises one question that looks like bookkeeping and
is not: **what number**, and therefore **what promise**.

The case for `1.0.0` is that the feature list is not a prototype's. Import,
dashboards, derived metrics, period comparison, the ledger, manual entry,
energy planning, appearance, pins, sleep and workouts are all implemented and
tested, and calling that `0.1.0` undersells it to anyone reading the version
number as a maturity signal.

The case against is that semver's `1.0.0` is not a maturity claim, it is a
compatibility contract, and Verve cannot honour it yet. There is one connector.
The Catalog is a closed set that a second connector will push on (ADR 0002,
ADR 0009). The API's shape is still moving with every milestone: the workouts
read path just added an entity endpoint whose window semantics deliberately
depart from `/v1/series` (ADR 0028), and Annotations will touch the Panel
payload. Freezing that at `1.0.0` means either honouring a contract that
blocks the next milestone, or breaking it and teaching readers that the version
number means nothing.

Meanwhile there is a promise Verve *can* make today, and it is the one a
self-hosted app actually owes its user. It is not about JSON.

## Decision

**The first tag is `v0.1.0`, and `0.x` continues until the interface settles.**
More features land before the leading zero goes: Annotations at least. The
version number tracks the compatibility of the interface, not the length of the
feature list.

**What a tag promises is the data.** Migrations are forward-only and apply
themselves on startup, the whole state is one directory, and backup is copying
it (ADR 0004). Upgrading is pulling a tag and restarting, with no migrate step
and no manual intervention, at every version including across minors. That
guarantee is already true and already tested; the tag publishes it rather than
creating it.

**What a tag does not promise is the JSON, the URL shapes or the screens.** A
`0.x` bump may change any of them. There is no deprecation window, because
there is no published API client to deprecate against, and pretending otherwise
would cost real design freedom to protect a hypothetical consumer.

**There is no downgrade.** Forward-only migrations mean an older binary against
a newer database does not run. Stated in `docs/deployment.md` beside the backup
instructions, which is where an operator is standing when it matters.

**Releases are cut by pushing a tag, and the version lives nowhere else.**
`main.version` defaults to `"dev"`, the Makefile stamps `git describe`, the
Dockerfile takes `ARG VERSION` defaulting to `"docker"`, and goreleaser stamps
`{{ .Version }}`. No bump commit, no `version.txt`, no `package.json` version to
keep in sync (`web/package.json` is `private: true` and is the version of
nothing).

**The release notes are the changelog.** goreleaser generates them from commit
subjects and already filters `docs:`, `test:` and `chore:`.

**Images go to ghcr.io, `latest` follows the newest tag.** Never `main`.

## Considered Options

**`v1.0.0` as the first tag.** Rejected. It spends the number that will mean
something later, on a version whose API is still moving with every milestone.
The signal it would send, "this is stable", is the one claim that is false,
while the claims that are true, "this works" and "your data is safe", are
better made in prose on the README than by a digit.

**Staying untagged until the feature set is v1-complete.** Rejected, and this
is the decision that actually drives the milestone. Release machinery that has
never run is not machinery, it is a plan: the goreleaser config had a real bug
in it (a missing `rm` of stale hashed assets) and the CI had never once
compiled the TypeScript, and neither was going to be discovered by reading. The
first tag is worth cutting *because* the stakes at `0.1.0` are low. Doing it
for the first time on the day v1 is meant to ship is how a release day becomes
a release week.

**A committed `CHANGELOG.md`.** Rejected. This repository's commit bodies are
essays explaining reasoning, and they are already the real changelog; a
hand-maintained file would duplicate them at first and diverge from them by the
third tag. Keep-a-changelog earns its place where commits are terse. Here the
generated notes plus `git log` are strictly more information.

**Docker Hub, or Docker Hub in addition to ghcr.** Rejected for now. A second
registry means a second account, a `DOCKERHUB_TOKEN` to create and rotate by
hand, and a namespace that can be squatted, in exchange for discoverability
Verve is not currently short of. ghcr authenticates with the `GITHUB_TOKEN` the
workflow already holds. Adding Docker Hub later is a second login step and a
second tag list.

**Multi-arch images in the first release.** Rejected for now, and this one is
wanted. arm64 is a plausible median deployment for a self-hosted health
warehouse, and it is cheap: `CGO_ENABLED=0` means the Go stage cross-compiles
natively from `TARGETARCH` rather than crawling under QEMU. It is deferred one
release because the thing being established is that a tag yields a pullable
image at all, and a second platform is a second way for that to fail before it
has succeeded once.

**Signing, SBOM and provenance.** Deferred, not rejected. Cosign and SLSA
attestation are self-contained work and are worse when rushed into a release
that is itself running for the first time.

## Consequences

- The README's status banner changes from "no version has been tagged" to an
  explanation of what `0.x` means. It stays a banner: a leading zero invites the
  reading "unfinished", and the accurate reading is "unfrozen".
- `SECURITY.md` can answer its own open question. The newest minor receives
  fixes; nothing is backported.
- The install path stops with a `git clone`. `compose.yml` points at the
  published image, and building locally becomes the path for running an
  unreleased `main` rather than the default.
- CI now builds the SPA on every PR. This is a direct consequence of tagging:
  until now the only thing that compiled the TypeScript was a developer's
  `make ui` and goreleaser's before-hook, and the latter runs after the tag is
  pushed, which would make the tag itself the first typecheck.
- `make ci` now needs Node. `make ci-go` is the Go-only subset, and the
  independence is real rather than a convenience: the Go job's ability to build
  against the committed `dist/.gitkeep` placeholder is a tested property.
- Every future breaking change to the API is free until `1.0.0`, and this ADR
  is what makes that explicit rather than accidental. The criterion for `1.0.0`
  is a second connector having pushed on the Catalog and the API having stopped
  moving, not a feature count.
- `latest` will move under anyone who does not pin. That is the documented
  behaviour of `latest` everywhere, and `docs/deployment.md` says to pin if you
  want to choose your upgrade moment.
