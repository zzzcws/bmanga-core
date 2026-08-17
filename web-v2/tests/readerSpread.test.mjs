import assert from "node:assert/strict";
import test from "node:test";

import { splitWideActive, splitWidePanelStep } from "../src/lib/readerSpread.ts";

test("横页拆分只对达到阈值的宽图生效", () => {
  assert.equal(splitWideActive("split-wide", 2300, 1600), true);
  assert.equal(splitWideActive("split-wide", 1100, 1600), false);
  assert.equal(splitWideActive("fit-page", 2300, 1600), false);
});

test("向前阅读按右半到左半，且不提前换作品页", () => {
  assert.deepEqual(splitWidePanelStep(0, 1, true), { handled: true, panel: 1 });
  assert.deepEqual(splitWidePanelStep(1, 1, true), { handled: false, panel: 1 });
});

test("向后阅读先从左半回到右半，竖图不拦截换页", () => {
  assert.deepEqual(splitWidePanelStep(1, -1, true), { handled: true, panel: 0 });
  assert.deepEqual(splitWidePanelStep(0, -1, true), { handled: false, panel: 0 });
  assert.deepEqual(splitWidePanelStep(1, -1, false), { handled: false, panel: 1 });
});
