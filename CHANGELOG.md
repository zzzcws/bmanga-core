# Changelog

All notable changes will be documented in this file. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning policy will
be finalized before the first release.

## Unreleased

### Added

- Isolated publication-candidate source tree with no private runtime database or media.
- V2-only clean-checkout web build.
- Go-only multi-stage container and read-only Compose deployment skeleton.
- Explicit tools-only Compose scanner profile; source libraries stay read-only.
- Isolated Linux-container preflight for authenticated service startup, V2 asset
  serving, and an empty-library scan through Compose.
- Initial contribution, security, conduct, governance, support, and dependency-notice policies.
- Apache License 2.0 project licensing and the final
  `github.com/zzzcws/bmanga-core` repository/module identity.
- A hash-verified third-party license-text bundle for the current
  Linux/amd64, CGO-disabled binaries and browser artifact profile.
- A `scratch` final container stage running as numeric user `65532:65532`,
  with no host init injection and a pre-build Linux/amd64 license-profile
  guard; verified with synthetic scanner, authenticated service, and session
  writes.

### Security

- Publication CI rejects tracked secrets, private path markers, and runtime artifacts.
- Provider/network adapters are absent; the candidate container also excludes document/archive helper runtimes.
- Login failures return a generic response and no longer persist client,
  username-match, or password-length diagnostics.
- Go was raised to 1.26.6 and `golang.org/x/image` to 0.45.0; source and binary
  vulnerability scans report no reachable findings.
- The scanner rejects any configuration that would place its SQLite database
  inside a source-library root.
- Removed the author-exclusion API and cleanup/quarantine catalogue surface.
  Startup now fails closed before migration when an existing database contains
  active legacy visibility decisions, without deleting or rewriting those rows.

### Known blockers

- Container SBOM, human review of the current Linux/amd64 license mapping, and
  signed provenance are not yet release-complete. Additional platform bundles
  are required only if support for those artifact targets is proposed.
- Independent clean-Linux-host Compose validation remains open; the local
  Docker Desktop Linux preflight is complete.
