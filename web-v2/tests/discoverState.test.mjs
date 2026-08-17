import assert from "node:assert/strict";
import test from "node:test";

import {
  DISCOVER_HISTORY_LIMIT,
  hasDiscoverAuxiliary,
  mergeDiscoverPayload,
  planDiscoverRequest,
} from "../src/lib/discoverState.ts";

const stats = {
  favorite_count: 2,
  history_count: 3,
  liked_count: 4,
  rated_count: 5,
  reread_count: 6,
};

function work(candidateID) {
  return {
    candidate_id: candidateID,
    work_identity_id: `identity-${candidateID}`,
    title: candidateID,
    display_title: candidateID,
  };
}

function progress(candidateID) {
  return {
    candidate_id: candidateID,
    work_identity_id: `identity-${candidateID}`,
    page_manifest_id: `manifest-${candidateID}`,
    manifest_hash: "hash",
    index: 2,
    count: 10,
    progress_percent: 30,
    completed: false,
    progress_status: "reading",
    reader_fit_mode: "fit-page",
    reader_split_panel: 0,
    stage_scroll_top: 0,
    stage_scroll_left: 0,
    updated_at: "2026-07-13T12:00:00Z",
    last_read_at: "2026-07-13T12:00:00Z",
    title: candidateID,
  };
}

function payload(overrides = {}) {
  return {
    total: 1,
    random_mode: "unread",
    random_items: [work("random-1")],
    ...overrides,
  };
}

test("首次加载和 dataRevision 变化时请求完整辅助数据", () => {
  const initial = planDiscoverRequest("unread", 0, null);
  const revised = planDiscoverRequest("liked", 3, 2);

  assert.equal(initial.includeAuxiliary, true);
  assert.deepEqual(initial.query, {
    randomMode: "unread",
    randomLimit: 12,
    historyLimit: DISCOVER_HISTORY_LIMIT,
    includeHistory: undefined,
    includeStats: undefined,
    lean: true,
  });
  assert.equal(revised.includeAuxiliary, true);
  assert.equal(revised.query.includeHistory, undefined);
  assert.equal(revised.query.includeStats, undefined);
});

test("同一 dataRevision 的换批和模式切换只请求随机书架", () => {
  const planned = planDiscoverRequest("reread", 7, 7);

  assert.equal(planned.includeAuxiliary, false);
  assert.deepEqual(planned.query, {
    randomMode: "reread",
    randomLimit: 12,
    historyLimit: 6,
    includeHistory: 0,
    includeStats: 0,
    lean: true,
  });
});

test("部分响应复用辅助数据并重新计算合并后的 total", () => {
  const current = {
    total: 3,
    random_mode: "unread",
    random_items: [work("old-random")],
    history: [{ ...work("history-1"), progress: progress("history-1") }],
    stats,
  };

  const merged = mergeDiscoverPayload(current, payload({
    random_mode: "liked",
    random_items: [work("new-random")],
  }));

  assert.equal(merged.random_mode, "liked");
  assert.equal(merged.random_items[0]?.candidate_id, "new-random");
  assert.equal(merged.history, current.history);
  assert.equal(merged.stats, current.stats);
  assert.equal(merged.total, 2);
});

test("完整响应使用最新辅助数据，首次缺省时给出安全空值", () => {
  const complete = payload({ history: [], stats });
  const mergedComplete = mergeDiscoverPayload(null, complete);
  const mergedMissing = mergeDiscoverPayload(null, payload());

  assert.equal(hasDiscoverAuxiliary(complete), true);
  assert.equal(hasDiscoverAuxiliary(payload()), false);
  assert.deepEqual(mergedComplete.history, []);
  assert.equal(mergedComplete.stats, stats);
  assert.deepEqual(mergedMissing.history, []);
  assert.deepEqual(mergedMissing.stats, {
    favorite_count: 0,
    history_count: 0,
    liked_count: 0,
    rated_count: 0,
    reread_count: 0,
  });
});
