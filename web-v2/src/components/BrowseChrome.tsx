import { useEffect, useRef, type FormEventHandler, type ReactNode } from "react";

import { useI18n, type LocalizedText } from "../i18n";
import type { DiscoverMode } from "../lib/browseRoute";

const copy = {
  readingProgress: {
    "zh-CN": "阅读进度 {percent}%",
    en: "Reading progress: {percent}%",
    ja: "読書進捗 {percent}%",
  },
  chanceKicker: {
    "zh-CN": "CURATED BY CHANCE",
    en: "CURATED BY CHANCE",
    ja: "偶然が選ぶ一冊",
  },
  drawing: {
    "zh-CN": "正在抽取…",
    en: "Choosing…",
    ja: "選んでいます…",
  },
  drawOne: {
    "zh-CN": "抽一本",
    en: "Choose one",
    ja: "一冊選ぶ",
  },
  refreshing: {
    "zh-CN": "正在换一批…",
    en: "Refreshing…",
    ja: "入れ替え中…",
  },
  refresh: {
    "zh-CN": "换一批",
    en: "Refresh selection",
    ja: "別の候補を見る",
  },
  discoveryScope: {
    "zh-CN": "发现范围",
    en: "Discovery scope",
    ja: "発見する範囲",
  },
  searchKicker: {
    "zh-CN": "SEARCH YOUR SHELF",
    en: "SEARCH YOUR SHELF",
    ja: "本棚を検索",
  },
  searchFor: {
    "zh-CN": "在馆藏里寻找",
    en: "Search the library for",
    ja: "ライブラリから検索",
  },
  searchPromptFirst: {
    "zh-CN": "记得一点名字，",
    en: "A fragment of a name",
    ja: "名前の一部だけでも、",
  },
  searchPromptSecond: {
    "zh-CN": "就足够找到它。",
    en: "is enough to find it.",
    ja: "作品を見つけられます。",
  },
  searchDescription: {
    "zh-CN": "支持作品标题、作者与汉化组；不需要输入完整名称。",
    en: "Search by title, creator, or translation group; a full name is not required.",
    ja: "作品名・作者・翻訳グループから検索できます。完全な名前を入力する必要はありません。",
  },
  catalogSearch: {
    "zh-CN": "馆藏检索",
    en: "Library search",
    ja: "ライブラリ検索",
  },
  searchExample: {
    "zh-CN": "例如：炎拳、藤本树、汉化组",
    en: "For example: title, creator, translation group",
    ja: "例：作品名、作者、翻訳グループ",
  },
  search: {
    "zh-CN": "搜索",
    en: "Search",
    ja: "検索",
  },
  searchShortcut: {
    "zh-CN": "按 Enter 开始 · Ctrl K 可随时回到顶部搜索",
    en: "Press Enter to search · Ctrl K returns to the top search field",
    ja: "Enter で検索 · Ctrl K で上部の検索欄へ移動",
  },
  clueKicker: {
    "zh-CN": "BEGIN WITH A CLUE",
    en: "BEGIN WITH A CLUE",
    ja: "手がかりから始める",
  },
  clueTitle: {
    "zh-CN": "从一句关键词开始。",
    en: "Start with a keyword.",
    ja: "キーワードから始めましょう。",
  },
  clueDescription: {
    "zh-CN": "结果会保留作品类型与排序条件；重新搜索时会自动回到第一页。",
    en: "Results preserve the work type and sort order; a new search returns to the first page.",
    ja: "作品タイプと並び順は維持され、新しい検索では自動的に最初のページへ戻ります。",
  },
  browseAll: {
    "zh-CN": "浏览全部馆藏",
    en: "Browse the full library",
    ja: "すべての作品を見る",
  },
  letLibraryChoose: {
    "zh-CN": "让书库替我挑一本",
    en: "Let the library choose for me",
    ja: "ライブラリに一冊選んでもらう",
  },
  searchableFields: {
    "zh-CN": "可搜索字段",
    en: "Searchable fields",
    ja: "検索できる項目",
  },
  workTitle: {
    "zh-CN": "作品标题",
    en: "Work title",
    ja: "作品名",
  },
  creatorName: {
    "zh-CN": "作者姓名",
    en: "Creator name",
    ja: "作者名",
  },
  translationAndSource: {
    "zh-CN": "汉化与来源",
    en: "Translation and source",
    ja: "翻訳と出典",
  },
} satisfies Record<string, LocalizedText>;

