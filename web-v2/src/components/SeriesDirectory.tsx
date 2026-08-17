import { useEffect, useMemo, useState } from "react";

import { useI18n, type LocalizedText } from "../i18n";
import { preferredScrollBehavior } from "../lib/motion";
import {
  SERIES_DIRECTORY_RANGE_SIZE,
  seriesDirectoryRangeForGroup,
  seriesDirectoryRangeWindow,
} from "../lib/seriesDirectoryRange";
import { buildSeriesOutline, nextSeriesReadableFromOutline, type SeriesOutlineGroup } from "../lib/seriesOrder";
import type { SeriesDetailResponse, WorkSummary } from "../types";

interface SeriesDirectoryProps {
  data: SeriesDetailResponse;
  activeCandidateID?: string;
  onOpen: (item: WorkSummary, nextItem?: WorkSummary) => void;
}

const copy = {
  directoryAria: { "zh-CN": "系列章节目录", en: "Series chapter directory", ja: "シリーズの章一覧" },
  readingOrder: { "zh-CN": "READING ORDER", en: "READING ORDER", ja: "読書順" },
  directoryTitle: { "zh-CN": "章节目录", en: "Chapter directory", ja: "章一覧" },
  entries: { "zh-CN": "{count} 个条目", en: "{count} entries", ja: "{count} 件" },
  sectionNavigation: { "zh-CN": "目录分区", en: "Directory sections", ja: "目録セクション" },
  groupsAndEntries: {
    "zh-CN": "{groups} 组 · {entries} 个条目",
    en: "{groups} groups · {entries} entries",
    ja: "{groups} グループ · {entries} 件",
  },
  rangeControls: { "zh-CN": "{section}范围切换", en: "Range controls for {section}", ja: "{section}の範囲切り替え" },
  previousRange: { "zh-CN": "← 上一段", en: "← Previous range", ja: "← 前の範囲" },
  range: { "zh-CN": "范围", en: "Range", ja: "範囲" },
  nextRange: { "zh-CN": "下一段 →", en: "Next range →", ja: "次の範囲 →" },
  rangePosition: { "zh-CN": "第 {start}–{end} 组", en: "Groups {start}–{end}", ja: "グループ {start}–{end}" },
  noEntries: { "zh-CN": "暂无条目", en: "No entries", ja: "項目なし" },
  rangeStatus: { "zh-CN": "第 {current} / {pages} 段", en: "Range {current} of {pages}", ja: "{pages} 範囲中 {current} 番目" },
  windowNote: {
    "zh-CN": "当前显示第 {start}–{end} 组，共 {total} 组；每段最多 {maximum} 组。",
    en: "Showing groups {start}–{end} of {total}; up to {maximum} groups per range.",
    ja: "全 {total} グループ中 {start}～{end} を表示しています。1 範囲は最大 {maximum} グループです。",
  },
  unnamedEntry: { "zh-CN": "未命名条目", en: "Untitled entry", ja: "名称未設定の項目" },
  unread: { "zh-CN": "未读", en: "Unread", ja: "未読" },
  unavailable: { "zh-CN": "暂不可读", en: "Unavailable", ja: "現在閲覧できません" },
  read: { "zh-CN": "已读", en: "Read", ja: "読了" },
  pageProgress: { "zh-CN": "第 {current} / {count} 页", en: "Page {current} of {count}", ja: "{count} ページ中 {current} ページ目" },
  pages: { "zh-CN": "{count} 页", en: "{count} pages", ja: "{count} ページ" },
  pagesPending: { "zh-CN": "页数待确认", en: "Page count pending", ja: "ページ数未確認" },
  collectedEntries: { "zh-CN": "{count} 个收录条目", en: "{count} included entries", ja: "収録項目 {count} 件" },
} satisfies Record<string, LocalizedText>;

