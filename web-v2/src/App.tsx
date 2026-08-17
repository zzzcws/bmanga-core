import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type MouseEvent,
} from "react";

import {
  ApiError,
  apiErrorText,
  getContinueTarget,
  getDiscover,
  getLibraryPageState,
  getPages,
  getProgress,
  getRandomWork,
  getReadingHistory,
  getSeries,
  getSeriesDetail,
  getSeriesProgress,
  getShelf,
  getUserMark,
  getWork,
  getWorks,
  coverUrl,
  pageUrl,
  saveLibraryPageState,
  saveLibraryPageStates,
  saveProgress,
  saveUserMark,
} from "./lib/api";
import { LibraryMasthead, LibraryToolbar } from "./components/LibraryChrome";
import { Brand } from "./components/Brand";
import { BookCard, Cover } from "./components/CatalogCard";
import { CatalogSkeleton, Status } from "./components/CatalogFeedback";
import { Pagination } from "./components/Pagination";
import {
  ReaderControls,
  ReaderProgress,
  ReaderTopbar,
  type ActiveReaderFitMode,
} from "./components/ReaderChrome";
import { SectionHeader } from "./components/SectionHeader";
import {
  DiscoveryLead,
  DiscoveryModeRail,
  EditorialMasthead,
  EveningHero,
  MetricLedger,
  SearchLead,
  SearchStart,
} from "./components/BrowseChrome";
import { DetailCoverFrame, DetailHeader, PersonalNoteEditor } from "./components/DetailChrome";
import { AsyncRegionBoundary } from "./components/AsyncRegionBoundary";
import { RelatedWorks } from "./components/RelatedWorks";
import { nextSeriesReadable } from "./lib/seriesOrder";
import { preferredScrollBehavior } from "./lib/motion";
import { hasDiscoverAuxiliary, mergeDiscoverPayload, planDiscoverRequest } from "./lib/discoverState";
import {
  CatalogPageCache,
  adjacentCatalogOffsets,
  catalogCacheScopeKey,
  catalogPageCacheKey,
  favoritesCacheScopeKey,
  favoritesPageCacheKey,
  isFavoritesPageFresh,
  mergeCatalogResponse,
  mergeFavoritesResponse,
  nextFavoritesOffset,
  selectFavoritesDisplayItems,
  type CatalogCacheScope,
  type CatalogPage,
  type FavoritesCacheScope,
  type FavoritesPage,
} from "./lib/catalogPageCache";
import {
  CATALOG_PAGE_SIZE as PAGE_SIZE,
  FAVORITES_PAGE_SIZE,
  browseRouteKey,
  defaultBrowseRoute,
  libraryPageScopeOffset,
  parseBrowseURL,
  rememberLibraryPageScope,
  sanitizeBrowseRoute,
  serializeBrowseURL,
  type BrowseRouteState,
  type BrowseScopeState,
  type CatalogMode,
  type CatalogSort,
  type DiscoverMode,
  type LibraryPageScopes,
  type View,
} from "./lib/browseRoute";
import {
  acknowledgePendingProgress,
  discardPendingProgressForCandidate,
  enqueuePendingProgress,
  nextProgressTimestamp,
  pendingProgressCount,
  pendingProgressEntries,
} from "./lib/progressQueue";
import {
  acknowledgePendingLibraryPageMutation,
  buildLibraryPageMutation,
  clearCachedLibraryPageState,
  compactLibraryPageMutations,
  explicitLibraryPageParameters,
  libraryPageScopesFromState,
  parseLibraryPageState,
  pendingLibraryPageMutations,
  rebaseLibraryPageMutation,
  enqueuePendingLibraryPageMutation,
  reconcileLibraryPageStates,
  writeCachedLibraryPageState,
  type LibraryPageState,
  type LibraryPageStateMutation,
} from "./lib/libraryPageState";
import {
  acknowledgePendingUserMark,
  flushPendingUserMarks,
  hasPendingUserMark,
  queuePendingUserMark,
} from "./lib/userMarkQueue";
import {
  readerImageRequestKey,
  requestedSplitPanelForPage,
  readerStageClickAction,
  showReaderVisualLoading,
  shouldRefreshReaderChromeOnPointerMove,
} from "./lib/readerInteraction";
import { ReaderPageCache, ReaderPageCacheTimeoutError } from "./lib/readerPageCache";
import { readerForwardPrefetchIndices } from "./lib/readerPrefetch";
import {
  ReaderPreparationCache,
  safeReaderWarmIndex,
  waitForReaderPreparation,
} from "./lib/readerPreparationCache";
import { splitWideActive, splitWidePanelStep } from "./lib/readerSpread";
import { readerUsesSourceQuality, snapReaderPixel } from "./lib/readerImage";
import {
  patchCatalogItemProgress,
  patchContinueTargetProgress,
  patchDetailProgress,
  patchHistoryEntryDetailProgress,
  type DetailState,
} from "./lib/detailProgress";
import type { PersonalMarkField } from "./lib/userMarks";
import {
  catalogModeOptions,
  catalogSortOptions,
  cleanTitle,
  isSeries,
  itemCoverID,
  itemID,
  itemKindDisplayLabel,
  itemKindLabel,
  itemTitle,
  pageMeta,
  progressFor,
} from "./lib/catalogPresentation";
import { uniqueRelatedWorks } from "./lib/relatedWorksPresentation";
import { workCreatorNames, workSeriesNames, workTranslationNames } from "./lib/workMetadataPresentation";
import type {
  CatalogItem,
  ContinueTarget,
  DiscoverResponse,
  PagesResponse,
  ProgressSavePayload,
  ReadingHistoryItem,
  ReadingProgress,
  SeriesDetailResponse,
  ShelfItem,
  TargetType,
  UserMark,
  UserMarkSavePayload,
  WorkDetailResponse,
  WorkSummary,
} from "./types";
import {
  type CatalogActiveRequest,
  type CatalogPrefetchEntry,
  type FavoritesActiveRequest,
  type FavoritesPrefetchEntry,
  type PersistReaderOptions,
  type ReaderContext,
  type ReaderIntent,
  type ReaderPageAsset,
  type ReaderPagePrefetchPlan,
  type ReaderState,
  type ToastState,
  type UiHistoryEntry,
  type UiHistoryMarker,
  type UiSnapshot,
  CATALOG_PREFETCH_LIMIT,
  EMPTY_CATALOG_ITEMS,
  EMPTY_SHELF_ITEMS,
  HOME_RECENT_LIMIT,
  MY_HISTORY_LIMIT,
  READER_FIT_KEY,
  browseRouteFromSnapshot,
  browseSnapshotOverrides,
  chapterLabel,
  compactNumber,
  detailHasUnsavedNote,
  detailMatchesTarget,
  detailReaderWarmTarget,
  discoverModes,
  favoriteFor,
  focusRelatedSection,
  formatDay,
  formatLastRead,
  greeting,
  heroCreator,
  heroNote,
  heroPosition,
  initialBrowseRoute,
  localDateKey,
  markLegacyLibrarySortHandled,
  navItems,
  numberValue,
  persistBrowseScopes,
  persistLibraryPageScopes,
  replaceLibraryPageScopes,
  progressTimestamp,
  readerImageMax,
  readerNextItemLabel,
  recordValue,
  remainingMinutes,
  rememberReaderFit,
  sameManifest,
  seriesAggregateProgress,
  seriesContinueItem,
  shouldPrefetchReaderPages,
  storedBrowseScopes,
  storedLibraryPageScopes,
  storedReaderFit,
  userMarkMatchesPayload,
  viewLabels,
  ReaderPageResponseError,
  loadReaderPageAsset,
  readerPreparationRequest,
} from "./appShared";

const SeriesDirectory = lazy(() => import("./components/SeriesDirectory").then((module) => ({ default: module.SeriesDirectory })));
const PersonalMarkPanel = lazy(() => import("./components/PersonalMarkPanel").then((module) => ({ default: module.PersonalMarkPanel })));
const LIBRARY_PAGE_HYDRATION_TIMEOUT_MS = 1200;
const LIBRARY_PAGE_SYNC_DELAY_MS = 450;

type LibraryPageStartupMutationStage = {
  route: BrowseRouteState;
  scopes: LibraryPageScopes;
  generation: number;
  notBefore: number;
};

function libraryPageRouteSignature(value: BrowseRouteState): string {
  const route = sanitizeBrowseRoute(value);
  return `${route.sort}\u0000${route.catalogMode}\u0000${route.offset}`;
}

