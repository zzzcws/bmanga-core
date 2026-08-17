# Reviewed documentation assets

This directory contains UI screenshots captured from a real `bmanga-core`
instance populated with synthetic demonstration data. The abstract cover artwork
was created with OpenAI image-generation assistance from prompts for wholly
fictional demo art; it does not depict or reproduce an actual comic. The
screenshots do not contain third-party comic pages, a real library, user records,
or operational data. This provenance note records how the assets were made and
is not a legal conclusion about rights or licensing.

`manifest.json` is a narrow, fail-closed allowlist for PNG documentation assets.
The publication safety checker permits a PNG here only when all of the following
remain exact:

- direct path under `docs/assets/`;
- byte length and SHA-256;
- PNG signature, chunk framing, chunk CRCs, and IHDR dimensions;
- the complete set of PNG paths on the prospective public Git surface.

Other binary formats and PNGs absent from the manifest remain blocked. Manifest
records cannot point outside this directory or into a nested directory.

## Human review boundary

The checker does not OCR pixels, interpret visible text, determine copyright, or
inspect PNG chunk payloads for semantic privacy issues. The hash proves only that
the bytes are identical to the version a maintainer reviewed; it does not perform
that review.

Before changing a screenshot or its manifest record, a maintainer must:

1. use only synthetic demo content with rights to publish;
2. remove unnecessary metadata;
3. visually inspect the entire image for personal or operational information;
4. inspect the file's metadata/chunk inventory;
5. update path, size, SHA-256, and dimensions in the same change;
6. run `python -m unittest tools/check_github_upload_safety_test.py` and
   `python tools/check-github-upload-safety.py --strict-paths`.
