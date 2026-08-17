import assert from "node:assert/strict";
import test from "node:test";

import {
  CLEAR_SCORE_CONTROL_VALUE,
  PERSONAL_RATING_OPTIONS,
  createUserMarkPatch,
  currentPersonalMarkValue,
  parseScoreControlValue,
  personalRatingOptions,
  personalMarkFieldsForTarget,
  qualityRatingOptions,
  readStatusOptions,
  rereadPriorityOptions,
  scoreControlValue,
} from "../src/lib/userMarks.ts";

test("0 分与未评分使用不同控制值", () => {
  assert.equal(scoreControlValue(null), CLEAR_SCORE_CONTROL_VALUE);
  assert.equal(scoreControlValue(0), "0");
  assert.equal(parseScoreControlValue(CLEAR_SCORE_CONTROL_VALUE, 0, 10), null);
  assert.equal(parseScoreControlValue("0", 0, 10), 0);
  assert.equal(parseScoreControlValue("10", 0, 10), 10);
  assert.throws(() => parseScoreControlValue("", 0, 10), RangeError);
  assert.throws(() => parseScoreControlValue("11", 0, 10), RangeError);

  const values = PERSONAL_RATING_OPTIONS.map((option) => option.value);
  assert.deepEqual(values, [null, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10]);
});

test("个人评分清除发送 null，真实 0 分保持为 0", () => {
  assert.deepEqual(createUserMarkPatch("work", "work-1", "personal_rating", null), {
    target_type: "work",
    target_id: "work-1",
    personal_rating: null,
  });
  assert.deepEqual(createUserMarkPatch("work", "work-1", "personal_rating", 0), {
    target_type: "work",
    target_id: "work-1",
    personal_rating: 0,
  });
});

test("每次操作只生成一个受控 patch 字段", () => {
  assert.deepEqual(createUserMarkPatch("series", " series-1 ", "read_status", "completed", "2026-07-14T01:02:03.000Z"), {
    target_type: "series",
    target_id: "series-1",
    client_updated_at: "2026-07-14T01:02:03.000Z",
    read_status: "completed",
  });
  assert.deepEqual(createUserMarkPatch("series", "series-1", "reread_priority", 3), {
    target_type: "series",
    target_id: "series-1",
    reread_priority: 3,
  });
  assert.deepEqual(createUserMarkPatch("work", "work-1", "translation_quality", null), {
    target_type: "work",
    target_id: "work-1",
    translation_quality: null,
  });
  assert.deepEqual(createUserMarkPatch("work", "work-1", "image_quality", 5), {
    target_type: "work",
    target_id: "work-1",
    image_quality: 5,
  });
});

test("系列不暴露作品级质量分", () => {
  assert.deepEqual(personalMarkFieldsForTarget("series"), ["read_status", "personal_rating", "reread_priority"]);
  assert.deepEqual(personalMarkFieldsForTarget("work"), [
    "read_status",
    "personal_rating",
    "reread_priority",
    "translation_quality",
    "image_quality",
  ]);
  assert.throws(
    () => createUserMarkPatch("series", "series-1", "translation_quality", 4),
    /系列标记不支持/,
  );
});

test("读取当前值时保留 0 分，不把它降级成未评分", () => {
  const mark = {
    read_status: "reading",
    personal_rating: 0,
    reread_priority: 2,
    translation_quality: null,
    image_quality: 4,
  };
  assert.equal(currentPersonalMarkValue(mark, "personal_rating"), 0);
  assert.equal(currentPersonalMarkValue(mark, "read_status"), "reading");
  assert.equal(currentPersonalMarkValue(mark, "translation_quality"), null);
  assert.equal(currentPersonalMarkValue(null, "reread_priority"), 0);
});

test("非法阅读状态、区间和空目标会在发请求前被拒绝", () => {
  assert.throws(() => createUserMarkPatch("work", "work-1", "read_status", "done"), /无效的阅读状态/);
  assert.throws(() => createUserMarkPatch("work", "work-1", "reread_priority", 4), RangeError);
  assert.throws(() => createUserMarkPatch("work", "work-1", "image_quality", 0), RangeError);
  assert.throws(() => createUserMarkPatch("work", " ", "personal_rating", 8), /缺少目标 ID/);
});

test("个人标记选项和本地校验错误支持中英日三语", () => {
  assert.deepEqual(readStatusOptions("en").map((option) => option.label), [
    "Unread",
    "Reading",
    "Completed",
    "On hold",
  ]);
  assert.deepEqual(readStatusOptions("ja").map((option) => option.label), ["未読", "読書中", "読了", "保留"]);
  assert.equal(personalRatingOptions("en")[0].accessibleLabel, "Clear the personal rating and return to unrated");
  assert.equal(personalRatingOptions("ja")[10].accessibleLabel, "個人評価：9点");
  assert.equal(rereadPriorityOptions("en")[2].accessibleLabel, "Medium reread priority");
  assert.equal(rereadPriorityOptions("ja")[3].accessibleLabel, "再読優先度：高");
  assert.equal(qualityRatingOptions("en")[5].accessibleLabel, "Quality rating: 5");
  assert.equal(qualityRatingOptions("ja")[0].label, "クリア");

  assert.throws(
    () => parseScoreControlValue("", 0, 10, "en"),
    /explicit integer or the clear value/,
  );
  assert.throws(
    () => parseScoreControlValue("11", 0, 10, "ja"),
    /0 から 10 の範囲/,
  );
  assert.throws(
    () => createUserMarkPatch("work", " ", "personal_rating", 8, "", "en"),
    /missing a target ID/,
  );
  assert.throws(
    () => createUserMarkPatch("series", "series-1", "translation_quality", 4, "", "ja"),
    /シリーズマーク/,
  );
});
