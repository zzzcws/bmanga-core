import { DEFAULT_LOCALE, intlLocale, localizeMessage, type Locale } from "./locale.ts";

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
  locale: Locale = DEFAULT_LOCALE,
): string {
  const range = seriesDirectoryRangeWindow(labels.length, requestedIndex, pageSize);
  if (!labels.length) return localizeMessage({
    "zh-CN": "暂无条目",
    en: "No items",
    ja: "項目なし",
  }, locale);
  const first = String(labels[range.start] || "").trim();
  const last = String(labels[Math.max(range.start, range.end - 1)] || "").trim();
  const formatter = new Intl.NumberFormat(intlLocale(locale), { maximumFractionDigits: 0 });
  const position = localizeMessage({
    "zh-CN": "第 {start}–{end} 组",
    en: "Groups {start}–{end}",
    ja: "グループ {start}–{end}",
  }, locale, {
    start: formatter.format(range.start + 1),
    end: formatter.format(range.end),
  });
  if (!first && !last) return position;
  if (!last || first === last) return `${position} · ${first || last}`;
  return `${position} · ${first} — ${last}`;
}
