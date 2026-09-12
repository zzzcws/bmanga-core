# Synthetic browser regression tests

These tests launch their own loopback Vite child and a headless Chromium browser.
Every API, cover and page request is mocked. Unexpected API requests and external
origins fail closed. Fixtures are generated titles, identities and geometric SVGs;
no library, account, source service or private deployment is required or contacted.

Use Node 24.19.0, install the existing `web-v2` lockfile with `npm ci`, and keep
browser tooling separate from application dependencies:

```sh
npm install --prefix /tmp/bmanga-ui-tools --ignore-scripts --no-audit --no-fund --package-lock=false playwright-core@1.59.1
node /tmp/bmanga-ui-tools/node_modules/playwright-core/cli.js install --with-deps chromium
export BMANGA_UI_SMOKE_TOOL_ROOT=/tmp/bmanga-ui-tools
cd web-v2
node tests/detail-opening-ui-smoke.mjs
node tests/webtoon-ui-smoke.mjs
```

On Windows set `BMANGA_UI_SMOKE_TOOL_ROOT` to the separate tooling directory.
The harness can use installed Chrome/Edge, or `BMANGA_UI_SMOKE_BROWSER` can point
to a chosen Chromium executable. Without that override it also supports
Playwright's installed Chromium (including Linux CI). No package/lockfile edits
or runtime browser dependency are needed.

Coverage includes directory-first loading behind a deliberately held progress
response, failed-progress retry without an unread claim, stale response/session
fencing, genuine queued-progress ACKs, personal-mark responses, deliberate
directory collapse, preserved unsaved notes/focus/scroll across visibility and
resize, and 320px detail layout. The reader test uses three 1200×9600 synthetic
strips, checks page order and decoded one-pixel contrast, longest-edge to
width-quality refetch, explicit fit changes, width/height resize anchors, and
absence of premature completion.

This validates Chromium rendering and request contracts with synthetic responses.
It does not claim real Safari/iOS, arbitrary comic files, source-site availability,
server thumbnail generation, or production network performance have been tested.
