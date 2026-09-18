import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  READER_IMAGE_MAX_DIMENSION,
  readerImageCacheBucket,
  readerImageMaxForViewport,
  readerImageIsLongStrip,
  readerImageReadyForScroll,
  readerUsesScrollableWidthLayout,
  readerScrollAtEnd,
  readerScrollablePageFinished,
  readerScrollAnchorForGeometry,
  readerScrollGeometryBeforeResize,
  readerScrollPositionForAnchor,
  readerUsesSourceQuality,
  snapReaderPixel,
} from "../src/lib/readerImage.ts";

test("fit-page sizes from the longest rendered edge on a 3x phone", () => {
  assert.equal(readerImageMaxForViewport("fit-page", 390, 844, 3), 2400);
  assert.equal(readerImageMaxForViewport("fit-page", 390, 844, 4), 2400);
});

test("split-wide budgets both halves without exceeding the reader cap", () => {
  assert.equal(readerImageMaxForViewport("split-wide", 390, 844, 3), 2600);
  assert.equal(readerImageMaxForViewport("split-wide", 1366, 1024, 3), READER_IMAGE_MAX_DIMENSION);
});

test("fit-width follows physical viewport width and keeps a useful floor", () => {
  assert.equal(readerImageMaxForViewport("fit-width", 390, 844, 3), 1200);
  assert.equal(readerImageMaxForViewport("fit-width", 1024, 768, 2), 2200);
});

test("reader cache buckets round upward without exceeding the delivery cap", () => {
  assert.equal(readerImageCacheBucket(1200), 1200);
  assert.equal(readerImageCacheBucket(1201), 1400);
  assert.equal(readerImageCacheBucket(2300), 2400);
  assert.equal(readerImageCacheBucket(3199), READER_IMAGE_MAX_DIMENSION);
  assert.equal(readerImageCacheBucket(2600, 2400), 2400);
});

test("only manga candidates request source-preserving page delivery", () => {
  assert.equal(readerUsesSourceQuality("manga_image_folder"), true);
  assert.equal(readerUsesSourceQuality("manga_file"), true);
  assert.equal(readerUsesSourceQuality("doujin"), false);
  assert.equal(readerUsesSourceQuality(undefined), false);
});

test("split positioning snaps to physical pixels", () => {
  assert.equal(snapReaderPixel(100.2, 2), 100);
  assert.ok(Math.abs(snapReaderPixel(100.2, 3) - (301 / 3)) < Number.EPSILON * 100);
});

test("auto recognizes decoded long strips without changing explicit page fit", () => {
  assert.equal(readerImageIsLongStrip(800, 2400), true);
  assert.equal(readerImageIsLongStrip(800, 2399), false);
  assert.equal(readerImageIsLongStrip(2400, 800), false);
  for (const invalid of [0, -1, NaN, Infinity]) {
    assert.equal(readerImageIsLongStrip(invalid, 2400), false);
    assert.equal(readerImageIsLongStrip(800, invalid), false);
  }
  assert.equal(readerUsesScrollableWidthLayout("auto", 800, 8000), true);
  assert.equal(readerUsesScrollableWidthLayout("auto", 800, 1200), false);
  assert.equal(readerUsesScrollableWidthLayout("fit-page", 800, 8000), false);
  assert.equal(readerUsesScrollableWidthLayout("split-wide", 800, 8000), false);
  assert.equal(readerUsesScrollableWidthLayout("fit-width", 800, 1200), true);
  assert.equal(readerImageMaxForViewport("auto", 390, 844, 3), 2400);
});

test("a single tall page is finished only after reaching its bottom", () => {
  const page = { clientHeight: 800, clientWidth: 390, scrollHeight: 8000, scrollWidth: 390, scrollTop: 2000, scrollLeft: 0 };
  assert.equal(readerScrollablePageFinished(true, false, null), false);
  assert.equal(readerScrollablePageFinished(true, false, page), false);
  assert.equal(readerScrollablePageFinished(true, false, { ...page, scrollTop: 7200 }), true);
  assert.equal(readerScrollablePageFinished(false, false, page), true);
  assert.equal(readerScrollablePageFinished(true, true, page), true);
  assert.equal(readerScrollablePageFinished(true, true, null), true);
  assert.equal(readerScrollAtEnd(7192, 8000, 800), true);
  assert.equal(readerScrollAtEnd(7191, 8000, 800), false);
  assert.equal(readerScrollAtEnd(0, 500, 800), true);
});

test("resizing width preserves the content anchor and end position", () => {
  const page = { clientHeight: 800, clientWidth: 390, scrollHeight: 8000, scrollWidth: 390, scrollTop: 2000, scrollLeft: 0 };
  const larger = { ...page, clientWidth: 780, scrollWidth: 780, scrollHeight: 16000 };
  const anchor = readerScrollAnchorForGeometry(page);
  assert.deepEqual(readerScrollPositionForAnchor(anchor, larger), { left: 0, top: 4000 });
  assert.deepEqual(readerScrollPositionForAnchor(readerScrollAnchorForGeometry({ ...page, scrollTop: 7200 }), larger), { left: 0, top: 15200 });
  const horizontallyScrolled = { ...page, scrollWidth: 780, scrollLeft: 100 };
  assert.ok(Math.abs(readerScrollPositionForAnchor(readerScrollAnchorForGeometry(horizontallyScrolled), { ...larger, scrollWidth: 1560 }).left - 200) < 1e-9);
  assert.deepEqual(readerScrollPositionForAnchor(anchor, { ...page, scrollHeight: 100 }), { left: 0, top: 0 });
});

