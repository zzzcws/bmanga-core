import assert from "node:assert/strict";
import test from "node:test";

import {
  LIBRARY_PAGE_STATE_CACHE_KEY,
  LIBRARY_PAGE_STATE_PENDING_KEY,
  acknowledgePendingLibraryPageMutation,
  buildLibraryPageMutation,
  clearCachedLibraryPageState,
  clearPendingLibraryPageMutation,
  compareLibraryPageEvents,
  enqueuePendingLibraryPageMutation,
  explicitLibraryPageParameters,
  hasExplicitLibraryPageParameters,
  latestLibraryPageStateEvent,
  libraryPageScopesFromState,
  nextLibraryPageEventID,
  nextLibraryPageTimestamp,
  parseLibraryPageMutation,
  parseLibraryPageState,
  pendingLibraryPageMutations,
  readCachedLibraryPageState,
  readPendingLibraryPageMutation,
  reconcileLibraryPageStates,
  rebaseLibraryPageMutation,
  writeCachedLibraryPageState,
  writePendingLibraryPageMutation,
} from "../src/lib/libraryPageState.ts";
import { getLibraryPageState, saveLibraryPageState, saveLibraryPageStates } from "../src/lib/api.ts";

class MemoryStorage {
  values = new Map();

  get length() {
    return this.values.size;
  }

  clear() {
    this.values.clear();
  }

  getItem(key) {
    return this.values.has(key) ? this.values.get(key) : null;
  }

  key(index) {
    return [...this.values.keys()][index] ?? null;
  }

  removeItem(key) {
    this.values.delete(key);
  }

  setItem(key, value) {
    this.values.set(key, String(value));
  }
}

function event(second, eventID, overrides = {}) {
  return {
    updated_at: `2026-08-08T08:00:${String(second).padStart(2, "0")}.000Z`,
    event_id: eventID,
    ...overrides,
  };
}

function canonical(overrides = {}) {
  return {
    version: 1,
    sort: "added_desc",
    sort_updated_at: "2026-08-08T08:00:01.000Z",
    sort_event_id: "sort-1",
    positions: {
      all: { offset: 19, updated_at: "2026-08-08T08:00:02.000Z", event_id: "all-2" },
      doujin: { offset: 90, updated_at: "2026-08-08T08:00:03.000Z", event_id: "doujin-3" },
      series: { offset: 999_999_999, updated_at: "2026-08-08T08:00:04.000Z", event_id: "series-4" },
    },
    updated_at: "2026-08-08T08:00:04.000Z",
    ...overrides,
  };
}

function mutation(sort, mode, offset, second, eventID, initialOffsets = undefined) {
  return {
    sort,
    mode,
    offset,
    ...event(second, eventID),
    ...(initialOffsets === undefined ? {} : { initial_offsets: initialOffsets }),
  };
}

test("canonical state 安全解析、规范化 offset 并转换为 LibraryPageScopes", () => {
  const state = parseLibraryPageState(canonical());
  assert(state);
  assert.equal(state.positions.all.offset, 18);
  assert.equal(state.positions.series.offset, 999_990);
  assert.deepEqual(libraryPageScopesFromState(state), {
    sort: "added_desc",
    offsets: { all: 18, doujin: 90, series: 999_990 },
  });

  assert.equal(parseLibraryPageState({ ...canonical(), version: 2 }), null);
  assert.equal(parseLibraryPageState({ ...canonical(), sort: "unsafe" }), null);
  assert.equal(parseLibraryPageState({ ...canonical(), sort_event_id: "" }), null);
  assert.equal(parseLibraryPageState({
    ...canonical(),
    positions: { ...canonical().positions, doujin: { offset: 0, updated_at: "bad", event_id: "x" } },
  }), null);
  assert.equal(parseLibraryPageState({ ...canonical(), sort_event_id: "界".repeat(101) }), null);
});

