# bmanga-core

bmanga-core is a local-first, self-hosted catalog and reader for manga and other
page-oriented image archives. The intended public core indexes libraries that
the operator is authorized to access, keeps the source mounts read-only, and
stores catalog and personal reading state in a local SQLite database.

> **Publication status:** this repository is a source-preview candidate and has
> not been published yet. The source is licensed under Apache License 2.0. A
> source-only public preview is intentionally separate from a supported tag,
> container, or binary release; the artifact evidence listed below must still
> be completed before the first public tag.

The founding maintainer's initial-content publication authority is recorded in
[`docs/first-party-rights.md`](docs/first-party-rights.md). Third-party
artifact mappings remain subject to the independent review described under
`LICENSES/`.

## Current candidate boundary

The checked-in container is deliberately small: it builds the Go service and
the V2 web application from a clean checkout, then copies only the static Go
binaries, generated web assets, and license bundle into a `scratch` runtime.
It contains no Python runtime or document/archive helper packages.

| Input | Candidate container |
| --- | --- |
| Image folders | Reader path available |
| ZIP/CBZ image archives | Go-native reader path available |
| One-level nested ZIP image archives | Go-native reader path available |
| Image-based EPUB | Go-native ZIP reader path available |
| PDF or PDF embedded in ZIP | Not included in this image |
| 7z | Not included in this image |
| MOBI conversion | Not included in this image |
| Provider/network adapters | Absent from the public-core image |

The generic scanner indexes only the formats listed as available above. PDF,
7z, and MOBI helpers are intentionally outside this public-core profile; any
future optional component needs a separate capability, security, and license
review.

bmanga does not bundle books, covers, account credentials, provider sessions,
or sample archives. It is not intended to bypass authentication, DRM, access
controls, or a content provider's terms. Operators are responsible for using
material they are permitted to access.

## Clean-checkout build

The source build requires Go 1.26.6 or later and Node.js 22 or later.

```sh
node tools/build-web-assets.mjs --ci
go test ./...
go vet ./...
mkdir -p out
CGO_ENABLED=0 go build -buildvcs=false -mod=readonly -trimpath -o out/bmanga ./cmd/bmanga-go
CGO_ENABLED=0 go build -buildvcs=false -mod=readonly -trimpath -o out/bmanga-scan ./cmd/bmanga-scan
```

`tools/build-web-assets.mjs` installs the exact V2 dependency lock when
`--ci` is supplied. It builds only `web-v2` and writes ignored output under
`web/v2`; it does not fetch a temporary Classic UI minifier.

To build the Go-only container:

```sh
docker build -t bmanga:local .
```

The Node and Go build images are pinned to reviewed multi-architecture manifest
digests; the final runtime has no base filesystem and runs as numeric user
`65532:65532`. Until another platform receives its own reviewed artifact and
license inventory, the Dockerfile fails closed on targets other than
`linux/amd64`. The release workflow still needs signed provenance and generated
source/image SBOMs before a tag can be published.

## Local Compose skeleton

Copy the example, replace every placeholder, and point the library variable at
an absolute directory containing only material you are authorized to read:

```sh
cp config/compose.env.example .env
docker compose --env-file .env build
docker compose --env-file .env up -d
```

The default port is published only on `127.0.0.1`. Source material is mounted
at `/libraries/main` read-only, while application data and caches use separate
Docker volumes. Do not expose the service directly to the Internet; place it
behind an authenticated TLS reverse proxy and set `BMANGA_COOKIE_SECURE=1`.

A new catalog must be initialized before the shelf contains works. Copy
`config/libraries.example.json` to the ignored local path
`config/libraries.json`. The example already uses the Compose paths
`/app/data/bmanga.sqlite` and `/libraries/main`; adjust the library metadata as
needed. Run the scanner explicitly; it is in a tools-only profile and never
starts with the server:

```sh
docker compose --env-file .env --profile tools run --rm scan
docker compose --env-file .env up -d bmanga
```

The scanner can also run as `bmanga-scan -config config/libraries.json` in a
source build. For that mode, replace the two container paths with authorized
absolute host paths. It writes only the configured SQLite catalog; the
configured library root is read-only in the Compose profile. Never reuse a
database or configuration exported from a private deployment as public sample
data.

## Local-only workflows

The public core includes three deliberately narrow local workflows:

- Work details can store presentation-only overrides for title, creator,
  series label, and language. These values live in SQLite and never rename or
  rewrite source files. See
  [`docs/features/metadata-overrides.md`](docs/features/metadata-overrides.md).
- Settings show bounded aggregate diagnostics for process uptime, database
  availability, and application-cache usage. The endpoint returns no paths or
  underlying error text and performs no network or maintenance operation. See
  [`docs/features/runtime-diagnostics.md`](docs/features/runtime-diagnostics.md).
- `bmanga-import-plan` compares an explicitly selected intake tree with an
  explicitly selected library tree and emits a private JSON review plan. It
  hashes files read-only and exposes no apply, move, overwrite, quarantine, or
  delete action. See
  [`docs/read-only-import-planner.md`](docs/read-only-import-planner.md).

The planner is a source tool rather than part of the current container image:

```sh
go run ./cmd/bmanga-import-plan \
  --root /srv/bmanga-review \
  --intake incoming \
  --library library
```

## Repository layout

- `cmd/bmanga-go` — service entry point and authentication boundary.
- `cmd/bmanga-scan` — explicit, source-agnostic catalog scanner.
- `cmd/bmanga-import-plan` — bounded, read-only intake/library comparison.
- `internal/importplan` — path-safe hashing and conflict-plan implementation.
- `internal/prototype` — catalog, reader, review, and local-state APIs.
- `web-v2` — React/Vite user interface and tests.
- `tools/build-web-assets.mjs` — V2-only production asset builder.
- `tools/check-github-upload-safety.py` — tracked-file publication gate with
  optional external, redacted project-specific privacy terms.
- `Dockerfile` and `compose.yaml` — clean-checkout, Go-only deployment profile.
- `docs/architecture/public-core-boundary.md` — capabilities and safety invariants.
- `docs/releasing.md` — privacy, supply-chain, and release evidence gates.

## Project policies

- [License](LICENSE)
- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Maintainers and governance](MAINTAINERS.md)
- [Support](SUPPORT.md)
- [Changelog](CHANGELOG.md)
- [Third-party notices and mapped original texts](THIRD_PARTY_NOTICES.md)
- [Third-party license bundle](LICENSES/README.md)

## Publication gates

A source-only preview may be made public after all of these source gates pass:

1. Keep the initial-content rights attestation accurate.
2. Create the first real commit and run the publication, project-specific
   privacy, Git-history, object, and metadata scans against that exact commit.
3. Confirm the public tree contains no runtime data, private deployment
   material, third-party media, or unreviewed copied assets.

That source preview does not authorize a tag, container, or binary release.
Artifact publication remains blocked until maintainers complete human review
of the checked-in third-party mapping, reconcile a final image SBOM or
equivalent immutable file inventory, validate the Compose image on a clean
Linux Docker host, and bind the resulting hashes to the source revision in
signed provenance. Those artifact controls do not block the source-only
preview.
