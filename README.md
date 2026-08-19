# bmanga-core

**English** · [简体中文](README.zh-CN.md) · [日本語](README.ja.md)

**Turn an authorized local manga or comics folder into a local-first,
self-hosted web library with a searchable shelf, reader, and reading progress.
The supplied Compose profile mounts the source library read-only.**

[5-minute GHCR trial](#5-minute-ghcr-trial) ·
[Report a bug][bug-report] · [Ask a question][discussions]

[![Alpha release](https://img.shields.io/github/v/release/zzzcws/bmanga-core?include_prereleases&sort=semver&label=alpha)](https://github.com/zzzcws/bmanga-core/releases/tag/v0.1.0-alpha.3)
[![CI](https://github.com/zzzcws/bmanga-core/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/zzzcws/bmanga-core/actions/workflows/ci.yml)
[![Container](https://img.shields.io/badge/GHCR-linux%2Famd64-2496ED?logo=docker&logoColor=white)](https://github.com/zzzcws/bmanga-core/pkgs/container/bmanga-core)
[![License](https://img.shields.io/github/license/zzzcws/bmanga-core)](LICENSE)

> [!WARNING]
> bmanga-core is an **alpha preview**. The published container currently supports
> **Linux/amd64 only** and should not be exposed directly to the Internet.

> [!NOTE]
> The published `v0.1.0-alpha.3` image includes an English / 简体中文 / 日本語
> interface selector. It changes interface copy only; it does not translate book
> contents or catalog metadata. Simplified Chinese remains the safe default until
> a language is selected explicitly.

![Desktop home](docs/assets/home-desktop.png)

**More synthetic demo views:**

![Desktop library](docs/assets/library-desktop.png)

![Mobile library](docs/assets/library-mobile.png)

_The screenshots use synthetic demo metadata and artwork; bmanga-core ships no
importable books, standalone cover images, or sample archives. They show the
Simplified Chinese interface; the language selector is available in Settings._

## What the public core does

- Indexes authorized local image folders and supported image archives into a
  local SQLite catalog.
- Provides a web shelf, catalog search, work details, an image reader, and local
  reading progress.
- Keeps source-library mounts read-only in the supplied Compose profile; display
  metadata overrides do not rename or rewrite source files.
- Runs without provider accounts or online-provider integrations. Core catalog
  and reading workflows do not require a non-essential external service.
- Includes bounded runtime diagnostics and a source-only, read-only import-plan
  tool for comparing intake and library trees.

### Supported input boundary

| Input | Alpha public core |
| --- | --- |
| Image folders | Supported |
| ZIP/CBZ image archives | Supported |
| One-level nested ZIP image archives | Supported |
| Image-based EPUB | Supported through the Go-native ZIP reader |
| PDF, including PDF inside ZIP | Not included |
| 7z | Not included |
| MOBI conversion | Not included |
| Online providers, downloads, or sync adapters | Absent |

Unsupported formats are intentionally rejected or skipped rather than silently
routed through private helpers. See
[`docs/architecture/public-core-boundary.md`](docs/architecture/public-core-boundary.md)
for the complete boundary.

## 5-minute GHCR trial

You need Docker with Compose, a Linux/amd64 host (or a compatible amd64 Linux
VM), and a folder containing only material you are authorized to read.

```sh
git clone https://github.com/zzzcws/bmanga-core.git
cd bmanga-core
cp config/compose.env.example .env
cp config/libraries.example.json config/libraries.json
```

Edit the untracked `.env` file and set at least these values:

```dotenv
BMANGA_IMAGE=ghcr.io/zzzcws/bmanga-core:0.1.0-alpha.3
BMANGA_AUTH_USER=bmanga
BMANGA_AUTH_PASSWORD=<a-long-random-password>
BMANGA_SESSION_SECRET=<a-different-long-random-value>
BMANGA_LIBRARY_PATH=/absolute/path/to/your/authorized-library
```

Then pull the immutable alpha, scan explicitly, and start the service:

```sh
docker compose --env-file .env --profile tools pull
docker compose --env-file .env --profile tools run --rm scan
docker compose --env-file .env up -d bmanga
```

Open <http://127.0.0.1:8765> and sign in with the credentials from `.env`.
The first scan writes the catalog to the `bmanga-data` volume; it does not write
to the mounted source library. Stop the service with:

```sh
docker compose --env-file .env down
```

There is deliberately no `latest` tag during alpha. Review the
[release notes](https://github.com/zzzcws/bmanga-core/releases/tag/v0.1.0-alpha.3)
before updating a pinned version.

## Early testers wanted

The project is looking for honest feedback from people who already maintain an
authorized local manga or comics library. A useful first test is small:

1. Time the setup and first scan on Linux/amd64.
2. Try one or more supported input types.
3. Check shelf navigation, reading progress, and reader behavior on desktop or
   mobile.
4. Tell us what was confusing, slow, or broken.

[Open a bug report][bug-report], [suggest a feature][feature-request], or
[start a discussion][discussions].
Do not attach copyrighted media, credentials, private hostnames, absolute
library paths, or unredacted logs. Security reports belong in the private
channel described in [`SECURITY.md`](SECURITY.md).

## Build from a clean checkout

Source builds require Go 1.26.6 or later and Node.js 24 or later. CI and the
container build currently use the reviewed Node.js 24.19.0 toolchain exactly.

```sh
node tools/build-web-assets.mjs --ci
go test ./...
go vet ./...
mkdir -p out
CGO_ENABLED=0 go build -buildvcs=false -mod=readonly -trimpath \
  -o out/bmanga ./cmd/bmanga-go
CGO_ENABLED=0 go build -buildvcs=false -mod=readonly -trimpath \
  -o out/bmanga-scan ./cmd/bmanga-scan
```

Build the local container with:

```sh
docker build -t bmanga:local .
```

The frontend build uses the committed npm lockfile and writes ignored output to
`web/v2`. The final container contains the two static Go binaries, generated web
assets, and the reviewed license bundle; it contains no Python runtime or
Node.js/npm runtime, or document/archive helper packages.

## Local-only workflows

- **Presentation metadata overrides** store title, creator, series label, and
  language overrides in SQLite without changing source files. See
  [`docs/features/metadata-overrides.md`](docs/features/metadata-overrides.md).
- **Runtime diagnostics** expose bounded uptime, database-availability, and
  application-cache aggregates without paths or underlying error text. See
  [`docs/features/runtime-diagnostics.md`](docs/features/runtime-diagnostics.md).
- **Read-only import planner** hashes explicitly selected intake and library
  trees and writes a private JSON review plan. It has no apply, move, overwrite,
  quarantine, or delete action. It is a source tool and is not in the alpha
  container. See [`docs/read-only-import-planner.md`](docs/read-only-import-planner.md).

## Repository map

- `cmd/bmanga-go` — service entry point and authentication boundary.
- `cmd/bmanga-scan` — explicit, source-agnostic catalog scanner.
- `cmd/bmanga-import-plan` — bounded, read-only intake/library comparison.
- `internal/prototype` — catalog, reader, review, and local-state APIs.
- `web-v2` — React/Vite interface and tests.
- `tools/build-web-assets.mjs` — locked V2 production asset builder.
- `Dockerfile` and `compose.yaml` — clean-checkout Linux/amd64 deployment profile.
- `docs/releasing.md` — privacy, supply-chain, and release evidence gates.

## Safety, content, and release boundary

bmanga-core does not bundle importable books, standalone cover images, account
credentials, provider sessions, or sample archives. It is not intended to bypass
authentication, DRM, access controls, or provider terms. Operators are responsible
for using only material they are permitted to access.

The supplied Compose profile binds HTTP to `127.0.0.1`, mounts the source
library read-only, drops Linux capabilities, and runs the scratch image as
numeric user `65532:65532`. These are deployment constraints, not a claim that
every environment or untrusted archive is safe. If remote access is required,
use an authenticated TLS reverse proxy and set `BMANGA_COOKIE_SECURE=1`.

Public source and published artifacts have separate gates. Tagged alpha images
are built from an immutable commit, inspected and smoke-tested, and published
with an image SBOM, GitHub build provenance, and a keyless signature. The
founding maintainer's initial-content publication authority is recorded in
[`docs/first-party-rights.md`](docs/first-party-rights.md); the checked-in
third-party mapping and technical review records live under [`LICENSES/`](LICENSES/).
Those records are not legal advice.

## Project links

- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Support](SUPPORT.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Maintainers and governance](MAINTAINERS.md)
- [Changelog](CHANGELOG.md)
- [Apache-2.0 project license](LICENSE)
- [Third-party notices](THIRD_PARTY_NOTICES.md)
- [Third-party license bundle](LICENSES/README.md)

[bug-report]: https://github.com/zzzcws/bmanga-core/issues/new?template=bug_report.yml
[feature-request]: https://github.com/zzzcws/bmanga-core/issues/new?template=feature_request.yml
[discussions]: https://github.com/zzzcws/bmanga-core/discussions
