import assert from "node:assert/strict";
import test from "node:test";

import {
  browseRouteKey,
  defaultLibraryPageScopes,
  libraryPageScopeOffset,
  mergeLegacyLibraryPageScopes,
  migrateLegacyLibraryPageScopes,
  parseBrowseScopes,
  parseLibraryPageScopes,
  parseBrowseURL,
  rememberLibraryPageScope,
  sanitizeBrowseRoute,
  serializeBrowseScopes,
  serializeLibraryPageScopes,
  serializeBrowseURL,
} from "../src/lib/browseRoute.ts";

test("默认地址恢复 Home 且不制造参数", () => {
  const route = parseBrowseURL("http://bmanga.test/v2/");
  assert.deepEqual(route, {
    view: "home",
    catalogMode: "all",
    sort: "added_desc",
    offset: 0,
    searchQuery: "",
    discoverMode: "unread",
  });
  assert.equal(serializeBrowseURL("http://bmanga.test/v2/", route), "/v2/");
});

test("中文搜索深链恢复关键词、类型、排序和对齐页码", () => {
  const route = parseBrowseURL("http://bmanga.test/v2/?view=search&q=%E7%82%8E%E6%8B%B3&kind=series&sort=pages_desc&offset=37");
  assert.equal(route.view, "search");
  assert.equal(route.searchQuery, "炎拳");
  assert.equal(route.catalogMode, "series");
  assert.equal(route.sort, "pages_desc");
  assert.equal(route.offset, 36);
});

test("没有 view 时可从搜索和发现参数推断页面", () => {
  assert.equal(parseBrowseURL("/v2/?q=test").view, "search");
  assert.equal(parseBrowseURL("/v2/?discover=liked").view, "discover");
  assert.equal(parseBrowseURL("/v2/library?kind=doujin").view, "library");
});

test("非法枚举和危险 offset 安全回退", () => {
  assert.deepEqual(
    parseBrowseURL("/v2/?view=library&kind=bad&sort=drop_table&offset=-99"),
    sanitizeBrowseRoute({ view: "library" }),
  );
  assert.equal(parseBrowseURL("/v2/?view=my&offset=27").offset, 20);
  assert.equal(parseBrowseURL("/v2/?view=library&offset=999999999").offset, 999990);
});

test("序列化省略默认值并保留非 bmanga 参数与 hash", () => {
  const href = serializeBrowseURL("http://bmanga.test/v2/?entry=shared-link&view=home#shelf", {
    view: "search",
    catalogMode: "doujin",
    sort: "title_asc",
    offset: 18,
    searchQuery: "藤本 树",
    discoverMode: "unread",
  });
  const url = new URL(href, "http://bmanga.test");
  assert.equal(url.searchParams.get("entry"), "shared-link");
  assert.equal(url.pathname, "/v2/search");
  assert.equal(url.searchParams.has("view"), false);
  assert.equal(url.searchParams.get("kind"), "doujin");
  assert.equal(url.searchParams.get("sort"), "title_asc");
  assert.equal(url.searchParams.get("offset"), "18");
  assert.equal(url.searchParams.get("q"), "藤本 树");
  assert.equal(url.hash, "#shelf");
});

test("parse、serialize、parse 往返保持规范状态", () => {
  const source = parseBrowseURL("/v2/?view=discover&discover=reread");
  const href = serializeBrowseURL("http://bmanga.test/v2/?tracking=1", source);
  assert.deepEqual(parseBrowseURL(href), source);
  assert.equal(browseRouteKey(source), "browse:/v2/discover?discover=reread");
});

test("书库、搜索和发现 scope 相互隔离", () => {
  const serialized = serializeBrowseScopes({
    library: sanitizeBrowseRoute({ view: "library", catalogMode: "doujin", sort: "pages_desc", offset: 54 }),
    search: sanitizeBrowseRoute({ view: "search", catalogMode: "series", searchQuery: "炎拳", offset: 18 }),
    discover: sanitizeBrowseRoute({ view: "discover", discoverMode: "liked" }),
  });
  const scopes = parseBrowseScopes(serialized);
  assert.equal(scopes.library?.catalogMode, "doujin");
  assert.equal(scopes.library?.offset, 54);
  assert.equal(scopes.search?.catalogMode, "series");
  assert.equal(scopes.search?.searchQuery, "炎拳");
  assert.equal(scopes.discover?.discoverMode, "liked");
});

test("损坏的 session scope 安全回退", () => {
  assert.deepEqual(parseBrowseScopes("not-json"), {});
  const scopes = parseBrowseScopes(JSON.stringify({ library: { catalogMode: "oops", offset: 19 }, unknown: { view: "settings" } }));
  assert.equal(scopes.library?.catalogMode, "all");
  assert.equal(scopes.library?.offset, 18);
  assert.equal("unknown" in scopes, false);
});

test("书库三个内容类型分别记住对齐后的页码", () => {
  let scopes = defaultLibraryPageScopes();
  scopes = rememberLibraryPageScope(scopes, sanitizeBrowseRoute({ view: "library", catalogMode: "all", offset: 2448 }));
  scopes = rememberLibraryPageScope(scopes, sanitizeBrowseRoute({ view: "library", catalogMode: "doujin", offset: 91 }));
  scopes = rememberLibraryPageScope(scopes, sanitizeBrowseRoute({ view: "library", catalogMode: "series", offset: 36 }));

  assert.equal(libraryPageScopeOffset(scopes, "all", "added_desc"), 2448);
  assert.equal(libraryPageScopeOffset(scopes, "doujin", "added_desc"), 90);
  assert.equal(libraryPageScopeOffset(scopes, "series", "added_desc"), 36);

  const restored = parseLibraryPageScopes(serializeLibraryPageScopes(scopes));
  assert.deepEqual(restored, scopes);
});

