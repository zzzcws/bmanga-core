import type { CatalogMode, CatalogSort } from "./browseRoute";

export interface CatalogCacheScope {
  dataRevision: number;
  requestRevision: number;
  view: "library" | "search";
  library?: string;
  catalogMode: CatalogMode;
  sort: CatalogSort;
  searchQuery?: string;
  limit: number;
}

export interface CatalogPage<T> {
  items: T[];
  total: number;
  offset: number;
}

export interface CatalogPagePayload<T> {
  items?: T[] | null;
  total?: unknown;
  offset?: unknown;
}

function nonNegativeInteger(value: unknown, fallback = 0): number {
  const numeric = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(numeric)) return fallback;
  return Math.max(0, Math.floor(numeric));
}

function normalizedLimit(value: unknown): number {
  return Math.max(1, nonNegativeInteger(value, 1));
}

export function alignedCatalogOffset(offset: unknown, limit: unknown): number {
  const safeLimit = normalizedLimit(limit);
  return Math.floor(nonNegativeInteger(offset) / safeLimit) * safeLimit;
}

export function catalogCacheScopeKey(scope: CatalogCacheScope): string {
  const view = scope.view === "search" ? "search" : "library";
  const searchQuery = view === "search" ? String(scope.searchQuery || "").trim() : "";
  return JSON.stringify([
    "catalog-v1",
    nonNegativeInteger(scope.dataRevision),
    view,
    String(scope.library || "").trim(),
    scope.catalogMode,
    scope.sort,
    searchQuery,
    normalizedLimit(scope.limit),
  ]);
}

export function catalogPageCacheKey(scope: CatalogCacheScope, offset: unknown): string {
  return `${catalogCacheScopeKey(scope)}:${alignedCatalogOffset(offset, scope.limit)}`;
}

export function canPrefetchCatalog(scope: CatalogCacheScope): boolean {
  return scope.view !== "search" || Boolean(String(scope.searchQuery || "").trim());
}

export function adjacentCatalogOffsets<T>(scope: CatalogCacheScope, page: CatalogPage<T>): number[] {
  if (!canPrefetchCatalog(scope)) return [];
  const nextOffset = alignedCatalogOffset(page.offset, scope.limit) + normalizedLimit(scope.limit);
  return nextOffset < nonNegativeInteger(page.total) ? [nextOffset] : [];
}

export function normalizeCatalogPage<T>(
  payload: CatalogPagePayload<T>,
  requestedOffset: unknown,
  limit: unknown,
): CatalogPage<T> {
  const safeLimit = normalizedLimit(limit);
  const total = nonNegativeInteger(payload.total);
  const maxOffset = total > 0 ? Math.floor((total - 1) / safeLimit) * safeLimit : 0;
  const fallbackOffset = alignedCatalogOffset(requestedOffset, safeLimit);
  const returnedOffset = alignedCatalogOffset(payload.offset ?? fallbackOffset, safeLimit);
  return {
    items: Array.isArray(payload.items) ? payload.items.slice() : [],
    total,
    offset: Math.min(maxOffset, returnedOffset),
  };
}

export class CatalogPageCache<T> {
  readonly maxEntries: number;
  private readonly entries = new Map<string, T>();

  constructor(maxEntries = 8) {
    this.maxEntries = Math.max(1, nonNegativeInteger(maxEntries, 8));
  }

  get size(): number {
    return this.entries.size;
  }

  has(key: string): boolean {
    return this.entries.has(key);
  }

  peek(key: string): T | undefined {
    return this.entries.get(key);
  }

  get(key: string): T | undefined {
    const value = this.entries.get(key);
    if (value === undefined) return undefined;
    this.entries.delete(key);
    this.entries.set(key, value);
    return value;
  }

  set(key: string, value: T): void {
    this.entries.delete(key);
    this.entries.set(key, value);
    while (this.entries.size > this.maxEntries) {
      const oldest = this.entries.keys().next().value;
      if (oldest === undefined) break;
      this.entries.delete(oldest);
    }
  }

