# Reader layout and responsive details

The public reader supports English, Simplified Chinese, and Japanese. These
changes apply only to supported local image formats; they do not add online
providers, downloads, PDF support, or access to another installation's data.

## Layout and image quality

- **Auto** is the default when no layout preference has been saved. A decoded
  page at least three times as tall as it is wide uses scrollable width fit;
  ordinary pages fit within the viewport. Explicit saved preferences still win.
- **Fit width** requests a horizontal pixel budget based on viewport width and
  device pixel ratio. It does not shrink a whole long strip to a fixed height
  and then stretch that tiny thumbnail across the screen.
- `/page?max_width=N` caps the requested width at 3200 pixels. Width-derived
  images have separate cache keys from existing longest-edge `max=N` images.
  Sources already within the requested width retain their original bytes.
- Width derivatives have a 128 MiB estimated working-memory budget checked
  before full decoding. An otherwise valid source exceeding that budget is
  served unchanged instead of allocating a very large resize buffer; existing
  input byte/pixel limits still apply. This is not a process-wide RSS limit.
- Changing the layout reloads the matching derivative. A width change preserves
  a content-relative scroll anchor; a height-only browser-chrome change does not
  reset the reading position to the top.
- Merely reaching the last long-image page does not mark the work completed:
  its scrollable content must also reach the bottom, or the reader's explicit
  ending flow must be used.

There is no upscaling enhancement: a low-resolution original remains limited by
its source pixels. Very large source images remain subject to existing resource
limits and browser decoding capacity. This is not a tiled-image implementation.

## Series detail loading

The title, directory, and marks can appear before the saved reading position has
been confirmed. Progress has distinct checking, ready, and error states; a
failed request is not treated as an unread work. Retrying the progress check
does not reload the directory or replace an unsaved note. After an error, an
individual chapter can still be selected; the reader checks its real manifest.

`GET /api/series-progress?id=...&manifest=stored` reads the saved summary without
opening source archives to discover pages. It does not change progress data.
The ordinary progress/reader endpoints still validate the actual page manifest
before resume or save. Late responses are fenced to their opening session, and
newer acknowledged progress is retained when a summary finishes later.

The main series query limits aggregate and cover-ranking inputs to the selected
group. Synthetic benchmarks are available with:

```sh
go test ./internal/prototype -run '^$' -bench BenchmarkSeriesDetailSelectedGroup -benchtime=3x
```

This measures the SQL path, not a promise about end-to-end load time over a
network. Source storage, image decoding, and connection latency still matter.

Background progress or mark changes do not refocus the detail header or reset a
user-selected directory range/fold. **Locate in directory** explicitly reveals
the current chapter when it is folded or outside the displayed range.

## Regression coverage

Go tests exercise selected-group queries, cancellation, legacy/NULL cases,
stored-summary identity mapping, image pixels and cache-axis separation. The
browser tests use generated catalogs and colored long strips on an owned
loopback server. They reject unexpected API and external-origin requests.
Playwright is installed separately as test tooling and is not shipped in the
runtime container. See [browser test setup](../../web-v2/tests/README.md).
