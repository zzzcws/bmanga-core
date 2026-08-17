import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as delay } from "node:timers/promises";

const WEB_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const PROJECT_ROOT = resolve(WEB_ROOT, "..");
const requireFromTool = createRequire(join(PROJECT_ROOT, ".tools", "ui-smoke", "package.json"));
const { chromium } = requireFromTool("playwright-core");
const port = 18921;
const base = `http://127.0.0.1:${port}`;
const alternateBase = `http://localhost:${port}`;

const LIBRARY_PAGE_MODES = ["all", "doujin", "series"];
const LIBRARY_PAGE_PENDING_KEY = "bmanga.v2.libraryPageState.pending.v1";

function cloneJSON(value) {
  return value === null ? null : JSON.parse(JSON.stringify(value));
}

function alignedLibraryOffset(value) {
  const numeric = Number(value);
  if (!Number.isFinite(numeric) || numeric <= 0) return 0;
  return Math.floor(Math.min(1_000_000, numeric) / 18) * 18;
}

function libraryPageStateFixture({
  sort = "added_desc",
  all = 0,
  doujin = 0,
  series = 0,
  clock = "2025-01-01T00:00:00.000Z",
  eventID = "fixture-seed",
} = {}) {
  return {
    version: 1,
    sort,
    sort_updated_at: clock,
    sort_event_id: eventID,
    positions: {
      all: { offset: alignedLibraryOffset(all), updated_at: clock, event_id: eventID },
      doujin: { offset: alignedLibraryOffset(doujin), updated_at: clock, event_id: eventID },
      series: { offset: alignedLibraryOffset(series), updated_at: clock, event_id: eventID },
    },
    updated_at: clock,
  };
}

function libraryPageEventAfter(incoming, current) {
  const incomingTime = Date.parse(incoming.updated_at);
  const currentTime = Date.parse(current.updated_at);
  if (incomingTime !== currentTime) return incomingTime > currentTime;
  return incoming.event_id > current.event_id;
}

function latestLibraryPageEvent(state) {
  return [
    { updated_at: state.sort_updated_at, event_id: state.sort_event_id },
    ...LIBRARY_PAGE_MODES.map((mode) => state.positions[mode]),
  ].reduce((latest, current) => libraryPageEventAfter(current, latest) ? current : latest);
}

function latestLibraryPageTime(state) {
  return latestLibraryPageEvent(state).updated_at;
}

function createLibraryPageStateStore(initialState = null, {
  defaultGetBehavior = {},
  postDelayMs = 0,
  postStatus = 200,
} = {}) {
  let canonical = cloneJSON(initialState);
  const requests = [];
  const queuedGetBehaviors = [];
  let activePostDelayMs = Math.max(0, Number(postDelayMs) || 0);
  let activePostStatus = Number(postStatus) || 200;

  const save = (mutation) => {
    const event = { updated_at: mutation.updated_at, event_id: mutation.event_id };
    if (!canonical) {
      const initialOffsets = mutation.initial_offsets || {};
      const positions = Object.fromEntries(LIBRARY_PAGE_MODES.map((mode) => [mode, {
        offset: alignedLibraryOffset(initialOffsets[mode]),
        ...event,
      }]));
      positions[mutation.mode].offset = alignedLibraryOffset(mutation.offset);
      canonical = {
        version: 1,
        sort: mutation.sort,
        sort_updated_at: mutation.updated_at,
        sort_event_id: mutation.event_id,
        positions,
        updated_at: mutation.updated_at,
      };
      return true;
    }

    if (mutation.sort !== canonical.sort) {
      if (!libraryPageEventAfter(event, latestLibraryPageEvent(canonical))) return false;
      canonical.sort = mutation.sort;
      canonical.sort_updated_at = mutation.updated_at;
      canonical.sort_event_id = mutation.event_id;
      canonical.positions = Object.fromEntries(LIBRARY_PAGE_MODES.map((mode) => [mode, {
        offset: mode === mutation.mode ? alignedLibraryOffset(mutation.offset) : 0,
        ...event,
      }]));
      canonical.updated_at = mutation.updated_at;
      return true;
    }

    const current = canonical.positions[mutation.mode];
    if (!libraryPageEventAfter(event, current)) return false;
    canonical.positions[mutation.mode] = {
      offset: alignedLibraryOffset(mutation.offset),
      ...event,
    };
    canonical.updated_at = latestLibraryPageTime(canonical);
    return true;
  };

  return {
    requests,
    get state() {
      return cloneJSON(canonical);
    },
    queueGetResponse(behavior = {}) {
      queuedGetBehaviors.push({ ...behavior });
    },
    setPostDelay(delayMs) {
      activePostDelayMs = Math.max(0, Number(delayMs) || 0);
    },
    setPostStatus(status) {
      activePostStatus = Number(status) || 200;
    },
    async fulfill(route) {
      const request = route.request();
      const url = new URL(request.url());
      const method = request.method().toUpperCase();
      if (method === "GET") {
        const behavior = { ...defaultGetBehavior, ...(queuedGetBehaviors.shift() || {}) };
        const responseState = Object.prototype.hasOwnProperty.call(behavior, "state")
          ? cloneJSON(behavior.state)
          : behavior.snapshot
            ? cloneJSON(canonical)
            : undefined;
        const record = { method, origin: url.origin, state: null, behavior: { ...behavior }, started_at: Date.now() };
        requests.push(record);
        const wait = Math.max(0, Number(behavior.delayMs) || 0);
        if (wait > 0) await delay(wait);
        const status = Number(behavior.status) || 200;
        if (status >= 400) {
          await route.fulfill({ status, json: { error: `library page state fixture ${status}` } });
          record.completed_at = Date.now();
          return;
        }
        const state = responseState === undefined ? cloneJSON(canonical) : responseState;
        record.state = cloneJSON(state);
        await route.fulfill({
          json: {
            state,
            updated_at: state?.updated_at || "",
          },
        });
        record.completed_at = Date.now();
        return;
      }
      if (method === "POST") {
        assert.equal(request.headers()["x-bmanga-write"], "same-origin", "library page write lost its intent header");
        const body = request.postDataJSON();
        const mutations = (Array.isArray(body?.states) ? body.states : [body?.state])
          .filter(Boolean)
          .sort((left, right) => libraryPageEventAfter(left, right) ? 1 : libraryPageEventAfter(right, left) ? -1 : 0);
        assert(mutations.length > 0 && mutations.every((mutation) => LIBRARY_PAGE_MODES.includes(mutation.mode)), "library page write has no valid mutation batch");
        const record = {
          method,
          origin: url.origin,
          state: cloneJSON(mutations.at(-1)),
          states: cloneJSON(mutations),
          stored: null,
          status: activePostStatus,
          started_at: Date.now(),
        };
        requests.push(record);
        if (activePostDelayMs > 0) await delay(activePostDelayMs);
        if (record.status >= 400) {
          await route.fulfill({ status: record.status, json: { error: `library page state write fixture ${record.status}` } });
          record.completed_at = Date.now();
          return;
        }
        const stored = mutations.reduce((changed, mutation) => save(mutation) || changed, false);
        record.stored = stored;
        await route.fulfill({
          json: {
            ok: true,
            stored,
            state: cloneJSON(canonical),
            updated_at: canonical?.updated_at || "",
            acknowledged_event_ids: mutations.map((mutation) => mutation.event_id),
          },
        });
        record.completed_at = Date.now();
        return;
      }
      await route.fulfill({ status: 405, json: { error: "method not allowed" } });
    },
  };
}

