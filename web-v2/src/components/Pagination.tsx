import { useEffect, useState, type FormEvent } from "react";

import { useI18n, type LocalizedText } from "../i18n";
import { clampPaginationPage, paginationTokens } from "../lib/pagination";

const copy = {
  first: { "zh-CN": "首页", en: "First", ja: "最初" },
  previous: { "zh-CN": "上一页", en: "Previous", ja: "前へ" },
  pagesLabel: { "zh-CN": "第 {page} 页，共 {pages} 页", en: "Page {page} of {pages}", ja: "{pages} ページ中 {page} ページ目" },
  pageLabel: { "zh-CN": "第 {page} 页", en: "Page {page}", ja: "{page} ページ目" },
  summaryPrefix: { "zh-CN": "第 ", en: "Page ", ja: "" },
  summarySuffix: { "zh-CN": " 页", en: "", ja: " ページ" },
  next: { "zh-CN": "下一页", en: "Next", ja: "次へ" },
  last: { "zh-CN": "末页", en: "Last", ja: "最後" },
  jumpTo: { "zh-CN": "跳至", en: "Go to", ja: "移動先" },
  inputLabel: { "zh-CN": "输入页码，范围 1 到 {pages}", en: "Enter a page number from 1 to {pages}", ja: "1 から {pages} までのページ番号を入力" },
  pageUnit: { "zh-CN": "页", en: "page", ja: "ページ" },
  jump: { "zh-CN": "跳转", en: "Go", ja: "移動" },
} satisfies Record<string, LocalizedText>;

export interface PaginationProps {
  disabled?: boolean;
  page: number;
  pages: number;
  label: string;
  kicker?: string;
  onPageChange: (page: number) => void;
}

export function Pagination({ disabled = false, page, pages, label, kicker, onPageChange }: PaginationProps) {
  const { number, text } = useI18n();
  const [draft, setDraft] = useState(String(page));
  const safePage = clampPaginationPage(page, pages);
  const safePages = Number.isFinite(pages) ? Math.max(1, Math.floor(pages)) : 1;

  useEffect(() => setDraft(String(safePage)), [safePage]);

  const goToPage = (nextPage: number) => {
    const target = clampPaginationPage(nextPage, safePages);
    setDraft(String(target));
    if (target !== safePage) onPageChange(target);
  };

  const submitJump = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const requested = Number.parseInt(draft, 10);
    if (!Number.isFinite(requested)) {
      setDraft(String(safePage));
      return;
    }
    goToPage(requested);
  };

  return (
    <nav className="pagination" aria-label={label}>
      {kicker ? <span className="pagination-kicker" aria-hidden="true">{kicker}</span> : null}
      <div className="pagination-main">
        <button className="pagination-edge" type="button" disabled={disabled || safePage === 1} onClick={() => goToPage(1)}>{text(copy.first)}</button>
        <button type="button" disabled={disabled || safePage === 1} onClick={() => goToPage(safePage - 1)}>{text(copy.previous)}</button>
        <div className="pagination-pages" aria-label={text(copy.pagesLabel, { page: number(safePage), pages: number(safePages) })}>
          {paginationTokens(safePage, safePages).map((token, index) => token === "ellipsis"
            ? <span className="pagination-ellipsis" aria-hidden="true" key={`ellipsis-${index}`}>…</span>
            : <button className="pagination-page" type="button" disabled={disabled} aria-label={text(copy.pageLabel, { page: number(token) })} aria-current={token === safePage ? "page" : undefined} onClick={() => goToPage(token)} key={token}>{number(token)}</button>)}
        </div>
        <span className="pagination-summary" aria-live="polite">{text(copy.summaryPrefix)}<strong>{number(safePage)}</strong> / {number(safePages)}{text(copy.summarySuffix)}</span>
        <button type="button" disabled={disabled || safePage === safePages} onClick={() => goToPage(safePage + 1)}>{text(copy.next)}</button>
        <button className="pagination-edge" type="button" disabled={disabled || safePage === safePages} onClick={() => goToPage(safePages)}>{text(copy.last)}</button>
      </div>
      <form className="pagination-jump" onSubmit={submitJump} noValidate>
        <label><span>{text(copy.jumpTo)}</span><input type="number" disabled={disabled} min="1" max={safePages} step="1" inputMode="numeric" value={draft} onChange={(event) => setDraft(event.target.value)} aria-label={text(copy.inputLabel, { pages: number(safePages) })} /></label>
        <span>{text(copy.pageUnit)}</span>
        <button type="submit" disabled={disabled}>{text(copy.jump)}</button>
      </form>
    </nav>
  );
}
