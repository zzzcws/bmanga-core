# Local metadata display overrides

`metadata-overlay-lite` lets an operator correct how one indexed work is shown
without renaming, moving, rewriting, or deleting anything in the configured
library root.

## Scope

The public write API accepts exactly four display fields:

- `title`
- `creator`
- `series`
- `language`

The `series` field is the series label shown for that work. It does not rename
or mutate a catalog series group. Overrides are stored against the stable work
identity, so a rescan can replace the current candidate path without replacing
the local display choice. Clearing an override restores the value derived from
the scanner and structured catalog data.

## API

`GET /api/metadata-overrides?target_type=work&target_id=<candidate-id>` returns
the four active public override fields for the current work identity.

`POST /api/metadata-overrides` accepts one field at a time:

```json
{
  "target_type": "work",
  "target_id": "candidate-id",
  "field_name": "title",
  "field_value": "Local display title"
}
```

An empty `field_value` clears that field. The request body is fail-closed: all
four keys are required, unknown keys are rejected, values must be strings, and
only the documented field names are accepted. Normal same-origin write-token
protection applies.

## Deliberate exclusions

This capability has no provider, scraper, account, network, or batch-import
integration. It cannot submit provenance identifiers, change scanned metadata,
or write to a source library. Compatibility-only metadata fields already found
in an existing database remain read-only through this API.
