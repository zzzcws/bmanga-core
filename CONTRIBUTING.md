# Contributing

Thank you for helping improve bmanga-core. The project is licensed under the
Apache License 2.0. Unless explicitly stated otherwise, contributions
intentionally submitted for inclusion in the project are accepted under the
same license, as described in Section 5 of `LICENSE`.

## Before opening an issue or pull request

- Do not attach manga pages, covers, archives, database files, logs containing
  titles or paths, credentials, cookies, tokens, or private host information.
- Use synthetic fixtures and reserved example addresses and domains.
- Do not add provider-specific downloading, authentication bypass, DRM bypass,
  or scraping behavior without an approved design and terms review.
- Keep library mounts read-only. A change that can move, overwrite, quarantine,
  or delete source files needs a separate threat model and explicit safeguards.
- Keep generated files such as `web/v2`, local databases, caches, and build
  output out of Git.

## Development checks

Run the checks that match your change:

```sh
go test ./...
go vet ./...
python tools/check-github-upload-safety.py --strict-paths --json
cd web-v2
npm ci
npm test
npm run typecheck
npm run build
```

For a clean V2 build from the repository root, run:

```sh
node tools/build-web-assets.mjs --ci
```

## Pull request expectations

A pull request should explain the user-visible outcome, safety implications,
tests performed, and any new dependencies or network access. Keep changes
focused. New dependencies require a license/source review and an update to
`THIRD_PARTY_NOTICES.md`; shipped dependencies must also appear in the SBOM.

Security fixes should follow [SECURITY.md](SECURITY.md) and must not be disclosed
in a public issue before a maintainer has assessed the report.
