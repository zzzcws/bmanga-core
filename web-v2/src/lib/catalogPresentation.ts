import type { CatalogMode, CatalogSort } from "./browseRoute";
import type { CatalogItem } from "../types";
import { DEFAULT_LOCALE, intlLocale, localizeMessage, type Locale } from "./locale.ts";

export type CatalogCardContext = "default" | "new" | "search" | "discover" | "related";

export interface CatalogProgress {
  index: number;
  count: number;
  percent: number;
  completed: boolean;
}

function numericValue(value: unknown, fallback = 0): number {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function booleanValue(value: unknown): boolean {
  if (typeof value === "boolean") return value;
  if (typeof value === "number") return value !== 0;
  if (typeof value === "string") return ["1", "true", "yes", "on"].includes(value.trim().toLowerCase());
  return false;
}

export function itemID(item: CatalogItem): string {
  return String(item.candidate_id || item.group_id || "");
}

export function itemCoverID(item: CatalogItem): string {
  return String(item.selected_candidate_id || item.candidate_id || "");
}

function formatInteger(value: number, locale: Locale): string {
  return new Intl.NumberFormat(intlLocale(locale), { maximumFractionDigits: 0 }).format(value);
}

export function itemTitle(item: CatalogItem, locale: Locale = DEFAULT_LOCALE): string {
  return String(item.display_title || item.series_title || item.title || localizeMessage({
    "zh-CN": "未命名作品",
    en: "Untitled work",
    ja: "無題の作品",
  }, locale));
}

export function isSeries(item: CatalogItem): boolean {
  return Boolean(item.group_id && (item.shelf_type === "series" || !item.candidate_id));
}

export function progressFor(item: CatalogItem): CatalogProgress | null {
  const progress = item.progress;
  const count = numericValue(progress?.count ?? item.progress_count);
  const index = numericValue(progress?.index ?? item.progress_index);
  const percent = numericValue(progress?.progress_percent ?? item.progress_percent, count ? ((index + 1) / count) * 100 : 0);
  const completed = booleanValue(progress?.completed ?? item.progress_completed);
  if (!count && !percent && !completed) return null;
  return { index, count, percent: completed ? 100 : Math.max(0, Math.min(100, percent)), completed };
}

export function cleanTitle(value: string): string {
  const raw = value.trim();
  if (!raw) return value;
  const outside = raw.replace(/\[[^\]]+\]/gu, " ").replace(/\s+/gu, " ").trim();
  const genericOutside = /^(?:完结|完結|完本|全集|全卷|完整版|连载中|連載中)$/u.test(outside);
  if (outside && !genericOutside) return outside;

  const bracketValues = [...raw.matchAll(/\[([^\]]+)\]/gu)]
    .map((match) => match[1]?.trim() || "")
    .filter((part) => part && !/^(?:完结|完結|完本|全集|全卷|完整版|连载中|連載中|JPG|JPEG|PNG|WEBP|PDF|EPUB|MOBI|DL版)$/iu.test(part))
    .filter((part) => !/^\d+(?:\.\d+)?\s*(?:KB|MB|GB)$/iu.test(part))
    .filter((part) => !/^\d+(?:\.\d+)?\s*[-~至]\s*\d+(?:\.\d+)?\s*(?:话|話|卷|巻|章|册|冊)?$/u.test(part));
  const bracketTitle = bracketValues.reduce((best, part) => part.length > best.length ? part : best, "");
  return bracketTitle || outside || raw;
}

export function pageMeta(item: CatalogItem, locale: Locale = DEFAULT_LOCALE): string {
  if (isSeries(item)) {
    const count = numericValue(item.item_count);
    if (item.item_summary) return String(item.item_summary);
    if (locale === "en") return `${formatInteger(count, locale)} ${count === 1 ? "item" : "items"}`;
    return localizeMessage({
      "zh-CN": "{count} 个条目",
      en: "{count} items",
      ja: "{count} 項目",
    }, locale, { count: formatInteger(count, locale) });
  }
  const count = numericValue(item.readable_page_count);
  if (count) {
    if (locale === "en") return `${formatInteger(count, locale)} ${count === 1 ? "page" : "pages"}`;
    return localizeMessage({
      "zh-CN": "{count} 页",
      en: "{count} pages",
      ja: "{count} ページ",
    }, locale, { count: formatInteger(count, locale) });
  }
  return String(item.display_library_name || localizeMessage({
    "zh-CN": "作品",
    en: "Work",
    ja: "作品",
  }, locale));
}

export function itemCreatorLabel(item: CatalogItem): string {
  return String(item.display_creator || "").trim();
}

export function itemKindLabel(item: CatalogItem): "SERIES" | "DOUJIN" | "MANGA" {
  if (isSeries(item)) return "SERIES";
  const candidateType = String(item.candidate_type || "").toLowerCase();
  return candidateType === "doujin" || String(item.display_library_name || "").includes("同人") ? "DOUJIN" : "MANGA";
}

