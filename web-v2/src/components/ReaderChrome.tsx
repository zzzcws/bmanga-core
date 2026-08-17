import { forwardRef, type FormEvent } from "react";

import type { ReaderFitMode } from "../types";

export type ActiveReaderFitMode = Extract<ReaderFitMode, "fit-page" | "fit-width" | "split-wide">;

export interface ReaderTopbarProps {
  title: string;
  kind: "SERIES" | "DOUJIN" | "MANGA";
  currentIndex: number;
  requestedIndex: number;
  pageCount: number;
  imageLoading: boolean;
  inactive?: boolean;
  onClose: () => void;
  onReveal: () => void;
}

export const ReaderTopbar = forwardRef<HTMLButtonElement, ReaderTopbarProps>(function ReaderTopbar({
  title,
  kind,
  currentIndex,
  requestedIndex,
  pageCount,
  imageLoading,
  inactive = false,
  onClose,
  onReveal,
}, ref) {
  const pageLabel = imageLoading && requestedIndex !== currentIndex
    ? `${String(currentIndex + 1).padStart(2, "0")} → ${String(requestedIndex + 1).padStart(2, "0")}`
    : `${String(currentIndex + 1).padStart(2, "0")} / ${String(pageCount).padStart(2, "0")}`;
  const livePageLabel = imageLoading && requestedIndex !== currentIndex
    ? `正在从第 ${currentIndex + 1} 页翻到第 ${requestedIndex + 1} 页，共 ${pageCount} 页`
    : `第 ${currentIndex + 1} 页，共 ${pageCount} 页`;
  return (
    <header className="reader-topbar" aria-hidden={inactive ? true : undefined} inert={inactive ? true : undefined} onMouseEnter={onReveal}>
      <button ref={ref} type="button" className="icon-button" onClick={onClose} aria-label="退出阅读">×</button>
      <div className="reader-title-stack"><small>PRIVATE READING · {kind}</small><strong>{title}</strong></div>
      <span className="reader-key-hint" aria-hidden="true">← → 翻页 · ↑ ↓ 滚动</span>
      <span className="reader-page-count" aria-hidden="true">{pageLabel}</span>
      <span className="sr-only" role="status" aria-live="polite">{livePageLabel}</span>
    </header>
  );
});

export interface ReaderControlsProps {
  fitMode: ActiveReaderFitMode;
  pageDraft: string;
  pageCount: number;
  requestedIndex: number;
  calibrationOpen: boolean;
  ending: boolean;
  hasNextItem: boolean;
  imageLoading: boolean;
  splitWideActive: boolean;
  splitPanel: number;
  pendingProgressCount: number;
  inactive?: boolean;
  onPrevious: () => void;
  onNext: () => void;
  onFirst: () => void;
  onLast: () => void;
  onOpenNextItem?: () => void;
  nextItemLabel?: string;
  onFitChange: (mode: ActiveReaderFitMode) => void;
  onPageDraftChange: (value: string) => void;
  onPageDraftCommit: () => void;
  onReveal: () => void;
}

export function ReaderControls({
  fitMode,
  pageDraft,
  pageCount,
  requestedIndex,
  calibrationOpen,
  ending,
  hasNextItem,
  imageLoading,
  splitWideActive,
  splitPanel,
  pendingProgressCount,
  inactive = false,
  onPrevious,
  onNext,
  onFirst,
  onLast,
  onOpenNextItem,
  nextItemLabel = "下一话",
  onFitChange,
  onPageDraftChange,
  onPageDraftCommit,
  onReveal,
}: ReaderControlsProps) {
  const submitPage = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onPageDraftCommit();
  };
  const nextLabel = splitWideActive && !ending
    ? (splitPanel < 1 ? "左半页 →" : requestedIndex >= pageCount - 1 ? "读完本话 →" : "下一页 →")
    : ending
    ? (hasNextItem ? "下一话 →" : "已读完")
    : requestedIndex >= pageCount - 1 ? "读完本话 →" : "下一页 →";
  const firstPhysicalPage = requestedIndex <= 0;
  const lastPhysicalPage = requestedIndex >= pageCount - 1;
  const canRewindSplitPanel = !ending && !imageLoading && splitWideActive && splitPanel > 0;
  const jumpToFirst = () => {
    if (firstPhysicalPage && canRewindSplitPanel) onPrevious();
    else onFirst();
  };
  const jumpToLast = () => {
    if (lastPhysicalPage && canRewindSplitPanel) onPrevious();
    else onLast();
  };
  return (
    <nav className="reader-controls" aria-label="阅读控制" aria-hidden={inactive ? true : undefined} inert={inactive ? true : undefined} onMouseEnter={onReveal}>
      <button type="button" onClick={onPrevious} disabled={calibrationOpen || (!ending && requestedIndex <= 0 && (!splitWideActive || splitPanel <= 0))}>← 上一页</button>
      <div className="reader-fit-toggle" role="group" aria-label="页面适配"><button type="button" aria-pressed={fitMode === "fit-page"} onClick={() => onFitChange("fit-page")}>整页</button><button type="button" aria-pressed={fitMode === "fit-width"} onClick={() => onFitChange("fit-width")}>适宽</button><button type="button" aria-pressed={fitMode === "split-wide"} onClick={() => onFitChange("split-wide")}>横页拆分</button></div>
      <form className="reader-page-jump" onSubmit={submitPage}><button type="button" className="reader-edge-jump" disabled={calibrationOpen || (firstPhysicalPage && !canRewindSplitPanel)} onClick={jumpToFirst}>首页</button><label htmlFor="reader-page-input">页码</label><input id="reader-page-input" inputMode="numeric" pattern="[0-9]*" value={pageDraft} disabled={calibrationOpen} onChange={(event) => onPageDraftChange(event.target.value.replace(/\D+/gu, ""))} aria-label={`跳转页码，共 ${pageCount} 页`} /><span>/ {pageCount}</span><button type="submit" className="reader-page-submit" disabled={calibrationOpen}>跳转</button><button type="button" className="reader-edge-jump" disabled={calibrationOpen || (lastPhysicalPage && !canRewindSplitPanel)} onClick={jumpToLast}>末页</button></form>
      <span className="reader-sync-state" data-state={pendingProgressCount ? "pending" : "synced"}>{pendingProgressCount ? `待同步 ${pendingProgressCount}` : "进度已同步"}</span>
      <div className="reader-secondary-actions">{onOpenNextItem ? <button type="button" title={nextItemLabel} onClick={onOpenNextItem}>{nextItemLabel}</button> : null}</div>
      <button type="button" onClick={onNext} disabled={calibrationOpen || (ending && !hasNextItem) || (imageLoading && requestedIndex >= pageCount - 1)}>{nextLabel}</button>
    </nav>
  );
}

export function ReaderProgress({ currentIndex, pageCount }: { currentIndex: number; pageCount: number }) {
  const percent = pageCount > 0 ? ((currentIndex + 1) / pageCount) * 100 : 0;
  return <footer className="reader-progress" aria-hidden="true"><span style={{ width: `${percent}%` }} /></footer>;
}
