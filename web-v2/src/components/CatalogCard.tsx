import { memo } from "react";

import { useI18n, type LocalizedText } from "../i18n";
import { coverUrl } from "../lib/api";
import {
  cleanTitle,
  isSeries,
  itemCoverID,
  itemCreatorLabel,
  itemKindLabel,
  itemTitle,
  progressFor,
  type CatalogCardContext,
} from "../lib/catalogPresentation";
import type { CatalogItem } from "../types";

const copy = {
  cover: {
    "zh-CN": "{title}封面",
    en: "Cover of {title}",
    ja: "{title}の表紙",
  },
  read: {
    "zh-CN": "已读",
    en: "Read",
    ja: "読了",
  },
  readToPage: {
    "zh-CN": "读到第 {page} 页",
    en: "Read through page {page}",
    ja: "{page} ページまで読了",
  },
  unread: {
    "zh-CN": "未读",
    en: "Unread",
    ja: "未読",
  },
  new: {
    "zh-CN": "NEW",
    en: "NEW",
    ja: "新着",
  },
  pages: {
    "zh-CN": "{count} 页",
    en: "{count} pages",
    ja: "{count} ページ",
  },
  entries: {
    "zh-CN": "{count} 个条目",
    en: "{count} entries",
    ja: "{count} 件",
  },
  work: {
    "zh-CN": "作品",
    en: "Work",
    ja: "作品",
  },
  seriesKind: {
    "zh-CN": "漫画系列",
    en: "Series",
    ja: "漫画シリーズ",
  },
  doujinKind: {
    "zh-CN": "同人本",
    en: "Doujin work",
    ja: "同人誌",
  },
  mangaKind: {
    "zh-CN": "漫画",
    en: "Comic",
    ja: "漫画",
  },
  creator: {
    "zh-CN": "作者／社团",
    en: "Creator / circle",
    ja: "作者／サークル",
  },
  creatorTitle: {
    "zh-CN": "作者／社团：{creator}",
    en: "Creator / circle: {creator}",
    ja: "作者／サークル：{creator}",
  },
  type: {
    "zh-CN": "类型 · {kind}",
    en: "Type · {kind}",
    ja: "種類 · {kind}",
  },
  translation: {
    "zh-CN": "汉化／翻译 · {value}",
    en: "Translation · {value}",
    ja: "翻訳 · {value}",
  },
  supplemental: {
    "zh-CN": "补充信息 · {value}",
    en: "Additional information · {value}",
    ja: "補足情報 · {value}",
  },
  library: {
    "zh-CN": "馆藏 · {value}",
    en: "Library · {value}",
    ja: "ライブラリ · {value}",
  },
  series: {
    "zh-CN": "系列 · {value}",
    en: "Series · {value}",
    ja: "シリーズ · {value}",
  },
  chapter: {
    "zh-CN": "章节 · {value}",
    en: "Chapter · {value}",
    ja: "話数 · {value}",
  },
  seriesChapter: {
    "zh-CN": "系列 · {series}；章节 · {chapter}",
    en: "Series · {series}; chapter · {chapter}",
    ja: "シリーズ · {series} / 話数 · {chapter}",
  },
} satisfies Record<string, LocalizedText>;

export interface CoverProps {
  item: CatalogItem;
  size?: number;
  eager?: boolean;
  decorative?: boolean;
}