test("scroll restoration waits for the matching visible image, not the decoded cache entry", () => {
  const expected = { loading: false, url: "blob:synthetic-new-width", width: 1200, height: 9600 };
  const ready = { complete: true, src: expected.url, naturalWidth: 1200, naturalHeight: 9600 };
  assert.equal(readerImageReadyForScroll(null, expected), false);
  assert.equal(readerImageReadyForScroll({ ...ready, complete: false, naturalWidth: 0, naturalHeight: 0 }, expected), false);
  assert.equal(readerImageReadyForScroll({ ...ready, naturalHeight: 0 }, expected), false);
  assert.equal(readerImageReadyForScroll({ ...ready, src: "blob:synthetic-old-width" }, expected), false);
  assert.equal(readerImageReadyForScroll({ ...ready, naturalWidth: 300, naturalHeight: 2400 }, expected), false);
  assert.equal(readerImageReadyForScroll(ready, { ...expected, loading: true }), false);
  assert.equal(readerImageReadyForScroll(ready, expected), true);
});

test("resize captures the live offset before a queued scroll event arrives", () => {
  const previous = { clientHeight: 580, clientWidth: 370, scrollHeight: 2960, scrollWidth: 370, scrollTop: 0, scrollLeft: 0 };
  const live = { ...previous, clientWidth: 580, scrollWidth: 580, scrollHeight: 4640, scrollTop: 1000 };
  const anchor = readerScrollAnchorForGeometry(readerScrollGeometryBeforeResize(previous, live));
  assert.equal(anchor.y, 1000 / 2960);
  assert.ok(Math.abs(readerScrollPositionForAnchor(anchor, live).top / 4640 - 1000 / 2960) < 1e-9);
  assert.equal(readerScrollGeometryBeforeResize(null, live), live);
  assert.equal(readerScrollGeometryBeforeResize(previous, { ...previous, scrollTop: 1000 }).scrollTop, 1000);
});

test("a scroll event delivered after layout but before resize retains the old coordinate system", () => {
  const previous = { clientHeight: 580, clientWidth: 370, scrollHeight: 2960, scrollWidth: 370, scrollTop: 0, scrollLeft: 0 };
  const live = { ...previous, clientWidth: 580, scrollWidth: 580, scrollHeight: 4640, scrollTop: 1000 };
  const scrollEvent = readerScrollGeometryBeforeResize(previous, live);
  const resizeEvent = readerScrollGeometryBeforeResize(scrollEvent, live);
  assert.equal(resizeEvent.clientWidth, 370);
  assert.equal(readerScrollAnchorForGeometry(resizeEvent).y, 1000 / 2960);
});

test("shrinking preserves content and end anchors when the browser clamps offsets", () => {
  const previous = { clientHeight: 580, clientWidth: 580, scrollHeight: 4640, scrollWidth: 1160, scrollTop: 4000, scrollLeft: 500 };
  const live = { ...previous, clientWidth: 370, scrollWidth: 740, scrollHeight: 2960, scrollTop: 2380, scrollLeft: 370 };
  const anchor = readerScrollAnchorForGeometry(readerScrollGeometryBeforeResize(previous, live));
  assert.equal(anchor.y, 4000 / 4640);
  assert.equal(anchor.x, 500 / 1160);
  const end = readerScrollAnchorForGeometry(readerScrollGeometryBeforeResize({ ...previous, scrollTop: 4060, scrollLeft: 580 }, live));
  assert.deepEqual(readerScrollPositionForAnchor(end, live), { top: 2380, left: 370 });
});

test("height-only resize uses live image extent without scaling the latest offset", () => {
  const previous = { clientHeight: 580, clientWidth: 370, scrollHeight: 2890, scrollWidth: 370, scrollTop: 0, scrollLeft: 0 };
  const live = { ...previous, clientHeight: 476, scrollHeight: 2960, scrollTop: 1100 };
  const anchor = readerScrollAnchorForGeometry(readerScrollGeometryBeforeResize(previous, live));
  assert.equal(readerScrollPositionForAnchor(anchor, live).top, 1100);
});

test("a pending visible-image load does not consume the source-content anchor on collapsed layout", () => {
  const before = { clientHeight: 844, clientWidth: 370, scrollHeight: 2960, scrollWidth: 370, scrollTop: 1000, scrollLeft: 0 };
  const anchor = readerScrollAnchorForGeometry(before);
  const expected = { loading: false, url: "blob:synthetic-new-width", width: 1200, height: 9600 };
  let pending = anchor;
  const image = { complete: false, src: expected.url, naturalWidth: 0, naturalHeight: 0 };
  let restored = null;
  const restore = (geometry) => {
    if (!pending || !readerImageReadyForScroll(image, expected)) return;
    restored = readerScrollPositionForAnchor(pending, geometry);
    pending = null;
  };
  restore({ ...before, clientWidth: 580, scrollWidth: 580, scrollHeight: 844, scrollTop: 0 });
  assert.equal(restored, null);
  assert.equal(pending, anchor);
  Object.assign(image, { complete: true, naturalWidth: 1200, naturalHeight: 9600 });
  restore({ ...before, clientWidth: 580, scrollWidth: 580, scrollHeight: 4640, scrollTop: 0 });
  assert.equal(pending, null);
  assert.ok(Math.abs(restored.top / 4640 - 1000 / 2960) < 1e-9);
});

test("split-wide regression styles never interpolate the persistent crop transform", async () => {
  const css = await readFile(new URL("../src/design-system/reader-regressions.css", import.meta.url), "utf8");
  assert.doesNotMatch(css, /will-change:\s*transform/i);
  assert.doesNotMatch(css, /backface-visibility/i);
  assert.doesNotMatch(css, /transition\s*:[^;]*transform[^;]*;/i);
});
