
import { ApiError, apiErrorText } from "./lib/api";
import { type ActiveReaderFitMode } from "./components/ReaderChrome";
import { seriesReadingOrder } from "./lib/seriesOrder";
import { preferredScrollBehavior } from "./lib/motion";
import { type CatalogPage, type FavoritesPage } from "./lib/catalogPageCache";
import {
  BROWSE_SCOPE_STORAGE_NAME,
  LEGACY_LIBRARY_PAGE_SCOPES_MIGRATION_KEY,
  LEGACY_LIBRARY_PAGE_SCOPES_STORAGE_NAME,
  LIBRARY_PAGE_SCOPES_STORAGE_NAME,
  libraryPageScopeOffset,
  mergeLegacyLibraryPageScopes,
  migrateLegacyLibraryPageScopes,
  parseBrowseScopes,
  parseLibraryPageScopes,
  parseBrowseURL,
  sanitizeBrowseRoute,
  serializeBrowseScopes,
  serializeLibraryPageScopes,
  type BrowseRouteState,
  type BrowseScopeState,
  type CatalogMode,
  type CatalogSort,
  type DiscoverMode,
  type LibraryPageScopes,
  type View,
} from "./lib/browseRoute";
import { type PendingProgressEntry } from "./lib/progressQueue";
import { hasPendingUserMark } from "./lib/userMarkQueue";
import { type ReaderWarmProgress } from "./lib/readerPreparationCache";
import {
  readerImageCacheBucket,
  readerImageMaxForViewport,
  readerUsesSourceQuality,
} from "./lib/readerImage";
import { type DetailState } from "./lib/detailProgress";
import type { PersonalMarkField } from "./lib/userMarks";
import { cleanTitle, pageMeta, progressFor } from "./lib/catalogPresentation";
import { selectSeriesContinueItem } from "./lib/seriesResume";
import {
  DEFAULT_LOCALE,
  intlLocale,
  localizeMessage,
  type Locale,
} from "./lib/locale";
import type {
  CatalogItem,
  PagesResponse,
  ReadingProgress,
  SeriesDetailResponse,
  ShelfItem,
  TargetType,
  UserMark,
  UserMarkSavePayload,
  WorkDetailResponse,
  WorkSummary,
} from "./types";

export interface ReaderState {
  item: WorkSummary;
  pages: PagesResponse;
  index: number;
  requestedIndex: number;
  savedIndex: number;
  imageURL: string;
  pageRevision: number;
  imageLoading: boolean;
  error: string;
  fitMode: ActiveReaderFitMode;
  splitPanel: 0 | 1;
  requestedSplitPanel: 0 | 1;
  imageNaturalWidth: number;
  imageNaturalHeight: number;
  stageScrollTop: number;
  stageScrollLeft: number;
  restoreScroll: boolean;
  ending: boolean;
  chromeVisible: boolean;
  seriesID?: string;
  nextItem?: WorkSummary;
  stalePending?: Pick<PendingProgressEntry, "entryID">;
  calibration: { status: string; oldIndex: number; oldCount: number } | null;
  calibrationSaving?: boolean;
}

export interface PersistReaderOptions {
  force?: boolean;
  silent?: boolean;
}

export interface ReaderContext {
  seriesID?: string;
  nextItem?: WorkSummary;
}

export interface ReaderIntent extends ReaderContext {
  item: WorkSummary;
  requestedIndex?: number;
}

export interface ToastState {
  kind?: "success" | "error";
  message: string;
  actionLabel?: string;
  onAction?: () => void;
}

export interface UiSnapshot {
  view: View;
  detail: DetailState | null;
  detailLoading: boolean;
  detailIntent: CatalogItem | null;
  reader: ReaderState | null;
  readerLoading: boolean;
  readerIntent: ReaderIntent | null;
  noteDraft: string;
  offset: number;
  favoritesOffset: number;
  searchQuery: string;
  searchDraft: string;
  catalogMode: CatalogMode;
  sort: CatalogSort;
  discoverMode: DiscoverMode;
  scrollY: number;
  detailScrollTop: number;
}

export interface UiHistoryEntry {
  parent: number | null;
  position: number;
  routeKey: string;
  snapshot: UiSnapshot;
}

