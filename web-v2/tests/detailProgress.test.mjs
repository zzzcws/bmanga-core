import assert from "node:assert/strict";
import test from "node:test";

import {
  applySeriesProgressSummary,
  patchContinueTargetProgress,
  patchDetailProgress,
  patchHistoryEntryDetailProgress,
  patchSnapshotDetailProgress,
} from "../src/lib/detailProgress.ts";

function progress(candidateID, index, count, completed = false) {
  return {
    candidate_id: candidateID,
    work_identity_id: `work:${candidateID}`,
    page_manifest_id: `manifest:${candidateID}`,
    manifest_hash: `hash:${candidateID}`,
    index,
    count,
    progress_percent: count > 0 ? ((index + 1) / count) * 100 : 0,
    completed,
    progress_status: "normal",
    reader_fit_mode: "fit-page",
    reader_split_panel: 0,
    stage_scroll_top: 0,
    stage_scroll_left: 0,
    updated_at: "2026-07-14T12:38:27.790Z",
    last_read_at: "2026-07-14T12:38:27.790Z",
    title: candidateID,
  };
}

function work(candidateID, sequence) {
  return {
    candidate_id: candidateID,
    work_identity_id: `work:${candidateID}`,
    title: candidateID,
    display_title: candidateID,
    can_read: true,
    sequence_number: sequence,
  };
}

function seriesDetail(currentProgress) {
  const chapter51 = work("chapter-5.1", 15);
  const chapter52 = work("chapter-5.2", 16);
  chapter51.progress = currentProgress;
  return {
    kind: "series",
    data: {
      series: { group_id: "series-1", series_title: "Series", display_title: "Series" },
      items: [chapter51, chapter52],
      sections: [],
      sectioned: false,
      section_summary: "",
      cover_candidates: [],
      mark: null,
    },
    progress: currentProgress,
  };
}

test("fresh detail summary updates the target while retaining mutable notes and directory additions", () => {
  const current = seriesDetail(progress("chapter-5.1", 2, 13));
  current.progressState = "loading";
  current.data.mark = { notes: "Saved note remains unchanged", favorite: true };
  current.data.items.push(work("chapter-5.3", 17));
  const latest = { ...progress("chapter-5.2", 4, 14), updated_at: "2026-07-15T00:00:00Z", last_read_at: "2026-07-15T00:00:00Z" };
  const result = applySeriesProgressSummary(current, "series-1", latest);
  assert.equal(result.progressState, "ready");
  assert.equal(result.progress, latest);
  assert.equal(result.data.items[1].progress, latest);
  assert.equal(result.data.items.length, 3);
  assert.equal(result.data.mark, current.data.mark);
  assert.equal(current.progressState, "loading");
});

test("a late detail summary cannot roll back a newer ACK in the same or another chapter", () => {
  const older = progress("chapter-5.1", 2, 13);
  const newer = { ...older, index: 8, updated_at: "2026-07-15T00:00:00Z", last_read_at: "2026-07-15T00:00:00Z" };
  const same = applySeriesProgressSummary(seriesDetail(newer), "series-1", older, [newer]);
  assert.equal(same.progress, newer);
  assert.equal(same.data.items[0].progress, newer);
  const current = patchDetailProgress(seriesDetail(older), "chapter-5.2", { ...newer, candidate_id: "chapter-5.2", work_identity_id: "work:chapter-5.2" });
  const other = applySeriesProgressSummary(current, "series-1", older, [current.progress]);
  assert.equal(other.progress.candidate_id, "chapter-5.2");
  assert.equal(other.progress.index, 8);
});

test("detail summaries reject stale series and replacement identities without unlocking reading", () => {
  const current = seriesDetail(null);
  assert.equal(applySeriesProgressSummary(current, "other-series", progress("chapter-5.1", 2, 13)), current);
  for (const incoming of [progress("missing-chapter", 2, 13), { ...progress("chapter-5.1", 2, 13), work_identity_id: "replaced-work" }]) {
    const result = applySeriesProgressSummary(current, "series-1", incoming);
    assert.equal(result.progressState, "error");
    assert.equal(result.data, current.data);
  }
});

