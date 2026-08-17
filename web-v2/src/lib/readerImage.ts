import type { ActiveReaderFitMode } from "../components/ReaderChrome";

export const READER_IMAGE_MAX_DIMENSION = 3200;
const READER_IMAGE_MIN_DIMENSION = 1200;
const READER_IMAGE_MAX_DPR = 3;
const READER_IMAGE_CACHE_BUCKET_STEP = 200;

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

export function snapReaderPixel(value: number, devicePixelRatio: number): number {
  const dpr = Math.min(READER_IMAGE_MAX_DPR, Math.max(1, finitePositive(devicePixelRatio, 1)));
  return Math.round(value * dpr) / dpr;
}
