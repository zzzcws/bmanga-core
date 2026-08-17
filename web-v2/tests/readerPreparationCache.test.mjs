import assert from "node:assert/strict";
import test from "node:test";

import {
  ReaderPreparationCache,
  safeReaderWarmIndex,
  waitForReaderPreparation,
} from "../src/lib/readerPreparationCache.ts";

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
}

test("详情预取与正式打开共享同一个书页清单请求", async () => {
  const pending = deferred();
  let loads = 0;
  const cache = new ReaderPreparationCache(async () => {
    loads += 1;
    return pending.promise;
  }, { timeoutMs: 0 });

  const detailWarm = cache.load("work-1");
  const readerOpen = cache.load("work-1");
  assert.equal(readerOpen, detailWarm);
  pending.resolve({ count: 12 });
  assert.deepEqual(await readerOpen, { count: 12 });
  assert.equal(loads, 1);
});

test("短 TTL 到期后重新读取书页清单", async () => {
  let now = 1_000;
  let loads = 0;
  const cache = new ReaderPreparationCache(async () => ({ revision: ++loads }), {
    now: () => now,
    timeoutMs: 0,
    ttlMs: 100,
  });

  assert.deepEqual(await cache.load("work-1"), { revision: 1 });
  now += 99;
  assert.deepEqual(await cache.load("work-1"), { revision: 1 });
  now += 2;
  assert.deepEqual(await cache.load("work-1"), { revision: 2 });
});

test("详情预取失败不会污染缓存，点击阅读仍可重试", async () => {
  let loads = 0;
  const cache = new ReaderPreparationCache(async () => {
    loads += 1;
    if (loads === 1) throw new Error("temporary");
    return { ok: true };
  }, { timeoutMs: 0 });

  await assert.rejects(cache.load("work-1"), /temporary/u);
  assert.equal(cache.has("work-1"), false);
  assert.deepEqual(await cache.load("work-1"), { ok: true });
  assert.equal(loads, 2);
});

test("切换详情时只保留当前作品并取消旧预取", async () => {
  const requests = new Map();
  const cache = new ReaderPreparationCache((key, signal) => {
    const pending = deferred();
    requests.set(key, { pending, signal });
    signal.addEventListener("abort", () => pending.reject(new DOMException("cancelled", "AbortError")), { once: true });
    return pending.promise;
  }, { maxEntries: 1, timeoutMs: 0 });

  const oldRequest = cache.load("work-old");
  await Promise.resolve();
  const currentRequest = cache.load("work-current");
  await assert.rejects(oldRequest, { name: "AbortError" });
  assert.equal(requests.get("work-old").signal.aborted, true);
  assert.equal(cache.has("work-old"), false);
  assert.equal(cache.has("work-current"), true);
  requests.get("work-current").pending.resolve({ id: "work-current" });
  assert.deepEqual(await currentRequest, { id: "work-current" });
  cache.clear();
});

test("取消一个正式等待者不会中断共享的详情预取", async () => {
  const pending = deferred();
  let aborted = false;
  const cache = new ReaderPreparationCache((_key, signal) => {
    signal.addEventListener("abort", () => { aborted = true; }, { once: true });
    return pending.promise;
  }, { timeoutMs: 0 });
  const shared = cache.load("work-1");
  const controller = new AbortController();
  const formalWait = waitForReaderPreparation(shared, controller.signal);
  controller.abort();

  await assert.rejects(formalWait, { name: "AbortError" });
  assert.equal(aborted, false);
  pending.resolve({ ok: true });
  assert.deepEqual(await shared, { ok: true });
});

test("只在恢复页属于当前 manifest 时预热该页", () => {
  const pages = { readable: true, count: 14, page_manifest_id: "manifest-2", manifest_hash: "hash-2" };
  assert.equal(safeReaderWarmIndex(pages, null), 0);
  assert.equal(safeReaderWarmIndex(pages, { index: 7, page_manifest_id: "manifest-2" }), 7);
  assert.equal(safeReaderWarmIndex(pages, { index: 7, page_manifest_id: "manifest-1" }), null);
  assert.equal(safeReaderWarmIndex(pages, { index: 99, manifest_hash: "hash-2" }), 13);
  assert.equal(safeReaderWarmIndex(pages, { index: 7 }), null);
  assert.equal(safeReaderWarmIndex(pages, { index: 7, page_manifest_id: "manifest-1" }, 0), 0);
  assert.equal(safeReaderWarmIndex({ ...pages, readable: false }, null), null);
});
