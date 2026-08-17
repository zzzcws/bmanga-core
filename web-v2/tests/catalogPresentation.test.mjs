import assert from "node:assert/strict";
import test from "node:test";

import {
  cleanTitle,
  isSeries,
  itemCoverID,
  itemCreatorLabel,
  itemContextLabel,
  itemID,
  itemKindDisplayLabel,
  itemKindLabel,
  itemTitle,
  pageMeta,
  progressFor,
} from "../src/lib/catalogPresentation.ts";

test("作品和系列使用稳定标识、封面与标题回退", () => {
  const work = { candidate_id: "work-1", display_title: "作品一" };
  const series = { group_id: "series-1", shelf_type: "series", selected_candidate_id: "cover-1", series_title: "系列一" };
  assert.equal(itemID(work), "work-1");
  assert.equal(itemCoverID(work), "work-1");
  assert.equal(itemTitle(work), "作品一");
  assert.equal(isSeries(work), false);
  assert.equal(itemID(series), "series-1");
  assert.equal(itemCoverID(series), "cover-1");
  assert.equal(itemTitle(series), "系列一");
  assert.equal(isSeries(series), true);
});

test("阅读进度兼容嵌套和旧扁平字段并限制百分比", () => {
  assert.deepEqual(progressFor({ progress: { index: 4, count: 20, progress_percent: 25, completed: false } }), {
    index: 4,
    count: 20,
    percent: 25,
    completed: false,
  });
  assert.deepEqual(progressFor({ progress_index: 8, progress_count: 10, progress_percent: 140 }), {
    index: 8,
    count: 10,
    percent: 100,
    completed: false,
  });
  assert.deepEqual(progressFor({ progress_index: 0, progress_count: 12, progress_completed: "true" }), {
    index: 0,
    count: 12,
    percent: 100,
    completed: true,
  });
  assert.equal(progressFor({}), null);
});

test("目录展示标题、页数与类型保持编辑式契约", () => {
  assert.equal(cleanTitle("[SyntheticCircleAlpha] [PDF] Synthetic Catalog Title [DL版]"), "Synthetic Catalog Title");
  assert.equal(cleanTitle("[SyntheticCircleAlpha][Synthetic Catalog Chronicle][901-943话][2.60 GB]"), "Synthetic Catalog Chronicle");
  assert.equal(cleanTitle("[SyntheticCircleBeta][Synthetic Harbor Handbook][901~934话][完结][WEBP]"), "Synthetic Harbor Handbook");
  assert.equal(pageMeta({ candidate_id: "a", readable_page_count: 24 }), "24 页");
  assert.equal(pageMeta({ group_id: "s", shelf_type: "series", item_count: 8 }), "8 个条目");
  assert.equal(itemCreatorLabel({ candidate_id: "a", display_creator: "作者 / 社团" }), "作者 / 社团");
  assert.equal(itemCreatorLabel({ candidate_id: "a" }), "");
  assert.equal(itemKindLabel({ candidate_id: "a", candidate_type: "doujin" }), "DOUJIN");
  assert.equal(itemKindLabel({ candidate_id: "a", display_library_name: "普通漫画" }), "MANGA");
  assert.equal(itemKindLabel({ group_id: "s", shelf_type: "series" }), "SERIES");
  assert.equal(itemKindDisplayLabel({ candidate_id: "a", candidate_type: "doujin" }), "同人本");
  assert.equal(itemKindDisplayLabel({ candidate_id: "a", display_library_name: "普通漫画" }), "漫画");
  assert.equal(itemKindDisplayLabel({ group_id: "s", shelf_type: "series" }), "漫画系列");
  assert.equal(itemContextLabel({ candidate_id: "a", translation_sources: "汉化组甲" }, "search"), "汉化／翻译 · 汉化组甲");
  assert.equal(itemContextLabel({ candidate_id: "a", display_library_name: "同人本" }, "search"), "馆藏 · 同人本");
  assert.equal(itemContextLabel({ candidate_id: "a", series_title: "系列甲" }, "discover"), "系列 · 系列甲");
  assert.equal(itemContextLabel({ candidate_id: "a", series_title: "系列甲", item_label: "第 4 话" }, "discover"), "系列 · 系列甲；章节 · 第 4 话");
  assert.equal(itemContextLabel({ candidate_id: "a", translation_sources: "汉化组甲" }, "related"), "");
});