export interface UiHistoryMarker {
  bmangaV2: 1;
  session: string;
  entry: number;
  position: number;
  guard?: 1;
}

export interface CatalogPrefetchEntry {
  scopeKey: string;
  controller: AbortController;
  promise: Promise<CatalogPage<CatalogItem> | null>;
}

export interface CatalogActiveRequest {
  session: number;
  controller: AbortController;
}

export interface FavoritesPrefetchEntry {
  scopeKey: string;
  controller: AbortController;
  promise: Promise<FavoritesPage<ShelfItem> | null>;
}

export interface FavoritesActiveRequest {
  session: number;
  controller: AbortController;
}

export const READER_FIT_KEY = "bmanga.readerFit.v3";
export const CATALOG_PREFETCH_LIMIT = 4;
export const HOME_RECENT_LIMIT = 6;
export const MY_HISTORY_LIMIT = 6;
export const EMPTY_CATALOG_ITEMS: CatalogItem[] = [];
export const EMPTY_SHELF_ITEMS: ShelfItem[] = [];

export function browseRouteFromSnapshot(snapshot: UiSnapshot): BrowseRouteState {
  return sanitizeBrowseRoute({
    view: snapshot.view,
    catalogMode: snapshot.catalogMode,
    sort: snapshot.sort,
    offset: snapshot.view === "my" ? snapshot.favoritesOffset : snapshot.offset,
    searchQuery: snapshot.searchQuery,
    discoverMode: snapshot.discoverMode,
  });
}

export function browseSnapshotOverrides(route: BrowseRouteState, searchDraft = route.searchQuery): Partial<UiSnapshot> {
  return {
    view: route.view,
    offset: route.view === "library" || route.view === "search" ? route.offset : 0,
    favoritesOffset: route.view === "my" ? route.offset : 0,
    searchQuery: route.searchQuery,
    searchDraft,
    catalogMode: route.catalogMode,
    sort: route.sort,
    discoverMode: route.discoverMode,
  };
}

export function storedBrowseScopes(): BrowseScopeState {
  if (typeof window === "undefined") return {};
  try {
    return parseBrowseScopes(window.sessionStorage.getItem(BROWSE_SCOPE_STORAGE_NAME));
  } catch {
    return {};
  }
}

export function persistBrowseScopes(scopes: BrowseScopeState): void {
  try {
    window.sessionStorage.setItem(BROWSE_SCOPE_STORAGE_NAME, serializeBrowseScopes(scopes));
  } catch {
    // Session storage is an enhancement; private browsing must remain usable without it.
  }
}

export function storedLibraryPageScopes(sort: CatalogSort, forceSort = false): LibraryPageScopes {
  if (typeof window === "undefined") return { sort, offsets: {} };
  let raw: string | null = null;
  try {
    raw = window.localStorage.getItem(LIBRARY_PAGE_SCOPES_STORAGE_NAME);
  } catch {
    // Fall through to the former session-scoped V2 value.
  }
  if (!raw) {
    try {
      raw = window.sessionStorage.getItem(LIBRARY_PAGE_SCOPES_STORAGE_NAME);
    } catch {
      // Storage is optional; an empty scope still keeps browsing usable.
    }
  }
  let scopes = parseLibraryPageScopes(raw, sort);
  if (forceSort && scopes.sort !== sort) scopes = { sort, offsets: {} };
  try {
    const migrationKey = `${LEGACY_LIBRARY_PAGE_SCOPES_MIGRATION_KEY}.${scopes.sort}`;
    if (window.localStorage.getItem(migrationKey) !== "1") {
      const legacy = migrateLegacyLibraryPageScopes(
        window.localStorage.getItem(LEGACY_LIBRARY_PAGE_SCOPES_STORAGE_NAME),
        scopes.sort,
      );
      if (Object.keys(legacy.offsets).length) {
        scopes = mergeLegacyLibraryPageScopes(scopes, legacy);
        window.localStorage.setItem(LIBRARY_PAGE_SCOPES_STORAGE_NAME, serializeLibraryPageScopes(scopes));
        window.localStorage.setItem(migrationKey, "1");
      }
    }
  } catch {
    // Legacy migration is best-effort and never blocks the library.
  }
  return scopes;
}

