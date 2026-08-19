# Third-party license-text bundle

This directory preserves the original license and patent files mapped in
`manifest.json`. The manifest records the component, exact version, artifact
role, source locator, byte count, and SHA-256 for every included file.

The current snapshot represents only the checked-in `linux/amd64`,
`CGO_ENABLED=0` container profile. Its final image starts from `scratch`; build
images are not copied into the final filesystem. The Dockerfile rejects any
other target before compiling, and Compose does not inject a host init binary.
Web assets are built with exact Node.js 24.19.0 from the digest-pinned Node
build image; neither Node.js nor npm is copied into the final image.
The mapped components are:

- Go 1.26.6 and the ten external modules linked into both Go binaries;
- React, React DOM, and Scheduler in the browser bundle;
- Vite's injected runtime and Rolldown's injected module-preload polyfill.

Rolldown's npm package refers to, but does not include, its supplemental
`THIRD-PARTY-LICENSE`. The copy here comes from the versioned upstream `v1.1.5`
tag; its fixed URL and reviewed SHA-256 are recorded and verified.

## Verification and regeneration

Verify the checked-in bytes and locked inputs without network access:

```sh
python tools/check-third-party-licenses.py --verify
```

CI also rebuilds both Go binaries to verify linked modules and compares the
installed npm production tree and original package license files:

```sh
python tools/check-third-party-licenses.py --verify --verify-go-linkage
npm --prefix web-v2 ci
python tools/check-third-party-licenses.py --verify --verify-web-sources
```

Maintainers can regenerate this exact profile after installing Go 1.26.6 and
running `npm --prefix web-v2 ci`:

```sh
python tools/update-third-party-licenses.py
```

Regeneration fetches only Rolldown's missing supplemental notice from a fixed
tag and verifies its reviewed SHA-256. All other text comes from the exact Go
toolchain/module caches and locked npm installation.

## Review status and fail-closed transitions

Hash verification proves only that the reviewed input files have not changed.
It is not a legal opinion. The current profile was technically reviewed by
OpenAI Codex under explicit maintainer delegation and publication authority;
the exact conclusions and obligations are recorded in
`LICENSES/reviews/linux-amd64.json`. The manifest is release-ready only for
this reviewed Linux/amd64 artifact profile.
The modernc.org/sqlite 1.56.0 upgrade also has a separate structured artifact-
boundary review at
`LICENSES/reviews/sqlite-v1.56.0-linux-amd64-technical.json`. It records and
pins the evidence that the optional `modernc.org/sqlite/vec` package is present
in the downloaded module but is not imported, linked, or copied into the
scratch runtime image. Adding that package later requires its sqlite-vec
license text and a new review.
The schema label `human-reviewed` denotes that the maintainer-authorized review
gate is complete; the decision's `reviewedBy` field explicitly identifies the
delegated AI technical reviewer rather than implying legal-counsel review.

The current intended release scope is exactly `linux/amd64`,
`CGO_ENABLED=0`, and its inventory is present. Another `GOOS/GOARCH` becomes a
new release blocker only if maintainers decide to publish that target; its
inventory must be generated and reviewed before the Dockerfile guard changes.

`python tools/check-third-party-licenses.py --release-readiness` fails whenever
the review record, component inventory, or evidence hashes do not match. Do
not change the state merely to make a release job pass.

A completed review is recorded as one structured JSON decision under
`LICENSES/reviews/`. It contains exactly these top-level fields:
`schemaVersion`, `artifactProfileSha256`, `reviewedBy`, `reviewedAt`, and
`components`. `artifactProfileSha256` is the SHA-256 of the UTF-8 JSON encoding
of the current `artifactProfile` using sorted keys, no insignificant
whitespace, and `ensure_ascii=false`. `reviewedAt` must be a real RFC 3339 UTC
timestamp ending in `Z`.

The decision must cover the exact manifest component set. Every component
records its `id`, `decision: "approved"`, a non-placeholder
`licenseConclusion`, a non-empty list of explicit `obligations`, and the exact
set of mapped license/notice `path` and `sha256` values. Every manifest
component then changes to `human-reviewed` and references the same decision
through `reviewEvidence.decisionFile` and the decision file's exact
`reviewEvidence.sha256`; partial transitions are rejected. Only after that may
readiness become true with no blockers.

These machine checks prove structural completeness, exact inventory binding,
and evidence-file integrity only. They do not turn a technical review into
legal advice or independently prove its conclusions. Publication authority is
recorded by the maintainer; release authenticity is enforced through tag-gated,
pinned, minimum-permission GitHub workflows, immutable digests, build
attestations, and image signatures.
