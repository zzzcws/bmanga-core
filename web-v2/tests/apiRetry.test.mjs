import assert from "node:assert/strict";
import test from "node:test";

import { ApiError, apiErrorText, apiGet, pageUrl } from "../src/lib/api.ts";

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

test("known API errors are synthesized in the requested locale", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => new Response(JSON.stringify({ error: "work not found" }), {
    status: 404,
    headers: { "content-type": "application/json" },
  });
  try {
    await assert.rejects(
      apiGet("/api/work", { locale: "en", params: { id: "missing" } }),
      (error) => error instanceof ApiError && error.message === "This work could not be found.",
    );
    await assert.rejects(
      apiGet("/api/work", { locale: "ja", params: { id: "missing" } }),
      (error) => error instanceof ApiError && error.message === "この作品が見つかりません。",
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("unknown server details are preserved behind a localized prefix", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => new Response(JSON.stringify({ error: "storage paused" }), {
    status: 400,
    headers: { "content-type": "application/json" },
  });
  try {
    await assert.rejects(
      apiGet("/api/work", { locale: "en", params: { id: "work-1" } }),
      (error) => error instanceof ApiError && error.message === "Server message: storage paused",
    );
    await assert.rejects(
      apiGet("/api/work", { locale: "ja", params: { id: "work-1" } }),
      (error) => error instanceof ApiError && error.message === "サーバーからのメッセージ：storage paused",
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("local API failures preserve sanitized local errors and expose locale-aware cancellation and same-origin text", async () => {
  assert.equal(apiErrorText({ name: "AbortError" }, "en"), "The request was cancelled.");
  assert.equal(apiErrorText({ name: "AbortError" }, "ja"), "リクエストはキャンセルされました。");
  assert.equal(apiErrorText(new Error("No readable pages"), "en"), "No readable pages");
  const localFailure = ["C:", "private", "library", "failure"].join("\\");
  assert.equal(apiErrorText(new Error(localFailure), "en"), "Local path");
  await assert.rejects(
    apiGet("https://example.invalid/api", { locale: "ja" }),
    (error) => error instanceof ApiError
      && apiErrorText(error, "ja") === "bmanga と同一オリジンの API のみにアクセスできます。",
  );
});
