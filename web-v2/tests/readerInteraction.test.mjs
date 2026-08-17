import assert from "node:assert/strict";
import test from "node:test";

import {
  readerImageRequestKey,
  readerStageClickAction,
  requestedSplitPanelForPage,
  showReaderVisualLoading,
  shouldRefreshReaderChromeOnPointerMove,
} from "../src/lib/readerInteraction.ts";

test("隐藏工具栏时左右点击直接翻页且不改变工具栏状态", () => {
  assert.deepEqual(readerStageClickAction(0.18, false), { type: "navigate", direction: -1, revealChrome: false });
  assert.deepEqual(readerStageClickAction(0.82, false), { type: "navigate", direction: 1, revealChrome: false });
});

test("显示工具栏时左右点击仍直接翻页", () => {
  assert.deepEqual(readerStageClickAction(0.18, true), { type: "navigate", direction: -1, revealChrome: false });
  assert.deepEqual(readerStageClickAction(0.82, true), { type: "navigate", direction: 1, revealChrome: false });
});

test("只有中间点击切换工具栏，边界沿用原来的严格比较", () => {
  assert.deepEqual(readerStageClickAction(0.5, false), { type: "chrome", visible: true });
  assert.deepEqual(readerStageClickAction(0.5, true), { type: "chrome", visible: false });
  assert.deepEqual(readerStageClickAction(0.32, false), { type: "chrome", visible: true });
  assert.deepEqual(readerStageClickAction(0.68, true), { type: "chrome", visible: false });
  assert.deepEqual(readerStageClickAction(Number.NaN, false), { type: "chrome", visible: true });
});

test("鼠标移动只在工具栏已经显示时续期，不会从隐藏态唤醒", () => {
  assert.equal(shouldRefreshReaderChromeOnPointerMove(true), true);
  assert.equal(shouldRefreshReaderChromeOnPointerMove(false), false);
  assert.equal(shouldRefreshReaderChromeOnPointerMove(undefined), false);
});

test("阅读器加载提示按完整请求身份隔离，快速翻页不遮住旧图", () => {
  const first = readerImageRequestKey("work-a", "manifest-a", 3, 0, true, false);
  const retry = readerImageRequestKey("work-a", "manifest-a", 3, 1, true, false);
  assert.notEqual(first, retry);
  assert.equal(readerImageRequestKey("work-a", "manifest-a", 3, 1, false, false), "");
  assert.equal(readerImageRequestKey("work-a", "manifest-a", 3, 1, true, true), "");

  assert.equal(showReaderVisualLoading(true, "", first, ""), true);
  assert.equal(showReaderVisualLoading(true, "blob:old", first, ""), false);
  assert.equal(showReaderVisualLoading(true, "blob:old", first, first), true);
  assert.equal(showReaderVisualLoading(true, "blob:old", retry, first), false);
  assert.equal(showReaderVisualLoading(false, "blob:new", "", first), false);
});

test("跨页请求与重试保留目标半页，但不提前改写当前画面", () => {
  assert.equal(requestedSplitPanelForPage(5, 4, 4, 1, 1), 0);
  assert.equal(requestedSplitPanelForPage(3, 4, 4, 0, 0, 1), 1);
  assert.equal(requestedSplitPanelForPage(3, 4, 3, 0, 1), 1);
  assert.equal(requestedSplitPanelForPage(4, 4, 3, 1, 0), 1);
});
