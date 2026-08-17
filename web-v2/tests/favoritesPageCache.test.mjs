import assert from "node:assert/strict";
import test from "node:test";

import {
  CatalogPageCache,
  favoritesCacheScopeKey,
  favoritesPageCacheKey,
  isFavoritesPageFresh,
  mergeFavoritesResponse,
  nextFavoritesOffset,
  selectFavoritesDisplayItems,
} from "../src/lib/catalogPageCache.ts";

function scope(overrides = {}) {
  return {
    dataRevision: 3,
    requestRevision: 2,
    limit: 20,
    ...overrides,
  };
}

test("收藏缓存键覆盖数据版本并对齐 offset", () => {
  assert.equal(favoritesPageCacheKey(scope(), 21), favoritesPageCacheKey(scope(), 20));
  assert.notEqual(favoritesCacheScopeKey(scope()), favoritesCacheScopeKey(scope({ dataRevision: 4 })));
  assert.equal(favoritesCacheScopeKey(scope()), favoritesCacheScopeKey(scope({ requestRevision: 3 })));
  assert.notEqual(favoritesCacheScopeKey(scope()), favoritesCacheScopeKey(scope({ limit: 10 })));
});

test("收藏只预取下一页，末页不会继续预取", () => {
  assert.equal(nextFavoritesOffset(scope(), { items: ["a"], total: 55, offset: 0 }), 20);
  assert.equal(nextFavoritesOffset(scope(), { items: ["b"], total: 55, offset: 20 }), 40);
  assert.equal(nextFavoritesOffset(scope(), { items: ["c"], total: 55, offset: 40 }), null);
});

test("收藏响应只按精确请求页写入共享 LRU，越界响应不会污染末页", () => {
  const cache = new CatalogPageCache(2);
  const exact = mergeFavoritesResponse(cache, scope(), 20, {
    items: ["page-2"],
    total: 55,
    offset: 20,
  }, 1_000);
  assert.equal(exact.exact, true);
  assert.deepEqual(exact.page, { items: ["page-2"], total: 55, offset: 20, cachedAt: 1_000 });
  assert.deepEqual(cache.get(favoritesPageCacheKey(scope(), 20)), exact.page);

  const redirected = mergeFavoritesResponse(cache, scope(), 60, {
    items: [],
    total: 55,
    offset: 40,
  }, 2_000);
  assert.equal(redirected.exact, false);
  assert.equal(cache.has(favoritesPageCacheKey(scope(), 60)), false);
  assert.deepEqual(cache.get(favoritesPageCacheKey(scope(), 20)), exact.page);
});

test("收藏页的短期新鲜窗口避免返回翻页时重复请求", () => {
  const page = { items: [], total: 0, offset: 0, cachedAt: 10_000 };
  assert.equal(isFavoritesPageFresh(page, 39_999), true);
  assert.equal(isFavoritesPageFresh(page, 40_001), false);
});

test("显示策略只保留同页 stale 或精确缓存，不把上一页冒充未缓存的新页", () => {
  const previousItems = ["page-1"];
  const cachedPage = { items: ["page-2"], total: 55, offset: 20, cachedAt: 10_000 };
  assert.equal(selectFavoritesDisplayItems(20, 20, 0, previousItems), null);
  assert.deepEqual(selectFavoritesDisplayItems(20, 20, 20, ["stale-page-2"]), ["stale-page-2"]);
  assert.deepEqual(selectFavoritesDisplayItems(20, 20, 0, previousItems, cachedPage), ["page-2"]);
  assert.deepEqual(selectFavoritesDisplayItems(20, 20, 20, []), [], "同页空结果必须与尚未加载区分");
});
