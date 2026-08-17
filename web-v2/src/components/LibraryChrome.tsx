import { forwardRef } from "react";

import type { CatalogMode, CatalogSort } from "../lib/browseRoute";
import { catalogModeOption, catalogModeOptions, catalogSortOptions } from "../lib/catalogPresentation";

export function LibraryMasthead({
  error,
  loading,
  mode,
  page,
  pages,
  total,
}: {
  error: boolean;
  loading: boolean;
  mode: CatalogMode;
  page: number;
  pages: number;
  total: number;
}) {
  const currentMode = catalogModeOption(mode);
  const unavailable = loading || error;
  return (
    <header className="catalog-header library-masthead">
      <div className="library-title-block">
        <span className="eyebrow">THE COLLECTION</span>
        <h1 className="page-title">私人书库</h1>
        <p>让封面先说话，再从作者、页数与阅读痕迹里找到下一册。</p>
      </div>
      <aside className="library-edition" aria-label="当前馆藏概况">
        <span>PRIVATE CATALOGUE</span>
        <strong>{unavailable ? "—" : total.toLocaleString("zh-CN")}</strong>
        <small>{currentMode.english} · {error ? "暂不可用" : loading ? "整理中" : `${page} / ${pages}`}</small>
      </aside>
    </header>
  );
}

export const LibraryToolbar = forwardRef<HTMLDivElement, {
  disabled?: boolean;
  error: boolean;
  loading: boolean;
  mode: CatalogMode;
  page: number;
  pages: number;
  sort: CatalogSort;
  onModeChange: (mode: CatalogMode) => void;
  onSortChange: (sort: CatalogSort) => void;
}>(function LibraryToolbar({ disabled = false, error, loading, mode, page, pages, sort, onModeChange, onSortChange }, ref) {
  const currentMode = catalogModeOption(mode);
  const totalPages = error || loading ? "—" : String(pages);
  return (
    <div className="library-toolbar library-commandbar" aria-busy={loading} data-state={error ? "error" : loading ? "loading" : "ready"} ref={ref} tabIndex={-1}>
      <div className="library-scope">
        <span className="library-control-label">浏览范围</span>
        <div className="filter-tabs" role="group" aria-label="内容类型">
          {catalogModeOptions.map((option) => (
            <button
              type="button"
              disabled={disabled}
              className={`filter-chip ${mode === option.id ? "active" : ""}`}
              aria-pressed={mode === option.id}
              onClick={() => onModeChange(option.id)}
              key={option.id}
            >
              {option.label}
            </button>
          ))}
        </div>
        <small className="library-scope-note">{error ? "馆藏暂时无法读取" : loading ? "正在整理当前范围" : currentMode.description}</small>
      </div>
      <div className="library-command-meta">
        <span className="library-page-brief" aria-label={`第 ${page} 页，共 ${totalPages} 页`}>
          <small>PAGE</small>
          <strong>{String(page).padStart(2, "0")}</strong>
          <span>/ {totalPages}</span>
        </span>
        <label className="select-field">
          <span>排序</span>
          <select disabled={disabled} value={sort} onChange={(event) => onSortChange(event.target.value as CatalogSort)}>
            {catalogSortOptions.map((option) => <option value={option.id} key={option.id}>{option.label}</option>)}
          </select>
        </label>
      </div>
    </div>
  );
});
