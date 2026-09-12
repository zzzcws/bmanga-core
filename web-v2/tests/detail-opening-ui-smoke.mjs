import assert from "node:assert/strict";
import { setTimeout as delay } from "node:timers/promises";
import { coverSVG, progress, series, seriesDetail, syntheticUI } from "./helpers/synthetic-ui.mjs";

// All titles, identifiers, marks and chapter data are generated fixtures.
let pendingProgress;
let failProgress = false;
let currentProgress = progress("Alpha");
let detailCount = 32;
let detailGate;
let progressSaveGate;
let progressSaves = 0;
let progressReads = 0;
const ui = await syntheticUI({ routeAPI: async ({ request, route, url, json }) => {
  const id = url.searchParams.get("id") || url.searchParams.get("group_id");
  if (url.pathname === "/page") return route.fulfill({ contentType: "image/svg+xml", body: coverSVG });
  if (url.pathname === "/api/series-detail") {
    const snapshot = seriesDetail(id, detailCount);
    if (detailGate) await detailGate.promise;
    return json(snapshot);
  }
  if (url.pathname === "/api/series-progress") {
    progressReads++;
    const result = id === "Alpha" ? currentProgress : progress("Beta", 8, 10);
    const failed = failProgress, held = pendingProgress;
    if (held) await held.promise;
    return failed ? json({ error: "Synthetic progress temporarily unavailable" }, 503) : json({ group_id: id, progress: result });
  }
  if (["/api/shelf", "/api/series", "/api/works"].includes(url.pathname)) return json({ total: 2, items: [series("Alpha"), series("Beta")] });
  if (url.pathname === "/api/progress" && request.method() === "GET") return json({
    candidate_id: id, work_identity_id: `${id}-identity`, page_manifest_id: `${id}-manifest`,
    manifest_hash: `${id}-hash`, count: 16, progress: currentProgress,
  });
  if (url.pathname === "/api/pages" && request.method() === "GET") return json({
    candidate_id: id, work_identity_id: `${id}-identity`, page_manifest_id: `${id}-manifest`,
    manifest_hash: `${id}-hash`, count: 16, readable: true,
    pages: Array.from({ length: 16 }, (_, index) => ({ index, relative_path: `synthetic/${index}.svg`, extension: ".svg", size_bytes: 100 })),
  });
  if (url.pathname === "/api/progress" && request.method() === "POST") {
    progressSaves++;
    const body = request.postDataJSON();
    if (progressSaveGate) await progressSaveGate.promise;
    currentProgress = { ...body, candidate_id: body.candidate_id, last_read_at: body.updated_at, progress_percent: (body.index + 1) / body.count * 100 };
    return json({ ok: true, stored: true, progress: currentProgress,
      acknowledged_client_event_id: body.client_event_id, acknowledged_updated_at: body.updated_at });
  }
  if (url.pathname === "/api/user-mark" && request.method() === "POST") {
    const body = request.postDataJSON();
    return json({ ok: true, mark: { ...body, favorite: Boolean(body.favorite), notes: body.notes || "" } });
  }
  return false;
} });
const { page } = ui;
const directory = page.locator(".series-directory");
const panel = page.locator(".detail-panel");
const feedback = page.locator(".series-progress-check");
const open = async (name) => page.locator('.book-card[data-kind="series"]').filter({ hasText: `Synthetic ${name}` }).first().click();
const close = async () => { await page.getByRole("button", { name: "Close details", exact: true }).click(); await panel.waitFor({ state: "hidden" }); };
const snapshot = () => page.evaluate(() => ({
  scroll: document.querySelector(".detail-body").scrollTop,
  focus: document.activeElement?.outerHTML,
  calls: window.__syntheticFocusCalls.length,
  draft: document.querySelector(".detail-panel textarea")?.value,
  expanded: [...document.querySelectorAll(".series-outline-toggle")].map((node) => node.getAttribute("aria-expanded")),
}));
const stable = async (before, label) => {
  const after = await snapshot();
  assert(Math.abs(after.scroll - before.scroll) <= 2, `${label}: scroll jumped ${before.scroll} -> ${after.scroll}`);
  assert.equal(after.focus, before.focus, `${label}: focused element changed`);
  assert.equal(after.calls, before.calls, `${label}: background update called focus()`);
  assert.equal(after.draft, before.draft, `${label}: unsaved note changed`);
  assert.deepEqual(after.expanded, before.expanded, `${label}: directory expansion changed`);
};
try {
  pendingProgress = ui.gate();
  await page.goto(`${ui.base}/v2/?view=library`, { waitUntil: "domcontentloaded" });
  await open("Alpha");
  await directory.waitFor({ state: "visible" });
  await feedback.waitFor({ state: "visible" });
  assert.equal(await page.locator(".series-detail").getAttribute("data-progress-state"), "loading");
  assert.match(await feedback.innerText(), /loading|checking/iu);
  assert.equal(progressReads, 1);
  assert(!/\bunread\b/iu.test(await panel.locator(".series-detail-intro").innerText()), "Pending progress was represented as unread");

  // Deliberately move focus and scroll before the delayed progress resolves.
  await directory.locator(".series-outline-toggle").nth(1).click();
  await directory.locator(".series-outline-toggle").first().click();
  await panel.locator("textarea").fill("Synthetic unsaved note");
  await directory.locator(".series-outline-toggle").nth(1).evaluate((element) => element.focus({ preventScroll: true }));
  assert(await directory.locator(".series-outline-toggle").nth(1).evaluate((element) => element === document.activeElement), "Focus must be on an enabled directory control");
  await page.locator(".detail-body").evaluate((element) => { element.scrollTop = Math.min(1700, element.scrollHeight - element.clientHeight); });
  const beforeProgress = await snapshot();
  assert.deepEqual(beforeProgress.expanded, ["false", "true"], "Exercise the user's explicit collapsed-section choice");
  assert(beforeProgress.scroll > 100, "Fixture must produce a scrollable directory");
  pendingProgress.release(); pendingProgress = null;
  await page.locator(".series-resume-location").filter({ hasText: "6 / 16" }).waitFor();
  await stable(beforeProgress, "Delayed progress completion");

  // The same already-open dialog receives a real synthetic mark response.
  await panel.locator(".favorite-button").evaluate((element) => element.click());
  await page.waitForFunction(() => document.querySelector(".favorite-button")?.getAttribute("aria-pressed") === "true");
  await stable(beforeProgress, "Mark completion");
  await page.evaluate(() => {
    window.dispatchEvent(new Event("blur"));
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" });
    document.dispatchEvent(new Event("visibilitychange"));
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    document.dispatchEvent(new Event("visibilitychange"));
    window.dispatchEvent(new Event("focus"));
  });
  await page.setViewportSize({ width: 390, height: 740 });
  await page.setViewportSize({ width: 390, height: 844 });
  await delay(250);
  await stable(beforeProgress, "Visibility and mobile browser-chrome resize");
  progressSaveGate = ui.gate();
  await page.evaluate(async () => {
    const queue = await import("/v2/src/lib/progressQueue.ts");
    queue.enqueuePendingProgress({ candidate_id: "Alpha-chapter-4", work_identity_id: "Alpha-chapter-4-identity",
      page_manifest_id: "Alpha-chapter-4-manifest", manifest_hash: "Alpha-chapter-4-hash",
      index: 7, count: 16, completed: false, updated_at: new Date().toISOString() }, "Alpha-chapter-4-identity");
    window.dispatchEvent(new Event("online"));
  });
  for (let attempt = 0; !progressSaves && attempt < 60; attempt++) await delay(50);
  assert.equal(progressSaves, 1, "Exercise the actual queued progress acknowledgement path");
  progressSaveGate.release(); progressSaveGate = null;
  await page.locator(".series-resume-location").filter({ hasText: "8 / 16" }).waitFor();
  await stable(beforeProgress, "Directory item patched by delayed progress acknowledgement");
  await page.locator(".series-resume-location").getByRole("button", { name: "Locate in directory", exact: true }).click();
  await page.waitForFunction(() => document.querySelector(".series-outline-toggle")?.getAttribute("aria-expanded") === "true");
  assert(await page.locator("#series-current-entry").isVisible(), "Explicit location must reopen the current chapter's section");
  await page.waitForFunction(() => {
    const item = document.querySelector("#series-current-entry")?.getBoundingClientRect();
    const body = document.querySelector(".detail-body")?.getBoundingClientRect();
    return item && body && item.top >= body.top - 2 && item.bottom <= body.bottom + 2;
  }).catch(async () => {
    const geometry = await page.evaluate(() => ({
      item: document.querySelector("#series-current-entry")?.getBoundingClientRect().toJSON(),
      body: document.querySelector(".detail-body")?.getBoundingClientRect().toJSON(),
      scrollTop: document.querySelector(".detail-body")?.scrollTop,
      scrollCalls: window.__syntheticScrollCalls,
    }));
    assert.fail(`Explicit location did not bring the chapter into view: ${JSON.stringify(geometry)}`);
  });
  // A draft is intentionally unsaved; reset before closing to avoid discarding it implicitly.
  await panel.locator("textarea").fill("");
  await close();

  failProgress = true;
  await open("Alpha");
  await directory.waitFor();
  await feedback.getByRole("button", { name: /retry/iu }).waitFor();
  assert.equal(await page.locator(".series-detail").getAttribute("data-progress-state"), "error");
  assert(!/\bunread\b/iu.test(await panel.locator(".series-detail-intro").innerText()), "Failed progress was represented as unread");
  failProgress = false;
  const readsBeforeRetry = progressReads;
  await feedback.getByRole("button", { name: /retry/iu }).click();
  await page.locator(".series-resume-location").filter({ hasText: "8 / 16" }).waitFor();
  assert.equal(progressReads, readsBeforeRetry + 1, "Progress retry did not issue exactly one request");
  await close();

  pendingProgress = ui.gate();
  await open("Alpha");
  await directory.waitFor();
  await close();
  const oldResponse = pendingProgress;
  pendingProgress = null;
  await open("Beta");
  await page.locator(".series-resume-location").filter({ hasText: "11 / 16" }).waitFor();
  oldResponse.release();
  await delay(250);
  assert.match(await page.locator("#detail-heading").innerText(), /Synthetic Beta/u);
  assert.match(await page.locator(".series-resume-location").innerText(), /11 \/ 16/u);
  await close();

  // A summary for a later 50-item range must not override a range the user
  // deliberately selected while waiting. Only explicit Locate may navigate it.
  detailCount = 160;
  currentProgress = progress("Alpha", 75, 9);
  pendingProgress = ui.gate();
  await open("Alpha"); await directory.waitFor();
  const range = directory.locator(".series-range-select select").first();
  await range.selectOption("1");
  await range.selectOption("0");
  await range.focus();
  await page.locator(".detail-body").evaluate((element) => { element.scrollTop = 1200; });
  const beforeRange = await snapshot();
  pendingProgress.release(); pendingProgress = null;
  await page.locator(".series-resume-location").filter({ hasText: "10 / 16" }).waitFor();
  assert.equal(await range.inputValue(), "0", "Late summary navigated away from the user's first range");
  await stable(beforeRange, "Late summary must preserve explicitly selected 50-item range");
  await page.locator(".series-resume-location").getByRole("button", { name: "Locate in directory", exact: true }).click();
  await page.waitForFunction(() => document.querySelector(".series-range-select select")?.value === "1");
  assert.match(await page.locator("#series-current-entry").innerText(), /Synthetic chapter 75/u);
  await close();
  detailCount = 32;

  // Late directory responses are fenced by the same opening-session identity.
  detailGate = ui.gate();
  await open("Alpha");
  await page.getByRole("button", { name: "Cancel opening work", exact: true }).click();
  const oldDetail = detailGate; detailGate = null;
  await open("Beta"); await directory.waitFor();
  oldDetail.release(); await delay(200);
  assert.match(await page.locator("#detail-heading").innerText(), /Synthetic Beta/u);
  assert.equal(await page.locator(".detail-body").evaluate((element) => element.scrollTop), 0, "Intentional opening must start at the top");
  await page.setViewportSize({ width: 320, height: 740 });
  assert(await panel.evaluate((element) => element.getBoundingClientRect().right <= innerWidth + 1), "320px detail overflows viewport");
  assert(ui.requests.filter((request) => request.path === "/api/series-progress").every((request) => new URLSearchParams(request.search).get("manifest") === "stored"), "Detail summary must use stored manifests");
  ui.assertClean();
  process.stdout.write("Synthetic detail smoke passed: progressive directory, retry, stale-session fencing, focus/scroll, 320px layout.\n");
} finally { await ui.close(); }
