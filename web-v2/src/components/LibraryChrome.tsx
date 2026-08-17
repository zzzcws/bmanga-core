import { forwardRef } from "react";

import { useI18n, type LocalizedText } from "../i18n";
import type { CatalogMode, CatalogSort } from "../lib/browseRoute";
import { catalogModeOptions, catalogSortOptions } from "../lib/catalogPresentation";

const copy = {
  collectionKicker: { "zh-CN": "THE COLLECTION", en: "THE COLLECTION", ja: "コレクション" },
  title: { "zh-CN": "私人书库", en: "Private library", ja: "プライベートライブラリ" },
  description: {
    "zh-CN": "让封面先说话，再从作者、页数与阅读痕迹里找到下一册。",
    en: "Let the covers speak first, then find your next book through creators, page counts, and reading history.",
    ja: "まず表紙を眺め、作者・ページ数・読書履歴から次の一冊を見つけましょう。",
  },
  overview: { "zh-CN": "当前馆藏概况", en: "Current library overview", ja: "現在のライブラリ概要" },
  privateCatalogue: { "zh-CN": "PRIVATE CATALOGUE", en: "PRIVATE CATALOGUE", ja: "プライベートカタログ" },
  unavailable: { "zh-CN": "暂不可用", en: "Unavailable", ja: "利用できません" },
  organizing: { "zh-CN": "整理中", en: "Organizing", ja: "整理中" },
  browseScope: { "zh-CN": "浏览范围", en: "Browse scope", ja: "閲覧範囲" },
  contentType: { "zh-CN": "内容类型", en: "Content type", ja: "コンテンツタイプ" },
  libraryUnavailable: { "zh-CN": "馆藏暂时无法读取", en: "The library is temporarily unavailable", ja: "ライブラリを一時的に読み込めません" },
  organizingScope: { "zh-CN": "正在整理当前范围", en: "Organizing the current scope", ja: "現在の範囲を整理しています" },
  pageLabel: { "zh-CN": "第 {page} 页，共 {pages} 页", en: "Page {page} of {pages}", ja: "{pages} ページ中 {page} ページ目" },
  pageKicker: { "zh-CN": "PAGE", en: "PAGE", ja: "ページ" },
  sort: { "zh-CN": "排序", en: "Sort", ja: "並び順" },
  modeAllLabel: { "zh-CN": "全部", en: "All", ja: "すべて" },
  modeAllKicker: { "zh-CN": "ALL WORKS", en: "ALL WORKS", ja: "すべての作品" },
  modeAllDescription: { "zh-CN": "同人本与漫画系列统一陈列", en: "Browse doujin works and series together", ja: "同人誌と漫画シリーズをまとめて表示" },
  modeDoujinLabel: { "zh-CN": "同人本", en: "Doujin works", ja: "同人誌" },
  modeDoujinKicker: { "zh-CN": "DOUJIN ARCHIVE", en: "DOUJIN ARCHIVE", ja: "同人誌アーカイブ" },
  modeDoujinDescription: { "zh-CN": "按册浏览独立作品与合本", en: "Browse standalone works and collected editions", ja: "単独作品と合本を一冊ずつ閲覧" },
  modeSeriesLabel: { "zh-CN": "漫画系列", en: "Series", ja: "漫画シリーズ" },
  modeSeriesKicker: { "zh-CN": "SERIES INDEX", en: "SERIES INDEX", ja: "シリーズ索引" },
  modeSeriesDescription: { "zh-CN": "按系列进入章节目录", en: "Open chapter directories by series", ja: "シリーズごとにチャプター一覧を表示" },
  sortAdded: { "zh-CN": "最近入库", en: "Recently added", ja: "追加日の新しい順" },
  sortTitle: { "zh-CN": "标题 A–Z", en: "Title A–Z", ja: "タイトル A–Z" },
  sortPages: { "zh-CN": "页数最多", en: "Most pages", ja: "ページ数の多い順" },
} satisfies Record<string, LocalizedText>;

const modeCopy: Record<CatalogMode, Readonly<{ label: LocalizedText; kicker: LocalizedText; description: LocalizedText }>> = {
  all: { label: copy.modeAllLabel, kicker: copy.modeAllKicker, description: copy.modeAllDescription },
  doujin: { label: copy.modeDoujinLabel, kicker: copy.modeDoujinKicker, description: copy.modeDoujinDescription },
  series: { label: copy.modeSeriesLabel, kicker: copy.modeSeriesKicker, description: copy.modeSeriesDescription },
};

const sortCopy: Record<CatalogSort, LocalizedText> = {
  added_desc: copy.sortAdded,
  title_asc: copy.sortTitle,
  pages_desc: copy.sortPages,
};

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
  const { number, text } = useI18n();
  const currentMode = modeCopy[mode];
  const unavailable = loading || error;
  return (
    <header className="catalog-header library-masthead">
      <div className="library-title-block">
        <span className="eyebrow">{text(copy.collectionKicker)}</span>
        <h1 className="page-title">{text(copy.title)}</h1>
        <p>{text(copy.description)}</p>
      </div>
      <aside className="library-edition" aria-label={text(copy.overview)}>
        <span>{text(copy.privateCatalogue)}</span>
        <strong>{unavailable ? "—" : number(total)}</strong>
        <small>{text(currentMode.kicker)} · {error ? text(copy.unavailable) : loading ? text(copy.organizing) : `${number(page)} / ${number(pages)}`}</small>
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
  const { number, text } = useI18n();
  const currentMode = modeCopy[mode];
  const totalPages = error || loading ? "—" : number(pages);
  return (
    <div className="library-toolbar library-commandbar" aria-busy={loading} data-state={error ? "error" : loading ? "loading" : "ready"} ref={ref} tabIndex={-1}>
      <div className="library-scope">
        <span className="library-control-label">{text(copy.browseScope)}</span>
        <div className="filter-tabs" role="group" aria-label={text(copy.contentType)}>
          {catalogModeOptions.map((option) => (
            <button
              type="button"
              disabled={disabled}
              className={`filter-chip ${mode === option.id ? "active" : ""}`}
              aria-pressed={mode === option.id}
              onClick={() => onModeChange(option.id)}
              key={option.id}
            >
              {text(modeCopy[option.id].label)}
            </button>
          ))}
        </div>
        <small className="library-scope-note">{error ? text(copy.libraryUnavailable) : loading ? text(copy.organizingScope) : text(currentMode.description)}</small>
      </div>
      <div className="library-command-meta">
        <span className="library-page-brief" aria-label={text(copy.pageLabel, { page: number(page), pages: totalPages })}>
          <small>{text(copy.pageKicker)}</small>
          <strong>{number(page, { minimumIntegerDigits: 2, useGrouping: false })}</strong>
          <span>/ {totalPages}</span>
        </span>
        <label className="select-field">
          <span>{text(copy.sort)}</span>
          <select disabled={disabled} value={sort} onChange={(event) => onSortChange(event.target.value as CatalogSort)}>
            {catalogSortOptions.map((option) => <option value={option.id} key={option.id}>{text(sortCopy[option.id])}</option>)}
          </select>
        </label>
      </div>
    </div>
  );
});
