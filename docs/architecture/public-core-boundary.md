# Public-core boundary

Status: release-candidate decision record.

## Product promise

bmanga core is a source-agnostic, local-first catalog and reader for
page-oriented image libraries that the operator is authorized to access. The
service reads library mounts, writes catalog/cache/personal state only to its
application-data location, and exposes an authenticated web UI.

## Included

- An explicit scanner for image folders, ZIP/CBZ image archives, and
  image-based EPUB files.
- SQLite catalog identities that remain stable across rescans.
- Local catalog, search, series, detail, reader, progress, pagination, and
  personal-mark workflows.
- Archive path and resource-limit checks.
- Exact cover duplicate evidence and manual corrections.
- A bounded, read-only intake/library comparison tool that emits a private
  review plan but cannot apply, move, overwrite, quarantine, or delete files.
- Local presentation-only metadata overrides for title, creator, series label,
  and language; scanned metadata and source files remain unchanged.
- Local runtime diagnostics limited to the application version, process uptime,
  a bounded database health check, and aggregate application-cache file/byte
  counts and scan-error totals.
- A V2-only web application and a loopback-first container profile.

## Excluded

- Provider-specific search, scraping, comments, authentication, or downloads.
- External account/progress synchronization.
- Import apply, quarantine, source move/overwrite/deletion, and NAS maintenance
  automation.
- Public-access, DDNS, tunnel, certificate, or reverse-proxy automation.
- GPU/model jobs and automated similarity decisions.
- PDF, PDF-inside-ZIP, 7z, and MOBI helpers in the public-core image.
- Private deployment history, data, caches, logs, screenshots, credentials,
  policies, manifests, and operational handoffs.

## Safety invariants

1. A scan never modifies a configured library root.
2. Container library mounts are read-only.
3. Unsupported formats fail closed and are never advertised as readable.
4. Source paths and catalog metadata are private user data and never belong in
   issue templates, fixtures, telemetry, or release artifacts.
5. Network adapters are absent from core. A future adapter must be separately
   enabled, documented, licensed, threat-modeled, and tested against provider
   terms before it can ship.
6. Destructive source operations are not a core extension point.
7. An existing database with active legacy cleanup, quarantine, deletion, or
   author-exclusion decisions is rejected before schema migration. The public
   core never silently ignores, deletes, or rewrites those decisions.
8. The final container remains a CGO-disabled `scratch` runtime. Adding CA
   certificates, timezone data, OS user lookup, external helpers, or another
   base filesystem requires a new capability, security, and license review.
   Compose must not inject a host init binary, and Docker builds must reject
   platforms without a separately reviewed artifact/license inventory.
9. Runtime diagnostics are read-only and local. They never invoke a shell,
   inspect sockets, probe external services, trigger refresh or cleanup work,
   or return absolute paths and underlying error text. Cache inspection is
   bounded and reports only aggregate counts.
10. Import planning is a dry run over explicit, disjoint trees inside one
    security root. It rejects links, reparse points, special files, boundary
    escapes, and configured resource-limit overruns; it has no apply endpoint.
11. Metadata overrides affect only local presentation state. They never rename
    a catalog series, mutate scanner output, or write to a library mount.

## Extension rule

An optional component must communicate through an explicit capability
boundary, declare every network and filesystem permission, ship its own
dependency notices and SBOM, and remain disabled unless the operator installs
and configures it. Build tags alone are not considered sufficient separation
for code that would otherwise be distributed in the core source archive.
