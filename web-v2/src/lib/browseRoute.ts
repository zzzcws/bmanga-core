export type View = "home" | "library" | "discover" | "search" | "my" | "settings";
export type CatalogMode = "all" | "doujin" | "series";
export type CatalogSort = "added_desc" | "title_asc" | "pages_desc";
export type DiscoverMode = "unread" | "reading" | "liked" | "reread" | "any";

export interface BrowseRouteState {
  view: View;
  catalogMode: CatalogMode;
  sort: CatalogSort;
  offset: number;
  searchQuery: string;
  discoverMode: DiscoverMode;
}

export type BrowseScopeState = Partial<Record<View, BrowseRouteState>>;

export interface LibraryPageScopes {
  sort: CatalogSort;
  offsets: Partial<Record<CatalogMode, number>>;
}

export const CATALOG_PAGE_SIZE = 18;
export const FAVORITES_PAGE_SIZE = 10;
export const BROWSE_SCOPE_STORAGE_NAME = "bmanga.v2.browseScopes.v1";
export const LIBRARY_PAGE_SCOPES_STORAGE_NAME = "bmanga.v2.libraryPageScopes.v1";
export const LEGACY_LIBRARY_PAGE_SCOPES_STORAGE_NAME = "bmanga.browseScopeState.v1";
export const LEGACY_LIBRARY_PAGE_SCOPES_MIGRATION_KEY = "bmanga.v2.libraryPageScopes.legacyMigrated.v1";

const MAX_OFFSET = 1_000_000;
const MAX_QUERY_LENGTH = 500;
const OWNED_PARAMS = ["view", "kind", "sort", "offset", "q", "discover"] as const;
const VIEWS = new Set<View>(["home", "library", "discover", "search", "my", "settings"]);
const CATALOG_MODES = new Set<CatalogMode>(["all", "doujin", "series"]);
const CATALOG_SORTS = new Set<CatalogSort>(["added_desc", "title_asc", "pages_desc"]);
const DISCOVER_MODES = new Set<DiscoverMode>(["unread", "reading", "liked", "reread", "any"]);