export function Cover({ item, size = 640, eager = false, decorative = false }: CoverProps) {
  const { locale, text } = useI18n();
  const id = itemCoverID(item);
  const progress = progressFor(item);
  const responsiveCardCover = size === 640;
  return (
    <div className="book-cover">
      {id ? (
        <img
          key={`${id}:${size}`}
          src={coverUrl(id, responsiveCardCover ? 420 : size)}
          srcSet={responsiveCardCover ? `${coverUrl(id, 420)} 1x, ${coverUrl(id, 640)} 2x` : undefined}
          width={3}
          height={4}
          alt={decorative ? "" : text(copy.cover, { title: itemTitle(item, locale) })}
          loading={eager ? "eager" : "lazy"}
          decoding="async"
          fetchPriority={eager ? "high" : "auto"}
          onLoad={(event) => {
            const image = event.currentTarget;
            const reveal = () => {
              image.classList.remove("image-failed");
              image.classList.add("image-ready");
            };
            try {
              void image.decode().then(reveal, reveal);
            } catch {
              reveal();
            }
          }}
          onError={(event) => {
            event.currentTarget.classList.remove("image-ready");
            event.currentTarget.classList.add("image-failed");
          }}
        />
      ) : null}
      <span className="cover-placeholder" aria-hidden="true">BM</span>
      {progress ? <span className="cover-progress" style={{ width: `${progress.percent}%` }} /> : null}
    </div>
  );
}

export type BookCardContext = CatalogCardContext;

export interface BookCardProps {
  item: CatalogItem;
  index: number;
  onOpen: (item: CatalogItem) => void;
  context?: BookCardContext;
  priority?: boolean;
}

export const BookCard = memo(function BookCard({ item, index, onOpen, context = "default", priority = false }: BookCardProps) {
  const { locale, number, text } = useI18n();
  const progress = progressFor(item);
  const candidateType = String(item.candidate_type || "").toLowerCase();
  const kind = itemKindLabel(item);
  const kindLabel = kind === "SERIES" ? text(copy.seriesKind) : kind === "DOUJIN" ? text(copy.doujinKind) : text(copy.mangaKind);
  const creatorLabel = itemCreatorLabel(item);
  const itemPageMeta = (() => {
    if (isSeries(item)) {
      if (item.item_summary) return String(item.item_summary);
      return text(copy.entries, { count: number(Number(item.item_count) || 0) });
    }
    const count = Number(item.readable_page_count) || 0;
    return count
      ? text(copy.pages, { count: number(count) })
      : String(item.display_library_name || text(copy.work));
  })();
  const supplementalContext = (() => {
    if (context === "search") {
      const translation = String(item.translation_sources || "").trim();
      if (translation) return text(copy.translation, { value: translation });
      const subtitle = String(item.display_subtitle || "").trim();
      if (subtitle && cleanTitle(subtitle) !== cleanTitle(itemTitle(item, locale))) return text(copy.supplemental, { value: subtitle });
      const library = String(item.display_library_name || item.library_name || "").trim();
      return library ? text(copy.library, { value: library }) : "";
    }
    if (context === "discover") {
      const series = String(item.series_title || "").trim();
      const chapter = String(item.item_label || "").trim();
      if (series && chapter) return text(copy.seriesChapter, { series, chapter });
      if (series) return text(copy.series, { value: series });
      return chapter ? text(copy.chapter, { value: chapter }) : "";
    }
    return "";
  })();
  const readingState = progress
    ? progress.completed
      ? `${text(copy.read)} · 100%`
      : `${text(copy.readToPage, { page: number(progress.index + 1) })} · ${number(Math.round(progress.percent))}%`
    : `${itemPageMeta} · ${context === "new" ? text(copy.new) : text(copy.unread)}`;
  return (
    <button className="book-card" type="button" data-kind={isSeries(item) ? "series" : candidateType || "work"} onClick={() => onOpen(item)}>
      <Cover item={item} eager={priority} decorative />
      <span className="book-info">
        <span className="book-title-row"><strong className="book-title">{cleanTitle(itemTitle(item, locale))}</strong><span className="book-index" aria-hidden="true">{number(index + 1, { minimumIntegerDigits: 2, useGrouping: false })}</span></span>
        {creatorLabel ? <small className="book-attribution" title={text(copy.creatorTitle, { creator: creatorLabel })}><span>{text(copy.creator)}</span><strong>{creatorLabel}</strong></small> : null}
        <small className="book-meta"><span className="book-kind">{text(copy.type, { kind: kindLabel })}</span><span className="book-reading-state">{readingState}</span></small>
        {supplementalContext ? <small className="book-match">{supplementalContext}</small> : null}
      </span>
    </button>
  );
});
