import { forwardRef, type FormEvent } from "react";

import { useI18n, type LocalizedText } from "../i18n";
import type { ReaderFitMode } from "../types";

const copy = {
  loadingPage: {
    "zh-CN": "正在从第 {current} 页翻到第 {requested} 页，共 {count} 页",
    en: "Loading page {requested} from page {current}, {count} pages total",
    ja: "{current} ページ目から {requested} ページ目を読み込み中、全 {count} ページ",
  },
  currentPage: { "zh-CN": "第 {current} 页，共 {count} 页", en: "Page {current} of {count}", ja: "全 {count} ページ中 {current} ページ目" },
  exit: { "zh-CN": "退出阅读", en: "Exit reader", ja: "リーダーを閉じる" },
  readingKicker: { "zh-CN": "PRIVATE READING", en: "PRIVATE READING", ja: "プライベート読書" },
  keyHint: { "zh-CN": "← → 翻页 · ↑ ↓ 滚动", en: "← → Turn page · ↑ ↓ Scroll", ja: "← → ページ移動 · ↑ ↓ スクロール" },
  nextChapter: { "zh-CN": "下一话", en: "Next chapter", ja: "次の話" },
  leftPanel: { "zh-CN": "左半页 →", en: "Left half →", ja: "左半分 →" },
  finishChapter: { "zh-CN": "读完本话 →", en: "Finish chapter →", ja: "この話を読み終える →" },
  nextPage: { "zh-CN": "下一页 →", en: "Next page →", ja: "次のページ →" },
  nextChapterArrow: { "zh-CN": "下一话 →", en: "Next chapter →", ja: "次の話 →" },
  finished: { "zh-CN": "已读完", en: "Finished", ja: "読了" },
  controls: { "zh-CN": "阅读控制", en: "Reader controls", ja: "リーダー操作" },
  previousPage: { "zh-CN": "← 上一页", en: "← Previous page", ja: "← 前のページ" },
  fitGroup: { "zh-CN": "页面适配", en: "Page fit", ja: "ページ表示" },
  fitPage: { "zh-CN": "整页", en: "Fit page", ja: "ページ全体" },
  fitWidth: { "zh-CN": "适宽", en: "Fit width", ja: "幅に合わせる" },
  splitWide: { "zh-CN": "横页拆分", en: "Split wide pages", ja: "見開きを分割" },
  first: { "zh-CN": "首页", en: "First", ja: "最初" },
  pageNumber: { "zh-CN": "页码", en: "Page", ja: "ページ番号" },
  jumpLabel: { "zh-CN": "跳转页码，共 {count} 页", en: "Go to a page, {count} pages total", ja: "ページへ移動、全 {count} ページ" },
  jump: { "zh-CN": "跳转", en: "Go", ja: "移動" },
  last: { "zh-CN": "末页", en: "Last", ja: "最後" },
  pending: { "zh-CN": "待同步 {count}", en: "Pending sync: {count}", ja: "同期待ち {count}" },
  synced: { "zh-CN": "进度已同步", en: "Progress synced", ja: "進捗を同期済み" },
} satisfies Record<string, LocalizedText>;

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
  const { number, text } = useI18n();
  const pageLabel = imageLoading && requestedIndex !== currentIndex
    ? `${number(currentIndex + 1, { minimumIntegerDigits: 2, useGrouping: false })} → ${number(requestedIndex + 1, { minimumIntegerDigits: 2, useGrouping: false })}`
    : `${number(currentIndex + 1, { minimumIntegerDigits: 2, useGrouping: false })} / ${number(pageCount, { minimumIntegerDigits: 2, useGrouping: false })}`;
  const livePageLabel = imageLoading && requestedIndex !== currentIndex
    ? text(copy.loadingPage, { current: number(currentIndex + 1), requested: number(requestedIndex + 1), count: number(pageCount) })
    : text(copy.currentPage, { current: number(currentIndex + 1), count: number(pageCount) });
  return (
    <header className="reader-topbar" aria-hidden={inactive ? true : undefined} inert={inactive ? true : undefined} onMouseEnter={onReveal}>
      <button ref={ref} type="button" className="icon-button" onClick={onClose} aria-label={text(copy.exit)}>×</button>
      <div className="reader-title-stack"><small>{text(copy.readingKicker)} · {kind}</small><strong>{title}</strong></div>
      <span className="reader-key-hint" aria-hidden="true">{text(copy.keyHint)}</span>
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
  nextItemLabel,
  onFitChange,
  onPageDraftChange,
  onPageDraftCommit,
  onReveal,
}: ReaderControlsProps) {
  const { number, text } = useI18n();
  const localizedNextItemLabel = nextItemLabel || text(copy.nextChapter);
  const submitPage = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onPageDraftCommit();
  };
  const nextLabel = splitWideActive && !ending
    ? (splitPanel < 1 ? text(copy.leftPanel) : requestedIndex >= pageCount - 1 ? text(copy.finishChapter) : text(copy.nextPage))
    : ending
    ? (hasNextItem ? text(copy.nextChapterArrow) : text(copy.finished))
    : requestedIndex >= pageCount - 1 ? text(copy.finishChapter) : text(copy.nextPage);
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
    <nav className="reader-controls" aria-label={text(copy.controls)} aria-hidden={inactive ? true : undefined} inert={inactive ? true : undefined} onMouseEnter={onReveal}>
      <button type="button" onClick={onPrevious} disabled={calibrationOpen || (!ending && requestedIndex <= 0 && (!splitWideActive || splitPanel <= 0))}>{text(copy.previousPage)}</button>
      <div className="reader-fit-toggle" role="group" aria-label={text(copy.fitGroup)}><button type="button" aria-pressed={fitMode === "fit-page"} onClick={() => onFitChange("fit-page")}>{text(copy.fitPage)}</button><button type="button" aria-pressed={fitMode === "fit-width"} onClick={() => onFitChange("fit-width")}>{text(copy.fitWidth)}</button><button type="button" aria-pressed={fitMode === "split-wide"} onClick={() => onFitChange("split-wide")}>{text(copy.splitWide)}</button></div>
      <form className="reader-page-jump" onSubmit={submitPage}><button type="button" className="reader-edge-jump" disabled={calibrationOpen || (firstPhysicalPage && !canRewindSplitPanel)} onClick={jumpToFirst}>{text(copy.first)}</button><label htmlFor="reader-page-input">{text(copy.pageNumber)}</label><input id="reader-page-input" inputMode="numeric" pattern="[0-9]*" value={pageDraft} disabled={calibrationOpen} onChange={(event) => onPageDraftChange(event.target.value.replace(/\D+/gu, ""))} aria-label={text(copy.jumpLabel, { count: number(pageCount) })} /><span>/ {number(pageCount)}</span><button type="submit" className="reader-page-submit" disabled={calibrationOpen}>{text(copy.jump)}</button><button type="button" className="reader-edge-jump" disabled={calibrationOpen || (lastPhysicalPage && !canRewindSplitPanel)} onClick={jumpToLast}>{text(copy.last)}</button></form>
      <span className="reader-sync-state" data-state={pendingProgressCount ? "pending" : "synced"}>{pendingProgressCount ? text(copy.pending, { count: number(pendingProgressCount) }) : text(copy.synced)}</span>
      <div className="reader-secondary-actions">{onOpenNextItem ? <button type="button" title={localizedNextItemLabel} onClick={onOpenNextItem}>{localizedNextItemLabel}</button> : null}</div>
      <button type="button" onClick={onNext} disabled={calibrationOpen || (ending && !hasNextItem) || (imageLoading && requestedIndex >= pageCount - 1)}>{nextLabel}</button>
    </nav>
  );
}

export function ReaderProgress({ currentIndex, pageCount }: { currentIndex: number; pageCount: number }) {
  const percent = pageCount > 0 ? ((currentIndex + 1) / pageCount) * 100 : 0;
  return <footer className="reader-progress" aria-hidden="true"><span style={{ width: `${percent}%` }} /></footer>;
}
