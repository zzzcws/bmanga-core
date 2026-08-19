import assert from "node:assert/strict";
import test from "node:test";

import {
  DEFAULT_LOCALE,
  LOCALE_STORAGE_KEY,
  SUPPORTED_LOCALES,
  applyDocumentLanguage,
  assertCompleteLocaleDictionaries,
  intlLocale,
  interpolateMessage,
  parseLocale,
  persistLocale,
  readStoredLocale,
  resolveLocale,
  translateMessage,
  validateLocaleDictionaries,
} from "../src/lib/locale.ts";

function memoryStorage(initial = {}) {
  const values = new Map(Object.entries(initial));
  return {
    getItem(key) {
      return values.has(key) ? values.get(key) : null;
    },
    setItem(key, value) {
      values.set(key, value);
    },
    value(key) {
      return values.get(key);
    },
  };
}

const completeDictionaries = {
  "zh-CN": {
    greeting: "你好，{name}",
    resultCount: "共 {count} 项",
  },
  en: {
    greeting: "Hello, {name}",
    resultCount: "{count} results",
  },
  ja: {
    greeting: "こんにちは、{name}",
    resultCount: "{count} 件",
  },
};

test("locale parsing is strict and Chinese remains the deterministic default", () => {
  assert.deepEqual(SUPPORTED_LOCALES, ["zh-CN", "en", "ja"]);
  assert.equal(DEFAULT_LOCALE, "zh-CN");
  assert.equal(parseLocale("zh-CN"), "zh-CN");
  assert.equal(parseLocale(" en "), "en");
  assert.equal(parseLocale("ja"), "ja");
  assert.equal(parseLocale("EN"), null);
  assert.equal(parseLocale("zh"), null);
  assert.equal(parseLocale("fr"), null);
  assert.equal(parseLocale({ language: "en" }), null);
  assert.equal(resolveLocale("fr"), "zh-CN");
  assert.equal(resolveLocale(undefined), "zh-CN");
  assert.equal(readStoredLocale(null), "zh-CN");
});

test("stored locale is restored and invalid or unavailable storage fails closed to Chinese", () => {
  assert.equal(readStoredLocale(memoryStorage({ [LOCALE_STORAGE_KEY]: "en" })), "en");
  assert.equal(readStoredLocale(memoryStorage({ [LOCALE_STORAGE_KEY]: "ja" })), "ja");
  assert.equal(readStoredLocale(memoryStorage({ [LOCALE_STORAGE_KEY]: "{broken" })), "zh-CN");
  assert.equal(readStoredLocale(memoryStorage({ [LOCALE_STORAGE_KEY]: '"en"' })), "zh-CN");
  assert.equal(readStoredLocale({ getItem() { throw new Error("storage disabled"); } }), "zh-CN");
});

test("locale switching persists only supported values and tolerates disabled storage", () => {
  const storage = memoryStorage();
  assert.equal(persistLocale("ja", storage), true);
  assert.equal(storage.value(LOCALE_STORAGE_KEY), "ja");
  assert.equal(readStoredLocale(storage), "ja");
  assert.equal(persistLocale("en", storage), true);
  assert.equal(readStoredLocale(storage), "en");
  assert.equal(persistLocale("fr", storage), false);
  assert.equal(readStoredLocale(storage), "en");
  assert.equal(persistLocale("en", null), false);
  assert.equal(persistLocale("en", { setItem() { throw new Error("storage disabled"); } }), false);
});

test("document language updates through an injected target and invalid values use Chinese", () => {
  const target = { lang: "" };
  assert.equal(applyDocumentLanguage("ja", target), "ja");
  assert.equal(target.lang, "ja");
  assert.equal(applyDocumentLanguage("invalid", target), "zh-CN");
  assert.equal(target.lang, "zh-CN");
});

test("Intl locales use explicit regional mappings without browser-language inference", () => {
  assert.equal(intlLocale("zh-CN"), "zh-CN");
  assert.equal(intlLocale("en"), "en-US");
  assert.equal(intlLocale("ja"), "ja-JP");
  assert.equal(intlLocale("de"), "zh-CN");
});

test("messages interpolate named values without treating replacement text as syntax", () => {
  assert.equal(interpolateMessage("{name} 有 {count} 项", { name: "A$&B", count: 3 }), "A$&B 有 3 项");
  assert.equal(interpolateMessage("{name} / {missing}", { name: "示例" }), "示例 / {missing}");
  assert.equal(translateMessage(completeDictionaries, "en", "greeting", { name: "Mina" }), "Hello, Mina");
  assert.equal(translateMessage(completeDictionaries, "ja", "resultCount", { count: 12 }), "12 件");
});

test("missing, blank, and invalid-locale messages fall back to Chinese", () => {
  const dictionaries = {
    "zh-CN": { greeting: "你好，{name}", onlyChinese: "仅有中文" },
    en: { greeting: "Hello, {name}", onlyChinese: "" },
    ja: { greeting: "こんにちは、{name}" },
  };
  assert.equal(translateMessage(dictionaries, "en", "onlyChinese"), "仅有中文");
  assert.equal(translateMessage(dictionaries, "ja", "onlyChinese"), "仅有中文");
  assert.equal(translateMessage(dictionaries, "fr", "greeting", { name: "Lin" }), "你好，Lin");
  assert.equal(translateMessage(dictionaries, "en", "unknown.key"), "unknown.key");
});

test("dictionary validation accepts exact three-language key and placeholder parity", () => {
  const validation = validateLocaleDictionaries(completeDictionaries);
  assert.equal(validation.complete, true);
  assert.deepEqual(validation.sourceKeys, ["greeting", "resultCount"]);
  assert.deepEqual(validation.issues, []);
  assert.doesNotThrow(() => assertCompleteLocaleDictionaries(completeDictionaries));
});

test("dictionary validation reports absent locales, missing and extra keys, blanks, and placeholder drift", () => {
  const missingLocale = validateLocaleDictionaries({
    "zh-CN": completeDictionaries["zh-CN"],
    en: completeDictionaries.en,
  });
  assert.equal(missingLocale.complete, false);
  assert(missingLocale.issues.some((issue) => issue.locale === "ja" && issue.kind === "missing-locale"));

  const invalid = validateLocaleDictionaries({
    "zh-CN": completeDictionaries["zh-CN"],
    en: { greeting: "Hello, {person}", resultCount: "" },
    ja: { greeting: "こんにちは、{name}", extra: "余分" },
  });
  assert.equal(invalid.complete, false);
  assert(invalid.issues.some((issue) => issue.locale === "en" && issue.key === "greeting" && issue.kind === "placeholder-mismatch"));
  assert(invalid.issues.some((issue) => issue.locale === "en" && issue.key === "resultCount" && issue.kind === "empty-message"));
  assert(invalid.issues.some((issue) => issue.locale === "ja" && issue.key === "resultCount" && issue.kind === "missing-key"));
  assert(invalid.issues.some((issue) => issue.locale === "ja" && issue.key === "extra" && issue.kind === "extra-key"));
  assert.throws(() => assertCompleteLocaleDictionaries({
    "zh-CN": completeDictionaries["zh-CN"],
    en: completeDictionaries.en,
  }), /ja:\*:missing-locale/u);
});