export function persistLibraryPageScopes(scopes: LibraryPageScopes, mode: CatalogMode): LibraryPageScopes {
  let next = scopes;
  try {
    const latest = parseLibraryPageScopes(window.localStorage.getItem(LIBRARY_PAGE_SCOPES_STORAGE_NAME), scopes.sort);
    if (latest.sort === scopes.sort) {
      next = {
        sort: scopes.sort,
        offsets: { ...latest.offsets, [mode]: scopes.offsets[mode] || 0 },
      };
    }
    window.localStorage.setItem(LIBRARY_PAGE_SCOPES_STORAGE_NAME, serializeLibraryPageScopes(next));
    window.sessionStorage.removeItem(LIBRARY_PAGE_SCOPES_STORAGE_NAME);
  } catch {
    try {
      window.sessionStorage.setItem(LIBRARY_PAGE_SCOPES_STORAGE_NAME, serializeLibraryPageScopes(next));
    } catch {
      // Category page memory is optional when browser storage is unavailable.
    }
  }
  return next;
}

export function replaceLibraryPageScopes(scopes: LibraryPageScopes): LibraryPageScopes {
  const next = parseLibraryPageScopes(serializeLibraryPageScopes(scopes), scopes.sort);
  try {
    window.localStorage.setItem(LIBRARY_PAGE_SCOPES_STORAGE_NAME, serializeLibraryPageScopes(next));
    window.sessionStorage.removeItem(LIBRARY_PAGE_SCOPES_STORAGE_NAME);
  } catch {
    try {
      window.sessionStorage.setItem(LIBRARY_PAGE_SCOPES_STORAGE_NAME, serializeLibraryPageScopes(next));
    } catch {
      // Shared page state remains usable in memory when browser storage is unavailable.
    }
  }
  return next;
}

export function markLegacyLibrarySortHandled(sort: CatalogSort): void {
  try {
    window.localStorage.setItem(`${LEGACY_LIBRARY_PAGE_SCOPES_MIGRATION_KEY}.${sort}`, "1");
  } catch {
    // An explicit sort change still works when storage is unavailable.
  }
}

export function initialBrowseRoute(href: string): BrowseRouteState {
  const route = parseBrowseURL(href);
  if (typeof window === "undefined" || route.view !== "library") return route;
  try {
    const url = new URL(href, window.location.origin);
    if (url.searchParams.has("offset")) return route;
  } catch {
    return route;
  }
  const rememberedOffset = libraryPageScopeOffset(storedLibraryPageScopes(route.sort, true), route.catalogMode, route.sort);
  return rememberedOffset > 0 ? sanitizeBrowseRoute({ ...route, offset: rememberedOffset }) : route;
}

export function storedReaderFit(progress?: ReadingProgress | null): ActiveReaderFitMode {
  const fromProgress = String(progress?.reader_fit_mode || "");
  if (fromProgress === "fit-page" || fromProgress === "fit-width" || fromProgress === "split-wide") return fromProgress;
  try {
    const stored = window.localStorage.getItem(READER_FIT_KEY);
    if (stored === "fit-width" || stored === "fit-page" || stored === "split-wide") return stored;
  } catch {
    // Storage can be unavailable without blocking reading.
  }
  return "fit-page";
}

export type DetailReaderWarmTarget = {
  candidateID: string;
  fitMode: ActiveReaderFitMode;
  preserveSource: boolean;
  progress: ReaderWarmProgress | null;
  requestedIndex?: number;
};

export function detailReaderWarmTarget(detail: DetailState | null): DetailReaderWarmTarget | null {
  if (!detail) return null;
  const item = detail.kind === "work"
    ? detail.data.work
    : seriesContinueItem(detail.data, detail.progress);
  if (!item?.candidate_id || !item.can_read) return null;
  const catalogProgress = progressFor(item);
  const seriesProgress = detail.kind === "series" && detail.progress?.candidate_id === item.candidate_id
    ? detail.progress
    : null;
  const fullProgress = seriesProgress || item.progress || null;
  const progress = fullProgress || (catalogProgress ? {
    index: catalogProgress.index,
    page_manifest_id: item.progress_page_manifest_id,
    manifest_hash: item.progress_manifest_hash,
  } : null);
  const preferredFit = String(fullProgress?.reader_fit_mode || item.progress_reader_fit_mode || "");
  const fitMode = preferredFit === "fit-page" || preferredFit === "fit-width" || preferredFit === "split-wide"
    ? preferredFit
    : storedReaderFit(fullProgress);
  return {
    candidateID: item.candidate_id,
    fitMode,
    preserveSource: readerUsesSourceQuality(item.candidate_type),
    progress,
    requestedIndex: catalogProgress?.completed ? 0 : undefined,
  };
}

