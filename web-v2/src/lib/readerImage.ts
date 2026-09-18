import type { ActiveReaderFitMode } from "../components/ReaderChrome";

export const READER_IMAGE_MAX_DIMENSION = 3200;
export const READER_LONG_STRIP_ASPECT_RATIO = 3;
const READER_IMAGE_MIN_DIMENSION = 1200;
const READER_IMAGE_MAX_DPR = 3;
const READER_IMAGE_CACHE_BUCKET_STEP = 200;
const READER_SCROLL_END_TOLERANCE = 8;

export interface ReaderScrollGeometry {
  clientHeight: number;
  clientWidth: number;
  scrollHeight: number;
  scrollLeft: number;
  scrollTop: number;
  scrollWidth: number;
}

export interface ReaderScrollAnchor {
  atBottom: boolean;
  atRight: boolean;
  x: number;
  y: number;
}

/** The cache decodes a separate Image object. A newly mounted visible image
 * can still have no intrinsic size, even when that cache request is complete. */
export function readerImageReadyForScroll(
  image: { complete: boolean; src: string; naturalWidth: number; naturalHeight: number } | null,
  expected: { loading: boolean; url: string; width: number; height: number },
): boolean {
  return !expected.loading
    && Boolean(expected.url)
    && expected.width > 0
    && expected.height > 0
    && Boolean(image?.complete
      && image.src === expected.url
      && image.naturalWidth === expected.width
      && image.naturalHeight === expected.height);
}

function finitePositive(value: number, fallback: number): number {
  return Number.isFinite(value) && value > 0 ? value : fallback;
}

/**
 * Keep reader requests on a compact set of cache sizes. Rounding upward
 * preserves the requested physical-pixel budget while allowing phones and
 * tablets with slightly different viewport sizes to reuse the same server
 * thumbnail.
 */
export function readerImageCacheBucket(
  target: number,
  maxDimension = READER_IMAGE_MAX_DIMENSION,
): number {
  const maximum = Math.max(
    READER_IMAGE_MIN_DIMENSION,
    finitePositive(maxDimension, READER_IMAGE_MAX_DIMENSION),
  );
  const clamped = Math.min(
    maximum,
    Math.max(READER_IMAGE_MIN_DIMENSION, finitePositive(target, READER_IMAGE_MIN_DIMENSION)),
  );
  return Math.min(
    maximum,
    Math.ceil(clamped / READER_IMAGE_CACHE_BUCKET_STEP) * READER_IMAGE_CACHE_BUCKET_STEP,
  );
}

/**
 * The page endpoint limits the longest source edge, so fit-page must size from
 * the longest edge the viewport may display. Using the viewport's shorter edge
 * undersamples portrait pages on high-DPR phones.
 */
export function readerImageMaxForViewport(
  fitMode: ActiveReaderFitMode,
  viewportWidth: number,
  viewportHeight: number,
  devicePixelRatio: number,
): number {
  const width = Math.max(320, finitePositive(viewportWidth, 320));
  const height = Math.max(480, finitePositive(viewportHeight, 480));
  const dpr = Math.min(READER_IMAGE_MAX_DPR, Math.max(1, finitePositive(devicePixelRatio, 1)));
  const renderedLongestEdge = fitMode === "fit-width"
    ? width
    : fitMode === "split-wide"
      ? Math.max(width * 2, height)
      : Math.max(width, height * 0.92);
  const target = Math.min(
    READER_IMAGE_MAX_DIMENSION,
    Math.max(READER_IMAGE_MIN_DIMENSION, renderedLongestEdge * dpr),
  );
  return readerImageCacheBucket(target);
}

export function readerUsesSourceQuality(candidateType: unknown): boolean {
  return String(candidateType || "").trim().toLowerCase().startsWith("manga_");
}

/**
 * Pages above this ratio are not useful when squeezed into a single viewport.
 * The check intentionally uses decoded dimensions, so it works for folders,
 * archives and other local pages without trusting catalog type labels.
 */
export function readerImageIsLongStrip(width: number, height: number): boolean {
  const safeWidth = finitePositive(width, 0);
  const safeHeight = finitePositive(height, 0);
  return safeWidth > 0
    && safeHeight > 0
    && safeHeight / safeWidth >= READER_LONG_STRIP_ASPECT_RATIO;
}

export function readerUsesScrollableWidthLayout(
  fitMode: ActiveReaderFitMode,
  width: number,
  height: number,
): boolean {
  return fitMode === "fit-width"
    || (fitMode === "auto" && readerImageIsLongStrip(width, height));
}

function clampUnit(value: number): number {
  return Math.min(1, Math.max(0, Number.isFinite(value) ? value : 0));
}

