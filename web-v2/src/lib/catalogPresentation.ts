import type { CatalogMode, CatalogSort } from "./browseRoute";
import type { CatalogItem } from "../types";

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

export function itemTitle(item: CatalogItem): string {
  return String(item.display_title || item.series_title || item.title || "未命名作品");
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

export function pageMeta(item: CatalogItem): string {
  if (isSeries(item)) return String(item.item_summary || `${numericValue(item.item_count)} 个条目`);
  const count = numericValue(item.readable_page_count);
  return count ? `${count} 页` : String(item.display_library_name || "作品");
}

export function itemCreatorLabel(item: CatalogItem): string {
  return String(item.display_creator || "").trim();
}

export function itemKindLabel(item: CatalogItem): "SERIES" | "DOUJIN" | "MANGA" {
  if (isSeries(item)) return "SERIES";
  const candidateType = String(item.candidate_type || "").toLowerCase();
  return candidateType === "doujin" || String(item.display_library_name || "").includes("同人") ? "DOUJIN" : "MANGA";
}

export function itemKindDisplayLabel(item: CatalogItem): "漫画系列" | "同人本" | "漫画" {
  const kind = itemKindLabel(item);
  if (kind === "SERIES") return "漫画系列";
  return kind === "DOUJIN" ? "同人本" : "漫画";
}

export function itemContextLabel(item: CatalogItem, context: CatalogCardContext): string {
  if (context === "search") {
    const translation = String(item.translation_sources || "").trim();
    if (translation) return `汉化／翻译 · ${translation}`;
    const subtitle = String(item.display_subtitle || "").trim();
    if (subtitle && cleanTitle(subtitle) !== cleanTitle(itemTitle(item))) return `补充信息 · ${subtitle}`;
    const library = String(item.display_library_name || item.library_name || "").trim();
    return library ? `馆藏 · ${library}` : "";
  }
  if (context === "discover") {
    const series = String(item.series_title || "").trim();
    const chapter = String(item.item_label || "").trim();
    if (series && chapter) return `系列 · ${series}；章节 · ${chapter}`;
    if (series) return `系列 · ${series}`;
    return chapter ? `章节 · ${chapter}` : "";
  }
  return "";
}

export const catalogModeOptions = [
  {
    id: "all",
    label: "全部",
    english: "ALL WORKS",
    description: "同人本与漫画系列统一陈列",
  },
  {
    id: "doujin",
    label: "同人本",
    english: "DOUJIN ARCHIVE",
    description: "按册浏览独立作品与合本",
  },
  {
    id: "series",
    label: "漫画系列",
    english: "SERIES INDEX",
    description: "按系列进入章节目录",
  },
] as const satisfies ReadonlyArray<{
  id: CatalogMode;
  label: string;
  english: string;
  description: string;
}>;

export const catalogSortOptions = [
  { id: "added_desc", label: "最近入库" },
  { id: "title_asc", label: "标题 A–Z" },
  { id: "pages_desc", label: "页数最多" },
] as const satisfies ReadonlyArray<{ id: CatalogSort; label: string }>;

export function catalogModeOption(mode: CatalogMode) {
  return catalogModeOptions.find((option) => option.id === mode) || catalogModeOptions[0];
}