export function rememberReaderFit(mode: ActiveReaderFitMode): void {
  try {
    window.localStorage.setItem(READER_FIT_KEY, mode);
  } catch {
    // Reading still works when the preference cannot be persisted.
  }
}

export function sameManifest(
  left: { page_manifest_id?: unknown; manifest_hash?: unknown },
  right: { page_manifest_id?: unknown; manifest_hash?: unknown },
): boolean {
  const leftID = String(left.page_manifest_id || "");
  const rightID = String(right.page_manifest_id || "");
  const leftHash = String(left.manifest_hash || "");
  const rightHash = String(right.manifest_hash || "");
  let compared = false;
  if (leftID && rightID) {
    compared = true;
    if (leftID !== rightID) return false;
  }
  if (leftHash && rightHash) {
    compared = true;
    if (leftHash !== rightHash) return false;
  }
  return compared;
}

export function progressTimestamp(value: unknown): number {
  const parsed = Date.parse(String(value || ""));
  return Number.isFinite(parsed) ? parsed : 0;
}

export function decodeReaderImage(url: string, signal: AbortSignal): Promise<{ width: number; height: number }> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    let settled = false;
    const finish = (reason?: unknown) => {
      if (settled) return;
      settled = true;
      signal.removeEventListener("abort", abort);
      image.onload = null;
      image.onerror = null;
      if (reason) reject(reason);
      else if (image.naturalWidth > 0 && image.naturalHeight > 0) resolve({ width: image.naturalWidth, height: image.naturalHeight });
      else reject(new Error("图片无法解码"));
    };
    const abort = () => {
      image.src = "";
      finish(new DOMException("页面加载已取消", "AbortError"));
    };
    image.onload = () => {
      try {
        void image.decode().then(() => finish(), () => finish());
      } catch {
        finish();
      }
    };
    image.onerror = () => finish(new Error("图片无法解码"));
    signal.addEventListener("abort", abort, { once: true });
    if (signal.aborted) {
      abort();
      return;
    }
    image.decoding = "async";
    image.src = url;
  });
}

export type ReaderPageAsset = {
  height: number;
  objectURL: string;
  width: number;
};

export type ReaderPagePrefetchPlan = {
  cacheKey: string;
  candidateID: string;
  count: number;
  imageMax: number;
  imageURL: string;
  index: number;
  pageManifestID?: string;
  preserveSource: boolean;
};

export class ReaderPageResponseError extends Error {
  constructor(readonly status: number) {
    super(`Reader page request failed with HTTP ${status}`);
    this.name = "ReaderPageResponseError";
  }
}

export async function loadReaderPageAsset(url: string, signal: AbortSignal): Promise<ReaderPageAsset> {
  const response = await fetch(url, {
    signal,
    credentials: "same-origin",
    headers: { Accept: "image/avif,image/webp,image/png,image/jpeg,image/*,*/*;q=0.8" },
  });
  if (!response.ok) throw new ReaderPageResponseError(response.status);
  const blob = await response.blob();
  if (!blob.size) throw new Error("这一页没有返回有效图片。");
  const objectURL = URL.createObjectURL(blob);
  try {
    const naturalSize = await decodeReaderImage(objectURL, signal);
    return { objectURL, width: naturalSize.width, height: naturalSize.height };
  } catch (reason) {
    URL.revokeObjectURL(objectURL);
    throw reason;
  }
}

export function shouldPrefetchReaderPages(): boolean {
  if (typeof navigator === "undefined") return true;
  const connection = (navigator as Navigator & { connection?: { effectiveType?: string; saveData?: boolean } }).connection;
  if (connection?.saveData) return false;
  return !["slow-2g", "2g"].includes(String(connection?.effectiveType || "").toLowerCase());
}