test("同人本第七页在打开作品后仍可恢复", () => {
  const pageSeven = sanitizeBrowseRoute({
    view: "library",
    catalogMode: "doujin",
    sort: "added_desc",
    offset: 6 * 18,
  });
  const remembered = rememberLibraryPageScope(defaultLibraryPageScopes(), pageSeven);
  const restored = parseLibraryPageScopes(serializeLibraryPageScopes(remembered));
  assert.equal(libraryPageScopeOffset(restored, "doujin", "added_desc"), 108);
});

test("书库排序改变时清空旧排序下的分类页码", () => {
  let scopes = defaultLibraryPageScopes();
  scopes = rememberLibraryPageScope(scopes, sanitizeBrowseRoute({ view: "library", catalogMode: "all", offset: 72 }));
  scopes = rememberLibraryPageScope(scopes, sanitizeBrowseRoute({ view: "library", catalogMode: "doujin", offset: 90 }));
  scopes = rememberLibraryPageScope(scopes, sanitizeBrowseRoute({ view: "library", catalogMode: "series", sort: "pages_desc", offset: 36 }));

  assert.equal(scopes.sort, "pages_desc");
  assert.deepEqual(scopes.offsets, { series: 36 });
  assert.equal(libraryPageScopeOffset(scopes, "doujin", "pages_desc"), 0);
  assert.equal(libraryPageScopeOffset(scopes, "series", "pages_desc"), 36);
  assert.equal(libraryPageScopeOffset(scopes, "series", "added_desc"), 0);
});

test("损坏的分类页码 session 安全回退并规范化 offset", () => {
  assert.deepEqual(parseLibraryPageScopes("not-json", "title_asc"), { sort: "title_asc", offsets: {} });
  assert.deepEqual(
    parseLibraryPageScopes(JSON.stringify({ sort: "bad", offsets: { all: 19, doujin: -1, series: 999999999, unknown: 54 } })),
    { sort: "added_desc", offsets: { all: 18, doujin: 0, series: 999990 } },
  );
});

test("旧版三个书库 scope 按页码迁移到 V2 的 18 项分页", () => {
  const legacy = migrateLegacyLibraryPageScopes(JSON.stringify({
    "shelf:": { bmangaView: "shelf", bmangaPage: 17, bmangaOffset: 960, bmangaLimit: 60, bmangaSort: "added_desc" },
    "works:doujin": { bmangaView: "works", bmangaType: "doujin", bmangaPage: 42, bmangaOffset: 2460, bmangaLimit: 60, bmangaSort: "added_desc" },
    "series:": { bmangaView: "series", bmangaOffset: 120, bmangaLimit: 60, bmangaSort: "added_desc" },
  }));

  assert.deepEqual(legacy, {
    sort: "added_desc",
    offsets: { all: 288, doujin: 738, series: 36 },
  });
});

test("旧版页码迁移忽略不同排序，并由已有 V2 深页优先", () => {
  const legacy = migrateLegacyLibraryPageScopes(JSON.stringify({
    "shelf:": { bmangaPage: 9, bmangaSort: "title_asc" },
    "works:doujin": { bmangaPage: 6, bmangaSort: "added_desc" },
    "series:": { bmangaPage: 3, bmangaSort: "added_desc" },
  }));
  assert.deepEqual(legacy.offsets, { doujin: 90, series: 36 });

  const merged = mergeLegacyLibraryPageScopes(
    { sort: "added_desc", offsets: { all: 72, doujin: 0 } },
    legacy,
  );
  assert.deepEqual(merged, { sort: "added_desc", offsets: { all: 72, doujin: 90, series: 36 } });
  assert.deepEqual(migrateLegacyLibraryPageScopes("not-json"), defaultLibraryPageScopes());
});

test("旧版未知排序和带过滤条件的深页不会迁入无过滤书库", () => {
  const migrated = migrateLegacyLibraryPageScopes(JSON.stringify({
    "shelf:": { bmangaPage: 9, bmangaSort: "title_desc" },
    "works:doujin": { bmangaPage: 6, bmangaSort: "added_desc", bmangaTag: "仅本标签" },
    "series:": { bmangaPage: 3, bmangaSort: "added_desc", bmangaSearch: "炎拳" },
  }));
  assert.deepEqual(migrated, defaultLibraryPageScopes());
});

test("旧版最小 scope 从 filter signature 判断是否可安全迁移", () => {
  const migrated = migrateLegacyLibraryPageScopes(JSON.stringify({
    "shelf:": {
      bmangaPage: 11,
      bmangaFilterSignature: JSON.stringify({ search: "", library: "", source: "", pageStatus: "", action: "", userMark: "", tag: "", tagQuick: "", sort: "added_desc" }),
    },
    "works:doujin": {
      bmangaPage: 7,
      bmangaFilterSignature: JSON.stringify({ search: "", library: "", source: "", pageStatus: "", action: "", userMark: "", tag: "", tagQuick: "", sort: "title_desc" }),
    },
    "series:": {
      bmangaPage: 4,
      bmangaFilterSignature: JSON.stringify({ search: "炎拳", sort: "added_desc" }),
    },
  }));
  assert.deepEqual(migrated.offsets, { all: 180 });

  const malformed = migrateLegacyLibraryPageScopes(JSON.stringify({
    "shelf:": { bmangaPage: 11, bmangaFilterSignature: "not-json" },
  }));
  assert.deepEqual(malformed, defaultLibraryPageScopes());
});
