import { useEffect, useMemo, useState } from "react";

import { preferredScrollBehavior } from "../lib/motion";
import {
  SERIES_DIRECTORY_RANGE_SIZE,
  seriesDirectoryRangeForGroup,
  seriesDirectoryRangeLabel,
  seriesDirectoryRangeWindow,
} from "../lib/seriesDirectoryRange";
import { buildSeriesOutline, nextSeriesReadableFromOutline, type SeriesOutlineGroup } from "../lib/seriesOrder";
import type { SeriesDetailResponse, WorkSummary } from "../types";

interface SeriesDirectoryProps {
  data: SeriesDetailResponse;
  activeCandidateID?: string;
  onOpen: (item: WorkSummary, nextItem?: WorkSummary) => void;
}

function numberValue(value: unknown, fallback = 0): number {
  const parsed = typeof value === "number" ? value : Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function booleanValue(value: unknown): boolean {
  if (typeof value === "boolean") return value;
  if (typeof value === "number") return value !== 0;
  return ["1", "true", "yes", "on"].includes(String(value || "").trim().toLowerCase());
}

function itemTitle(item: WorkSummary): string {
  return String(item.display_title || item.title || item.relative_path || "未命名条目").trim();
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

function progressLabel(item: WorkSummary): string {
  const progress = itemProgress(item);
  if (!progress) return item.can_read ? "未读" : "暂不可读";
  if (progress.completed) return "已读";
  return progress.count > 0 ? `第 ${progress.index + 1} / ${progress.count} 页` : `${Math.round(progress.percent)}%`;
}

function entryMeta(item: WorkSummary): string {
  const values = [
    itemPageCount(item) ? `${itemPageCount(item)} 页` : "页数待确认",
    String(item.translation_sources || "").trim(),
    String(item.display_library_name || item.library_name || "").trim(),
  ].filter(Boolean);
  return values.slice(0, 3).join(" · ");
}

export function SeriesDirectory({ data, activeCandidateID, onOpen }: SeriesDirectoryProps) {
  const outline = useMemo(() => buildSeriesOutline(data), [data]);
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

  return (
    <section className="series-directory" aria-label="系列章节目录" data-section-count={outline.sections.length} data-entry-count={outline.entries.length}>
      <header className="series-directory-heading">
        <div><span>READING ORDER</span><h2>章节目录</h2></div>
        <p>{data.sectioned ? data.section_summary : (data.series.item_summary || `${outline.entries.length} 个条目`)}</p>
      </header>
      {outline.sections.length > 1 ? (
        <nav className="series-section-nav" aria-label="目录分区">
          {outline.sections.map((section, index) => <button type="button" className={index === activeSectionIndex ? "active" : ""} onClick={() => jumpToSection(index)} key={section.key}><span>{String(index + 1).padStart(2, "0")}</span>{section.title}</button>)}
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
                <span>{String(sectionIndex + 1).padStart(2, "0")}</span>
                <strong>{section.title}</strong>
                <small>{section.groups.length} 组 · {entryCount} 个条目</small>
                <b aria-hidden="true">{open ? "−" : "+"}</b>
              </button>
              {open ? (
                <div className="series-outline-body">
                  {range.pages > 1 ? (
                    <nav className="series-range-controls" aria-label={`${section.title}范围切换`}>
                      <button className="series-range-previous" type="button" disabled={range.index <= 0} onClick={() => changeRange(range.index - 1)}>← 上一段</button>
                      <label className="series-range-select"><span>范围</span><select value={range.index} onChange={(event) => changeRange(Number(event.target.value))}>{Array.from({ length: range.pages }, (_, rangeIndex) => <option value={rangeIndex} key={rangeIndex}>{seriesDirectoryRangeLabel(rangeLabels, rangeIndex)}</option>)}</select></label>
                      <span className="series-range-status" aria-live="polite">{range.index + 1} / {range.pages}</span>
                      <button className="series-range-next" type="button" disabled={range.index >= range.pages - 1} onClick={() => changeRange(range.index + 1)}>下一段 →</button>
                    </nav>
                  ) : null}
                  {range.pages > 1 ? <p className="series-window-note">当前显示第 {range.start + 1}–{range.end} 组，共 {section.groups.length} 组；每段最多 {SERIES_DIRECTORY_RANGE_SIZE} 组。</p> : null}
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
                            <span>{String(groupNumber).padStart(3, "0")}</span>
                            <strong>{group.label}</strong>
                            <small>{entryMeta(selected)}</small>
                            <em>{progressLabel(selected)}</em>
                          </button>
                          {group.items.length > 1 ? (
                            <details className="series-group-entries" open={current}>
                              <summary>{group.items.length} 个收录条目</summary>
                              <div>
                                {group.items.map((item, itemIndex) => {
                                  const itemCurrent = item.candidate_id === activeCandidateID;
                                  return <button type="button" className={itemCurrent ? "is-active" : ""} aria-current={itemCurrent ? "true" : undefined} disabled={!item.can_read} onClick={() => onOpen(item, nextSeriesReadableFromOutline(outline, item.candidate_id))} key={item.candidate_id}><span>{String(itemIndex + 1).padStart(2, "0")}</span><strong>{itemTitle(item)}</strong><small>{entryMeta(item)}</small><em>{progressLabel(item)}</em></button>;
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
