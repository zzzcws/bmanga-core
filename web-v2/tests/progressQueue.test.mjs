import assert from "node:assert/strict";
import test from "node:test";

const LEGACY_STORAGE_KEY = "bmanga.v2.progressPending.v2";
const ENTRY_STORAGE_PREFIX = "bmanga.v2.progressPending.v3.entry.";
const queueModuleURL = new URL("../src/lib/progressQueue.ts", import.meta.url);

class MemoryStorage {
  constructor(values = new Map()) {
    this.values = values;
    this.resetCalls();
  }

  resetCalls() {
    this.calls = {
      length: 0,
      clear: 0,
      getItem: 0,
      key: 0,
      removeItem: 0,
      setItem: 0,
    };
  }

  get length() {
    this.calls.length += 1;
    return this.values.size;
  }

  clear() {
    this.calls.clear += 1;
    this.values.clear();
  }

  getItem(key) {
    this.calls.getItem += 1;
    return this.values.has(String(key)) ? this.values.get(String(key)) : null;
  }

  key(index) {
    this.calls.key += 1;
    return [...this.values.keys()][index] ?? null;
  }

  removeItem(key) {
    this.calls.removeItem += 1;
    this.values.delete(String(key));
  }

  setItem(key, value) {
    this.calls.setItem += 1;
    this.values.set(String(key), String(value));
  }
}

class SnapshotStorage extends MemoryStorage {
  constructor(values) {
    super(values);
    this.visibleKeys = [...values.keys()];
  }

  get length() {
    this.calls.length += 1;
    return this.visibleKeys.length;
  }

  key(index) {
    this.calls.key += 1;
    return this.visibleKeys[index] ?? null;
  }
}

const sharedValues = new Map();
const liveStorage = new MemoryStorage(sharedValues);
globalThis.window = { localStorage: liveStorage };

let moduleVersion = 0;
async function freshQueueModule() {
  moduleVersion += 1;
  return import(`${queueModuleURL.href}?test=${moduleVersion}`);
}

function useStorage(localStorage) {
  globalThis.window = { localStorage };
}

function rawEntryCount() {
  return [...sharedValues.keys()].filter((key) => key.startsWith(ENTRY_STORAGE_PREFIX)).length;
}

function payload(candidateID, manifestID, index = 0) {
  return {
    candidate_id: candidateID,
    page_manifest_id: manifestID,
    manifest_hash: `hash-${manifestID}`,
    index,
    count: 12,
    completed: false,
  };
}

test.beforeEach(() => {
  sharedValues.clear();
  liveStorage.resetCalls();
  useStorage(liveStorage);
});

test("同 candidate 与 manifest 的严格旧物理记录会压缩，逻辑读取返回最新写", async () => {
  const queue = await freshQueueModule();

  queue.enqueuePendingProgress(payload("candidate-a", "manifest-a", 1), "work-a");
  const latest = queue.enqueuePendingProgress(payload("candidate-a", "manifest-a", 7));

  assert.equal(latest.logicalPendingCount, 1);
  assert.equal(rawEntryCount(), 1);
  const entries = queue.pendingProgressEntries();
  assert.equal(entries.length, 1);
  assert.equal(entries[0].entryID, latest.entryID);
  assert.equal(entries[0].payload.index, 7);

  assert.equal(queue.acknowledgePendingProgress(latest.entryID), 0);
  assert.equal(rawEntryCount(), 0);
});

test("同 candidate 的不同 manifest 相互隔离", async () => {
  const queue = await freshQueueModule();

  queue.enqueuePendingProgress(payload("candidate-a", "manifest-old", 3), "work-a");
  const latest = queue.enqueuePendingProgress(payload("candidate-a", "manifest-new", 4), "work-a");

  assert.equal(latest.logicalPendingCount, 2);
  const entries = queue.pendingProgressEntries();
  assert.equal(entries.length, 2);
  assert.deepEqual(entries.map((entry) => entry.payload.page_manifest_id), ["manifest-old", "manifest-new"]);
});