function browserExecutablePath() {
  return process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || process.env.CHROME_PATH || "";
}

async function waitForServer() {
  for (let attempt = 0; attempt < 60; attempt += 1) {
    try {
      const response = await fetch(`${base}/v2/library`);
      if (response.ok) return;
    } catch {
      // Vite is still starting.
    }
    await delay(100);
  }
  throw new Error("Vite pagination smoke server did not start");
}

function catalogResponse(url, kind = "work") {
  const limit = Number(url.searchParams.get("limit") || 18);
  const offset = Number(url.searchParams.get("offset") || 0);
  const total = url.searchParams.get("mark") === "favorite" ? 55 : 31_291;
  const count = Math.max(0, Math.min(limit, total - offset));
  return {
    total,
    limit,
    offset,
    items: Array.from({ length: count }, (_, index) => kind === "series" ? {
      shelf_type: "series",
      group_id: `pagination-series-${offset + index}`,
      series_title: `分页测试系列 ${offset + index + 1}`,
      display_title: `分页测试系列 ${offset + index + 1}`,
      selected_candidate_id: `pagination-series-cover-${offset + index}`,
      counted_items: 8,
      counted_pages: 240,
      can_read: true,
    } : {
      shelf_type: "work",
      candidate_id: `pagination-${offset + index}`,
      work_identity_id: `pagination-work-${offset + index}`,
      title: `分页测试作品 ${offset + index + 1}`,
      display_title: `分页测试作品 ${offset + index + 1}`,
      readable_page_count: 24,
      can_read: true,
    }),
  };
}

function workDetailResponse(candidateID) {
  const rawIndex = Number(String(candidateID).replace(/^pagination-/, ""));
  const index = Number.isFinite(rawIndex) && rawIndex >= 0 ? rawIndex : 0;
  const title = `分页测试作品 ${index + 1}`;
  return {
    work: {
      shelf_type: "work",
      candidate_id: candidateID,
      work_identity_id: `pagination-work-${index}`,
      title,
      display_title: title,
      readable_page_count: 24,
      can_read: true,
    },
    translations: [],
    series: [],
    doujin_series: [],
    creators: [],
    mark: null,
    related: { editions: [], series: [], creators: [] },
    title_hints: { creators: [], series: "" },
  };
}

function catalogRequestCount(requests, pathname, offset) {
  return requests.filter((url) => url.pathname === pathname && url.searchParams.get("offset") === String(offset)).length;
}

function favoriteRequestCount(requests, offset) {
  return requests.filter((url) => url.pathname === "/api/shelf"
    && url.searchParams.get("mark") === "favorite"
    && url.searchParams.get("offset") === String(offset)).length;
}

async function waitForLibraryCatalog(page, expectedPage, expectedTitle) {
  await page.waitForFunction(({ pageNumber, title }) => {
    const input = document.querySelector('nav[aria-label="书库分页"] input[type="number"]');
    const firstCard = document.querySelector(".catalog-grid .book-card");
    return input?.value === String(pageNumber) && firstCard?.textContent?.includes(title);
  }, { pageNumber: expectedPage, title: expectedTitle });
}

async function waitForRequestCount(requests, pathname, offset, minimum = 1) {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    if (catalogRequestCount(requests, pathname, offset) >= minimum) return;
    await delay(20);
  }
  throw new Error(`catalog request did not arrive: ${pathname}?offset=${offset}`);
}

async function waitForFavoriteRequestCount(requests, offset, minimum = 1) {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    if (favoriteRequestCount(requests, offset) >= minimum) return;
    await delay(20);
  }
  throw new Error(`favorite request did not arrive: /api/shelf?mark=favorite&offset=${offset}`);
}

async function waitForLibraryStateStoreRequest(store, startIndex, predicate, { completed = false, timeoutMs = 5_000 } = {}) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const match = store.requests.slice(startIndex).find((request) => predicate(request) && (!completed || request.completed_at));
    if (match) return match;
    await delay(20);
  }
  throw new Error(`library page state fixture request did not arrive after index ${startIndex}`);
}

async function waitForFavoritesPage(page, expectedPage, expectedTitle) {
  await page.waitForFunction(({ pageNumber, title }) => {
    const input = document.querySelector('nav[aria-label="收藏分页"] input[type="number"]');
    const firstCard = document.querySelector(".my-page > .catalog-grid .book-card");
    return input?.value === String(pageNumber) && firstCard?.textContent?.includes(title);
  }, { pageNumber: expectedPage, title: expectedTitle });
}

async function mockCatalogAPI(context, {
  favoriteDelayByOffset = {},
  libraryPageStore = createLibraryPageStateStore(),
} = {}) {
  await context.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/api/library-page-state") {
      await libraryPageStore.fulfill(route);
      return;
    }
    if (url.pathname === "/api/reading-history") {
      await route.fulfill({ json: { total: 0, limit: 12, offset: 0, items: [] } });
      return;
    }
    if (url.pathname === "/api/shelf") {
      if (url.searchParams.get("mark") === "favorite") {
        const wait = Number(favoriteDelayByOffset[url.searchParams.get("offset") || "0"] || 0);
        if (wait > 0) await delay(wait);
      }
      await route.fulfill({ json: catalogResponse(url) });
      return;
    }
    if (url.pathname === "/api/works") {
      await route.fulfill({ json: catalogResponse(url) });
      return;
    }
    if (url.pathname === "/api/series") {
      await route.fulfill({ json: catalogResponse(url, "series") });
      return;
    }
    if (url.pathname === "/api/work") {
      await route.fulfill({ json: workDetailResponse(url.searchParams.get("id") || "pagination-0") });
      return;
    }
    if (url.pathname === "/cover") {
      await route.fulfill({ status: 404, body: "" });
      return;
    }
    if (url.pathname.startsWith("/api/")) {
      await route.fulfill({ status: 404, json: { error: `unhandled pagination smoke fixture: ${url.pathname}` } });
      return;
    }
    await route.continue();
  });
  return libraryPageStore;
}

const vite = spawn(process.execPath, [
  join(WEB_ROOT, "node_modules", "vite", "bin", "vite.js"),
  "--host", "127.0.0.1",
  "--port", String(port),
  "--strictPort",
], {
  cwd: WEB_ROOT,
  windowsHide: true,
  stdio: "ignore",
});

