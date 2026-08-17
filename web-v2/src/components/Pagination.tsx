import { useEffect, useState, type FormEvent } from "react";

import { clampPaginationPage, paginationTokens } from "../lib/pagination";

export interface PaginationProps {
  disabled?: boolean;
  page: number;
  pages: number;
  label: string;
  kicker?: string;
  onPageChange: (page: number) => void;
}

export function Pagination({ disabled = false, page, pages, label, kicker, onPageChange }: PaginationProps) {
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
        <button className="pagination-edge" type="button" disabled={disabled || safePage === 1} onClick={() => goToPage(1)}>首页</button>
        <button type="button" disabled={disabled || safePage === 1} onClick={() => goToPage(safePage - 1)}>上一页</button>
        <div className="pagination-pages" aria-label={`第 ${safePage} 页，共 ${safePages} 页`}>
          {paginationTokens(safePage, safePages).map((token, index) => token === "ellipsis"
            ? <span className="pagination-ellipsis" aria-hidden="true" key={`ellipsis-${index}`}>…</span>
            : <button className="pagination-page" type="button" disabled={disabled} aria-label={`第 ${token} 页`} aria-current={token === safePage ? "page" : undefined} onClick={() => goToPage(token)} key={token}>{token}</button>)}
        </div>
        <span className="pagination-summary" aria-live="polite">第 <strong>{safePage}</strong> / {safePages} 页</span>
        <button type="button" disabled={disabled || safePage === safePages} onClick={() => goToPage(safePage + 1)}>下一页</button>
        <button className="pagination-edge" type="button" disabled={disabled || safePage === safePages} onClick={() => goToPage(safePages)}>末页</button>
      </div>
      <form className="pagination-jump" onSubmit={submitJump} noValidate>
        <label><span>跳至</span><input type="number" disabled={disabled} min="1" max={safePages} step="1" inputMode="numeric" value={draft} onChange={(event) => setDraft(event.target.value)} aria-label={`输入页码，范围 1 到 ${safePages}`} /></label>
        <span>页</span>
        <button type="submit" disabled={disabled}>跳转</button>
      </form>
    </nav>
  );
}
