import type { ReadingProgress, WorkSummary } from "../types";

function finiteNumber(value: unknown): number {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

export function isMeaningfulSeriesProgress(progress: ReadingProgress | null | undefined): boolean {
  if (!progress) return false;
  return Boolean(progress.completed)
    || finiteNumber(progress.index) > 0
    || finiteNumber(progress.reader_split_panel) > 0
    || finiteNumber(progress.stage_scroll_top) > 0
    || finiteNumber(progress.stage_scroll_left) > 0;
}

export function seriesProgressTimestamp(progress: ReadingProgress | null | undefined): number {
  const raw = String(progress?.last_read_at || progress?.updated_at || "").trim();
  if (!raw) return Number.NEGATIVE_INFINITY;
  const parsed = Date.parse(raw);
  return Number.isFinite(parsed) ? parsed : Number.NEGATIVE_INFINITY;
}

export function compareSeriesResumeProgress(
  left: ReadingProgress | null | undefined,
  right: ReadingProgress | null | undefined,
): number {
  if (!left) return right ? -1 : 0;
  if (!right) return 1;
  const leftMeaningful = isMeaningfulSeriesProgress(left);
  const rightMeaningful = isMeaningfulSeriesProgress(right);
  if (leftMeaningful !== rightMeaningful) return leftMeaningful ? 1 : -1;
  const leftTime = seriesProgressTimestamp(left);
  const rightTime = seriesProgressTimestamp(right);
  if (leftTime !== rightTime) return leftTime > rightTime ? 1 : -1;
  return 0;
}

export function preferredSeriesResumeProgress(
  current: ReadingProgress | null | undefined,
  incoming: ReadingProgress,
): ReadingProgress {
  if (!current) return incoming;
  const comparison = compareSeriesResumeProgress(incoming, current);
  if (comparison > 0) return incoming;
  if (comparison < 0) return current;
  return incoming.candidate_id === current.candidate_id ? incoming : current;
}

export function selectSeriesContinueItem(
  readable: WorkSummary[],
  fallbackProgress: ReadingProgress | null = null,
): WorkSummary | undefined {
  if (!readable.length) return undefined;
  let anchorIndex = -1;
  let anchorProgress: ReadingProgress | null = null;
  for (let index = 0; index < readable.length; index += 1) {
    const item = readable[index];
    if (!item) continue;
    const progress = item.progress
      || (fallbackProgress?.candidate_id === item.candidate_id ? fallbackProgress : null);
    if (!progress) continue;
    const comparison = compareSeriesResumeProgress(progress, anchorProgress);
    if (comparison > 0 || (comparison === 0 && index > anchorIndex)) {
      anchorIndex = index;
      anchorProgress = progress;
    }
  }
  if (anchorIndex < 0 || !anchorProgress) return undefined;
  if (!anchorProgress.completed) return readable[anchorIndex];
  return readable[anchorIndex + 1] || readable[anchorIndex];
}
