import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

import {
  DEFAULT_LOCALE,
  LOCALE_STORAGE_KEY,
  applyDocumentLanguage,
  interpolateMessage,
  intlLocale,
  parseLocale,
  persistLocale,
  readStoredLocale,
  type Locale,
  type MessageParameters,
} from "./lib/locale";

export type LocalizedText = Readonly<Record<Locale, string>>;

interface I18nContextValue {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  text: (messages: LocalizedText, parameters?: MessageParameters) => string;
  tr: (chinese: string, english: string, japanese: string, parameters?: MessageParameters) => string;
  number: (value: number, options?: Intl.NumberFormatOptions) => string;
  date: (value: Date | number | string, options?: Intl.DateTimeFormatOptions) => string;
}

const documentText: Readonly<Record<Locale, Readonly<{ description: string }>>> = {
  "zh-CN": {
    description: "本地优先、自托管的漫画与分页图像归档目录和阅读器。",
  },
  en: {
    description: "A local-first, self-hosted catalog and reader for comics and page-oriented image archives.",
  },
  ja: {
    description: "漫画やページ画像アーカイブ向けの、ローカルファーストでセルフホスト可能なカタログ兼リーダーです。",
  },
};

const I18nContext = createContext<I18nContextValue | null>(null);

function updateDocument(locale: Locale): void {
  if (typeof document === "undefined") return;
  applyDocumentLanguage(locale, document.documentElement);
  const description = document.querySelector<HTMLMetaElement>('meta[name="description"]');
  if (description) description.content = documentText[locale].description;
}

export function I18nProvider({ children }: Readonly<{ children: ReactNode }>) {
  const [locale, setLocaleState] = useState<Locale>(() => readStoredLocale());
  const localeRef = useRef(locale);
  localeRef.current = locale;

  const setLocale = useCallback((nextLocale: Locale) => {
    const resolved = parseLocale(nextLocale) ?? DEFAULT_LOCALE;
    setLocaleState(resolved);
    persistLocale(resolved);
  }, []);

  useEffect(() => {
    updateDocument(locale);
  }, [locale]);

  useEffect(() => {
    if (typeof window === "undefined") return undefined;
    const syncLocale = (event: StorageEvent) => {
      if (event.key !== LOCALE_STORAGE_KEY && event.key !== null) return;
      setLocaleState(parseLocale(event.newValue) ?? DEFAULT_LOCALE);
    };
    window.addEventListener("storage", syncLocale);
    return () => window.removeEventListener("storage", syncLocale);
  }, []);

  const text = useCallback((messages: LocalizedText, parameters: MessageParameters = {}) => (
    interpolateMessage(messages[localeRef.current] || messages[DEFAULT_LOCALE], parameters)
  ), []);

  const tr = useCallback((chinese: string, english: string, japanese: string, parameters: MessageParameters = {}) => (
    interpolateMessage({ "zh-CN": chinese, en: english, ja: japanese }[localeRef.current], parameters)
  ), []);

  const number = useCallback((value: number, options?: Intl.NumberFormatOptions) => (
    new Intl.NumberFormat(intlLocale(localeRef.current), options).format(value)
  ), []);

  const date = useCallback((value: Date | number | string, options?: Intl.DateTimeFormatOptions) => {
    const parsed = value instanceof Date ? value : new Date(value);
    return new Intl.DateTimeFormat(intlLocale(localeRef.current), options).format(parsed);
  }, []);

  const context = useMemo<I18nContextValue>(() => ({
    locale,
    setLocale,
    text,
    tr,
    number,
    date,
  }), [date, locale, number, setLocale, text, tr]);

  return <I18nContext.Provider value={context}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nContextValue {
  const context = useContext(I18nContext);
  if (!context) throw new Error("useI18n must be used inside I18nProvider");
  return context;
}
