import assert from "node:assert/strict";
import test from "node:test";

import {
  workCreatorNames,
  workLanguageNames,
  workMetadataOriginalValues,
  workMetadataOverrideValues,
  workSeriesNames,
  workTranslationNames,
} from "../src/lib/workMetadataPresentation.ts";

function detail(overrides = {}) {
  return {
    work: { candidate_id: "work-1", display_title: "作品", ...(overrides.work || {}) },
    creators: overrides.creators || [],
    series: overrides.series || [],
    doujin_series: overrides.doujin_series || [],
    translations: overrides.translations || [],
    title_hints: overrides.title_hints || { creators: [], series: "" },
    related: { series: [], creators: [] },
    mark: null,
  };
}

test("作者显示按人工校正、结构化、展示值和标题提示依次回退", () => {
  assert.deepEqual(workCreatorNames(detail({
    work: {
      metadata_overrides: {
        creator: { field_value: "Synthetic Public Creator" },
        author: { field_value: "Synthetic Legacy Author" },
      },
    },
  })), ["Synthetic Public Creator"]);

  assert.deepEqual(workCreatorNames(detail({
    work: {
      metadata_overrides: {
        circle: { field_value: "Synthetic Override Circle" },
        author: { field_value: "Synthetic Override Author" },
      },
      display_creator: "Synthetic Display Creator",
    },
    creators: [{ creator_display: "Synthetic Structured Creator" }],
    title_hints: { creators: ["Synthetic Hint Creator"], series: "" },
  })), ["Synthetic Override Circle (Synthetic Override Author)"]);

  assert.deepEqual(workCreatorNames(detail({
    work: { display_creator: "Synthetic Display Fallback" },
    creators: [{ creator_display: "SyntheticCircleAlpha (SyntheticArtistBeta)" }],
    title_hints: { creators: ["Synthetic Hint Alpha", "Synthetic Hint Alpha"], series: "" },
  })), ["SyntheticCircleAlpha (SyntheticArtistBeta)"]);

  assert.deepEqual(workCreatorNames(detail({ work: { display_creator: "Synthetic Display Fallback" } })), ["Synthetic Display Fallback"]);
  assert.deepEqual(workCreatorNames(detail({ title_hints: { creators: ["Synthetic Hint Fallback"], series: "" } })), ["Synthetic Hint Fallback"]);
});

test("语言显示使用本地覆盖并对标签去重", () => {
  assert.deepEqual(workLanguageNames(detail({
    work: { metadata_overrides: { language: { field_value: "中文, English, 中文" } } },
  })), ["中文", "English"]);
  assert.deepEqual(workLanguageNames(detail({
    work: { language: "English" },
  })), ["English"]);
});

test("编辑器数据区分扫描原值和本地覆盖", () => {
  const data = detail({
    work: {
      title: "Local Display Title",
      metadata_source_title: "Scanned Work Title",
      metadata_overrides: {
        title: { field_value: "Local Display Title" },
        creator: { field_value: "Local Creator" },
        series: { field_value: "Local Series" },
        language: { field_value: "Local Language" },
      },
    },
    creators: [{ creator_display: "Scanned Creator" }],
    series: [{ series_title: "Scanned Series" }],
  });
  assert.deepEqual(workMetadataOverrideValues(data), {
    title: "Local Display Title",
    creator: "Local Creator",
    series: "Local Series",
    language: "Local Language",
  });
  assert.deepEqual(workMetadataOriginalValues(data), {
    title: "Scanned Work Title",
    creator: "Scanned Creator",
    series: "Scanned Series",
    language: "",
  });
});

test("系列优先使用校正和结构化数据", () => {
  assert.deepEqual(workSeriesNames(detail({
    work: { metadata_overrides: { series: { field_value: "校正系列" } } },
    series: [{ series_title: "结构化系列" }],
    title_hints: { creators: [], series: "标题系列" },
  })), ["校正系列"]);
  assert.deepEqual(workSeriesNames(detail({
    series: [{ series_title: "结构化系列" }],
    doujin_series: [{ series_title: "结构化系列" }],
    title_hints: { creators: [], series: "标题系列" },
  })), ["结构化系列"]);
});

test("汉化来源合并结构化和标题回退并忽略大小写重复", () => {
  assert.deepEqual(workTranslationNames(detail({
    work: { translation_sources: "CHINESE, 星空汉化组" },
    translations: [{ translation_group: "Chinese" }],
  })), ["Chinese", "星空汉化组"]);
});
