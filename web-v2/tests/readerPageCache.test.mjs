import assert from "node:assert/strict";
import test from "node:test";

import { ReaderPageCache, ReaderPageCacheTimeoutError } from "../src/lib/readerPageCache.ts";

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
}

test("同一书页的预取与正式显示共享一个进行中的请求", async () => {
  const pending = deferred();
  let loads = 0;
  const cache = new ReaderPageCache(async () => {
    loads += 1;
    return pending.promise;
  }, { timeoutMs: 0 });

  const prefetched = cache.load("page-2");
  const displayed = cache.load("page-2");
  assert.equal(prefetched, displayed);
  pending.resolve({ url: "blob:page-2" });
  assert.deepEqual(await displayed, { url: "blob:page-2" });
  assert.equal(loads, 1);
});

test("LRU 淘汰会保留当前显示页并释放最旧的相邻页", async () => {
  const disposed = [];
  const cache = new ReaderPageCache(async (key) => ({ key }), {
    dispose: (value) => disposed.push(value.key),
    maxEntries: 2,
    timeoutMs: 0,
  });

  await cache.load("current");
  cache.setPinnedKey("current");
  await cache.load("previous");
  await cache.load("next");

  assert.equal(cache.size, 2);
  assert.equal(cache.has("current"), true);
  assert.equal(cache.has("previous"), false);
  assert.equal(cache.has("next"), true);
  assert.deepEqual(disposed, ["previous"]);
});

test("失败的预取不会污染缓存，正式显示可以重新请求", async () => {
  let loads = 0;
  const cache = new ReaderPageCache(async () => {
    loads += 1;
    if (loads === 1) throw new Error("temporary");
    return { ok: true };
  }, { timeoutMs: 0 });

  await assert.rejects(cache.load("page"), /temporary/u);
  assert.equal(cache.has("page"), false);
  assert.deepEqual(await cache.load("page"), { ok: true });
  assert.equal(loads, 2);
});

test("清空阅读会取消进行中请求并释放所有已解码页面", async () => {
  const pending = deferred();
  const disposed = [];
  let aborted = false;
  const cache = new ReaderPageCache(async (key, signal) => {
    if (key === "ready") return { key };
    signal.addEventListener("abort", () => {
      aborted = true;
      pending.reject(new DOMException("cancelled", "AbortError"));
    }, { once: true });
    return pending.promise;
  }, {
    dispose: (value) => disposed.push(value.key),
    timeoutMs: 0,
  });

  await cache.load("ready");
  const loading = cache.load("loading");
  await Promise.resolve();
  cache.clear();

  await assert.rejects(loading, { name: "AbortError" });
  assert.equal(aborted, true);
  assert.deepEqual(disposed, ["ready"]);
  assert.equal(cache.size, 0);
});

test("超时请求使用独立错误类型并允许后续重试", async () => {
  let loads = 0;
  const cache = new ReaderPageCache((key, signal) => new Promise((resolve, reject) => {
    loads += 1;
    signal.addEventListener("abort", () => reject(new DOMException("timeout", "AbortError")), { once: true });
    if (loads > 1) resolve({ key });
  }), { timeoutMs: 10 });

  await assert.rejects(cache.load("slow"), ReaderPageCacheTimeoutError);
  assert.equal(cache.has("slow"), false);
  assert.deepEqual(await cache.load("slow"), { key: "slow" });
  assert.equal(loads, 2);
});

test("即使 loader 忽略取消，超时也会主动结算并只释放一次迟到结果", async () => {
  const pending = deferred();
  const disposed = [];
  const cache = new ReaderPageCache(() => pending.promise, {
    dispose: (value) => disposed.push(value.key),
    timeoutMs: 10,
  });

  await assert.rejects(cache.load("ignored-abort"), ReaderPageCacheTimeoutError);
  pending.resolve({ key: "late" });
  await Promise.resolve();
  await Promise.resolve();

  assert.deepEqual(disposed, ["late"]);
  assert.equal(cache.has("ignored-abort"), false);
});
