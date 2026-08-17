import type { ProgressSavePayload } from "../types";

const LEGACY_STORAGE_KEY = "bmanga.v2.progressPending.v2";
const ENTRY_STORAGE_PREFIX = "bmanga.v2.progressPending.v3.entry.";
const MAX_ENTRIES = 80;

export interface PendingProgressEntry {
  entryID: string;
  semanticKey: string;
  sequence: number;
  workIdentityID: string;
  queuedAt: string;
  payload: ProgressSavePayload;
}

export interface PendingProgressMutationResult extends PendingProgressEntry {
  logicalPendingCount: number;
}

interface LegacyPendingProgressEntry {
  key?: unknown;
  sequence?: unknown;
  workIdentityID?: unknown;
  queuedAt?: unknown;
  payload?: unknown;
}

let lastSequence = 0;
let lastTimestamp = 0;

function storage(): Storage | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

function validPayload(value: unknown): value is ProgressSavePayload {
  if (!value || typeof value !== "object") return false;
  const payload = value as Partial<ProgressSavePayload>;
  return Boolean(String(payload.candidate_id || "").trim())
    && Number.isSafeInteger(payload.index)
    && Number(payload.index) >= 0
    && Number.isSafeInteger(payload.count)
    && Number(payload.count) >= 0;
}

function manifestKey(payload: ProgressSavePayload): string {
  return String(payload.page_manifest_id || payload.manifest_hash || "legacy");
}

function progressSemanticKey(payload: ProgressSavePayload): string {
  return `${String(payload.candidate_id).trim()}\u0000${manifestKey(payload)}`;
}

function timestampValue(value: unknown): number {
  const parsed = Date.parse(String(value || ""));
  return Number.isFinite(parsed) ? parsed : 0;
}

function entryTimestamp(entry: PendingProgressEntry): number {
  return Math.max(timestampValue(entry.queuedAt), timestampValue(entry.payload.updated_at));
}

function entryStorageKey(entryID: string): string {
  return `${ENTRY_STORAGE_PREFIX}${encodeURIComponent(entryID)}`;
}

function normalizeEntry(value: unknown): PendingProgressEntry | null {
  if (!value || typeof value !== "object") return null;
  const record = value as Partial<PendingProgressEntry>;
  const entryID = String(record.entryID || "").trim();
  const sequence = Number(record.sequence);
  if (!entryID || !Number.isSafeInteger(sequence) || sequence <= 0 || !validPayload(record.payload)) return null;
  return {
    entryID,
    semanticKey: progressSemanticKey(record.payload),
    sequence,
    workIdentityID: String(record.workIdentityID || ""),
    queuedAt: String(record.queuedAt || ""),
    payload: record.payload,
  };
}

function storageKeys(target: Storage): string[] {
  const keys: string[] = [];
  try {
    for (let index = 0; index < target.length; index += 1) {
      const key = target.key(index);
      if (key) keys.push(key);
    }
  } catch {
    return [];
  }
  return keys;
}

function stableHash(value: string): string {
  let hash = 0x811c9dc5;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193);
  }
  return (hash >>> 0).toString(36);
}

function legacyEntryID(entry: LegacyPendingProgressEntry, payload: ProgressSavePayload, sequence: number): string {
  return `legacy-${sequence.toString(36)}-${stableHash([
    String(entry.key || ""),
    progressSemanticKey(payload),
    String(entry.queuedAt || ""),
    String(payload.updated_at || ""),
    String(payload.index),
  ].join("\u0001"))}`;
}

