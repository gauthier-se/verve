Status: done

# 04: docs: what a published version changes, and ADR 0029 on what 0.x promises

Depends on 01, 02, 03. Lands in the same PR as the tag.

## What

- **ADR 0029, "A 0.x tag promises the data, not the interface".** The decision
  and its reasoning:

  The first tag is `v0.1.0` and not `v1.0.0`. Verve has one connector, one
  user in practice, and an API whose shape is still being pulled at by every
  milestone: the workouts read path just added an entity endpoint that departs
  from `/v1/series` window semantics on purpose, and the next connector will
  push on the Catalog. Calling that `1.0.0` under semver means either freezing
  it or lying about it.

  What the tag *does* promise, because it is already true and tested:
  migrations are forward-only and self-applying (`cmd/verve/serve.go`), the
  data directory is the whole state, and backup is copying a folder (ADR 0004).
  Upgrading is pulling a tag and restarting. That is the guarantee a
  self-hosted app owes its user, and it is orthogonal to whether the JSON has
  settled.

  Rejected alternatives to record, each with its reason:
  * **`v1.0.0` as the first tag.** Signals a stability commitment that a
    one-connector app cannot honour, and spends the number that will actually
    mean something later.
  * **A committed `CHANGELOG.md`.** goreleaser generates notes from commit
    subjects and already filters `docs:`, `test:` and `chore:`. This repo's
    commit *bodies* are the reasoning, and they are the real changelog; a
    hand-kept file would duplicate them at first and diverge from them by the
    third tag. The release page is the changelog.
  * **`latest` tracking `main`.** An image that moves under a `compose pull`
    breaks instances without a version to point at.
  * **Docker Hub in addition to ghcr.** A second registry, a second account, a
    second credential, for an audience that is already on the repository.
  * **Signing and SBOM in this milestone.** Wanted, self-contained, and worse
    if rushed at tag time.

- **`SECURITY.md`, "Supported versions"** currently reads "Verve has not
  reached a tagged release. Only the current `main` branch is supported [...]
  Once versions are published this section will say which ones receive fixes."
  It answers itself: on `0.x`, the newest minor receives fixes, and there is no
  backporting to older minors. Say it in those words, with the reason (one
  maintainer, side project) that `SECURITY.md:21` already gives for its
  response times. A reporter should be told which version to test against, and
  the answer is "the newest tag, or `main`".

- **The README's status block.** The
  `> **Status: pre-release.** [...] install today means Docker Compose or a
  build from source` block goes. What replaces it is not another banner: it is
  the install line, because the block existed only to explain the absence of
  one. Keep one sentence naming the version scheme, so a reader knows `0.x`
  means the interface can still move and their data will not.

- **`CONTRIBUTING.md` gains a short release section**, after "Packaging" in the
  architecture list or as its own heading: a release is `git tag vX.Y.Z` on
  `main` and a push, which triggers the binaries and the image; the version is
  never written into a file; the changelog is generated. Four sentences, so the
  second maintainer does not have to read two workflow files to find out.

- **`ROADMAP.md`**: the release leaves "Next", which then holds Annotations
  alone. The "Shipped" table gains no row: a release is not a milestone of
  capability, and the table is about what Verve can answer. Instead, the
  paragraph under "Next" stops saying that installing means cloning.

## Why here

Every sentence in this issue is false until the tag exists, and every one of
them is a sentence someone reads *before* deciding to install. The pre-release
banner is currently the second thing on the README, which is honest today and
becomes the loudest wrong statement on the page the moment `v0.1.0` is
published.

The ADR is worth writing even though the decision looks small, because "why is
this still 0.x" is a question that will be asked again at every milestone, and
the answer is a criterion (one connector, an API still moving) rather than a
mood. Written down, it also names what would justify `1.0.0`.

## Done when

- No file in the repository claims Verve has no tagged release.
- `SECURITY.md` names which versions receive fixes.
- The README's first install instruction does not require a git checkout.
- ADR 0029 exists and is linked from the ADR index if one is kept.

## Comments

All the writing landed: ADR 0029, the `SECURITY.md` supported-versions section
answering its own open question, the README status block, the CONTRIBUTING
"Cutting a release" section, the ROADMAP, and the forward-only upgrade note in
`docs/deployment.md` beside the backup instructions.

Two adjustments to the spec above, both because the milestone's scope changed
after it was written:

- **The ROADMAP does get a Shipped row**, "Releases", against what this issue
  said. The reasoning it gave (a release is not a capability, and the table is
  about what Verve can answer) is right about the *release* and wrong about the
  *machinery*: CI compiling the front end and a tag publishing an image are
  work that shipped, and leaving the table silent about them would make the
  next reader wonder whether it happened.
- **The README banner stayed a banner** rather than being deleted. A leading
  zero invites the reading "unfinished"; the accurate reading is "unfrozen",
  and that needs a sentence.

`npm audit fix` was taken while here: nanoid and postcss patch bumps, lockfile
only, `web/package.json` untouched. The remaining esbuild advisory
(GHSA-67mh-4wv8-2f99, dev-server only) needs a Vite major and is deliberately
not in a release milestone.

**The tag itself is not pushed.** That is a person's decision, not this issue's,
and it is the last step: `goreleaser build --snapshot --clean` as a dry run,
then `git tag -a v0.1.0 && git push origin v0.1.0`.
