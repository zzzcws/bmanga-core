import type { TargetType, UserMark, UserMarkSavePayload } from "../types";

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

export const READ_STATUS_OPTIONS: readonly PersonalMarkOption[] = [
  { value: "unread", label: "未读" },
  { value: "reading", label: "在读" },
  { value: "completed", label: "已读" },
  { value: "abandoned", label: "搁置" },
];

export const PERSONAL_RATING_OPTIONS: readonly PersonalMarkOption[] = [
  { value: null, label: "清除", accessibleLabel: "清除个人评分，恢复为未评分" },
  { value: 0, label: "0", accessibleLabel: "个人评分 0 分" },
  ...Array.from({ length: 10 }, (_, index) => ({
    value: index + 1,
    label: String(index + 1),
    accessibleLabel: `个人评分 ${index + 1} 分`,
  })),
];

export const REREAD_PRIORITY_OPTIONS: readonly PersonalMarkOption[] = [
  { value: 0, label: "无", accessibleLabel: "不设置重看优先级" },
  { value: 1, label: "低", accessibleLabel: "低重看优先级" },
  { value: 2, label: "中", accessibleLabel: "中重看优先级" },
  { value: 3, label: "高", accessibleLabel: "高重看优先级" },
];

export const QUALITY_RATING_OPTIONS: readonly PersonalMarkOption[] = [
  { value: null, label: "清除", accessibleLabel: "清除质量评分，恢复为未评分" },
  ...Array.from({ length: 5 }, (_, index) => ({
    value: index + 1,
    label: String(index + 1),
    accessibleLabel: `质量评分 ${index + 1} 分`,
  })),
];

const validReadStatuses = new Set(["unread", "reading", "completed", "abandoned"]);

export function scoreControlValue(value: number | null): string {
  return value === null ? CLEAR_SCORE_CONTROL_VALUE : String(value);
}

export function parseScoreControlValue(value: string, minimum: number, maximum: number): number | null {
  const normalized = value.trim();
  if (normalized === CLEAR_SCORE_CONTROL_VALUE) return null;
  if (!/^(?:0|[1-9][0-9]*)$/.test(normalized)) {
    throw new RangeError("评分必须是明确的整数或清除值");
  }
  const parsed = Number(normalized);
  if (!Number.isSafeInteger(parsed) || parsed < minimum || parsed > maximum) {
    throw new RangeError(`评分必须在 ${minimum} 到 ${maximum} 之间`);
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

function assertIntegerRange(value: PersonalMarkValue, minimum: number, maximum: number, field: string): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < minimum || value > maximum) {
    throw new RangeError(`${field} out of range`);
  }
  return value;
}

export function createUserMarkPatch(
  targetType: TargetType,
  targetID: string,
  field: PersonalMarkField,
  value: PersonalMarkValue,
  clientUpdatedAt = "",
): UserMarkSavePayload {
  const normalizedTargetID = targetID.trim();
  if (!normalizedTargetID) throw new Error("个人标记缺少目标 ID");
  if (!personalMarkFieldsForTarget(targetType).includes(field)) {
    throw new Error(`系列标记不支持 ${field}`);
  }

  const payload: UserMarkSavePayload = { target_type: targetType, target_id: normalizedTargetID };
  if (clientUpdatedAt.trim()) payload.client_updated_at = clientUpdatedAt.trim();
  switch (field) {
    case "read_status":
      if (typeof value !== "string" || !validReadStatuses.has(value)) {
        throw new Error("无效的阅读状态");
      }
      payload.read_status = value;
      break;
    case "personal_rating":
      payload.personal_rating = value === null ? null : assertIntegerRange(value, 0, 10, field);
      break;
    case "reread_priority":
      payload.reread_priority = assertIntegerRange(value, 0, 3, field);
      break;
    case "translation_quality":
      payload.translation_quality = value === null ? null : assertIntegerRange(value, 1, 5, field);
      break;
    case "image_quality":
      payload.image_quality = value === null ? null : assertIntegerRange(value, 1, 5, field);
      break;
  }
  return payload;
}
