## Outcome

Describe the user-visible result and why it belongs in the source-agnostic core.

## Safety and compatibility

- [ ] No credentials, private paths, personal data, databases, logs, or copyrighted media are included.
- [ ] Source-library access remains read-only unless a separately reviewed write boundary is involved.
- [ ] New network/provider behavior is absent or has an explicit terms and threat-model review.
- [ ] New dependencies are documented in `THIRD_PARTY_NOTICES.md` and are SBOM-ready.

## Verification

List the tests, builds, and synthetic fixtures used to verify the change.