test("canonical state 拒绝前后端排序规则不一致的 Unicode event_id", () => {
  assert.equal(parseLibraryPageState({
    version: 1,
    sort: "added_desc",
    sort_updated_at: "2026-08-08T00:00:00.000Z",
    sort_event_id: "事件",
    positions: {
      all: { offset: 0, updated_at: "2026-08-08T00:00:00.000Z", event_id: "事件" },
      doujin: { offset: 0, updated_at: "2026-08-08T00:00:00.000Z", event_id: "事件" },
      series: { offset: 0, updated_at: "2026-08-08T00:00:00.000Z", event_id: "事件" },
    },
    updated_at: "2026-08-08T00:00:00.000Z",
  }), null);
});

test("mutation builder 对齐当前页并携带三类 initial_offsets 与唯一 event_id", () => {
  const scopes = { sort: "added_desc", offsets: { all: 36, doujin: 90, series: 18 } };
  const fixedTime = "2026-08-08T08:01:00.000Z";
  const built = buildLibraryPageMutation(scopes, "doujin", {
    offset: 91,
    updatedAt: fixedTime,
    eventID: "fixed-event",
  });
  assert.deepEqual(built, {
    sort: "added_desc",
    mode: "doujin",
    offset: 90,
    updated_at: fixedTime,
    event_id: "fixed-event",
    initial_offsets: { all: 36, doujin: 90, series: 18 },
  });
  assert.deepEqual(parseLibraryPageMutation(built), built);

  const sameMillisecondA = buildLibraryPageMutation(scopes, "all", { updatedAt: fixedTime });
  const sameMillisecondB = buildLibraryPageMutation(scopes, "all", { updatedAt: fixedTime });
  assert.equal(sameMillisecondA.updated_at, sameMillisecondB.updated_at);
  assert.notEqual(sameMillisecondA.event_id, sameMillisecondB.event_id);
  assert.notEqual(compareLibraryPageEvents(sameMillisecondA, sameMillisecondB), 0);
  assert.throws(() => buildLibraryPageMutation(scopes, "bad", {}), /unsupported library page mode/u);
});

test("event_id 在 crypto.randomUUID 不可用时仍保持同毫秒唯一", () => {
  const cryptoDescriptor = Object.getOwnPropertyDescriptor(globalThis, "crypto");
  const originalNow = Date.now;
  try {
    Object.defineProperty(globalThis, "crypto", { configurable: true, value: undefined });
    Date.now = () => 1_775_555_555_555;
    const first = nextLibraryPageEventID();
    const second = nextLibraryPageEventID();
    assert.match(first, /^library-page-/u);
    assert.notEqual(first, second);
  } finally {
    Date.now = originalNow;
    if (cryptoDescriptor) Object.defineProperty(globalThis, "crypto", cryptoDescriptor);
    else Reflect.deleteProperty(globalThis, "crypto");
  }
});

test("nextLibraryPageTimestamp 严格晚于 canonical 的全部时钟", () => {
  const future = new Date(Date.now() + 2_000).toISOString();
  const state = canonical({
    updated_at: future,
    positions: {
      ...canonical().positions,
      series: { ...canonical().positions.series, updated_at: future },
    },
  });
  const next = nextLibraryPageTimestamp(parseLibraryPageState(state));
  assert(Date.parse(next) > Date.parse(future));
  assert(Date.parse(nextLibraryPageTimestamp(next)) > Date.parse(next));
});

test("future timestamp 依据服务端时钟重建，后续事件不再沿用快钟", () => {
  const originalNow = Date.now;
  const serverNow = originalNow();
  try {
    Date.now = () => serverNow + 10 * 60_000;
    const future = mutation("added_desc", "all", 18, 1, "future-event");
    future.updated_at = new Date(Date.now()).toISOString();
    const rebased = rebaseLibraryPageMutation(future, new Date(serverNow).toISOString());
    assert(rebased);
    assert(Date.parse(rebased.updated_at) <= serverNow + 5 * 60_000);
    assert.notEqual(rebased.event_id, future.event_id);
    assert(Date.parse(nextLibraryPageTimestamp(rebased)) < Date.now());
  } finally {
    Date.now = originalNow;
    rebaseLibraryPageMutation(mutation("added_desc", "all", 0, 1, "clock-reset"), new Date(Date.now()).toISOString());
  }
});

