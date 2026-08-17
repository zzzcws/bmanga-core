# Third-party license-text bundle

This directory preserves the original license and patent files mapped in
`manifest.json`. The manifest records the component, exact version, artifact
role, source locator, byte count, and SHA-256 for every included file.

The current snapshot represents only the checked-in `linux/amd64`,
`CGO_ENABLED=0` container profile. Its final image starts from `scratch`; build
images are not copied into the final filesystem. The Dockerfile rejects any
other target before compiling, and Compose does not inject a host init binary.
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

## Deliberate fail-closed status

Hash verification proves only that the reviewed input files have not changed.
It is not a legal opinion or a compatibility determination. The manifest keeps
`releaseReadiness.ready` set to `false` until a human reviews every
component-to-file mapping and applicable obligation for the current artifact
profile.

The current intended release scope is exactly `linux/amd64`,
`CGO_ENABLED=0`, and its inventory is present. Another `GOOS/GOARCH` becomes a
new release blocker only if maintainers decide to publish that target; its
inventory must be generated and reviewed before the Dockerfile guard changes.

`python tools/check-third-party-licenses.py --release-readiness` intentionally
fails while the human-review blocker remains. Do not change the state merely
to make a release job pass.

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
and evidence-file integrity only. They do not prove the reviewer's identity,
that the stated review actually occurred, or that its legal conclusion is
correct. Reviewer authenticity must be enforced outside this checker with
CODEOWNERS review, protected-branch approval, and signed commits or signed
release attestations. Do not treat a self-authored JSON file as independent
review evidence.
