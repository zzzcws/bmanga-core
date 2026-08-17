import { preferredSeriesResumeProgress } from "./seriesResume.ts";
import { nextSeriesReadable } from "./seriesOrder.ts";
import type {
  CatalogItem,
  ContinueTarget,
  ReadingProgress,
  SeriesDetailResponse,
  WorkDetailResponse,
  WorkSummary,
} from "../types";

export type DetailState =
  | { kind: "work"; data: WorkDetailResponse }
  | { kind: "series"; data: SeriesDetailResponse; progress: ReadingProgress | null };

export function patchCatalogItemProgress<T extends CatalogItem>(
  item: T,
  candidateID: string,
  progress: ReadingProgress,
): T {
  if (item.candidate_id !== candidateID) return item;
  return {
    ...item,
    progress,
    progress_index: progress.index,
    progress_count: progress.count,
    progress_percent: progress.progress_percent,
    progress_completed: progress.completed,
    progress_status: progress.progress_status,
  } as T;
}

export function patchDetailProgress(
  current: DetailState | null,
  candidateID: string,
  progress: ReadingProgress,
): DetailState | null {
  if (!current) return current;
  if (current.kind === "work") {
    const work = patchCatalogItemProgress(current.data.work, candidateID, progress);
    return work === current.data.work
      ? current
      : { ...current, data: { ...current.data, work } };
  }

  if (!current.data.items.some((item) => item.candidate_id === candidateID)) return current;
  const items = current.data.items.map((item) => patchCatalogItemProgress(item, candidateID, progress));
  const data = { ...current.data, items };
  const completedAdvance = Boolean(
    current.progress?.completed
    && current.progress.candidate_id !== candidateID
    && nextSeriesReadable(data, current.progress.candidate_id)?.candidate_id === candidateID,
  );
  const seriesProgress = completedAdvance
    ? progress
    : preferredSeriesResumeProgress(current.progress, progress);
  return { ...current, data, progress: seriesProgress };
}

export function patchContinueTargetProgress(
  current: ContinueTarget | null,
  candidateID: string,
  progress: ReadingProgress,
  sourceItem?: WorkSummary,
  sourceNextItem?: WorkSummary,
  sourceSeries?: ContinueTarget["series"],
): ContinueTarget | null {
  if (!current) {
    if (!sourceItem || sourceItem.candidate_id !== candidateID) return null;
    return {
      item: patchCatalogItemProgress(sourceItem, candidateID, progress),
      progress,
      series: sourceSeries || null,
      next_item: sourceNextItem || null,
    };
  }
  if (current.item.candidate_id === candidateID) {
    return {
      ...current,
      item: patchCatalogItemProgress(current.item, candidateID, progress),
      progress,
      next_item: sourceNextItem || current.next_item,
    };
  }
  const incomingTime = Date.parse(String(progress.updated_at || progress.last_read_at || ""));
  const currentTime = Date.parse(String(current.progress?.updated_at || current.progress?.last_read_at || ""));
  if (Number.isFinite(currentTime) && (!Number.isFinite(incomingTime) || incomingTime <= currentTime)) return current;
  if (current.next_item?.candidate_id === candidateID) {
    return {
      ...current,
      item: patchCatalogItemProgress(sourceItem || current.next_item, candidateID, progress),
      progress,
      series: sourceSeries === undefined ? current.series : sourceSeries,
      next_item: sourceNextItem || null,
    };
  }
  if (!sourceItem || sourceItem.candidate_id !== candidateID) return current;
  return {
    item: patchCatalogItemProgress(sourceItem, candidateID, progress),
    progress,
    series: sourceSeries || null,
    next_item: sourceNextItem || null,
  };
}

export function patchSnapshotDetailProgress<T extends { detail: DetailState | null }>(
  snapshot: T,
  candidateID: string,
  progress: ReadingProgress,
): T {
  const detail = patchDetailProgress(snapshot.detail, candidateID, progress);
  return detail === snapshot.detail ? snapshot : { ...snapshot, detail };
}

export function patchHistoryEntryDetailProgress<
  TSnapshot extends { detail: DetailState | null },
  TEntry extends { snapshot: TSnapshot },
>(
  entries: Iterable<TEntry>,
  candidateID: string,
  progress: ReadingProgress,
): number {
  let patchedCount = 0;
  for (const entry of entries) {
    const snapshot = patchSnapshotDetailProgress(entry.snapshot, candidateID, progress);
    if (snapshot === entry.snapshot) continue;
    entry.snapshot = snapshot;
    patchedCount += 1;
  }
  return patchedCount;
}
