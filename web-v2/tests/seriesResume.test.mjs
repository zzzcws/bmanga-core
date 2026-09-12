import assert from "node:assert/strict";
import test from "node:test";

import {
  compareSeriesResumeProgress,
  isMeaningfulSeriesProgress,
  preferredSeriesResumeProgress,
  selectSeriesContinueItem,
  selectConfirmedSeriesContinueItem,
} from "../src/lib/seriesResume.ts";
import { applySeriesProgressSummary } from "../src/lib/detailProgress.ts";
import { nextSeriesReadable, seriesReadingOrder } from "../src/lib/seriesOrder.ts";

function progress(candidateID, index, updatedAt, options = {}) {
  return {
    candidate_id: candidateID,
    work_identity_id: `work:${candidateID}`,
    page_manifest_id: `manifest:${candidateID}`,
    manifest_hash: `hash:${candidateID}`,
    index,
    count: options.count ?? 190,
    progress_percent: 0,
    completed: options.completed ?? false,
    progress_status: "normal",
    reader_fit_mode: "fit-page",
    reader_split_panel: options.readerSplitPanel ?? 0,
    stage_scroll_top: options.stageScrollTop ?? 0,
    stage_scroll_left: options.stageScrollLeft ?? 0,
    updated_at: updatedAt,
    last_read_at: updatedAt,
    title: candidateID,
  };
}

function work(candidateID, sequence, itemProgress) {
  return {
    candidate_id: candidateID,
    work_identity_id: `work:${candidateID}`,
    title: candidateID,
    display_title: candidateID,
    can_read: true,
    sequence_number: sequence,
    progress: itemProgress,
  };
}

test("a confirmed reset anchor outranks newer cached progress, while legacy selection stays compatible", () => {
  const a = work("synthetic-a", 1, progress("synthetic-a", 4, "2026-01-02T12:00:00Z"));
  const b = work("synthetic-b", 2);
  const incoming = progress("synthetic-b", 8, "2026-01-02T10:00:00Z");
  const applied = applySeriesProgressSummary({ kind: "series", data: { series: { group_id: "synthetic" }, items: [a, b], sections: [] }, progress: null }, "synthetic", incoming);
  const order = seriesReadingOrder(applied.data);
  assert.equal(selectSeriesContinueItem(order, applied.progress), a, "Legacy callers retain their earlier policy");
  assert.equal(selectConfirmedSeriesContinueItem(applied.data.items, applied.progress, order[0], (id) => nextSeriesReadable(applied.data, id))?.candidate_id, "synthetic-b");
  assert.equal(selectConfirmedSeriesContinueItem(applied.data.items, null, order[0], () => undefined), a, "Confirmed unread starts with the first chapter without scanning cached progress");
});

test("confirmed secondary editions resume exactly and completion advances to the next chapter group", () => {
  const primary = work("synthetic-primary", 1);
  const secondary = work("synthetic-secondary", 1);
  const next = work("synthetic-next", 2);
  const data = {
    series: { group_id: "synthetic" }, items: [primary, secondary, next], sectioned: true,
    sections: [{ title: "Synthetic main", groups: [
      { key: "first", sequence: 1, sort: 0, primary, items: [primary, secondary] },
      { key: "next", sequence: 2, sort: 1, primary: next, items: [next] },
    ] }],
  };
  const choose = (incoming) => selectConfirmedSeriesContinueItem(data.items, incoming, seriesReadingOrder(data)[0], (id) => nextSeriesReadable(data, id));
  assert.equal(choose(progress(secondary.candidate_id, 8, "2026-01-01T00:00:00Z")), secondary);
  assert.equal(choose(progress(secondary.candidate_id, 189, "2026-01-01T00:00:00Z", { completed: true })), next);
  assert.equal(choose(progress(next.candidate_id, 189, "2026-01-01T00:00:00Z", { completed: true })), next, "Completed final chapter remains the final chapter");
  assert.equal(choose({ ...progress(secondary.candidate_id, 8, "2026-01-01T00:00:00Z"), work_identity_id: "replaced-identity" }), undefined);
  assert.equal(choose(progress("missing", 8, "2026-01-01T00:00:00Z")), undefined);
});

test("美食猎人形态会选择最近真正翻阅的第 8 卷，而不是停在首页的第 22 卷", () => {
  const items = [
    work("volume-8", 8, progress("volume-8", 45, "2026-07-28T04:22:23.925Z")),
    work("volume-21", 21, progress("volume-21", 189, "2026-07-21T03:25:26.024Z", { completed: true })),
    work("volume-22", 22, progress("volume-22", 0, "2026-07-28T04:18:17.086Z")),
  ];
  assert.equal(selectSeriesContinueItem(items)?.candidate_id, "volume-8");
});

test("ONE PIECE 形态不会让较高卷的第 1 页覆盖较低卷的有效进度", () => {
  const items = [
    work("volume-1", 1, progress("volume-1", 4, "2026-05-12T11:54:40Z")),
    work("volume-4", 4, progress("volume-4", 0, "2026-05-12T11:54:50Z")),
  ];
  assert.equal(selectSeriesContinueItem(items)?.candidate_id, "volume-1");
});

test("齐木楠雄形态保留已完成的末卷，不被后来误开的第 1 卷拉回", () => {
  const items = [
    work("volume-1", 1, progress("volume-1", 0, "2026-07-21T03:33:07.101Z")),
    work("volume-14", 14, progress("volume-14", 180, "2026-07-21T03:33:02.388Z", { completed: true, count: 181 })),
  ];
  assert.equal(selectSeriesContinueItem(items)?.candidate_id, "volume-14");
});

test("完成当前卷后仍会前进到阅读顺序中的下一卷", () => {
  const items = [
    work("volume-1", 1, progress("volume-1", 9, "2026-07-20T00:00:00Z", { completed: true, count: 10 })),
    work("volume-2", 2),
  ];
  assert.equal(selectSeriesContinueItem(items)?.candidate_id, "volume-2");
});

test("横页第二半与页内滚动都属于有效阅读", () => {
  assert.equal(isMeaningfulSeriesProgress(progress("split", 0, "2026-07-20T00:00:00Z", { readerSplitPanel: 1 })), true);
  assert.equal(isMeaningfulSeriesProgress(progress("scroll", 0, "2026-07-20T00:00:00Z", { stageScrollTop: 320 })), true);
  assert.equal(isMeaningfulSeriesProgress(progress("opened", 0, "2026-07-20T00:00:00Z")), false);
});

test("有效阅读优先于较新的首页打开记录，之后再按绝对时间比较", () => {
  const meaningful = progress("meaningful", 2, "2026-07-20T04:00:00Z");
  const opened = progress("opened", 0, "2026-07-20T12:30:00+08:00");
  assert.equal(compareSeriesResumeProgress(meaningful, opened), 1);
  assert.equal(preferredSeriesResumeProgress(opened, meaningful), meaningful);

  const later = progress("later", 3, "2026-07-20T05:00:00Z");
  assert.equal(preferredSeriesResumeProgress(meaningful, later), later);
});
