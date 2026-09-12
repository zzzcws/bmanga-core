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
  | { kind: "series"; data: SeriesDetailResponse; progress: ReadingProgress | null; progressState?: "loading" | "ready" | "error"; progressError?: string };

// Apply only progress leaves: a late summary must not replace notes, directory
// updates, or a newer progress ACK that arrived while this read was in flight.
export function applySeriesProgressSummary(
  current: DetailState | null,
  seriesID: string,
  incoming: ReadingProgress | null,
  acknowledgedSinceRead: readonly ReadingProgress[] = [],
  invalidItemError = "Reading progress no longer matches this directory.",
): DetailState | null {
  if (current?.kind !== "series" || current.data.series.group_id !== seriesID) return current;
  const replayAcknowledgements = (state: DetailState | null): DetailState | null => {
    for (const ack of acknowledgedSinceRead) {
      if (state?.kind !== "series" || !ack.work_identity_id) continue;
      const item = state.data.items.find((entry) => entry.candidate_id === ack.candidate_id && entry.work_identity_id === ack.work_identity_id)
        || state.data.items.find((entry) => entry.work_identity_id === ack.work_identity_id);
      if (!item) continue;
      const projected = ack.candidate_id === item.candidate_id ? ack : { ...ack, candidate_id: item.candidate_id };
      const progress = preferredSeriesResumeProgress(state.progress?.candidate_id === item.candidate_id ? state.progress : null, projected);
      state = patchDetailProgress(state, item.candidate_id, progress, ack.work_identity_id);
    }
    return state;
  };
  if (!incoming) {
    // An authoritative unread response also applies to history snapshots. Keep
    // only ACKs actually received during this read, not old cached resume data.
    const clearProgress = <T extends CatalogItem>(item: T): T => Object.fromEntries(Object.entries(item).filter(([key]) => !key.startsWith("progress"))) as T;
    const cleared: DetailState = {
      ...current,
      data: { ...current.data, series: clearProgress(current.data.series), items: current.data.items.map(clearProgress) },
      progress: null,
      progressState: "ready",
      progressError: "",
    };
    return replayAcknowledgements(cleared);
  }
  const item = current.data.items.find((entry) => entry.candidate_id === incoming.candidate_id);
  if (!item || (incoming.work_identity_id && item.work_identity_id && incoming.work_identity_id !== item.work_identity_id)) {
    return { ...current, progressState: "error", progressError: invalidItemError };
  }
  const data = { ...current.data, items: current.data.items.map((entry) => patchCatalogItemProgress(entry, item.candidate_id, incoming)) };
  return replayAcknowledgements({ ...current, data, progress: incoming, progressState: "ready", progressError: "" });
}

export function patchCatalogItemProgress<T extends CatalogItem>(
  item: T,
  candidateID: string,
  progress: ReadingProgress,
  expectedWorkIdentityID = "",
): T {
  if (item.candidate_id !== candidateID) return item;
  if (expectedWorkIdentityID && item.work_identity_id !== expectedWorkIdentityID) return item;
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
  expectedWorkIdentityID = "",
): DetailState | null {
  if (!current) return current;
  if (current.kind === "work") {
    const work = patchCatalogItemProgress(current.data.work, candidateID, progress, expectedWorkIdentityID);
    return work === current.data.work
      ? current
      : { ...current, data: { ...current.data, work } };
  }

  if (!current.data.items.some((item) => item.candidate_id === candidateID
    && (!expectedWorkIdentityID || item.work_identity_id === expectedWorkIdentityID))) return current;
  const items = current.data.items.map((item) => patchCatalogItemProgress(item, candidateID, progress, expectedWorkIdentityID));
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
  expectedWorkIdentityID = "",
): ContinueTarget | null {
  const identityMatches = (item?: WorkSummary | null): boolean => Boolean(item)
    && (!expectedWorkIdentityID || item?.work_identity_id === expectedWorkIdentityID);
  if (!current) {
    if (!sourceItem || sourceItem.candidate_id !== candidateID || !identityMatches(sourceItem)) return null;
    return {
      item: patchCatalogItemProgress(sourceItem, candidateID, progress, expectedWorkIdentityID),
      progress,
      series: sourceSeries || null,
      next_item: sourceNextItem || null,
    };
  }
  if (current.item.candidate_id === candidateID && identityMatches(current.item)) {
    return {
      ...current,
      item: patchCatalogItemProgress(current.item, candidateID, progress, expectedWorkIdentityID),
      progress,
      next_item: sourceNextItem || current.next_item,
    };
  }
  const incomingTime = Date.parse(String(progress.updated_at || progress.last_read_at || ""));
  const currentTime = Date.parse(String(current.progress?.updated_at || current.progress?.last_read_at || ""));
  if (Number.isFinite(currentTime) && (!Number.isFinite(incomingTime) || incomingTime <= currentTime)) return current;
  if (current.next_item?.candidate_id === candidateID && identityMatches(current.next_item)) {
    return {
      ...current,
      item: patchCatalogItemProgress(sourceItem && identityMatches(sourceItem) ? sourceItem : current.next_item, candidateID, progress, expectedWorkIdentityID),
      progress,
      series: sourceSeries === undefined ? current.series : sourceSeries,
      next_item: sourceNextItem || null,
    };
  }
  if (!sourceItem || sourceItem.candidate_id !== candidateID || !identityMatches(sourceItem)) return current;
  return {
    item: patchCatalogItemProgress(sourceItem, candidateID, progress, expectedWorkIdentityID),
    progress,
    series: sourceSeries || null,
    next_item: sourceNextItem || null,
  };
}

export function patchSnapshotDetailProgress<T extends { detail: DetailState | null }>(
  snapshot: T,
  candidateID: string,
  progress: ReadingProgress,
  expectedWorkIdentityID = "",
): T {
  const detail = patchDetailProgress(snapshot.detail, candidateID, progress, expectedWorkIdentityID);
  return detail === snapshot.detail ? snapshot : { ...snapshot, detail };
}

export function patchHistoryEntryDetailProgress<
  TSnapshot extends { detail: DetailState | null },
  TEntry extends { snapshot: TSnapshot },
>(
  entries: Iterable<TEntry>,
  candidateID: string,
  progress: ReadingProgress,
  expectedWorkIdentityID = "",
): number {
  let patchedCount = 0;
  for (const entry of entries) {
    const snapshot = patchSnapshotDetailProgress(entry.snapshot, candidateID, progress, expectedWorkIdentityID);
    if (snapshot === entry.snapshot) continue;
    entry.snapshot = snapshot;
    patchedCount += 1;
  }
  return patchedCount;
}