export function readerImageMax(fitMode: ActiveReaderFitMode, sourceQuality = false): number {
  if (typeof window === "undefined") return 1800;
  if (!sourceQuality) {
    const dpr = Math.min(2, Math.max(1, window.devicePixelRatio || 1));
    const stageWidth = Math.max(320, window.innerWidth);
    const stageHeight = Math.max(480, window.innerHeight);
    const target = fitMode === "fit-width"
      ? stageWidth * dpr
      : fitMode === "split-wide"
        ? Math.max(stageWidth * 2, stageHeight * 1.2) * dpr
        : Math.min(stageWidth, stageHeight * 0.92) * dpr;
    return readerImageCacheBucket(target, 2400);
  }
  return readerImageMaxForViewport(
    fitMode,
    window.innerWidth,
    window.innerHeight,
    window.devicePixelRatio || 1,
  );
}

export const discoverModes: Array<{ id: DiscoverMode; label: string; title: string; copy: string }> = [
  { id: "unread", label: "未读", title: "从没翻开过的书里，挑一册今晚认识。", copy: "只从未读作品中抽取，让书架里安静太久的封面重新被看见。" },
  { id: "reading", label: "在读", title: "把一段已经开始的阅读，接着读下去。", copy: "从尚未读完的作品里换一组，适合找回最近搁下的章节。" },
  { id: "liked", label: "喜欢", title: "从你喜欢过的作品里，找一本久别重逢。", copy: "依据收藏与评分留下的偏好，随机抽取值得再翻一次的作品。" },
  { id: "reread", label: "重读", title: "今晚不追新书，只重温一本旧爱。", copy: "从标记过重读优先级的作品中抽取，保留有意选择的偶然。" },
  { id: "any", label: "随缘", title: "不设条件，让整座书库替你做决定。", copy: "从所有可读作品中随机抽取，适合完全不知道想看什么的夜晚。" },
];

export function discoverModesForLocale(locale: Locale = DEFAULT_LOCALE): Array<{ id: DiscoverMode; label: string; title: string; copy: string }> {
  const messages: Record<DiscoverMode, { label: Record<Locale, string>; title: Record<Locale, string>; copy: Record<Locale, string> }> = {
    unread: {
      label: { "zh-CN": "未读", en: "Unread", ja: "未読" },
      title: { "zh-CN": "从没翻开过的书里，挑一册今晚认识。", en: "Meet a book you have never opened.", ja: "まだ開いたことのない一冊と、今夜出会いましょう。" },
      copy: { "zh-CN": "只从未读作品中抽取，让书架里安静太久的封面重新被看见。", en: "Pick only from unread works and bring a quiet cover back into view.", ja: "未読作品だけから選び、長く眠っていた表紙をもう一度見つけます。" },
    },
    reading: {
      label: { "zh-CN": "在读", en: "In progress", ja: "読書中" },
      title: { "zh-CN": "把一段已经开始的阅读，接着读下去。", en: "Continue a story you already started.", ja: "読みかけの一冊を、続きを開いてみましょう。" },
      copy: { "zh-CN": "从尚未读完的作品里换一组，适合找回最近搁下的章节。", en: "Choose from unfinished works and return to a recently paused chapter.", ja: "未読了の作品から選び、最近止めた章へ戻ります。" },
    },
    liked: {
      label: { "zh-CN": "喜欢", en: "Liked", ja: "お気に入り" },
      title: { "zh-CN": "从你喜欢过的作品里，找一本久别重逢。", en: "Rediscover something you once liked.", ja: "以前好きだった作品と、もう一度出会いましょう。" },
      copy: { "zh-CN": "依据收藏与评分留下的偏好，随机抽取值得再翻一次的作品。", en: "Use your saved preferences to find something worth opening again.", ja: "保存した好みをもとに、もう一度読みたい作品を選びます。" },
    },
    reread: {
      label: { "zh-CN": "重读", en: "Reread", ja: "再読" },
      title: { "zh-CN": "今晚不追新书，只重温一本旧爱。", en: "Skip the new arrivals and revisit an old favorite.", ja: "今夜は新刊ではなく、懐かしい一冊を読み返しましょう。" },
      copy: { "zh-CN": "从标记过重读优先级的作品中抽取，保留有意选择的偶然。", en: "Pick from works you marked for rereading, with a little room for chance.", ja: "再読候補にした作品から、偶然を少し残して選びます。" },
    },
    any: {
      label: { "zh-CN": "随缘", en: "Surprise me", ja: "おまかせ" },
      title: { "zh-CN": "不设条件，让整座书库替你做决定。", en: "Let the whole library decide for you.", ja: "条件を決めず、ライブラリ全体に選んでもらいましょう。" },
      copy: { "zh-CN": "从所有可读作品中随机抽取，适合完全不知道想看什么的夜晚。", en: "Pick from every readable work when you have no idea what to read.", ja: "何を読みたいか決まらない夜に、読める全作品から選びます。" },
    },
  };
  return (["unread", "reading", "liked", "reread", "any"] as const).map((id) => ({
    id,
    label: messages[id].label[locale],
    title: messages[id].title[locale],
    copy: messages[id].copy[locale],
  }));
}

