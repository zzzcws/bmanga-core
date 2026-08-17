import assert from "node:assert/strict";
import test from "node:test";

import { ApiError, apiGet, pageUrl } from "../src/lib/api.ts";

test("reader page URLs cap at 3200 and opt into source quality only when requested", () => {
  assert.equal(
    pageUrl("work-1", 3, "manifest-1", 9000, true),
    "/page?id=work-1&index=3&manifest=manifest-1&max=3200&quality=source",
  );
  assert.equal(
    pageUrl("work-1", 3, "manifest-1", 2400),
    "/page?id=work-1&index=3&manifest=manifest-1&max=2400",
  );
});

test("GET retries one transient server failure and returns the recovered payload", async () => {
  const originalFetch = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = async () => {
    calls += 1;
    if (calls === 1) {
      return new Response(JSON.stringify({ error: "temporary" }), {
        status: 503,
        headers: { "content-type": "application/json" },
      });
    }
    return new Response(JSON.stringify({ ok: true }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  };
  try {
    assert.deepEqual(await apiGet("/api/work", { params: { id: "work-1" } }), { ok: true });
    assert.equal(calls, 2);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("GET does not retry a permanent client error", async () => {
  const originalFetch = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = async () => {
    calls += 1;
    return new Response(JSON.stringify({ error: "work not found" }), {
      status: 404,
      headers: { "content-type": "application/json" },
    });
  };
  try {
    await assert.rejects(
      apiGet("/api/work", { params: { id: "missing" } }),
      (error) => error instanceof ApiError && error.status === 404,
    );
    assert.equal(calls, 1);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("GET retry delay obeys cancellation", async () => {
  const originalFetch = globalThis.fetch;
  const controller = new AbortController();
  let calls = 0;
  globalThis.fetch = async () => {
    calls += 1;
    return new Response(JSON.stringify({ error: "temporary" }), {
      status: 500,
      headers: { "content-type": "application/json" },
    });
  };
  try {
    const pending = apiGet("/api/pages", { signal: controller.signal, params: { id: "work-1" } });
    globalThis.setTimeout(() => controller.abort(), 10);
    await assert.rejects(pending, (error) => error?.name === "AbortError");
    assert.equal(calls, 1);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
