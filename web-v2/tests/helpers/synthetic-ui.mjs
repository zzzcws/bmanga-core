import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { createRequire } from "node:module";
import { createServer } from "node:net";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as delay } from "node:timers/promises";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const projectRoot = resolve(webRoot, "..");
const toolRoot = process.env.BMANGA_UI_SMOKE_TOOL_ROOT
  ? resolve(process.env.BMANGA_UI_SMOKE_TOOL_ROOT) : join(projectRoot, ".tools", "ui-smoke");

export function deferred() {
  let release;
  const promise = new Promise((resolvePromise) => { release = resolvePromise; });
  return { promise, release };
}

export function chapter(seriesID, number) {
  const candidateID = `${seriesID}-chapter-${number}`;
  return {
    candidate_id: candidateID, work_identity_id: `${candidateID}-identity`,
    title: `Synthetic chapter ${number}`, display_title: `Synthetic chapter ${number}`,
    chapter: String(number), sequence_number: number, series_title: `Synthetic ${seriesID}`,
    candidate_type: "manga_archive", library_name: "Synthetic fixtures", library_key: "synthetic",
    source_kind: "archive", extension: ".zip", relative_path: `synthetic/${candidateID}.zip`,
    can_read: true, readable_page_count: 16, page_count_status: "counted",
  };
}

export function series(seriesID, count = 32) {
  return {
    shelf_type: "series", group_id: seriesID, series_title: `Synthetic ${seriesID}`,
    display_title: `Synthetic ${seriesID}`, selected_candidate_id: `${seriesID}-chapter-1`,
    counted_items: count, item_count: count, candidate_count: count, can_read: true,
    library_key: "synthetic", library_name: "Synthetic fixtures", section_count: 1,
  };
}

export function progress(seriesID, number = 4, index = 5) {
  const candidateID = `${seriesID}-chapter-${number}`;
  return {
    candidate_id: candidateID, work_identity_id: `${candidateID}-identity`,
    page_manifest_id: `${candidateID}-manifest`, manifest_hash: `${candidateID}-hash`,
    index, count: 16, completed: false, progress_percent: (index + 1) / 16 * 100,
    updated_at: "2026-01-02T03:04:05Z", last_read_at: "2026-01-02T03:04:05Z",
  };
}

export function seriesDetail(seriesID, count = 32) {
  const items = Array.from({ length: count }, (_, index) => chapter(seriesID, index + 1));
  const groups = items.map((item, index) => ({
    key: `${seriesID}-group-${index}`, label: `Synthetic chapter ${index + 1}`,
    sort: index, sequence: index + 1, items: [item], primary: item,
  }));
  const middle = Math.ceil(groups.length / 2);
  return {
    series: { ...series(seriesID, count), section_count: 2 }, items,
    sections: [{ title: "Synthetic first half", sort: 1, groups: groups.slice(0, middle) },
      { title: "Synthetic second half", sort: 2, groups: groups.slice(middle) }],
    sectioned: true, section_summary: `${count} synthetic chapters`, cover_candidates: [],
    mark: { target_type: "series", target_id: seriesID, favorite: false, notes: "" },
  };
}

export const coverSVG = '<svg xmlns="http://www.w3.org/2000/svg" width="420" height="600"><rect width="420" height="600" fill="#244e50"/><circle cx="210" cy="230" r="110" fill="#e7c985"/></svg>';

/** A real browser with only synthetic same-origin API/asset responses. Unknown
 * API requests fail closed; the Vite proxy also points at a closed loopback port.
 * Playwright is a separately installed test tool, never a runtime dependency. */