export const navItems: Array<{ id: View; index: string; label: string }> = [
  { id: "home", index: "01", label: "首页" },
  { id: "library", index: "02", label: "书库" },
  { id: "discover", index: "03", label: "发现" },
  { id: "search", index: "04", label: "搜索" },
  { id: "my", index: "05", label: "我的" },
];

export function navItemsForLocale(locale: Locale = DEFAULT_LOCALE): Array<{ id: View; index: string; label: string }> {
  const labels: Record<Exclude<View, "settings">, Record<Locale, string>> = {
    home: { "zh-CN": "首页", en: "Home", ja: "ホーム" },
    library: { "zh-CN": "书库", en: "Library", ja: "ライブラリ" },
    discover: { "zh-CN": "发现", en: "Discover", ja: "見つける" },
    search: { "zh-CN": "搜索", en: "Search", ja: "検索" },
    my: { "zh-CN": "我的", en: "My shelf", ja: "マイページ" },
  };
  return (["home", "library", "discover", "search", "my"] as const).map((id, offset) => ({
    id,
    index: String(offset + 1).padStart(2, "0"),
    label: labels[id][locale],
  }));
}

export const viewLabels: Record<View, string> = {
  home: "首页",
  library: "书库",
  discover: "发现",
  search: "搜索",
  my: "我的阅读",
  settings: "设置",
};

export function viewLabelsForLocale(locale: Locale = DEFAULT_LOCALE): Record<View, string> {
  return {
    home: localizeMessage({ "zh-CN": "首页", en: "Home", ja: "ホーム" }, locale),
    library: localizeMessage({ "zh-CN": "书库", en: "Library", ja: "ライブラリ" }, locale),
    discover: localizeMessage({ "zh-CN": "发现", en: "Discover", ja: "見つける" }, locale),
    search: localizeMessage({ "zh-CN": "搜索", en: "Search", ja: "検索" }, locale),
    my: localizeMessage({ "zh-CN": "我的阅读", en: "My reading", ja: "マイリーディング" }, locale),
    settings: localizeMessage({ "zh-CN": "设置", en: "Settings", ja: "設定" }, locale),
  };
}

export function numberValue(value: unknown, fallback = 0): number {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
}

export function booleanValue(value: unknown): boolean {
  if (typeof value === "boolean") return value;
  if (typeof value === "number") return value !== 0;
  if (typeof value === "string") return ["1", "true", "yes", "on"].includes(value.trim().toLowerCase());
  return false;
}