function migrateLegacyEntries(target: Storage): void {
  let raw = "";
  try {
    raw = target.getItem(LEGACY_STORAGE_KEY) || "";
  } catch {
    return;
  }
  if (!raw) return;

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw) as unknown;
  } catch {
    try {
      target.removeItem(LEGACY_STORAGE_KEY);
    } catch {
      // Ignore unavailable storage; the malformed legacy value is harmless.
    }
    return;
  }
  if (!Array.isArray(parsed)) {
    try {
      target.removeItem(LEGACY_STORAGE_KEY);
    } catch {
      // Ignore unavailable storage; the malformed legacy value is harmless.
    }
    return;
  }

  let migratedAll = true;
  for (const value of parsed.slice(-MAX_ENTRIES)) {
    if (!value || typeof value !== "object") continue;
    const legacy = value as LegacyPendingProgressEntry;
    const sequence = Number(legacy.sequence);
    if (!Number.isSafeInteger(sequence) || sequence <= 0 || !validPayload(legacy.payload)) continue;
    const entryID = legacyEntryID(legacy, legacy.payload, sequence);
    const inferredQueuedAt = new Date(Math.max(0, Math.floor(sequence / 1000))).toISOString();
    const entry: PendingProgressEntry = {
      entryID,
      semanticKey: progressSemanticKey(legacy.payload),
      sequence,
      workIdentityID: String(legacy.workIdentityID || ""),
      queuedAt: String(legacy.queuedAt || legacy.payload.updated_at || inferredQueuedAt),
      payload: { ...legacy.payload },
    };
    try {
      const key = entryStorageKey(entryID);
      if (!target.getItem(key)) target.setItem(key, JSON.stringify(entry));
    } catch {
      migratedAll = false;
    }
  }

  if (migratedAll) {
    try {
      target.removeItem(LEGACY_STORAGE_KEY);
    } catch {
      // A later read can retry removing the legacy array.
    }
  }
}

function readStoredEntries(target: Storage): PendingProgressEntry[] {
  const entries: PendingProgressEntry[] = [];
  for (const key of storageKeys(target)) {
    if (!key.startsWith(ENTRY_STORAGE_PREFIX)) continue;
    try {
      const entry = normalizeEntry(JSON.parse(target.getItem(key) || "null") as unknown);
      if (entry && key === entryStorageKey(entry.entryID)) {
        entries.push(entry);
      } else {
        target.removeItem(key);
      }
    } catch {
      // One damaged entry must not hide the rest of the queue.
      try {
        target.removeItem(key);
      } catch {
        // Cleanup is best-effort when storage becomes unavailable mid-read.
      }
    }
  }
  return entries;
}

function readRawEntries(target = storage()): PendingProgressEntry[] {
  if (!target) return [];
  migrateLegacyEntries(target);
  const entries = readStoredEntries(target);
  if (entries.length <= MAX_ENTRIES) return entries;
  return compactStoredEntries(target, entries);
}

function compareEntries(left: PendingProgressEntry, right: PendingProgressEntry): number {
  if (left.sequence !== right.sequence) return left.sequence - right.sequence;
  const timestampDifference = entryTimestamp(left) - entryTimestamp(right);
  if (timestampDifference !== 0) return timestampDifference;
  return left.entryID.localeCompare(right.entryID);
}

function isStrictlyOlder(left: PendingProgressEntry, right: PendingProgressEntry): boolean {
  if (left.sequence !== right.sequence) return left.sequence < right.sequence;
  const leftTimestamp = entryTimestamp(left);
  const rightTimestamp = entryTimestamp(right);
  if (leftTimestamp !== rightTimestamp) return leftTimestamp < rightTimestamp;
  // Equal clocks may be concurrent writes. A random entry ID is not temporal evidence.
  return false;
}

function foldedEntries(entries: PendingProgressEntry[]): PendingProgressEntry[] {
  const latestBySemantic = new Map<string, PendingProgressEntry>();
  for (const entry of entries) {
    const current = latestBySemantic.get(entry.semanticKey);
    if (!current || compareEntries(current, entry) < 0) latestBySemantic.set(entry.semanticKey, entry);
  }
  return [...latestBySemantic.values()].sort(compareEntries);
}

function removeEntry(target: Storage, entryID: string): void {
  try {
    target.removeItem(entryStorageKey(entryID));
  } catch {
    // Storage cleanup is best-effort; a later flush can retry.
  }
}

function supersededEntryIDs(sourceEntries: PendingProgressEntry[]): Set<string> {
  const groups = new Map<string, PendingProgressEntry[]>();
  for (const entry of sourceEntries) {
    const group = groups.get(entry.semanticKey) || [];
    group.push(entry);
    groups.set(entry.semanticKey, group);
  }
  const removed = new Set<string>();
  for (const group of groups.values()) {
    for (const entry of group) {
      if (group.some((other) => other.entryID !== entry.entryID && isStrictlyOlder(entry, other))) {
        removed.add(entry.entryID);
      }
    }
  }
  return removed;
}