function enumValue<T extends string>(value: unknown, allowed: Set<T>, fallback: T): T {
  return typeof value === "string" && allowed.has(value as T) ? value as T : fallback;
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function pageSize(view: View): number {
  return view === "my" ? FAVORITES_PAGE_SIZE : CATALOG_PAGE_SIZE;
}

function alignedOffset(value: unknown, view: View): number {
  const numeric = typeof value === "number" ? value : Number.parseInt(stringValue(value), 10);
  if (!Number.isFinite(numeric) || numeric <= 0) return 0;
  const size = pageSize(view);
  return Math.floor(Math.min(MAX_OFFSET, numeric) / size) * size;
}

function recordValue(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

function sanitizeLibraryPageScopes(value: unknown, fallbackSort: CatalogSort): LibraryPageScopes {
  const source = recordValue(value);
  const sort = enumValue(source.sort, CATALOG_SORTS, fallbackSort);
  const sourceOffsets = recordValue(source.offsets);
  const offsets: Partial<Record<CatalogMode, number>> = {};
  for (const mode of CATALOG_MODES) {
    if (mode in sourceOffsets) offsets[mode] = alignedOffset(sourceOffsets[mode], "library");
  }
  return { sort, offsets };
}

export function defaultLibraryPageScopes(sort: CatalogSort = "added_desc"): LibraryPageScopes {
  return sanitizeLibraryPageScopes({ sort, offsets: {} }, sort);
}

export function parseLibraryPageScopes(raw: string | null, fallbackSort: CatalogSort = "added_desc"): LibraryPageScopes {
  if (!raw) return defaultLibraryPageScopes(fallbackSort);
  try {
    return sanitizeLibraryPageScopes(JSON.parse(raw), fallbackSort);
  } catch {
    return defaultLibraryPageScopes(fallbackSort);
  }
}

export function serializeLibraryPageScopes(scopes: LibraryPageScopes): string {
  return JSON.stringify(sanitizeLibraryPageScopes(scopes, scopes.sort));
}

export function rememberLibraryPageScope(scopes: LibraryPageScopes, value: BrowseRouteState): LibraryPageScopes {
  const route = sanitizeBrowseRoute(value);
  const current = sanitizeLibraryPageScopes(scopes, route.sort);
  if (route.view !== "library") return current;
  const offsets = current.sort === route.sort ? { ...current.offsets } : {};
  offsets[route.catalogMode] = route.offset;
  return { sort: route.sort, offsets };
}

export function libraryPageScopeOffset(
  scopes: LibraryPageScopes | null | undefined,
  mode: CatalogMode,
  sort: CatalogSort,
): number {
  if (!scopes) return 0;
  const current = sanitizeLibraryPageScopes(scopes, sort);
  return current.sort === sort ? current.offsets[mode] || 0 : 0;
}

function legacyBrowsePage(source: Record<string, unknown>): number {
  const savedPage = Number(source.bmangaPage);
  if (Number.isFinite(savedPage) && savedPage > 0) return Math.max(1, Math.floor(savedPage));
  const offset = Math.max(0, Number(source.bmangaOffset) || 0);
  const savedLimit = Number(source.bmangaLimit);
  const limit = Number.isFinite(savedLimit) && savedLimit > 0 ? savedLimit : 60;
  return Math.max(1, Math.floor(offset / limit) + 1);
}

function legacyBrowseFilterState(source: Record<string, unknown>): { sort: string; filtered: boolean } | null {
  let signature: Record<string, unknown> = {};
  const rawSignature = source.bmangaFilterSignature;
  if (typeof rawSignature === "string" && rawSignature.trim()) {
    try {
      signature = recordValue(JSON.parse(rawSignature));
    } catch {
      return null;
    }
  } else if (rawSignature && typeof rawSignature === "object") {
    signature = recordValue(rawSignature);
  } else if (rawSignature !== undefined && rawSignature !== null && rawSignature !== "") {
    return null;
  }
  const value = (direct: string, legacy: string): string => (
    Object.prototype.hasOwnProperty.call(source, direct)
      ? stringValue(source[direct]).trim()
      : stringValue(signature[legacy]).trim()
  );
  const sort = value("bmangaSort", "sort") || "added_desc";
  const filterMappings: Array<[string, string]> = [
    ["bmangaSearch", "search"],
    ["bmangaLibrary", "library"],
    ["bmangaSource", "source"],
    ["bmangaPageStatus", "pageStatus"],
    ["bmangaAction", "action"],
    ["bmangaUserMark", "userMark"],
    ["bmangaTag", "tag"],
    ["bmangaTagQuick", "tagQuick"],
  ];
  const filtered = filterMappings.some(([direct, legacy]) => Boolean(value(direct, legacy)));
  return { sort, filtered };
}

export function migrateLegacyLibraryPageScopes(
  raw: string | null,
  sort: CatalogSort = "added_desc",
): LibraryPageScopes {
  const migrated = defaultLibraryPageScopes(sort);
  if (!raw) return migrated;
  try {
    const source = recordValue(JSON.parse(raw));
    const mappings: Array<[CatalogMode, string]> = [
      ["all", "shelf:"],
      ["doujin", "works:doujin"],
      ["series", "series:"],
    ];
    for (const [mode, key] of mappings) {
      const legacy = recordValue(source[key]);
      if (!Object.keys(legacy).length) continue;
      const filters = legacyBrowseFilterState(legacy);
      if (!filters || filters.filtered) continue;
      if (!CATALOG_SORTS.has(filters.sort as CatalogSort) || filters.sort !== migrated.sort) continue;
      migrated.offsets[mode] = alignedOffset((legacyBrowsePage(legacy) - 1) * CATALOG_PAGE_SIZE, "library");
    }
    return migrated;
  } catch {
    return migrated;
  }
}

export function mergeLegacyLibraryPageScopes(
  current: LibraryPageScopes,
  legacy: LibraryPageScopes,
): LibraryPageScopes {
  const safeCurrent = sanitizeLibraryPageScopes(current, current.sort);
  const safeLegacy = sanitizeLibraryPageScopes(legacy, safeCurrent.sort);
  if (safeLegacy.sort !== safeCurrent.sort) return safeCurrent;
  const offsets = { ...safeCurrent.offsets };
  for (const mode of CATALOG_MODES) {
    const currentOffset = offsets[mode] || 0;
    const legacyOffset = safeLegacy.offsets[mode] || 0;
    if (currentOffset <= 0 && legacyOffset > 0) offsets[mode] = legacyOffset;
  }
  return { sort: safeCurrent.sort, offsets };
}

export function defaultBrowseRoute(view: View = "home"): BrowseRouteState {
  return {
    view,
    catalogMode: "all",
    sort: "added_desc",
    offset: 0,
    searchQuery: "",
    discoverMode: "unread",
  };
}

export function sanitizeBrowseRoute(value: Partial<BrowseRouteState> | Record<string, unknown>): BrowseRouteState {
  const source = recordValue(value);
  const view = enumValue(source.view, VIEWS, "home");
  const catalogView = view === "library" || view === "search";
  return {
    view,
    catalogMode: catalogView ? enumValue(source.catalogMode, CATALOG_MODES, "all") : "all",
    sort: catalogView ? enumValue(source.sort, CATALOG_SORTS, "added_desc") : "added_desc",
    offset: view === "library" || view === "search" || view === "my" ? alignedOffset(source.offset, view) : 0,
    searchQuery: view === "search" ? stringValue(source.searchQuery).trim().slice(0, MAX_QUERY_LENGTH) : "",
    discoverMode: view === "discover" ? enumValue(source.discoverMode, DISCOVER_MODES, "unread") : "unread",
  };
}

export function parseBrowseURL(input: string | URL): BrowseRouteState {
  const url = input instanceof URL ? new URL(input.href) : new URL(input, "http://bmanga.invalid");
  const params = url.searchParams;
  const pathMatch = url.pathname.match(/\/v2\/(home|library|discover|search|my|settings)\/?$/u);
  const pathView = pathMatch?.[1] || "";
  const requestedView = params.get("view");
  const inferredView = params.has("q") ? "search" : params.has("discover") ? "discover" : "home";
  const view = enumValue(pathView || requestedView || inferredView, VIEWS, "home");
  return sanitizeBrowseRoute({
    view,
    catalogMode: params.get("kind") || "all",
    sort: params.get("sort") || "added_desc",
    offset: params.get("offset") || "0",
    searchQuery: params.get("q") || "",
    discoverMode: params.get("discover") || "unread",
  });
}

export function serializeBrowseURL(input: string | URL, value: BrowseRouteState): string {
  const url = input instanceof URL ? new URL(input.href) : new URL(input, "http://bmanga.invalid");
  const route = sanitizeBrowseRoute(value);
  for (const name of OWNED_PARAMS) url.searchParams.delete(name);
  const baseMatch = url.pathname.match(/^(.*\/v2)(?:\/.*)?$/u);
  const basePath = baseMatch?.[1] || "/v2";
  url.pathname = route.view === "home" ? `${basePath}/` : `${basePath}/${route.view}`;
  if (route.view === "library" || route.view === "search") {
    if (route.catalogMode !== "all") url.searchParams.set("kind", route.catalogMode);
    if (route.sort !== "added_desc") url.searchParams.set("sort", route.sort);
    if (route.offset > 0) url.searchParams.set("offset", String(route.offset));
    if (route.view === "search" && route.searchQuery) url.searchParams.set("q", route.searchQuery);
  } else if (route.view === "discover") {
    if (route.discoverMode !== "unread") url.searchParams.set("discover", route.discoverMode);
  } else if (route.view === "my") {
    if (route.offset > 0) url.searchParams.set("offset", String(route.offset));
  }
  return `${url.pathname}${url.search}${url.hash}`;
}

export function browseRouteKey(value: BrowseRouteState): string {
  return `browse:${serializeBrowseURL("http://bmanga.invalid/v2/", value)}`;
}

export function parseBrowseScopes(raw: string | null): BrowseScopeState {
  if (!raw) return {};
  try {
    const parsed = recordValue(JSON.parse(raw));
    const scopes: BrowseScopeState = {};
    for (const view of VIEWS) {
      if (view === "home" || !(view in parsed)) continue;
      scopes[view] = sanitizeBrowseRoute({ ...recordValue(parsed[view]), view });
    }
    return scopes;
  } catch {
    return {};
  }
}

export function serializeBrowseScopes(scopes: BrowseScopeState): string {
  const safe: BrowseScopeState = {};
  for (const view of VIEWS) {
    const route = scopes[view];
    if (view !== "home" && route) safe[view] = sanitizeBrowseRoute({ ...route, view });
  }
  return JSON.stringify(safe);
}