function numberValue(value: unknown, fallback = 0): number {
  const parsed = typeof value === "number" ? value : Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function booleanValue(value: unknown): boolean {
  if (typeof value === "boolean") return value;
  if (typeof value === "number") return value !== 0;
  return ["1", "true", "yes", "on"].includes(String(value || "").trim().toLowerCase());
}

function itemTitle(item: WorkSummary, fallback: string): string {
  return String(item.display_title || item.title || item.relative_path || fallback).trim();
}

function itemPageCount(item: WorkSummary): number {
  return Math.max(0, numberValue(item.readable_page_count));
}

function itemProgress(item: WorkSummary): { index: number; count: number; percent: number; completed: boolean } | null {
  if (item.progress) {
    return {
      index: numberValue(item.progress.index),
      count: numberValue(item.progress.count),
      percent: numberValue(item.progress.progress_percent),
      completed: Boolean(item.progress.completed),
    };
  }
  const hasProgress = item.progress_updated_at || item.progress_last_read_at || item.progress_count || item.progress_percent || item.progress_completed;
  if (!hasProgress) return null;
  return {
    index: numberValue(item.progress_index),
    count: numberValue(item.progress_count),
    percent: numberValue(item.progress_percent),
    completed: booleanValue(item.progress_completed),
  };
}

function groupContains(group: SeriesOutlineGroup, candidateID?: string): boolean {
  return Boolean(candidateID && group.items.some((item) => item.candidate_id === candidateID));
}

function groupDefault(group: SeriesOutlineGroup, activeCandidateID?: string): WorkSummary | null {
  return group.items.find((item) => item.candidate_id === activeCandidateID)
    || group.primary
    || group.items.find((item) => item.can_read)
    || group.items[0]
    || null;
}

function progressLabel(
  item: WorkSummary,
  localize: (message: LocalizedText, parameters?: Readonly<Record<string, string | number>>) => string,
  formatNumber: (value: number, options?: Intl.NumberFormatOptions) => string,
): string {
  const progress = itemProgress(item);
  if (!progress) return localize(item.can_read ? copy.unread : copy.unavailable);
  if (progress.completed) return localize(copy.read);
  return progress.count > 0
    ? localize(copy.pageProgress, {
      current: formatNumber(progress.index + 1),
      count: formatNumber(progress.count),
    })
    : `${formatNumber(Math.round(progress.percent))}%`;
}

function entryMeta(
  item: WorkSummary,
  localize: (message: LocalizedText, parameters?: Readonly<Record<string, string | number>>) => string,
  formatNumber: (value: number, options?: Intl.NumberFormatOptions) => string,
): string {
  const pageCount = itemPageCount(item);
  const values = [
    pageCount ? localize(copy.pages, { count: formatNumber(pageCount) }) : localize(copy.pagesPending),
    String(item.translation_sources || "").trim(),
    String(item.display_library_name || item.library_name || "").trim(),
  ].filter(Boolean);
  return values.slice(0, 3).join(" · ");
}

export function SeriesDirectory({ data, activeCandidateID, onOpen }: SeriesDirectoryProps) {
  const { locale, number, text } = useI18n();
  const outline = useMemo(() => buildSeriesOutline(data, locale), [data, locale]);
  const activeSectionIndex = Math.max(0, outline.sections.findIndex((section) => section.groups.some((group) => groupContains(group, activeCandidateID))));
  const [openSections, setOpenSections] = useState<Set<number>>(() => new Set([activeSectionIndex]));
  const [sectionRanges, setSectionRanges] = useState<Record<string, number>>(() => {
    const section = outline.sections[activeSectionIndex];
    if (!section || !activeCandidateID) return {};
    const groupIndex = section.groups.findIndex((group) => groupContains(group, activeCandidateID));
    return groupIndex < 0
      ? {}
      : { [section.key]: seriesDirectoryRangeForGroup(groupIndex, section.groups.length) };
  });

  useEffect(() => {
    setOpenSections((current) => new Set(current).add(activeSectionIndex));
    if (!activeCandidateID) return;
    const section = outline.sections[activeSectionIndex];
    if (!section) return;
    const groupIndex = section.groups.findIndex((group) => groupContains(group, activeCandidateID));
    if (groupIndex < 0) return;
    const rangeIndex = seriesDirectoryRangeForGroup(groupIndex, section.groups.length);
    setSectionRanges((current) => current[section.key] === rangeIndex
      ? current
      : { ...current, [section.key]: rangeIndex });
  }, [activeCandidateID, activeSectionIndex, outline]);

  const jumpToSection = (index: number) => {
    setOpenSections((current) => new Set(current).add(index));
    window.requestAnimationFrame(() => document.getElementById(`series-outline-section-${index}`)?.scrollIntoView({ block: "start", behavior: preferredScrollBehavior() }));
  };

  const rangeLabel = (labels: readonly string[], requestedIndex: number): string => {
    const range = seriesDirectoryRangeWindow(labels.length, requestedIndex);
    if (!labels.length) return text(copy.noEntries);
    const first = String(labels[range.start] || "").trim();
    const last = String(labels[Math.max(range.start, range.end - 1)] || "").trim();
    const position = text(copy.rangePosition, {
      start: number(range.start + 1),
      end: number(range.end),
    });
    if (!first && !last) return position;
    if (!last || first === last) return `${position} · ${first || last}`;
    return `${position} · ${first} — ${last}`;
  };

  return (
    <section className="series-directory" aria-label={text(copy.directoryAria)} data-section-count={outline.sections.length} data-entry-count={outline.entries.length}>
      <header className="series-directory-heading">
        <div><span>{text(copy.readingOrder)}</span><h2>{text(copy.directoryTitle)}</h2></div>
        <p>{data.sectioned ? data.section_summary : (data.series.item_summary || text(copy.entries, { count: number(outline.entries.length) }))}</p>
      </header>
      {outline.sections.length > 1 ? (
        <nav className="series-section-nav" aria-label={text(copy.sectionNavigation)}>
          {outline.sections.map((section, index) => <button type="button" className={index === activeSectionIndex ? "active" : ""} onClick={() => jumpToSection(index)} key={section.key}><span>{number(index + 1, { minimumIntegerDigits: 2, useGrouping: false })}</span>{section.title}</button>)}
        </nav>
      ) : null}
      <div className="series-outline">
        {outline.sections.map((section, sectionIndex) => {
          const open = openSections.has(sectionIndex);
          const activeGroupIndex = section.groups.findIndex((group) => groupContains(group, activeCandidateID));
          const defaultRange = activeGroupIndex >= 0
            ? seriesDirectoryRangeForGroup(activeGroupIndex, section.groups.length)
            : 0;
          const range = seriesDirectoryRangeWindow(
            section.groups.length,
            sectionRanges[section.key] ?? defaultRange,
          );
          const visibleGroups = section.groups.slice(range.start, range.end);
          const rangeLabels = section.groups.map((group) => group.label);
          const entryCount = section.groups.reduce((total, group) => total + group.items.length, 0);
          const changeRange = (nextIndex: number) => {
            const nextRange = seriesDirectoryRangeWindow(section.groups.length, nextIndex);
            setSectionRanges((current) => current[section.key] === nextRange.index
              ? current
              : { ...current, [section.key]: nextRange.index });
          };
          return (
            <section className={`series-outline-section ${sectionIndex === activeSectionIndex ? "is-current" : ""}`} id={`series-outline-section-${sectionIndex}`} key={section.key}>
              <button className="series-outline-toggle" type="button" aria-expanded={open} onClick={() => setOpenSections((current) => { const next = new Set(current); if (next.has(sectionIndex)) next.delete(sectionIndex); else next.add(sectionIndex); return next; })}>
                <span>{number(sectionIndex + 1, { minimumIntegerDigits: 2, useGrouping: false })}</span>
                <strong>{section.title}</strong>
                <small>{text(copy.groupsAndEntries, { groups: number(section.groups.length), entries: number(entryCount) })}</small>
                <b aria-hidden="true">{open ? "−" : "+"}</b>
              </button>
              {open ? (
                <div className="series-outline-body">
                  {range.pages > 1 ? (
                    <nav className="series-range-controls" aria-label={text(copy.rangeControls, { section: section.title })}>
                      <button className="series-range-previous" type="button" disabled={range.index <= 0} onClick={() => changeRange(range.index - 1)}>{text(copy.previousRange)}</button>
                      <label className="series-range-select"><span>{text(copy.range)}</span><select value={range.index} onChange={(event) => changeRange(Number(event.target.value))}>{Array.from({ length: range.pages }, (_, rangeIndex) => <option value={rangeIndex} key={rangeIndex}>{rangeLabel(rangeLabels, rangeIndex)}</option>)}</select></label>
                      <span className="series-range-status" aria-live="polite">{text(copy.rangeStatus, { current: number(range.index + 1), pages: number(range.pages) })}</span>
                      <button className="series-range-next" type="button" disabled={range.index >= range.pages - 1} onClick={() => changeRange(range.index + 1)}>{text(copy.nextRange)}</button>
                    </nav>
                  ) : null}
                  {range.pages > 1 ? <p className="series-window-note">{text(copy.windowNote, { start: number(range.start + 1), end: number(range.end), total: number(section.groups.length), maximum: number(SERIES_DIRECTORY_RANGE_SIZE) })}</p> : null}
                  <div className="series-group-grid">
                    {visibleGroups.map((group) => {
                      const selected = groupDefault(group, activeCandidateID);
                      if (!selected) return null;
                      const current = groupContains(group, activeCandidateID);
                      const nextItem = nextSeriesReadableFromOutline(outline, selected.candidate_id);
                      const groupNumber = (outline.groupIndex.get(group.key) ?? 0) + 1;
                      return (
                        <article id={current ? "series-current-entry" : undefined} className={`series-group ${current ? "is-active" : ""}`} key={group.key}>
                          <button className="series-group-primary" type="button" aria-current={current ? "true" : undefined} disabled={!selected.can_read} onClick={() => onOpen(selected, nextItem)}>
                            <span>{number(groupNumber, { minimumIntegerDigits: 3, useGrouping: false })}</span>
                            <strong>{group.label}</strong>
                            <small>{entryMeta(selected, text, number)}</small>
                            <em>{progressLabel(selected, text, number)}</em>
                          </button>
                          {group.items.length > 1 ? (
                            <details className="series-group-entries" open={current}>
                              <summary>{text(copy.collectedEntries, { count: number(group.items.length) })}</summary>
                              <div>
                                {group.items.map((item, itemIndex) => {
                                  const itemCurrent = item.candidate_id === activeCandidateID;
                                  return <button type="button" className={itemCurrent ? "is-active" : ""} aria-current={itemCurrent ? "true" : undefined} disabled={!item.can_read} onClick={() => onOpen(item, nextSeriesReadableFromOutline(outline, item.candidate_id))} key={item.candidate_id}><span>{number(itemIndex + 1, { minimumIntegerDigits: 2, useGrouping: false })}</span><strong>{itemTitle(item, text(copy.unnamedEntry))}</strong><small>{entryMeta(item, text, number)}</small><em>{progressLabel(item, text, number)}</em></button>;
                                })}
                              </div>
                            </details>
                          ) : null}
                        </article>
                      );
                    })}
                  </div>
                </div>
              ) : null}
            </section>
          );
        })}
      </div>
    </section>
  );
}
