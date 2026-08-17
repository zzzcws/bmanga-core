import assert from "node:assert/strict";
import test from "node:test";

class MemoryStorage {
  values = new Map();
  getItem(key) { return this.values.get(key) ?? null; }
  setItem(key, value) { this.values.set(key, String(value)); }
  removeItem(key) { this.values.delete(key); }
  key(index) { return [...this.values.keys()][index] ?? null; }
  get length() { return this.values.size; }
}

globalThis.window = { localStorage: new MemoryStorage() };
const queue = await import("../src/lib/userMarkQueue.ts");

test("相同目标和字段只保留最新个人标记事件", () => {
  window.localStorage.values.clear();
  queue.queuePendingUserMark({ target_type: "work", target_id: "w1", personal_rating: 6, client_updated_at: "2026-07-14T01:00:00.000Z" });
  queue.queuePendingUserMark({ target_type: "work", target_id: "w1", personal_rating: null, client_updated_at: "2026-07-14T01:00:01.000Z" });
  const items = queue.pendingUserMarks();
  assert.equal(items.length, 1);
  assert.equal(items[0].payload.personal_rating, null);
});

test("不同字段不会互相覆盖，并兼容逐事件确认", () => {
  window.localStorage.values.clear();
  const rating = queue.queuePendingUserMark({ target_type: "work", target_id: "w1", personal_rating: 10, client_updated_at: "2026-07-14T01:00:00.000Z" });
  queue.queuePendingUserMark({ target_type: "work", target_id: "w1", reread_priority: 3, client_updated_at: "2026-07-14T01:00:01.000Z" });
  queue.acknowledgePendingUserMark(rating);
  const items = queue.pendingUserMarks();
  assert.equal(items.length, 1);
  assert.equal(items[0].payload.reread_priority, 3);
});

test("可以确认某个具体客户端事件是否已可靠暂存", () => {
  window.localStorage.values.clear();
  const payload = queue.queuePendingUserMark({ target_type: "work", target_id: "w1", personal_rating: 0, client_updated_at: "2026-07-14T01:00:00.000Z" });
  assert.equal(queue.hasPendingUserMark(payload), true);
  assert.equal(queue.hasPendingUserMark({ target_type: "work", target_id: "w1", personal_rating: 0 }), true);
  assert.equal(queue.hasPendingUserMark({ target_type: "work", target_id: "w1", personal_rating: 1 }), false);
  assert.equal(queue.hasPendingUserMark({ ...payload, client_updated_at: "2026-07-14T01:00:01.000Z" }), false);
  queue.acknowledgePendingUserMark(payload);
  assert.equal(queue.hasPendingUserMark(payload), false);
});

test("存储写入失败时不会误报为已经暂存", () => {
  const original = window.localStorage;
  window.localStorage = {
    getItem() { return null; },
    setItem() { throw new Error("quota exceeded"); },
  };
  const payload = queue.queuePendingUserMark({ target_type: "work", target_id: "w2", personal_rating: 8, client_updated_at: "2026-07-14T01:00:00.000Z" });
  assert.equal(queue.hasPendingUserMark(payload), false);
  window.localStorage = original;
});

test("重试保留失败事件，同时不覆盖重试期间产生的新事件", async () => {
  window.localStorage.values.clear();
  queue.queuePendingUserMark({ target_type: "work", target_id: "w1", personal_rating: 7, client_updated_at: "2026-07-14T01:00:00.000Z" });
  const result = await queue.flushPendingUserMarks(async (payload) => {
    queue.queuePendingUserMark({ ...payload, personal_rating: 9, client_updated_at: "2026-07-14T01:00:02.000Z" });
    throw new Error("offline");
  });
  assert.equal(result.sent.length, 0);
  assert.equal(result.remaining, 1);
  assert.equal(queue.pendingUserMarks()[0].payload.personal_rating, 9);
});

test("非 Error 失败原因使用当前界面语言的安全回退文案", async () => {
  window.localStorage.values.clear();
  queue.queuePendingUserMark({ target_type: "work", target_id: "en", personal_rating: 4 });
  await queue.flushPendingUserMarks(async () => { throw "offline"; }, "en");
  assert.equal(queue.pendingUserMarks()[0].last_error, "Save failed");

  window.localStorage.values.clear();
  queue.queuePendingUserMark({ target_type: "work", target_id: "ja", personal_rating: 4 });
  await queue.flushPendingUserMarks(async () => { throw "offline"; }, "ja");
  assert.equal(queue.pendingUserMarks()[0].last_error, "保存に失敗しました");
});