test("按 entryID 确认旧写不会误删同 semantic 的较新进度", async () => {
  const queue = await freshQueueModule();

  const oldEntry = queue.enqueuePendingProgress(payload("candidate-a", "manifest-a", 2), "work-a");
  const newEntry = queue.enqueuePendingProgress(payload("candidate-a", "manifest-a", 8), "work-a");

  assert.equal(queue.acknowledgePendingProgress(oldEntry.entryID), 1);

  assert.equal(rawEntryCount(), 1);
  assert.deepEqual(queue.pendingProgressEntries().map((entry) => entry.entryID), [newEntry.entryID]);
});

test("两个独立模块上下文交错写不同作品不会互相覆盖", async () => {
  const originalNow = Date.now;
  Date.now = () => 1_700_000_000_000;
  try {
    const firstTab = await freshQueueModule();
    const secondTab = await freshQueueModule();
    const firstWork = firstTab.enqueuePendingProgress(payload("candidate-a", "manifest-a", 2), "work-a");
    const secondWork = secondTab.enqueuePendingProgress(payload("candidate-b", "manifest-b", 6), "work-b");
    const firstWorkNewer = firstTab.enqueuePendingProgress(payload("candidate-a", "manifest-a", 9), "work-a");

    assert.equal(rawEntryCount(), 2);
    assert.deepEqual(
      firstTab.pendingProgressEntries().map((entry) => [entry.payload.candidate_id, entry.payload.index]),
      [["candidate-b", 6], ["candidate-a", 9]],
    );

    secondTab.acknowledgePendingProgress(secondWork.entryID);
    firstTab.acknowledgePendingProgress(firstWork.entryID);
    assert.deepEqual(firstTab.pendingProgressEntries().map((entry) => entry.entryID), [firstWorkNewer.entryID]);
  } finally {
    Date.now = originalNow;
  }
});

test("同毫秒且同逻辑时钟的并发 semantic 写，旧 ACK 只删精确 entryID", async () => {
  const originalNow = Date.now;
  Date.now = () => 1_700_000_000_000;
  try {
    const firstTab = await freshQueueModule();
    const secondTab = await freshQueueModule();
    const firstSnapshot = new SnapshotStorage(sharedValues);
    const secondSnapshot = new SnapshotStorage(sharedValues);

    useStorage(firstSnapshot);
    const firstEntry = firstTab.enqueuePendingProgress(payload("candidate-a", "manifest-a", 2), "work-a");
    useStorage(secondSnapshot);
    const concurrentEntry = secondTab.enqueuePendingProgress(payload("candidate-a", "manifest-a", 9), "work-a");

    useStorage(liveStorage);
    assert.equal(firstEntry.sequence, concurrentEntry.sequence);
    assert.equal(firstEntry.queuedAt, concurrentEntry.queuedAt);
    assert.equal(rawEntryCount(), 2);

    firstTab.acknowledgePendingProgress(firstEntry.entryID);
    assert.equal(rawEntryCount(), 1);
    assert.deepEqual(firstTab.pendingProgressEntries().map((entry) => entry.entryID), [concurrentEntry.entryID]);
  } finally {
    Date.now = originalNow;
  }
});

test("旧 v2 数组首次读取时迁移为独立记录且可正常确认", async () => {
  const queue = await freshQueueModule();
  liveStorage.setItem(LEGACY_STORAGE_KEY, JSON.stringify([{
    key: "work-a\u0000manifest-a",
    sequence: 1_700_000_000_000_001,
    workIdentityID: "work-a",
    queuedAt: "2023-11-14T22:13:20.000Z",
    payload: payload("candidate-a", "manifest-a", 4),
  }]));

  const entries = queue.pendingProgressEntries();
  assert.equal(entries.length, 1);
  assert.match(entries[0].entryID, /^legacy-/);
  assert.equal(liveStorage.getItem(LEGACY_STORAGE_KEY), null);
  assert.equal(rawEntryCount(), 1);

  queue.acknowledgePendingProgress(entries[0].entryID);
  assert.equal(rawEntryCount(), 0);
});

test("时间戳在同一毫秒内及模块重载后仍严格递增", async () => {
  const originalNow = Date.now;
  Date.now = () => 1_700_000_000_000;
  try {
    const firstQueue = await freshQueueModule();
    const firstEntry = firstQueue.enqueuePendingProgress(payload("candidate-a", "manifest-a", 2), "work-a");
    const reloadedQueue = await freshQueueModule();
    const values = [
      Date.parse(firstEntry.queuedAt),
      Date.parse(reloadedQueue.nextProgressTimestamp()),
      Date.parse(reloadedQueue.nextProgressTimestamp()),
    ];
    assert.ok(values[1] > values[0]);
    assert.ok(values[2] > values[1]);
  } finally {
    Date.now = originalNow;
  }
});

