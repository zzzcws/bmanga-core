# Release checklist

The project is licensed under Apache-2.0. Every source and binary distribution
must include the root `LICENSE` and all notices required by shipped third-party
components.

## Source and privacy

- Work from a clean checkout of the public repository, never from a private
  deployment directory.
- Confirm the initial-content rights attestation remains accurate and review
  the origin and publication rights of every newly added first-party asset.
- Confirm the publication safety gate and gitleaks pass with no allowlist added
  solely to silence a finding.
- Run the publication gate with a private project-specific literal dictionary
  stored outside the repository. Use `--privacy-terms-file PATH` (or
  `BMANGA_PRIVACY_TERMS_FILE`) and retain only the redacted report; never check
  the dictionary or its raw terms into Git or upload them with an application.
- Confirm the reachable Git history contains no credentials, private paths,
  user metadata, screenshots, database content, or third-party media.
- Confirm every tracked binary or generated asset has documented provenance
  and redistribution terms.

## Correctness and security

- Run all Go tests, vet, govulncheck, frontend tests, typecheck, build, and the
  complete npm audit.
- Run the synthetic scan-to-reader integration test on every supported
  platform.
- Verify unsupported formats fail closed and provider/network adapters are
  absent from the core binary and web bundle.
- Review changes to authentication, archive limits, path boundaries, and
  source-mount permissions.

## Supply chain

- Build only from the committed lockfiles and digest-pinned Node/Go build
  images and Dockerfile frontend; keep the final runtime at `scratch` unless a
  new runtime dependency is separately reviewed.
- Generate and review CycloneDX source SBOMs and an SBOM for each final
  container architecture.
- Run `python tools/check-third-party-licenses.py --verify` and the linkage/web
  source modes used by CI. Review every original text and component mapping.
- The first artifact profile is intentionally limited to Linux/amd64 with CGO
  disabled, and its inventory is checked in. Regenerate
  `LICENSES/manifest.json` for any newly proposed `GOOS/GOARCH`; never reuse
  the Linux/amd64 mapping as evidence for another platform. Change the current
  Dockerfile guard only together with that reviewed per-platform manifest.
- Bundle the project license, `THIRD_PARTY_NOTICES.md`, and the complete
  `LICENSES/` tree in every binary/container distribution.
- Run `python tools/check-third-party-licenses.py --release-readiness`; a
  nonzero result is a non-waivable release stop, not an instruction to flip the
  manifest state.
- Record source revision, build parameters, build-image digests, binary hashes,
  license-manifest hash, and SBOM hashes in GitHub build provenance and the
  release record.
- Publish only from an immutable release tag; attach an SBOM attestation and
  sign the image digest with keyless Cosign identity.

## Release evidence

- Update the changelog and supported-version table.
- Link the passing required checks from the release record.
- Keep a synthetic, reproducible smoke transcript; never attach a real catalog
  or operational log.
- Record any time-bounded exception with an owner and removal date. Security,
  privacy, license, and unknown-provenance findings are not waivable.