export async function syntheticUI({ routeAPI, viewport = { width: 390, height: 844 }, deviceScaleFactor = 3 }) {
  const viteEntry = join(webRoot, "node_modules", "vite", "bin", "vite.js");
  assert(existsSync(viteEntry), "Run npm ci in web-v2 before the synthetic UI smoke");
  const { chromium } = createRequire(join(toolRoot, "package.json"))("playwright-core");
  const executablePath = [process.env.BMANGA_UI_SMOKE_BROWSER,
    chromium.executablePath(),
    "C:/Program Files/Google/Chrome/Application/chrome.exe",
    "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe"].find((path) => path && existsSync(path));
  assert(executablePath, "Set BMANGA_UI_SMOKE_BROWSER to an installed Chromium browser");
  const listener = createServer();
  await new Promise((resolveListen, reject) => {
    listener.once("error", reject);
    listener.listen(0, "127.0.0.1", resolveListen);
  });
  const port = listener.address().port;
  await new Promise((resolveClose) => listener.close(resolveClose));
  const base = `http://127.0.0.1:${port}`;
  const vite = spawn(process.execPath, [viteEntry, "--host", "127.0.0.1", "--port", String(port), "--strictPort"], {
    cwd: webRoot, windowsHide: true, stdio: "ignore",
    env: { ...process.env, BMANGA_VITE_BACKEND: "http://127.0.0.1:1" },
  });
  let spawnError;
  vite.once("error", (error) => { spawnError = error; });
  let browser;
  const requests = [], unexpected = [], pageErrors = [], gates = [];
  const gate = () => { const value = deferred(); gates.push(value); return value; };
  const close = async () => {
    for (const value of gates) value.release();
    if (browser) await browser.close();
    if (vite.exitCode === null) vite.kill(); // Only our own child, never a shared dev server.
  };
  try {
    let ready = false;
    for (let attempt = 0; attempt < 100; attempt++) {
      assert(!spawnError && vite.exitCode === null, "Owned Vite process failed before readiness");
      try { ready = (await fetch(`${base}/v2/`, { signal: AbortSignal.timeout(300), redirect: "error" })).ok; } catch { /* Startup only. */ }
      if (ready) break;
      await delay(100);
    }
    assert(ready, "Owned Vite process did not become ready");
    browser = await chromium.launch({ executablePath, headless: true });
    const context = await browser.newContext({ viewport, deviceScaleFactor, serviceWorkers: "block", reducedMotion: "reduce" });
    context.setDefaultTimeout(10_000);
    await context.addInitScript(() => {
      localStorage.setItem("bmanga.uiLocale.v1", "en");
      window.__syntheticFocusCalls = [];
      window.__syntheticScrollCalls = [];
      const originalFocus = HTMLElement.prototype.focus;
      HTMLElement.prototype.focus = function (options) {
        window.__syntheticFocusCalls.push({ tag: this.tagName, label: this.getAttribute("aria-label"), preventScroll: options?.preventScroll === true });
        return originalFocus.call(this, options);
      };
      const originalScrollIntoView = Element.prototype.scrollIntoView;
      Element.prototype.scrollIntoView = function (options) {
        window.__syntheticScrollCalls.push({ id: this.id, top: this.getBoundingClientRect().top, options });
        return originalScrollIntoView.call(this, options);
      };
    });
    await context.route("**/*", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      if (url.origin !== base) {
        unexpected.push(`external:${url.protocol}`);
        return route.abort("blockedbyclient");
      }
      if (!url.pathname.startsWith("/api/") && !/^\/(?:cover|page)(?:$|\/)/u.test(url.pathname)) return route.continue();
      requests.push({ path: url.pathname, search: url.search, method: request.method() });
      const json = (body, status = 200) => route.fulfill({ status, contentType: "application/json", headers: { "Cache-Control": "no-store" }, body: JSON.stringify(body) });
      if (await routeAPI({ route, request, url, json, gate }) !== false) return;
      if (url.pathname === "/cover") return route.fulfill({ contentType: "image/svg+xml", body: coverSVG });
      const defaults = {
        "/api/dashboard": { totals: { total_works: 64 }, libraries: [], page_status: [], actions: [] },
        "/api/health": { status: "ok" }, "/api/continue-target": { target: null },
        "/api/library-page-state": { state: null, states: [] },
        "/api/user-tags": { items: [] }, "/api/reading-history": { items: [], total: 0 },
        "/api/discover": { random_items: [], history: [], facets: [] },
      };
      if (request.method() === "GET" && Object.hasOwn(defaults, url.pathname)) return json(defaults[url.pathname]);
      if (request.method() === "POST" && url.pathname === "/api/library-page-state") {
        const body = request.postDataJSON();
        return json({ ok: true, stored: true, state: body, results: [] });
      }
      unexpected.push(`${request.method()} ${url.pathname}`);
      return json({ error: "Synthetic test has no fixture for this request" }, 501);
    });
    const page = await context.newPage();
    page.on("pageerror", (error) => pageErrors.push(error.message));
    return { page, context, base, requests, unexpected, pageErrors, gate, close,
      assertClean() { assert.deepEqual(unexpected, [], "Unexpected API or external requests"); assert.deepEqual(pageErrors, [], "Browser runtime errors"); } };
  } catch (error) { await close(); throw error; }
}
