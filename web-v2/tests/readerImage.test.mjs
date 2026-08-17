import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  READER_IMAGE_MAX_DIMENSION,
  readerImageCacheBucket,
  readerImageMaxForViewport,
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

test("split-wide regression styles never interpolate the persistent crop transform", async () => {
  const css = await readFile(new URL("../src/design-system/reader-regressions.css", import.meta.url), "utf8");
  assert.doesNotMatch(css, /will-change:\s*transform/i);
  assert.doesNotMatch(css, /backface-visibility/i);
  assert.doesNotMatch(css, /transition\s*:[^;]*transform[^;]*;/i);
});
