import assert from "node:assert/strict";
import test from "node:test";

import { readerForwardPrefetchIndices } from "../src/lib/readerPrefetch.ts";

test("同人本顺序准备后两页且不会越过结尾", () => {
  assert.deepEqual(readerForwardPrefetchIndices(3, 12, 2), [4, 5]);
  assert.deepEqual(readerForwardPrefetchIndices(10, 12, 2), [11]);
  assert.deepEqual(readerForwardPrefetchIndices(11, 12, 2), []);
});

test("高清漫画可限制为只准备紧邻下一页", () => {
  assert.deepEqual(readerForwardPrefetchIndices(3, 12, 1), [4]);
});

test("异常输入不会生成无界预取计划", () => {
  assert.deepEqual(readerForwardPrefetchIndices(-3, 3, 99), [1, 2]);
  assert.deepEqual(readerForwardPrefetchIndices(1, 3, 0), []);
});
