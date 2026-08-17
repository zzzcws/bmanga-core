import type { TargetType, UserMark, UserMarkSavePayload } from "../types";
import { DEFAULT_LOCALE, intlLocale, localizeMessage, type Locale } from "./locale.ts";

export const CLEAR_SCORE_CONTROL_VALUE = "__clear__";

export type PersonalMarkField =
  | "read_status"
  | "personal_rating"
  | "reread_priority"
  | "translation_quality"
  | "image_quality";

export type PersonalMarkValue = string | number | null;

export interface PersonalMarkOption {
  value: PersonalMarkValue;
  label: string;
  accessibleLabel?: string;
}

function scoreText(value: number, locale: Locale): string {
  return new Intl.NumberFormat(intlLocale(locale), { useGrouping: false }).format(value);
}

export function readStatusOptions(locale: Locale = DEFAULT_LOCALE): readonly PersonalMarkOption[] {
  return [
    { value: "unread", label: localizeMessage({ "zh-CN": "未读", en: "Unread", ja: "未読" }, locale) },
    { value: "reading", label: localizeMessage({ "zh-CN": "在读", en: "Reading", ja: "読書中" }, locale) },
    { value: "completed", label: localizeMessage({ "zh-CN": "已读", en: "Completed", ja: "読了" }, locale) },
    { value: "abandoned", label: localizeMessage({ "zh-CN": "搁置", en: "On hold", ja: "保留" }, locale) },
  ];
}

export function personalRatingOptions(locale: Locale = DEFAULT_LOCALE): readonly PersonalMarkOption[] {
  return [
    {
      value: null,
      label: localizeMessage({ "zh-CN": "清除", en: "Clear", ja: "クリア" }, locale),
      accessibleLabel: localizeMessage({
        "zh-CN": "清除个人评分，恢复为未评分",
        en: "Clear the personal rating and return to unrated",
        ja: "個人評価をクリアして未評価に戻す",
      }, locale),
    },
    ...Array.from({ length: 11 }, (_, index) => {
      const score = scoreText(index, locale);
      return {
        value: index,
        label: score,
        accessibleLabel: localizeMessage({
          "zh-CN": "个人评分 {score} 分",
          en: "Personal rating: {score}",
          ja: "個人評価：{score}点",
        }, locale, { score }),
      };
    }),
  ];
}

export function rereadPriorityOptions(locale: Locale = DEFAULT_LOCALE): readonly PersonalMarkOption[] {
  const priorities = [
    { value: 0, label: { "zh-CN": "无", en: "None", ja: "なし" }, accessibleLabel: {
      "zh-CN": "不设置重看优先级",
      en: "Do not set a reread priority",
      ja: "再読優先度を設定しない",
    } },
    { value: 1, label: { "zh-CN": "低", en: "Low", ja: "低" }, accessibleLabel: {
      "zh-CN": "低重看优先级", en: "Low reread priority", ja: "再読優先度：低",
    } },
    { value: 2, label: { "zh-CN": "中", en: "Medium", ja: "中" }, accessibleLabel: {
      "zh-CN": "中重看优先级", en: "Medium reread priority", ja: "再読優先度：中",
    } },
    { value: 3, label: { "zh-CN": "高", en: "High", ja: "高" }, accessibleLabel: {
      "zh-CN": "高重看优先级", en: "High reread priority", ja: "再読優先度：高",
    } },
  ] as const;
  return priorities.map((option) => ({
    value: option.value,
    label: localizeMessage(option.label, locale),
    accessibleLabel: localizeMessage(option.accessibleLabel, locale),
  }));
}

export function qualityRatingOptions(locale: Locale = DEFAULT_LOCALE): readonly PersonalMarkOption[] {
  return [
    {
      value: null,
      label: localizeMessage({ "zh-CN": "清除", en: "Clear", ja: "クリア" }, locale),
      accessibleLabel: localizeMessage({
        "zh-CN": "清除质量评分，恢复为未评分",
        en: "Clear the quality rating and return to unrated",
        ja: "品質評価をクリアして未評価に戻す",
      }, locale),
    },
    ...Array.from({ length: 5 }, (_, index) => {
      const value = index + 1;
      const score = scoreText(value, locale);
      return {
        value,
        label: score,
        accessibleLabel: localizeMessage({
          "zh-CN": "质量评分 {score} 分",
          en: "Quality rating: {score}",
          ja: "品質評価：{score}点",
        }, locale, { score }),
      };
    }),
  ];
}

export const READ_STATUS_OPTIONS = readStatusOptions();
export const PERSONAL_RATING_OPTIONS = personalRatingOptions();
export const REREAD_PRIORITY_OPTIONS = rereadPriorityOptions();
export const QUALITY_RATING_OPTIONS = qualityRatingOptions();

