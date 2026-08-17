import type { UserMarkSavePayload, UserMarkSaveResponse } from "../types";
import { DEFAULT_LOCALE, localizeMessage, type Locale } from "./locale.ts";

export const USER_MARK_PENDING_KEY = "bmanga.userMarkPending.v1";

export interface PendingUserMark {
  key: string;
  payload: UserMarkSavePayload;
  attempts: number;
  updated_at: string;
  last_error?: string;
  last_attempt_at?: string;
}

export interface UserMarkFlushResult {
  sent: Array<{ payload: UserMarkSavePayload; response: UserMarkSaveResponse }>;
  remaining: number;
}

function storage(): Storage | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

export function userMarkPayloadFields(payload: UserMarkSavePayload): string[] {
  return Object.keys(payload)
    .filter((key) => key !== "target_type" && key !== "target_id" && key !== "client_updated_at")
    .sort();
}

export function userMarkPendingKey(payload: UserMarkSavePayload): string {
  return [payload.target_type, payload.target_id, userMarkPayloadFields(payload).join("+")].join("::");
}

function pendingEventID(item: PendingUserMark): string {
  return `${item.key}::event::${String(item.payload.client_updated_at || item.updated_at || "legacy")}`;
}

function validPending(value: unknown): value is PendingUserMark {
  if (!value || typeof value !== "object") return false;
  const item = value as Partial<PendingUserMark>;
  const payload = item.payload as Partial<UserMarkSavePayload> | undefined;
  return Boolean(item.key && payload?.target_type && payload.target_id && userMarkPayloadFields(payload as UserMarkSavePayload).length);
}

export function pendingUserMarks(): PendingUserMark[] {
  const target = storage();
  if (!target) return [];
  try {
    const value = JSON.parse(target.getItem(USER_MARK_PENDING_KEY) || "{}");
    return (Array.isArray(value?.items) ? value.items : []).filter(validPending).slice(-100);
  } catch {
    return [];
  }
}

function writePending(items: PendingUserMark[]): void {
  const target = storage();
  if (!target) return;
  try {
    target.setItem(USER_MARK_PENDING_KEY, JSON.stringify({
      items: items.slice(-100),
      updated_at: new Date().toISOString(),
    }));
  } catch {
    // Private browsing and exhausted quotas must not block a server save.
  }
}

export function queuePendingUserMark(payload: UserMarkSavePayload): UserMarkSavePayload {
  if (!payload.target_type || !payload.target_id || !userMarkPayloadFields(payload).length) return payload;
  const now = new Date().toISOString();
  const queuedPayload = payload.client_updated_at ? { ...payload } : { ...payload, client_updated_at: now };
  const key = userMarkPendingKey(queuedPayload);
  const items = pendingUserMarks().filter((item) => item.key !== key);
  items.push({ key, payload: queuedPayload, attempts: 0, updated_at: now });
  writePending(items);
  return queuedPayload;
}

export function hasPendingUserMark(payload: UserMarkSavePayload): boolean {
  const key = userMarkPendingKey(payload);
  const eventTime = String(payload.client_updated_at || "");
  const fields = userMarkPayloadFields(payload);
  const requested = payload as unknown as Record<string, unknown>;
  return pendingUserMarks().some((item) => item.key === key && (
    eventTime
      ? String(item.payload.client_updated_at || "") === eventTime
      : fields.every((field) => Object.is((item.payload as unknown as Record<string, unknown>)[field], requested[field]))
  ));
}

export function acknowledgePendingUserMark(payload: UserMarkSavePayload): void {
  const key = userMarkPendingKey(payload);
  const eventTime = String(payload.client_updated_at || "");
  writePending(pendingUserMarks().filter((item) => item.key !== key || (
    eventTime
    && String(item.payload.client_updated_at || "")
    && String(item.payload.client_updated_at) !== eventTime
  )));
}

let flushing = false;

export async function flushPendingUserMarks(
  send: (payload: UserMarkSavePayload) => Promise<UserMarkSaveResponse>,
  locale: Locale = DEFAULT_LOCALE,
): Promise<UserMarkFlushResult> {
  if (flushing) return { sent: [], remaining: pendingUserMarks().length };
  const initial = pendingUserMarks();
  if (!initial.length) return { sent: [], remaining: 0 };
  flushing = true;
  const sent: UserMarkFlushResult["sent"] = [];
  const failed: PendingUserMark[] = [];
  try {
    for (const item of initial) {
      try {
        const response = await send(item.payload);
        sent.push({ payload: item.payload, response });
      } catch (reason) {
        failed.push({
          ...item,
          attempts: Number(item.attempts || 0) + 1,
          last_error: reason instanceof Error ? reason.message : localizeMessage({
            "zh-CN": "保存失败",
            en: "Save failed",
            ja: "保存に失敗しました",
          }, locale),
          last_attempt_at: new Date().toISOString(),
        });
      }
    }
    const attempted = new Set(initial.map(pendingEventID));
    const fresh = pendingUserMarks().filter((item) => !attempted.has(pendingEventID(item)));
    const freshKeys = new Set(fresh.map((item) => item.key));
    writePending([...fresh, ...failed.filter((item) => !freshKeys.has(item.key))]);
  } finally {
    flushing = false;
  }
  return { sent, remaining: pendingUserMarks().length };
}