export function itemKindDisplayLabel(item: CatalogItem, locale: Locale = DEFAULT_LOCALE): string {
  const kind = itemKindLabel(item);
  const messages = {
    SERIES: { "zh-CN": "漫画系列", en: "Manga series", ja: "漫画シリーズ" },
    DOUJIN: { "zh-CN": "同人本", en: "Doujin work", ja: "同人誌" },
    MANGA: { "zh-CN": "漫画", en: "Manga", ja: "漫画" },
  } as const;
  return localizeMessage(messages[kind], locale);
}

export function itemContextLabel(
  item: CatalogItem,
  context: CatalogCardContext,
  locale: Locale = DEFAULT_LOCALE,
): string {
  if (context === "search") {
    const translation = String(item.translation_sources || "").trim();
    if (translation) return localizeMessage({
      "zh-CN": "汉化／翻译 · {value}",
      en: "Translation · {value}",
      ja: "翻訳 · {value}",
    }, locale, { value: translation });
    const subtitle = String(item.display_subtitle || "").trim();
    if (subtitle && cleanTitle(subtitle) !== cleanTitle(itemTitle(item, locale))) return localizeMessage({
      "zh-CN": "补充信息 · {value}",
      en: "Additional information · {value}",
      ja: "補足情報 · {value}",
    }, locale, { value: subtitle });
    const library = String(item.display_library_name || item.library_name || "").trim();
    return library ? localizeMessage({
      "zh-CN": "馆藏 · {value}",
      en: "Library · {value}",
      ja: "ライブラリ · {value}",
    }, locale, { value: library }) : "";
  }
  if (context === "discover") {
    const series = String(item.series_title || "").trim();
    const chapter = String(item.item_label || "").trim();
    if (series && chapter) return localizeMessage({
      "zh-CN": "系列 · {series}；章节 · {chapter}",
      en: "Series · {series}; Chapter · {chapter}",
      ja: "シリーズ · {series} / チャプター · {chapter}",
    }, locale, { series, chapter });
    if (series) return localizeMessage({
      "zh-CN": "系列 · {value}",
      en: "Series · {value}",
      ja: "シリーズ · {value}",
    }, locale, { value: series });
    return chapter ? localizeMessage({
      "zh-CN": "章节 · {value}",
      en: "Chapter · {value}",
      ja: "チャプター · {value}",
    }, locale, { value: chapter }) : "";
  }
  return "";
}

export interface CatalogModeOption {
  id: CatalogMode;
  label: string;
  english: string;
  description: string;
}

export function catalogModeOptionsFor(locale: Locale = DEFAULT_LOCALE): readonly CatalogModeOption[] {
  return [
  {
    id: "all",
    label: localizeMessage({ "zh-CN": "全部", en: "All", ja: "すべて" }, locale),
    english: "ALL WORKS",
    description: localizeMessage({
      "zh-CN": "同人本与漫画系列统一陈列",
      en: "Browse doujin works and manga series together",
      ja: "同人誌と漫画シリーズをまとめて表示",
    }, locale),
  },
  {
    id: "doujin",
    label: localizeMessage({ "zh-CN": "同人本", en: "Doujin works", ja: "同人誌" }, locale),
    english: "DOUJIN ARCHIVE",
    description: localizeMessage({
      "zh-CN": "按册浏览独立作品与合本",
      en: "Browse standalone works and collections by volume",
      ja: "単独作品と合本を1冊ずつ閲覧",
    }, locale),
  },
  {
    id: "series",
    label: localizeMessage({ "zh-CN": "漫画系列", en: "Manga series", ja: "漫画シリーズ" }, locale),
    english: "SERIES INDEX",
    description: localizeMessage({
      "zh-CN": "按系列进入章节目录",
      en: "Open a chapter directory for each series",
      ja: "シリーズごとに章一覧を表示",
    }, locale),
  },
  ];
}

export interface CatalogSortOption {
  id: CatalogSort;
  label: string;
}

export function catalogSortOptionsFor(locale: Locale = DEFAULT_LOCALE): readonly CatalogSortOption[] {
  return [
    { id: "added_desc", label: localizeMessage({ "zh-CN": "最近入库", en: "Recently added", ja: "最近追加" }, locale) },
    { id: "title_asc", label: localizeMessage({ "zh-CN": "标题 A–Z", en: "Title A–Z", ja: "タイトル A–Z" }, locale) },
    { id: "pages_desc", label: localizeMessage({ "zh-CN": "页数最多", en: "Most pages", ja: "ページ数が多い順" }, locale) },
  ];
}

export const catalogModeOptions = catalogModeOptionsFor();
export const catalogSortOptions = catalogSortOptionsFor();

export function catalogModeOption(mode: CatalogMode, locale: Locale = DEFAULT_LOCALE): CatalogModeOption {
  const options = catalogModeOptionsFor(locale);
  return options.find((option) => option.id === mode) || options[0]!;
}
