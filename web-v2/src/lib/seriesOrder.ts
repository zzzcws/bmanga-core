import type {
  SeriesDetailResponse,
  SeriesSection,
  SeriesSectionGroup,
  WorkSummary,
} from "../types";
import { DEFAULT_LOCALE, localizeMessage, type Locale } from "./locale.ts";

export interface SeriesOutlineGroup {
  key: string;
  label: string;
  sort: number;
  sequence: number;
  items: WorkSummary[];
  primary: WorkSummary | null;
}

export interface SeriesOutlineSection {
  key: string;
  title: string;
  sort: number;
  groups: SeriesOutlineGroup[];
}

export interface SeriesOutline {
  sections: SeriesOutlineSection[];
  entries: WorkSummary[];
  readingEntries: WorkSummary[];
  readingGroupIndex: Map<string, number>;
  groupIndex: Map<string, number>;
}

function numberValue(value: unknown, fallback = 0): number {
  const parsed = typeof value === "number" ? value : Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function itemTitle(item: WorkSummary, locale: Locale): string {
  return String(item.display_title || item.title || item.relative_path || localizeMessage({
    "zh-CN": "未命名条目",
    en: "Untitled item",
    ja: "無題の項目",
  }, locale)).trim();
}

function canonicalItems(data: SeriesDetailResponse): Map<string, WorkSummary> {
  return new Map((data.items || []).map((item) => [item.candidate_id, item]));
}

function normalizeGroup(
  group: SeriesSectionGroup,
  canonical: Map<string, WorkSummary>,
  fallbackKey: string,
  locale: Locale,
): SeriesOutlineGroup | null {
  const seen = new Set<string>();
  const items = (group.items || []).flatMap((item) => {
    const candidateID = String(item?.candidate_id || "");
    if (!candidateID || seen.has(candidateID)) return [];
    seen.add(candidateID);
    return [canonical.get(candidateID) || item];
  });
  const primaryID = String(group.primary?.candidate_id || "");
  const primary = items.find((item) => item.candidate_id === primaryID)
    || items.find((item) => item.can_read)
    || items[0]
    || null;
  if (!items.length) return null;
  const sequence = Number(group.sequence);
  return {
    key: String(group.key || fallbackKey),
    label: String(group.label || primary?.item_label || itemTitle(primary || items[0]!, locale)).trim(),
    sort: numberValue(group.sort, Number.MAX_SAFE_INTEGER),
    sequence: Number.isFinite(sequence) ? sequence : Number.POSITIVE_INFINITY,
    items,
    primary,
  };
}

function fallbackGroups(items: WorkSummary[], locale: Locale): SeriesOutlineGroup[] {
  return items.map((item, index) => ({
    key: `fallback:${item.candidate_id}`,
    label: String(item.item_label || item.display_title || item.title || localizeMessage({
      "zh-CN": "条目 {index}",
      en: "Item {index}",
      ja: "項目 {index}",
    }, locale, { index: index + 1 })),
    sort: index,
    sequence: numberValue(item.sequence_number, Number.POSITIVE_INFINITY),
    items: [item],
    primary: item,
  }));
}

export function buildSeriesOutline(
  data: SeriesDetailResponse,
  locale: Locale = DEFAULT_LOCALE,
): SeriesOutline {
  const canonical = canonicalItems(data);
  const rawSections = Array.isArray(data.sections) ? data.sections : [];
  const normalized = rawSections.flatMap((section: SeriesSection, sectionIndex) => {
    const groups = (section.groups || []).flatMap((group, groupIndex) => {
      const normalizedGroup = normalizeGroup(group, canonical, `section:${sectionIndex}:group:${groupIndex}`, locale);
      return normalizedGroup ? [normalizedGroup] : [];
    });
    if (!groups.length) return [];
    const sourceTitle = String(section.title || "").trim();
    const title = sourceTitle || localizeMessage({
      "zh-CN": "本篇",
      en: "Main story",
      ja: "本編",
    }, locale);
    return [{
      key: `section:${sectionIndex}:${sourceTitle || "本篇"}`,
      title,
      sort: numberValue(section.sort, sectionIndex),
      groups,
    }];
  });

  let sections: SeriesOutlineSection[];
  if (data.sectioned && normalized.length) {
    sections = normalized;
  } else {
    const groups = normalized.flatMap((section) => section.groups);
    const stable = groups.map((group, index) => ({ group, index }));
    stable.sort((left, right) => {
      if (left.group.sequence !== right.group.sequence) return left.group.sequence - right.group.sequence;
      if (left.group.sort !== right.group.sort) return left.group.sort - right.group.sort;
      return left.index - right.index;
    });
    sections = [{
      key: "section:continuous",
      title: localizeMessage({
        "zh-CN": "章节目录",
        en: "Chapter directory",
        ja: "章一覧",
      }, locale),
      sort: 0,
      groups: stable.map((entry) => entry.group),
    }];
  }

  if (!sections.length) {
    sections = [{
      key: "section:fallback",
      title: localizeMessage({
        "zh-CN": "章节目录",
        en: "Chapter directory",
        ja: "章一覧",
      }, locale),
      sort: 0,
      groups: fallbackGroups(data.items || [], locale),
    }];
  }

  const included = new Set(sections.flatMap((section) => section.groups.flatMap((group) => group.items.map((item) => item.candidate_id))));
  const missing = (data.items || []).filter((item) => !included.has(item.candidate_id));
  if (missing.length) sections[sections.length - 1]?.groups.push(...fallbackGroups(missing, locale));

  const entries: WorkSummary[] = [];
  const readingEntries: WorkSummary[] = [];
  const readingGroupIndex = new Map<string, number>();
  const entrySeen = new Set<string>();
  const groupIndex = new Map<string, number>();
  let displayIndex = 0;
  for (const section of sections) {
    for (const group of section.groups) {
      groupIndex.set(group.key, displayIndex);
      displayIndex += 1;
      const readingEntry = group.primary?.can_read
        ? group.primary
        : group.items.find((item) => item.can_read) || group.primary || group.items[0];
      const readingIndex = readingEntries.length;
      if (readingEntry) readingEntries.push(readingEntry);
      for (const item of group.items) {
        readingGroupIndex.set(item.candidate_id, readingIndex);
        if (entrySeen.has(item.candidate_id)) continue;
        entrySeen.add(item.candidate_id);
        entries.push(item);
      }
    }
  }
  return { sections, entries, readingEntries, readingGroupIndex, groupIndex };
}

export function seriesReadingOrder(
  data: SeriesDetailResponse,
  locale: Locale = DEFAULT_LOCALE,
): WorkSummary[] {
  return buildSeriesOutline(data, locale).readingEntries;
}

export function nextSeriesReadableFromOutline(outline: SeriesOutline, candidateID: string): WorkSummary | undefined {
  const index = outline.readingGroupIndex.get(candidateID)
    ?? outline.readingEntries.findIndex((entry) => entry.candidate_id === candidateID);
  if (index < 0) return undefined;
  return outline.readingEntries.slice(index + 1).find((entry) => entry.can_read);
}

export function nextSeriesReadable(
  data: SeriesDetailResponse,
  candidateID: string,
  locale: Locale = DEFAULT_LOCALE,
): WorkSummary | undefined {
  return nextSeriesReadableFromOutline(buildSeriesOutline(data, locale), candidateID);
}