test("canonical freshness 在同一时间以全部 scope 的最大 event_id 判定", () => {
  const tiedTime = "2026-08-08T08:00:09.000Z";
  const state = canonical({
    sort_updated_at: tiedTime,
    sort_event_id: "event-a",
    positions: {
      all: { ...canonical().positions.all, updated_at: tiedTime, event_id: "event-b" },
      doujin: { ...canonical().positions.doujin, updated_at: tiedTime, event_id: "event-z" },
      series: { ...canonical().positions.series, updated_at: tiedTime, event_id: "event-c" },
    },
    updated_at: tiedTime,
  });
  assert.deepEqual(latestLibraryPageStateEvent(state), {
    updated_at: tiedTime,
    event_id: "event-z",
  });
});

test("相同 sort epoch 的延迟快照按 mode 合并，不回滚已更新页码", () => {
  const current = canonical({
    positions: {
      all: { offset: 180, updated_at: "2026-08-08T08:00:30.000Z", event_id: "all-30" },
      doujin: { offset: 18, updated_at: "2026-08-08T08:00:10.000Z", event_id: "doujin-10" },
      series: { offset: 72, updated_at: "2026-08-08T08:00:20.000Z", event_id: "series-20" },
    },
    updated_at: "2026-08-08T08:00:30.000Z",
  });
  const delayed = canonical({
    positions: {
      all: { offset: 180, updated_at: "2026-08-08T08:00:30.000Z", event_id: "all-30" },
      doujin: { offset: 54, updated_at: "2026-08-08T08:00:25.000Z", event_id: "doujin-25" },
      series: { offset: 0, updated_at: "2026-08-08T08:00:09.000Z", event_id: "series-09" },
    },
    updated_at: "2026-08-08T08:00:30.000Z",
  });
  const merged = reconcileLibraryPageStates(delayed, current);
  assert(merged);
  assert.equal(merged.positions.doujin.offset, 54);
  assert.equal(merged.positions.series.offset, 72);
  assert.equal(merged.updated_at, "2026-08-08T08:00:30.000Z");
});

test("显式 URL 参数分别辨识 offset 与 sort", () => {
  assert.deepEqual(explicitLibraryPageParameters("/v2/library?offset=18"), { offset: true, sort: false });
  assert.deepEqual(explicitLibraryPageParameters("/v2/library?sort=added_desc"), { offset: false, sort: true });
  assert.deepEqual(explicitLibraryPageParameters("/v2/library?offset=&sort="), { offset: true, sort: true });
  assert.equal(hasExplicitLibraryPageParameters("/v2/library?kind=doujin"), false);
  assert.equal(hasExplicitLibraryPageParameters("http://[invalid"), false);
});

test("canonical cache 在损坏或不可用 localStorage 下安全回退", () => {
  const storage = new MemoryStorage();
  assert.deepEqual(writeCachedLibraryPageState(canonical(), storage), parseLibraryPageState(canonical()));
  assert.deepEqual(readCachedLibraryPageState(storage), parseLibraryPageState(canonical()));
  clearCachedLibraryPageState(storage);
  assert.equal(readCachedLibraryPageState(storage), null);

  storage.setItem(LIBRARY_PAGE_STATE_CACHE_KEY, "not-json");
  assert.equal(readCachedLibraryPageState(storage), null);
  const brokenStorage = {
    getItem() { throw new Error("blocked"); },
    setItem() { throw new Error("blocked"); },
    removeItem() { throw new Error("blocked"); },
  };
  assert.equal(readCachedLibraryPageState(brokenStorage), null);
  assert.doesNotThrow(() => writeCachedLibraryPageState(canonical(), brokenStorage));
  assert.doesNotThrow(() => clearCachedLibraryPageState(brokenStorage));
});

