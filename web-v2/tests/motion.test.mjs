import assert from "node:assert/strict";
import test from "node:test";

import { preferredScrollBehavior } from "../src/lib/motion.ts";

test("服务端环境不请求平滑滚动", () => {
  assert.equal(preferredScrollBehavior(), "auto");
});

test("遵循系统的减少动态效果偏好", () => {
  const originalWindow = globalThis.window;
  try {
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: { matchMedia: () => ({ matches: true }) },
    });
    assert.equal(preferredScrollBehavior(), "auto");
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: { matchMedia: () => ({ matches: false }) },
    });
    assert.equal(preferredScrollBehavior(), "smooth");
  } finally {
    if (originalWindow === undefined) delete globalThis.window;
    else Object.defineProperty(globalThis, "window", { configurable: true, value: originalWindow });
  }
});
