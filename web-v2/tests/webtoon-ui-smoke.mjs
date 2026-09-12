import assert from "node:assert/strict";
import { setTimeout as delay } from "node:timers/promises";
import { chapter, syntheticUI } from "./helpers/synthetic-ui.mjs";

// Three entirely generated source strips, each 1200 x 9600. Distinct colors
// prove page order; a one-source-pixel alternating pattern proves decoding did
// not reuse the blurry longest-edge thumbnail after switching to width fit.
const colors = ["#cc4422", "#228844", "#3355bb"];
const work = { ...chapter("Strip", 1), title: "Synthetic vertical strips", display_title: "Synthetic vertical strips", readable_page_count: 3 };
const identity = { candidate_id: work.candidate_id, work_identity_id: work.work_identity_id,
  page_manifest_id: "synthetic-strip-manifest", manifest_hash: "synthetic-strip-hash", count: 3 };
const served = [], saves = [];
let resizeResponseGate, resizeRequestStarted = false;
const ui = await syntheticUI({ routeAPI: async ({ request, route, url, json }) => {
  if (["/api/shelf", "/api/works", "/api/series"].includes(url.pathname)) return json({ total: 1, items: [{ ...work, shelf_type: "work" }] });
  if (url.pathname === "/api/work") return json({ work, mark: null, related: { editions: [], series: [], creators: [] }, series: [] });
  if (url.pathname === "/api/pages") return json({ ...identity, readable: true,
    pages: colors.map((_, index) => ({ index, relative_path: `synthetic-strip-${index}.svg`, extension: ".svg", size_bytes: 2000 })) });
  if (url.pathname === "/api/progress" && request.method() === "GET") return json({ ...identity, progress: null });
  if (url.pathname === "/api/progress" && request.method() === "POST") {
    const body = request.postDataJSON(); saves.push(body);
    return json({ ok: true, stored: true, progress: { ...body, last_read_at: body.updated_at },
      acknowledged_client_event_id: body.client_event_id, acknowledged_updated_at: body.updated_at });
  }
  if (url.pathname === "/api/metadata-overrides") return json({ overrides: {}, provenance: {}, target_type: "work", target_id: work.candidate_id });
  if (url.pathname === "/page") {
    const index = Number(url.searchParams.get("index"));
    assert(Number.isInteger(index) && index >= 0 && index < colors.length);
    const maxWidth = Number(url.searchParams.get("max_width"));
    const max = Number(url.searchParams.get("max"));
    assert(!(maxWidth && max), "Width and longest-edge request limits must be mutually exclusive");
    const width = maxWidth ? Math.min(1200, maxWidth) : max ? Math.min(1200, Math.floor(max / 8)) : 1200;
    const height = width * 8;
    served.push({ index, width, height, maxWidth, max });
    if (resizeResponseGate && index === 0 && maxWidth > 1200) {
      resizeRequestStarted = true;
      await resizeResponseGate.promise;
    }
    return route.fulfill({ contentType: "image/svg+xml", headers: { "Cache-Control": "no-store" }, body:
      `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 1200 9600"><defs><pattern id="pixels" width="2" height="2" patternUnits="userSpaceOnUse"><rect width="1" height="2" fill="black"/><rect x="1" width="1" height="2" fill="white"/></pattern></defs><rect width="1200" height="9600" fill="${colors[index]}"/><rect x="0" y="64" width="1200" height="128" fill="url(#pixels)"/><text x="80" y="500" font-size="80" fill="white">SYNTHETIC SEGMENT ${index + 1}</text></svg>` });
  }
  return false;
} });
const { page } = ui;
await page.addInitScript(() => {
  window.__readerTrace = [];
  const sources = new Map();
  window.__readerGeometry = () => {
    const stage = document.querySelector(".reader-stage"), img = stage?.querySelector(".reader-image");
    if (!stage) return null;
    if (img && !sources.has(img.src)) sources.set(img.src, sources.size + 1);
    return { mode: document.querySelector(".reader-fit-toggle [aria-pressed=true]")?.textContent,
      width: stage.clientWidth, height: stage.clientHeight, top: stage.scrollTop,
      scrollHeight: stage.scrollHeight, loading: stage.classList.contains("is-loading"),
      complete: img?.complete, naturalWidth: img?.naturalWidth, naturalHeight: img?.naturalHeight,
      imageHeight: img?.getBoundingClientRect().height, source: img ? sources.get(img.src) : 0 };
  };
  const originalScrollTo = Element.prototype.scrollTo;
  Element.prototype.scrollTo = function (...args) {
    if (!this.classList.contains("reader-stage")) return originalScrollTo.apply(this, args);
    const before = window.__readerGeometry();
    const result = originalScrollTo.apply(this, args);
    window.__readerTrace.push({ kind: "scrollTo", at: Math.round(performance.now()), args, before, after: window.__readerGeometry() });
    if (window.__readerTrace.length > 80) window.__readerTrace.shift();
    return result;
  };
});
const expectedRGB = [[204, 68, 34], [34, 136, 68], [51, 85, 187]];
async function waitForReaderSettled(mode, viewport = page.viewportSize()) {
  // Wait for the requested mode and decoded image, then require stable geometry
  // across the resize debounce. Do NOT wait for a particular scroll position:
  // a stable but incorrect zero must still fail the independent anchor assertion.
  await page.evaluate(() => { window.__readerSettlement = null; });
  await page.waitForFunction(({ expectedMode, width, height }) => {
    const geometry = window.__readerGeometry();
    const ready = geometry && geometry.mode === expectedMode && innerWidth === width && innerHeight === height
      && !geometry.loading && geometry.complete && geometry.imageHeight > 0
      && (expectedMode === "Fit page" ? geometry.naturalWidth > 0 && geometry.naturalWidth <= 400 : geometry.naturalWidth >= 1000);
    if (!ready) { window.__readerSettlement = null; return false; }
    const signature = JSON.stringify(geometry);
    const previous = window.__readerSettlement;
    if (!previous || previous.signature !== signature) {
      window.__readerSettlement = { signature, since: performance.now() };
      return false;
    }
    return performance.now() - previous.since >= 350;
  }, { expectedMode: mode, ...viewport });
}
async function assertStrip(index) {
  await page.waitForFunction(({ expected, rgb }) => {
    const img = document.querySelector(".reader-image");
    if (!img?.complete || img.naturalWidth < 1000 || !img.alt.includes(String(expected + 1))) return false;
    const canvas = document.createElement("canvas"); canvas.width = 1; canvas.height = 1;
    const context = canvas.getContext("2d");
    context.drawImage(img, 100, 250, 1, 1, 0, 0, 1, 1);
    return [...context.getImageData(0, 0, 1, 1).data].slice(0, 3).every((value, channel) => value === rgb[channel]);
  }, { expected: index, rgb: expectedRGB[index] });
  await waitForReaderSettled("Auto");
  const geometry = await page.locator(".reader-stage").evaluate((stage) => {
    const img = stage.querySelector(".reader-image");
    const rect = img.getBoundingClientRect();
    return { classes: stage.className, auto: stage.dataset.autoLongStrip,
      stageWidth: stage.clientWidth, sourceWidth: img.naturalWidth, renderedWidth: rect.width,
      renderedHeight: rect.height, clientHeight: stage.clientHeight, scrollHeight: stage.scrollHeight,
      scrollWidth: stage.scrollWidth, dpr: devicePixelRatio };
  });
  assert.match(geometry.classes, /fit-width/u);
  assert.equal(geometry.auto, "true");
  assert(geometry.renderedWidth >= geometry.stageWidth - 32, `Segment ${index}: not width fitted`);
  assert(geometry.renderedHeight > geometry.clientHeight * 3, "Long strip was squeezed into the viewport");
  assert(geometry.sourceWidth >= Math.min(1200, geometry.renderedWidth * geometry.dpr) - 2, "Insufficient decoded horizontal pixels");
  assert(geometry.scrollWidth <= geometry.stageWidth + 2, "Mobile long strip overflows horizontally");
  const contrast = await page.locator(".reader-image").evaluate((img) => {
    const canvas = document.createElement("canvas"); canvas.width = 2; canvas.height = 1;
    const context = canvas.getContext("2d");
    context.drawImage(img, 20, 80, 2, 1, 0, 0, 2, 1);
    const pixels = context.getImageData(0, 0, 2, 1).data;
    return Math.abs(pixels[0] - pixels[4]);
  });
  assert(contrast > 240, "One-source-pixel stripes lost contrast in the decoded width-quality image");
  assert(served.some((entry) => entry.index === index && entry.maxWidth >= 1000 && !entry.max), `Segment ${index}: missing width-quality request`);
}
try {
  await page.goto(`${ui.base}/v2/?view=library`, { waitUntil: "domcontentloaded" });
  await page.locator(".book-card").filter({ hasText: work.title }).first().click();
  await page.locator(".detail-actions .primary").click();
  await assertStrip(0);
  assert(served.some((entry) => entry.index === 0 && entry.max > 0), "Initial auto-fit detection request was not exercised");
  const firstHighQuality = served.findIndex((entry) => entry.index === 0 && entry.maxWidth > 0);
  assert(firstHighQuality > served.findIndex((entry) => entry.index === 0 && entry.max > 0), "Width-quality request must follow decoded aspect detection");
  await page.locator(".reader-stage").evaluate((stage) => { stage.scrollTop = 1100; });
  const initialScroll = await page.locator(".reader-stage").evaluate((stage) => stage.scrollTop);
  await page.setViewportSize({ width: 390, height: 740 });
  await waitForReaderSettled("Auto");
  assert(Math.abs(await page.locator(".reader-stage").evaluate((stage) => stage.scrollTop) - initialScroll) < 12, "Browser chrome resize lost strip reading position");
  await page.setViewportSize({ width: 390, height: 844 });
  await waitForReaderSettled("Auto");
  await page.locator(".reader-fit-toggle").getByRole("button", { name: "Fit page", exact: true }).evaluate((element) => element.click());
  await waitForReaderSettled("Fit page");
  assert(await page.locator(".reader-stage").evaluate((stage) => {
    const rect = stage.querySelector(".reader-image").getBoundingClientRect();
    return rect.width > 0 && rect.height > 0 && rect.width <= stage.clientWidth + 1 && rect.height <= stage.clientHeight + 1;
  }), "Decoded fit-page image must still fit completely inside the stage");
  await page.locator(".reader-fit-toggle").getByRole("button", { name: "Fit width", exact: true }).evaluate((element) => element.click());
  await waitForReaderSettled("Fit width");
  await page.locator(".reader-fit-toggle").getByRole("button", { name: "Auto", exact: true }).evaluate((element) => element.click());
  await assertStrip(0);
  await page.locator(".reader-stage").evaluate((stage) => { stage.scrollTop = 1000; });
  const anchorBefore = await page.locator(".reader-stage").evaluate((stage) => stage.scrollTop / stage.scrollHeight);
  const sourceBeforeResize = await page.evaluate(() => window.__readerGeometry().source);
  resizeResponseGate = ui.gate();
  await page.setViewportSize({ width: 600, height: 844 });
  for (let attempt = 0; !resizeRequestStarted && attempt < 100; attempt++) await delay(25);
  assert(resizeRequestStarted, "Width resize must exercise the new physical-pixel request bucket");
  await delay(220); // Deliberately slow the new bucket; this is fixture latency, not settlement.
  resizeResponseGate.release(); resizeResponseGate = null;
  await page.waitForFunction((oldSource) => {
    const geometry = window.__readerGeometry();
    return geometry?.complete && geometry.source !== oldSource;
  }, sourceBeforeResize);
  await waitForReaderSettled("Auto");
  const anchorAfter = await page.locator(".reader-stage").evaluate((stage) => stage.scrollTop / stage.scrollHeight);
  assert(Math.abs(anchorBefore - anchorAfter) < 0.02, `Width resize lost source-content anchor: ${anchorBefore} -> ${anchorAfter}`);
  await page.setViewportSize({ width: 390, height: 844 });
  await assertStrip(0);
  const anchorReturned = await page.locator(".reader-stage").evaluate((stage) => stage.scrollTop / stage.scrollHeight);
  assert(Math.abs(anchorBefore - anchorReturned) < 0.02, `Returning width resize lost source-content anchor: ${anchorBefore} -> ${anchorReturned}`);
  // Keyboard navigation uses the same reader controls without touching source files.
  await page.locator(".reader-stage").focus();
  await page.keyboard.press("ArrowRight"); await assertStrip(1);
  await page.keyboard.press("ArrowRight"); await assertStrip(2);
  await page.keyboard.press("ArrowLeft"); await assertStrip(1);
  assert(!saves.some((entry) => entry.completed), "Merely opening the last long strip marked the work complete");
  assert(saves.every((entry) => entry.candidate_id === work.candidate_id), "Unexpected progress target");
  await page.keyboard.press("ArrowRight"); await assertStrip(2);
  await page.locator(".reader-stage").evaluate((stage) => { stage.scrollTop = stage.scrollHeight; });
  await page.keyboard.press("ArrowRight");
  await page.locator(".reader-ending-card").waitFor();
  for (let attempt = 0; !saves.some((entry) => entry.completed) && attempt < 100; attempt++) await delay(25);
  assert(saves.some((entry) => entry.completed && entry.index === 2), "Explicitly finishing the last strip must still save completion");
  ui.assertClean();
  process.stdout.write("Synthetic Webtoon smoke passed: mobile auto width, decoded pixel budget, ordered 3 strips, scroll restoration, no early completion.\n");
} catch (error) {
  process.stderr.write(`${JSON.stringify(await page.evaluate(() => ({ finalGeometry: window.__readerGeometry?.(), trace: window.__readerTrace }))) }\n`);
  throw error;
} finally { await ui.close(); }