test("pending 队列按 sort epoch 压缩并保留 A→B→A 的重置语义", () => {
  const storage = new MemoryStorage();
  const all1 = mutation("added_desc", "all", 18, 1, "a");
  const doujin2 = mutation("added_desc", "doujin", 36, 2, "b");
  const all3 = mutation("added_desc", "all", 54, 3, "c");
  enqueuePendingLibraryPageMutation(all1, storage);
  enqueuePendingLibraryPageMutation(doujin2, storage);
  enqueuePendingLibraryPageMutation(all3, storage);
  assert.deepEqual(pendingLibraryPageMutations(storage).map((item) => [item.mode, item.offset]), [
    ["doujin", 36],
    ["all", 54],
  ]);

  const newSort4 = mutation("title_asc", "doujin", 0, 4, "d");
  const series5 = mutation("title_asc", "series", 72, 5, "e");
  enqueuePendingLibraryPageMutation(newSort4, storage);
  assert.deepEqual(pendingLibraryPageMutations(storage).map((item) => item.event_id), ["b", "c", "d"]);
  enqueuePendingLibraryPageMutation(series5, storage);
  assert.deepEqual(pendingLibraryPageMutations(storage).map((item) => item.event_id), ["b", "c", "d", "e"]);

  enqueuePendingLibraryPageMutation(mutation("added_desc", "all", 0, 1, "old"), storage);
  assert.deepEqual(pendingLibraryPageMutations(storage).map((item) => item.event_id), ["b", "c", "d", "e"]);
  enqueuePendingLibraryPageMutation(mutation("added_desc", "all", 90, 6, "f"), storage);
  assert.deepEqual(pendingLibraryPageMutations(storage).map((item) => item.event_id), ["d", "e", "f"]);
  assert.equal(readPendingLibraryPageMutation(storage)?.event_id, "f");
  assert.match(storage.getItem(LIBRARY_PAGE_STATE_PENDING_KEY) || "", /"items"/u);
});

test("pending ACK 仅移除匹配 event_id，并兼容旧单项格式", () => {
  const storage = new MemoryStorage();
  const all = mutation("added_desc", "all", 18, 1, "all-event");
  const doujin = mutation("added_desc", "doujin", 36, 2, "doujin-event");
  writePendingLibraryPageMutation(all, storage);
  writePendingLibraryPageMutation(doujin, storage);
  acknowledgePendingLibraryPageMutation("all-event", storage);
  assert.deepEqual(pendingLibraryPageMutations(storage).map((item) => item.event_id), ["doujin-event"]);
  clearPendingLibraryPageMutation("not-current", storage);
  assert.equal(readPendingLibraryPageMutation(storage)?.event_id, "doujin-event");
  clearPendingLibraryPageMutation("doujin-event", storage);
  assert.deepEqual(pendingLibraryPageMutations(storage), []);

  storage.setItem(LIBRARY_PAGE_STATE_PENDING_KEY, JSON.stringify(all));
  assert.equal(readPendingLibraryPageMutation(storage)?.event_id, "all-event");
  clearPendingLibraryPageMutation(undefined, storage);
  assert.equal(storage.getItem(LIBRARY_PAGE_STATE_PENDING_KEY), null);
});

test("API helpers 使用同源 GET 与带 state 包装的 POST", async () => {
  const originalFetch = globalThis.fetch;
  const calls = [];
  globalThis.fetch = async (url, options = {}) => {
    calls.push({ url: String(url), options });
    const body = options.method === "POST"
      ? { ok: true, stored: true, state: canonical(), updated_at: canonical().updated_at }
      : { state: canonical(), updated_at: canonical().updated_at };
    return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
  };
  try {
    const loaded = await getLibraryPageState();
    assert.equal(loaded.state?.version, 1);
    const payload = mutation("added_desc", "all", 18, 1, "api-event", { all: 18, doujin: 0, series: 0 });
    const saved = await saveLibraryPageState(payload);
    assert.equal(saved.stored, true);
    const batchSaved = await saveLibraryPageStates([payload]);
    assert.equal(batchSaved.stored, true);
    assert.equal(calls[0].url, "/api/library-page-state");
    assert.equal(calls[0].options.method, "GET");
    assert.equal(calls[1].url, "/api/library-page-state");
    assert.equal(calls[1].options.method, "POST");
    assert.deepEqual(JSON.parse(calls[1].options.body), { state: payload });
    assert.equal(new Headers(calls[1].options.headers).get("X-Bmanga-Write"), "same-origin");
    assert.equal(calls[2].url, "/api/library-page-state");
    assert.equal(calls[2].options.method, "POST");
    assert.deepEqual(JSON.parse(calls[2].options.body), { states: [payload] });
  } finally {
    globalThis.fetch = originalFetch;
  }
});
