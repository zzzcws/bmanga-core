import test from "node:test";
import assert from "node:assert/strict";

import {
  buildSeriesOutline,
  nextSeriesReadable,
  seriesReadingOrder,
} from "../src/lib/seriesOrder.ts";
import {
  SERIES_DIRECTORY_RANGE_SIZE,
  seriesDirectoryRangeForGroup,
  seriesDirectoryRangeLabel,
  seriesDirectoryRangeWindow,
} from "../src/lib/seriesDirectoryRange.ts";

function work(candidateID, canRead = true) {
  return {
    candidate_id: candidateID,
    display_title: candidateID,
    can_read: canRead,
    readable_page_count: 20,
  };
}

function seriesFixture({ sectioned = true } = {}) {
  const a = work("chapter-1-primary");
  const aAlternate = work("chapter-1-alternate");
  const bUnavailable = work("chapter-2-unavailable", false);
  const bReadable = work("chapter-2-readable");
  const extra = work("extra-1");
  return {
    series: { group_id: "series", display_title: "Series" },
    items: [a, aAlternate, bUnavailable, bReadable, extra],
    sectioned,
    section_summary: "3 groups",
    cover_candidates: [],
    mark: null,
    sections: [
      {
        title: "本篇",
        sort: 0,
        groups: [
          { key: "chapter-1", label: "第1话", sort: 1, sequence: 1, items: [a, aAlternate], primary: a },
          { key: "chapter-2", label: "第2话", sort: 2, sequence: 2, items: [bUnavailable, bReadable], primary: bUnavailable },
        ],
      },
      {
        title: "番外",
        sort: 1,
        groups: [
          { key: "extra-1", label: "番外", sort: 0, sequence: 0, items: [extra], primary: extra },
        ],
      },
    ],
  };
}

test("reading order advances by chapter group instead of opening another edition", () => {
  const data = seriesFixture();
  assert.deepEqual(seriesReadingOrder(data).map((item) => item.candidate_id), [
    "chapter-1-primary",
    "chapter-2-readable",
    "extra-1",
  ]);
  assert.equal(nextSeriesReadable(data, "chapter-1-alternate")?.candidate_id, "chapter-2-readable");
  assert.equal(nextSeriesReadable(data, "chapter-2-unavailable")?.candidate_id, "extra-1");
  assert.equal(nextSeriesReadable(data, "extra-1"), undefined);
  assert.equal(buildSeriesOutline(data).entries.length, 5, "alternate editions must remain reachable in the directory");
});

test("sectioned order keeps extras after the main story even when sequence labels restart", () => {
  const data = seriesFixture();
  assert.equal(seriesReadingOrder(data).at(-1)?.candidate_id, "extra-1");
});

test("continuous series still sorts chapter groups by numeric sequence", () => {
  const data = seriesFixture({ sectioned: false });
  assert.deepEqual(seriesReadingOrder(data).map((item) => item.candidate_id), [
    "extra-1",
    "chapter-1-primary",
    "chapter-2-readable",
  ]);
});

test("series directory ranges keep at most fifty groups and clamp invalid ranges", () => {
  assert.equal(SERIES_DIRECTORY_RANGE_SIZE, 50);
  assert.deepEqual(seriesDirectoryRangeWindow(117, 1), {
    index: 1,
    pages: 3,
    start: 50,
    end: 100,
  });
  assert.deepEqual(seriesDirectoryRangeWindow(117, 99), {
    index: 2,
    pages: 3,
    start: 100,
    end: 117,
  });
  assert.deepEqual(seriesDirectoryRangeWindow(12, -3), {
    index: 0,
    pages: 1,
    start: 0,
    end: 12,
  });
});

test("series directory active group selects its containing range", () => {
  assert.equal(seriesDirectoryRangeForGroup(0, 117), 0);
  assert.equal(seriesDirectoryRangeForGroup(49, 117), 0);
  assert.equal(seriesDirectoryRangeForGroup(50, 117), 1);
  assert.equal(seriesDirectoryRangeForGroup(116, 117), 2);
  assert.equal(seriesDirectoryRangeForGroup(999, 117), 2);
});

test("series directory range labels expose both numeric and chapter bounds", () => {
  const labels = Array.from({ length: 87 }, (_, index) => `第 ${index + 1} 卷`);
  assert.equal(seriesDirectoryRangeLabel(labels, 0), "第 1–50 组 · 第 1 卷 — 第 50 卷");
  assert.equal(seriesDirectoryRangeLabel(labels, 1), "第 51–87 组 · 第 51 卷 — 第 87 卷");
});

test("series outline fallbacks and range chrome support English and Japanese", () => {
  const item = { candidate_id: "untitled", can_read: true, readable_page_count: 1 };
  const data = {
    series: { group_id: "series" },
    items: [item],
    sectioned: false,
    sections: [],
    cover_candidates: [],
    mark: null,
  };
  const english = buildSeriesOutline(data, "en");
  const japanese = buildSeriesOutline(data, "ja");
  assert.equal(english.sections[0].title, "Chapter directory");
  assert.equal(english.sections[0].groups[0].label, "Item 1");
  assert.equal(japanese.sections[0].title, "章一覧");
  assert.equal(japanese.sections[0].groups[0].label, "項目 1");

  const labels = ["Volume 1", "Volume 2"];
  assert.equal(seriesDirectoryRangeLabel(labels, 0, SERIES_DIRECTORY_RANGE_SIZE, "en"), "Groups 1–2 · Volume 1 — Volume 2");
  assert.equal(seriesDirectoryRangeLabel(labels, 0, SERIES_DIRECTORY_RANGE_SIZE, "ja"), "グループ 1–2 · Volume 1 — Volume 2");
  assert.equal(seriesDirectoryRangeLabel([], 0, SERIES_DIRECTORY_RANGE_SIZE, "en"), "No items");
  assert.equal(seriesDirectoryRangeLabel([], 0, SERIES_DIRECTORY_RANGE_SIZE, "ja"), "項目なし");
});
