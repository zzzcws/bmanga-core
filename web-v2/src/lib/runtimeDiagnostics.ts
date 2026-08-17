import { DEFAULT_LOCALE, intlLocale, localizeMessage, type Locale } from "./locale.ts";

export interface RuntimeDiagnosticsLite {
  ok: boolean;
  version: string;
  uptime_seconds: number;
  database: {
    status: "healthy" | "unavailable";
  };
  cache: {
    file_count: number;
    bytes: number;
    scan_errors: number;
    complete: boolean;
  };
}

function finiteNonNegative(value: number): number {
  return Number.isFinite(value) && value > 0 ? value : 0;
}

function formatNumber(value: number, locale: Locale, fractionDigits = 0): string {
  return new Intl.NumberFormat(intlLocale(locale), {
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
    useGrouping: false,
  }).format(value);
}

export function formatRuntimeBytes(value: number, locale: Locale = DEFAULT_LOCALE): string {
  const bytes = finiteNonNegative(value);
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let amount = bytes;
  let unitIndex = 0;
  while (amount >= 1024 && unitIndex < units.length - 1) {
    amount /= 1024;
    unitIndex += 1;
  }
  const digits = unitIndex === 0 || amount >= 100 ? 0 : amount >= 10 ? 1 : 2;
  return `${formatNumber(amount, locale, digits)} ${units[unitIndex]}`;
}

function durationUnit(value: number, unit: "second" | "minute" | "hour" | "day", locale: Locale): string {
  const number = formatNumber(value, locale);
  const singular = value === 1;
  const messages = {
    second: {
      "zh-CN": "{value} 秒",
      en: singular ? "{value} second" : "{value} seconds",
      ja: "{value}秒",
    },
    minute: {
      "zh-CN": "{value} 分钟",
      en: singular ? "{value} minute" : "{value} minutes",
      ja: "{value}分",
    },
    hour: {
      "zh-CN": "{value} 小时",
      en: singular ? "{value} hour" : "{value} hours",
      ja: "{value}時間",
    },
    day: {
      "zh-CN": "{value} 天",
      en: singular ? "{value} day" : "{value} days",
      ja: "{value}日",
    },
  } as const;
  return localizeMessage(messages[unit], locale, { value: number });
}

export function formatRuntimeUptime(value: number, locale: Locale = DEFAULT_LOCALE): string {
  const seconds = Math.floor(finiteNonNegative(value));
  if (seconds < 60) return durationUnit(seconds, "second", locale);
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return durationUnit(minutes, "minute", locale);
  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  if (hours < 24) {
    return remainingMinutes
      ? `${durationUnit(hours, "hour", locale)}${locale === "ja" ? "" : " "}${durationUnit(remainingMinutes, "minute", locale)}`
      : durationUnit(hours, "hour", locale);
  }
  const days = Math.floor(hours / 24);
  const remainingHours = hours % 24;
  return remainingHours
    ? `${durationUnit(days, "day", locale)}${locale === "ja" ? "" : " "}${durationUnit(remainingHours, "hour", locale)}`
    : durationUnit(days, "day", locale);
}