test("an unread summary confirms readiness without inventing progress", () => {
  const current = seriesDetail(null);
  const result = applySeriesProgressSummary(current, "series-1", null);
  assert.equal(result.progressState, "ready");
  assert.equal(result.progress, null);
  assert.equal(result.data.mark, current.data.mark);
});

test("an authoritative null clears old history progress but retains ACKs from this read", () => {
  const stale = progress("chapter-5.1", 2, 13);
  const current = seriesDetail(stale);
  current.data.items[0].progress_index = 2;
  current.data.items[0].progress_count = 13;
  current.data.items[0].progress_updated_at = stale.updated_at;
  const cleared = applySeriesProgressSummary(current, "series-1", null);
  assert.equal(cleared.progress, null);
  assert.equal(cleared.data.items[0].progress, undefined);
  assert.equal(cleared.data.items[0].progress_count, undefined);
  assert.equal(cleared.data.items[0].progress_updated_at, undefined);
  const ack = progress("chapter-5.2", 8, 14);
  const retained = applySeriesProgressSummary(current, "series-1", null, [ack]);
  assert.equal(retained.progress, ack);
  assert.equal(retained.data.items[1].progress, ack);
  assert.equal(retained.data.items[0].progress, undefined);
});

test("ACKs received before directory mount replay against final membership for null and non-null summaries", () => {
  const stale = progress("chapter-5.1", 2, 13);
  const ack = { ...progress("chapter-5.2", 9, 14), updated_at: "2026-07-15T00:00:00Z", last_read_at: "2026-07-15T00:00:00Z" };
  for (const summary of [null, stale]) {
    const result = applySeriesProgressSummary(seriesDetail(stale), "series-1", summary, [ack, progress("unrelated", 11, 20)]);
    assert.equal(result.progress, ack);
    assert.equal(result.data.items[1].progress.index, 9);
    assert.equal(result.data.items.length, 2);
    const staleAck = { ...ack, index: 1, updated_at: "2026-07-13T00:00:00Z", last_read_at: "2026-07-13T00:00:00Z" };
    assert.equal(applySeriesProgressSummary(seriesDetail(ack), "series-1", ack, [staleAck]).progress.index, 9);
  }
  const changed = seriesDetail(stale);
  changed.data.items[1].candidate_id = "current-chapter";
  const remapped = applySeriesProgressSummary(changed, "series-1", null, [ack]);
  assert.equal(remapped.progress.candidate_id, "current-chapter");
  assert.equal(remapped.progress.work_identity_id, ack.work_identity_id);
});

test("a non-null authoritative summary replaces a later cached anchor after another-device reset", () => {
  const cached = { ...progress("chapter-5.1", 12, 13), updated_at: "2026-07-15T00:00:00Z", last_read_at: "2026-07-15T00:00:00Z" };
  const remaining = progress("chapter-5.2", 2, 14);
  const current = seriesDetail(cached);
  const result = applySeriesProgressSummary(current, "series-1", remaining);
  assert.equal(result.progress, remaining);
  assert.equal(result.progressState, "ready");
  assert.equal(result.data.items[0].progress, cached, "Unrelated chapter display data is not erased by a one-position summary");
});


test("summary identity errors use the caller's localized message", () => {
  const result = applySeriesProgressSummary(seriesDetail(null), "series-1", progress("missing", 1, 12), [], "Localized directory mismatch");
  assert.equal(result.progressState, "error");
  assert.equal(result.progressError, "Localized directory mismatch");
});

