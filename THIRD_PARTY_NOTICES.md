# Third-party notices — reviewed Linux/amd64 artifact

bmanga-core itself is licensed under Apache License 2.0; see `LICENSE`.
Third-party components retain their own terms. Original license and patent
files are preserved under `LICENSES/` and mapped to exact component versions,
artifact roles, source locators, byte counts, and SHA-256 values in
`LICENSES/manifest.json`.

This inventory and its decision record are a maintainer-authorized, Codex-
assisted technical review, not legal advice. The exact component mappings and
obligations are approved for the `linux/amd64`, `CGO_ENABLED=0` profile. Adding
another published platform requires a new platform-specific inventory and
review before that platform is enabled.

The OCI aggregate license expression for this image is
`Apache-2.0 AND BSD-2-Clause AND BSD-3-Clause AND CC0-1.0 AND ISC AND MIT AND LicenseRef-SQLite-Public-Domain`.
`LicenseRef-SQLite-Public-Domain` refers to
`LICENSES/go/modernc.org/sqlite@v1.56.0/SQLITE-LICENSE`.

## Covered artifact profile

The checked-in profile is the final `linux/amd64`, `CGO_ENABLED=0` container.
Both Go binaries were inspected with `go version -m`; they link the same ten
external modules:

- `github.com/dustin/go-humanize` 1.0.1
- `github.com/google/uuid` 1.6.0
- `github.com/remyoudompheng/bigfft` 24d4a6f8daec
- `golang.org/x/image` 0.45.0
- `golang.org/x/sys` 0.47.0
- `golang.org/x/text` 0.41.0
- `modernc.org/libc` 1.74.4
- `modernc.org/mathutil` 1.7.1
- `modernc.org/memory` 1.11.0
- `modernc.org/sqlite` 1.56.0

The bundle also preserves Go 1.26.6 `LICENSE` and `PATENTS`, the `PATENTS`
files for the `golang.org/x/*` modules, and applicable modernc supplemental
files including `LICENSE-3RD-PARTY.md`, `LICENSE-GO`, `LICENSE-MMAP-GO`, and
`SQLITE-LICENSE`.

The downloaded `modernc.org/sqlite` 1.56.0 module also contains the optional
`modernc.org/sqlite/vec` package, which bundles sqlite-vec 0.1.9. Neither
shipped entrypoint imports that side-effect package: the reviewed Linux/amd64
import graphs contain only `modernc.org/sqlite`, `modernc.org/sqlite/lib`, and
`modernc.org/sqlite/vtab`, and symbol inspection finds no `sqlite/vec` package
in either binary. The scratch image copies only those binaries, generated web
assets, and the mapped license bundle, not the Go module cache or source tree.
Accordingly sqlite-vec code is not part of this artifact. Importing or
distributing it later requires adding its upstream license text and completing
a new artifact review. The structured evidence is recorded in
`LICENSES/reviews/sqlite-v1.56.0-linux-amd64-technical.json`.

The browser production tree contains React 19.2.7, React DOM 19.2.7, and
Scheduler 0.27.0. The built bundle additionally contains Vite 8.1.4 injected
runtime code and Rolldown 1.1.5's module-preload polyfill, so their complete
published license files are included. Rolldown's npm archive refers to but
omits `THIRD-PARTY-LICENSE`; the bundle adds the exact file from the versioned
upstream `v1.1.5` tag and pins both its URL and reviewed SHA-256.

## Container boundary

The final image uses `scratch` and numeric user `65532:65532`. It contains only
the two static Go binaries, generated web assets, the project license, this
notice, and the `LICENSES/` tree. Node and Go images are build inputs and their
filesystems are not copied into the distributed runtime image. Compose does not
enable Docker's host-provided init injection.

The Dockerfile rejects any target other than `linux/amd64` before compilation,
and both Go builds receive the checked `TARGETOS` and `TARGETARCH` explicitly.
Supporting another platform requires generating and reviewing that platform's
artifact inventory first; it is not an override to the current guard.

This narrow boundary is valid only while the service remains CGO-disabled and
does not require runtime CA certificates, timezone data, operating-system user
lookup, or external helper programs. A change that adds any such capability
must re-open the runtime image and license review.

## Verification

The integrity check is network-free and fails on a changed lockfile, changed
Docker runtime boundary, missing/unmapped text, or mismatched hash:

```sh
python tools/check-third-party-licenses.py --verify
```

CI additionally rebuilds both Go binaries and compares the locked npm
production tree to the manifest. Release automation must also run:

```sh
python tools/check-third-party-licenses.py --release-readiness
```

That command fails if the reviewed decision, mapped files, or readiness state
diverge. The platform guard remains a conditional gate: a new target cannot be
enabled until its own inventory has been generated and reviewed.

## Deliberately excluded from this runtime

- PyMuPDF/MuPDF and `fitz`-based PDF helpers
- Python and local wheel bundles
- `p7zip`, `py7zr`, and their transitive codecs
- Calibre/`ebook-convert` and MOBI conversion caches

A future optional image or helper needs its own source offer, original license
texts, SBOM, security boundary, and compatibility review. Process separation
alone must not be treated as resolving license obligations.
