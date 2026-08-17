# Security policy

## Supported versions

There is no supported public release yet. Security fixes are currently applied
only to the unreleased candidate branch. A supported-version table will be added
with the first release.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting feature for this repository. If
that feature is unavailable, open a minimal issue asking for a private contact
channel; do not include exploit details, credentials, private paths, library
metadata, or personal data in the issue.

Include the affected revision, impact, prerequisites, and a minimal reproduction
using synthetic data. Maintainers will acknowledge a complete report as soon as
practical and coordinate disclosure after a fix is available.

## Deployment baseline

- Bind to loopback by default and require authentication before any LAN bind.
- Use TLS at a trusted reverse proxy for remote access.
- Keep source libraries mounted read-only.
- Keep secrets in untracked files or a platform secret store.
- The public core does not ship provider adapters or online downloading. Treat
  any proposed adapter as a separate network, terms, and threat-model review.
- Do not treat containers as a sandbox for hostile documents; enforce archive,
  decompression, image-size, timeout, and filesystem-boundary limits.

The candidate container intentionally omits Python/PDF/7z/MOBI helpers. Reports
that require such a helper should identify the optional component separately.