test("identity-guarded ACKs cannot patch replacement candidates or history", () => {
  const current = seriesDetail(null);
  const ack = progress("chapter-5.1", 8, 13);
  assert.equal(patchDetailProgress(current, ack.candidate_id, ack, "unrelated-identity"), current);
  const snapshot = { detail: current };
  assert.equal(patchSnapshotDetailProgress(snapshot, ack.candidate_id, ack, "unrelated-identity"), snapshot);
  const next = applySeriesProgressSummary(current, "series-1", null, [{ ...ack, work_identity_id: "unrelated-identity" }]);
  assert.equal(next.progress, null);
});

test("next chapter progress patches the restored parent snapshot", () => {
  const chapter51 = progress("chapter-5.1", 12, 13, true);
  const chapter52 = progress("chapter-5.2", 0, 14, false);
  const parent = { view: "library", detail: seriesDetail(chapter51), scrollY: 320 };

  const patched = patchSnapshotDetailProgress(parent, "chapter-5.2", chapter52);

  assert.notEqual(patched, parent);
  assert.equal(patched.detail.progress.candidate_id, "chapter-5.2");
  assert.equal(patched.detail.data.items[1].progress.candidate_id, "chapter-5.2");
  assert.equal(patched.detail.data.items[1].progress_index, 0);
  assert.equal(patched.scrollY, 320);
  assert.equal(parent.detail.progress.candidate_id, "chapter-5.1");
});

test("progress application updates every related history entry before back navigation", () => {
  const chapter51 = progress("chapter-5.1", 12, 13, true);
  const chapter52 = progress("chapter-5.2", 0, 14, false);
  const unrelatedSnapshot = { detail: seriesDetail(chapter51), route: "unrelated" };
  unrelatedSnapshot.detail.data.series.group_id = "series-2";
  unrelatedSnapshot.detail.data.items = [work("other-work", 1)];
  const entries = new Map([
    [1, { parent: 0, snapshot: { detail: seriesDetail(chapter51), route: "series-detail" } }],
    [2, { parent: 1, snapshot: { detail: seriesDetail(chapter51), route: "reader" } }],
    [3, { parent: 0, snapshot: unrelatedSnapshot }],
  ]);
  const unrelatedEntry = entries.get(3);

  const patchedCount = patchHistoryEntryDetailProgress(entries.values(), "chapter-5.2", chapter52);

  assert.equal(patchedCount, 2);
  assert.equal(entries.get(1).snapshot.detail.progress.candidate_id, "chapter-5.2");
  assert.equal(entries.get(2).snapshot.detail.progress.candidate_id, "chapter-5.2");
  assert.equal(entries.get(1).parent, 0);
  assert.equal(entries.get(2).parent, 1);
  assert.equal(entries.get(3), unrelatedEntry);
});

test("a newer meaningful earlier-chapter save becomes the series resume cursor", () => {
  const chapter52 = progress("chapter-5.2", 0, 14, false);
  const chapter51Late = progress("chapter-5.1", 7, 13, false);
  chapter51Late.updated_at = "2026-07-14T12:39:27.790Z";
  const current = patchDetailProgress(seriesDetail(progress("chapter-5.1", 12, 13, true)), "chapter-5.2", chapter52);

  const patched = patchDetailProgress(current, "chapter-5.1", chapter51Late);

  assert.equal(patched.progress.candidate_id, "chapter-5.1");
  assert.equal(patched.data.items[0].progress.candidate_id, "chapter-5.1");
  assert.equal(patched.data.items[0].progress.index, 7);
  assert.equal(patched.data.items[0].progress.completed, false);
});

test("opening an earlier chapter at page one does not replace meaningful series progress", () => {
  const chapter52 = progress("chapter-5.2", 4, 14, false);
  const chapter51Opened = progress("chapter-5.1", 0, 13, false);
  chapter51Opened.updated_at = "2026-07-14T12:39:27.790Z";
  chapter51Opened.last_read_at = chapter51Opened.updated_at;
  const current = patchDetailProgress(
    seriesDetail(progress("chapter-5.1", 12, 13, true)),
    "chapter-5.2",
    chapter52,
  );

  const patched = patchDetailProgress(current, "chapter-5.1", chapter51Opened);

  assert.equal(patched.progress.candidate_id, "chapter-5.2");
  assert.equal(patched.data.items[0].progress.index, 0);
});

