# Read-only import planner

`bmanga-import-plan` inventories one explicitly selected intake directory and
compares it with one explicitly selected library directory. It is a dry-run
review tool, not an importer.

The command:

- requires `--root`, `--intake`, and `--library`;
- requires the intake and library to be disjoint directories beneath the same
  explicit security root;
- opens files read-only and emits JSON only to standard output;
- never copies, moves, overwrites, renames, or deletes a source file;
- rejects symbolic links, Windows reparse points, special files, and paths that
  leave the explicit root;
- limits directory entries, regular files, per-file bytes, total bytes,
  directory depth, and relative-path length for each selected tree; and
- hashes whole files without extracting or executing archive contents.

Run the planner only while both selected trees are quiescent. The scan pins and
rechecks each object it reads, but it is not an atomic filesystem snapshot. If
an operator later decides to perform a separate import or maintenance action,
the operator must rerun the plan immediately beforehand and review the new
tree hashes and classifications. A prior plan is not authorization to act on a
tree that may have changed.

Example:

```sh
go run ./cmd/bmanga-import-plan \
  --root /srv/bmanga-review \
  --intake incoming \
  --library library
```

The JSON plan is written to standard output. If a durable review report is
needed, the operator must explicitly redirect stdout to a location outside the
intake and library trees.

## Classification

Each supported intake file is classified independently:

- `new`: no exact content match and no relative-name collision was found in
  the library or the same intake batch;
- `exact-match`: both byte size and SHA-256 match a file already present
  anywhere in the library or another file in the same intake batch;
- `name-conflict`: the normalized relative target path (Unicode NFC plus
  case-folding) already identifies different content in the library, or two
  intake paths normalize to the same target name with different content; or
- `unsupported`: the extension is outside the public core's image, ZIP/CBZ,
  and image-EPUB boundary.

`name-conflict` takes precedence over an exact match found elsewhere because
the proposed relative destination is already occupied by different content.
All paths in the JSON are relative and every match declares whether it came
from `intake` or `library`. `matchCount` is the complete number of matches for
the selected classification. To keep large duplicate groups bounded, `matches`
contains at most eight deterministic examples and `matchesTruncated` reports
whether more matches were omitted.

This first version compares files rather than interpreting books or archive
members. It does not approve an import and deliberately exposes no apply,
quarantine, cleanup, overwrite, or deletion operation.

File-kind and eligibility labels are based on the filename extension. They do
not prove that a ZIP, CBZ, or EPUB is readable, structurally valid, or safe to
consume. The normal scanner/validator remains authoritative for archive
readability and supported contents.

## Resource limits

The CLI exposes bounded overrides for:

- `--max-files`
- `--max-entries`
- `--max-file-bytes`
- `--max-total-bytes`
- `--max-depth`
- `--max-path-bytes`

The limits apply separately to intake and library. Hard ceilings in the
implementation prevent disabling the bounds with arbitrarily large values.
Directory entries are read in fixed-size batches and rejected as soon as the
configured total-entry limit would be crossed.

## Privacy

The plan contains relative filenames, sizes, and content hashes. Those are
private library metadata and should not be committed, attached to public
issues, or published as a CI artifact.