interface EditorialMastheadProps {
  eyebrow: string;
  title: ReactNode;
  titleID: string;
  meta?: ReactNode;
  folio?: string;
  className?: string;
}

export function EditorialMasthead({
  eyebrow,
  title,
  titleID,
  meta,
  folio,
  className = "",
}: EditorialMastheadProps) {
  return (
    <header className={`editorial-masthead ${className}`.trim()} aria-labelledby={titleID}>
      <div className="editorial-masthead-copy home-intro-copy">
        <span className="eyebrow">{eyebrow}</span>
        <h1 className="page-title" id={titleID}>{title}</h1>
      </div>
      {meta || folio ? (
        <div className="editorial-masthead-aside">
          {folio ? <span aria-hidden="true">{folio}</span> : null}
          {meta ? <div>{meta}</div> : null}
        </div>
      ) : null}
    </header>
  );
}

interface EveningHeroProps {
  cover: ReactNode;
  eyebrow: string;
  title: ReactNode;
  subtitle: ReactNode;
  progressPercent?: number | null;
  action: string;
  onAction: () => void;
  note: string;
  noteLabel: string;
  noteMeta: ReactNode;
  folio: string;
  coverBadge: string;
}

export function EveningHero({
  cover,
  eyebrow,
  title,
  subtitle,
  progressPercent,
  action,
  onAction,
  note,
  noteLabel,
  noteMeta,
  folio,
  coverBadge,
}: EveningHeroProps) {
  const { number, text } = useI18n();
  const formattedPercent = number(progressPercent ?? 0, { maximumFractionDigits: 0 });
  return (
    <section className="hero evening-hero" aria-labelledby="evening-hero-title">
      <span className="evening-hero-folio" aria-hidden="true">{folio}</span>
      <div className="hero-cover">{cover}<span className="hero-cover-badge" aria-hidden="true">{coverBadge}</span></div>
      <div className="hero-content">
        <span className="eyebrow">{eyebrow}</span>
        <h2 id="evening-hero-title">{title}</h2>
        <p>{subtitle}</p>
        {progressPercent !== null && progressPercent !== undefined ? (
          <div className="progress" role="progressbar" aria-label={text(copy.readingProgress, { percent: formattedPercent })} aria-valuemin={0} aria-valuemax={100} aria-valuenow={progressPercent}>
            <span className="progress-track"><span style={{ width: `${progressPercent}%` }} /></span>
            <output>{formattedPercent}%</output>
          </div>
        ) : null}
        <button className="button primary" type="button" onClick={onAction}>
          {action} <span aria-hidden="true">→</span>
        </button>
      </div>
      <aside className="hero-note">
        <span className="hero-note-label">{noteLabel}</span>
        <blockquote>“{note}”</blockquote>
        <div className="hero-note-meta">{noteMeta}</div>
      </aside>
    </section>
  );
}

interface DiscoveryLeadProps {
  modeLabel: string;
  title: string;
  copy: string;
  loading: boolean;
  opening: boolean;
  onOpenRandom: () => void;
  onRefresh: () => void;
}

export function DiscoveryLead({
  modeLabel,
  title,
  copy: description,
  loading,
  opening,
  onOpenRandom,
  onRefresh,
}: DiscoveryLeadProps) {
  const { text } = useI18n();
  return (
    <section className="discover-lead editorial-dark-panel" aria-labelledby="discover-lead-title">
      <span className="editorial-dark-panel-mark" aria-hidden="true">?</span>
      <div className="discover-lead-copy">
        <span>{text(copy.chanceKicker)} · {modeLabel}</span>
        <h2 id="discover-lead-title">{title}</h2>
        <p>{description}</p>
      </div>
      <div className="discover-actions">
        <button className="discover-primary" type="button" disabled={opening} onClick={onOpenRandom}>
          {opening ? text(copy.drawing) : text(copy.drawOne)}<b aria-hidden="true">→</b>
        </button>
        <button type="button" disabled={loading} onClick={onRefresh}>
          {loading ? text(copy.refreshing) : text(copy.refresh)}
        </button>
      </div>
    </section>
  );
}

export interface DiscoveryModeOption {
  id: DiscoverMode;
  label: string;
  hint: string;
}

interface DiscoveryModeRailProps {
  active: DiscoverMode;
  options: DiscoveryModeOption[];
  onChange: (mode: DiscoverMode) => void;
}