export function readerScrollAtEnd(
  scrollTop: number,
  scrollHeight: number,
  clientHeight: number,
  tolerance = READER_SCROLL_END_TOLERANCE,
): boolean {
  const maximum = Math.max(0, finitePositive(scrollHeight, 0) - finitePositive(clientHeight, 0));
  return maximum <= Math.max(0, tolerance)
    || Math.max(0, Number.isFinite(scrollTop) ? scrollTop : 0) >= maximum - Math.max(0, tolerance);
}

export function readerScrollablePageFinished(
  scrollable: boolean,
  ending: boolean,
  geometry: ReaderScrollGeometry | null,
): boolean {
  if (!scrollable || ending) return true;
  return Boolean(geometry && readerScrollAtEnd(
    geometry.scrollTop,
    geometry.scrollHeight,
    geometry.clientHeight,
  ));
}

/** Scroll events are queued, so resize can observe a newer DOM offset before
 * the scroll handler has saved it. Keep the pre-resize coordinate system but
 * read live offsets; retain an older offset only when shrinkage clamped it. */
export function readerScrollGeometryBeforeResize(
  previous: ReaderScrollGeometry | null,
  live: ReaderScrollGeometry,
): ReaderScrollGeometry {
  if (!previous || (previous.clientWidth === live.clientWidth && previous.clientHeight === live.clientHeight)) return live;
  const maxTop = Math.max(0, live.scrollHeight - live.clientHeight);
  const maxLeft = Math.max(0, live.scrollWidth - live.clientWidth);
  // Height-only browser chrome changes do not rescale the image. Use its live
  // extent, even if a preceding fit transition finished after the last sample.
  const before = previous.clientWidth === live.clientWidth
    ? { ...live, clientHeight: previous.clientHeight }
    : previous;
  return {
    ...before,
    scrollTop: previous.scrollTop > maxTop && live.scrollTop >= maxTop - READER_SCROLL_END_TOLERANCE
      ? previous.scrollTop : live.scrollTop,
    scrollLeft: previous.scrollLeft > maxLeft && live.scrollLeft >= maxLeft - READER_SCROLL_END_TOLERANCE
      ? previous.scrollLeft : live.scrollLeft,
  };
}

/** Preserve the source-content point at the viewport's top-left while a
 * width-based page is rescaled. Bottom/right are explicit anchors so a reader
 * already at the end stays at the end when browser chrome or orientation
 * changes the viewport size. */
export function readerScrollAnchorForGeometry(geometry: ReaderScrollGeometry): ReaderScrollAnchor {
  const scrollHeight = Math.max(0, Number(geometry.scrollHeight) || 0);
  const scrollWidth = Math.max(0, Number(geometry.scrollWidth) || 0);
  const maxLeft = Math.max(0, scrollWidth - Math.max(0, Number(geometry.clientWidth) || 0));
  return {
    atBottom: readerScrollAtEnd(geometry.scrollTop, scrollHeight, geometry.clientHeight),
    atRight: maxLeft <= READER_SCROLL_END_TOLERANCE
      || Math.max(0, Number(geometry.scrollLeft) || 0) >= maxLeft - READER_SCROLL_END_TOLERANCE,
    x: scrollWidth > 0 ? clampUnit(Math.max(0, Number(geometry.scrollLeft) || 0) / scrollWidth) : 0,
    y: scrollHeight > 0 ? clampUnit(Math.max(0, Number(geometry.scrollTop) || 0) / scrollHeight) : 0,
  };
}

export function readerScrollPositionForAnchor(
  anchor: ReaderScrollAnchor,
  geometry: ReaderScrollGeometry,
): { left: number; top: number } {
  const scrollHeight = Math.max(0, Number(geometry.scrollHeight) || 0);
  const scrollWidth = Math.max(0, Number(geometry.scrollWidth) || 0);
  const maxTop = Math.max(0, scrollHeight - Math.max(0, Number(geometry.clientHeight) || 0));
  const maxLeft = Math.max(0, scrollWidth - Math.max(0, Number(geometry.clientWidth) || 0));
  return {
    top: anchor.atBottom ? maxTop : Math.min(maxTop, Math.max(0, clampUnit(anchor.y) * scrollHeight)),
    left: anchor.atRight ? maxLeft : Math.min(maxLeft, Math.max(0, clampUnit(anchor.x) * scrollWidth)),
  };
}

export function snapReaderPixel(value: number, devicePixelRatio: number): number {
  const dpr = Math.min(READER_IMAGE_MAX_DPR, Math.max(1, finitePositive(devicePixelRatio, 1)));
  return Math.round(value * dpr) / dpr;
}