test("unrelated detail snapshots retain identity and private state", () => {
  const snapshot = {
    detail: seriesDetail(progress("chapter-5.1", 12, 13, true)),
    noteDraft: "private note",
  };

  const patched = patchSnapshotDetailProgress(snapshot, "other-work", progress("other-work", 0, 3));

  assert.equal(patched, snapshot);
  assert.equal(patched.noteDraft, "private note");
});

test("首页续读目标在进入下一话后原子前移", () => {
  const chapter51 = work("chapter-5.1", 15);
  const chapter52 = work("chapter-5.2", 16);
  const chapter53 = work("chapter-5.3", 17);
  const current = {
    item: chapter51,
    progress: progress("chapter-5.1", 12, 13, true),
    series: { group_id: "series-1", series_title: "Series" },
    next_item: chapter52,
  };
  const chapter52Progress = progress("chapter-5.2", 0, 14, false);
  chapter52Progress.updated_at = "2026-07-14T12:39:27.790Z";
  chapter52Progress.last_read_at = chapter52Progress.updated_at;

  const patched = patchContinueTargetProgress(current, "chapter-5.2", chapter52Progress, chapter52, chapter53);

  assert.equal(patched.item.candidate_id, "chapter-5.2");
  assert.equal(patched.item.progress_index, 0);
  assert.equal(patched.progress.candidate_id, "chapter-5.2");
  assert.equal(patched.next_item.candidate_id, "chapter-5.3");
  assert.equal(patched.series, current.series);
  assert.equal(current.item.candidate_id, "chapter-5.1");
});

test("无关作品进度不会改写首页续读目标", () => {
  const current = {
    item: work("chapter-5.1", 15),
    progress: progress("chapter-5.1", 2, 13, false),
    series: null,
    next_item: null,
  };
  assert.equal(
    patchContinueTargetProgress(current, "other-work", progress("other-work", 0, 4), work("other-work", 1)),
    current,
  );
});

test("更新的无关作品会成为首页续读目标，旧并发响应不会倒灌", () => {
  const current = {
    item: work("chapter-5.1", 15),
    progress: progress("chapter-5.1", 2, 13, false),
    series: { group_id: "series-1", series_title: "Series 1" },
    next_item: work("chapter-5.2", 16),
  };
  const newer = progress("other-work", 1, 4, false);
  newer.updated_at = "2026-07-14T12:39:27.790Z";
  newer.last_read_at = newer.updated_at;
  const other = work("other-work", 1);
  const promoted = patchContinueTargetProgress(current, "other-work", newer, other, undefined, null);
  assert.equal(promoted.item.candidate_id, "other-work");
  assert.equal(promoted.progress.index, 1);
  assert.equal(promoted.series, null);
  assert.equal(promoted.next_item, null);

  const older = progress("late-old-work", 0, 2, false);
  older.updated_at = "2026-07-14T12:37:27.790Z";
  older.last_read_at = older.updated_at;
  assert.equal(
    patchContinueTargetProgress(promoted, "late-old-work", older, work("late-old-work", 1)),
    promoted,
  );
});

test("首次阅读会立即创建首页续读目标", () => {
  const firstWork = work("first-work", 1);
  const firstProgress = progress("first-work", 0, 8, false);
  const created = patchContinueTargetProgress(null, "first-work", firstProgress, firstWork);

  assert.equal(created.item.candidate_id, "first-work");
  assert.equal(created.item.progress_index, 0);
  assert.equal(created.progress, firstProgress);
  assert.equal(created.series, null);
  assert.equal(created.next_item, null);
  assert.equal(patchContinueTargetProgress(null, "missing", firstProgress), null);
});