export function DiscoveryModeRail({ active, options, onChange }: DiscoveryModeRailProps) {
  const activeRef = useRef<HTMLButtonElement | null>(null);
  const { number, text } = useI18n();

  useEffect(() => {
    if (!window.matchMedia("(max-width: 760px)").matches) return;
    activeRef.current?.scrollIntoView({ behavior: "auto", block: "nearest", inline: "center" });
  }, [active]);

  return (
    <div className="discover-mode-bar" role="group" aria-label={text(copy.discoveryScope)}>
      {options.map((option, index) => (
        <button
          type="button"
          className={active === option.id ? "active" : ""}
          aria-pressed={active === option.id}
          onClick={() => onChange(option.id)}
          ref={active === option.id ? activeRef : undefined}
          key={option.id}
        >
          <i aria-hidden="true">{number(index + 1, { minimumIntegerDigits: 2, useGrouping: false })}</i>
          <span>{option.label}</span>
          <small>{option.hint}</small>
        </button>
      ))}
    </div>
  );
}

interface MetricLedgerProps {
  label: string;
  items: Array<{ label: string; value: ReactNode }>;
  className?: string;
  tabIndex?: number;
}

export function MetricLedger({ label, items, className = "", tabIndex }: MetricLedgerProps) {
  const { number } = useI18n();
  return (
    <dl className={`metric-ledger ${className}`.trim()} aria-label={label} tabIndex={tabIndex}>
      {items.map((item, index) => (
        <div key={`${item.label}-${index}`}>
          <dt>{item.label}</dt>
          <dd>{item.value}</dd>
          <span aria-hidden="true">{number(index + 1, { minimumIntegerDigits: 2, useGrouping: false })}</span>
        </div>
      ))}
    </dl>
  );
}

interface SearchLeadProps {
  draft: string;
  hasQuery: boolean;
  query: string;
  onDraftChange: (value: string) => void;
  onSubmit: FormEventHandler<HTMLFormElement>;
}

export function SearchLead({ draft, hasQuery, query, onDraftChange, onSubmit }: SearchLeadProps) {
  const { text } = useI18n();
  return (
    <section className="search-hero editorial-dark-panel" aria-labelledby="search-hero-title">
      <span className="editorial-dark-panel-mark search-mark" aria-hidden="true">⌕</span>
      <div>
        <span>{text(copy.searchKicker)}</span>
        <h2 id="search-hero-title">
          {hasQuery
            ? <>{text(copy.searchFor)}<br />“{query}”</>
            : <>{text(copy.searchPromptFirst)}<br />{text(copy.searchPromptSecond)}</>}
        </h2>
        <p>{text(copy.searchDescription)}</p>
      </div>
      <form className="search-command" role="search" onSubmit={onSubmit}>
        <label htmlFor="catalog-search-input">{text(copy.catalogSearch)}</label>
        <div>
          <input
            id="catalog-search-input"
            value={draft}
            onChange={(event) => onDraftChange(event.target.value)}
            placeholder={text(copy.searchExample)}
            autoComplete="off"
          />
          <button type="submit" disabled={!draft.trim()}>{text(copy.search)} <span aria-hidden="true">→</span></button>
        </div>
        <small>{text(copy.searchShortcut)}</small>
      </form>
    </section>
  );
}

interface SearchStartProps {
  onBrowse: () => void;
  onDiscover: () => void;
}

export function SearchStart({ onBrowse, onDiscover }: SearchStartProps) {
  const { number, text } = useI18n();
  return (
    <section className="search-start" aria-labelledby="search-start-title">
      <span>{text(copy.clueKicker)}</span>
      <h2 id="search-start-title">{text(copy.clueTitle)}</h2>
      <p>{text(copy.clueDescription)}</p>
      <div className="search-start-actions">
        <button type="button" onClick={onBrowse}>{text(copy.browseAll)} <b aria-hidden="true">→</b></button>
        <button type="button" onClick={onDiscover}>{text(copy.letLibraryChoose)}</button>
      </div>
      <ul aria-label={text(copy.searchableFields)}>
        <li><b>{number(1, { minimumIntegerDigits: 2, useGrouping: false })}</b><span>{text(copy.workTitle)}</span></li>
        <li><b>{number(2, { minimumIntegerDigits: 2, useGrouping: false })}</b><span>{text(copy.creatorName)}</span></li>
        <li><b>{number(3, { minimumIntegerDigits: 2, useGrouping: false })}</b><span>{text(copy.translationAndSource)}</span></li>
      </ul>
    </section>
  );
}