let browser;
try {
  await waitForServer();
  const executablePath = browserExecutablePath();
  browser = await chromium.launch({ ...(executablePath ? { executablePath } : {}), headless: true });
  const migrationContext = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: "block" });
  await mockCatalogAPI(migrationContext);
  await migrationContext.addInitScript(() => {
    localStorage.setItem("bmanga.browseScopeState.v1", JSON.stringify({
      "shelf:": { bmangaView: "shelf", bmangaPage: 9, bmangaOffset: 480, bmangaLimit: 60, bmangaSort: "added_desc" },
      "works:doujin": { bmangaView: "works", bmangaType: "doujin", bmangaPage: 6, bmangaOffset: 300, bmangaLimit: 60, bmangaSort: "added_desc" },
      "series:": { bmangaView: "series", bmangaPage: 3, bmangaOffset: 120, bmangaLimit: 60, bmangaSort: "added_desc" },
    }));
  });
  const migrationPage = await migrationContext.newPage();
  await migrationPage.goto(`${base}/v2/library`, { waitUntil: "domcontentloaded" });
  const migrationPager = migrationPage.getByRole("navigation", { name: "书库分页" });
  const migrationTabs = migrationPage.getByRole("group", { name: "内容类型" });
  await migrationPager.waitFor({ state: "visible" });
  assert.equal(await migrationPager.locator("input[type=number]").inputValue(), "9", "legacy all page was not restored on first V2 load");
  await migrationPage.waitForFunction(() => new URL(location.href).searchParams.get("offset") === "144");
  const migratedDoujinResponse = migrationPage.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/works" && url.searchParams.get("offset") === "90";
  });
  await migrationTabs.getByRole("button", { name: "同人本", exact: true }).click();
  await migratedDoujinResponse;
  assert.equal(await migrationPager.locator("input[type=number]").inputValue(), "6", "legacy doujin page was not migrated");
  const migratedSeriesResponse = migrationPage.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/series" && url.searchParams.get("offset") === "36";
  });
  await migrationTabs.getByRole("button", { name: "漫画系列", exact: true }).click();
  await migratedSeriesResponse;
  assert.equal(await migrationPager.locator("input[type=number]").inputValue(), "3", "legacy series page was not migrated");
  assert.equal(await migrationPage.evaluate(() => localStorage.getItem("bmanga.v2.libraryPageScopes.legacyMigrated.v1.added_desc")), "1", "legacy migration marker was not saved");
  const explicitPage = await migrationContext.newPage();
  await explicitPage.goto(`${base}/v2/library?kind=doujin&offset=18`, { waitUntil: "domcontentloaded" });
  const explicitPager = explicitPage.getByRole("navigation", { name: "书库分页" });
  await explicitPager.waitFor({ state: "visible" });
  assert.equal(await explicitPager.locator("input[type=number]").inputValue(), "2", "explicit URL offset was overwritten by migrated memory");
  const homePage = await migrationContext.newPage();
  await homePage.goto(`${base}/v2/`, { waitUntil: "domcontentloaded" });
  const homeRestoreResponse = homePage.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/shelf" && url.searchParams.get("offset") === "144";
  });
  await homePage.getByRole("navigation", { name: "主要导航" }).getByRole("button", { name: /书库/u }).click();
  await homeRestoreResponse;
  const homePager = homePage.getByRole("navigation", { name: "书库分页" });
  assert.equal(await homePager.locator("input[type=number]").inputValue(), "9", "Home to Library did not restore persistent page memory");
  await migrationContext.close();

  const storedSortContext = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: "block" });
  await mockCatalogAPI(storedSortContext);
  await storedSortContext.addInitScript(() => {
    localStorage.setItem("bmanga.v2.libraryPageScopes.v1", JSON.stringify({ sort: "title_asc", offsets: { all: 72 } }));
    sessionStorage.setItem("bmanga.v2.browseScopes.v1", JSON.stringify({
      library: { view: "library", catalogMode: "all", sort: "title_asc", offset: 0, searchQuery: "", discoverMode: "unread" },
    }));
  });
  const storedSortPage = await storedSortContext.newPage();
  await storedSortPage.goto(`${base}/v2/`, { waitUntil: "domcontentloaded" });
  const storedSortResponse = storedSortPage.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/shelf" && url.searchParams.get("sort") === "title_asc" && url.searchParams.get("offset") === "72";
  });
  await storedSortPage.getByRole("navigation", { name: "主要导航" }).getByRole("button", { name: /书库/u }).click();
  await storedSortResponse;
  const storedSortPager = storedSortPage.getByRole("navigation", { name: "书库分页" });
  assert.equal(await storedSortPager.locator("input[type=number]").inputValue(), "5", "Home to Library lost a non-default persistent sort page");
  assert.equal(await storedSortPage.locator(".library-toolbar .select-field select").inputValue(), "title_asc", "Home to Library lost the persistent sort");
  await storedSortContext.close();

  const deferredMigrationContext = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: "block" });
  await mockCatalogAPI(deferredMigrationContext);
  await deferredMigrationContext.addInitScript(() => {
    localStorage.setItem("bmanga.browseScopeState.v1", JSON.stringify({
      "shelf:": { bmangaView: "shelf", bmangaPage: 9, bmangaSort: "added_desc" },
    }));
  });
  const unmatchedSortPage = await deferredMigrationContext.newPage();
  await unmatchedSortPage.goto(`${base}/v2/library?sort=title_asc`, { waitUntil: "domcontentloaded" });
  await unmatchedSortPage.getByRole("navigation", { name: "书库分页" }).waitFor({ state: "visible" });
  assert.equal(await unmatchedSortPage.evaluate(() => localStorage.getItem("bmanga.v2.libraryPageScopes.legacyMigrated.v1.title_asc")), null, "unmatched legacy sort was marked as migrated");
  await deferredMigrationContext.close();

  const matchingMigrationContext = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: "block" });
  await mockCatalogAPI(matchingMigrationContext);
  await matchingMigrationContext.addInitScript(() => {
    localStorage.setItem("bmanga.browseScopeState.v1", JSON.stringify({
      "shelf:": { bmangaView: "shelf", bmangaPage: 9, bmangaSort: "added_desc" },
    }));
  });
  const matchingSortPage = await matchingMigrationContext.newPage();
  await matchingSortPage.goto(`${base}/v2/library`, { waitUntil: "domcontentloaded" });
  const matchingPager = matchingSortPage.getByRole("navigation", { name: "书库分页" });
  await matchingPager.waitFor({ state: "visible" });
  assert.equal(await matchingPager.locator("input[type=number]").inputValue(), "9", "matching sort did not apply the legacy migration");
  assert.equal(await matchingSortPage.evaluate(() => localStorage.getItem("bmanga.v2.libraryPageScopes.legacyMigrated.v1.added_desc")), "1", "matching legacy sort was not marked after migration");
  await matchingMigrationContext.close();

  const loadingContext = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: "block" });
  const loadingRequests = [];
  const loadingResponses = [];
  loadingContext.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname.startsWith("/api/")) loadingRequests.push(url);
  });
  loadingContext.on("response", (response) => {
    const url = new URL(response.url());
    if (url.pathname.startsWith("/api/")) loadingResponses.push(url);
  });
  await mockCatalogAPI(loadingContext, { favoriteDelayByOffset: { 20: 1_000 } });
  const loadingPage = await loadingContext.newPage();
  await loadingPage.goto(`${base}/v2/`, { waitUntil: "domcontentloaded" });
  await loadingPage.waitForFunction(() => document.querySelectorAll(".home-book-grid .book-card").length === 6);
  assert.equal(loadingRequests.some((url) => url.pathname === "/api/shelf" && url.searchParams.get("mark") === null && url.searchParams.get("limit") === "6"), true, "Home did not request exactly six recent arrivals");
  assert.equal(await loadingPage.locator(".library-count").textContent(), "31,291 部作品", "Home lost the total works count from the lightweight shelf response");

  await loadingPage.getByRole("navigation", { name: "主要导航" }).getByRole("button", { name: /我的/u }).click();
  await waitForFavoritesPage(loadingPage, 1, "分页测试作品 1");
  assert.equal(loadingRequests.some((url) => url.pathname === "/api/reading-history" && url.searchParams.get("limit") === "6"), true, "My requested more reading-history rows than it renders");
  await waitForFavoriteRequestCount(loadingResponses, 10);
  const favoritesPager = loadingPage.getByRole("navigation", { name: "收藏分页" });
  const prefetchedFavoritesRequests = favoriteRequestCount(loadingRequests, 10);
  await favoritesPager.getByRole("button", { name: "下一页", exact: true }).click();
  await waitForFavoritesPage(loadingPage, 2, "分页测试作品 11");
  assert.equal(favoriteRequestCount(loadingRequests, 10), prefetchedFavoritesRequests, "opening the prefetched Favorites page issued a duplicate request");

  const cachedFavoritesRequests = favoriteRequestCount(loadingRequests, 0);
  await favoritesPager.getByRole("button", { name: "首页", exact: true }).click();
  await waitForFavoritesPage(loadingPage, 1, "分页测试作品 1");
  assert.equal(favoriteRequestCount(loadingRequests, 0), cachedFavoritesRequests, "returning to a cached Favorites page issued a duplicate request");

  const uncachedFavoritesResponse = loadingPage.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/shelf" && url.searchParams.get("mark") === "favorite" && url.searchParams.get("offset") === "20";
  });
  await favoritesPager.locator("input[type=number]").fill("3");
  await favoritesPager.getByRole("button", { name: "跳转", exact: true }).click();
  await loadingPage.waitForFunction(() => document.querySelector('nav[aria-label="收藏分页"] input[type="number"]')?.value === "3");
  assert.equal(await loadingPage.locator(".my-page > .catalog-grid:not(.catalog-skeleton) .book-card").count(), 0, "an uncached Favorites route displayed cards from the previous page");
  assert.equal(await loadingPage.locator(".my-page > .catalog-skeleton").count(), 1, "an uncached Favorites route did not expose its loading skeleton");
  await uncachedFavoritesResponse;
  await waitForFavoritesPage(loadingPage, 3, "分页测试作品 21");
  await loadingContext.close();

  for (const startupNavigationCase of [
    {
      name: "late-success-home",
      destinationName: /首页/u,
      destinationPath: "/v2/",
      viewSelector: ".home-view",
      contentSelector: ".home-book-grid .book-card",
      getBehaviors: [{ delayMs: 700, snapshot: true }],
    },
    {
      name: "retry-success-my",
      destinationName: /我的/u,
      destinationPath: "/v2/my",
      viewSelector: ".my-page",
      contentSelector: ".my-page > .catalog-grid .book-card",
      getBehaviors: [{ status: 503 }, { delayMs: 600, snapshot: true }],
    },
  ]) {
    const startupStore = createLibraryPageStateStore(
      libraryPageStateFixture({ doujin: 90, eventID: startupNavigationCase.name }),
    );
    for (const behavior of startupNavigationCase.getBehaviors) startupStore.queueGetResponse(behavior);
    const startupContext = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: "block" });
    await mockCatalogAPI(startupContext, { libraryPageStore: startupStore });
    const startupPage = await startupContext.newPage();
    const startupRequestStart = startupStore.requests.length;
    await startupPage.goto(`${base}/v2/library?kind=doujin&sort=title_asc&offset=126`, { waitUntil: "domcontentloaded" });
    await waitForLibraryStateStoreRequest(
      startupStore,
      startupRequestStart,
      (request) => request.method === "GET",
    );
    await startupPage.getByRole("navigation", { name: "主要导航" })
      .getByRole("button", { name: startupNavigationCase.destinationName })
      .click();
    await startupPage.locator(startupNavigationCase.viewSelector).waitFor({ state: "visible" });
    await startupPage.locator(startupNavigationCase.contentSelector).first().waitFor({ state: "visible" });

    let completedGetIndex = startupRequestStart;
    for (let index = 0; index < startupNavigationCase.getBehaviors.length; index += 1) {
      const completedGet = await waitForLibraryStateStoreRequest(
        startupStore,
        completedGetIndex,
        (request) => request.method === "GET",
        { completed: true },
      );
      completedGetIndex = startupStore.requests.indexOf(completedGet) + 1;
    }
    await delay(650);
    assert.equal(new URL(startupPage.url()).pathname, startupNavigationCase.destinationPath, `${startupNavigationCase.name} forced navigation back to Library`);
    assert(await startupPage.locator(startupNavigationCase.viewSelector).isVisible(), `${startupNavigationCase.name} cleared the current view`);
    assert(await startupPage.locator(startupNavigationCase.contentSelector).first().isVisible(), `${startupNavigationCase.name} cleared the current view content`);
    const abandonedStartupWrites = startupStore.requests.filter((request) => request.method === "POST");
    assert.equal(
      abandonedStartupWrites.length,
      0,
      `${startupNavigationCase.name} emitted an abandoned explicit startup write: ${JSON.stringify(abandonedStartupWrites)}`,
    );
    await startupPage.evaluate(() => window.dispatchEvent(new Event("pagehide")));
    await delay(100);
    assert.equal(
      startupStore.requests.filter((request) => request.method === "POST").length,
      0,
      `${startupNavigationCase.name} retained a startup write for pagehide`,
    );
    await startupContext.close();
  }

  const normalPending = {
    sort: "title_asc",
    mode: "doujin",
    offset: 90,
    updated_at: new Date(Date.now() - 10_000).toISOString(),
    event_id: "normal-pending-n",
    initial_offsets: { all: 0, doujin: 90, series: 0 },
  };
  const preservedPendingStore = createLibraryPageStateStore(null, { postDelayMs: 2_000, postStatus: 503 });
  const preservedPendingContext = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: "block" });
  await preservedPendingContext.addInitScript(({ key, pending }) => {
    localStorage.setItem(key, JSON.stringify({ items: [pending] }));
  }, { key: LIBRARY_PAGE_PENDING_KEY, pending: normalPending });
  await mockCatalogAPI(preservedPendingContext, { libraryPageStore: preservedPendingStore });
  const preservedPendingPage = await preservedPendingContext.newPage();
  await preservedPendingPage.goto(`${base}/v2/library?kind=doujin&sort=title_asc&offset=126`, { waitUntil: "domcontentloaded" });
  const initialOfflinePost = await waitForLibraryStateStoreRequest(
    preservedPendingStore,
    0,
    (request) => request.method === "POST" && request.states?.some((state) => state.event_id === normalPending.event_id),
  );
  await waitForLibraryCatalog(preservedPendingPage, 8, "分页测试作品 127");
  const explicitRoute = new URL(preservedPendingPage.url());
  assert.equal(explicitRoute.pathname, "/v2/library", "fully explicit startup did not remain on Library before leaving");
  assert.equal(explicitRoute.searchParams.get("kind"), "doujin", "fully explicit startup did not apply its mode before leaving");
  assert.equal(explicitRoute.searchParams.get("sort"), "title_asc", "fully explicit startup did not apply its sort before leaving");
  assert.equal(explicitRoute.searchParams.get("offset"), "126", "fully explicit startup did not apply page 8 before leaving");
  const explicitAppliedAt = await preservedPendingPage.evaluate(() => performance.now());
  const pendingBeforeLeaving = await preservedPendingPage.evaluate((key) => {
    const raw = localStorage.getItem(key);
    return raw ? JSON.parse(raw)?.items || [] : [];
  }, LIBRARY_PAGE_PENDING_KEY);
  assert.deepEqual(
    pendingBeforeLeaving.map((item) => item.event_id),
    [normalPending.event_id],
    "staged startup A replaced normal pending N before promotion",
  );
  await preservedPendingPage.getByRole("navigation", { name: "主要导航" })
    .getByRole("button", { name: /首页/u })
    .click();
  const explicitToLeaveMs = await preservedPendingPage.evaluate((appliedAt) => performance.now() - appliedAt, explicitAppliedAt);
  assert(explicitToLeaveMs < 450, `test did not leave within staged startup A window: ${explicitToLeaveMs.toFixed(1)}ms`);
  await preservedPendingPage.locator(".home-view").waitFor({ state: "visible" });
  const pendingAfterLeaving = await preservedPendingPage.evaluate((key) => {
    const raw = localStorage.getItem(key);
    return raw ? JSON.parse(raw)?.items || [] : [];
  }, LIBRARY_PAGE_PENDING_KEY);
  assert.deepEqual(
    pendingAfterLeaving.map((item) => item.event_id),
    [normalPending.event_id],
    `canceling startup A did not restore normal pending N: ${JSON.stringify(pendingAfterLeaving)}`,
  );
  await waitForLibraryStateStoreRequest(
    preservedPendingStore,
    0,
    (request) => request === initialOfflinePost,
    { completed: true, timeoutMs: 3_000 },
  );
  const postAttemptsBeforeOnline = preservedPendingStore.requests.filter((request) => request.method === "POST");
  assert(
    postAttemptsBeforeOnline.flatMap((request) => request.states || []).every((state) => state.event_id === normalPending.event_id),
    `canceled staged startup A leaked into an offline delivery attempt: ${JSON.stringify(postAttemptsBeforeOnline)}`,
  );

  preservedPendingStore.setPostDelay(0);
  preservedPendingStore.setPostStatus(200);
  const onlineRequestStart = preservedPendingStore.requests.length;
  await preservedPendingPage.evaluate(() => window.dispatchEvent(new Event("online")));
  const deliveredNormalPending = await waitForLibraryStateStoreRequest(
    preservedPendingStore,
    onlineRequestStart,
    (request) => request.method === "POST"
      && request.status === 200
      && request.states?.some((state) => state.event_id === normalPending.event_id),
    { completed: true },
  );
  assert.deepEqual(
    deliveredNormalPending.states.map((state) => state.event_id),
    [normalPending.event_id],
    "normal pending N was not delivered independently after startup A was canceled",
  );
  assert.equal(preservedPendingStore.state?.sort, normalPending.sort, "delivered normal pending N lost its sort");
  assert.equal(preservedPendingStore.state?.positions?.doujin?.offset, normalPending.offset, "delivered normal pending N lost its page");
  const attemptedEventIDs = preservedPendingStore.requests
    .filter((request) => request.method === "POST")
    .flatMap((request) => request.states || [])
    .map((state) => state.event_id);
  assert(
    attemptedEventIDs.every((eventID) => eventID === normalPending.event_id),
    `canceled staged startup A was promoted or delivered: ${JSON.stringify(attemptedEventIDs)}`,
  );
  const pendingAfterDelivery = await preservedPendingPage.evaluate((key) => localStorage.getItem(key), LIBRARY_PAGE_PENDING_KEY);
  assert.equal(pendingAfterDelivery, null, "normal pending N remained queued after successful delivery ACK");
  assert.equal(new URL(preservedPendingPage.url()).pathname, "/v2/", "normal pending delivery forced navigation back to Library");
  await preservedPendingContext.close();

  const overlayStartupStore = createLibraryPageStateStore();
  const overlayStartupContext = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: "block" });
  await mockCatalogAPI(overlayStartupContext, { libraryPageStore: overlayStartupStore });
  const overlayStartupPage = await overlayStartupContext.newPage();
  await overlayStartupPage.goto(`${base}/v2/library?kind=doujin&sort=title_asc&offset=126`, { waitUntil: "domcontentloaded" });
  await waitForLibraryCatalog(overlayStartupPage, 8, "分页测试作品 127");
  const overlayRoute = new URL(overlayStartupPage.url());
  assert.equal(overlayRoute.searchParams.get("kind"), "doujin", "overlay startup did not apply its explicit mode");
  assert.equal(overlayRoute.searchParams.get("sort"), "title_asc", "overlay startup did not apply its explicit sort");
  assert.equal(overlayRoute.searchParams.get("offset"), "126", "overlay startup did not apply page 8");
  const overlayRouteAppliedAt = await overlayStartupPage.evaluate(() => performance.now());
  await overlayStartupPage.locator(".catalog-grid .book-card").first().click();
  const overlayOpenedAfterMs = await overlayStartupPage.evaluate((appliedAt) => performance.now() - appliedAt, overlayRouteAppliedAt);
  assert(overlayOpenedAfterMs < 450, `detail overlay did not open within staged startup A window: ${overlayOpenedAfterMs.toFixed(1)}ms`);
  await overlayStartupPage.getByRole("dialog", { name: "分页测试作品 127" }).waitFor({ state: "visible" });
  await overlayStartupPage.goBack();
  const overlayClosedAfterMs = await overlayStartupPage.evaluate((appliedAt) => performance.now() - appliedAt, overlayRouteAppliedAt);
  assert(overlayClosedAfterMs < 450, `Back did not close the detail overlay within staged startup A window: ${overlayClosedAfterMs.toFixed(1)}ms`);
  await overlayStartupPage.locator(".detail-overlay").waitFor({ state: "detached" });
  assert.equal(
    overlayStartupStore.requests.filter((request) => request.method === "POST").length,
    0,
    "startup A was promoted before the detail overlay and Back sequence completed",
  );
  await waitForLibraryCatalog(overlayStartupPage, 8, "分页测试作品 127");
  const overlayStartupPost = await waitForLibraryStateStoreRequest(
    overlayStartupStore,
    0,
    (request) => request.method === "POST"
      && request.status === 200
      && request.states?.some((state) => state.sort === "title_asc" && state.mode === "doujin" && state.offset === 126),
    { completed: true },
  );
  assert.equal(overlayStartupPost.states.length, 1, "overlay startup A was not delivered as one independent mutation");
  assert.equal(overlayStartupStore.state?.sort, "title_asc", "overlay startup A lost its explicit sort");
  assert.equal(overlayStartupStore.state?.positions?.doujin?.offset, 126, "overlay startup A lost page 8");
  assert.equal(
    await overlayStartupPage.evaluate((key) => localStorage.getItem(key), LIBRARY_PAGE_PENDING_KEY),
    null,
    "overlay startup A remained pending after its successful ACK",
  );
  await overlayStartupContext.close();

  for (const hydrationCase of [
    { name: "timeout", behavior: { delayMs: 1_600, snapshot: true } },
    { name: "failure", behavior: { status: 503 } },
  ]) {
    const hydrationStore = createLibraryPageStateStore(
      libraryPageStateFixture({ all: 36, eventID: `hydration-${hydrationCase.name}` }),
      { defaultGetBehavior: hydrationCase.behavior },
    );
    const hydrationContext = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: "block" });
    await mockCatalogAPI(hydrationContext, { libraryPageStore: hydrationStore });
    await hydrationContext.addInitScript(() => {
      localStorage.setItem("bmanga.v2.libraryPageScopes.v1", JSON.stringify({ sort: "added_desc", offsets: { all: 144 } }));
    });
    const hydrationPage = await hydrationContext.newPage();
    await hydrationPage.goto(`${base}/v2/library`, { waitUntil: "domcontentloaded" });
    await waitForLibraryCatalog(hydrationPage, 9, "分页测试作品 145");
    await delay(750);
    assert.equal(
      hydrationStore.requests.filter((request) => request.method === "POST").length,
      0,
      `${hydrationCase.name} hydration posted an unverified origin-local page`,
    );
    await hydrationPage.evaluate(() => window.dispatchEvent(new Event("pagehide")));
    await delay(100);
    assert.equal(
      hydrationStore.requests.filter((request) => request.method === "POST").length,
      0,
      `${hydrationCase.name} pagehide flushed an unverified origin-local page`,
    );
    await hydrationContext.close();
  }

  const staleStore = createLibraryPageStateStore(libraryPageStateFixture({ doujin: 90, eventID: "stale-seed" }));
  const staleContext = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: "block" });
  await mockCatalogAPI(staleContext, { libraryPageStore: staleStore });
  const stalePage = await staleContext.newPage();
  await stalePage.goto(`${base}/v2/library?kind=doujin`, { waitUntil: "domcontentloaded" });
  await waitForLibraryCatalog(stalePage, 6, "分页测试作品 91");
  staleStore.queueGetResponse({ delayMs: 900, snapshot: true });
  const staleGetRequest = stalePage.waitForRequest((request) => {
    const url = new URL(request.url());
    return url.pathname === "/api/library-page-state" && request.method() === "GET";
  });
  const staleGetResponse = stalePage.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/library-page-state" && response.request().method() === "GET";
  });
  await stalePage.evaluate(() => window.dispatchEvent(new Event("focus")));
  await staleGetRequest;

  const stalePager = stalePage.getByRole("navigation", { name: "书库分页" });
  const localBatchWrite = stalePage.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/library-page-state" && response.request().method() === "POST";
  });
  await stalePager.locator("input[type=number]").fill("8");
  await stalePager.getByRole("button", { name: "跳转", exact: true }).click();
  await waitForLibraryCatalog(stalePage, 8, "分页测试作品 127");
  const staleTypeTabs = stalePage.getByRole("group", { name: "内容类型" });
  await staleTypeTabs.getByRole("button", { name: "漫画系列", exact: true }).click();
  await waitForLibraryCatalog(stalePage, 1, "分页测试系列 1");
  await stalePager.locator("input[type=number]").fill("3");
  await stalePager.getByRole("button", { name: "跳转", exact: true }).click();
  await waitForLibraryCatalog(stalePage, 3, "分页测试系列 37");
  await localBatchWrite;
  const stalePost = staleStore.requests.filter((request) => request.method === "POST").at(-1);
  assert.deepEqual(stalePost?.states?.map((state) => state.mode).sort(), ["doujin", "series"], "local page changes were not saved as one mode batch");
  assert.equal(stalePost?.states?.find((state) => state.mode === "doujin")?.offset, 126, "local batch lost the doujin page");
  assert.equal(stalePost?.states?.find((state) => state.mode === "series")?.offset, 36, "local batch lost the series page");
  await staleGetResponse;
  await waitForLibraryCatalog(stalePage, 3, "分页测试系列 37");
  assert.equal(new URL(stalePage.url()).searchParams.get("kind"), "series", "late stale GET rolled the visible mode back");
  assert.equal(new URL(stalePage.url()).searchParams.get("offset"), "36", "late stale GET rolled the visible page back");
  await delay(100);
  assert.equal(staleStore.requests.filter((request) => request.method === "POST").length, 1, "late stale GET caused a feedback write");
  await staleContext.close();

  const visibilityStore = createLibraryPageStateStore(
    libraryPageStateFixture({ doujin: 90, eventID: "visibility-seed" }),
    { postDelayMs: 300 },
  );
  const visibilityContext = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: "block" });
  await visibilityContext.addInitScript(() => {
    let testVisibilityState = "visible";
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => testVisibilityState });
    Object.defineProperty(document, "hidden", { configurable: true, get: () => testVisibilityState === "hidden" });
    window.__setBmangaTestVisibility = (nextState) => {
      testVisibilityState = nextState;
      document.dispatchEvent(new Event("visibilitychange"));
    };
  });
  await mockCatalogAPI(visibilityContext, { libraryPageStore: visibilityStore });
  const visibilityLAN = await visibilityContext.newPage();
  await visibilityLAN.goto(`${base}/v2/library?kind=doujin`, { waitUntil: "domcontentloaded" });
  await waitForLibraryCatalog(visibilityLAN, 6, "分页测试作品 91");
  const visibilityWAN = await visibilityContext.newPage();
  await visibilityWAN.goto(`${alternateBase}/v2/library?kind=doujin`, { waitUntil: "domcontentloaded" });
  await waitForLibraryCatalog(visibilityWAN, 6, "分页测试作品 91");
  await visibilityLAN.bringToFront();
  assert.equal(await visibilityLAN.evaluate(() => document.visibilityState), "visible", "LAN page did not become visible before the switch test");

  const visibilityRequestStart = visibilityStore.requests.length;
  const visibilityPager = visibilityLAN.getByRole("navigation", { name: "书库分页" });
  await visibilityPager.locator("input[type=number]").fill("9");
  await visibilityPager.getByRole("button", { name: "跳转", exact: true }).click();
  await visibilityLAN.waitForFunction(() => document.querySelector('nav[aria-label="书库分页"] input[type="number"]')?.value === "9");
  await visibilityLAN.evaluate(() => window.__setBmangaTestVisibility("hidden"));
  await visibilityWAN.bringToFront();
  await visibilityWAN.evaluate(() => window.__setBmangaTestVisibility("visible"));
  const hiddenFlush = await waitForLibraryStateStoreRequest(
    visibilityStore,
    visibilityRequestStart,
    (request) => request.method === "POST" && request.origin === new URL(base).origin,
  );
  assert.equal(await visibilityLAN.evaluate(() => document.visibilityState), "hidden", "LAN page did not enter the hidden flush path");
  assert.equal(await visibilityWAN.evaluate(() => document.visibilityState), "visible", "WAN page did not enter the visible refresh path");
  const firstVisibleGet = await waitForLibraryStateStoreRequest(
    visibilityStore,
    visibilityRequestStart,
    (request) => request.method === "GET" && request.origin === new URL(alternateBase).origin,
    { completed: true },
  );
  await waitForLibraryCatalog(visibilityWAN, 6, "分页测试作品 91");
  await waitForLibraryStateStoreRequest(
    visibilityStore,
    visibilityRequestStart,
    (request) => request === hiddenFlush,
    { completed: true },
  );
  const firstVisibleGetIndex = visibilityStore.requests.indexOf(firstVisibleGet);
  await waitForLibraryStateStoreRequest(
    visibilityStore,
    firstVisibleGetIndex + 1,
    (request) => request.method === "GET" && request.origin === new URL(alternateBase).origin,
    { completed: true, timeoutMs: 3_000 },
  );
  await waitForLibraryCatalog(visibilityWAN, 9, "分页测试作品 145");
  const visibilityPost = visibilityStore.requests.filter((request) => request.method === "POST").at(-1);
  assert.equal(visibilityPost?.origin, new URL(base).origin, "hidden flush was not sent by the LAN origin");
  assert.equal(visibilityPost?.state?.offset, 144, "hidden flush saved the wrong page");
  assert.equal(visibilityStore.state?.positions?.doujin?.offset, 144, "visible followup did not converge on the hidden origin page");
  await visibilityContext.close();

  const sharedClock = "2025-01-01T00:00:00.000Z";
  const sharedStore = createLibraryPageStateStore({
    version: 1,
    sort: "added_desc",
    sort_updated_at: sharedClock,
    sort_event_id: "shared-seed",
    positions: {
      all: { offset: 0, updated_at: sharedClock, event_id: "shared-seed" },
      doujin: { offset: 90, updated_at: sharedClock, event_id: "shared-seed" },
      series: { offset: 0, updated_at: sharedClock, event_id: "shared-seed" },
    },
    updated_at: sharedClock,
  });
  const syncContext = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: "block" });
  await mockCatalogAPI(syncContext, { libraryPageStore: sharedStore });

  const lanPage = await syncContext.newPage();
  await lanPage.addInitScript(() => {
    localStorage.setItem("bmanga.pagination.origin-probe", "lan");
    localStorage.setItem("bmanga.v2.libraryPageScopes.v1", JSON.stringify({ sort: "added_desc", offsets: { doujin: 18 } }));
  });
  await lanPage.goto(`${base}/v2/library?kind=doujin`, { waitUntil: "domcontentloaded" });
  await waitForLibraryCatalog(lanPage, 6, "分页测试作品 91");

  const wanPage = await syncContext.newPage();
  await wanPage.addInitScript(() => {
    localStorage.setItem("bmanga.pagination.origin-probe", "wan");
    localStorage.setItem("bmanga.v2.libraryPageScopes.v1", JSON.stringify({ sort: "added_desc", offsets: { doujin: 36 } }));
  });
  await wanPage.goto(`${alternateBase}/v2/library?kind=doujin`, { waitUntil: "domcontentloaded" });
  await waitForLibraryCatalog(wanPage, 6, "分页测试作品 91");
  assert.equal(await lanPage.evaluate(() => localStorage.getItem("bmanga.pagination.origin-probe")), "lan", "LAN origin storage was replaced by WAN storage");
  assert.equal(await wanPage.evaluate(() => localStorage.getItem("bmanga.pagination.origin-probe")), "wan", "WAN origin storage was replaced by LAN storage");
  await delay(700);
  assert.equal(sharedStore.requests.filter((request) => request.method === "POST").length, 0, "initial remote hydration echoed a library page write");

  const lanPager = lanPage.getByRole("navigation", { name: "书库分页" });
  const lanWrite = lanPage.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/library-page-state" && response.request().method() === "POST";
  });
  await lanPager.locator("input[type=number]").fill("8");
  await lanPager.getByRole("button", { name: "跳转", exact: true }).click();
  await waitForLibraryCatalog(lanPage, 8, "分页测试作品 127");
  await lanWrite;
  const lanPost = sharedStore.requests.filter((request) => request.method === "POST").at(-1);
  assert.equal(lanPost?.origin, new URL(base).origin, "LAN page state write used the wrong origin");
  assert.equal(lanPost?.state?.mode, "doujin", "LAN page state write lost the doujin scope");
  assert.equal(lanPost?.state?.offset, 126, "LAN page state write saved the wrong offset");

  const wanRefresh = wanPage.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/library-page-state" && response.request().method() === "GET";
  });
  await wanPage.evaluate(() => window.dispatchEvent(new Event("focus")));
  await wanRefresh;
  await waitForLibraryCatalog(wanPage, 8, "分页测试作品 127");
  await delay(700);
  assert.equal(sharedStore.requests.filter((request) => request.method === "POST").length, 1, "WAN remote apply echoed LAN state back to the server");

  const wanPager = wanPage.getByRole("navigation", { name: "书库分页" });
  const wanWrite = wanPage.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/library-page-state" && response.request().method() === "POST";
  });
  await wanPager.locator("input[type=number]").fill("11");
  await wanPager.getByRole("button", { name: "跳转", exact: true }).click();
  await waitForLibraryCatalog(wanPage, 11, "分页测试作品 181");
  await wanWrite;
  const wanPost = sharedStore.requests.filter((request) => request.method === "POST").at(-1);
  assert.equal(wanPost?.origin, new URL(alternateBase).origin, "WAN page state write used the wrong origin");
  assert.equal(wanPost?.state?.mode, "doujin", "WAN page state write lost the doujin scope");
  assert.equal(wanPost?.state?.offset, 180, "WAN page state write saved the wrong offset");

  const lanRefresh = lanPage.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/library-page-state" && response.request().method() === "GET";
  });
  await lanPage.evaluate(() => window.dispatchEvent(new Event("focus")));
  await lanRefresh;
  await waitForLibraryCatalog(lanPage, 11, "分页测试作品 181");
  await delay(700);
  assert.equal(sharedStore.requests.filter((request) => request.method === "POST").length, 2, "LAN remote apply echoed WAN state back to the server");
  await syncContext.close();

  const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: "block" });
  const apiRequests = [];
  context.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname.startsWith("/api/")) apiRequests.push(url);
  });
  await mockCatalogAPI(context);

  const page = await context.newPage();
  await page.goto(`${base}/v2/library`, { waitUntil: "domcontentloaded" });
  const pager = page.getByRole("navigation", { name: "书库分页" });
  await pager.waitFor({ state: "visible" });
  assert.equal(apiRequests.some((url) => url.pathname === "/api/reading-history"), false, "Library requested Home/My reading history");
  assert.equal(apiRequests.some((url) => url.pathname === "/api/shelf" && url.searchParams.get("limit") === "12"), false, "Library requested Home recent arrivals");
  assert.equal(apiRequests.some((url) => url.pathname === "/api/work"), false, "Library requested Home hero detail");
  assert.equal(await pager.locator(".pagination-page").count(), 6, "desktop compact page rail is missing numbered controls");
  assert.equal(await pager.locator(".pagination-ellipsis").count(), 1, "desktop compact page rail is missing its ellipsis");
  assert.equal(await pager.locator("input[type=number]").getAttribute("max"), "1739", "page jump maximum is wrong");

  await waitForRequestCount(apiRequests, "/api/shelf", 18);
  const prefetchedSecondPageRequests = catalogRequestCount(apiRequests, "/api/shelf", 18);
  await pager.getByRole("button", { name: "下一页", exact: true }).click();
  await waitForLibraryCatalog(page, 2, "分页测试作品 19");
  assert.equal(catalogRequestCount(apiRequests, "/api/shelf", 18), prefetchedSecondPageRequests, "opening the prefetched next page issued a duplicate request");
  const cachedFirstPageRequests = catalogRequestCount(apiRequests, "/api/shelf", 0);
  await pager.getByRole("button", { name: "首页", exact: true }).click();
  await waitForLibraryCatalog(page, 1, "分页测试作品 1");
  assert.equal(catalogRequestCount(apiRequests, "/api/shelf", 0), cachedFirstPageRequests, "returning to a cached page issued a duplicate request");

  const jumpResponse = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/shelf" && url.searchParams.get("offset") === "2448";
  });
  await pager.locator("input[type=number]").fill("137");
  await pager.getByRole("button", { name: "跳转", exact: true }).click();
  await jumpResponse;
  await page.waitForFunction(() => new URL(location.href).searchParams.get("offset") === "2448");
  assert.equal(await pager.locator(".pagination-page[aria-current=page]").innerText(), "137", "jump did not select page 137");
  await page.waitForFunction(() => document.activeElement?.classList.contains("library-commandbar"));
  assert.equal(await page.evaluate(() => document.activeElement?.classList.contains("library-commandbar")), true, "completed pagination did not restore focus to the library summary");
  const pageImages = page.locator(".catalog-grid .book-card img");
  assert.equal(await pageImages.first().getAttribute("loading"), "eager", "deep-page first-row covers lost eager loading");
  assert.equal(await pageImages.first().getAttribute("fetchpriority"), "high", "deep-page first-row covers lost high priority");
  assert.equal(await pageImages.nth(2).getAttribute("loading"), "lazy", "only the first two visible cards should be eager");
  assert.equal(await pageImages.nth(6).getAttribute("loading"), "lazy", "below-first-row cover should stay lazy");
  assert.match(await pageImages.first().getAttribute("src") || "", /[?&]size=420(?:&|$)/u, "card cover did not reuse the 420px cache tier");
  const coverSrcSet = await pageImages.first().getAttribute("srcset") || "";
  assert.match(coverSrcSet, /[?&]size=420(?:&|\s)/u, "card cover srcset is missing the 420px tier");
  assert.match(coverSrcSet, /[?&]size=640(?:&|\s)/u, "card cover srcset is missing the 640px tier");

  const firstPageRequestsBeforeReturn = catalogRequestCount(apiRequests, "/api/shelf", 0);
  await pager.getByRole("button", { name: "首页", exact: true }).click();
  await page.waitForFunction(() => !new URL(location.href).searchParams.has("offset"));
  await waitForLibraryCatalog(page, 1, "分页测试作品 1");
  assert.equal(catalogRequestCount(apiRequests, "/api/shelf", 0), firstPageRequestsBeforeReturn, "deep-page return bypassed the first-page cache");

  const deepPageRequestsBeforeBack = catalogRequestCount(apiRequests, "/api/shelf", 2448);
  await page.goBack();
  await waitForLibraryCatalog(page, 137, "分页测试作品 2449");
  assert.equal(catalogRequestCount(apiRequests, "/api/shelf", 2448), deepPageRequestsBeforeBack, "Back bypassed the deep-page cache");
  assert.equal(await pager.locator("input[type=number]").inputValue(), "137", "Back did not restore the jumped page");

  const typeTabs = page.getByRole("group", { name: "内容类型" });
  const doujinFirstResponse = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/works" && url.searchParams.get("offset") === "0";
  });
  await typeTabs.getByRole("button", { name: "同人本", exact: true }).click();
  await doujinFirstResponse;
  assert.equal(await pager.locator("input[type=number]").inputValue(), "1", "new doujin scope did not start on page 1");

  const doujinSixthResponse = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/works" && url.searchParams.get("offset") === "90";
  });
  await pager.locator("input[type=number]").fill("6");
  await pager.getByRole("button", { name: "跳转", exact: true }).click();
  await doujinSixthResponse;

  const restoredAllRequestCount = catalogRequestCount(apiRequests, "/api/shelf", 2448);
  await typeTabs.getByRole("button", { name: "全部", exact: true }).click();
  await waitForLibraryCatalog(page, 137, "分页测试作品 2449");
  assert.equal(catalogRequestCount(apiRequests, "/api/shelf", 2448), restoredAllRequestCount, "category restore bypassed the cached all shelf");
  assert.equal(await pager.locator("input[type=number]").inputValue(), "137", "all scope did not restore page 137");

  const seriesFirstResponse = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/series" && url.searchParams.get("offset") === "0";
  });
  await typeTabs.getByRole("button", { name: "漫画系列", exact: true }).click();
  await seriesFirstResponse;
  assert.equal(await pager.locator("input[type=number]").inputValue(), "1", "new series scope did not start on page 1");

  const seriesThirdResponse = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/series" && url.searchParams.get("offset") === "36";
  });
  await pager.locator("input[type=number]").fill("3");
  await pager.getByRole("button", { name: "跳转", exact: true }).click();
  await seriesThirdResponse;

  const restoredDoujinRequestCount = catalogRequestCount(apiRequests, "/api/works", 90);
  await typeTabs.getByRole("button", { name: "同人本", exact: true }).click();
  await waitForLibraryCatalog(page, 6, "分页测试作品 91");
  assert.equal(catalogRequestCount(apiRequests, "/api/works", 90), restoredDoujinRequestCount, "category restore bypassed the cached doujin page");
  assert.equal(await pager.locator("input[type=number]").inputValue(), "6", "doujin scope did not restore page 6");

  const seriesBackRequestCount = catalogRequestCount(apiRequests, "/api/series", 36);
  await page.goBack();
  await waitForLibraryCatalog(page, 3, "分页测试系列 37");
  assert.equal(catalogRequestCount(apiRequests, "/api/series", 36), seriesBackRequestCount, "Back bypassed the cached series page");
  assert.equal(await pager.locator("input[type=number]").inputValue(), "3", "Back did not restore the series scope page");

  const doujinForwardRequestCount = catalogRequestCount(apiRequests, "/api/works", 90);
  await page.goForward();
  await waitForLibraryCatalog(page, 6, "分页测试作品 91");
  assert.equal(catalogRequestCount(apiRequests, "/api/works", 90), doujinForwardRequestCount, "Forward bypassed the cached doujin page");
  assert.equal(await pager.locator("input[type=number]").inputValue(), "6", "Forward did not restore the doujin scope page");

  await page.reload({ waitUntil: "domcontentloaded" });
  await pager.waitFor({ state: "visible" });
  const restoredAllAfterReload = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/shelf" && url.searchParams.get("offset") === "2448";
  });
  await typeTabs.getByRole("button", { name: "全部", exact: true }).click();
  await restoredAllAfterReload;
  assert.equal(await pager.locator("input[type=number]").inputValue(), "137", "all scope memory did not survive reload");

  const restoredSeriesAfterReload = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/series" && url.searchParams.get("offset") === "36";
  });
  await typeTabs.getByRole("button", { name: "漫画系列", exact: true }).click();
  await restoredSeriesAfterReload;
  assert.equal(await pager.locator("input[type=number]").inputValue(), "3", "series scope memory did not survive reload");

  const sortedSeriesResponse = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/series" && url.searchParams.get("sort") === "title_asc" && url.searchParams.get("offset") === "0";
  });
  await page.locator(".library-toolbar .select-field select").selectOption("title_asc");
  await sortedSeriesResponse;
  assert.equal(await pager.locator("input[type=number]").inputValue(), "1", "sort change did not reset the active scope");
  assert.equal(await page.evaluate(() => localStorage.getItem("bmanga.v2.libraryPageScopes.legacyMigrated.v1.title_asc")), "1", "explicit sort reset did not suppress later legacy restoration");

  const resetDoujinResponse = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/works" && url.searchParams.get("sort") === "title_asc" && url.searchParams.get("offset") === "0";
  });
  await typeTabs.getByRole("button", { name: "同人本", exact: true }).click();
  await resetDoujinResponse;
  assert.equal(await pager.locator("input[type=number]").inputValue(), "1", "sort change did not reset the other category scopes");

  await page.setViewportSize({ width: 320, height: 800 });
  const geometry = await pager.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    return {
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth,
      left: rect.left,
      right: rect.right,
      numberedRailVisible: getComputedStyle(element.querySelector(".pagination-pages")).display !== "none",
    };
  });
  assert(geometry.scrollWidth <= geometry.clientWidth, `mobile document overflow: ${JSON.stringify(geometry)}`);
  assert(geometry.left >= -1 && geometry.right <= geometry.clientWidth + 1, `mobile pager overflow: ${JSON.stringify(geometry)}`);
  assert.equal(geometry.numberedRailVisible, false, "mobile numbered rail should collapse into the page summary");
  assert.equal(await pager.locator(".pagination-main > button").count(), 4, "mobile first/previous/next/last controls are incomplete");
  assert(await pager.locator("input[type=number]").isVisible(), "mobile direct page input is hidden");
  await page.getByRole("navigation", { name: "手机导航" }).getByRole("button", { name: /首页/u }).click();
  await page.waitForFunction(() => document.activeElement?.id === "main-content");
  assert.equal(await page.title(), "首页 · bmanga", "view change did not update the document title");
  await context.close();
  process.stdout.write("pagination UI smoke passed at desktop and 320px\n");
} finally {
  if (browser) await browser.close();
  vite.kill();
  await delay(100);
}
