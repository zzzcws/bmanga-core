export const SERIES_DIRECTORY_RANGE_SIZE = 50;

export interface SeriesDirectoryRangeWindow {
  index: number;
  pages: number;
  start: number;
  end: number;
}

function positiveInteger(value: number, fallback: number): number {
  return Number.isFinite(value) && value > 0 ? Math.max(1, Math.floor(value)) : fallback;
}

export function seriesDirectoryRangeWindow(
  total: number,
  requestedIndex: number,
  pageSize = SERIES_DIRECTORY_RANGE_SIZE,
): SeriesDirectoryRangeWindow {
  const safeTotal = Number.isFinite(total) ? Math.max(0, Math.floor(total)) : 0;
  const safePageSize = positiveInteger(pageSize, SERIES_DIRECTORY_RANGE_SIZE);
  const pages = Math.max(1, Math.ceil(safeTotal / safePageSize));
  const numericIndex = Number.isFinite(requestedIndex) ? Math.floor(requestedIndex) : 0;
  const index = Math.max(0, Math.min(pages - 1, numericIndex));
  const start = index * safePageSize;
  return {
    index,
    pages,
    start,
    end: Math.min(start + safePageSize, safeTotal),
  };
}

export function seriesDirectoryRangeForGroup(
  groupIndex: number,
  total: number,
  pageSize = SERIES_DIRECTORY_RANGE_SIZE,
): number {
  const safeTotal = Number.isFinite(total) ? Math.max(0, Math.floor(total)) : 0;
  if (!safeTotal || !Number.isFinite(groupIndex) || groupIndex < 0) return 0;
  const safePageSize = positiveInteger(pageSize, SERIES_DIRECTORY_RANGE_SIZE);
  const clampedGroupIndex = Math.min(safeTotal - 1, Math.floor(groupIndex));
  return Math.floor(clampedGroupIndex / safePageSize);
}

export function seriesDirectoryRangeLabel(
  labels: readonly string[],
  requestedIndex: number,
  pageSize = SERIES_DIRECTORY_RANGE_SIZE,
): string {
  const range = seriesDirectoryRangeWindow(labels.length, requestedIndex, pageSize);
  if (!labels.length) return "暂无条目";
  const first = String(labels[range.start] || "").trim();
  const last = String(labels[Math.max(range.start, range.end - 1)] || "").trim();
  const position = `第 ${range.start + 1}–${range.end} 组`;
  if (!first && !last) return position;
  if (!last || first === last) return `${position} · ${first || last}`;
  return `${position} · ${first} — ${last}`;
}
