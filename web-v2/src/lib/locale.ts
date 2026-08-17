export const SUPPORTED_LOCALES = ["zh-CN", "en", "ja"] as const;

export type Locale = (typeof SUPPORTED_LOCALES)[number];

export const DEFAULT_LOCALE: Locale = "zh-CN";
export const LOCALE_STORAGE_KEY = "bmanga.uiLocale.v1";

export const LOCALE_OPTIONS: ReadonlyArray<Readonly<{ id: Locale; label: string }>> = [
  { id: "zh-CN", label: "简体中文" },
  { id: "en", label: "English" },
  { id: "ja", label: "日本語" },
];

const supportedLocaleSet = new Set<string>(SUPPORTED_LOCALES);

const intlLocales: Readonly<Record<Locale, string>> = {
  "zh-CN": "zh-CN",
  en: "en-US",
  ja: "ja-JP",
};

export interface LocaleStorageReader {
  getItem(key: string): string | null;
}

export interface LocaleStorageWriter {
  setItem(key: string, value: string): void;
}

export interface DocumentLanguageTarget {
  lang: string;
}

export type MessageParameters = Readonly<Record<string, string | number>>;
export type MessageDictionary = Readonly<Record<string, string>>;
export type LocalizedMessage = Readonly<Record<Locale, string>>;
export type LocaleMessageDictionaries = Readonly<Record<Locale, MessageDictionary>>;
export type PartialLocaleMessageDictionaries = Readonly<Partial<Record<Locale, MessageDictionary>>>;

export type LocaleDictionaryIssueKind =
  | "missing-locale"
  | "missing-key"
  | "empty-message"
  | "extra-key"
  | "placeholder-mismatch";

export interface LocaleDictionaryIssue {
  kind: LocaleDictionaryIssueKind;
  locale: Locale;
  key?: string;
  expected?: readonly string[];
  actual?: readonly string[];
}

export interface LocaleDictionaryValidation {
  complete: boolean;
  sourceKeys: readonly string[];
  issues: readonly LocaleDictionaryIssue[];
}

export function parseLocale(value: unknown): Locale | null {
  if (typeof value !== "string") return null;
  const normalized = value.trim();
  return supportedLocaleSet.has(normalized) ? normalized as Locale : null;
}

export function resolveLocale(value: unknown): Locale {
  return parseLocale(value) ?? DEFAULT_LOCALE;
}

function browserLocalStorage(): (LocaleStorageReader & LocaleStorageWriter) | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

export function readStoredLocale(storage?: LocaleStorageReader | null): Locale {
  const target = storage === undefined ? browserLocalStorage() : storage;
  if (!target) return DEFAULT_LOCALE;
  try {
    return resolveLocale(target.getItem(LOCALE_STORAGE_KEY));
  } catch {
    return DEFAULT_LOCALE;
  }
}

export function persistLocale(locale: unknown, storage?: LocaleStorageWriter | null): boolean {
  const parsed = parseLocale(locale);
  if (!parsed) return false;
  const target = storage === undefined ? browserLocalStorage() : storage;
  if (!target) return false;
  try {
    target.setItem(LOCALE_STORAGE_KEY, parsed);
    return true;
  } catch {
    return false;
  }
}

export function applyDocumentLanguage(locale: unknown, target: DocumentLanguageTarget): Locale {
  const resolved = resolveLocale(locale);
  target.lang = resolved;
  return resolved;
}

export function intlLocale(locale: unknown): string {
  return intlLocales[resolveLocale(locale)];
}

const placeholderPattern = /\{([A-Za-z][A-Za-z0-9_]*)\}/gu;

function placeholders(value: string): string[] {
  return [...new Set([...value.matchAll(placeholderPattern)].map((match) => match[1] || ""))]
    .filter(Boolean)
    .sort();
}

export function interpolateMessage(template: string, parameters: MessageParameters = {}): string {
  return template.replace(placeholderPattern, (placeholder, name: string) => (
    Object.prototype.hasOwnProperty.call(parameters, name)
      ? String(parameters[name])
      : placeholder
  ));
}

export function localizeMessage(
  messages: LocalizedMessage,
  locale: unknown,
  parameters: MessageParameters = {},
): string {
  return interpolateMessage(messages[resolveLocale(locale)] || messages[DEFAULT_LOCALE], parameters);
}

function usableMessage(dictionary: MessageDictionary | undefined, key: string): string | null {
  const value = dictionary?.[key];
  return typeof value === "string" && value.trim() ? value : null;
}

export function translateMessage(
  dictionaries: PartialLocaleMessageDictionaries,
  locale: unknown,
  key: string,
  parameters: MessageParameters = {},
): string {
  const resolved = resolveLocale(locale);
  const template = usableMessage(dictionaries[resolved], key)
    ?? usableMessage(dictionaries[DEFAULT_LOCALE], key)
    ?? key;
  return interpolateMessage(template, parameters);
}

export function validateLocaleDictionaries(
  dictionaries: PartialLocaleMessageDictionaries,
): LocaleDictionaryValidation {
  const issues: LocaleDictionaryIssue[] = [];
  const source = dictionaries[DEFAULT_LOCALE];
  const sourceKeys = source ? Object.keys(source).sort() : [];

  for (const locale of SUPPORTED_LOCALES) {
    const dictionary = dictionaries[locale];
    if (!dictionary) {
      issues.push({ kind: "missing-locale", locale });
      continue;
    }

    const dictionaryKeys = Object.keys(dictionary).sort();
    for (const key of sourceKeys) {
      if (!Object.prototype.hasOwnProperty.call(dictionary, key)) {
        issues.push({ kind: "missing-key", locale, key });
        continue;
      }
      const value = dictionary[key];
      if (typeof value !== "string" || !value.trim()) {
        issues.push({ kind: "empty-message", locale, key });
        continue;
      }
      const expected = placeholders(source?.[key] || "");
      const actual = placeholders(value);
      if (expected.join("\u0000") !== actual.join("\u0000")) {
        issues.push({ kind: "placeholder-mismatch", locale, key, expected, actual });
      }
    }

    for (const key of dictionaryKeys) {
      if (!source || !Object.prototype.hasOwnProperty.call(source, key)) {
        issues.push({ kind: "extra-key", locale, key });
      }
    }
  }

  return {
    complete: issues.length === 0,
    sourceKeys,
    issues,
  };
}

export function assertCompleteLocaleDictionaries(
  dictionaries: PartialLocaleMessageDictionaries,
): asserts dictionaries is LocaleMessageDictionaries {
  const validation = validateLocaleDictionaries(dictionaries);
  if (validation.complete) return;
  const summary = validation.issues.map((issue) => (
    `${issue.locale}:${issue.key || "*"}:${issue.kind}`
  )).join(", ");
  throw new Error(`Locale dictionaries are incomplete: ${summary}`);
}
