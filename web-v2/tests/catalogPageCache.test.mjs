import assert from "node:assert/strict";
import test from "node:test";

import {
  CatalogPageCache,
  adjacentCatalogOffsets,
  canPrefetchCatalog,
  catalogCacheScopeKey,
  catalogPageCacheKey,
  favoritesCacheScopeKey,
  mergeCatalogResponse,
  normalizeCatalogPage,
} from "../src/lib/catalogPageCache.ts";

function scope(overrides = {}) {
  return {
    dataRevision: 4,
    requestRevision: 2,
    view: "library",
    library: "",
    catalogMode: "all",
    sort: "added_desc",
    searchQuery: "",
    limit: 18,
    ...overrides,
  };
}

test("缓存键覆盖数据版本与完整查询 scope、忽略请求版本，并对齐分页 offset", () => {
  const base = scope();
  assert.equal(catalogPageCacheKey(base, 19), catalogPageCacheKey(base, 18));
  assert.notEqual(catalogCacheScopeKey(base), catalogCacheScopeKey(scope({ dataRevision: 5 })));
  assert.equal(catalogCacheScopeKey(base), catalogCacheScopeKey(scope({ requestRevision: 3 })));
  assert.notEqual(catalogCacheScopeKey(base), catalogCacheScopeKey(scope({ library: "manga" })));
  assert.notEqual(catalogCacheScopeKey(base), catalogCacheScopeKey(scope({ catalogMode: "series" })));
  assert.notEqual(catalogCacheScopeKey(base), catalogCacheScopeKey(scope({ sort: "title_asc" })));
  assert.notEqual(
    catalogCacheScopeKey(scope({ view: "search", searchQuery: "炎拳" })),
    catalogCacheScopeKey(scope({ view: "search", searchQuery: "链锯人" })),
  );
});

test("收藏缓存键同样忽略请求版本，但数据版本仍会隔离缓存", () => {
  const base = { dataRevision: 4, requestRevision: 2, limit: 18 };
  assert.equal(favoritesCacheScopeKey(base), favoritesCacheScopeKey({ ...base, requestRevision: 3 }));
  assert.notEqual(favoritesCacheScopeKey(base), favoritesCacheScopeKey({ ...base, dataRevision: 5 }));
});

test("LRU 有固定上限，读取会刷新淘汰顺序", () => {
  const cache = new CatalogPageCache(2);
  cache.set("a", 1);
  cache.set("b", 2);
  assert.equal(cache.get("a"), 1);
  cache.set("c", 3);

  assert.equal(cache.size, 2);
  assert.equal(cache.has("a"), true);
  assert.equal(cache.has("b"), false);
  assert.equal(cache.get("c"), 3);
});

test("updateAll 原位更新所有值且不改变 LRU 淘汰顺序", () => {
  const cache = new CatalogPageCache(2);
  cache.set("a", { count: 1 });
  cache.set("b", { count: 2 });

  cache.updateAll((value, key) => ({ count: value.count + (key === "a" ? 10 : 20) }));
  assert.deepEqual(cache.peek("a"), { count: 11 });
  assert.deepEqual(cache.peek("b"), { count: 22 });

  cache.set("c", { count: 3 });
  assert.equal(cache.has("a"), false, "批量更新不应把最旧条目移动到 LRU 队尾");
  assert.equal(cache.has("b"), true);
  assert.equal(cache.has("c"), true);
});

test("delete 只移除指定页，供强制重试保留其他 scope 缓存", () => {
  const cache = new CatalogPageCache(3);
  cache.set("library", 1);
  cache.set("search", 2);
  assert.equal(cache.delete("search"), true);
  assert.equal(cache.has("library"), true);
  assert.equal(cache.has("search"), false);
});

test("只为有效目录请求预取存在的下一页，空搜索与末页不预取", () => {
  const page = { items: ["a"], total: 55, offset: 18 };
  assert.equal(canPrefetchCatalog(scope()), true);
  assert.deepEqual(adjacentCatalogOffsets(scope(), page), [36]);
  assert.deepEqual(adjacentCatalogOffsets(scope(), { ...page, offset: 54 }), []);

  const blankSearch = scope({ view: "search", searchQuery: "   " });
  assert.equal(canPrefetchCatalog(blankSearch), false);
  assert.deepEqual(adjacentCatalogOffsets(blankSearch, page), []);
});

test("响应规范化会限制越界 offset，并保留独立 items 数组", () => {
  const items = [{ id: 1 }];
  const page = normalizeCatalogPage({ items, total: 20, offset: 999 }, 18, 18);
  assert.deepEqual(page, { items, total: 20, offset: 18 });
  assert.notEqual(page.items, items);
});

test("合并响应按服务端有效页写入缓存并标记是否命中请求页", () => {
  const cache = new CatalogPageCache(4);
  const catalogScope = scope();
  const exact = mergeCatalogResponse(cache, catalogScope, 18, {
    items: ["page-2"],
    total: 50,
    offset: 18,
  });
  assert.equal(exact.exact, true);
  assert.deepEqual(cache.get(catalogPageCacheKey(catalogScope, 18)), exact.page);

  const redirected = mergeCatalogResponse(cache, catalogScope, 54, {
    items: ["last"],
    total: 20,
    offset: 18,
  });
  assert.equal(redirected.exact, false);
  assert.equal(redirected.page.offset, 18);
  assert.equal(cache.has(catalogPageCacheKey(catalogScope, 54)), false);
  assert.deepEqual(
    cache.get(catalogPageCacheKey(catalogScope, 18)),
    exact.page,
    "越界空响应不应覆盖已缓存的真实末页",
  );
});
