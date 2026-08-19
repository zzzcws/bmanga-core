# Changelog

All notable changes will be documented in this file. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Public tags use
Semantic Versioning; alpha releases may still change compatibility boundaries.

## Unreleased

### Changed

- The reviewed frontend build chain now uses exact Node.js 24.19.0 from a
  digest-pinned Node 24 Active LTS image in Docker and CI, while the final
  runtime remains a Node-free `scratch` image.

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
