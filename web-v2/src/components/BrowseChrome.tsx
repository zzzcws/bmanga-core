import { useEffect, useRef, type FormEventHandler, type ReactNode } from "react";

import type { DiscoverMode } from "../lib/browseRoute";

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
  return (
    <section className="hero evening-hero" aria-labelledby="evening-hero-title">
      <span className="evening-hero-folio" aria-hidden="true">{folio}</span>
      <div className="hero-cover">{cover}<span className="hero-cover-badge" aria-hidden="true">{coverBadge}</span></div>
      <div className="hero-content">
        <span className="eyebrow">{eyebrow}</span>
        <h2 id="evening-hero-title">{title}</h2>
        <p>{subtitle}</p>
        {progressPercent !== null && progressPercent !== undefined ? (
          <div className="progress" role="progressbar" aria-label={`阅读进度 ${progressPercent}%`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={progressPercent}>
            <span className="progress-track"><span style={{ width: `${progressPercent}%` }} /></span>
            <output>{progressPercent}%</output>
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
  copy,
  loading,
  opening,
  onOpenRandom,
  onRefresh,
}: DiscoveryLeadProps) {
  return (
    <section className="discover-lead editorial-dark-panel" aria-labelledby="discover-lead-title">
      <span className="editorial-dark-panel-mark" aria-hidden="true">?</span>
      <div className="discover-lead-copy">
        <span>CURATED BY CHANCE · {modeLabel}</span>
        <h2 id="discover-lead-title">{title}</h2>
        <p>{copy}</p>
      </div>
      <div className="discover-actions">
        <button className="discover-primary" type="button" disabled={opening} onClick={onOpenRandom}>
          {opening ? "正在抽取…" : "抽一本"}<b aria-hidden="true">→</b>
        </button>
        <button type="button" disabled={loading} onClick={onRefresh}>
          {loading ? "正在换一批…" : "换一批"}
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

  useEffect(() => {
    if (!window.matchMedia("(max-width: 760px)").matches) return;
    activeRef.current?.scrollIntoView({ behavior: "auto", block: "nearest", inline: "center" });
  }, [active]);

  return (
    <div className="discover-mode-bar" role="group" aria-label="发现范围">
      {options.map((option, index) => (
        <button
          type="button"
          className={active === option.id ? "active" : ""}
          aria-pressed={active === option.id}
          onClick={() => onChange(option.id)}
          ref={active === option.id ? activeRef : undefined}
          key={option.id}
        >
          <i aria-hidden="true">{String(index + 1).padStart(2, "0")}</i>
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
  return (
    <dl className={`metric-ledger ${className}`.trim()} aria-label={label} tabIndex={tabIndex}>
      {items.map((item, index) => (
        <div key={`${item.label}-${index}`}>
          <dt>{item.label}</dt>
          <dd>{item.value}</dd>
          <span aria-hidden="true">{String(index + 1).padStart(2, "0")}</span>
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
  return (
    <section className="search-hero editorial-dark-panel" aria-labelledby="search-hero-title">
      <span className="editorial-dark-panel-mark search-mark" aria-hidden="true">⌕</span>
      <div>
        <span>SEARCH YOUR SHELF</span>
        <h2 id="search-hero-title">
          {hasQuery ? <>在馆藏里寻找<br />“{query}”</> : <>记得一点名字，<br />就足够找到它。</>}
        </h2>
        <p>支持作品标题、作者与汉化组；不需要输入完整名称。</p>
      </div>
      <form className="search-command" role="search" onSubmit={onSubmit}>
        <label htmlFor="catalog-search-input">馆藏检索</label>
        <div>
          <input
            id="catalog-search-input"
            value={draft}
            onChange={(event) => onDraftChange(event.target.value)}
            placeholder="例如：炎拳、藤本树、汉化组"
            autoComplete="off"
          />
          <button type="submit" disabled={!draft.trim()}>搜索 <span aria-hidden="true">→</span></button>
        </div>
        <small>按 Enter 开始 · Ctrl K 可随时回到顶部搜索</small>
      </form>
    </section>
  );
}

interface SearchStartProps {
  onBrowse: () => void;
  onDiscover: () => void;
}

export function SearchStart({ onBrowse, onDiscover }: SearchStartProps) {
  return (
    <section className="search-start" aria-labelledby="search-start-title">
      <span>BEGIN WITH A CLUE</span>
      <h2 id="search-start-title">从一句关键词开始。</h2>
      <p>结果会保留作品类型与排序条件；重新搜索时会自动回到第一页。</p>
      <div className="search-start-actions">
        <button type="button" onClick={onBrowse}>浏览全部馆藏 <b aria-hidden="true">→</b></button>
        <button type="button" onClick={onDiscover}>让书库替我挑一本</button>
      </div>
      <ul aria-label="可搜索字段">
        <li><b>01</b><span>作品标题</span></li>
        <li><b>02</b><span>作者姓名</span></li>
        <li><b>03</b><span>汉化与来源</span></li>
      </ul>
    </section>
  );
}