test("损坏的 legacy 与独立记录不会遮蔽健康队列，下一次入队可恢复", async () => {
  const queue = await freshQueueModule();
  liveStorage.setItem(LEGACY_STORAGE_KEY, "{not-json");
  liveStorage.setItem(`${ENTRY_STORAGE_PREFIX}damaged`, "{not-json");

  assert.deepEqual(queue.pendingProgressEntries(), []);
  const mutation = queue.enqueuePendingProgress(payload("candidate-a", "manifest-a", 1));
  assert.equal(mutation.logicalPendingCount, 1);
  assert.equal(queue.pendingProgressCount(), 1);
  assert.equal(liveStorage.getItem(LEGACY_STORAGE_KEY), null);
  assert.equal(rawEntryCount(), 1);
});

test("物理队列只保留最新 80 项", async () => {
  const queue = await freshQueueModule();

  let latest;
  for (let index = 0; index < 85; index += 1) {
    latest = queue.enqueuePendingProgress(payload(`candidate-${index}`, `manifest-${index}`, index));
  }

  assert.equal(latest.logicalPendingCount, 80);
  const entries = queue.pendingProgressEntries();
  assert.equal(rawEntryCount(), 80);
  assert.equal(entries.length, 80);
  assert.equal(entries[0].payload.candidate_id, "candidate-5");
  assert.equal(entries[79].payload.candidate_id, "candidate-84");
});

test("单部作品高频保存不会挤掉另一部作品的唯一待同步进度", async () => {
  const queue = await freshQueueModule();

  queue.enqueuePendingProgress(payload("candidate-b", "manifest-b", 3));
  for (let index = 0; index < 100; index += 1) {
    queue.enqueuePendingProgress(payload("candidate-a", "manifest-a", index));
  }

  const entries = queue.pendingProgressEntries();
  assert.deepEqual(entries.map((entry) => entry.payload.candidate_id).sort(), ["candidate-a", "candidate-b"]);
  assert.equal(entries.find((entry) => entry.payload.candidate_id === "candidate-a")?.payload.index, 99);
  assert.ok(rawEntryCount() <= 3, `raw queue retained ${rawEntryCount()} superseded records`);
});

test("enqueue 与 ACK 正常热路径各只枚举一次 storage，并直接返回逻辑数量", async () => {
  const queue = await freshQueueModule();
  const unrelatedKeys = 24;
  for (let index = 0; index < unrelatedKeys; index += 1) {
    liveStorage.setItem(`unrelated-${index}`, String(index));
  }

  liveStorage.resetCalls();
  const first = queue.enqueuePendingProgress(payload("candidate-a", "manifest-a", 1));
  assert.equal(first.logicalPendingCount, 1);
  assert.equal(liveStorage.calls.key, unrelatedKeys);

  liveStorage.resetCalls();
  const second = queue.enqueuePendingProgress(payload("candidate-b", "manifest-b", 2));
  assert.equal(second.logicalPendingCount, 2);
  assert.equal(liveStorage.calls.key, unrelatedKeys + 1);

  liveStorage.resetCalls();
  assert.equal(queue.acknowledgePendingProgress(first.entryID), 1);
  assert.equal(liveStorage.calls.key, unrelatedKeys + 2);
  assert.deepEqual(queue.pendingProgressEntries().map((entry) => entry.entryID), [second.entryID]);
});

test("discard mutation 返回删除后的准确逻辑数量", async () => {
  const queue = await freshQueueModule();
  queue.enqueuePendingProgress(payload("candidate-a", "manifest-a", 1));
  queue.enqueuePendingProgress(payload("candidate-b", "manifest-b", 2));

  assert.equal(queue.discardPendingProgressForCandidate("candidate-a"), 1);
  assert.equal(queue.discardPendingProgressForCandidate(""), 1);
  assert.equal(queue.discardPendingProgressForCandidate("candidate-b"), 0);
});