export function recordValue(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

export function compactNumber(value: unknown, locale: Locale = DEFAULT_LOCALE): string {
  return new Intl.NumberFormat(intlLocale(locale), { notation: "compact", maximumFractionDigits: 1 }).format(numberValue(value));
}

export function greeting(locale: Locale = DEFAULT_LOCALE): string {
  const hour = new Date().getHours();
  if (hour < 6) return localizeMessage({ "zh-CN": "夜深了，继续你的阅读", en: "A late night for another chapter", ja: "夜更けに、続きを読む" }, locale);
  if (hour < 12) return localizeMessage({ "zh-CN": "早上好，开始今天的阅读", en: "Good morning. Start today's reading", ja: "おはようございます。今日の読書を始めましょう" }, locale);
  if (hour < 18) return localizeMessage({ "zh-CN": "下午好，留一点时间阅读", en: "Good afternoon. Make time to read", ja: "こんにちは。読書の時間を少しだけ" }, locale);
  return localizeMessage({ "zh-CN": "晚上好，继续你的阅读", en: "Good evening. Continue your reading", ja: "こんばんは。続きを読みましょう" }, locale);
}

export function formatDay(locale: Locale = DEFAULT_LOCALE): string {
  return new Intl.DateTimeFormat(intlLocale(locale), { weekday: "long", month: "long", day: "numeric" }).format(new Date());
}

export function localDateKey(date = new Date()): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

export function heroPosition(item: CatalogItem, detail: WorkDetailResponse | null, locale: Locale = DEFAULT_LOCALE): string {
  const labels = [
    item.item_label,
    detail?.doujin_series?.find((entry) => entry.sequence_label)?.sequence_label,
  ];
  const label = labels.map((value) => String(value || "").trim()).find(Boolean);
  if (label) return label;
  const progress = progressFor(item);
  return progress
    ? localizeMessage({ "zh-CN": "第 {page} 页", en: "Page {page}", ja: "{page}ページ" }, locale, { page: progress.index + 1 })
    : pageMeta(item, locale);
}

export function heroCreator(detail: WorkDetailResponse | null): string {
  const values = [
    ...(detail?.creators?.map((creator) => creator.creator_display) || []),
    ...(detail?.title_hints?.creators || []),
    ...(detail?.doujin_series?.map((entry) => entry.creator_display) || []),
  ];
  return values.map((value) => String(value || "").trim()).find(Boolean) || "";
}

export function favoriteFor(item: CatalogItem, mark?: UserMark | null): boolean {
  return mark ? Boolean(mark.favorite) : booleanValue(item.user_favorite);
}

export function seriesContinueItem(data: SeriesDetailResponse, seriesProgress: ReadingProgress | null = null): WorkSummary | undefined {
  const readable = seriesReadingOrder(data).filter((item) => item.can_read);
  if (!readable.length) return undefined;
  const resumed = selectSeriesContinueItem(readable, seriesProgress);
  if (resumed) return resumed;
  return readable.find((item) => item.candidate_id === data.series.selected_candidate_id) || readable[0];
}

export function seriesAggregateProgress(data: SeriesDetailResponse): { percent: number; readPages: number; totalPages: number } {
  let readPages = 0;
  let totalPages = 0;
  for (const item of seriesReadingOrder(data).filter((entry) => entry.can_read)) {
    const pages = Math.max(0, numberValue(item.readable_page_count));
    if (!pages) continue;
    totalPages += pages;
    const progress = progressFor(item);
    if (!progress) continue;
    readPages += progress.completed ? pages : Math.min(pages, Math.max(0, progress.index + 1));
  }
  return {
    percent: totalPages ? (readPages / totalPages) * 100 : 0,
    readPages,
    totalPages,
  };
}

export function chapterLabel(item: WorkSummary): string {
  const label = String(item.item_label || item.display_title || item.title || "").trim();
  const matches = [...label.matchAll(/第\s*\d+(?:\.\d+)?\s*[话話卷巻章册冊]|(?:vol\.?|卷|巻)\s*\d+(?:\.\d+)?/giu)];
  const concise = matches.at(-1)?.[0]?.replace(/\s+/g, "");
  return concise || cleanTitle(label);
}

export function readerNextItemLabel(item: WorkSummary, locale: Locale = DEFAULT_LOCALE): string {
  const label = chapterLabel(item);
  return label.length <= 10
    ? localizeMessage({ "zh-CN": "下一话 · {label}", en: "Next · {label}", ja: "次の話 · {label}" }, locale, { label })
    : localizeMessage({ "zh-CN": "下一话", en: "Next chapter", ja: "次の話" }, locale);
}

export function userMarkMatchesPayload(mark: UserMark, payload: UserMarkSavePayload, field: PersonalMarkField): boolean {
  return Object.is(mark[field], payload[field]);
}

export function focusRelatedSection(id: "detail-related-editions-title" | "detail-related-series-title" | "detail-related-creators-title") {
  window.requestAnimationFrame(() => {
    const target = document.getElementById(id);
    target?.scrollIntoView({ behavior: preferredScrollBehavior(), block: "start" });
    target?.focus({ preventScroll: true });
  });
}

export async function readerPreparationRequest<T>(label: string, request: Promise<T>, locale: Locale = DEFAULT_LOCALE): Promise<T> {
  try {
    return await request;
  } catch (reason) {
    if ((reason as { name?: string })?.name === "AbortError") throw reason;
    const status = reason instanceof ApiError && reason.status > 0 ? `（HTTP ${reason.status}）` : "";
    throw new Error(localizeMessage({
      "zh-CN": "{label}读取失败{status}：{error}",
      en: "Could not load {label}{status}: {error}",
      ja: "{label}を読み込めませんでした{status}：{error}",
    }, locale, { label, status, error: apiErrorText(reason, locale) }), { cause: reason });
  }
}

export function detailMatchesTarget(detail: DetailState | null, targetType: TargetType, targetID: string): boolean {
  if (!detail || detail.kind !== targetType) return false;
  return detail.kind === "work"
    ? detail.data.work.candidate_id === targetID
    : detail.data.series.group_id === targetID;
}

export function detailHasUnsavedNote(detail: DetailState | null, draft: string): boolean {
  if (!detail || draft === String(detail.data.mark?.notes || "")) return false;
  const targetID = detail.kind === "work" ? detail.data.work.candidate_id : detail.data.series.group_id;
  return !hasPendingUserMark({ target_type: detail.kind, target_id: targetID, notes: draft });
}

export function remainingMinutes(item: CatalogItem): number | null {
  const progress = progressFor(item);
  if (!progress?.count) return null;
  const remainingPages = Math.max(0, progress.count - (progress.index + 1));
  if (!remainingPages) return 0;
  return Math.max(1, Math.ceil((remainingPages * 35) / 60));
}

export function formatLastRead(item: CatalogItem, locale: Locale = DEFAULT_LOCALE): string {
  const value = String(
    item.progress?.last_read_at
      || item.progress_last_read_at
      || item.progress?.updated_at
      || item.progress_updated_at
      || "",
  ).trim();
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const readDay = new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
  const dayDelta = Math.round((today - readDay) / 86_400_000);
  const label = dayDelta === 0
    ? localizeMessage({ "zh-CN": "今天", en: "Today", ja: "今日" }, locale)
    : dayDelta === 1
      ? localizeMessage(
        date.getHours() >= 18
          ? { "zh-CN": "昨晚", en: "Last night", ja: "昨夜" }
          : { "zh-CN": "昨天", en: "Yesterday", ja: "昨日" },
        locale,
      )
      : new Intl.DateTimeFormat(intlLocale(locale), { month: "short", day: "numeric" }).format(date);
  const time = new Intl.DateTimeFormat(intlLocale(locale), { hour: "2-digit", minute: "2-digit", hour12: false }).format(date);
  return `${label} · ${time}`;
}

export function heroNote(item: CatalogItem, detail: WorkDetailResponse | null, locale: Locale = DEFAULT_LOCALE): string {
  const personalNote = String(detail?.mark?.notes || "").trim();
  if (personalNote) return personalNote;
  const progress = progressFor(item);
  if (progress?.completed) return localizeMessage({ "zh-CN": "这一册已经读完，随时可以回来重温。", en: "Finished. Come back whenever you want to reread it.", ja: "読了済みです。いつでも読み返せます。" }, locale);
  if (progress?.count) {
    const remainingPages = Math.max(0, progress.count - (progress.index + 1));
    if (remainingPages > 0 && remainingPages <= 8) return localizeMessage({ "zh-CN": "只剩 {pages} 页，今晚正好读完这一段。", en: "Only {pages} pages left—just enough to finish tonight.", ja: "残り{pages}ページ。今夜ちょうど読み切れそうです。" }, locale, { pages: remainingPages });
  }
  if (progress) return localizeMessage({ "zh-CN": "书页替你记住了上次停下的地方。", en: "Your last page is waiting for you.", ja: "前回止めたページを覚えています。" }, locale);
  return localizeMessage({ "zh-CN": "新入馆的一册，等你从第一页翻开。", en: "A new arrival, waiting to be opened from page one.", ja: "新しく加わった一冊。最初のページからどうぞ。" }, locale);
}