function compactStoredEntries(target: Storage, sourceEntries: PendingProgressEntry[]): PendingProgressEntry[] {
  const removed = supersededEntryIDs(sourceEntries);
  const current = sourceEntries.filter((entry) => !removed.has(entry.entryID));
  const overflow = [...current]
    .sort(compareEntries)
    .slice(0, Math.max(0, current.length - MAX_ENTRIES));
  for (const entry of overflow) removed.add(entry.entryID);
  for (const entryID of removed) removeEntry(target, entryID);
  return current.filter((entry) => !removed.has(entry.entryID));
}

function randomEntryID(): string {
  try {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") return crypto.randomUUID();
  } catch {
    // Fall through to a collision-resistant local ID.
  }
  return `pp-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}-${lastSequence.toString(36)}`;
}

function takeProgressTimestamp(entries: PendingProgressEntry[]): string {
  const persistedTimestamp = entries.reduce((latest, entry) => Math.max(latest, entryTimestamp(entry)), 0);
  const now = Math.max(Date.now(), lastTimestamp + 1, persistedTimestamp + 1);
  lastTimestamp = now;
  return new Date(now).toISOString();
}

export function nextProgressTimestamp(): string {
  return takeProgressTimestamp(readRawEntries());
}

export function pendingProgressEntries(): PendingProgressEntry[] {
  return foldedEntries(readRawEntries());
}

export function pendingProgressCount(): number {
  return pendingProgressEntries().length;
}

export function enqueuePendingProgress(payload: ProgressSavePayload, workIdentityID = ""): PendingProgressMutationResult {
  const target = storage();
  const entries = readRawEntries(target);
  const persistedSequence = entries.reduce((latest, current) => Math.max(latest, current.sequence), 0);
  const sequence = Math.max(Date.now() * 1000, lastSequence + 1, persistedSequence + 1);
  lastSequence = sequence;
  const entry: PendingProgressEntry = {
    entryID: randomEntryID(),
    semanticKey: progressSemanticKey(payload),
    sequence,
    workIdentityID,
    queuedAt: takeProgressTimestamp(entries),
    payload: { ...payload },
  };
  if (!target) return { ...entry, logicalPendingCount: 0 };
  try {
    target.setItem(entryStorageKey(entry.entryID), JSON.stringify(entry));
    const retained = compactStoredEntries(target, [...entries, entry]);
    return { ...entry, logicalPendingCount: foldedEntries(retained).length };
  } catch {
    // Storage can be unavailable in private browsing or under quota pressure.
    return { ...entry, logicalPendingCount: foldedEntries(entries).length };
  }
}

export function acknowledgePendingProgress(entryID: string): number {
  const target = storage();
  if (!target) return 0;
  const entries = readRawEntries(target);
  const confirmed = entries.find((entry) => entry.entryID === entryID);
  if (!confirmed) return foldedEntries(entries).length;
  const removed = new Set<string>();
  for (const entry of entries) {
    if (entry.entryID === confirmed.entryID || (
      entry.semanticKey === confirmed.semanticKey
      && isStrictlyOlder(entry, confirmed)
    )) {
      removeEntry(target, entry.entryID);
      removed.add(entry.entryID);
    }
  }
  return foldedEntries(entries.filter((entry) => !removed.has(entry.entryID))).length;
}

export function discardPendingProgressForCandidate(candidateID: string): number {
  const target = storage();
  if (!target) return 0;
  const entries = readRawEntries(target);
  if (!candidateID) return foldedEntries(entries).length;
  const retained: PendingProgressEntry[] = [];
  for (const entry of entries) {
    if (entry.payload.candidate_id === candidateID) removeEntry(target, entry.entryID);
    else retained.push(entry);
  }
  return foldedEntries(retained).length;
}

export function newestPendingProgress(candidateID: string): PendingProgressEntry | null {
  const matches = pendingProgressEntries().filter((entry) => entry.payload.candidate_id === candidateID);
  return matches.sort((left, right) => compareEntries(right, left))[0] || null;
}
