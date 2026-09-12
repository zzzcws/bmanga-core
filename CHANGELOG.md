# Changelog

All notable changes will be documented in this file. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Public tags use
Semantic Versioning; alpha releases may still change compatibility boundaries.

## Unreleased

## [0.1.0-alpha.7] - 2026-09-12

### Fixed

- Limit series-detail aggregates and cover selection to the requested series;
  display the directory before the saved reading-position check completes.
- Separate progress checking/error/unread states, allow independent retry, and
  preserve authoritative summaries and newer acknowledgements across races.
- Keep mobile detail focus, scroll position, directory folds and ranges stable
  during background updates; explicitly reveal the current chapter on request.
- Add automatic long-strip width fit and width-based image requests, preserving
  source pixels instead of stretching longest-edge thumbnails. Keep distinct
  derivative caches, bound resize working memory, and retain source bytes when
  a safe width derivative cannot be generated within the memory budget.
- Preserve scroll anchors when the viewport changes and avoid marking the last
  long strip complete before its scrollable content has been read.

### Verification

- Add synthetic Go query, progress, image and resource-budget regressions plus
  isolated mobile Chromium detail/reader tests in CI and release gates.
- Preserve English, Simplified Chinese and Japanese interfaces. Dependency locks,
  runtime component inventory and the local-only/read-only source boundary are
  unchanged. Online providers and private operational data are not included.

## [0.1.0-alpha.6] - 2026-09-12

### Fixed

- Materialize the exact locked Go dependency graph before verifying source
  license hashes, so release checks work in a genuinely empty module cache.
  Keep all checksum and license checks fail-closed and add regression tests.
- Include the maintenance changes below. The immutable alpha.5 source tag
  was stopped by this gate before image construction; no alpha.5 container
  or GitHub release was published. That tag is retained for the audit trail.

## [0.1.0-alpha.5] - 2026-09-12 (candidate; not published)

### Changed

- Update the Go 1.26 toolchain to 1.26.8 with an exact-version guard and a
  verified immutable builder digest; retain the supported 1.26 line rather
  than mixing an unreviewed Go 1.27 migration into routine maintenance.
- Upgrade `modernc.org/sqlite` to 1.58.0, `modernc.org/libc` to 1.75.6, and
  `modernc.org/memory` to 1.12.1 with refreshed Linux/amd64 artifact evidence.
- Update `@vitejs/plugin-react` to 6.1.1, `@types/react-dom` to 19.2.7, and
  Node 24-aligned `@types/node` to 24.13.4. Keep Node type major upgrades
  separate from patch maintenance and tied to a reviewed builder migration.
- Update pinned SBOM and provenance actions. Explicitly disable optional
  artifact storage records so the existing minimum-permission release
  workflow retains its previous scope.

- Upgrade `modernc.org/sqlite` from 1.56.0 to 1.57.0 with regenerated
  Linux/amd64 linkage, license, and artifact-boundary evidence. The optional
  `modernc.org/sqlite/vec` payload remains excluded from both shipped binaries
  and the scratch image.
- Upgrade Vite from 8.2.1 to 8.2.2 and `@vitejs/plugin-react` from 6.0.5 to
  6.1.0 with regenerated browser-source and license evidence. The reviewed
  production web output remains byte-identical.

## [0.1.0-alpha.4] - 2026-08-25

### Changed

- The reviewed frontend build chain now uses exact Node.js 24.19.0 from a
  digest-pinned Node 24 Active LTS image in Docker and CI, while the final
  runtime remains a Node-free `scratch` image.
- Frontend dependencies now use Vite 8.2.1, React 19.2.8, and Node 24-aligned
  type definitions; the reviewed browser artifact component set remains
  explicit and separate from type-only build inputs.
- GitHub checkout, Go, Node.js, Python, and artifact-upload actions now use
  reviewed major-version updates pinned to full commit SHAs.

### Security

- Upgrade `modernc.org/sqlite` from 1.50.0 to 1.56.0 for the upstream
  journal-rollback corruption fix and pin its required `modernc.org/libc`
  1.74.4 dependency. Regenerated license/linkage evidence proves the optional
  `modernc.org/sqlite/vec` package is not imported, linked, or shipped in the
  reviewed scratch image.

## [0.1.0-alpha.3] - 2026-08-17

### Added

- Explicit English, Simplified Chinese, and Japanese interface selection across
  the main web application and sign-in page. Simplified Chinese remains the
  default; the selected locale persists locally and synchronizes across tabs.
- Localized navigation, actions, validation, status messages, document titles,
  metadata descriptions, and accessibility labels without translating user
  book content or catalog metadata.
- A complete Japanese README and consistent English / 简体中文 / 日本語
  navigation across all three project introductions.

## [0.1.0-alpha.2] - 2026-08-17

### Added

- Product-first English and Simplified Chinese README paths with a pinned GHCR
  quick start, early-tester feedback links, and fully synthetic interface
  screenshots.

### Fixed

- Cover delivery for freshly scanned public-core databases now follows the
  actual `cover_assets` schema instead of querying a removed private-only
  relative-path column.

## [0.1.0-alpha.1] - 2026-08-17

### Added

- Isolated publication-candidate source tree with no private runtime database
  or media.
- V2-only clean-checkout web build.
- Go-only multi-stage container and read-only Compose deployment skeleton.
- Explicit tools-only Compose scanner profile; source libraries stay read-only.
- Isolated Linux-container preflight for authenticated service startup, V2 asset
  serving, and an empty-library scan through Compose.
- Initial contribution, security, conduct, governance, support, and
  dependency-notice policies.
- Apache License 2.0 project licensing and the final
  `github.com/zzzcws/bmanga-core` repository/module identity.
- A hash-verified third-party license-text bundle for the current
  Linux/amd64, CGO-disabled binaries and browser artifact profile.
- A `scratch` final container stage running as numeric user `65532:65532`,
  with no host init injection and a pre-build Linux/amd64 license-profile
  guard; verified with synthetic scanner, authenticated service, and session
  writes.
- A bounded, read-only import planner that compares explicit intake and library
  trees by whole-file SHA-256 without exposing an apply operation.
- Presentation-only title, creator, series-label, and language overrides stored
  in SQLite without changing scanned metadata or source files.
- Local runtime diagnostics limited to process uptime, bounded database health,
  and aggregate application-cache counts and bytes.

### Security

- Publication CI rejects tracked secrets, private path markers, and runtime
  artifacts.
- Provider/network adapters are absent; the candidate container also excludes
  document/archive helper runtimes.
- Login failures return a generic response and no longer persist client,
  username-match, or password-length diagnostics.
- Go was raised to 1.26.6 and `golang.org/x/image` to 0.45.0; source and binary
  vulnerability scans report no reachable findings.
- The scanner rejects any configuration that would place its SQLite database
  inside a source-library root.
- Removed the author-exclusion API and cleanup/quarantine catalogue surface.
  Startup now fails closed before migration when an existing database contains
  active legacy visibility decisions, without deleting or rewriting those rows.

### Release boundary

- The first container is a Linux/amd64, CGO-disabled pre-release. It carries
  the reviewed license bundle and is published only from an immutable tag with
  an image SBOM, build attestation, keyless signature, and synthetic smoke
  evidence. Other platforms remain fail-closed until separately inventoried.