  delete(key: string): boolean {
    return this.entries.delete(key);
  }

  updateAll(updater: (value: T, key: string) => T): void {
    const updates = Array.from(this.entries, ([key, value]) => [key, updater(value, key)] as const);
    for (const [key, value] of updates) {
      this.entries.set(key, value);
    }
  }

  clear(): void {
    this.entries.clear();
  }
}

export function mergeCatalogResponse<T>(
  cache: CatalogPageCache<CatalogPage<T>>,
  scope: CatalogCacheScope,
  requestedOffset: unknown,
  payload: CatalogPagePayload<T>,
): { page: CatalogPage<T>; exact: boolean } {
  const expectedOffset = alignedCatalogOffset(requestedOffset, scope.limit);
  const page = normalizeCatalogPage(payload, expectedOffset, scope.limit);
  const exact = page.offset === expectedOffset;
  // A request can become out of range while it is in flight (for example when
  // the total shrinks). The server intentionally returns an empty page at the
  // clamped offset. Never store that response under the last valid page key:
  // the caller first repairs the route, then fetches or reuses the real page.
  if (exact) cache.set(catalogPageCacheKey(scope, expectedOffset), page);
  return { page, exact };
}

export const FAVORITES_CACHE_FRESH_MS = 30_000;

export interface FavoritesCacheScope {
  dataRevision: number;
  requestRevision: number;
  limit: number;
}

export interface FavoritesPage<T> extends CatalogPage<T> {
  cachedAt: number;
}

export function favoritesCacheScopeKey(scope: FavoritesCacheScope): string {
  return JSON.stringify([
    "favorites-v1",
    nonNegativeInteger(scope.dataRevision),
    normalizedLimit(scope.limit),
  ]);
}

export function favoritesPageCacheKey(scope: FavoritesCacheScope, offset: unknown): string {
  return `${favoritesCacheScopeKey(scope)}:${alignedCatalogOffset(offset, scope.limit)}`;
}

export function nextFavoritesOffset<T>(scope: FavoritesCacheScope, page: CatalogPage<T>): number | null {
  const nextOffset = alignedCatalogOffset(page.offset, scope.limit) + normalizedLimit(scope.limit);
  return nextOffset < nonNegativeInteger(page.total) ? nextOffset : null;
}

export function isFavoritesPageFresh<T>(page: FavoritesPage<T>, now = Date.now(), maxAge = FAVORITES_CACHE_FRESH_MS): boolean {
  return Math.max(0, now - page.cachedAt) <= Math.max(0, maxAge);
}

export function selectFavoritesDisplayItems<T>(
  requestedOffset: unknown,
  limit: unknown,
  displayedOffset: unknown,
  displayedItems: T[],
  cachedPage?: FavoritesPage<T>,
): T[] | null {
  const requested = alignedCatalogOffset(requestedOffset, limit);
  if (cachedPage?.offset === requested) return cachedPage.items;
  const numericDisplayedOffset = Number(displayedOffset);
  if (Number.isFinite(numericDisplayedOffset) && numericDisplayedOffset >= 0
    && alignedCatalogOffset(numericDisplayedOffset, limit) === requested) {
    return displayedItems;
  }
  return null;
}

export function mergeFavoritesResponse<T>(
  cache: CatalogPageCache<FavoritesPage<T>>,
  scope: FavoritesCacheScope,
  requestedOffset: unknown,
  payload: CatalogPagePayload<T>,
  now = Date.now(),
): { page: FavoritesPage<T>; exact: boolean } {
  const expectedOffset = alignedCatalogOffset(requestedOffset, scope.limit);
  const normalized = normalizeCatalogPage(payload, expectedOffset, scope.limit);
  const exact = normalized.offset === expectedOffset;
  const page: FavoritesPage<T> = { ...normalized, cachedAt: now };
  if (exact) cache.set(favoritesPageCacheKey(scope, expectedOffset), page);
  return { page, exact };
}