function App() {
  const initialBrowseRef = useRef<BrowseRouteState | null>(null);
  if (!initialBrowseRef.current) initialBrowseRef.current = initialBrowseRoute(typeof window === "undefined" ? "/v2/" : window.location.href);
  const initialBrowse = initialBrowseRef.current;
  const browseScopesRef = useRef<BrowseScopeState | null>(null);
  if (!browseScopesRef.current) {
    browseScopesRef.current = storedBrowseScopes();
    browseScopesRef.current[initialBrowse.view] = initialBrowse;
  }
  const libraryPageScopesRef = useRef<LibraryPageScopes | null>(null);
  if (!libraryPageScopesRef.current) {
    libraryPageScopesRef.current = rememberLibraryPageScope(
      storedLibraryPageScopes(initialBrowse.sort, initialBrowse.view === "library"),
      initialBrowse,
    );
  }
  const initialLibraryPageParametersRef = useRef(explicitLibraryPageParameters(
    typeof window === "undefined" ? "/v2/" : window.location.href,
  ));
  const libraryPageCanonicalRef = useRef<LibraryPageState | null>(null);
  const libraryPagePendingRef = useRef<LibraryPageStateMutation[] | null>(null);
  if (libraryPagePendingRef.current === null) libraryPagePendingRef.current = pendingLibraryPageMutations();
  const libraryPageApplyingRemoteRef = useRef(false);
  const libraryPageDeferredStateRef = useRef<LibraryPageState | null>(null);
  const libraryPageDeferredStartupIntentRef = useRef(false);
  const libraryPageStartupIntentActiveRef = useRef(true);
  const libraryPageStartupMutationRef = useRef<LibraryPageStartupMutationStage | null>(null);
  const libraryPageNavigationGenerationRef = useRef(0);
  const libraryPageStartupGenerationRef = useRef(0);
  const libraryPageLastRouteSignatureRef = useRef("");
  const libraryPageSyncTimerRef = useRef<number | null>(null);
  const libraryPageStartupSyncTimerRef = useRef<number | null>(null);
  const libraryPageFlushRef = useRef<Promise<LibraryPageState | null> | null>(null);
  const libraryPageRefreshRef = useRef<Promise<boolean> | null>(null);
  const libraryPageStateConfirmedRef = useRef(false);
  const libraryPageEntryRefreshRef = useRef(false);
  const libraryPagePreviousViewRef = useRef<View>(initialBrowse.view);
  const [libraryPageStateReady, setLibraryPageStateReady] = useState(false);
  const [libraryPageStateConfirmed, setLibraryPageStateConfirmed] = useState(false);
  const [libraryPageEntryRefreshing, setLibraryPageEntryRefreshing] = useState(false);
  const [view, setView] = useState<View>(initialBrowse.view);
  const [history, setHistory] = useState<ReadingHistoryItem[]>([]);
  const [historyLoading, setHistoryLoading] = useState(true);
  const [historyError, setHistoryError] = useState("");
  const [continueTarget, setContinueTarget] = useState<ContinueTarget | null>(null);
  const [continueLoading, setContinueLoading] = useState(true);
  const [continueError, setContinueError] = useState("");
  const [recent, setRecent] = useState<ShelfItem[]>([]);
  const [recentTotal, setRecentTotal] = useState(0);
  const [recentLoading, setRecentLoading] = useState(true);
  const [recentError, setRecentError] = useState("");
  const [catalog, setCatalog] = useState<CatalogItem[]>([]);
  const [catalogTotal, setCatalogTotal] = useState(0);
  const [catalogDisplayScopeID, setCatalogDisplayScopeID] = useState("");
  const [catalogDisplayOffset, setCatalogDisplayOffset] = useState(-1);
  const [catalogMode, setCatalogMode] = useState<CatalogMode>(initialBrowse.catalogMode);
  const [sort, setSort] = useState<CatalogSort>(initialBrowse.sort);
  const [offset, setOffset] = useState(initialBrowse.view === "library" || initialBrowse.view === "search" ? initialBrowse.offset : 0);
  const [searchDraft, setSearchDraft] = useState(initialBrowse.searchQuery);
  const [searchQuery, setSearchQuery] = useState(initialBrowse.searchQuery);
  const [discover, setDiscover] = useState<DiscoverResponse | null>(null);
  const [discoverMode, setDiscoverMode] = useState<DiscoverMode>(initialBrowse.discoverMode);
  const [discoverLoading, setDiscoverLoading] = useState(false);
  const [discoverError, setDiscoverError] = useState("");
  const [discoverErrorKind, setDiscoverErrorKind] = useState<"batch" | "random">("batch");
  const [discoverRevision, setDiscoverRevision] = useState(0);
  const [randomOpening, setRandomOpening] = useState(false);
  const [favorites, setFavorites] = useState<ShelfItem[]>([]);
  const [favoritesTotal, setFavoritesTotal] = useState(0);
  const [favoritesOffset, setFavoritesOffset] = useState(initialBrowse.view === "my" ? initialBrowse.offset : 0);
  const [favoritesDisplayOffset, setFavoritesDisplayOffset] = useState(-1);
  const [favoritesLoading, setFavoritesLoading] = useState(false);
  const [favoritesSettledKey, setFavoritesSettledKey] = useState("");
  const [favoritesError, setFavoritesError] = useState("");
  const [favoritesRevision, setFavoritesRevision] = useState(0);
  const [readerFitPreference, setReaderFitPreference] = useState<ActiveReaderFitMode>(() => storedReaderFit(null));
  const [favoriteSavingIDs, setFavoriteSavingIDs] = useState<Set<string>>(() => new Set());
  const [noteDraft, setNoteDraft] = useState("");
  const [noteSaving, setNoteSaving] = useState(false);
  const [personalMarkSavingField, setPersonalMarkSavingField] = useState<PersonalMarkField | null>(null);
  const [personalMarkStatus, setPersonalMarkStatus] = useState("");
  const [toast, setToast] = useState<ToastState | null>(null);
  const [dataRevision, setDataRevision] = useState(0);
  const [catalogLoading, setCatalogLoading] = useState(false);
  const [catalogSettledKey, setCatalogSettledKey] = useState("");
  const [catalogRevision, setCatalogRevision] = useState(0);
  const [catalogError, setCatalogError] = useState("");
  const [detail, setDetail] = useState<DetailState | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailIntent, setDetailIntent] = useState<CatalogItem | null>(null);
  const [detailError, setDetailError] = useState("");
  const [reader, setReader] = useState<ReaderState | null>(null);
  const [readerLoading, setReaderLoading] = useState(false);
  const [readerIntent, setReaderIntent] = useState<ReaderIntent | null>(null);
  const [readerRetryIntent, setReaderRetryIntent] = useState<ReaderIntent | null>(null);
  const [readerPageDraft, setReaderPageDraft] = useState("");
  const [readerStageSize, setReaderStageSize] = useState({ width: 0, height: 0 });
  const [readerSlowLoadingKey, setReaderSlowLoadingKey] = useState("");
  const [pendingProgressTotal, setPendingProgressTotal] = useState(() => pendingProgressCount());
  const [heroDetail, setHeroDetail] = useState<WorkDetailResponse | null>(null);
  const detailRef = useRef<DetailState | null>(null);
  const detailSessionRef = useRef(0);
  const detailPanelRef = useRef<HTMLElement | null>(null);
  const detailScrollRef = useRef<HTMLDivElement | null>(null);
  const pendingDetailScrollTopRef = useRef<number | null>(null);
  const detailLoadingCloseRef = useRef<HTMLButtonElement | null>(null);
  const detailLoadAbortRef = useRef<AbortController | null>(null);
  const readerSessionRef = useRef(0);
  const detailCloseRef = useRef<HTMLButtonElement | null>(null);
  const detailTriggerRef = useRef<HTMLElement | null>(null);
  const noteSavingRef = useRef(false);
  const readerCloseRef = useRef<HTMLButtonElement | null>(null);
  const readerLoadingCloseRef = useRef<HTMLButtonElement | null>(null);
  const readerDialogRef = useRef<HTMLElement | null>(null);
  const readerCalibrationRef = useRef<HTMLElement | null>(null);
  const readerCalibrationPrimaryRef = useRef<HTMLButtonElement | null>(null);
  const readerEndingRef = useRef<HTMLElement | null>(null);
  const readerTriggerRef = useRef<HTMLElement | null>(null);
  const readerStageRef = useRef<HTMLDivElement | null>(null);
  const readerLoadAbortRef = useRef<AbortController | null>(null);
  const readerPageAbortRef = useRef<AbortController | null>(null);
  const readerPageCacheRef = useRef<ReaderPageCache<ReaderPageAsset> | null>(null);
  if (!readerPageCacheRef.current) {
    readerPageCacheRef.current = new ReaderPageCache(loadReaderPageAsset, {
      dispose: (asset) => URL.revokeObjectURL(asset.objectURL),
      maxEntries: 4,
      timeoutMs: 25_000,
    });
  }
  const readerPreparationCacheRef = useRef<ReaderPreparationCache<PagesResponse> | null>(null);
  if (!readerPreparationCacheRef.current) {
    readerPreparationCacheRef.current = new ReaderPreparationCache(
      (candidateID, signal) => getPages(candidateID, { signal }),
      { maxEntries: 1, timeoutMs: 20_000, ttlMs: 12_000 },
    );
  }
  const readerImageCacheKeyRef = useRef("");
  const readerImageURLRef = useRef("");
  const readerPrefetchPlanRef = useRef<ReaderPagePrefetchPlan | null>(null);
  const readerPrefetchTimerRef = useRef<number | null>(null);
  const readerScrollTimerRef = useRef<number | null>(null);
  const readerChromeTimerRef = useRef<number | null>(null);
  const readerPointerMoveAtRef = useRef(0);
  const readerTouchStartRef = useRef<{ x: number; y: number } | null>(null);
  const readerSuppressClickRef = useRef(false);
  const readerSuppressClickTimerRef = useRef<number | null>(null);
  const progressSaveErrorRef = useRef<Set<string>>(new Set());
  const searchRef = useRef<HTMLInputElement | null>(null);
  const catalogTopRef = useRef<HTMLDivElement | null>(null);
  const mainRef = useRef<HTMLElement | null>(null);
  const previousViewRef = useRef<View>(view);
  const paginationFocusIntentRef = useRef<"catalog" | "favorites" | null>(null);
  const paginationFocusSawBusyRef = useRef(false);
  const previousReaderEndingRef = useRef(false);
  const toastTimerRef = useRef<number | null>(null);
  const favoriteSavingRef = useRef<Set<string>>(new Set());
  const catalogRequestRef = useRef(0);
  const catalogPageCacheRef = useRef(new CatalogPageCache<CatalogPage<CatalogItem>>(24));
  const catalogPrefetchRef = useRef<Map<string, CatalogPrefetchEntry>>(new Map());
  const catalogCoverWarmRef = useRef<Map<string, HTMLImageElement>>(new Map());
  const catalogActiveRequestRef = useRef<CatalogActiveRequest | null>(null);
  const favoritesRequestRef = useRef(0);
  const favoritesPageCacheRef = useRef(new CatalogPageCache<FavoritesPage<ShelfItem>>(6));
  const favoritesPrefetchRef = useRef<Map<string, FavoritesPrefetchEntry>>(new Map());
  const favoritesActiveRequestRef = useRef<FavoritesActiveRequest | null>(null);
  const discoverRequestRef = useRef(0);
  const discoverAuxiliaryRevisionRef = useRef<number | null>(null);
  const uiRef = useRef<UiSnapshot | null>(null);
  const historySessionRef = useRef(typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : `bmanga-${Date.now()}-${Math.random()}`);
  const historyEntriesRef = useRef<Map<number, UiHistoryEntry>>(new Map());
  const historyCurrentRef = useRef(0);
  const historyNextRef = useRef(0);
  const historyPositionRef = useRef(0);
  const historyReadyRef = useRef(false);
  const historyBouncingRef = useRef(false);
  const historyBounceExpectedRef = useRef<{ entry: number; position: number } | null>(null);
  const historyNavigatingRef = useRef(false);
  const historyPendingCollapseRef = useRef(false);
  const persistReaderRef = useRef<(current: ReaderState, options?: PersistReaderOptions) => Promise<ReadingProgress | null>>(async () => null);
  const resumeDetailRef = useRef<(item: CatalogItem, entry: number) => void>(() => undefined);
  const resumeReaderRef = useRef<(item: WorkSummary, requestedIndex: number | undefined, entry: number, context?: ReaderContext) => void>(() => undefined);
  const readerSaveSignatureRef = useRef<Map<string, string>>(new Map());
  const readerSaveSequenceRef = useRef<Map<string, number>>(new Map());
  const readerSaveAppliedRef = useRef<Map<string, number>>(new Map());
  const readerCatalogRefreshNeededRef = useRef(false);
  const readerFavoritesRefreshNeededRef = useRef(false);
  const readerMoveRef = useRef<(delta: number) => void>(() => undefined);
  const readerViewportRef = useRef<(direction: -1 | 1) => void>(() => undefined);
  const readerRetryRef = useRef<() => void>(() => undefined);
  const progressFlushRef = useRef<Promise<void> | null>(null);
  const randomRequestRef = useRef(0);
  const randomAbortRef = useRef<AbortController | null>(null);
  const seriesDetailCacheRef = useRef<Map<string, Promise<SeriesDetailResponse>>>(new Map());
  const seriesDetailResolvedRef = useRef<Map<string, SeriesDetailResponse>>(new Map());
  const discoverModeRef = useRef<DiscoverMode>(discoverMode);
  const detailTargetID = detail?.kind === "work" ? detail.data.work.candidate_id : detail?.data.series.group_id;
  const detailSavedNote = detail ? String(detail.data.mark?.notes || "") : "";
  const noteDirty = Boolean(detail && noteDraft !== detailSavedNote);
  const detailBusy = noteSaving || personalMarkSavingField !== null || Boolean(detailTargetID && favoriteSavingIDs.has(detailTargetID));
  const catalogCacheScope: CatalogCacheScope | null = view === "library" || view === "search"
    ? {
      dataRevision,
      requestRevision: catalogRevision,
      view,
        library: "",
        catalogMode,
        sort,
        searchQuery: view === "search" ? searchQuery : "",
        limit: PAGE_SIZE,
      }
    : null;
  const catalogCacheScopeID = catalogCacheScope ? catalogCacheScopeKey(catalogCacheScope) : "";
  const catalogRequestKey = ((view === "library" && libraryPageStateReady && !libraryPageEntryRefreshing) || (view === "search" && searchQuery.trim()))
    ? JSON.stringify([view, catalogMode, sort, offset, view === "search" ? searchQuery : "", catalogRevision, dataRevision])
    : "";
  const catalogCurrentCacheKey = catalogCacheScope && catalogRequestKey
    ? catalogPageCacheKey(catalogCacheScope, offset)
    : "";
  const catalogCachedPage = catalogCurrentCacheKey
    ? catalogPageCacheRef.current.peek(catalogCurrentCacheKey)
    : undefined;
  const catalogPending = Boolean(catalogRequestKey && catalogSettledKey !== catalogRequestKey && !catalogCachedPage);
  const libraryPageHydrating = view === "library" && (!libraryPageStateReady || libraryPageEntryRefreshing);
  const catalogBusy = libraryPageHydrating || Boolean(catalogRequestKey && !catalogCachedPage && (catalogLoading || catalogPending));
  const catalogStateMatchesScope = Boolean(catalogCacheScopeID && catalogDisplayScopeID === catalogCacheScopeID);
  const catalogStatePage = catalogStateMatchesScope
    ? { items: catalog, total: catalogTotal, offset: catalogDisplayOffset }
    : null;
  const catalogStateIsCurrent = Boolean(catalogStatePage && catalogDisplayOffset === offset);
  const catalogShowingStale = Boolean(catalogBusy && !catalogCachedPage && catalogStatePage?.items.length);
  const catalogPresentationPage = catalogCachedPage
    || (catalogStateIsCurrent || catalogShowingStale ? catalogStatePage : null);
  const catalogHasPresentationPage = Boolean(catalogPresentationPage);
  const catalogPageReady = Boolean(catalogCachedPage || (catalogStateIsCurrent && !catalogBusy));
  const catalogPresentationItems = catalogPresentationPage?.items || EMPTY_CATALOG_ITEMS;
  const catalogPresentationTotal = catalogPresentationPage?.total || 0;
  const catalogPresentationOffset = catalogPresentationPage?.offset ?? offset;
  const catalogVisibleError = !catalogBusy && !catalogCachedPage && catalogSettledKey === catalogRequestKey
    ? catalogError
    : "";
  const favoritesCacheScope: FavoritesCacheScope = {
    dataRevision,
    requestRevision: favoritesRevision,
    limit: FAVORITES_PAGE_SIZE,
  };
  const favoritesCacheScopeID = favoritesCacheScopeKey(favoritesCacheScope);
  const favoritesRequestKey = view === "my" ? favoritesPageCacheKey(favoritesCacheScope, favoritesOffset) : "";
  const favoritesPending = Boolean(favoritesRequestKey && favoritesSettledKey !== favoritesRequestKey);
  const favoritesBusy = favoritesLoading || favoritesPending;

  const invalidateCatalogPageCache = useCallback(() => {
    catalogPageCacheRef.current.clear();
    for (const entry of catalogPrefetchRef.current.values()) entry.controller.abort();
    catalogPrefetchRef.current.clear();
    const active = catalogActiveRequestRef.current;
    if (active) {
      catalogActiveRequestRef.current = null;
      if (catalogRequestRef.current === active.session) catalogRequestRef.current += 1;
      active.controller.abort();
    }
    setCatalogRevision((current) => current + 1);
  }, []);

  const invalidateFavoritesPageCache = useCallback(() => {
    favoritesPageCacheRef.current.clear();
    for (const entry of favoritesPrefetchRef.current.values()) entry.controller.abort();
    favoritesPrefetchRef.current.clear();
    const active = favoritesActiveRequestRef.current;
    if (!active) return;
    favoritesActiveRequestRef.current = null;
    if (favoritesRequestRef.current === active.session) favoritesRequestRef.current += 1;
    active.controller.abort();
  }, []);

  const warmCatalogCovers = useCallback((items: CatalogItem[]) => {
    if (typeof Image === "undefined" || !shouldPrefetchReaderPages()) return;
    const size = (window.devicePixelRatio || 1) >= 1.5 ? 640 : 420;
    for (const item of items.slice(0, 6)) {
      const id = itemCoverID(item);
      if (!id) continue;
      const url = coverUrl(id, size);
      const existing = catalogCoverWarmRef.current.get(url);
      if (existing) {
        catalogCoverWarmRef.current.delete(url);
        catalogCoverWarmRef.current.set(url, existing);
        continue;
      }
      const image = new Image();
      image.decoding = "async";
      image.fetchPriority = "low";
      image.onload = () => {
        try {
          void image.decode().catch(() => undefined);
        } catch {
          // A completed network warmup is still useful when decode() is unavailable.
        }
      };
      image.onerror = () => {
        if (catalogCoverWarmRef.current.get(url) === image) catalogCoverWarmRef.current.delete(url);
      };
      catalogCoverWarmRef.current.set(url, image);
      image.src = url;
      while (catalogCoverWarmRef.current.size > 12) {
        const oldest = catalogCoverWarmRef.current.entries().next().value as [string, HTMLImageElement] | undefined;
        if (!oldest) break;
        catalogCoverWarmRef.current.delete(oldest[0]);
        oldest[1].src = "";
      }
    }
  }, []);

  const retryCatalogPage = useCallback(() => {
    if (catalogCurrentCacheKey) {
      catalogPageCacheRef.current.delete(catalogCurrentCacheKey);
      const prefetched = catalogPrefetchRef.current.get(catalogCurrentCacheKey);
      prefetched?.controller.abort();
      catalogPrefetchRef.current.delete(catalogCurrentCacheKey);
    }
    setCatalogRevision((current) => current + 1);
  }, [catalogCurrentCacheKey]);

  const retryFavoritesPage = useCallback(() => {
    if (favoritesRequestKey) {
      favoritesPageCacheRef.current.delete(favoritesRequestKey);
      const prefetched = favoritesPrefetchRef.current.get(favoritesRequestKey);
      prefetched?.controller.abort();
      favoritesPrefetchRef.current.delete(favoritesRequestKey);
    }
    setFavoritesRevision((current) => current + 1);
  }, [favoritesRequestKey]);

  useEffect(() => () => {
    for (const [key, entry] of catalogPrefetchRef.current) {
      if (entry.scopeKey !== catalogCacheScopeID) continue;
      entry.controller.abort();
      catalogPrefetchRef.current.delete(key);
    }
  }, [catalogCacheScopeID]);

  useEffect(() => () => {
    for (const image of catalogCoverWarmRef.current.values()) image.src = "";
    catalogCoverWarmRef.current.clear();
  }, []);

  useEffect(() => {
    for (const [key, entry] of favoritesPrefetchRef.current) {
      if (view === "my" && entry.scopeKey === favoritesCacheScopeID) continue;
      entry.controller.abort();
      favoritesPrefetchRef.current.delete(key);
    }
    return undefined;
  }, [favoritesCacheScopeID, view]);

  useEffect(() => {
    const intent = paginationFocusIntentRef.current;
    if (!intent) return undefined;
    if ((intent === "catalog" && view !== "library" && view !== "search") || (intent === "favorites" && view !== "my")) {
      paginationFocusIntentRef.current = null;
      paginationFocusSawBusyRef.current = false;
      return undefined;
    }
    const busy = intent === "catalog" ? catalogBusy : favoritesBusy;
    if (busy) {
      paginationFocusSawBusyRef.current = true;
      return undefined;
    }
    if (!paginationFocusSawBusyRef.current && !(intent === "catalog" && catalogPageReady)) return undefined;
    const frame = window.requestAnimationFrame(() => {
      const target = intent === "catalog"
        ? catalogTopRef.current
        : document.querySelector<HTMLElement>(".my-summary");
      target?.focus({ preventScroll: true });
      paginationFocusIntentRef.current = null;
      paginationFocusSawBusyRef.current = false;
    });
    return () => window.cancelAnimationFrame(frame);
  }, [catalogBusy, catalogPageReady, catalogRequestKey, favoritesBusy, view]);

  useEffect(() => {
    if (!noteDirty) return undefined;
    const guardUnsavedNote = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", guardUnsavedNote);
    return () => window.removeEventListener("beforeunload", guardUnsavedNote);
  }, [noteDirty]);

  noteSavingRef.current = noteSaving;
  discoverModeRef.current = discoverMode;
  uiRef.current = {
    view,
    detail,
    detailLoading,
    detailIntent,
    reader,
    readerLoading,
    readerIntent,
    noteDraft,
    offset,
    favoritesOffset,
    searchQuery,
    searchDraft,
    catalogMode,
    sort,
    discoverMode,
    scrollY: typeof window === "undefined" ? 0 : window.scrollY,
    detailScrollTop: detail && detailScrollRef.current ? Math.max(0, Math.round(detailScrollRef.current.scrollTop)) : 0,
  };

  const captureUiSnapshot = useCallback((overrides: Partial<UiSnapshot> = {}): UiSnapshot => {
    const current = uiRef.current;
    if (!current) throw new Error("bmanga V2 history is not ready");
    return {
      ...current,
      scrollY: typeof window === "undefined" ? current.scrollY : window.scrollY,
      detailScrollTop: current.detail && detailScrollRef.current
        ? Math.max(0, Math.round(detailScrollRef.current.scrollTop))
        : current.detailScrollTop,
      ...overrides,
    };
  }, []);

  const applyUiSnapshot = useCallback((snapshot: UiSnapshot) => {
    pendingDetailScrollTopRef.current = snapshot.detail ? Math.max(0, snapshot.detailScrollTop || 0) : null;
    setView(snapshot.view);
    setDetail(snapshot.detail);
    setDetailLoading(snapshot.detailLoading);
    setDetailIntent(snapshot.detailIntent);
    setReader(snapshot.reader);
    setReaderLoading(snapshot.readerLoading);
    setReaderIntent(snapshot.readerIntent);
    setNoteDraft(snapshot.noteDraft);
    setOffset(snapshot.offset);
    setFavoritesOffset(snapshot.favoritesOffset);
    setSearchQuery(snapshot.searchQuery);
    setSearchDraft(snapshot.searchDraft);
    setCatalogMode(snapshot.catalogMode);
    setSort(snapshot.sort);
    setDiscoverMode(snapshot.discoverMode);
  }, []);

  useLayoutEffect(() => {
    const scrollTop = pendingDetailScrollTopRef.current;
    const scroller = detailScrollRef.current;
    if (!detail || scrollTop === null || !scroller) return;
    scroller.scrollTop = scrollTop;
    const frame = window.requestAnimationFrame(() => {
      if (pendingDetailScrollTopRef.current === scrollTop && detailScrollRef.current) {
        detailScrollRef.current.scrollTop = scrollTop;
        pendingDetailScrollTopRef.current = null;
      }
    });
    return () => window.cancelAnimationFrame(frame);
  }, [detail]);

  const pushUiSnapshot = useCallback((routeKey: string, overrides: Partial<UiSnapshot>, previousOverrides: Partial<UiSnapshot> = {}): number | null => {
    if (historyNavigatingRef.current) return null;
    const next = captureUiSnapshot(overrides);
    if (!historyReadyRef.current) {
      applyUiSnapshot(next);
      return -1;
    }
    const currentID = historyCurrentRef.current;
    const currentEntry = historyEntriesRef.current.get(currentID);
    if (currentEntry?.routeKey === routeKey) return null;
    if (currentEntry) currentEntry.snapshot = captureUiSnapshot(previousOverrides);
    const entry = historyNextRef.current + 1;
    const position = historyPositionRef.current + 1;
    historyNextRef.current = entry;
    historyEntriesRef.current.set(entry, { parent: currentID, position, routeKey, snapshot: next });
    const marker: UiHistoryMarker = { bmangaV2: 1, session: historySessionRef.current, entry, position };
    window.history.pushState(marker, "", serializeBrowseURL(window.location.href, browseRouteFromSnapshot(next)));
    historyCurrentRef.current = entry;
    historyPositionRef.current = position;
    applyUiSnapshot(next);
    return entry;
  }, [applyUiSnapshot, captureUiSnapshot]);

  const replaceUiSnapshot = useCallback((routeKey: string, overrides: Partial<UiSnapshot>) => {
    const next = captureUiSnapshot(overrides);
    if (!historyReadyRef.current) {
      applyUiSnapshot(next);
      return;
    }
    const currentID = historyCurrentRef.current;
    const currentEntry = historyEntriesRef.current.get(currentID);
    historyEntriesRef.current.set(currentID, {
      parent: currentEntry?.parent ?? null,
      position: currentEntry?.position ?? historyPositionRef.current,
      routeKey,
      snapshot: next,
    });
    const marker: UiHistoryMarker = { bmangaV2: 1, session: historySessionRef.current, entry: currentID, position: historyPositionRef.current };
    window.history.replaceState(marker, "", serializeBrowseURL(window.location.href, browseRouteFromSnapshot(next)));
    applyUiSnapshot(next);
  }, [applyUiSnapshot, captureUiSnapshot]);

  const rememberBrowseScope = useCallback((snapshot: UiSnapshot) => {
    if (snapshot.view === "home") return;
    const route = browseRouteFromSnapshot(snapshot);
    const scopes = browseScopesRef.current || {};
    scopes[snapshot.view] = route;
    browseScopesRef.current = scopes;
    persistBrowseScopes(scopes);
    if (snapshot.view === "library") {
      const libraryScopes = persistLibraryPageScopes(rememberLibraryPageScope(
        libraryPageScopesRef.current || storedLibraryPageScopes(route.sort, true),
        route,
      ), route.catalogMode);
      libraryPageScopesRef.current = libraryScopes;
    }
  }, []);

  const cancelLibraryPageStartupMutation = useCallback(() => {
    libraryPageStartupMutationRef.current = null;
    if (libraryPageStartupSyncTimerRef.current !== null) {
      window.clearTimeout(libraryPageStartupSyncTimerRef.current);
      libraryPageStartupSyncTimerRef.current = null;
    }
  }, []);

  const releaseLibraryPageStartupMutation = useCallback(() => {
    const staged = libraryPageStartupMutationRef.current;
    if (!staged) return;
    staged.notBefore = 0;
    if (libraryPageStartupSyncTimerRef.current !== null) {
      window.clearTimeout(libraryPageStartupSyncTimerRef.current);
      libraryPageStartupSyncTimerRef.current = null;
    }
  }, []);

  const promoteLibraryPageStartupMutation = useCallback((force = false): LibraryPageStateMutation | null => {
    const staged = libraryPageStartupMutationRef.current;
    if (!staged || (!force && Date.now() < staged.notBefore)) return null;
    const current = uiRef.current;
    if (
      staged.generation !== libraryPageNavigationGenerationRef.current
      || current?.view !== "library"
      || libraryPageRouteSignature(browseRouteFromSnapshot(current)) !== libraryPageRouteSignature(staged.route)
    ) {
      cancelLibraryPageStartupMutation();
      return null;
    }
    const mutation = buildLibraryPageMutation(staged.scopes, staged.route.catalogMode, { offset: staged.route.offset });
    libraryPageStartupMutationRef.current = null;
    if (libraryPageStartupSyncTimerRef.current !== null) {
      window.clearTimeout(libraryPageStartupSyncTimerRef.current);
      libraryPageStartupSyncTimerRef.current = null;
    }
    const memoryPending = compactLibraryPageMutations([...(libraryPagePendingRef.current || []), mutation]);
    const storedPending = enqueuePendingLibraryPageMutation(mutation);
    libraryPagePendingRef.current = compactLibraryPageMutations([...memoryPending, ...storedPending]);
    return mutation;
  }, [cancelLibraryPageStartupMutation]);

  const commitBrowseTransition = useCallback((value: BrowseRouteState, options: { replace?: boolean; scroll?: boolean; searchDraft?: string; previousSnapshot?: Partial<UiSnapshot> } = {}) => {
    const route = sanitizeBrowseRoute(value);
    const current = uiRef.current;
    if (!libraryPageApplyingRemoteRef.current) {
      libraryPageNavigationGenerationRef.current += 1;
      cancelLibraryPageStartupMutation();
    }
    if (current && !libraryPageApplyingRemoteRef.current) rememberBrowseScope(current);
    const draft = options.searchDraft ?? (current?.view === "search" && route.view === "search" ? current.searchDraft : route.searchQuery);
    const overrides: Partial<UiSnapshot> = {
      ...browseSnapshotOverrides(route, draft),
      detail: null,
      detailLoading: false,
      detailIntent: null,
      reader: null,
      readerLoading: false,
      readerIntent: null,
      scrollY: 0,
    };
    const next = captureUiSnapshot(overrides);
    rememberBrowseScope(next);
    const routeKey = browseRouteKey(route);
    const changed = !current
      || Boolean(current.detail || current.reader)
      || browseRouteKey(browseRouteFromSnapshot(current)) !== routeKey;
    if (options.replace) replaceUiSnapshot(routeKey, overrides);
    else pushUiSnapshot(routeKey, overrides, options.previousSnapshot);
    if (options.scroll !== false) window.scrollTo({ top: 0, behavior: "auto" });
    return changed;
  }, [cancelLibraryPageStartupMutation, captureUiSnapshot, pushUiSnapshot, rememberBrowseScope, replaceUiSnapshot]);

  const applyLibraryPageCanonicalState = useCallback((value: unknown, updateVisibleRoute = true): boolean => {
    const state = reconcileLibraryPageStates(value, libraryPageCanonicalRef.current);
    const scopes = libraryPageScopesFromState(state);
    if (!state || !scopes) return false;
    libraryPageCanonicalRef.current = state;
    libraryPageStateConfirmedRef.current = true;
    setLibraryPageStateConfirmed(true);
    writeCachedLibraryPageState(state);
    libraryPageScopesRef.current = replaceLibraryPageScopes(scopes);

    const current = uiRef.current;
    const remembered = browseScopesRef.current?.library;
    const routeSource = current?.view === "library"
      ? browseRouteFromSnapshot(current)
      : remembered || defaultBrowseRoute("library");
    const route = sanitizeBrowseRoute({
      ...routeSource,
      view: "library",
      sort: state.sort,
      offset: state.positions[routeSource.catalogMode].offset,
    });
    const browseScopes = browseScopesRef.current || {};
    browseScopes.library = route;
    browseScopesRef.current = browseScopes;
    persistBrowseScopes(browseScopes);
    libraryPageLastRouteSignatureRef.current = libraryPageRouteSignature(route);

    if (!updateVisibleRoute || current?.view !== "library") return true;
    if (current.detail || current.detailLoading || current.reader || current.readerLoading) {
      libraryPageDeferredStateRef.current = state;
      return true;
    }
    libraryPageDeferredStateRef.current = null;
    libraryPageApplyingRemoteRef.current = true;
    try {
      commitBrowseTransition(route, { replace: true, scroll: false });
    } finally {
      libraryPageApplyingRemoteRef.current = false;
    }
    return true;
  }, [commitBrowseTransition]);

  const flushLibraryPageState = useCallback((options: { keepalive?: boolean } = {}): Promise<LibraryPageState | null> => {
    if (libraryPageFlushRef.current) return libraryPageFlushRef.current;
    const task = (async () => {
      let canonical = libraryPageCanonicalRef.current;
      let singleMutationFallback = false;
      for (;;) {
        const pending = compactLibraryPageMutations(libraryPagePendingRef.current || []);
        if (!pending.length) {
          if (promoteLibraryPageStartupMutation()) {
            singleMutationFallback = false;
            continue;
          }
          break;
        }
        const delivery = singleMutationFallback ? pending.slice(0, 1) : pending;
        try {
          const response = singleMutationFallback
            ? await saveLibraryPageState(delivery[0]!, { keepalive: options.keepalive })
            : await saveLibraryPageStates(delivery, { keepalive: options.keepalive });
          const nextState = reconcileLibraryPageStates(response.state, libraryPageCanonicalRef.current);
          if (nextState) {
            canonical = nextState;
            libraryPageCanonicalRef.current = nextState;
            libraryPageStateConfirmedRef.current = true;
            setLibraryPageStateConfirmed(true);
            writeCachedLibraryPageState(nextState);
          } else {
            canonical = libraryPageCanonicalRef.current;
          }
          const expectedEventIDs = new Set(delivery.map((item) => item.event_id));
          const responseAcknowledged = Array.isArray(response.acknowledged_event_ids)
            ? new Set(response.acknowledged_event_ids.filter((eventID) => expectedEventIDs.has(eventID)))
            : null;
          if (!singleMutationFallback && (
            !responseAcknowledged
            || responseAcknowledged.size !== expectedEventIDs.size
            || [...expectedEventIDs].some((eventID) => !responseAcknowledged.has(eventID))
          )) break;
          const acknowledged = singleMutationFallback ? expectedEventIDs : responseAcknowledged!;
          const memoryRemaining = (libraryPagePendingRef.current || []).filter((item) => !acknowledged.has(item.event_id));
          let storedRemaining = pendingLibraryPageMutations();
          for (const eventID of acknowledged) storedRemaining = acknowledgePendingLibraryPageMutation(eventID);
          libraryPagePendingRef.current = compactLibraryPageMutations([...memoryRemaining, ...storedRemaining]);
        } catch (reason) {
          if (reason instanceof ApiError && reason.status === 400) {
            if (delivery.length > 1) {
              singleMutationFallback = true;
              continue;
            }
            const invalidEventID = delivery[0]!.event_id;
            const memoryRemaining = (libraryPagePendingRef.current || []).filter((item) => item.event_id !== invalidEventID);
            const storedRemaining = acknowledgePendingLibraryPageMutation(invalidEventID);
            const errorPayload = reason.payload && typeof reason.payload === "object"
              ? reason.payload as Record<string, unknown>
              : null;
            const rebased = errorPayload?.code === "future_timestamp"
              ? rebaseLibraryPageMutation(delivery[0], errorPayload.server_time, libraryPageCanonicalRef.current)
              : null;
            if (rebased) {
              const storedWithRebased = enqueuePendingLibraryPageMutation(rebased);
              libraryPagePendingRef.current = compactLibraryPageMutations([
                ...memoryRemaining,
                ...storedRemaining,
                rebased,
                ...storedWithRebased,
              ]);
            } else {
              libraryPagePendingRef.current = compactLibraryPageMutations([...memoryRemaining, ...storedRemaining]);
            }
            continue;
          }
          break;
        }
      }
      if (!(libraryPagePendingRef.current || []).length && !libraryPageStartupMutationRef.current && canonical) {
        applyLibraryPageCanonicalState(canonical);
      }
      return canonical;
    })().finally(() => {
      libraryPageFlushRef.current = null;
    });
    libraryPageFlushRef.current = task;
    return task;
  }, [applyLibraryPageCanonicalState, promoteLibraryPageStartupMutation]);

  const queueLibraryPageState = useCallback((routeValue: BrowseRouteState, scopesValue: LibraryPageScopes): LibraryPageStateMutation => {
    const route = sanitizeBrowseRoute(routeValue);
    const mutation = buildLibraryPageMutation(scopesValue, route.catalogMode, { offset: route.offset });
    const memoryPending = compactLibraryPageMutations([...(libraryPagePendingRef.current || []), mutation]);
    const storedPending = enqueuePendingLibraryPageMutation(mutation);
    libraryPagePendingRef.current = compactLibraryPageMutations([...memoryPending, ...storedPending]);
    if (libraryPageSyncTimerRef.current !== null) window.clearTimeout(libraryPageSyncTimerRef.current);
    libraryPageSyncTimerRef.current = window.setTimeout(() => {
      libraryPageSyncTimerRef.current = null;
      void flushLibraryPageState();
    }, LIBRARY_PAGE_SYNC_DELAY_MS);
    return mutation;
  }, [flushLibraryPageState]);

  const queueLibraryPageStartupState = useCallback((routeValue: BrowseRouteState, scopesValue: LibraryPageScopes) => {
    cancelLibraryPageStartupMutation();
    const route = sanitizeBrowseRoute(routeValue);
    libraryPageStartupMutationRef.current = {
      route,
      scopes: scopesValue,
      generation: libraryPageNavigationGenerationRef.current,
      notBefore: Date.now() + LIBRARY_PAGE_SYNC_DELAY_MS,
    };
    libraryPageStartupSyncTimerRef.current = window.setTimeout(() => {
      libraryPageStartupSyncTimerRef.current = null;
      void flushLibraryPageState().finally(() => {
        const staged = libraryPageStartupMutationRef.current;
        if (
          staged
          && Date.now() >= staged.notBefore
          && !(libraryPagePendingRef.current || []).length
          && libraryPageFlushRef.current === null
        ) {
          void flushLibraryPageState();
        }
      });
    }, LIBRARY_PAGE_SYNC_DELAY_MS);
  }, [cancelLibraryPageStartupMutation, flushLibraryPageState]);

  const applyExplicitLibraryPageStartupRoute = useCallback((canonical: LibraryPageState | null) => {
    const current = uiRef.current;
    const stillInStartupContext = current !== null
      && libraryPageStartupIntentActiveRef.current
      && libraryPageNavigationGenerationRef.current === libraryPageStartupGenerationRef.current
      && current.view === "library"
      && !current.detail
      && !current.detailLoading
      && !current.reader
      && !current.readerLoading;
    if (!stillInStartupContext) {
      libraryPageStartupIntentActiveRef.current = false;
      libraryPageDeferredStartupIntentRef.current = false;
      if (canonical) applyLibraryPageCanonicalState(canonical, false);
      return;
    }
    const explicitParameters = initialLibraryPageParametersRef.current;
    const remoteScopes = libraryPageScopesFromState(canonical);
    const resolvedSort = explicitParameters.sort ? initialBrowse.sort : canonical?.sort || initialBrowse.sort;
    const resolvedOffset = explicitParameters.offset
      ? initialBrowse.offset
      : canonical?.sort === resolvedSort
        ? canonical.positions[initialBrowse.catalogMode].offset
        : 0;
    const route = sanitizeBrowseRoute({ ...initialBrowse, sort: resolvedSort, offset: resolvedOffset });
    const baseScopes: LibraryPageScopes = remoteScopes?.sort === resolvedSort
      ? remoteScopes
      : { sort: resolvedSort, offsets: { all: 0, doujin: 0, series: 0 } };
    const scopes = replaceLibraryPageScopes(rememberLibraryPageScope(baseScopes, route));
    libraryPageScopesRef.current = scopes;
    libraryPageLastRouteSignatureRef.current = libraryPageRouteSignature(route);
    libraryPageDeferredStartupIntentRef.current = false;
    libraryPageStartupIntentActiveRef.current = false;
    libraryPageApplyingRemoteRef.current = true;
    try {
      commitBrowseTransition(route, { replace: true, scroll: false });
    } finally {
      libraryPageApplyingRemoteRef.current = false;
    }
    queueLibraryPageStartupState(route, scopes);
  }, [applyLibraryPageCanonicalState, commitBrowseTransition, initialBrowse, queueLibraryPageStartupState]);

  const refreshLibraryPageState = useCallback((): Promise<boolean> => {
    if (libraryPageRefreshRef.current) return libraryPageRefreshRef.current;
    const task = (async () => {
      if ((libraryPagePendingRef.current || []).length || libraryPageStartupMutationRef.current) {
        await flushLibraryPageState();
        if ((libraryPagePendingRef.current || []).length || libraryPageStartupMutationRef.current) return false;
      }
      try {
        const response = await getLibraryPageState();
        const state = parseLibraryPageState(response.state);
        // A local navigation may have been queued while this GET was in flight.
        // Let that newer event flush instead of briefly replacing it with stale remote state.
        if ((libraryPagePendingRef.current || []).length || libraryPageStartupMutationRef.current) return false;
        libraryPageStateConfirmedRef.current = true;
        setLibraryPageStateConfirmed(true);
        if (!state) {
          libraryPageCanonicalRef.current = null;
          clearCachedLibraryPageState();
          libraryPageLastRouteSignatureRef.current = "";
        }
        if (libraryPageDeferredStartupIntentRef.current) {
          applyExplicitLibraryPageStartupRoute(state);
        } else if (state) {
          applyLibraryPageCanonicalState(state);
        }
        return true;
      } catch {
        // The current origin-local page remains available until shared state can be read again.
        return false;
      }
    })().finally(() => {
      libraryPageRefreshRef.current = null;
    });
    libraryPageRefreshRef.current = task;
    return task;
  }, [applyExplicitLibraryPageStartupRoute, applyLibraryPageCanonicalState, flushLibraryPageState]);

  useEffect(() => {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), LIBRARY_PAGE_HYDRATION_TIMEOUT_MS);
    let active = true;
    let startupRetryTimer: number | null = null;
    const hydrate = async () => {
      const explicitParameters = initialLibraryPageParametersRef.current;
      const explicit = initialBrowse.view === "library" && (explicitParameters.offset || explicitParameters.sort);
      const explicitFullySpecified = explicitParameters.offset && explicitParameters.sort;
      const deferExplicitStartupRoute = () => {
        libraryPageDeferredStartupIntentRef.current = true;
        libraryPageLastRouteSignatureRef.current = libraryPageRouteSignature(initialBrowse);
      };
      if ((libraryPagePendingRef.current || []).length) {
        if (initialBrowse.view === "library") {
          libraryPageLastRouteSignatureRef.current = libraryPageRouteSignature(initialBrowse);
        }
        if (libraryPageSyncTimerRef.current !== null) {
          window.clearTimeout(libraryPageSyncTimerRef.current);
          libraryPageSyncTimerRef.current = null;
        }
        const flushTask = flushLibraryPageState();
        if (!explicit) {
          if (active) setLibraryPageStateReady(true);
          await flushTask;
          window.clearTimeout(timeout);
          return;
        }
        let pendingWaitTimer: number | null = null;
        const canonical = await Promise.race([
          flushTask,
          new Promise<null>((resolve) => {
            pendingWaitTimer = window.setTimeout(() => resolve(null), LIBRARY_PAGE_HYDRATION_TIMEOUT_MS);
          }),
        ]);
        if (pendingWaitTimer !== null) window.clearTimeout(pendingWaitTimer);
        if (active) {
          const resolvedCanonical = canonical || libraryPageCanonicalRef.current;
          if (resolvedCanonical || libraryPageStateConfirmedRef.current || explicitFullySpecified) {
            applyExplicitLibraryPageStartupRoute(resolvedCanonical);
          } else {
            deferExplicitStartupRoute();
          }
          setLibraryPageStateReady(true);
        }
        window.clearTimeout(timeout);
        return;
      }
      try {
        const response = await getLibraryPageState({ signal: controller.signal });
        if (!active) return;
        const state = parseLibraryPageState(response.state);
        libraryPageStateConfirmedRef.current = true;
        setLibraryPageStateConfirmed(true);
        if (state) {
          libraryPageCanonicalRef.current = state;
          writeCachedLibraryPageState(state);
        } else {
          libraryPageCanonicalRef.current = null;
          clearCachedLibraryPageState();
        }
        if (explicit) {
          applyExplicitLibraryPageStartupRoute(state);
          return;
        }
        if (!state) return;
        applyLibraryPageCanonicalState(state);
      } catch {
        if (!active) return;
        // A failed/slow read is not proof that the server has no state. Mark the
        // current route as observed so merely opening an old local page cannot overwrite it.
        if (explicit) {
          if (libraryPageCanonicalRef.current || explicitFullySpecified) {
            applyExplicitLibraryPageStartupRoute(libraryPageCanonicalRef.current);
          } else {
            deferExplicitStartupRoute();
          }
        } else if (initialBrowse.view === "library") {
          libraryPageLastRouteSignatureRef.current = libraryPageRouteSignature(initialBrowse);
        }
        startupRetryTimer = window.setTimeout(() => {
          startupRetryTimer = null;
          if (active) void refreshLibraryPageState();
        }, LIBRARY_PAGE_SYNC_DELAY_MS + 350);
      } finally {
        window.clearTimeout(timeout);
        if (active) setLibraryPageStateReady(true);
      }
    };
    void hydrate();
    return () => {
      active = false;
      window.clearTimeout(timeout);
      if (startupRetryTimer !== null) window.clearTimeout(startupRetryTimer);
      controller.abort();
    };
  }, [applyExplicitLibraryPageStartupRoute, applyLibraryPageCanonicalState, flushLibraryPageState, initialBrowse, refreshLibraryPageState]);

  useEffect(() => {
    if (!libraryPageStateReady || !libraryPageStateConfirmed || !libraryPageDeferredStartupIntentRef.current) return;
    applyExplicitLibraryPageStartupRoute(libraryPageCanonicalRef.current);
  }, [applyExplicitLibraryPageStartupRoute, libraryPageStateConfirmed, libraryPageStateReady]);

  useEffect(() => {
    const previousView = libraryPagePreviousViewRef.current;
    libraryPagePreviousViewRef.current = view;
    if (!libraryPageStateReady || view !== "library" || previousView === "library") return undefined;
    let active = true;
    libraryPageEntryRefreshRef.current = true;
    setLibraryPageEntryRefreshing(true);
    void refreshLibraryPageState().then((confirmed) => {
      if (!active || confirmed) return;
      libraryPageStateConfirmedRef.current = false;
      setLibraryPageStateConfirmed(false);
    }).finally(() => {
      if (!active) return;
      libraryPageEntryRefreshRef.current = false;
      setLibraryPageEntryRefreshing(false);
    });
    return () => {
      active = false;
      libraryPageEntryRefreshRef.current = false;
      setLibraryPageEntryRefreshing(false);
    };
  }, [libraryPageStateReady, refreshLibraryPageState, view]);

  useEffect(() => {
    if (!libraryPageStateReady || view !== "library") return;
    if (detail || detailLoading || reader || readerLoading) return;
    if (libraryPageEntryRefreshRef.current) return;
    if (!libraryPageStateConfirmedRef.current && !(libraryPagePendingRef.current || []).length) {
      void refreshLibraryPageState();
      return;
    }
    const route = uiRef.current ? browseRouteFromSnapshot(uiRef.current) : defaultBrowseRoute("library");
    const signature = libraryPageRouteSignature(route);
    if (libraryPageLastRouteSignatureRef.current === signature) return;
    libraryPageLastRouteSignatureRef.current = signature;
    const scopes = persistLibraryPageScopes(rememberLibraryPageScope(
      libraryPageScopesRef.current || storedLibraryPageScopes(route.sort, true),
      route,
    ), route.catalogMode);
    libraryPageScopesRef.current = scopes;
    queueLibraryPageState(route, scopes);
  }, [catalogMode, detail, detailLoading, libraryPageEntryRefreshing, libraryPageStateConfirmed, libraryPageStateReady, offset, queueLibraryPageState, reader, readerLoading, refreshLibraryPageState, sort, view]);

  useEffect(() => {
    const state = libraryPageDeferredStateRef.current;
    if (!libraryPageStateReady || !state || view !== "library") return;
    if (detail || detailLoading || reader || readerLoading) return;
    applyLibraryPageCanonicalState(state);
  }, [applyLibraryPageCanonicalState, detail, detailLoading, libraryPageStateReady, reader, readerLoading, view]);

  useEffect(() => {
    if (!libraryPageStateReady) return undefined;
    let followupTimer: number | null = null;
    let refreshSequence = 0;
    const refresh = () => {
      const sequence = ++refreshSequence;
      void refreshLibraryPageState().finally(() => {
        if (sequence !== refreshSequence || document.visibilityState !== "visible") return;
        if (followupTimer !== null) window.clearTimeout(followupTimer);
        followupTimer = window.setTimeout(() => {
          followupTimer = null;
          if (document.visibilityState === "visible") void refreshLibraryPageState();
        }, LIBRARY_PAGE_SYNC_DELAY_MS + 350);
      });
    };
    const stopScheduledSync = () => {
      refreshSequence += 1;
      if (followupTimer !== null) {
        window.clearTimeout(followupTimer);
        followupTimer = null;
      }
      if (libraryPageSyncTimerRef.current !== null) {
        window.clearTimeout(libraryPageSyncTimerRef.current);
        libraryPageSyncTimerRef.current = null;
      }
    };
    const flushWhenHidden = () => {
      stopScheduledSync();
      releaseLibraryPageStartupMutation();
      void flushLibraryPageState({ keepalive: true });
    };
    const flushOnPageHide = () => {
      stopScheduledSync();
      promoteLibraryPageStartupMutation(true);
      const pending = compactLibraryPageMutations(libraryPagePendingRef.current || []);
      // Send one self-contained snapshot even if a previous flush is in flight.
      // The server orders/idempotently merges the whole batch; localStorage remains
      // as a retry journal because a pagehide response cannot be relied upon for ACK.
      if (pending.length) void saveLibraryPageStates(pending, { keepalive: true });
    };
    const onVisibilityChange = () => {
      if (document.visibilityState === "visible") refresh();
      else flushWhenHidden();
    };
    window.addEventListener("focus", refresh);
    window.addEventListener("online", refresh);
    document.addEventListener("visibilitychange", onVisibilityChange);
    window.addEventListener("pagehide", flushOnPageHide);
    return () => {
      window.removeEventListener("focus", refresh);
      window.removeEventListener("online", refresh);
      document.removeEventListener("visibilitychange", onVisibilityChange);
      window.removeEventListener("pagehide", flushOnPageHide);
      if (followupTimer !== null) window.clearTimeout(followupTimer);
    };
  }, [flushLibraryPageState, libraryPageStateReady, promoteLibraryPageStartupMutation, refreshLibraryPageState, releaseLibraryPageStartupMutation]);

  useEffect(() => () => {
    if (libraryPageSyncTimerRef.current !== null) window.clearTimeout(libraryPageSyncTimerRef.current);
  }, []);

  const requestHistoryBack = useCallback((fallback: () => void) => {
    if (historyNavigatingRef.current) return;
    const entry = historyEntriesRef.current.get(historyCurrentRef.current);
    if (historyReadyRef.current && entry?.parent !== null && entry?.parent !== undefined) {
      historyNavigatingRef.current = true;
      window.history.back();
      return;
    }
    fallback();
  }, []);

  const closeDetail = useCallback(() => {
    const current = uiRef.current;
    if (detailHasUnsavedNote(current?.detail || null, current?.noteDraft || "")) {
      setToast({ message: "私人备注尚未保存。请先保存，或在备注区撤销修改后再关闭。" });
      return;
    }
    detailLoadAbortRef.current?.abort();
    detailLoadAbortRef.current = null;
    requestHistoryBack(() => {
      detailSessionRef.current += 1;
      setDetailLoading(false);
      setDetailIntent(null);
      setDetail(null);
    });
  }, [requestHistoryBack]);

  const cancelReaderRequests = useCallback((options: { preservePageCache?: boolean } = {}) => {
    readerLoadAbortRef.current?.abort();
    readerLoadAbortRef.current = null;
    readerPageAbortRef.current?.abort();
    readerPageAbortRef.current = null;
    if (readerScrollTimerRef.current !== null) window.clearTimeout(readerScrollTimerRef.current);
    if (readerChromeTimerRef.current !== null) window.clearTimeout(readerChromeTimerRef.current);
    if (readerSuppressClickTimerRef.current !== null) window.clearTimeout(readerSuppressClickTimerRef.current);
    readerScrollTimerRef.current = null;
    readerChromeTimerRef.current = null;
    readerPointerMoveAtRef.current = 0;
    readerSuppressClickTimerRef.current = null;
    readerSuppressClickRef.current = false;
    if (readerPrefetchTimerRef.current !== null) window.clearTimeout(readerPrefetchTimerRef.current);
    readerPrefetchTimerRef.current = null;
    readerPrefetchPlanRef.current = null;
    readerImageCacheKeyRef.current = "";
    readerImageURLRef.current = "";
    readerPageCacheRef.current?.setPinnedKey("");
    if (!options.preservePageCache) readerPageCacheRef.current?.clear();
  }, []);

  const readerWithLiveScroll = useCallback((current: ReaderState): ReaderState => {
    const stage = readerStageRef.current;
    if (!stage || current.fitMode !== "fit-width") return current;
    return {
      ...current,
      stageScrollTop: Math.max(0, Math.round(stage.scrollTop)),
      stageScrollLeft: Math.max(0, Math.round(stage.scrollLeft)),
    };
  }, []);

  const closeReader = useCallback(() => {
    const current = uiRef.current?.reader;
    if (current) void persistReaderRef.current(readerWithLiveScroll(current), { silent: true });
    if (readerCatalogRefreshNeededRef.current) {
      readerCatalogRefreshNeededRef.current = false;
      setCatalogRevision((revision) => revision + 1);
    }
    if (readerFavoritesRefreshNeededRef.current) {
      readerFavoritesRefreshNeededRef.current = false;
      setFavoritesRevision((revision) => revision + 1);
    }
    cancelReaderRequests();
    requestHistoryBack(() => {
      readerSessionRef.current += 1;
      setReaderLoading(false);
      setReaderIntent(null);
      setReader(null);
    });
  }, [cancelReaderRequests, readerWithLiveScroll, requestHistoryBack]);

  const collapseToNearestView = useCallback(() => {
    let entryID = historyCurrentRef.current;
    let steps = 0;
    const currentEntry = historyEntriesRef.current.get(entryID);
    if (currentEntry?.routeKey.startsWith("resolved:") && currentEntry.parent !== null) {
      entryID = currentEntry.parent;
      steps = 1;
    }
    while (steps < 30) {
      const entry = historyEntriesRef.current.get(entryID);
      if (!entry) break;
      if (!entry.snapshot.detail && !entry.snapshot.detailLoading && !entry.snapshot.detailIntent && !entry.snapshot.reader && !entry.snapshot.readerLoading && !entry.snapshot.readerIntent) {
        if (steps > 0) {
          historyNavigatingRef.current = true;
          window.history.go(-steps);
        }
        return;
      }
      if (entry.parent === null) break;
      entryID = entry.parent;
      steps += 1;
    }
    replaceUiSnapshot(`view:${view}`, { detail: null, detailLoading: false, detailIntent: null, reader: null, readerLoading: false, readerIntent: null });
  }, [replaceUiSnapshot, view]);

  const goToNearestView = useCallback(() => {
    if (historyNavigatingRef.current || historyBouncingRef.current) {
      historyPendingCollapseRef.current = true;
      return;
    }
    collapseToNearestView();
  }, [collapseToNearestView]);

  const retireFailedHistoryEntry = useCallback((entryID: number, layer: "detail" | "reader") => {
    const entry = historyEntriesRef.current.get(entryID);
    if (!entry) return;
    const snapshot = layer === "detail"
      ? { ...entry.snapshot, detail: null, detailLoading: false, detailIntent: null }
      : { ...entry.snapshot, reader: null, readerLoading: false, readerIntent: null };
    entry.routeKey = `failed:${entry.routeKey}:${Date.now()}`;
    entry.snapshot = snapshot;
    if (historyCurrentRef.current === entryID) uiRef.current = snapshot;
  }, []);

  const returnToMy = useCallback(() => {
    const current = historyEntriesRef.current.get(historyCurrentRef.current);
    const parent = current?.parent === null || current?.parent === undefined ? null : historyEntriesRef.current.get(current.parent);
    if (parent?.snapshot.view === "my" && !parent.snapshot.detail && !parent.snapshot.reader) {
      requestHistoryBack(() => undefined);
      return;
    }
    commitBrowseTransition(browseScopesRef.current?.my || defaultBrowseRoute("my"), { replace: true });
  }, [commitBrowseTransition, requestHistoryBack]);

  useEffect(() => {
    const previousScrollRestoration = window.history.scrollRestoration;
    window.history.scrollRestoration = "manual";
    if (!historyReadyRef.current) {
      const guard: UiHistoryMarker = { bmangaV2: 1, session: historySessionRef.current, entry: -1, position: -1, guard: 1 };
      const marker: UiHistoryMarker = { bmangaV2: 1, session: historySessionRef.current, entry: 0, position: 0 };
      const snapshot = captureUiSnapshot();
      const href = serializeBrowseURL(window.location.href, browseRouteFromSnapshot(snapshot));
      historyEntriesRef.current.set(0, { parent: null, position: 0, routeKey: browseRouteKey(browseRouteFromSnapshot(snapshot)), snapshot });
      const existing = recordValue(window.history.state) as Partial<UiHistoryMarker>;
      if (existing.bmangaV2 === 1) {
        window.history.replaceState(marker, "", href);
      } else {
        window.history.replaceState(guard, "", href);
        window.history.pushState(marker, "", href);
      }
      historyPositionRef.current = 0;
      historyReadyRef.current = true;
    }
    const onPopState = (event: PopStateEvent) => {
      if (libraryPageStartupIntentActiveRef.current) libraryPageNavigationGenerationRef.current += 1;
      const marker = recordValue(event.state) as Partial<UiHistoryMarker>;
      const knownMarker = marker.bmangaV2 === 1
        && marker.session === historySessionRef.current
        && typeof marker.entry === "number"
        && typeof marker.position === "number"
        && (marker.guard === 1 || historyEntriesRef.current.has(marker.entry));
      if (!knownMarker) {
        if (window.location.pathname !== "/v2" && !window.location.pathname.startsWith("/v2/")) return;
        const current = historyEntriesRef.current.get(historyCurrentRef.current);
        if (current) {
          current.snapshot = captureUiSnapshot();
          if (current.snapshot.reader) void persistReaderRef.current(readerWithLiveScroll(current.snapshot.reader), { silent: true });
        }
        cancelReaderRequests();
        detailSessionRef.current += 1;
        readerSessionRef.current += 1;
        const route = parseBrowseURL(window.location.href);
        const recovered = captureUiSnapshot({
          ...browseSnapshotOverrides(route),
          detail: null,
          detailLoading: false,
          detailIntent: null,
          reader: null,
          readerLoading: false,
          readerIntent: null,
          scrollY: 0,
        });
        const entry = historyNextRef.current + 1;
        const position = typeof marker.position === "number" ? marker.position : historyPositionRef.current - 1;
        historyNextRef.current = entry;
        historyCurrentRef.current = entry;
        historyPositionRef.current = position;
        historyEntriesRef.current.set(entry, { parent: null, position, routeKey: browseRouteKey(route), snapshot: recovered });
        historyNavigatingRef.current = false;
        const active: UiHistoryMarker = { bmangaV2: 1, session: historySessionRef.current, entry, position };
        window.history.replaceState(active, "", serializeBrowseURL(window.location.href, route));
        applyUiSnapshot(recovered);
        window.requestAnimationFrame(() => window.scrollTo({ top: 0, behavior: "auto" }));
        return;
      }
      const markerEntry = marker.entry as number;
      const markerPosition = marker.position as number;
      const flushPendingCollapse = () => {
        if (!historyPendingCollapseRef.current) return;
        historyPendingCollapseRef.current = false;
        window.queueMicrotask(goToNearestView);
      };
      if (historyBouncingRef.current) {
        const expected = historyBounceExpectedRef.current;
        if (expected && markerEntry === expected.entry && markerPosition === expected.position) {
          historyBouncingRef.current = false;
          historyBounceExpectedRef.current = null;
          historyNavigatingRef.current = false;
          flushPendingCollapse();
          return;
        }
        if (expected) {
          const correction = expected.position - markerPosition;
          if (correction !== 0) {
            window.history.go(correction);
            return;
          }
        }
        historyBouncingRef.current = false;
        historyBounceExpectedRef.current = null;
        historyNavigatingRef.current = false;
        return;
      }
      const current = historyEntriesRef.current.get(historyCurrentRef.current);
      const target = marker.guard === 1 ? historyEntriesRef.current.get(0) : historyEntriesRef.current.get(markerEntry);
      if (!target || !current) {
        historyNavigatingRef.current = false;
        return;
      }
      historyNavigatingRef.current = true;
      current.snapshot = captureUiSnapshot();
      const currentDetailID = current.snapshot.detail?.kind === "work"
        ? current.snapshot.detail.data.work.candidate_id
        : current.snapshot.detail?.data.series.group_id;
      const targetDetailID = target.snapshot.detail?.kind === "work"
        ? target.snapshot.detail.data.work.candidate_id
        : target.snapshot.detail?.data.series.group_id;
      const closesDirtyDetail = Boolean(
        currentDetailID
        && currentDetailID !== targetDetailID
        && detailHasUnsavedNote(current.snapshot.detail, current.snapshot.noteDraft),
      );
      const closesBusyDetail = Boolean(currentDetailID && currentDetailID !== targetDetailID && (noteSavingRef.current || favoriteSavingRef.current.has(currentDetailID)));
      if (closesBusyDetail || closesDirtyDetail) {
        if (closesDirtyDetail) setToast({ message: "私人备注尚未保存。请先保存，或在备注区撤销修改后再离开。" });
        const returnDistance = historyPositionRef.current - markerPosition;
        if (returnDistance === 0) {
          const active: UiHistoryMarker = {
            bmangaV2: 1,
            session: historySessionRef.current,
            entry: historyCurrentRef.current,
            position: historyPositionRef.current,
          };
          window.history.replaceState(active, "", serializeBrowseURL(window.location.href, browseRouteFromSnapshot(current.snapshot)));
          historyNavigatingRef.current = false;
          return;
        }
        historyBouncingRef.current = true;
        historyBounceExpectedRef.current = { entry: historyCurrentRef.current, position: historyPositionRef.current };
        window.history.go(returnDistance);
        return;
      }
      if (marker.guard === 1) {
        const active: UiHistoryMarker = { bmangaV2: 1, session: historySessionRef.current, entry: 0, position: 0 };
        if (current.snapshot.reader && (!target.snapshot.reader || target.snapshot.reader.item.candidate_id !== current.snapshot.reader.item.candidate_id)) {
          void persistReaderRef.current(readerWithLiveScroll(current.snapshot.reader), { silent: true });
        }
        cancelReaderRequests();
        detailSessionRef.current += 1;
        readerSessionRef.current += 1;
        historyCurrentRef.current = 0;
        historyPositionRef.current = 0;
        historyNavigatingRef.current = false;
        applyUiSnapshot(target.snapshot);
        window.history.pushState(active, "", serializeBrowseURL(window.location.href, browseRouteFromSnapshot(target.snapshot)));
        window.requestAnimationFrame(() => window.scrollTo({ top: target.snapshot.scrollY || 0, behavior: "auto" }));
        flushPendingCollapse();
        return;
      }
      if (current.snapshot.reader && (!target.snapshot.reader || target.snapshot.reader.item.candidate_id !== current.snapshot.reader.item.candidate_id)) {
        void persistReaderRef.current(readerWithLiveScroll(current.snapshot.reader), { silent: true });
      }
      cancelReaderRequests();
      detailSessionRef.current += 1;
      readerSessionRef.current += 1;
      historyCurrentRef.current = markerEntry;
      historyPositionRef.current = markerPosition;
      const restoredSnapshot = target.snapshot.reader
        ? {
          ...target.snapshot,
          reader: null,
          readerLoading: true,
          readerIntent: {
            item: target.snapshot.reader.item,
            seriesID: target.snapshot.reader.seriesID,
            nextItem: target.snapshot.reader.nextItem,
          },
        }
        : target.snapshot;
      target.snapshot = restoredSnapshot;
      applyUiSnapshot(restoredSnapshot);
      historyNavigatingRef.current = false;
      window.requestAnimationFrame(() => {
        window.scrollTo({ top: target.snapshot.scrollY, behavior: "auto" });
        if (target.snapshot.detailLoading && target.snapshot.detailIntent) {
          resumeDetailRef.current(target.snapshot.detailIntent, marker.entry as number);
        }
        if (target.snapshot.readerLoading && target.snapshot.readerIntent) {
          resumeReaderRef.current(target.snapshot.readerIntent.item, target.snapshot.readerIntent.requestedIndex, marker.entry as number, target.snapshot.readerIntent);
        }
      });
      flushPendingCollapse();
    };
    window.addEventListener("popstate", onPopState);
    return () => {
      window.history.scrollRestoration = previousScrollRestoration;
      window.removeEventListener("popstate", onPopState);
    };
  }, [applyUiSnapshot, cancelReaderRequests, captureUiSnapshot, goToNearestView, readerWithLiveScroll]);

  useEffect(() => {
    if (!historyReadyRef.current) return;
    const current = historyEntriesRef.current.get(historyCurrentRef.current);
    if (!current) return;
    current.snapshot = captureUiSnapshot();
  }, [captureUiSnapshot, catalogMode, detail, detailIntent, detailLoading, discoverMode, favoritesOffset, noteDraft, offset, reader, readerIntent, readerLoading, searchDraft, searchQuery, sort, view]);

  useEffect(() => {
    if (!historyReadyRef.current) return;
    const current = historyEntriesRef.current.get(historyCurrentRef.current);
    if (!current) return;
    const snapshot = captureUiSnapshot();
    current.snapshot = snapshot;
    rememberBrowseScope(snapshot);
    const marker: UiHistoryMarker = {
      bmangaV2: 1,
      session: historySessionRef.current,
      entry: historyCurrentRef.current,
      position: historyPositionRef.current,
    };
    window.history.replaceState(marker, "", serializeBrowseURL(window.location.href, browseRouteFromSnapshot(snapshot)));
  }, [captureUiSnapshot, catalogMode, discoverMode, favoritesOffset, offset, rememberBrowseScope, searchQuery, sort, view]);

  useEffect(() => {
    if (view !== "home" && view !== "my") return undefined;
    const controller = new AbortController();
    if (view === "home") {
      setContinueLoading(true);
      setContinueError("");
      setRecentLoading(true);
      setRecentError("");
      getContinueTarget({ signal: controller.signal })
        .then((data) => {
          setContinueTarget(data.target || null);
        })
        .catch((reason) => {
          if (reason?.name !== "AbortError") setContinueError(apiErrorText(reason));
        })
        .finally(() => {
          if (!controller.signal.aborted) setContinueLoading(false);
        });
      getShelf({ limit: HOME_RECENT_LIMIT, offset: 0, sort: "added_desc" }, { signal: controller.signal })
        .then((shelfData) => {
          setRecent(shelfData.items || []);
          setRecentTotal(numberValue(shelfData.total, shelfData.items?.length || 0));
        })
        .catch((reason) => {
          if (reason?.name !== "AbortError") setRecentError(apiErrorText(reason));
        })
        .finally(() => {
          if (!controller.signal.aborted) setRecentLoading(false);
        });
    } else {
      setHistoryLoading(true);
      setHistoryError("");
      getReadingHistory({ limit: MY_HISTORY_LIMIT }, { signal: controller.signal })
        .then((historyData) => {
          setHistory(historyData.items || []);
        })
        .catch((reason) => {
          if (reason?.name !== "AbortError") setHistoryError(apiErrorText(reason));
        })
        .finally(() => {
          if (!controller.signal.aborted) setHistoryLoading(false);
        });
    }
    return () => controller.abort();
  }, [dataRevision, view]);

  useEffect(() => {
    if (view !== "library" && view !== "search") return;
    if (view === "library" && !libraryPageStateReady) return;
    const session = catalogRequestRef.current + 1;
    catalogRequestRef.current = session;
    if (view === "search" && !searchQuery.trim()) {
      setCatalog([]);
      setCatalogTotal(0);
      setCatalogDisplayScopeID("");
      setCatalogDisplayOffset(-1);
      setCatalogError("");
      setCatalogSettledKey("");
      setCatalogLoading(false);
      return () => {
        if (catalogRequestRef.current === session) catalogRequestRef.current += 1;
      };
    }
    const scope = catalogCacheScope;
    if (!scope) return;
    const cacheKey = catalogPageCacheKey(scope, offset);

    const routeIsCurrent = () => {
      if (catalogRequestRef.current !== session) return false;
      const active = uiRef.current;
      return Boolean(active
        && active.view === view
        && active.offset === offset
        && active.catalogMode === catalogMode
        && active.sort === sort
        && (view !== "search" || active.searchQuery === searchQuery));
    };
    const requestCatalogPage = (requestOffset: number, signal: AbortSignal) => {
      const query = { limit: PAGE_SIZE, offset: requestOffset, sort, q: view === "search" ? searchQuery : "" };
      return catalogMode === "doujin"
        ? getWorks({ ...query, type: "doujin" }, { signal })
        : catalogMode === "series"
          ? getSeries(query, { signal })
          : getShelf(query, { signal });
    };
    const fetchAndCachePage = (requestOffset: number, signal: AbortSignal) => requestCatalogPage(requestOffset, signal)
      .then((data) => mergeCatalogResponse<CatalogItem>(catalogPageCacheRef.current, scope, requestOffset, data));
    const prefetchAdjacent = (pageData: CatalogPage<CatalogItem>) => {
      for (const nextOffset of adjacentCatalogOffsets(scope, pageData)) {
        const nextKey = catalogPageCacheKey(scope, nextOffset);
        if (catalogPageCacheRef.current.has(nextKey) || catalogPrefetchRef.current.has(nextKey)) continue;
        while (catalogPrefetchRef.current.size >= CATALOG_PREFETCH_LIMIT) {
          const oldest = catalogPrefetchRef.current.entries().next().value as [string, CatalogPrefetchEntry] | undefined;
          if (!oldest) break;
          oldest[1].controller.abort();
          catalogPrefetchRef.current.delete(oldest[0]);
        }
        const prefetchController = new AbortController();
        const prefetchPromise = fetchAndCachePage(nextOffset, prefetchController.signal)
          .then((result) => {
            if (!result.exact) return null;
            warmCatalogCovers(result.page.items);
            return result.page;
          })
          .catch(() => null)
          .finally(() => {
            if (catalogPrefetchRef.current.get(nextKey)?.controller === prefetchController) {
              catalogPrefetchRef.current.delete(nextKey);
            }
          });
        catalogPrefetchRef.current.set(nextKey, {
          scopeKey: catalogCacheScopeID,
          controller: prefetchController,
          promise: prefetchPromise,
        });
      }
    };
    const applyPage = (pageData: CatalogPage<CatalogItem>) => {
      if (!routeIsCurrent()) return;
      setCatalogTotal(pageData.total);
      setCatalogSettledKey(catalogRequestKey);
      if (offset !== pageData.offset) {
        const active = uiRef.current;
        const current = active ? browseRouteFromSnapshot(active) : defaultBrowseRoute(view);
        commitBrowseTransition(sanitizeBrowseRoute({ ...current, offset: pageData.offset }), { replace: true, scroll: false });
        return;
      }
      setCatalog(pageData.items);
      setCatalogDisplayScopeID(catalogCacheScopeID);
      setCatalogDisplayOffset(pageData.offset);
      prefetchAdjacent(pageData);
    };

    setCatalogError("");
    const cached = catalogPageCacheRef.current.get(cacheKey);
    if (cached) {
      setCatalogLoading(false);
      applyPage(cached);
      return () => {
        if (catalogRequestRef.current === session) catalogRequestRef.current += 1;
      };
    }

    setCatalogLoading(true);
    const controller = new AbortController();
    const activeRequest: CatalogActiveRequest = { session, controller };
    catalogActiveRequestRef.current = activeRequest;
    const prefetched = catalogPrefetchRef.current.get(cacheKey);
    const request = prefetched
      ? prefetched.promise.then((pageData) => pageData || fetchAndCachePage(offset, controller.signal).then((result) => result.page))
      : fetchAndCachePage(offset, controller.signal).then((result) => result.page);
    request
      .then(applyPage)
      .catch((reason) => {
        if (catalogRequestRef.current === session && reason?.name !== "AbortError") {
          setCatalogError(apiErrorText(reason));
          setCatalogSettledKey(catalogRequestKey);
        }
      })
      .finally(() => {
        if (catalogActiveRequestRef.current === activeRequest) catalogActiveRequestRef.current = null;
        if (catalogRequestRef.current === session) setCatalogLoading(false);
      });
    return () => {
      controller.abort();
      if (catalogActiveRequestRef.current === activeRequest) catalogActiveRequestRef.current = null;
      if (catalogRequestRef.current === session) catalogRequestRef.current += 1;
    };
  }, [catalogCacheScopeID, catalogMode, catalogRequestKey, catalogRevision, commitBrowseTransition, dataRevision, libraryPageStateReady, offset, searchQuery, sort, view, warmCatalogCovers]);

  useEffect(() => {
    if (view !== "discover") return;
    const controller = new AbortController();
    const session = discoverRequestRef.current + 1;
    const requestPlan = planDiscoverRequest(discoverMode, dataRevision, discoverAuxiliaryRevisionRef.current);
    discoverRequestRef.current = session;
    setDiscoverLoading(true);
    setDiscoverError("");
    setDiscoverErrorKind("batch");
    getDiscover(requestPlan.query, { signal: controller.signal })
      .then((data) => {
        if (discoverRequestRef.current !== session || uiRef.current?.view !== "discover" || discoverModeRef.current !== discoverMode) return;
        setDiscover((current) => mergeDiscoverPayload(current, data));
        if (requestPlan.includeAuxiliary && hasDiscoverAuxiliary(data)) {
          discoverAuxiliaryRevisionRef.current = dataRevision;
        }
      })
      .catch((reason) => {
        if (discoverRequestRef.current === session && reason?.name !== "AbortError") {
          setDiscoverErrorKind("batch");
          setDiscoverError(apiErrorText(reason));
        }
      })
      .finally(() => { if (discoverRequestRef.current === session) setDiscoverLoading(false); });
    return () => {
      controller.abort();
      if (discoverRequestRef.current === session) discoverRequestRef.current += 1;
    };
  }, [dataRevision, discoverMode, discoverRevision, view]);

  useEffect(() => {
    if (view !== "my") return;
    const session = favoritesRequestRef.current + 1;
    favoritesRequestRef.current = session;
    const scope = favoritesCacheScope;
    const scopeKey = favoritesCacheScopeID;
    const requestKey = favoritesRequestKey;
    const requestedOffset = favoritesOffset;
    const routeIsCurrent = () => favoritesRequestRef.current === session
      && uiRef.current?.view === "my"
      && uiRef.current.favoritesOffset === requestedOffset;
    const fetchAndCachePage = (requestOffset: number, signal: AbortSignal) => getShelf(
      { mark: "favorite", limit: FAVORITES_PAGE_SIZE, offset: requestOffset, sort: "added_desc" },
      { signal },
    ).then((data) => mergeFavoritesResponse<ShelfItem>(favoritesPageCacheRef.current, scope, requestOffset, data));
    const prefetchNext = (pageData: FavoritesPage<ShelfItem>) => {
      const nextOffset = nextFavoritesOffset(scope, pageData);
      if (nextOffset === null) return;
      const nextKey = favoritesPageCacheKey(scope, nextOffset);
      if (favoritesPageCacheRef.current.has(nextKey) || favoritesPrefetchRef.current.has(nextKey)) return;
      for (const [key, entry] of favoritesPrefetchRef.current) {
        if (key === nextKey) continue;
        entry.controller.abort();
        favoritesPrefetchRef.current.delete(key);
      }
      const prefetchController = new AbortController();
      const prefetchPromise = fetchAndCachePage(nextOffset, prefetchController.signal)
        .then((result) => result.exact ? result.page : null)
        .catch(() => null)
        .finally(() => {
          if (favoritesPrefetchRef.current.get(nextKey)?.controller === prefetchController) {
            favoritesPrefetchRef.current.delete(nextKey);
          }
        });
      favoritesPrefetchRef.current.set(nextKey, {
        scopeKey,
        controller: prefetchController,
        promise: prefetchPromise,
      });
    };
    const applyPage = (pageData: FavoritesPage<ShelfItem>, shouldPrefetch = true) => {
      if (!routeIsCurrent()) return;
      setFavoritesTotal(pageData.total);
      if (requestedOffset !== pageData.offset) {
        const current = uiRef.current ? browseRouteFromSnapshot(uiRef.current) : defaultBrowseRoute("my");
        commitBrowseTransition(sanitizeBrowseRoute({ ...current, view: "my", offset: pageData.offset }), { replace: true, scroll: false });
        return;
      }
      setFavorites(pageData.items);
      setFavoritesDisplayOffset(pageData.offset);
      setFavoritesSettledKey(requestKey);
      if (shouldPrefetch) prefetchNext(pageData);
    };

    setFavoritesError("");
    const cacheKey = favoritesPageCacheKey(scope, requestedOffset);
    const cached = favoritesPageCacheRef.current.get(cacheKey);
    if (cached) {
      const fresh = isFavoritesPageFresh(cached);
      setFavoritesLoading(false);
      applyPage(cached, fresh);
      if (fresh) {
        return () => {
          if (favoritesRequestRef.current === session) favoritesRequestRef.current += 1;
        };
      }
    }

    setFavoritesLoading(true);
    const controller = new AbortController();
    const activeRequest: FavoritesActiveRequest = { session, controller };
    favoritesActiveRequestRef.current = activeRequest;
    const prefetched = favoritesPrefetchRef.current.get(cacheKey);
    const request = prefetched
      ? prefetched.promise.then((pageData) => pageData || fetchAndCachePage(requestedOffset, controller.signal).then((result) => result.page))
      : fetchAndCachePage(requestedOffset, controller.signal).then((result) => result.page);
    request
      .then(applyPage)
      .catch((reason) => {
        if (routeIsCurrent() && reason?.name !== "AbortError") {
          setFavoritesError(apiErrorText(reason));
          setFavoritesSettledKey(requestKey);
        }
      })
      .finally(() => {
        if (favoritesActiveRequestRef.current === activeRequest) favoritesActiveRequestRef.current = null;
        if (favoritesRequestRef.current === session) setFavoritesLoading(false);
      });
    return () => {
      controller.abort();
      if (favoritesActiveRequestRef.current === activeRequest) favoritesActiveRequestRef.current = null;
      if (favoritesRequestRef.current === session) favoritesRequestRef.current += 1;
    };
  }, [commitBrowseTransition, favoritesCacheScopeID, favoritesOffset, favoritesRequestKey, favoritesRevision, view]);

  useEffect(() => () => {
    if (toastTimerRef.current !== null) window.clearTimeout(toastTimerRef.current);
    randomAbortRef.current?.abort();
    detailLoadAbortRef.current?.abort();
  }, []);

  useEffect(() => {
    randomRequestRef.current += 1;
    randomAbortRef.current?.abort();
    randomAbortRef.current = null;
    setRandomOpening(false);
  }, [discoverMode, view]);

  detailRef.current = detail;
  const detailWarmTarget = detailReaderWarmTarget(detail);
  const detailWarmCandidateID = detailWarmTarget?.candidateID || "";

  useEffect(() => {
    if (!detailWarmTarget || !shouldPrefetchReaderPages()) return undefined;
    const preparationCache = readerPreparationCacheRef.current;
    const pageCache = readerPageCacheRef.current;
    if (!preparationCache || !pageCache) return undefined;
    let active = true;
    let warmedPageURL = "";
    const timer = window.setTimeout(() => {
      void preparationCache.load(detailWarmTarget.candidateID).then((pages) => {
        if (!active) return;
        const index = safeReaderWarmIndex(pages, detailWarmTarget.progress, detailWarmTarget.requestedIndex);
        if (index === null) return;
        warmedPageURL = pageUrl(
          detailWarmTarget.candidateID,
          index,
          pages.page_manifest_id,
          readerImageMax(detailWarmTarget.fitMode, detailWarmTarget.preserveSource),
          detailWarmTarget.preserveSource,
        );
        void pageCache.load(warmedPageURL).catch(() => undefined);
      }).catch(() => undefined);
    }, 160);
    return () => {
      active = false;
      window.clearTimeout(timer);
      preparationCache.invalidate(detailWarmTarget.candidateID);
      if (warmedPageURL && readerImageCacheKeyRef.current !== warmedPageURL) pageCache.invalidate(warmedPageURL);
    };
    // Candidate identity is the lifetime of this single-target warmup. Detail mark
    // updates must not restart it or discard an in-flight request used by the reader.
  }, [detailWarmCandidateID]);

  const continueWarmCandidateID = view === "home" && continueTarget?.item?.can_read
    ? continueTarget.item.candidate_id
    : "";
  const continueWarmProgress = continueTarget?.progress
    || (continueTarget?.item?.progress as ReadingProgress | undefined)
    || null;
  const continueWarmManifestKey = [
    continueWarmCandidateID,
    String(continueWarmProgress?.page_manifest_id || ""),
    String(continueWarmProgress?.manifest_hash || ""),
    String(continueWarmProgress?.index ?? ""),
    readerFitPreference,
  ].join("|");

  useEffect(() => {
    if (!continueWarmCandidateID || !shouldPrefetchReaderPages()) return undefined;
    const preparationCache = readerPreparationCacheRef.current;
    const pageCache = readerPageCacheRef.current;
    if (!preparationCache || !pageCache) return undefined;
    const item = continueTarget?.item;
    if (!item || item.candidate_id !== continueWarmCandidateID) return undefined;
    const preferredFit = String(continueWarmProgress?.reader_fit_mode || "");
    const fitMode = preferredFit === "fit-page" || preferredFit === "fit-width" || preferredFit === "split-wide"
      ? preferredFit
      : readerFitPreference;
    const preserveSource = readerUsesSourceQuality(item.candidate_type);
    let active = true;
    let warmedPageURL = "";
    const timer = window.setTimeout(() => {
      void preparationCache.load(continueWarmCandidateID).then((pages) => {
        if (!active) return;
        const index = safeReaderWarmIndex(pages, continueWarmProgress);
        if (index === null) return;
        warmedPageURL = pageUrl(
          continueWarmCandidateID,
          index,
          pages.page_manifest_id,
          readerImageMax(fitMode, preserveSource),
          preserveSource,
        );
        void pageCache.load(warmedPageURL).catch(() => undefined);
      }).catch(() => undefined);
    }, 900);
    return () => {
      active = false;
      window.clearTimeout(timer);
      preparationCache.invalidate(continueWarmCandidateID);
      if (warmedPageURL && readerImageCacheKeyRef.current !== warmedPageURL) pageCache.invalidate(warmedPageURL);
    };
  }, [continueWarmCandidateID, continueWarmManifestKey]);

  const showToast = useCallback((next: ToastState) => {
    if (toastTimerRef.current !== null) window.clearTimeout(toastTimerRef.current);
    setToast(next);
    toastTimerRef.current = window.setTimeout(() => {
      setToast(null);
      toastTimerRef.current = null;
    }, next.actionLabel && next.onAction ? 16_000 : next.kind === "error" ? 10_000 : 8000);
  }, []);

  useEffect(() => {
    const contextTitle = reader
      ? `阅读 ${cleanTitle(itemTitle(reader.item))}`
      : detail
        ? cleanTitle(itemTitle(detail.kind === "work" ? detail.data.work : detail.data.series))
        : viewLabels[view];
    document.title = `${contextTitle} · bmanga`;
  }, [detail, reader, view]);

  useEffect(() => {
    const previous = previousViewRef.current;
    previousViewRef.current = view;
    if (previous === view) return undefined;
    const frame = window.requestAnimationFrame(() => mainRef.current?.focus({ preventScroll: true }));
    return () => window.cancelAnimationFrame(frame);
  }, [view]);

  const detailOpen = Boolean(detail);
  const detailLayerOpen = detailLoading || detailOpen;
  useEffect(() => {
    if (!detailLayerOpen) return undefined;
    return () => {
      if (detailTriggerRef.current?.isConnected) detailTriggerRef.current.focus();
    };
  }, [detailLayerOpen]);

  useEffect(() => {
    if (!detailLoading || detail) return undefined;
    const timer = window.setTimeout(() => detailLoadingCloseRef.current?.focus(), 0);
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        closeDetail();
      } else if (event.key === "Tab") {
        event.preventDefault();
        detailLoadingCloseRef.current?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener("keydown", onKey);
    };
  }, [closeDetail, detail, detailLoading]);

  useEffect(() => {
    if (!detail) return undefined;
    const timer = window.setTimeout(() => detailCloseRef.current?.focus(), 0);
    const onKey = (event: KeyboardEvent) => {
      if (reader) return;
      if (event.key === "Escape" && !detailBusy) {
        event.preventDefault();
        closeDetail();
        return;
      }
      if (event.key !== "Tab") return;
      const focusable = Array.from(detailPanelRef.current?.querySelectorAll<HTMLElement>(
        'button:not([disabled]), [href], input:not([disabled]), textarea:not([disabled]), select:not([disabled]), summary, [tabindex]:not([tabindex="-1"])',
      ) || []).filter((element) => {
        const style = getComputedStyle(element);
        return style.display !== "none" && style.visibility !== "hidden" && !element.closest("[inert]");
      });
      const first = focusable.at(0);
      const last = focusable.at(-1);
      if (!first || !last) return;
      if (!detailPanelRef.current?.contains(document.activeElement)) {
        event.preventDefault();
        first.focus();
      } else if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener("keydown", onKey);
    };
  }, [closeDetail, detail, detailBusy, reader]);

  const readerOpen = Boolean(reader);
  const readerLayerOpen = readerLoading || readerOpen;
  const readerCalibrationOpen = Boolean(reader?.calibration);
  const readerEndingOpen = Boolean(reader?.ending);
  useEffect(() => {
    if (!readerLayerOpen) return undefined;
    return () => {
      if (readerTriggerRef.current?.isConnected) readerTriggerRef.current.focus();
    };
  }, [readerLayerOpen]);

  useEffect(() => {
    if (!readerOpen) return undefined;
    const timer = window.setTimeout(() => {
      if (readerCalibrationOpen) {
        const primary = readerCalibrationPrimaryRef.current;
        if (primary && !primary.disabled) primary.focus();
        else readerCalibrationRef.current?.focus();
      }
      else readerStageRef.current?.focus({ preventScroll: true });
    }, 0);
    return () => window.clearTimeout(timer);
  }, [readerCalibrationOpen, readerOpen]);

  useEffect(() => {
    const previous = previousReaderEndingRef.current;
    previousReaderEndingRef.current = readerEndingOpen;
    if (!readerOpen || previous === readerEndingOpen) return undefined;
    const frame = window.requestAnimationFrame(() => {
      if (readerEndingOpen) readerEndingRef.current?.focus({ preventScroll: true });
      else if (!readerCalibrationOpen) readerStageRef.current?.focus({ preventScroll: true });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [readerCalibrationOpen, readerEndingOpen, readerOpen]);

  useEffect(() => {
    if (!readerLoading || reader) return undefined;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        closeReader();
      } else if (event.key === "Tab") {
        event.preventDefault();
        readerLoadingCloseRef.current?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [closeReader, reader, readerLoading]);

  useEffect(() => {
    if (!readerOpen) return undefined;
    const onKey = (event: KeyboardEvent) => {
      const current = uiRef.current?.reader;
      if (!current) return;
      if (event.key === "Escape") {
        event.preventDefault();
        if (current.calibrationSaving) return;
        closeReader();
        return;
      }
      if (event.key === "Tab") {
        if (!current.chromeVisible && !current.calibration) {
          event.preventDefault();
          setReader((current) => current ? { ...current, chromeVisible: true } : current);
          window.requestAnimationFrame(() => readerCloseRef.current?.focus());
          return;
        }
        const scope = current.calibration ? readerCalibrationRef.current : readerDialogRef.current;
        const focusable = Array.from(scope?.querySelectorAll<HTMLElement>(
          'button:not([disabled]), [href], input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])',
        ) || []).filter((element) => {
          const style = getComputedStyle(element);
          return style.display !== "none" && style.visibility !== "hidden" && !element.closest("[inert]");
        });
        const first = focusable.at(0);
        const last = focusable.at(-1);
        if (!first || !last) {
          if (current.calibration) {
            event.preventDefault();
            readerCalibrationRef.current?.focus();
          }
          return;
        }
        if (!scope?.contains(document.activeElement)) {
          event.preventDefault();
          first.focus();
        } else if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first.focus();
        }
        return;
      }
      if (current.calibration) return;
      const target = event.target;
      if (target instanceof Element && (target.closest("input, textarea, select") || (target instanceof HTMLElement && target.isContentEditable))) return;
      const activatesControl = target instanceof Element && Boolean(target.closest("button, a, summary, [role='button']"));
      if (event.key === "ArrowLeft") {
        event.preventDefault();
        readerMoveRef.current(-1);
      } else if (event.key === "ArrowRight") {
        event.preventDefault();
        readerMoveRef.current(1);
      } else if (event.key === "ArrowDown" || event.key === "PageDown" || (event.key === " " && !activatesControl)) {
        event.preventDefault();
        readerViewportRef.current(1);
      } else if (event.key === "ArrowUp" || event.key === "PageUp") {
        event.preventDefault();
        readerViewportRef.current(-1);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [closeReader, readerOpen]);

  const activateView = useCallback((next: View) => {
    const current = uiRef.current;
    if (detailHasUnsavedNote(current?.detail || null, current?.noteDraft || "")) {
      setToast({ message: "私人备注尚未保存。请先保存，或在备注区撤销修改后再离开。" });
      return;
    }
    if (current?.view === next && !current.detail && !current.reader) {
      if (next === "search") window.setTimeout(() => searchRef.current?.focus(), 80);
      return;
    }
    const remembered = browseScopesRef.current?.[next];
    const persistentLibrary = libraryPageScopesRef.current;
    let route = remembered || defaultBrowseRoute(next);
    if (next === "library" && persistentLibrary) {
      const mode = remembered?.catalogMode || "all";
      const persistentOffset = libraryPageScopeOffset(persistentLibrary, mode, persistentLibrary.sort);
      if (!remembered) {
        route = sanitizeBrowseRoute({
          view: "library",
          catalogMode: mode,
          sort: persistentLibrary.sort,
          offset: persistentOffset,
        });
      } else if (remembered.sort === persistentLibrary.sort && remembered.offset === 0 && persistentOffset > 0) {
        route = sanitizeBrowseRoute({ ...remembered, offset: persistentOffset });
      }
    }
    commitBrowseTransition(route);
    setCatalogError("");
    if (next === "search") window.setTimeout(() => searchRef.current?.focus(), 80);
  }, [commitBrowseTransition]);

  useEffect(() => {
    const onShortcut = (event: KeyboardEvent) => {
      if (!(event.ctrlKey || event.metaKey) || event.altKey || event.key.toLowerCase() !== "k") return;
      if (detailBusy || reader) return;
      event.preventDefault();
      activateView("search");
    };
    window.addEventListener("keydown", onShortcut);
    return () => window.removeEventListener("keydown", onShortcut);
  }, [activateView, detailBusy, reader]);

  const submitSearch = (event: FormEvent) => {
    event.preventDefault();
    const query = searchDraft.trim();
    const repeatsCurrentSearch = uiRef.current?.view === "search" && query === searchQuery;
    const base = uiRef.current?.view === "search"
      ? browseRouteFromSnapshot(uiRef.current)
      : browseScopesRef.current?.search || defaultBrowseRoute("search");
    commitBrowseTransition(sanitizeBrowseRoute({ ...base, view: "search", searchQuery: query, offset: 0 }), {
      searchDraft: query,
      previousSnapshot: uiRef.current?.view === "search" ? { searchDraft: searchQuery } : undefined,
    });
    if (repeatsCurrentSearch && query) retryCatalogPage();
  };

  const loadDetailForEntry = useCallback(async (item: CatalogItem, entry: number) => {
    const id = itemID(item);
    if (!id) return;
    detailLoadAbortRef.current?.abort();
    const controller = new AbortController();
    detailLoadAbortRef.current = controller;
    let timedOut = false;
    const timeout = window.setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, 20_000);
    const session = detailSessionRef.current + 1;
    detailSessionRef.current = session;
    setDetailError("");
    setReaderRetryIntent(null);
    setPersonalMarkStatus("");
    try {
      if (isSeries(item)) {
        const [data, progressData] = await Promise.all([
          getSeriesDetail(id, { signal: controller.signal }),
          getSeriesProgress(id, { signal: controller.signal }).catch((reason) => {
            if ((reason as { name?: string })?.name === "AbortError") throw reason;
            return { group_id: id, progress: null };
          }),
        ]);
        if (detailSessionRef.current !== session || (entry >= 0 && historyCurrentRef.current !== entry)) return;
        seriesDetailCacheRef.current.set(id, Promise.resolve(data));
        seriesDetailResolvedRef.current.set(id, data);
        setDetailIntent(null);
        setNoteDraft(String(data.mark?.notes || ""));
        setDetail({ kind: "series", data, progress: progressData.progress });
      } else {
        const data = await getWork(id, { signal: controller.signal });
        if (detailSessionRef.current !== session || (entry >= 0 && historyCurrentRef.current !== entry)) return;
        setDetailIntent(null);
        setNoteDraft(String(data.mark?.notes || ""));
        setDetail({ kind: "work", data });
      }
    } catch (reason) {
      if (controller.signal.aborted && !timedOut) return;
      if (detailSessionRef.current === session && (entry < 0 || historyCurrentRef.current === entry)) {
        if (entry >= 0) retireFailedHistoryEntry(entry, "detail");
        setDetailIntent(null);
        setDetailError(timedOut ? "作品详情读取超时，请检查网络后重试。" : apiErrorText(reason));
        closeDetail();
      }
    } finally {
      window.clearTimeout(timeout);
      if (detailLoadAbortRef.current === controller) detailLoadAbortRef.current = null;
      if (detailSessionRef.current === session && (entry < 0 || historyCurrentRef.current === entry)) setDetailLoading(false);
    }
  }, [closeDetail, retireFailedHistoryEntry]);
  resumeDetailRef.current = (item, entry) => { void loadDetailForEntry(item, entry); };

  const openDetail = useCallback((item: CatalogItem) => {
    const id = itemID(item);
    if (!id) return;
    if (favoriteSavingRef.current.has(id)) {
      showToast({ message: "收藏状态正在确认，请稍候再打开详情。" });
      return;
    }
    if (document.activeElement instanceof HTMLElement) detailTriggerRef.current = document.activeElement;
    const entry = pushUiSnapshot(`detail:${isSeries(item) ? "series" : "work"}:${id}`, {
      detail: null,
      detailLoading: true,
      detailIntent: item,
      reader: null,
      readerLoading: false,
      readerIntent: null,
      scrollY: window.scrollY,
      detailScrollTop: 0,
    });
    if (entry === null) return;
    void loadDetailForEntry(item, entry);
  }, [loadDetailForEntry, pushUiSnapshot, showToast]);

  const openRandomDiscoverWork = useCallback(async () => {
    if (randomOpening) return;
    const mode = discoverMode;
    const request = randomRequestRef.current + 1;
    randomRequestRef.current = request;
    randomAbortRef.current?.abort();
    const controller = new AbortController();
    randomAbortRef.current = controller;
    setRandomOpening(true);
    setDiscoverError("");
    setDiscoverErrorKind("random");
    try {
      const response = await getRandomWork({ randomMode: mode }, { signal: controller.signal });
      if (randomRequestRef.current !== request || uiRef.current?.view !== "discover" || discoverModeRef.current !== mode) return;
      if (!response.item || !itemID(response.item)) throw new Error("当前条件下没有可抽取的作品。");
      openDetail(response.item);
    } catch (reason) {
      if (randomRequestRef.current === request && (reason as { name?: string })?.name !== "AbortError") {
        setDiscoverErrorKind("random");
        setDiscoverError(apiErrorText(reason));
      }
    } finally {
      if (randomRequestRef.current === request) {
        randomAbortRef.current = null;
        setRandomOpening(false);
      }
    }
  }, [discoverMode, openDetail, randomOpening]);

  const applyFavoriteState = useCallback((item: CatalogItem, favorite: boolean, mark?: UserMark) => {
    const id = itemID(item);
    if (!id) return;
    const patchEntry = (entry: CatalogItem): CatalogItem => itemID(entry) === id ? { ...entry, user_favorite: favorite } : entry;
    catalogPageCacheRef.current.updateAll((page) => ({ ...page, items: page.items.map(patchEntry) }));
    invalidateFavoritesPageCache();
    setRecent((current) => current.map((entry) => patchEntry(entry) as ShelfItem));
    setHistory((current) => current.map((entry) => patchEntry(entry) as ReadingHistoryItem));
    setCatalog((current) => current.map(patchEntry));
    setDiscover((current) => current ? {
      ...current,
      random_items: current.random_items.map((entry) => patchEntry(entry) as WorkSummary),
      history: current.history.map((entry) => patchEntry(entry) as ReadingHistoryItem),
    } : null);
    setFavorites((current) => {
      const exists = current.some((entry) => itemID(entry) === id);
      if (!favorite) return current.filter((entry) => itemID(entry) !== id);
      const updated = current.map((entry) => patchEntry(entry) as ShelfItem);
      return exists ? updated : [patchEntry(item) as ShelfItem, ...updated].slice(0, FAVORITES_PAGE_SIZE);
    });
    setDetail((current) => {
      if (!current) return current;
      if (current.kind === "work" && current.data.work.candidate_id === id) {
        return {
          ...current,
          data: {
            ...current.data,
            work: { ...current.data.work, user_favorite: favorite },
            mark: mark || (current.data.mark ? { ...current.data.mark, favorite } : current.data.mark),
          },
        };
      }
      if (current.kind === "series" && current.data.series.group_id === id) {
        return {
          ...current,
          data: {
            ...current.data,
            series: { ...current.data.series, user_favorite: favorite },
            mark: mark || (current.data.mark ? { ...current.data.mark, favorite } : current.data.mark),
          },
        };
      }
      return current;
    });
    setHeroDetail((current) => current?.work.candidate_id === id ? {
      ...current,
      work: { ...current.work, user_favorite: favorite },
      mark: mark || (current.mark ? { ...current.mark, favorite } : current.mark),
    } : current);
  }, [invalidateFavoritesPageCache]);

  const changeFavorite = useCallback(async (
    item: CatalogItem,
    favorite: boolean,
    previous = favoriteFor(item),
    offerUndo = true,
  ) => {
    const id = itemID(item);
    if (!id || favoriteSavingRef.current.has(id) || favorite === previous) return;
    const targetType: TargetType = isSeries(item) ? "series" : "work";
    const queuedPayload = queuePendingUserMark({
      target_type: targetType,
      target_id: id,
      favorite,
      client_updated_at: new Date().toISOString(),
    });
    favoriteSavingRef.current.add(id);
    setFavoriteSavingIDs((current) => new Set(current).add(id));
    applyFavoriteState(item, favorite);
    setFavoritesTotal((current) => Math.max(0, current + (favorite ? 1 : -1)));
    try {
      const response = await saveUserMark(queuedPayload);
      if (!response.mark) throw new Error("收藏保存后未返回可确认的状态。");
      acknowledgePendingUserMark(queuedPayload);
      const actualFavorite = Boolean(response.mark.favorite);
      const favoriteRejected = Boolean(response.rejected_fields?.includes("favorite"));
      applyFavoriteState(item, actualFavorite, response.mark);
      if (actualFavorite !== favorite) {
        setFavoritesTotal((current) => Math.max(0, current + (actualFavorite ? 1 : 0) - (favorite ? 1 : 0)));
      }
      const title = cleanTitle(itemTitle(item));
      showToast({
        kind: favoriteRejected ? "error" : "success",
        message: favoriteRejected
          ? `《${title}》在其他页面已有更新，服务器保留了较新的收藏状态。`
          : actualFavorite ? `已收藏《${title}》` : `已将《${title}》移出收藏`,
        actionLabel: !favoriteRejected && offerUndo && actualFavorite === favorite ? "撤销" : undefined,
        onAction: !favoriteRejected && offerUndo && actualFavorite === favorite ? () => { void changeFavorite(item, previous, favorite, false); } : undefined,
      });
    } catch (reason) {
      try {
        const reconciled = await getUserMark(targetType, id);
        if (!reconciled.mark) throw new Error("收藏状态暂时无法读取。");
        const actualFavorite = Boolean(reconciled.mark.favorite);
        if (actualFavorite === favorite) {
          acknowledgePendingUserMark(queuedPayload);
          applyFavoriteState(item, actualFavorite, reconciled.mark);
          showToast({ kind: "success", message: "保存响应曾中断，但收藏状态已经重新核对。" });
        } else if (hasPendingUserMark(queuedPayload)) {
          showToast({ kind: "error", message: `${apiErrorText(reason)} 收藏操作已在本机暂存，联网后会自动重试。` });
        } else {
          applyFavoriteState(item, actualFavorite, reconciled.mark);
          setFavoritesTotal((current) => Math.max(0, current + (actualFavorite ? 1 : 0) - (favorite ? 1 : 0)));
          showToast({ kind: "error", message: `${apiErrorText(reason)} 浏览器无法暂存这次操作，已恢复服务器状态。` });
        }
      } catch {
        if (hasPendingUserMark(queuedPayload)) {
          showToast({ kind: "error", message: `${apiErrorText(reason)} 收藏操作已在本机暂存，联网后会自动重试。` });
        } else {
          applyFavoriteState(item, previous);
          setFavoritesTotal((current) => Math.max(0, current + (previous ? 1 : 0) - (favorite ? 1 : 0)));
          showToast({ kind: "error", message: `${apiErrorText(reason)} 浏览器无法暂存这次操作，页面已恢复原显示。` });
        }
      }
    } finally {
      setFavoritesRevision((current) => current + 1);
      favoriteSavingRef.current.delete(id);
      setFavoriteSavingIDs((current) => {
        const next = new Set(current);
        next.delete(id);
        return next;
      });
    }
  }, [applyFavoriteState, showToast]);

  const applyMarkToDetailSession = useCallback((
    targetType: TargetType,
    targetID: string,
    mark: UserMark,
    session: number,
    syncDraft: boolean,
  ) => {
    if (detailSessionRef.current !== session || !detailMatchesTarget(detailRef.current, targetType, targetID)) return false;
    setDetail((current) => {
      if (!current || current.kind !== targetType) return current;
      const currentID = current.kind === "work" ? current.data.work.candidate_id : current.data.series.group_id;
      return currentID === targetID ? { ...current, data: { ...current.data, mark } } as DetailState : current;
    });
    if (syncDraft) setNoteDraft(String(mark.notes || ""));
    return true;
  }, []);

  const clearWorkProgressState = useCallback((candidateID: string) => {
    const clearProgress = <T extends CatalogItem,>(item: T): T => item.candidate_id === candidateID
      ? {
        ...item,
        progress: undefined,
        progress_index: undefined,
        progress_count: undefined,
        progress_percent: undefined,
        progress_completed: false,
        progress_status: "normal",
        progress_last_read_at: undefined,
        progress_updated_at: undefined,
      } as T
      : item;
    setPendingProgressTotal(discardPendingProgressForCandidate(candidateID));
    invalidateCatalogPageCache();
    invalidateFavoritesPageCache();
    setRecent((current) => current.filter((item) => item.candidate_id !== candidateID));
    setHistory((current) => current.filter((item) => item.candidate_id !== candidateID));
    setCatalog((current) => current.map(clearProgress));
    setFavorites((current) => current.map(clearProgress));
    setDiscover((current) => current ? {
      ...current,
      random_items: current.random_items.map(clearProgress),
      history: current.history.filter((item) => item.candidate_id !== candidateID),
    } : current);
    setDetail((current) => {
      if (!current) return current;
      if (current.kind === "work") {
        return current.data.work.candidate_id === candidateID
          ? { ...current, data: { ...current.data, work: clearProgress(current.data.work) } }
          : current;
      }
      const items = current.data.items.map(clearProgress);
      return {
        ...current,
        data: { ...current.data, items },
        progress: current.progress?.candidate_id === candidateID ? null : current.progress,
      };
    });
    setHeroDetail((current) => current?.work.candidate_id === candidateID
      ? { ...current, work: clearProgress(current.work) }
      : current);
  }, [invalidateCatalogPageCache, invalidateFavoritesPageCache]);

  const saveDetailPersonalMark = useCallback(async (payload: UserMarkSavePayload, field: PersonalMarkField) => {
    const currentDetail = detailRef.current;
    if (!currentDetail || personalMarkSavingField !== null) return;
    const targetType: TargetType = currentDetail.kind;
    const targetID = currentDetail.kind === "work" ? currentDetail.data.work.candidate_id : currentDetail.data.series.group_id;
    if (payload.target_type !== targetType || payload.target_id !== targetID) return;
    const session = detailSessionRef.current;
    const queuedPayload = queuePendingUserMark(payload);
    setPersonalMarkSavingField(field);
    setPersonalMarkStatus("");
    try {
      const response = await saveUserMark(queuedPayload);
      if (!response.mark) throw new Error("个人标记保存后未返回可确认的状态。");
      acknowledgePendingUserMark(queuedPayload);
      applyMarkToDetailSession(targetType, targetID, response.mark, session, false);
      setHeroDetail((current) => current?.work.candidate_id === targetID ? { ...current, mark: response.mark } : current);
      const rejected = Boolean(response.rejected_fields?.includes(field))
        || (field === "read_status" && response.read_status_stored === false);
      if (targetType === "work" && queuedPayload.read_status === "unread" && response.reset_stored === true) {
        clearWorkProgressState(targetID);
      }
      invalidateCatalogPageCache();
      setDataRevision((current) => current + 1);
      setPersonalMarkStatus(rejected ? "服务器保留了其他页面中较新的值。" : "已保存。");
    } catch (reason) {
      let reconciled = false;
      try {
        const latest = await getUserMark(targetType, targetID);
        if (!latest.mark) throw new Error("个人标记状态暂时无法读取。");
        applyMarkToDetailSession(targetType, targetID, latest.mark, session, false);
        setHeroDetail((current) => current?.work.candidate_id === targetID ? { ...current, mark: latest.mark } : current);
        if (targetType === "work" && queuedPayload.read_status === "unread" && latest.mark.read_status === "unread") {
          const progress = await getProgress(targetID);
          if (!progress.progress) clearWorkProgressState(targetID);
        }
        reconciled = userMarkMatchesPayload(latest.mark, queuedPayload, field);
        if (reconciled) acknowledgePendingUserMark(queuedPayload);
      } catch {
        // The durable local queue remains the fallback when reconciliation is unavailable.
      }
      if (reconciled) {
        invalidateCatalogPageCache();
        setDataRevision((current) => current + 1);
        setPersonalMarkStatus("保存响应曾中断，但服务器状态已经重新核对。");
      } else if (hasPendingUserMark(queuedPayload)) {
        setPersonalMarkStatus(`保存失败，已在本机暂存，联网后会自动重试：${apiErrorText(reason)}`);
      } else {
        setPersonalMarkStatus(`保存失败，且浏览器无法暂存这次操作：${apiErrorText(reason)}`);
      }
    } finally {
      setPersonalMarkSavingField(null);
    }
  }, [applyMarkToDetailSession, clearWorkProgressState, invalidateCatalogPageCache, personalMarkSavingField]);

  const savePersonalNote = useCallback(async () => {
    if (!detail || noteSaving) return;
    const targetType: TargetType = detail.kind;
    const targetID = detail.kind === "work" ? detail.data.work.candidate_id : detail.data.series.group_id;
    const noteValue = noteDraft.slice(0, 4000);
    const session = detailSessionRef.current;
    const queuedPayload = queuePendingUserMark({
      target_type: targetType,
      target_id: targetID,
      notes: noteValue,
      client_updated_at: new Date().toISOString(),
    });
    setNoteSaving(true);
    try {
      const response = await saveUserMark(queuedPayload);
      if (!response.mark) throw new Error("备注保存后未返回可确认的状态。");
      acknowledgePendingUserMark(queuedPayload);
      const sameSession = detailSessionRef.current === session;
      applyMarkToDetailSession(targetType, targetID, response.mark, session, false);
      if (!sameSession && detailMatchesTarget(detailRef.current, targetType, targetID)) {
        const reopenedSession = detailSessionRef.current;
        try {
          const latest = await getUserMark(targetType, targetID);
          if (!latest.mark) throw new Error("备注状态暂时无法读取。");
          applyMarkToDetailSession(targetType, targetID, latest.mark, reopenedSession, true);
        } catch (reason) {
          showToast({ kind: "error", message: `备注已保存，但重新打开的详情暂时无法刷新：${apiErrorText(reason)}` });
          return;
        }
      }
      setHeroDetail((current) => current?.work.candidate_id === targetID ? { ...current, mark: response.mark } : current);
      const noteRejected = Boolean(response.rejected_fields?.includes("notes"));
      showToast(noteRejected
        ? { kind: "error", message: sameSession
          ? "其他页面已经保存了更新的私人备注；这次较早的写入已忽略，当前草稿仍保留。"
          : "其他页面已经保存了更新的私人备注；这次较早的写入已忽略，详情已显示服务器版本。" }
        : { message: noteValue.trim() ? "私人备注已保存。" : "私人备注已清空。" });
    } catch (reason) {
      try {
        const reconciled = await getUserMark(targetType, targetID);
        if (!reconciled.mark) throw new Error("备注状态暂时无法读取。");
        if (String(reconciled.mark.notes || "") === noteValue) {
          acknowledgePendingUserMark(queuedPayload);
          const currentSession = detailSessionRef.current;
          applyMarkToDetailSession(targetType, targetID, reconciled.mark, currentSession, currentSession !== session);
          setHeroDetail((current) => current?.work.candidate_id === targetID ? { ...current, mark: reconciled.mark } : current);
          showToast({ message: "保存响应曾中断，但私人备注已经重新核对。" });
        } else if (hasPendingUserMark(queuedPayload)) {
          showToast({ kind: "error", message: `${apiErrorText(reason)} 备注已在本机暂存，联网后会自动重试。` });
        } else {
          const currentSession = detailSessionRef.current;
          applyMarkToDetailSession(targetType, targetID, reconciled.mark, currentSession, true);
          setHeroDetail((current) => current?.work.candidate_id === targetID ? { ...current, mark: reconciled.mark } : current);
          showToast({ kind: "error", message: `${apiErrorText(reason)} 浏览器无法暂存草稿，已恢复服务器备注。` });
        }
      } catch {
        showToast({ kind: "error", message: hasPendingUserMark(queuedPayload)
          ? `${apiErrorText(reason)} 备注已在本机暂存，联网后会自动重试。`
          : `${apiErrorText(reason)} 浏览器无法暂存草稿，也暂时无法确认服务器状态。` });
      }
    } finally {
      setNoteSaving(false);
    }
  }, [applyMarkToDetailSession, detail, noteDraft, noteSaving, showToast]);

  const flushPendingMarks = useCallback(async () => {
    const result = await flushPendingUserMarks((payload) => saveUserMark(payload));
    if (!result.sent.length) return;
    for (const sent of result.sent) {
      const { payload, response } = sent;
      if (!response.mark) continue;
      applyMarkToDetailSession(payload.target_type, payload.target_id, response.mark, detailSessionRef.current, false);
      setHeroDetail((current) => current?.work.candidate_id === payload.target_id ? { ...current, mark: response.mark } : current);
      if (payload.target_type === "work" && payload.read_status === "unread" && response.mark.read_status === "unread") {
        if (response.reset_stored === true) {
          clearWorkProgressState(payload.target_id);
        } else {
          try {
            const progress = await getProgress(payload.target_id);
            if (!progress.progress) clearWorkProgressState(payload.target_id);
          } catch {
            // A later refresh will reconcile progress if this read is temporarily unavailable.
          }
        }
      }
    }
    invalidateCatalogPageCache();
    setDataRevision((current) => current + 1);
    setFavoritesRevision((current) => current + 1);
  }, [applyMarkToDetailSession, clearWorkProgressState, invalidateCatalogPageCache]);

  const loadReaderForEntry = useCallback(async (
    item: WorkSummary,
    requestedIndex: number | undefined,
    entry: number,
    context: ReaderContext = {},
  ) => {
    cancelReaderRequests({ preservePageCache: true });
    const session = readerSessionRef.current + 1;
    readerSessionRef.current = session;
    const controller = new AbortController();
    readerLoadAbortRef.current = controller;
    let timedOut = false;
    const timeout = window.setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, 30_000);
    setDetailError("");
    setReaderRetryIntent(null);
    try {
      const readBundle = () => Promise.all([
        readerPreparationRequest(
          "书页清单",
          readerPreparationCacheRef.current
            ? waitForReaderPreparation(readerPreparationCacheRef.current.load(item.candidate_id), controller.signal)
            : getPages(item.candidate_id, { signal: controller.signal }),
        ),
        readerPreparationRequest("阅读进度", getProgress(item.candidate_id, { signal: controller.signal })),
      ]);
      let [pages, progressData] = await readBundle();
      if (!sameManifest(pages, progressData)) {
        readerPreparationCacheRef.current?.invalidate(item.candidate_id);
        [pages, progressData] = await readBundle();
      }
      if (readerSessionRef.current !== session || (entry >= 0 && historyCurrentRef.current !== entry)) return;
      if (!pages.readable || pages.count < 1) throw new Error("这部作品暂时没有可阅读页面。");
      if (!sameManifest(pages, progressData)) throw new Error("书页清单正在更新，请稍后重新打开。");
      const saved = progressData.progress;
      const progressStatus = String(saved?.progress_status || "normal").trim() || "normal";
      const savedTime = progressTimestamp(saved?.updated_at || saved?.last_read_at);
      const queued = pendingProgressEntries()
        .filter((pending) => pending.payload.candidate_id === item.candidate_id)
        .sort((left, right) => {
          const timeDifference = progressTimestamp(right.payload.updated_at || right.queuedAt) - progressTimestamp(left.payload.updated_at || left.queuedAt);
          return timeDifference || right.sequence - left.sequence;
        });
      const pendingWinner = queued[0];
      const pendingWinnerTime = progressTimestamp(pendingWinner?.payload.updated_at || pendingWinner?.queuedAt);
      const pendingWins = Boolean(pendingWinner && (!saved || pendingWinnerTime > savedTime));
      let cleanedPendingCount: number | undefined;
      for (const pending of queued) {
        if (!pendingWins || pending.entryID !== pendingWinner?.entryID) {
          cleanedPendingCount = acknowledgePendingProgress(pending.entryID);
        }
      }
      if (cleanedPendingCount !== undefined) setPendingProgressTotal(cleanedPendingCount);
      const pendingMatches = Boolean(pendingWins && pendingWinner && sameManifest(pendingWinner.payload, pages));
      const effective = pendingMatches ? pendingWinner?.payload : saved;
      const stalePending = pendingWins && pendingWinner && !pendingMatches
        ? { entryID: pendingWinner.entryID }
        : undefined;
      const tentative = stalePending && pendingWinner ? pendingWinner.payload : effective;
      const start = Math.max(0, Math.min(pages.count - 1, requestedIndex ?? numberValue(tentative?.index)));
      const calibration = stalePending && pendingWinner
        ? { status: "local_pending_stale", oldIndex: numberValue(pendingWinner.payload.index), oldCount: numberValue(pendingWinner.payload.count) }
        : saved && progressStatus !== "normal" && !pendingMatches
        ? { status: progressStatus, oldIndex: numberValue(saved.index), oldCount: numberValue(saved.count) }
        : null;
      const requestedFit = String(tentative?.reader_fit_mode || "");
      const fitMode: ActiveReaderFitMode = requestedFit === "fit-width" || requestedFit === "fit-page" || requestedFit === "split-wide"
        ? requestedFit
        : storedReaderFit(saved);
      const restoringSamePage = requestedIndex === undefined || requestedIndex === numberValue(tentative?.index);
      const stageScrollTop = fitMode === "fit-width" && restoringSamePage ? Math.max(0, numberValue(tentative?.stage_scroll_top)) : 0;
      const stageScrollLeft = fitMode === "fit-width" && restoringSamePage ? Math.max(0, numberValue(tentative?.stage_scroll_left)) : 0;
      const splitPanel: 0 | 1 = fitMode === "split-wide" && restoringSamePage && numberValue(tentative?.reader_split_panel) >= 1 ? 1 : 0;
      setReaderIntent(null);
      setReader({
        item,
        pages,
        index: start,
        requestedIndex: start,
        savedIndex: calibration ? -1 : numberValue(saved?.index, -1),
        imageURL: "",
        pageRevision: 0,
        imageLoading: true,
        error: "",
        fitMode,
        splitPanel,
        requestedSplitPanel: splitPanel,
        imageNaturalWidth: 0,
        imageNaturalHeight: 0,
        stageScrollTop,
        stageScrollLeft,
        restoreScroll: true,
        ending: false,
        chromeVisible: true,
        seriesID: context.seriesID,
        nextItem: context.nextItem,
        stalePending,
        calibration,
      });
    } catch (reason) {
      if (controller.signal.aborted && !timedOut) return;
      if (readerSessionRef.current === session && (entry < 0 || historyCurrentRef.current === entry)) {
        if (entry >= 0) retireFailedHistoryEntry(entry, "reader");
        setReaderIntent(null);
        setReaderRetryIntent({ item, requestedIndex, ...context });
        setDetailError(timedOut ? "准备书页超时，请检查网络后重试。" : apiErrorText(reason));
        closeReader();
      }
    } finally {
      window.clearTimeout(timeout);
      if (readerLoadAbortRef.current === controller) readerLoadAbortRef.current = null;
      if (readerSessionRef.current === session && (entry < 0 || historyCurrentRef.current === entry)) setReaderLoading(false);
    }
  }, [cancelReaderRequests, closeReader, retireFailedHistoryEntry]);
  resumeReaderRef.current = (item, requestedIndex, entry, context = {}) => { void loadReaderForEntry(item, requestedIndex, entry, context); };

  const openReader = useCallback((item: WorkSummary, requestedIndex?: number, context: ReaderContext = {}) => {
    if (document.activeElement instanceof HTMLElement) readerTriggerRef.current = document.activeElement;
    const intent: ReaderIntent = { item, requestedIndex, ...context };
    const entry = pushUiSnapshot(`reader:${item.candidate_id}`, {
      reader: null,
      readerLoading: true,
      readerIntent: intent,
      scrollY: window.scrollY,
    });
    if (entry === null) return;
    void loadReaderForEntry(item, requestedIndex, entry, context);
  }, [loadReaderForEntry, pushUiSnapshot]);

  const applyProgressState = useCallback((
    candidateID: string,
    progress: ReadingProgress,
    sourceItem?: WorkSummary,
    sourceNextItem?: WorkSummary,
    sourceSeries?: ContinueTarget["series"],
  ) => {
    const patchItem = <T extends CatalogItem,>(item: T): T => item.candidate_id === candidateID
      ? patchCatalogItemProgress(item, candidateID, progress)
      : item;
    const affectedSeries = (item: CatalogItem) => isSeries(item) && (
      item.selected_candidate_id === candidateID
      || Boolean(sourceSeries?.group_id && item.group_id === sourceSeries.group_id)
    );
    const dirtyCatalogKeys: string[] = [];
    catalogPageCacheRef.current.updateAll((page, key) => {
      if (page.items.some(affectedSeries)) dirtyCatalogKeys.push(key);
      return { ...page, items: page.items.map((item) => patchItem(item)) };
    });
    const dirtyFavoriteKeys: string[] = [];
    favoritesPageCacheRef.current.updateAll((page, key) => {
      if (page.items.some(affectedSeries)) dirtyFavoriteKeys.push(key);
      return { ...page, items: page.items.map((item) => patchItem(item)) };
    });
    for (const key of dirtyCatalogKeys) catalogPageCacheRef.current.delete(key);
    for (const key of dirtyFavoriteKeys) favoritesPageCacheRef.current.delete(key);
    if (dirtyCatalogKeys.length) readerCatalogRefreshNeededRef.current = true;
    if (dirtyFavoriteKeys.length) readerFavoritesRefreshNeededRef.current = true;
    patchHistoryEntryDetailProgress(historyEntriesRef.current.values(), candidateID, progress);
    setContinueTarget((current) => patchContinueTargetProgress(current, candidateID, progress, sourceItem, sourceNextItem, sourceSeries));
    setRecent((current) => current.map((item) => patchItem(item)));
    setHistory((current) => {
      const patched = current.map((item) => patchItem(item));
      if (patched.some((item) => item.candidate_id === candidateID) || !sourceItem) return patched;
      const inserted = { ...sourceItem, progress, progress_index: progress.index, progress_count: progress.count, progress_percent: progress.progress_percent, progress_completed: progress.completed, progress_status: progress.progress_status } as ReadingHistoryItem;
      return [inserted, ...patched].slice(0, MY_HISTORY_LIMIT);
    });
    setCatalog((current) => current.map((item) => patchItem(item)));
    setFavorites((current) => current.map((item) => patchItem(item)));
    setDiscover((current) => current ? {
      ...current,
      random_items: discoverMode === "unread"
        ? current.random_items.filter((item) => item.candidate_id !== candidateID)
        : current.random_items.map((item) => patchItem(item)),
      history: (() => {
        const patched = current.history.map((item) => patchItem(item));
        if (patched.some((item) => item.candidate_id === candidateID) || !sourceItem) return patched;
        const inserted = { ...sourceItem, progress, progress_index: progress.index, progress_count: progress.count, progress_percent: progress.progress_percent, progress_completed: progress.completed, progress_status: progress.progress_status } as ReadingHistoryItem;
        return [inserted, ...patched].slice(0, 8);
      })(),
    } : current);
    setDetail((current) => patchDetailProgress(current, candidateID, progress));
    setHeroDetail((current) => current?.work.candidate_id === candidateID
      ? { ...current, work: patchItem(current.work) }
      : current);
  }, [discoverMode]);

  const flushPendingProgress = useCallback((): Promise<void> => {
    if (progressFlushRef.current) return progressFlushRef.current;
    const task = (async () => {
      let didMutate = false;
      for (const entry of pendingProgressEntries()) {
        try {
          const manifest = await getProgress(entry.payload.candidate_id);
          if (!sameManifest(entry.payload, manifest)) continue;
          const requestSequence = (readerSaveSequenceRef.current.get(entry.payload.candidate_id) ?? 0) + 1;
          readerSaveSequenceRef.current.set(entry.payload.candidate_id, requestSequence);
          const response = await saveProgress(entry.payload);
          if (response.discard_pending) {
            setPendingProgressTotal(acknowledgePendingProgress(entry.entryID));
            didMutate = true;
            progressSaveErrorRef.current.delete(entry.payload.candidate_id);
            if (!response.progress) continue;
          }
          if (!response.progress) continue;
          if (!response.discard_pending) {
            setPendingProgressTotal(acknowledgePendingProgress(entry.entryID));
            didMutate = true;
          }
          progressSaveErrorRef.current.delete(entry.payload.candidate_id);
          const appliedSequence = readerSaveAppliedRef.current.get(entry.payload.candidate_id) ?? 0;
          if (requestSequence >= appliedSequence) {
            readerSaveAppliedRef.current.set(entry.payload.candidate_id, requestSequence);
            applyProgressState(entry.payload.candidate_id, response.progress);
            const manifestID = String(entry.payload.page_manifest_id || entry.payload.manifest_hash || "unknown");
            setReader((latest) => latest?.item.candidate_id === entry.payload.candidate_id
              && String(latest.pages.page_manifest_id || latest.pages.manifest_hash || "unknown") === manifestID
              ? { ...latest, savedIndex: numberValue(response.progress?.index, latest.savedIndex) }
              : latest);
          }
        } catch (reason) {
          if (reason instanceof ApiError && reason.status === 409) continue;
          if (!(reason instanceof ApiError) || reason.status === 0 || reason.status >= 500) break;
        }
      }
      if (!didMutate) setPendingProgressTotal(pendingProgressCount());
    })().finally(() => {
      progressFlushRef.current = null;
    });
    progressFlushRef.current = task;
    return task;
  }, [applyProgressState]);

  const persistReader = useCallback(async (current: ReaderState, options: PersistReaderOptions = {}): Promise<ReadingProgress | null> => {
    if (current.calibration && !options.force) return null;
    const candidateID = current.item.candidate_id;
    const manifestID = String(current.pages.page_manifest_id || current.pages.manifest_hash || "unknown");
    const saveKey = `${candidateID}\u0000${manifestID}`;
    const stage = readerStageRef.current;
    const liveScrollTop = current.fitMode === "fit-width" && stage ? Math.max(0, Math.round(stage.scrollTop)) : Math.max(0, Math.round(current.stageScrollTop));
    const liveScrollLeft = current.fitMode === "fit-width" && stage ? Math.max(0, Math.round(stage.scrollLeft)) : Math.max(0, Math.round(current.stageScrollLeft));
    const currentSplitWide = splitWideActive(current.fitMode, current.imageNaturalWidth, current.imageNaturalHeight);
    const completed = current.index >= current.pages.count - 1
      && !current.imageLoading
      && !current.error
      && (!currentSplitWide || current.splitPanel >= 1);
    const signature = [current.index, current.pages.count, completed ? 1 : 0, current.fitMode, currentSplitWide ? current.splitPanel : 0, liveScrollTop, liveScrollLeft].join(":");
    if (!options.force && readerSaveSignatureRef.current.get(saveKey) === signature) return null;
    readerSaveSignatureRef.current.set(saveKey, signature);
    const payload: ProgressSavePayload = {
      candidate_id: candidateID,
      index: current.index,
      count: current.pages.count,
      completed,
      page_manifest_id: current.pages.page_manifest_id,
      manifest_hash: current.pages.manifest_hash,
      reader_fit_mode: current.fitMode,
      reader_split_panel: currentSplitWide ? current.splitPanel : 0,
      stage_scroll_top: liveScrollTop,
      stage_scroll_left: liveScrollLeft,
      updated_at: nextProgressTimestamp(),
    };
    const pending = enqueuePendingProgress(payload, current.item.work_identity_id);
    setPendingProgressTotal(pending.logicalPendingCount);
    const requestSequence = (readerSaveSequenceRef.current.get(candidateID) ?? 0) + 1;
    readerSaveSequenceRef.current.set(candidateID, requestSequence);
    try {
      const response = await saveProgress(payload, { keepalive: options.silent });
      if (response.timestamp_rejected && !options.silent) {
        showToast({
          kind: "error",
          message: "设备时间与服务器相差过大，这次位置没有写入；校准系统时间后请重试。",
        });
      }
      if (response.discard_pending && !response.progress) {
        setPendingProgressTotal(acknowledgePendingProgress(pending.entryID));
        progressSaveErrorRef.current.delete(candidateID);
        if (!options.silent && !response.timestamp_rejected) {
          showToast({
            kind: "error",
            message: "这次位置早于最近一次阅读状态变更，已安全忽略；重新打开作品即可继续。",
          });
        }
        return null;
      }
      if (!response.progress) throw new Error("服务器没有返回可确认的阅读位置。");
      if (options.force && (String(response.progress.progress_status || "") !== "normal" || !sameManifest(response.progress, current.pages))) {
        throw new Error("服务器尚未确认新的页面清单，请重试。");
      }
      let remainingPendingCount = acknowledgePendingProgress(pending.entryID);
      if (options.force && current.stalePending) remainingPendingCount = acknowledgePendingProgress(current.stalePending.entryID);
      setPendingProgressTotal(remainingPendingCount);
      progressSaveErrorRef.current.delete(candidateID);
      const appliedSequence = readerSaveAppliedRef.current.get(candidateID) ?? 0;
      if (requestSequence >= appliedSequence) {
        readerSaveAppliedRef.current.set(candidateID, requestSequence);
        const sourceSeries = current.seriesID
          ? {
            group_id: current.seriesID,
            series_title: String(
              current.item.series_title
              || (detailRef.current?.kind === "series" && detailRef.current.data.series.group_id === current.seriesID
                ? detailRef.current.data.series.series_title
                : "")
              || itemTitle(current.item),
            ),
          }
          : null;
        applyProgressState(candidateID, response.progress, current.item, current.nextItem, sourceSeries);
        const savedIndex = numberValue(response.progress.index, current.index);
        setReader((latest) => latest?.item.candidate_id === candidateID && String(latest.pages.page_manifest_id || latest.pages.manifest_hash || "unknown") === manifestID
          ? { ...latest, savedIndex }
          : latest,
        );
      }
      return response.progress;
    } catch (reason) {
      setPendingProgressTotal(pendingProgressCount());
      if (!options.silent && !progressSaveErrorRef.current.has(candidateID)) {
        progressSaveErrorRef.current.add(candidateID);
        showToast({
          kind: "error",
          message: reason instanceof ApiError && reason.status === 409
            ? "书页清单已经变化，这次位置已安全留在本机；请退出后重新打开。"
            : `阅读位置已留在本机，联网后会自动同步：${apiErrorText(reason)}`,
          actionLabel: options.force ? undefined : "立即重试",
          onAction: options.force ? undefined : () => { void flushPendingProgress(); },
        });
      }
      return null;
    }
  }, [applyProgressState, flushPendingProgress, showToast]);
  persistReaderRef.current = persistReader;

  const confirmReaderCalibration = useCallback(async () => {
    if (!reader?.calibration || reader.calibrationSaving) return;
    const candidateID = reader.item.candidate_id;
    const manifestID = String(reader.pages.page_manifest_id || reader.pages.manifest_hash || "unknown");
    setReader((current) => current?.item.candidate_id === candidateID ? { ...current, calibrationSaving: true } : current);
    const confirmed = { ...reader, calibration: null, calibrationSaving: true, savedIndex: -1 };
    const progress = await persistReader(confirmed, { force: true });
    setReader((current) => {
      if (!current || current.item.candidate_id !== candidateID || String(current.pages.page_manifest_id || current.pages.manifest_hash || "unknown") !== manifestID) return current;
      return progress
        ? { ...current, calibration: null, calibrationSaving: false, stalePending: undefined, savedIndex: numberValue(progress.index, reader.index) }
        : { ...current, calibrationSaving: false };
    });
  }, [persistReader, reader]);

  const revealReaderChrome = useCallback((hold = false) => {
    if (readerChromeTimerRef.current !== null) window.clearTimeout(readerChromeTimerRef.current);
    readerChromeTimerRef.current = null;
    const current = uiRef.current?.reader;
    if (!current) return;
    if (!current.chromeVisible) setReader((latest) => latest ? { ...latest, chromeVisible: true } : latest);
    if (hold || current.calibration || current.ending) return;
    readerChromeTimerRef.current = window.setTimeout(() => {
      const latest = uiRef.current?.reader;
      if (!latest || latest.calibration || latest.ending || latest.imageLoading) return;
      const active = document.activeElement;
      const focusedChrome = active instanceof Element
        && Boolean(readerDialogRef.current?.contains(active) && active.closest(".reader-topbar, .reader-controls"));
      if (focusedChrome) return;
      setReader((active) => active ? { ...active, chromeVisible: false } : active);
      readerChromeTimerRef.current = null;
    }, 3_200);
  }, []);

  const handleReaderPointerMove = useCallback(() => {
    if (!shouldRefreshReaderChromeOnPointerMove(uiRef.current?.reader?.chromeVisible)) return;
    const now = window.performance?.now() ?? Date.now();
    if (readerPointerMoveAtRef.current > 0 && now - readerPointerMoveAtRef.current < 220) return;
    readerPointerMoveAtRef.current = now;
    revealReaderChrome();
  }, [revealReaderChrome]);

  const goToReaderPage = useCallback((requested: number, retry = false, requestedSplitPanel?: 0 | 1, revealChrome = true) => {
    const current = uiRef.current?.reader;
    if (!current) return;
    const target = Math.max(0, Math.min(current.pages.count - 1, Math.round(requested)));
    const targetSplitPanel = requestedSplitPanelForPage(
      target,
      current.index,
      current.requestedIndex,
      current.splitPanel,
      current.requestedSplitPanel,
      requestedSplitPanel,
    );
    const sameRequestedPanel = targetSplitPanel === current.requestedSplitPanel;
    if (target === current.requestedIndex && sameRequestedPanel && !retry && !current.error) return;
    setReader({
      ...current,
      requestedIndex: target,
      pageRevision: retry || target === current.requestedIndex ? current.pageRevision + 1 : current.pageRevision,
      imageLoading: true,
      error: "",
      ending: false,
      chromeVisible: revealChrome ? true : current.chromeVisible,
      requestedSplitPanel: targetSplitPanel,
      stageScrollTop: target === current.index ? current.stageScrollTop : 0,
      stageScrollLeft: target === current.index ? current.stageScrollLeft : 0,
      restoreScroll: false,
    });
    if (revealChrome) revealReaderChrome();
  }, [revealReaderChrome]);

  const openNextReader = useCallback((explicit?: WorkSummary) => {
    const current = uiRef.current?.reader;
    const next = explicit || current?.nextItem;
    if (!current || !next?.can_read) return;
    void persistReaderRef.current(readerWithLiveScroll(current), { silent: true });
    cancelReaderRequests({ preservePageCache: true });
    const seriesDetail = detailRef.current?.kind === "series" && (!current.seriesID || detailRef.current.data.series.group_id === current.seriesID)
      ? detailRef.current.data
      : current.seriesID ? seriesDetailResolvedRef.current.get(current.seriesID) : undefined;
    const context: ReaderContext = {
      seriesID: current.seriesID,
      nextItem: seriesDetail ? nextSeriesReadable(seriesDetail, next.candidate_id) : undefined,
    };
    const intent: ReaderIntent = { item: next, ...context };
    replaceUiSnapshot(`reader:${next.candidate_id}`, {
      reader: null,
      readerLoading: true,
      readerIntent: intent,
    });
    void loadReaderForEntry(next, undefined, historyCurrentRef.current, context);
  }, [cancelReaderRequests, loadReaderForEntry, readerWithLiveScroll, replaceUiSnapshot]);

  const moveReader = useCallback((delta: number, revealChrome = true) => {
    const current = uiRef.current?.reader;
    if (!current || current.calibration || delta === 0) return;
    if (current.ending) {
      if (delta < 0) {
        setReader({ ...current, ending: false, chromeVisible: true });
        revealReaderChrome();
      } else if (current.nextItem) {
        openNextReader();
      }
      return;
    }
    const direction = delta > 0 ? 1 : -1;
    const currentSplitWide = !current.imageLoading
      && splitWideActive(current.fitMode, current.imageNaturalWidth, current.imageNaturalHeight);
    const panelStep = splitWidePanelStep(current.splitPanel, direction, currentSplitWide);
    if (panelStep.handled) {
      const next = {
        ...current,
        splitPanel: panelStep.panel,
        requestedSplitPanel: panelStep.panel,
        chromeVisible: revealChrome ? true : current.chromeVisible,
      };
      setReader(next);
      void persistReaderRef.current(next, { silent: true });
      if (revealChrome) revealReaderChrome();
      return;
    }
    const base = current.requestedIndex;
    if (direction > 0 && base >= current.pages.count - 1) {
      if (current.index >= current.pages.count - 1 && !current.imageLoading) {
        const completed = readerWithLiveScroll({ ...current, ending: true, chromeVisible: true });
        setReader(completed);
        void persistReaderRef.current(completed, { silent: true });
        revealReaderChrome(true);
      }
      return;
    }
    goToReaderPage(base + direction, false, direction < 0 && current.fitMode === "split-wide" ? 1 : 0, revealChrome);
  }, [goToReaderPage, openNextReader, readerWithLiveScroll, revealReaderChrome]);
  readerMoveRef.current = moveReader;

  const moveReaderViewport = useCallback((direction: -1 | 1, revealChrome = true) => {
    const current = uiRef.current?.reader;
    const stage = readerStageRef.current;
    if (!current || current.calibration) return;
    if (current.fitMode !== "fit-width" || !stage || current.ending) {
      moveReader(direction, revealChrome);
      return;
    }
    const maxScroll = Math.max(0, stage.scrollHeight - stage.clientHeight);
    const atBoundary = direction > 0 ? stage.scrollTop >= maxScroll - 8 : stage.scrollTop <= 8;
    if (atBoundary) {
      moveReader(direction, revealChrome);
      return;
    }
    stage.scrollBy({ top: direction * Math.max(180, stage.clientHeight * 0.84), behavior: preferredScrollBehavior() });
    if (revealChrome) revealReaderChrome();
  }, [moveReader, revealReaderChrome]);
  readerViewportRef.current = moveReaderViewport;

  const retryReaderPage = useCallback(() => {
    const current = uiRef.current?.reader;
    if (current) goToReaderPage(current.requestedIndex, true);
  }, [goToReaderPage]);
  readerRetryRef.current = retryReaderPage;

  const changeReaderFit = useCallback((mode: ActiveReaderFitMode) => {
    const current = uiRef.current?.reader;
    if (!current || current.fitMode === mode) return;
    rememberReaderFit(mode);
    setReaderFitPreference(mode);
    const refetchForSplit = mode === "split-wide" && current.fitMode !== "split-wide";
    const next = {
      ...current,
      fitMode: mode,
      splitPanel: 0 as const,
      requestedSplitPanel: 0 as const,
      stageScrollTop: 0,
      stageScrollLeft: 0,
      restoreScroll: true,
      chromeVisible: true,
      pageRevision: refetchForSplit ? current.pageRevision + 1 : current.pageRevision,
      imageLoading: refetchForSplit ? true : current.imageLoading,
      error: refetchForSplit ? "" : current.error,
    };
    setReader(next);
    window.requestAnimationFrame(() => readerStageRef.current?.scrollTo({ top: 0, left: 0, behavior: "auto" }));
    void persistReaderRef.current(next, { silent: true });
    revealReaderChrome();
  }, [revealReaderChrome]);

  const handleReaderScroll = useCallback(() => {
    const current = uiRef.current?.reader;
    const stage = readerStageRef.current;
    if (!current || !stage || current.fitMode !== "fit-width") return;
    if (readerScrollTimerRef.current !== null) window.clearTimeout(readerScrollTimerRef.current);
    readerScrollTimerRef.current = window.setTimeout(() => {
      const latest = uiRef.current?.reader;
      const latestStage = readerStageRef.current;
      if (!latest || !latestStage || latest.fitMode !== "fit-width") return;
      const scrolled = {
        ...latest,
        stageScrollTop: Math.max(0, Math.round(latestStage.scrollTop)),
        stageScrollLeft: Math.max(0, Math.round(latestStage.scrollLeft)),
      };
      setReader(scrolled);
      void persistReaderRef.current(scrolled, { silent: true });
      readerScrollTimerRef.current = null;
    }, 460);
  }, []);

  const commitReaderPageDraft = useCallback(() => {
    const current = uiRef.current?.reader;
    if (!current) return;
    const parsed = Number.parseInt(readerPageDraft, 10);
    const page = Number.isFinite(parsed) ? Math.max(1, Math.min(current.pages.count, parsed)) : current.index + 1;
    setReaderPageDraft(String(page));
    goToReaderPage(page - 1);
  }, [goToReaderPage, readerPageDraft]);

  const handleReaderStageClick = useCallback((event: MouseEvent<HTMLDivElement>) => {
    if (event.target instanceof Element && event.target.closest("button, input, a")) return;
    const current = uiRef.current?.reader;
    if (!current || current.calibration || current.ending) return;
    event.currentTarget.focus({ preventScroll: true });
    if (readerSuppressClickRef.current) {
      if (readerSuppressClickTimerRef.current !== null) window.clearTimeout(readerSuppressClickTimerRef.current);
      readerSuppressClickTimerRef.current = null;
      readerSuppressClickRef.current = false;
      return;
    }
    const rect = event.currentTarget.getBoundingClientRect();
    const ratio = rect.width ? (event.clientX - rect.left) / rect.width : 0.5;
    const action = readerStageClickAction(ratio, current.chromeVisible);
    if (action.type === "navigate") moveReaderViewport(action.direction, action.revealChrome);
    else if (action.visible) revealReaderChrome();
    else setReader({ ...current, chromeVisible: false });
  }, [moveReaderViewport, revealReaderChrome]);

  const readerCandidateID = reader?.item.candidate_id || "";
  const readerManifestID = String(reader?.pages.page_manifest_id || reader?.pages.manifest_hash || "");
  const readerRequestedIndex = reader?.requestedIndex ?? -1;
  const readerPageRevision = reader?.pageRevision ?? 0;
  const readerFitMode = reader?.fitMode || "fit-page";
  const readerEnding = Boolean(reader?.ending);
  const readerSplitWideActive = Boolean(reader && splitWideActive(reader.fitMode, reader.imageNaturalWidth, reader.imageNaturalHeight));
  const currentReaderImageRequestKey = readerImageRequestKey(
    reader?.item.candidate_id || "",
    readerManifestID,
    reader?.requestedIndex ?? -1,
    reader?.pageRevision ?? 0,
    Boolean(reader?.imageLoading),
    Boolean(reader?.ending),
  );
  const readerVisualLoading = showReaderVisualLoading(
    Boolean(reader?.imageLoading),
    reader?.imageURL || "",
    currentReaderImageRequestKey,
    readerSlowLoadingKey,
  );
  const readerLiveStatus = !reader || reader.calibration
    ? ""
    : reader.ending
      ? "本话读完。"
      : reader.imageLoading
        ? `正在载入第 ${reader.requestedIndex + 1} 页，共 ${reader.pages.count} 页。`
        : `第 ${reader.index + 1} 页，共 ${reader.pages.count} 页。${readerSplitWideActive ? (reader.splitPanel === 0 ? "当前为右半页。" : "当前为左半页。") : ""}${pendingProgressTotal ? `有 ${pendingProgressTotal} 条进度待同步。` : "阅读进度已同步。"}`;

  useEffect(() => {
    setReaderSlowLoadingKey("");
    if (!currentReaderImageRequestKey || !reader?.imageURL) return undefined;
    const timer = window.setTimeout(() => setReaderSlowLoadingKey(currentReaderImageRequestKey), 140);
    return () => window.clearTimeout(timer);
  }, [currentReaderImageRequestKey, reader?.imageURL]);

  const readerSplitImageStyle = useMemo(() => {
    if (!reader || !readerSplitWideActive || readerStageSize.width <= 0 || readerStageSize.height <= 0) return undefined;
    const halfWidth = reader.imageNaturalWidth / 2;
    const scale = Math.min(readerStageSize.width / halfWidth, readerStageSize.height / reader.imageNaturalHeight);
    const dpr = typeof window === "undefined" ? 1 : window.devicePixelRatio || 1;
    const width = Math.max(1, snapReaderPixel(reader.imageNaturalWidth * scale, dpr));
    const height = Math.max(1, snapReaderPixel(reader.imageNaturalHeight * scale, dpr));
    const shift = snapReaderPixel((reader.splitPanel === 0 ? -1 : 1) * width * 0.25, dpr);
    return {
      width: `${width}px`,
      height: `${height}px`,
      maxWidth: "none",
      maxHeight: "none",
      transform: `translateX(${shift}px)`,
    };
  }, [reader, readerSplitWideActive, readerStageSize.height, readerStageSize.width]);

  useLayoutEffect(() => {
    if (!reader || reader.fitMode !== "split-wide") {
      setReaderStageSize((current) => current.width || current.height ? { width: 0, height: 0 } : current);
      return undefined;
    }
    const stage = readerStageRef.current;
    if (!stage) return undefined;
    let frame = 0;
    const measure = () => {
      const next = { width: stage.clientWidth, height: stage.clientHeight };
      setReaderStageSize((current) => current.width === next.width && current.height === next.height ? current : next);
    };
    const scheduleMeasure = () => {
      if (frame) return;
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        measure();
      });
    };
    measure();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(scheduleMeasure);
    observer?.observe(stage);
    if (!observer) window.addEventListener("resize", scheduleMeasure);
    return () => {
      observer?.disconnect();
      if (!observer) window.removeEventListener("resize", scheduleMeasure);
      if (frame) window.cancelAnimationFrame(frame);
    };
  }, [reader?.fitMode, reader?.item.candidate_id]);

  useEffect(() => {
    if (!readerCandidateID || !readerManifestID || readerRequestedIndex < 0 || readerEnding) return undefined;
    const current = uiRef.current?.reader;
    if (!current?.imageLoading) return undefined;
    readerPageAbortRef.current?.abort();
    const controller = new AbortController();
    readerPageAbortRef.current = controller;
    const preserveSource = readerUsesSourceQuality(current.item.candidate_type);
    const imageMax = readerImageMax(readerFitMode, preserveSource);
    const requestedURL = pageUrl(
      readerCandidateID,
      readerRequestedIndex,
      current.pages.page_manifest_id,
      imageMax,
      preserveSource,
    );
    const pageCache = readerPageCacheRef.current;

    const load = async () => {
      try {
        if (!pageCache) throw new Error("阅读缓存尚未准备好。");
        const asset = await pageCache.load(requestedURL);
        if (controller.signal.aborted) return;
        const latest = uiRef.current?.reader;
        if (!latest
          || latest.item.candidate_id !== readerCandidateID
          || String(latest.pages.page_manifest_id || latest.pages.manifest_hash || "") !== readerManifestID
          || latest.requestedIndex !== readerRequestedIndex
          || latest.pageRevision !== readerPageRevision
          || latest.fitMode !== readerFitMode
          || latest.ending) return;
        readerImageCacheKeyRef.current = requestedURL;
        readerImageURLRef.current = asset.objectURL;
        readerPrefetchPlanRef.current = {
          cacheKey: requestedURL,
          candidateID: readerCandidateID,
          count: latest.pages.count,
          imageMax,
          imageURL: asset.objectURL,
          index: readerRequestedIndex,
          pageManifestID: latest.pages.page_manifest_id,
          preserveSource,
        };
        setReader({
          ...latest,
          index: readerRequestedIndex,
          imageURL: readerImageURLRef.current,
          imageLoading: false,
          error: "",
          splitPanel: latest.requestedSplitPanel,
          imageNaturalWidth: asset.width,
          imageNaturalHeight: asset.height,
          restoreScroll: true,
          ending: false,
        });
      } catch (reason) {
        if (controller.signal.aborted) return;
        const latest = uiRef.current?.reader;
        if (!latest
          || latest.item.candidate_id !== readerCandidateID
          || String(latest.pages.page_manifest_id || latest.pages.manifest_hash || "") !== readerManifestID
          || latest.requestedIndex !== readerRequestedIndex
          || latest.pageRevision !== readerPageRevision) return;
        const message = reason instanceof ReaderPageCacheTimeoutError
          ? `第 ${readerRequestedIndex + 1} 页加载超时。`
          : reason instanceof ReaderPageResponseError && reason.status === 409
            ? "书页清单已经变化，请退出后重新打开。"
            : reason instanceof ReaderPageResponseError
              ? `第 ${readerRequestedIndex + 1} 页暂时无法读取（HTTP ${reason.status}）。`
              : apiErrorText(reason);
        setReader({
          ...latest,
          imageLoading: false,
          error: message,
          chromeVisible: true,
        });
        revealReaderChrome(true);
      } finally {
        if (readerPageAbortRef.current === controller) readerPageAbortRef.current = null;
      }
    };
    void load();
    return () => {
      controller.abort();
      if (readerPrefetchTimerRef.current !== null) window.clearTimeout(readerPrefetchTimerRef.current);
      readerPrefetchTimerRef.current = null;
    };
  }, [readerCandidateID, readerEnding, readerFitMode, readerManifestID, readerPageRevision, readerRequestedIndex, revealReaderChrome]);

  useLayoutEffect(() => {
    const plan = readerPrefetchPlanRef.current;
    const pageCache = readerPageCacheRef.current;
    if (!plan || !pageCache || !reader?.imageURL
      || reader.imageURL !== plan.imageURL
      || reader.item.candidate_id !== plan.candidateID
      || reader.index !== plan.index) return undefined;
    readerPrefetchPlanRef.current = null;
    pageCache.setPinnedKey(plan.cacheKey);
    if (!shouldPrefetchReaderPages()) return undefined;

    const forwardIndices = readerForwardPrefetchIndices(
      plan.index,
      plan.count,
      plan.preserveSource ? 1 : 2,
    );
    void forwardIndices.reduce<Promise<void>>(
      (previous, index) => previous.then(async () => {
        const nextURL = pageUrl(
          plan.candidateID,
          index,
          plan.pageManifestID,
          plan.imageMax,
          plan.preserveSource,
        );
        await pageCache.load(nextURL);
      }),
      Promise.resolve(),
    ).catch(() => undefined);
    if (readerPrefetchTimerRef.current !== null) window.clearTimeout(readerPrefetchTimerRef.current);
    if (plan.index > 0) {
      const previousURL = pageUrl(
        plan.candidateID,
        plan.index - 1,
        plan.pageManifestID,
        plan.imageMax,
        plan.preserveSource,
      );
      readerPrefetchTimerRef.current = window.setTimeout(() => {
        readerPrefetchTimerRef.current = null;
        if (readerImageCacheKeyRef.current === plan.cacheKey && pageCache.has(previousURL)) {
          void pageCache.load(previousURL).catch(() => undefined);
        }
      }, 240);
    }
    return () => {
      if (readerPrefetchTimerRef.current !== null) window.clearTimeout(readerPrefetchTimerRef.current);
      readerPrefetchTimerRef.current = null;
    };
  }, [reader?.imageURL, reader?.index, reader?.item.candidate_id]);

  useLayoutEffect(() => {
    if (!reader?.imageURL || !reader.restoreScroll) return undefined;
    const latest = uiRef.current?.reader;
    const stage = readerStageRef.current;
    if (!latest || !stage || latest.imageURL !== reader.imageURL) return undefined;
    const top = latest.fitMode === "fit-width" ? latest.stageScrollTop : 0;
    const left = latest.fitMode === "fit-width" ? latest.stageScrollLeft : 0;
    stage.scrollTo({ top, left, behavior: "auto" });
    const settled = { ...latest, stageScrollTop: top, stageScrollLeft: left, restoreScroll: false };
    setReader(settled);
    void persistReaderRef.current(settled, { silent: true });
    return undefined;
  }, [reader?.fitMode, reader?.imageURL, reader?.restoreScroll]);

  useEffect(() => {
    if (!reader) {
      setReaderPageDraft("");
      return;
    }
    setReaderPageDraft(String(reader.index + 1));
  }, [reader?.index, reader?.item.candidate_id]);

  useEffect(() => {
    const seriesID = reader?.seriesID;
    const candidateID = reader?.item.candidate_id;
    if (!seriesID || !candidateID) return undefined;
    let active = true;
    let request = seriesDetailCacheRef.current.get(seriesID);
    if (!request) {
      request = getSeriesDetail(seriesID);
      seriesDetailCacheRef.current.set(seriesID, request);
      request.catch(() => {
        if (seriesDetailCacheRef.current.get(seriesID) === request) seriesDetailCacheRef.current.delete(seriesID);
      });
    }
    request.then((data) => {
      seriesDetailResolvedRef.current.set(seriesID, data);
      if (!active) return;
      const nextItem = nextSeriesReadable(data, candidateID);
      setReader((current) => current?.seriesID === seriesID && current.item.candidate_id === candidateID
        ? { ...current, nextItem }
        : current,
      );
    }).catch(() => undefined);
    return () => { active = false; };
  }, [reader?.item.candidate_id, reader?.seriesID]);

  useEffect(() => {
    const persistLiveReader = () => {
      const current = uiRef.current?.reader;
      if (current) void persistReaderRef.current(readerWithLiveScroll(current), { silent: true });
    };
    const onOnline = () => {
      void flushPendingProgress();
      void flushPendingMarks();
    };
    const onVisibility = () => {
      if (document.visibilityState === "hidden") persistLiveReader();
      else {
        void flushPendingProgress();
        void flushPendingMarks();
      }
    };
    const onStorage = (event: StorageEvent) => {
      if (event.key === READER_FIT_KEY || event.key === null) {
        const nextMode = event.newValue;
        setReaderFitPreference(nextMode === "fit-width" || nextMode === "fit-page" || nextMode === "split-wide" ? nextMode : "fit-page");
      }
      setPendingProgressTotal(pendingProgressCount());
      if (document.visibilityState === "visible") {
        void flushPendingProgress();
        void flushPendingMarks();
      }
    };
    void flushPendingProgress();
    void flushPendingMarks();
    window.addEventListener("online", onOnline);
    window.addEventListener("pagehide", persistLiveReader);
    window.addEventListener("storage", onStorage);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      window.removeEventListener("online", onOnline);
      window.removeEventListener("pagehide", persistLiveReader);
      window.removeEventListener("storage", onStorage);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [flushPendingMarks, flushPendingProgress, readerWithLiveScroll]);

  useEffect(() => () => {
    readerPreparationCacheRef.current?.clear();
    cancelReaderRequests();
  }, [cancelReaderRequests]);

  const continuedHero = continueTarget?.item || null;
  const newHero = recent.find((item) => !isSeries(item) && Boolean(item.candidate_id)) || recent[0] || null;
  const hero = continuedHero || newHero;
  const heroMode = continuedHero ? "continue" : "new";
  const heroCandidateID = hero && !isSeries(hero) ? String(hero.candidate_id || "") : "";
  const homeHeroLoading = !hero && (continueLoading || recentLoading);
  const homeHeroError = !hero ? [continueError, recentError].filter(Boolean).join(" · ") : "";

  useEffect(() => {
    if (!heroCandidateID) {
      setHeroDetail(null);
      return undefined;
    }
    setHeroDetail(null);
    const controller = new AbortController();
    getWork(heroCandidateID, { signal: controller.signal })
      .then((data) => {
        if (!controller.signal.aborted) setHeroDetail(data);
      })
      .catch(() => {
        if (!controller.signal.aborted) setHeroDetail(null);
      });
    return () => controller.abort();
  }, [heroCandidateID]);

  const totalWorks = view === "library" || view === "search"
    ? catalogPresentationTotal
    : recentTotal;
  const favoritesCachedPage = view === "my" && favoritesRequestKey
    ? favoritesPageCacheRef.current.peek(favoritesRequestKey)
    : undefined;
  const favoritesDisplayItems = view === "my"
    ? selectFavoritesDisplayItems(favoritesOffset, FAVORITES_PAGE_SIZE, favoritesDisplayOffset, favorites, favoritesCachedPage)
    : null;
  const favoritesPageReady = favoritesDisplayItems !== null;
  const visibleItems = view === "my" ? (favoritesDisplayItems || EMPTY_SHELF_ITEMS) : catalogPresentationItems;
  const visibleItemsOffset = view === "my" ? favoritesOffset : catalogPresentationOffset;
  const page = Math.floor(offset / PAGE_SIZE) + 1;
  const pages = Math.max(1, Math.ceil(catalogPresentationTotal / PAGE_SIZE));
  const favoritesPage = Math.floor(favoritesOffset / FAVORITES_PAGE_SIZE) + 1;
  const favoritesPages = Math.max(1, Math.ceil(favoritesTotal / FAVORITES_PAGE_SIZE));
  const discoverModeInfo = discoverModes.find((mode) => mode.id === discoverMode) || discoverModes[0]!;

  const changeReaderFitPreference = useCallback((mode: ActiveReaderFitMode) => {
    setReaderFitPreference(mode);
    rememberReaderFit(mode);
  }, []);

  const changeCatalogPage = useCallback((nextOffset: number) => {
    const current = uiRef.current ? browseRouteFromSnapshot(uiRef.current) : defaultBrowseRoute("library");
    paginationFocusIntentRef.current = "catalog";
    paginationFocusSawBusyRef.current = false;
    commitBrowseTransition(sanitizeBrowseRoute({ ...current, offset: Math.max(0, nextOffset) }), { scroll: false });
    window.requestAnimationFrame(() => catalogTopRef.current?.scrollIntoView({ behavior: preferredScrollBehavior(), block: "start" }));
  }, [commitBrowseTransition]);

  const changeCatalogMode = useCallback((nextMode: CatalogMode) => {
    const snapshot = uiRef.current;
    const current = snapshot ? browseRouteFromSnapshot(snapshot) : defaultBrowseRoute("library");
    if (current.catalogMode === nextMode) return;
    if (snapshot?.view === "library") rememberBrowseScope(snapshot);
    const nextOffset = current.view === "library"
      ? libraryPageScopeOffset(libraryPageScopesRef.current, nextMode, current.sort)
      : 0;
    commitBrowseTransition(sanitizeBrowseRoute({ ...current, catalogMode: nextMode, offset: nextOffset }));
  }, [commitBrowseTransition, rememberBrowseScope]);

  const changeCatalogSort = useCallback((nextSort: CatalogSort) => {
    const current = uiRef.current ? browseRouteFromSnapshot(uiRef.current) : defaultBrowseRoute("library");
    if (current.sort === nextSort) return;
    if (current.view === "library") markLegacyLibrarySortHandled(nextSort);
    commitBrowseTransition(sanitizeBrowseRoute({ ...current, sort: nextSort, offset: 0 }));
  }, [commitBrowseTransition]);

  const changeDiscoverMode = useCallback((nextMode: DiscoverMode) => {
    if (nextMode === discoverModeRef.current) return;
    const current = uiRef.current ? browseRouteFromSnapshot(uiRef.current) : defaultBrowseRoute("discover");
    setDiscoverLoading(true);
    setDiscoverError("");
    setDiscoverErrorKind("batch");
    commitBrowseTransition(sanitizeBrowseRoute({ ...current, view: "discover", discoverMode: nextMode }));
  }, [commitBrowseTransition]);

  const changeFavoritesPage = useCallback((nextOffset: number) => {
    const current = uiRef.current ? browseRouteFromSnapshot(uiRef.current) : defaultBrowseRoute("my");
    paginationFocusIntentRef.current = "favorites";
    paginationFocusSawBusyRef.current = false;
    commitBrowseTransition(sanitizeBrowseRoute({ ...current, view: "my", offset: Math.max(0, nextOffset) }), { scroll: false });
    window.requestAnimationFrame(() => document.querySelector(".my-summary")?.scrollIntoView({ behavior: preferredScrollBehavior(), block: "start" }));
  }, [commitBrowseTransition]);

  const clearSearch = useCallback(() => {
    const current = uiRef.current ? browseRouteFromSnapshot(uiRef.current) : defaultBrowseRoute("search");
    commitBrowseTransition(sanitizeBrowseRoute({ ...current, view: "search", searchQuery: "", offset: 0 }), { searchDraft: "" });
    window.setTimeout(() => searchRef.current?.focus(), 0);
  }, [commitBrowseTransition]);

  const content = useMemo(() => {
    if (view === "home") {
      const heroProgress = hero ? progressFor(hero) : null;
      const heroPercent = Math.round(heroProgress?.percent || 0);
      const creator = heroCreator(heroDetail);
      const minutes = hero ? remainingMinutes(hero) : null;
      const lastRead = hero ? formatLastRead(hero) : "";
      return (
        <section className="home-view">
          <EditorialMasthead
            className="home-intro"
            eyebrow="YOUR EVENING SHELF"
            title={greeting()}
            titleID="home-page-title"
            folio="01 / HOME"
            meta={<time className="home-date" dateTime={localDateKey()}>{formatDay()}</time>}
          />
          {homeHeroLoading ? <Status>正在整理今晚的书架…</Status> : homeHeroError ? <Status kind="error">{homeHeroError}</Status> : hero ? (
            <EveningHero
              cover={<Cover item={hero} eager />}
              eyebrow={heroMode === "continue" ? "CONTINUE READING" : "NEW ON YOUR SHELF"}
              title={<>{cleanTitle(itemTitle(hero))} <i>·</i> {heroPosition(hero, heroDetail)}{heroProgress ? <> <i>·</i> {heroPercent}%</> : null}</>}
              subtitle={heroProgress ? `上次读到 ${heroProgress.index + 1} / ${heroProgress.count} 页${creator ? ` · ${creator}` : ""}` : pageMeta(hero)}
              progressPercent={heroProgress ? heroPercent : null}
              action={heroProgress?.completed ? "重新阅读" : heroMode === "continue" ? "继续阅读" : isSeries(hero) ? "查看系列" : "开始阅读"}
              onAction={() => isSeries(hero) ? openDetail(hero) : openReader(
                hero as WorkSummary,
                heroProgress?.completed ? 0 : undefined,
                hero === continuedHero && continueTarget?.series ? {
                  seriesID: continueTarget.series.group_id,
                  nextItem: continueTarget.next_item || undefined,
                } : {},
              )}
              note={heroNote(hero, heroDetail)}
              noteLabel={String(heroDetail?.mark?.notes || "").trim() ? "PRIVATE NOTE" : "READING NOTE"}
              noteMeta={<><span>{minutes === 0 ? "本章已读完" : minutes ? `预计剩余约 ${minutes} 分钟` : "暂无法估算剩余时间"}</span>{lastRead ? <time>{lastRead}</time> : null}</>}
              folio={heroProgress ? String(heroProgress.index + 1).padStart(2, "0") : "NEW"}
              coverBadge={heroMode === "continue" ? "READING" : "NEW"}
            />
          ) : <Status kind="empty">还没有阅读记录，先从书库挑一本吧。</Status>}
          {continueLoading && hero ? <p className="home-refresh-note" role="status">正在核对最新书签…</p> : null}
          {continueError ? <div className="catalog-inline-error home-history-error" role="alert"><span>{continueTarget ? "最新书签暂时无法同步，当前保留上次续读位置" : "续读记录暂时没有读取成功，当前先展示新入库作品"}：{continueError}</span><button type="button" onClick={() => setDataRevision((current) => current + 1)}>重新读取</button></div> : null}
          <section className="home-section">
            <SectionHeader title="最近入库" eyebrow="NEW ARRIVALS" action="查看全部" onAction={() => activateView("library")} />
            {recent.length ? <><div className="book-grid home-book-grid">{recent.slice(0, 6).map((item, index) => <BookCard key={itemID(item)} item={item} index={index} context="new" onOpen={openDetail} />)}</div>{recentLoading ? <p className="catalog-refresh-note" role="status">正在同步最近入库…</p> : null}{recentError ? <div className="catalog-inline-error" role="alert"><span>当前保留上次读取的最近入库作品：{recentError}</span><button type="button" onClick={() => setDataRevision((current) => current + 1)}>重新读取</button></div> : null}</> : recentLoading ? <CatalogSkeleton count={6} className="home-book-grid" /> : recentError ? <div className="catalog-inline-error" role="alert"><span>{recentError}</span><button type="button" onClick={() => setDataRevision((current) => current + 1)}>重新读取</button></div> : <Status kind="empty">最近还没有新入库的作品。</Status>}
          </section>
        </section>
      );
    }

    if (view === "settings") {
      const readerFitLabel = readerFitPreference === "fit-page" ? "整页" : readerFitPreference === "fit-width" ? "适宽" : "横页拆分";
      return (
        <section className="workspace-page settings-page">
          <button className="subpage-back" type="button" onClick={returnToMy}>← 返回我的</button>
          <EditorialMasthead
            className="settings-masthead"
            eyebrow="READING SETTINGS"
            title="阅读偏好"
            titleID="settings-page-title"
            folio="06 / SETTINGS"
            meta={<span>{readerFitLabel}</span>}
          />
          <p className="page-lead settings-lead">这里只保存阅读器布局偏好，不读取或运行任何维护任务。单本作品已保存的布局与停留位置仍优先恢复。</p>
          <section className="settings-reader-card" aria-labelledby="reader-preference-title">
            <span>READING LAYOUT</span>
            <h2 id="reader-preference-title">默认阅读布局</h2>
            <p>用于还没有保存过单本布局的作品。已经读过的作品仍优先恢复它自己的布局和滚动位置。</p>
            <div className="settings-choice" role="group" aria-label="默认阅读布局">
              <button type="button" aria-pressed={readerFitPreference === "fit-page"} onClick={() => changeReaderFitPreference("fit-page")}><strong>整页</strong><small>完整看见一页，适合桌面与横图</small></button>
              <button type="button" aria-pressed={readerFitPreference === "fit-width"} onClick={() => changeReaderFitPreference("fit-width")}><strong>适宽</strong><small>按屏幕宽度放大，适合手机长读</small></button>
              <button type="button" aria-pressed={readerFitPreference === "split-wide"} onClick={() => changeReaderFitPreference("split-wide")}><strong>横页拆分</strong><small>宽图按日漫顺序先右后左，竖页保持整页</small></button>
            </div>
            <output aria-live="polite">当前默认：{readerFitPreference === "fit-page" ? "整页显示" : readerFitPreference === "fit-width" ? "适应屏幕宽度" : "横页从右到左拆分"}</output>
          </section>
        </section>
      );
    }

    if (view === "discover") {
      const activeDiscover = discover?.random_mode === discoverMode ? discover : null;
      const randomItems = activeDiscover?.random_items || [];
      const discoverHistory = discover?.history || [];
      const stats = discover?.stats;
      return (
        <section className="catalog-page discover-view">
          <EditorialMasthead
            className="catalog-header discover-page-header"
            eyebrow="DISCOVERY ROOM"
            title="发现一本意外之喜"
            titleID="discover-page-title"
            folio="03 / DISCOVER"
            meta={<span>不带维护任务，只保留闲逛的乐趣</span>}
          />
          <DiscoveryLead
            modeLabel={discoverModeInfo.label}
            title={discoverModeInfo.title}
            copy={discoverModeInfo.copy}
            loading={discoverLoading}
            opening={randomOpening}
            onOpenRandom={() => { void openRandomDiscoverWork(); }}
            onRefresh={() => setDiscoverRevision((current) => current + 1)}
          />
          <DiscoveryModeRail
            active={discoverMode}
            options={discoverModes.map((mode) => ({
              id: mode.id,
              label: mode.label,
              hint: mode.id === "unread" ? "第一次见" : mode.id === "reading" ? "接着读" : mode.id === "liked" ? "偏好里" : mode.id === "reread" ? "再翻一次" : "全馆抽取",
            }))}
            onChange={changeDiscoverMode}
          />
              {discoverError ? <div className="catalog-inline-error" role="alert"><span>{discoverError}</span><button type="button" onClick={() => discoverErrorKind === "random" ? void openRandomDiscoverWork() : setDiscoverRevision((current) => current + 1)}>{discoverErrorKind === "random" ? "再抽一本" : "重新载入书架"}</button></div> : null}
          <div className="discover-shelf" ref={catalogTopRef} aria-busy={discoverLoading}>
            <SectionHeader title={`${discoverModeInfo.label}书架`} eyebrow={`TONIGHT'S PICKS · ${discoverLoading && !activeDiscover ? "…" : randomItems.length}`} action={activeDiscover ? "再换一批" : undefined} onAction={activeDiscover ? () => setDiscoverRevision((current) => current + 1) : undefined} />
            {discoverLoading && !activeDiscover ? <CatalogSkeleton count={12} /> : randomItems.length ? <div className="book-grid catalog-grid">{randomItems.map((item, index) => <BookCard key={itemID(item)} item={item} index={index} context="discover" priority={index < 2} onOpen={openDetail} />)}</div> : !discoverError ? <div className="catalog-state empty"><span>NO PICKS TONIGHT</span><h2>这一格暂时没有书</h2><p>换一个发现范围，或让整座书库替你做决定。</p><button type="button" onClick={() => discoverMode === "any" ? setDiscoverRevision((current) => current + 1) : changeDiscoverMode("any")}>{discoverMode === "any" ? "重新抽取" : "改为随缘"}</button></div> : null}
            {discoverLoading && activeDiscover ? <p className="catalog-refresh-note" role="status">正在从书架深处换一批封面…</p> : null}
          </div>
          <MetricLedger
            className="discover-stats discover-stats-after-shelf"
            label="私人阅读概览"
            items={[
              { label: "留下书签", value: stats ? compactNumber(stats.history_count) : "—" },
              { label: "收藏", value: stats ? compactNumber(stats.favorite_count) : "—" },
              { label: "喜欢", value: stats ? compactNumber(stats.liked_count) : "—" },
              { label: "想重读", value: stats ? compactNumber(stats.reread_count) : "—" },
            ]}
          />
          {discoverHistory.length ? <section className="discover-history"><SectionHeader title="最近留下书签" eyebrow={`READING TRAIL · ${discoverHistory.length}`} action="查看我的" onAction={() => activateView("my")} /><div className="book-grid compact-grid">{discoverHistory.slice(0, 6).map((item, index) => <BookCard key={itemID(item)} item={item} index={index} onOpen={openDetail} />)}</div></section> : null}
        </section>
      );
    }

    if (view === "search") {
      const hasQuery = Boolean(searchQuery.trim());
      const resultStart = catalogPresentationTotal ? offset + 1 : 0;
      const resultEnd = Math.min(offset + PAGE_SIZE, catalogPresentationTotal);
      return (
        <section className="catalog-page search-view">
          <EditorialMasthead
            className="catalog-header search-page-header"
            eyebrow="FIND IN LIBRARY"
            title="搜索全部馆藏"
            titleID="search-page-title"
            folio="04 / SEARCH"
            meta={hasQuery ? <span>{catalogBusy ? "正在检索" : `${catalogPresentationTotal.toLocaleString("zh-CN")} 个结果`}</span> : <span>标题 · 作者 · 汉化组</span>}
          />
          <SearchLead draft={searchDraft} hasQuery={hasQuery} query={searchQuery} onDraftChange={setSearchDraft} onSubmit={submitSearch} />
          {!hasQuery ? <SearchStart onBrowse={() => activateView("library")} onDiscover={() => activateView("discover")} /> : (
            <>
              <div className="library-toolbar search-toolbar">
                <div className="filter-tabs" role="group" aria-label="结果类型">
                  {catalogModeOptions.map((option) => <button type="button" className={`filter-chip ${catalogMode === option.id ? "active" : ""}`} aria-pressed={catalogMode === option.id} onClick={() => changeCatalogMode(option.id)} key={option.id}>{option.label}</button>)}
                </div>
                <label className="select-field"><span>排序</span><select value={sort} onChange={(event) => changeCatalogSort(event.target.value as CatalogSort)}>{catalogSortOptions.map((option) => <option value={option.id} key={option.id}>{option.label}</option>)}</select></label>
              </div>
              <div className="search-result-line" ref={catalogTopRef} role="status" tabIndex={-1}><span>{catalogBusy ? "正在核对馆藏…" : catalogPresentationTotal ? `显示 ${resultStart}–${resultEnd}，共 ${catalogPresentationTotal.toLocaleString("zh-CN")} 个结果` : `没有找到“${searchQuery}”`}</span><button type="button" onClick={clearSearch}>清除关键词</button></div>
              <div className="catalog-results" aria-busy={catalogBusy}>
                {catalogBusy && !catalogHasPresentationPage ? <CatalogSkeleton count={18} /> : catalogVisibleError ? <div className="catalog-state error" role="alert"><span>SEARCH INTERRUPTED</span><h2>这次检索没有完成</h2><p>{catalogVisibleError}</p><button type="button" onClick={retryCatalogPage}>重试</button></div> : catalogPresentationItems.length ? <><div className={`book-grid catalog-grid ${catalogShowingStale ? "is-stale" : ""}`.trim()} aria-hidden={catalogShowingStale ? true : undefined} inert={catalogShowingStale ? true : undefined}>{catalogPresentationItems.map((item, index) => <BookCard key={itemID(item)} item={item} index={index + catalogPresentationOffset} context="search" priority={index < 2} onOpen={openDetail} />)}</div>{catalogShowingStale ? <p className="catalog-refresh-note" role="status">正在载入这一页结果…</p> : null}</> : <div className="catalog-state empty"><span>0 RESULTS</span><h2>没有找到相符的封面</h2><p>试试更短的标题、作者姓氏，或清除类型限制。</p><div><button type="button" onClick={clearSearch}>清除关键词</button><button type="button" onClick={() => activateView("library")}>浏览全部馆藏</button></div></div>}
              </div>
              {catalogPageReady && !catalogBusy && catalogPresentationTotal > PAGE_SIZE ? <Pagination page={page} pages={pages} label="搜索结果分页" onPageChange={(nextPage) => changeCatalogPage((nextPage - 1) * PAGE_SIZE)} /> : null}
            </>
          )}
        </section>
      );
    }

    const listLoading = view === "my" ? favoritesBusy && !favoritesPageReady : catalogBusy && !catalogHasPresentationPage;
    const listError = view === "my" ? (favoritesPageReady ? "" : favoritesError) : catalogVisibleError;
    return (
      <section className={`catalog-page ${view === "library" ? "library-page" : view === "my" ? "my-page" : ""}`}>
        {view === "library" ? <LibraryMasthead error={Boolean(catalogVisibleError)} loading={catalogBusy} mode={catalogMode} page={page} pages={pages} total={catalogPresentationTotal} /> : (
          <EditorialMasthead
            className="catalog-header personal-page-header"
            eyebrow="PERSONAL SHELF"
            title="我的阅读"
            titleID="personal-page-title"
            folio="05 / PERSONAL"
            meta={<span>书签、收藏与私人偏好</span>}
          />
        )}
        {view === "my" ? (
          <>
            <MetricLedger
              className="my-summary personal-ledger"
              label="私人阅读概览"
              tabIndex={-1}
              items={[
                { label: "收藏", value: favoritesTotal.toLocaleString("zh-CN") },
                { label: "最近记录", value: history.length.toLocaleString("zh-CN") },
              ]}
            />
            <div className="my-links"><button type="button" onClick={() => activateView("settings")}><strong>阅读设置</strong><small>默认布局与翻页偏好</small><span>→</span></button></div>
            <div className="my-history">
              <SectionHeader title="阅读足迹" eyebrow={`RECENTLY READ · ${history.length}`} />
              {historyLoading && !history.length ? <CatalogSkeleton count={6} compact /> : historyError && !history.length ? <div className="catalog-inline-error" role="alert"><span>{historyError}</span><button type="button" onClick={() => setDataRevision((current) => current + 1)}>重新读取</button></div> : history.length ? <><div className="book-grid compact-grid">{history.slice(0, 6).map((item, index) => <BookCard key={itemID(item)} item={item} index={index} onOpen={openDetail} />)}</div>{historyLoading ? <p className="catalog-refresh-note" role="status">正在同步最新书签…</p> : null}</> : <Status kind="empty">还没有阅读足迹。打开一本书后，阅读位置会自动留在这里。</Status>}
            </div>
            <SectionHeader title="我的收藏" eyebrow={`FAVORITES · ${favoritesTotal}`} />
          </>
        ) : null}
        {view === "library" ? (
          <LibraryToolbar ref={catalogTopRef} disabled={!libraryPageStateReady || !libraryPageStateConfirmed || libraryPageEntryRefreshing} error={Boolean(catalogVisibleError)} loading={catalogBusy} mode={catalogMode} page={page} pages={pages} sort={sort} onModeChange={changeCatalogMode} onSortChange={changeCatalogSort} />
        ) : null}
        {listLoading ? <CatalogSkeleton count={view === "my" ? 10 : 18} className={view === "library" ? "library-grid" : ""} /> : listError ? <div className="catalog-state error" role="alert"><span>LIBRARY INTERRUPTED</span><h2>书架暂时没有整理好</h2><p>{listError}</p><button type="button" onClick={view === "my" ? retryFavoritesPage : retryCatalogPage}>重试</button></div> : visibleItems.length ? (
          <div className={`book-grid catalog-grid ${view === "library" ? "library-grid" : ""} ${view === "library" && catalogShowingStale ? "is-stale" : ""}`.trim()} aria-busy={view === "my" ? favoritesBusy : catalogBusy} aria-hidden={view === "library" && catalogShowingStale ? true : undefined} inert={view === "library" && catalogShowingStale ? true : undefined}>{visibleItems.map((item, index) => <BookCard key={itemID(item)} item={item} index={index + visibleItemsOffset} priority={view === "library" && index < 2} onOpen={openDetail} />)}</div>
        ) : view === "my" && favoritesBusy ? <Status>正在同步这一页收藏…</Status> : <Status kind="empty">{view === "my" ? "还没有收藏。在作品详情中点“收藏”，它会留在这里。" : "当前条件下还没有作品。"}</Status>}
        {view === "library" && catalogShowingStale ? <p className="catalog-refresh-note" role="status">正在载入这一页馆藏…</p> : null}
        {view === "my" && favoritesPageReady && visibleItems.length > 0 && favoritesBusy ? <p className="catalog-refresh-note" role="status">正在同步这一页收藏…</p> : null}
        {view === "my" && favoritesPageReady && favoritesError ? <div className="catalog-inline-error" role="alert"><span>当前保留上次读取的收藏：{favoritesError}</span><button type="button" onClick={retryFavoritesPage}>重新读取</button></div> : null}
        {view === "library" && catalogPageReady && !catalogBusy && catalogPresentationTotal > PAGE_SIZE ? (
          <Pagination disabled={!libraryPageStateConfirmed || libraryPageEntryRefreshing} page={page} pages={pages} label="书库分页" kicker="COLLECTION NAVIGATION" onPageChange={(nextPage) => changeCatalogPage((nextPage - 1) * PAGE_SIZE)} />
        ) : null}
        {view === "my" && favoritesTotal > FAVORITES_PAGE_SIZE ? (
          <Pagination page={favoritesPage} pages={favoritesPages} label="收藏分页" onPageChange={(nextPage) => changeFavoritesPage((nextPage - 1) * FAVORITES_PAGE_SIZE)} />
        ) : null}
      </section>
    );
  }, [activateView, catalogBusy, catalogHasPresentationPage, catalogMode, catalogPageReady, catalogPresentationItems, catalogPresentationOffset, catalogPresentationTotal, catalogShowingStale, catalogVisibleError, changeCatalogMode, changeCatalogPage, changeCatalogSort, changeDiscoverMode, changeFavoritesPage, changeReaderFitPreference, clearSearch, continueError, continueLoading, continueTarget, continuedHero, discover, discoverError, discoverErrorKind, discoverLoading, discoverMode, discoverModeInfo, favorites, favoritesBusy, favoritesError, favoritesLoading, favoritesOffset, favoritesPage, favoritesPageReady, favoritesPages, favoritesTotal, hero, heroDetail, heroMode, history, historyError, historyLoading, homeHeroError, homeHeroLoading, libraryPageEntryRefreshing, libraryPageStateConfirmed, libraryPageStateReady, offset, openDetail, openRandomDiscoverWork, openReader, page, pages, randomOpening, readerFitPreference, recent, recentError, recentLoading, retryCatalogPage, retryFavoritesPage, returnToMy, searchDraft, searchQuery, sort, totalWorks, view, visibleItems, visibleItemsOffset]);

  const closeDetailOnBackdrop = (event: MouseEvent<HTMLDivElement>) => {
    if (event.target === event.currentTarget && !detailBusy) closeDetail();
  };

  const seriesReaderTarget = detail?.kind === "series" ? seriesContinueItem(detail.data, detail.progress) : undefined;
  const seriesReaderNextItem = detail?.kind === "series" && seriesReaderTarget
    ? nextSeriesReadable(detail.data, seriesReaderTarget.candidate_id)
    : undefined;
  const seriesReaderProgress = detail?.kind === "series" && seriesReaderTarget && detail.progress?.candidate_id === seriesReaderTarget.candidate_id
    ? detail.progress
    : seriesReaderTarget ? progressFor(seriesReaderTarget) : null;
  const seriesAggregate = detail?.kind === "series" ? seriesAggregateProgress(detail.data) : null;
  const searchShortcutLabel = typeof navigator !== "undefined" && /Mac|iPhone|iPad|iPod/i.test(navigator.platform)
    ? "⌘ K"
    : "Ctrl K";
  const browseSurfaceInert = detailLayerOpen;
  const detailWorkCreators = detail?.kind === "work" ? workCreatorNames(detail.data) : [];
  const detailWorkSeries = detail?.kind === "work" ? workSeriesNames(detail.data) : [];
  const detailWorkTranslations = detail?.kind === "work" ? workTranslationNames(detail.data) : [];
  const detailRelatedEditionItems = detail?.kind === "work" ? uniqueRelatedWorks(detail.data.related?.editions, detail.data.work.candidate_id) : [];
  const detailRelatedSeriesItems = detail?.kind === "work" ? uniqueRelatedWorks(detail.data.related?.series, detail.data.work.candidate_id) : [];
  const detailRelatedCreatorItems = detail?.kind === "work" ? uniqueRelatedWorks(detail.data.related?.creators, detail.data.work.candidate_id) : [];

  return (
    <div className="app-shell">
      <div className="app-surface" aria-hidden={reader || readerLoading ? true : undefined} inert={reader || readerLoading ? true : undefined}>
      <a className="skip-link" href="#main-content" aria-hidden={browseSurfaceInert ? true : undefined} inert={browseSurfaceInert ? true : undefined}>跳到正文</a>
      <aside className="sidebar" aria-hidden={browseSurfaceInert ? true : undefined} inert={browseSurfaceInert ? true : undefined}>
        <Brand />
        <span className="sidebar-section-label">READING ROOM</span>
        <nav className="nav" aria-label="主要导航">
          {navItems.map((item) => <button type="button" className={`nav-item ${item.id === "my" ? "account-entry" : ""} ${view === item.id ? "active" : ""}`} aria-current={view === item.id ? "page" : undefined} onClick={() => activateView(item.id)} key={item.id}><small>{item.index}</small><strong>{item.label}</strong><span className="nav-arrow" aria-hidden="true">›</span></button>)}
        </nav>
        <div className="sidebar-spacer" />
        <span className="sidebar-section-label">PREFERENCES</span>
        <nav className="nav secondary-nav" aria-label="偏好导航">
          <button type="button" className={`nav-item ${view === "settings" ? "active" : ""}`} aria-current={view === "settings" ? "page" : undefined} onClick={() => activateView("settings")}><small>↳</small><strong>阅读设置</strong></button>
        </nav>
        <small className="library-count">{totalWorks > 0 ? `${totalWorks.toLocaleString("zh-CN")} 部作品` : "私人馆藏"}</small>
      </aside>

      <main ref={mainRef} id="main-content" className="main app-main" tabIndex={-1} aria-label={`${viewLabels[view]}主内容`} aria-hidden={browseSurfaceInert ? true : undefined} inert={browseSurfaceInert ? true : undefined}>
        <header className="topbar">
          <span className="room-title location-label">私人书架</span>
          <form className="search" role="search" onSubmit={submitSearch}>
            <input ref={searchRef} value={searchDraft} onChange={(event) => setSearchDraft(event.target.value)} placeholder="搜索作品、作者或汉化组" aria-label="搜索作品、作者或汉化组" />
            <kbd>{searchShortcutLabel}</kbd>
          </form>
          <span className="local-only">私人部署 <i>漫</i></span>
        </header>
        <div className="page-content">{content}</div>
      </main>

      <nav className="mobile-nav" aria-label="手机导航" aria-hidden={browseSurfaceInert ? true : undefined} inert={browseSurfaceInert ? true : undefined}>
        {navItems.map((item) => { const active = view === item.id || (item.id === "my" && view === "settings"); return <button type="button" className={active ? "active" : ""} aria-current={active ? "page" : undefined} onClick={() => activateView(item.id)} key={item.id}><span>{item.index}</span>{item.label}</button>; })}
      </nav>

      {detailLoading && !detail ? <div className="detail-overlay detail-loading-overlay" onMouseDown={(event) => { if (event.target === event.currentTarget) closeDetail(); }}><section className="detail-loading-card" role="dialog" aria-modal="true" aria-label="正在打开作品"><button ref={detailLoadingCloseRef} type="button" aria-label="取消打开作品" onClick={closeDetail}>×</button><span className="eyebrow">OPENING THE ARCHIVE</span><Status>正在打开作品…</Status><small>按 Esc 可取消</small></section></div> : null}
      {detailError ? <div className="toast error" role="alert"><span>{detailError}</span>{readerRetryIntent ? <button type="button" onClick={() => { const { item, requestedIndex, ...context } = readerRetryIntent; setDetailError(""); setReaderRetryIntent(null); openReader(item, requestedIndex, context); }}>重试打开</button> : null}<button type="button" onClick={() => { setDetailError(""); setReaderRetryIntent(null); }}>关闭</button></div> : null}
      {detail ? (
        <div className="detail-overlay" onMouseDown={closeDetailOnBackdrop}>
          <section ref={detailPanelRef} className="detail-panel" role="dialog" aria-modal="true" aria-hidden={readerLayerOpen ? true : undefined} inert={readerLayerOpen ? true : undefined} aria-labelledby="detail-heading" data-note-dirty={noteDirty ? "true" : "false"}>
            <DetailHeader ref={detailCloseRef} kind={detail.kind} title={cleanTitle(detail.kind === "work" ? itemTitle(detail.data.work) : itemTitle(detail.data.series))} busy={detailBusy} onClose={closeDetail} />
            {detail.kind === "work" ? (
              <div ref={detailScrollRef} className="detail-body detail-work">
                <div className="detail-work-intro detail-summary">
                  <DetailCoverFrame kind={`类型 · ${itemKindDisplayLabel(detail.data.work)}`} state={progressFor(detail.data.work) ? `READ ${Math.round(progressFor(detail.data.work)!.percent)}%` : "UNREAD"}>
                    <Cover item={detail.data.work} size={1200} eager />
                  </DetailCoverFrame>
                  <div className="detail-copy">
                    <span className="eyebrow">CATALOGUE ENTRY</span>
                    <h1 id="detail-heading">{cleanTitle(itemTitle(detail.data.work))}</h1>
                    {detailWorkCreators.length || detailWorkSeries.length || detailWorkTranslations.length || detailRelatedEditionItems.length ? (
                      <dl className="detail-identities" aria-label="作品元数据">
                        {detailRelatedEditionItems.length ? <div><dt>其他版本</dt><dd><button type="button" onClick={() => focusRelatedSection("detail-related-editions-title")}>查看 {detailRelatedEditionItems.length} 本同作品版本</button></dd></div> : null}
                        {detailWorkCreators.length ? <div><dt>作者／社团</dt><dd><span>{detailWorkCreators.join(" · ")}</span>{detailRelatedCreatorItems.length ? <button type="button" onClick={() => focusRelatedSection("detail-related-creators-title")}>查看同作者作品</button> : null}</dd></div> : null}
                        {detailWorkSeries.length ? <div><dt>系列</dt><dd><span>{detailWorkSeries.join(" · ")}</span>{detailRelatedSeriesItems.length ? <button type="button" onClick={() => focusRelatedSection("detail-related-series-title")}>查看同系列作品</button> : null}</dd></div> : null}
                        {detailWorkTranslations.length ? <div><dt>汉化／翻译</dt><dd><span>{detailWorkTranslations.join(" · ")}</span></dd></div> : null}
                      </dl>
                    ) : null}
                    <MetricLedger
                      className="detail-metric-ledger"
                      label="作品信息"
                      items={[
                        { label: "可读页数", value: numberValue(detail.data.work.readable_page_count) || "—" },
                        { label: "馆藏", value: detail.data.work.display_library_name || detail.data.work.library_name || "本地" },
                        { label: "阅读进度", value: progressFor(detail.data.work) ? `${Math.round(progressFor(detail.data.work)!.percent)}%` : "未读" },
                      ]}
                    />
                    <div className="detail-actions">
                      <button className="button primary" type="button" disabled={!detail.data.work.can_read} onClick={() => openReader(detail.data.work, progressFor(detail.data.work)?.completed ? 0 : undefined, { seriesID: detail.data.series?.[0]?.group_id || undefined })}>{progressFor(detail.data.work)?.completed ? "重新阅读" : progressFor(detail.data.work) ? "继续阅读" : "开始阅读"} <span>→</span></button>
                      <button className={`button favorite-button ${favoriteFor(detail.data.work, detail.data.mark) ? "active" : ""}`} type="button" aria-pressed={favoriteFor(detail.data.work, detail.data.mark)} disabled={favoriteSavingIDs.has(detail.data.work.candidate_id)} onClick={() => { const current = favoriteFor(detail.data.work, detail.data.mark); void changeFavorite(detail.data.work, !current, current); }}>{favoriteSavingIDs.has(detail.data.work.candidate_id) ? "保存中…" : favoriteFor(detail.data.work, detail.data.mark) ? "已收藏" : "收藏"}</button>
                    </div>
                  </div>
                </div>
                <RelatedWorks currentID={detail.data.work.candidate_id} editions={detail.data.related?.editions} series={detail.data.related?.series} creators={detail.data.related?.creators} onOpen={openDetail} />
                <Suspense fallback={<Status>正在准备个人标记…</Status>}>
                  <PersonalMarkPanel targetType="work" targetID={detail.data.work.candidate_id} mark={detail.data.mark} disabled={detailBusy} savingField={personalMarkSavingField} statusMessage={personalMarkStatus} onPatch={(payload, field) => { void saveDetailPersonalMark(payload, field); }} />
                </Suspense>
                <div className="detail-lower-grid">
                  <PersonalNoteEditor title="私人备注" placeholder="留一句只属于这本书的话…" value={noteDraft} savedValue={String(detail.data.mark?.notes || "")} saving={noteSaving} onChange={setNoteDraft} onReset={() => setNoteDraft(String(detail.data.mark?.notes || ""))} onSave={() => { void savePersonalNote(); }} />
                  <section className="detail-section detail-about"><span>LOCAL SOURCE</span><h2>关于这本</h2><p>{detail.data.work.relative_path || "本地书库作品"}</p></section>
                </div>
              </div>
            ) : (
              <div ref={detailScrollRef} className="detail-body series-detail">
                <div className="series-detail-intro">
                  <DetailCoverFrame kind="SERIES" state={seriesAggregate?.readPages ? `READ ${Math.round(seriesAggregate.percent)}%` : "UNREAD"}>
                    <Cover item={detail.data.series} size={1200} eager />
                  </DetailCoverFrame>
                  <div className="detail-copy">
                    <span className="eyebrow">CATALOGUE ENTRY · SERIES</span>
                    <h1 id="detail-heading">{cleanTitle(itemTitle(detail.data.series))}</h1>
                    <p>{detail.data.sectioned ? detail.data.section_summary : (detail.data.series.item_summary || detail.data.section_summary)}</p>
                    <MetricLedger
                      className="detail-metric-ledger"
                      label="系列信息"
                      items={[
                        { label: "条目", value: numberValue(detail.data.series.counted_items, numberValue(detail.data.series.item_count)) || "—" },
                        { label: "分区", value: numberValue(detail.data.series.section_count) || (detail.data.sectioned ? detail.data.sections.length : 1) },
                        { label: "馆藏进度", value: seriesAggregate?.readPages ? `${Math.round(seriesAggregate.percent)}%` : "未读" },
                      ]}
                    />
                    <div className="detail-actions">
                      {seriesReaderTarget ? <button className="button primary" type="button" onClick={() => openReader(seriesReaderTarget, seriesReaderProgress?.completed ? 0 : undefined, { seriesID: detail.data.series.group_id, nextItem: seriesReaderNextItem })}>{seriesReaderProgress?.completed ? "重新阅读" : seriesReaderProgress ? "继续阅读" : `从${chapterLabel(seriesReaderTarget)}开始`} <span>→</span></button> : null}
                      <button className={`button favorite-button ${favoriteFor(detail.data.series, detail.data.mark) ? "active" : ""}`} type="button" aria-pressed={favoriteFor(detail.data.series, detail.data.mark)} disabled={favoriteSavingIDs.has(detail.data.series.group_id)} onClick={() => { const current = favoriteFor(detail.data.series, detail.data.mark); void changeFavorite(detail.data.series, !current, current); }}>{favoriteSavingIDs.has(detail.data.series.group_id) ? "保存中…" : favoriteFor(detail.data.series, detail.data.mark) ? "已收藏系列" : "收藏系列"}</button>
                    </div>
                    {seriesReaderTarget ? <p className="series-resume-location"><span>{seriesReaderProgress ? `继续位置：${chapterLabel(seriesReaderTarget)} · 第 ${seriesReaderProgress.index + 1}${seriesReaderProgress.count ? ` / ${seriesReaderProgress.count}` : ""} 页` : `阅读起点：${chapterLabel(seriesReaderTarget)}`}</span><button type="button" onClick={() => window.requestAnimationFrame(() => document.getElementById("series-current-entry")?.scrollIntoView({ behavior: preferredScrollBehavior(), block: "center" }))}>在目录中定位</button></p> : null}
                  </div>
                </div>
                <AsyncRegionBoundary resetKey={`series-directory-${detail.data.series.group_id}`} title="章节目录暂时没有载入" copy="详情仍然保留。可能刚好遇到版本更新或短暂网络中断，请重新加载后再试。">
                  <Suspense fallback={<div className="series-directory-loading" role="status">正在排印章节目录…</div>}>
                    <SeriesDirectory key={detail.data.series.group_id} data={detail.data} activeCandidateID={seriesReaderTarget?.candidate_id} onOpen={(item, nextItem) => { const progress = progressFor(item); openReader(item, progress?.completed ? 0 : undefined, { seriesID: detail.data.series.group_id, nextItem }); }} />
                  </Suspense>
                </AsyncRegionBoundary>
                <Suspense fallback={<Status>正在准备系列标记…</Status>}>
                  <PersonalMarkPanel targetType="series" targetID={detail.data.series.group_id} mark={detail.data.mark} disabled={detailBusy} savingField={personalMarkSavingField} statusMessage={personalMarkStatus} onPatch={(payload, field) => { void saveDetailPersonalMark(payload, field); }} />
                </Suspense>
                <PersonalNoteEditor title="系列备注" placeholder="记录这个系列值得记住的地方…" value={noteDraft} savedValue={String(detail.data.mark?.notes || "")} saving={noteSaving} onChange={setNoteDraft} onReset={() => setNoteDraft(String(detail.data.mark?.notes || ""))} onSave={() => { void savePersonalNote(); }} />
              </div>
            )}
          </section>
        </div>
      ) : null}

      </div>

      {toast ? <div className={`toast action-toast ${toast.kind === "error" ? "error" : "success"}`} role={toast.kind === "error" ? "alert" : "status"} aria-live="polite"><span>{toast.message}</span>{toast.actionLabel && toast.onAction ? <button type="button" onClick={() => { const action = toast.onAction; setToast(null); action?.(); }}>{toast.actionLabel}</button> : null}<button type="button" aria-label="关闭提示" onClick={() => setToast(null)}>×</button></div> : null}

      {readerLoading ? <section className="reader reader-loading" role="dialog" aria-modal="true" aria-label="正在准备阅读"><button ref={readerLoadingCloseRef} type="button" className="reader-loading-close" autoFocus onClick={closeReader} aria-label="取消并退出阅读">×</button><Status>正在整理这本书的页序…</Status></section> : null}
      {reader ? (
        <section ref={readerDialogRef} className={`reader ${reader.fitMode} ${readerSplitWideActive ? "split-wide-active" : ""} ${reader.chromeVisible || reader.calibration || reader.ending ? "" : "reader-chrome-hidden"}`} role="dialog" aria-modal="true" aria-label={`正在阅读 ${itemTitle(reader.item)}`} data-candidate-id={reader.item.candidate_id} data-next-candidate-id={reader.nextItem?.candidate_id || ""} data-split-wide-active={readerSplitWideActive ? "true" : "false"} data-split-panel={reader.splitPanel} onMouseMove={handleReaderPointerMove}>
          <span className="sr-only" role="status" aria-live="polite" aria-atomic="true">{readerLiveStatus}</span>
          <ReaderTopbar
            ref={readerCloseRef}
            title={cleanTitle(itemTitle(reader.item))}
            kind={reader.seriesID ? "SERIES" : itemKindLabel(reader.item)}
            currentIndex={reader.index}
            requestedIndex={reader.requestedIndex}
            pageCount={reader.pages.count}
            imageLoading={reader.imageLoading}
            inactive={Boolean(reader.calibration)}
            onClose={closeReader}
            onReveal={() => revealReaderChrome()}
          />
          {reader.calibration ? <aside ref={readerCalibrationRef} className="reader-calibration" role="alertdialog" aria-modal="true" aria-busy={reader.calibrationSaving} aria-labelledby="reader-calibration-title" aria-describedby="reader-calibration-copy" tabIndex={-1}><span>READING POSITION CHECK</span><strong id="reader-calibration-title">旧书签需要重新确认</strong><p id="reader-calibration-copy">旧记录在第 {reader.calibration.oldIndex + 1} / {reader.calibration.oldCount || "?"} 页。当前显示第 {reader.index + 1} / {reader.pages.count} 页；你可以先微调页码，确认前不会覆盖原有进度。</p><span className="sr-only" role="status" aria-live="polite" aria-atomic="true">{reader.calibrationSaving ? "正在确认阅读位置，请稍候。" : reader.imageLoading ? `正在载入第 ${reader.requestedIndex + 1} 页，共 ${reader.pages.count} 页。` : `当前是第 ${reader.index + 1} 页，共 ${reader.pages.count} 页。`}</span>{reader.error ? <div className="reader-calibration-error" role="alert"><span>{reader.error}</span><button type="button" onClick={retryReaderPage}>重试当前页</button></div> : null}<div className="reader-calibration-pages"><button type="button" disabled={reader.calibrationSaving || reader.requestedIndex <= 0} onClick={() => goToReaderPage(reader.requestedIndex - 1)}>← 前一页</button><button type="button" disabled={reader.calibrationSaving || reader.requestedIndex >= reader.pages.count - 1} onClick={() => goToReaderPage(reader.requestedIndex + 1)}>后一页 →</button></div><div><button type="button" disabled={reader.calibrationSaving} onClick={closeReader}>先退出</button><button ref={readerCalibrationPrimaryRef} type="button" className="primary" disabled={reader.calibrationSaving || reader.imageLoading || Boolean(reader.error)} onClick={() => { void confirmReaderCalibration(); }}>{reader.calibrationSaving ? "正在确认…" : "以当前页继续"}</button></div></aside> : null}
          <div
            ref={readerStageRef}
            className={`reader-stage ${reader.fitMode} ${readerSplitWideActive ? "split-wide-active" : ""} ${readerVisualLoading ? "is-loading" : ""}`}
            data-split-wide-active={readerSplitWideActive ? "true" : "false"}
            data-split-panel={reader.splitPanel}
            tabIndex={reader.calibration ? -1 : 0}
            aria-busy={reader.imageLoading}
            aria-hidden={reader.calibration ? true : undefined}
            inert={reader.calibration ? true : undefined}
            aria-label="漫画页面。左右方向键翻页，上下方向键滚动或翻页，按 Tab 进入阅读控制。"
            onClick={handleReaderStageClick}
            onScroll={handleReaderScroll}
            onTouchStart={(event) => {
              if (event.touches.length !== 1) {
                readerTouchStartRef.current = null;
                return;
              }
              const touch = event.touches[0];
              readerTouchStartRef.current = touch && touch.clientX > 24 ? { x: touch.clientX, y: touch.clientY } : null;
            }}
            onTouchMove={(event) => { if (event.touches.length !== 1) readerTouchStartRef.current = null; }}
            onTouchEnd={(event) => {
              const start = readerTouchStartRef.current;
              const touch = event.changedTouches[0];
              readerTouchStartRef.current = null;
              if (!start || !touch) return;
              const stage = readerStageRef.current;
              if (uiRef.current?.reader?.fitMode === "fit-width" && stage && stage.scrollWidth > stage.clientWidth + 8) return;
              const dx = touch.clientX - start.x;
              const dy = touch.clientY - start.y;
              if (Math.abs(dx) < 58 || Math.abs(dx) < Math.abs(dy) * 1.15) return;
              readerSuppressClickRef.current = true;
              if (readerSuppressClickTimerRef.current !== null) window.clearTimeout(readerSuppressClickTimerRef.current);
              readerSuppressClickTimerRef.current = window.setTimeout(() => {
                readerSuppressClickRef.current = false;
                readerSuppressClickTimerRef.current = null;
              }, 520);
              moveReader(dx < 0 ? 1 : -1, false);
            }}
            onTouchCancel={() => { readerTouchStartRef.current = null; }}
          >
            {reader.ending ? (
              <article ref={readerEndingRef} className="reader-ending-card" tabIndex={-1}>
                <h2>本话读完</h2>
                <p>{reader.nextItem ? `下一话是《${cleanTitle(itemTitle(reader.nextItem))}》，可以直接接着读。` : "这已经是当前目录里最后一个可读条目。"}</p>
                <div className="reader-ending-actions"><button type="button" onClick={() => moveReader(-1)}>返回末页</button>{reader.nextItem ? <button className="primary" type="button" onClick={() => openNextReader()}>阅读下一话</button> : null}<button type="button" onClick={closeReader}>退出阅读</button></div>
              </article>
            ) : (
              <>
                {reader.imageURL ? <img key={reader.imageURL} className={`reader-image ${readerSplitWideActive ? "is-split-wide" : ""}`} src={reader.imageURL} alt={`第 ${reader.index + 1} 页${readerSplitWideActive ? reader.splitPanel === 0 ? "右半页" : "左半页" : ""}`} draggable={false} style={readerSplitImageStyle} /> : null}
                {readerVisualLoading ? <div className="reader-loading-layer"><span>正在显影第 {reader.requestedIndex + 1} 页…</span></div> : null}
                {reader.error ? <div className="reader-error-layer" role="alert"><div><h3>这一页没有顺利打开</h3><p>{reader.error}</p><div className="reader-error-actions"><button type="button" onClick={retryReaderPage}>重试本页</button>{reader.requestedIndex > 0 ? <button type="button" onClick={() => moveReader(-1)}>上一页</button> : null}{reader.requestedIndex < reader.pages.count - 1 ? <button type="button" onClick={() => moveReader(1)}>下一页</button> : null}<button type="button" onClick={closeReader}>退出</button></div></div></div> : null}
              </>
            )}
          </div>
          <ReaderControls
            fitMode={reader.fitMode}
            pageDraft={readerPageDraft}
            pageCount={reader.pages.count}
            requestedIndex={reader.requestedIndex}
            calibrationOpen={Boolean(reader.calibration)}
            ending={reader.ending}
            hasNextItem={Boolean(reader.nextItem)}
            imageLoading={reader.imageLoading}
            splitWideActive={readerSplitWideActive}
            splitPanel={reader.splitPanel}
            pendingProgressCount={pendingProgressTotal}
            inactive={Boolean(reader.calibration)}
            onPrevious={() => moveReader(-1)}
            onNext={() => moveReader(1)}
            onFirst={() => goToReaderPage(0, false, 0)}
            onLast={() => goToReaderPage(reader.pages.count - 1, false, 0)}
            onOpenNextItem={reader.nextItem ? () => openNextReader() : undefined}
            nextItemLabel={reader.nextItem ? readerNextItemLabel(reader.nextItem) : undefined}
            onFitChange={changeReaderFit}
            onPageDraftChange={setReaderPageDraft}
            onPageDraftCommit={commitReaderPageDraft}
            onReveal={() => revealReaderChrome()}
          />
          <ReaderProgress currentIndex={reader.index} pageCount={reader.pages.count} />
        </section>
      ) : null}
    </div>
  );
}

export default App;
