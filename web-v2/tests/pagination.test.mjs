import assert from "node:assert/strict";
import test from "node:test";

import { clampPaginationPage, paginationTokens } from "../src/lib/pagination.ts";

test("短列表展示全部页码", () => {
  assert.deepEqual(paginationTokens(2, 5), [1, 2, 3, 4, 5]);
});

test("长列表在开头、中央和末尾保留紧凑导航", () => {
  assert.deepEqual(paginationTokens(1, 1739), [1, 2, 3, 4, 5, "ellipsis", 1739]);
  assert.deepEqual(paginationTokens(870, 1739), [1, "ellipsis", 869, 870, 871, "ellipsis", 1739]);
  assert.deepEqual(paginationTokens(1739, 1739), [1, "ellipsis", 1735, 1736, 1737, 1738, 1739]);
});

test("直接跳页会被限制在有效页码内", () => {
  assert.equal(clampPaginationPage(-8, 1739), 1);
  assert.equal(clampPaginationPage(88.9, 1739), 88);
  assert.equal(clampPaginationPage(9999, 1739), 1739);
  assert.equal(clampPaginationPage(Number.NaN, 1739), 1);
});