const validReadStatuses = new Set(["unread", "reading", "completed", "abandoned"]);

export function scoreControlValue(value: number | null): string {
  return value === null ? CLEAR_SCORE_CONTROL_VALUE : String(value);
}

export function parseScoreControlValue(
  value: string,
  minimum: number,
  maximum: number,
  locale: Locale = DEFAULT_LOCALE,
): number | null {
  const normalized = value.trim();
  if (normalized === CLEAR_SCORE_CONTROL_VALUE) return null;
  if (!/^(?:0|[1-9][0-9]*)$/.test(normalized)) {
    throw new RangeError(localizeMessage({
      "zh-CN": "评分必须是明确的整数或清除值",
      en: "The rating must be an explicit integer or the clear value",
      ja: "評価は明示的な整数またはクリア値である必要があります",
    }, locale));
  }
  const parsed = Number(normalized);
  if (!Number.isSafeInteger(parsed) || parsed < minimum || parsed > maximum) {
    throw new RangeError(localizeMessage({
      "zh-CN": "评分必须在 {minimum} 到 {maximum} 之间",
      en: "The rating must be between {minimum} and {maximum}",
      ja: "評価は {minimum} から {maximum} の範囲で指定してください",
    }, locale, {
      minimum: scoreText(minimum, locale),
      maximum: scoreText(maximum, locale),
    }));
  }
  return parsed;
}

export function personalMarkFieldsForTarget(targetType: TargetType): readonly PersonalMarkField[] {
  const common: PersonalMarkField[] = ["read_status", "personal_rating", "reread_priority"];
  return targetType === "work"
    ? [...common, "translation_quality", "image_quality"]
    : common;
}

export function currentPersonalMarkValue(mark: UserMark | null, field: PersonalMarkField): PersonalMarkValue {
  switch (field) {
    case "read_status":
      return mark?.read_status || "unread";
    case "personal_rating":
      return mark?.personal_rating ?? null;
    case "reread_priority":
      return mark?.reread_priority ?? 0;
    case "translation_quality":
      return mark?.translation_quality ?? null;
    case "image_quality":
      return mark?.image_quality ?? null;
  }
}

function assertIntegerRange(
  value: PersonalMarkValue,
  minimum: number,
  maximum: number,
  field: string,
  locale: Locale,
): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < minimum || value > maximum) {
    throw new RangeError(localizeMessage({
      "zh-CN": "{field} 超出允许范围",
      en: "{field} is out of range",
      ja: "{field} は許可範囲外です",
    }, locale, { field }));
  }
  return value;
}

export function createUserMarkPatch(
  targetType: TargetType,
  targetID: string,
  field: PersonalMarkField,
  value: PersonalMarkValue,
  clientUpdatedAt = "",
  locale: Locale = DEFAULT_LOCALE,
): UserMarkSavePayload {
  const normalizedTargetID = targetID.trim();
  if (!normalizedTargetID) throw new Error(localizeMessage({
    "zh-CN": "个人标记缺少目标 ID",
    en: "The personal mark is missing a target ID",
    ja: "個人マークの対象 ID がありません",
  }, locale));
  if (!personalMarkFieldsForTarget(targetType).includes(field)) {
    throw new Error(localizeMessage({
      "zh-CN": "系列标记不支持 {field}",
      en: "Series marks do not support {field}",
      ja: "シリーズマークでは {field} を使用できません",
    }, locale, { field }));
  }

  const payload: UserMarkSavePayload = { target_type: targetType, target_id: normalizedTargetID };
  if (clientUpdatedAt.trim()) payload.client_updated_at = clientUpdatedAt.trim();
  switch (field) {
    case "read_status":
      if (typeof value !== "string" || !validReadStatuses.has(value)) {
        throw new Error(localizeMessage({
          "zh-CN": "无效的阅读状态",
          en: "Invalid reading status",
          ja: "無効な読書状態です",
        }, locale));
      }
      payload.read_status = value;
      break;
    case "personal_rating":
      payload.personal_rating = value === null ? null : assertIntegerRange(value, 0, 10, field, locale);
      break;
    case "reread_priority":
      payload.reread_priority = assertIntegerRange(value, 0, 3, field, locale);
      break;
    case "translation_quality":
      payload.translation_quality = value === null ? null : assertIntegerRange(value, 1, 5, field, locale);
      break;
    case "image_quality":
      payload.image_quality = value === null ? null : assertIntegerRange(value, 1, 5, field, locale);
      break;
  }
  return payload;
}
