import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { formatRuntimeBytes, formatRuntimeUptime } from "../src/lib/runtimeDiagnostics.ts";

test("runtime diagnostics formats aggregate byte counts without paths", () => {
  assert.equal(formatRuntimeBytes(0), "0 B");
  assert.equal(formatRuntimeBytes(1024), "1.00 KiB");
  assert.equal(formatRuntimeBytes(1536), "1.50 KiB");
  assert.equal(formatRuntimeBytes(5 * 1024 * 1024), "5.00 MiB");
  assert.equal(formatRuntimeBytes(Number.NaN), "0 B");
  assert.equal(formatRuntimeBytes(-1), "0 B");
});

test("runtime diagnostics formats bounded uptime values", () => {
  assert.equal(formatRuntimeUptime(42), "42 秒");
  assert.equal(formatRuntimeUptime(65), "1 分钟");
  assert.equal(formatRuntimeUptime(3 * 3600 + 5 * 60), "3 小时 5 分钟");
  assert.equal(formatRuntimeUptime(2 * 86400 + 3600), "2 天 1 小时");
  assert.equal(formatRuntimeUptime(Number.POSITIVE_INFINITY), "0 秒");
});

test("runtime diagnostics bounds the GET and exposes an explicit retry", async () => {
  const component = await readFile(
    new URL("../src/components/RuntimeDiagnosticsLite.tsx", import.meta.url),
    "utf8",
  );

  assert.match(component, /runtimeDiagnosticsRequestTimeoutMs = 5_000/);
  assert.match(component, /globalThis\.setTimeout\(\(\) => \{\s*timedOut = true;\s*controller\.abort\(\)/);
  assert.match(component, /timedOut && isAbortError\(reason\)/);
  assert.match(component, /globalThis\.clearTimeout\(timeout\)/);
  assert.match(component, /setRequestRevision\(\(revision\) => revision \+ 1\)/);
  assert.match(component, />重新读取<\/button>/);
});
