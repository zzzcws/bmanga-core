import {
  parseLibraryPageScopes,
  sanitizeBrowseRoute,
  type CatalogMode,
  type CatalogSort,
  type LibraryPageScopes,
} from "./browseRoute.ts";

export const LIBRARY_PAGE_STATE_CACHE_KEY = "bmanga.v2.libraryPageState.v1";
export const LIBRARY_PAGE_STATE_PENDING_KEY = "bmanga.v2.libraryPageState.pending.v1";

const CATALOG_MODES = ["all", "doujin", "series"] as const satisfies readonly CatalogMode[];
const CATALOG_SORTS = new Set<CatalogSort>(["added_desc", "title_asc", "pages_desc"]);

export interface LibraryPagePosition {
  offset: number;
  updated_at: string;
  event_id: string;
}

export type LibraryPagePositions = Record<CatalogMode, LibraryPagePosition>;
export type LibraryPageInitialOffsets = Partial<Record<CatalogMode, number>>;

export interface LibraryPageState {
  version: 1;
  sort: CatalogSort;
  sort_updated_at: string;
  sort_event_id: string;
  positions: LibraryPagePositions;
  updated_at: string;
}

export type LibraryPageCanonicalState = LibraryPageState;

export interface LibraryPageStateMutation {
  sort: CatalogSort;
  mode: CatalogMode;
  offset: number;
  updated_at: string;
  event_id: string;
  initial_offsets?: LibraryPageInitialOffsets;
}

export interface LibraryPageStateResponse {
  state: LibraryPageState | null;
  updated_at?: string;
}

export interface LibraryPageStateSaveResponse extends LibraryPageStateResponse {
  ok: boolean;
  stored: boolean;
  acknowledged_event_ids?: string[];
}

export interface LibraryPageStateBatchSaveResponse extends LibraryPageStateSaveResponse {
  acknowledged_event_ids: string[];
}

export interface LibraryPageMutationOptions {
  offset?: number;
  updatedAt?: string;
  eventID?: string;
}

export interface ExplicitLibraryPageParameters {
  offset: boolean;
  sort: boolean;
}

export interface LibraryPageEventClock {
  updated_at: string;
  event_id: string;
}

export type LibraryPageTimestampSource = LibraryPageState | LibraryPageStateMutation | string | null | undefined;

let lastTimestamp = 0;
let lastEventSequence = 0;
let serverClockOffsetMs = 0;

function recordValue(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

function timestampText(value: unknown): string {
  if (typeof value !== "string") return "";
  const text = value.trim();
  return text && Number.isFinite(Date.parse(text)) ? text : "";
}

function timestampValue(value: unknown): number {
  const parsed = Date.parse(typeof value === "string" ? value : "");
  return Number.isFinite(parsed) ? parsed : 0;
}

function eventIDText(value: unknown): string {
  if (typeof value !== "string") return "";
  const text = value.trim();
  return /^[\x21-\x7e]{1,100}$/.test(text) ? text : "";
}

function catalogMode(value: unknown): CatalogMode | null {
  return typeof value === "string" && (CATALOG_MODES as readonly string[]).includes(value)
    ? value as CatalogMode
    : null;
}

function catalogSort(value: unknown): CatalogSort | null {
  return typeof value === "string" && CATALOG_SORTS.has(value as CatalogSort)
    ? value as CatalogSort
    : null;
}

function alignedOffset(value: unknown): number | null {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) return null;
  return sanitizeBrowseRoute({ view: "library", offset: value }).offset;
}

function parsePosition(value: unknown): LibraryPagePosition | null {
  const source = recordValue(value);
  if (!source) return null;
  const offset = alignedOffset(source.offset);
  const updatedAt = timestampText(source.updated_at);
  const eventID = eventIDText(source.event_id);
  if (offset === null || !updatedAt || !eventID) return null;
  return { offset, updated_at: updatedAt, event_id: eventID };
}

function parseInitialOffsets(value: unknown): LibraryPageInitialOffsets | null {
  const source = recordValue(value);
  if (!source) return null;
  const offsets: LibraryPageInitialOffsets = {};
  for (const mode of CATALOG_MODES) {
    if (!(mode in source)) continue;
    const offset = alignedOffset(source[mode]);
    if (offset === null) return null;
    offsets[mode] = offset;
  }
  return offsets;
}

export function parseLibraryPageState(value: unknown): LibraryPageState | null {
  const source = recordValue(value);
  if (!source || source.version !== 1) return null;
  const sort = catalogSort(source.sort);
  const sortUpdatedAt = timestampText(source.sort_updated_at);
  const sortEventID = eventIDText(source.sort_event_id);
  const updatedAt = timestampText(source.updated_at);
  const rawPositions = recordValue(source.positions);
  if (!sort || !sortUpdatedAt || !sortEventID || !updatedAt || !rawPositions) return null;

  const positions = {} as LibraryPagePositions;
  for (const mode of CATALOG_MODES) {
    const position = parsePosition(rawPositions[mode]);
    if (!position) return null;
    positions[mode] = position;
  }
  return {
    version: 1,
    sort,
    sort_updated_at: sortUpdatedAt,
    sort_event_id: sortEventID,
    positions,
    updated_at: updatedAt,
  };
}

export function parseLibraryPageMutation(value: unknown): LibraryPageStateMutation | null {
  const source = recordValue(value);
  if (!source) return null;
  const sort = catalogSort(source.sort);
  const mode = catalogMode(source.mode);
  const offset = alignedOffset(source.offset);
  const updatedAt = timestampText(source.updated_at);
  const eventID = eventIDText(source.event_id);
  if (!sort || !mode || offset === null || !updatedAt || !eventID) return null;
  const mutation: LibraryPageStateMutation = {
    sort,
    mode,
    offset,
    updated_at: updatedAt,
    event_id: eventID,
  };
  if (source.initial_offsets !== undefined) {
    const initialOffsets = parseInitialOffsets(source.initial_offsets);
    if (!initialOffsets) return null;
    mutation.initial_offsets = initialOffsets;
  }
  return mutation;
}

export function libraryPageScopesFromState(value: unknown): LibraryPageScopes | null {
  const state = parseLibraryPageState(value);
  if (!state) return null;
  return {
    sort: state.sort,
    offsets: {
      all: state.positions.all.offset,
      doujin: state.positions.doujin.offset,
      series: state.positions.series.offset,
    },
  };
}

function stateTimestamps(state: LibraryPageState): number[] {
  return [
    timestampValue(state.updated_at),
    timestampValue(state.sort_updated_at),
    ...CATALOG_MODES.map((mode) => timestampValue(state.positions[mode].updated_at)),
  ];
}

function timestampsFromSource(source: LibraryPageTimestampSource): number[] {
  if (!source) return [];
  if (typeof source === "string") return [timestampValue(source)];
  if ("positions" in source) return stateTimestamps(source);
  return [timestampValue(source.updated_at)];
}

function browserStorage(target?: Storage | null): Storage | null {
  if (target !== undefined) return target;
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

function readStorageValue(target: Storage | null, key: string): unknown {
  if (!target) return null;
  try {
    const raw = target.getItem(key);
    return raw ? JSON.parse(raw) as unknown : null;
  } catch {
    return null;
  }
}

export function readCachedLibraryPageState(target?: Storage | null): LibraryPageState | null {
  return parseLibraryPageState(readStorageValue(browserStorage(target), LIBRARY_PAGE_STATE_CACHE_KEY));
}

export function writeCachedLibraryPageState(value: unknown, target?: Storage | null): LibraryPageState | null {
  const state = parseLibraryPageState(value);
  const storage = browserStorage(target);
  if (!state || !storage) return state;
  try {
    storage.setItem(LIBRARY_PAGE_STATE_CACHE_KEY, JSON.stringify(state));
  } catch {
    // Browser storage is an offline enhancement; a server sync must still continue.
  }
  return state;
}

export function clearCachedLibraryPageState(target?: Storage | null): void {
  const storage = browserStorage(target);
  if (!storage) return;
  try {
    storage.removeItem(LIBRARY_PAGE_STATE_CACHE_KEY);
  } catch {
    // Unavailable storage must not block browsing.
  }
}

function compareMutations(left: LibraryPageStateMutation, right: LibraryPageStateMutation): number {
  return compareLibraryPageEvents(left, right);
}

export function compactLibraryPageMutations(items: LibraryPageStateMutation[]): LibraryPageStateMutation[] {
  const ordered = [...items].sort(compareMutations);
  const epochs: Array<{ sort: CatalogSort; retained: Map<CatalogMode, LibraryPageStateMutation> }> = [];
  for (const mutation of ordered) {
    let epoch = epochs.at(-1);
    if (!epoch || epoch.sort !== mutation.sort) {
      epoch = { sort: mutation.sort, retained: new Map() };
      epochs.push(epoch);
    }
    const current = epoch.retained.get(mutation.mode);
    if (!current || compareMutations(mutation, current) > 0) epoch.retained.set(mutation.mode, mutation);
  }
  // The final two sort epochs are sufficient to preserve reset semantics even
  // when the user switches A -> B -> A while offline and the server is still on A.
  return epochs.slice(-2).flatMap((epoch) => [...epoch.retained.values()].sort(compareMutations));
}

function pendingValues(value: unknown): unknown[] {
  if (Array.isArray(value)) return value;
  const record = recordValue(value);
  if (!record) return [];
  if (Array.isArray(record.items)) return record.items;
  return [record];
}

function writePendingMutations(items: LibraryPageStateMutation[], target?: Storage | null): LibraryPageStateMutation[] {
  const compacted = compactLibraryPageMutations(items);
  const storage = browserStorage(target);
  if (!storage) return compacted;
  try {
    if (!compacted.length) storage.removeItem(LIBRARY_PAGE_STATE_PENDING_KEY);
    else storage.setItem(LIBRARY_PAGE_STATE_PENDING_KEY, JSON.stringify({ items: compacted }));
  } catch {
    // The current in-memory mutations may still be sent when storage is unavailable.
  }
  return compacted;
}

export function pendingLibraryPageMutations(target?: Storage | null): LibraryPageStateMutation[] {
  const storage = browserStorage(target);
  const values = pendingValues(readStorageValue(storage, LIBRARY_PAGE_STATE_PENDING_KEY));
  return compactLibraryPageMutations(values.map(parseLibraryPageMutation).filter((item): item is LibraryPageStateMutation => Boolean(item)));
}

export function readPendingLibraryPageMutation(target?: Storage | null): LibraryPageStateMutation | null {
  return pendingLibraryPageMutations(target).at(-1) || null;
}

export function enqueuePendingLibraryPageMutation(value: unknown, target?: Storage | null): LibraryPageStateMutation[] {
  const mutation = parseLibraryPageMutation(value);
  if (!mutation) return pendingLibraryPageMutations(target);
  return writePendingMutations([...pendingLibraryPageMutations(target), mutation], target);
}

export function writePendingLibraryPageMutation(value: unknown, target?: Storage | null): LibraryPageStateMutation | null {
  const mutation = parseLibraryPageMutation(value);
  if (!mutation) return null;
  enqueuePendingLibraryPageMutation(mutation, target);
  return mutation;
}

export function acknowledgePendingLibraryPageMutation(eventID: string, target?: Storage | null): LibraryPageStateMutation[] {
  const normalizedEventID = eventIDText(eventID);
  if (!normalizedEventID) return pendingLibraryPageMutations(target);
  return writePendingMutations(
    pendingLibraryPageMutations(target).filter((mutation) => mutation.event_id !== normalizedEventID),
    target,
  );
}

export function clearPendingLibraryPageMutation(eventID?: string, target?: Storage | null): void {
  if (eventID) {
    acknowledgePendingLibraryPageMutation(eventID, target);
    return;
  }
  writePendingMutations([], target);
}

export function nextLibraryPageTimestamp(...seen: LibraryPageTimestampSource[]): string {
  const cached = readCachedLibraryPageState();
  const pending = pendingLibraryPageMutations();
  const timestamps = [
    ...seen.flatMap(timestampsFromSource),
    ...(cached ? stateTimestamps(cached) : []),
    ...pending.map((mutation) => timestampValue(mutation.updated_at)),
  ];
  const seenTimestamp = timestamps.reduce((latest, current) => Math.max(latest, current), 0);
  const next = Math.max(Date.now() + serverClockOffsetMs, lastTimestamp + 1, seenTimestamp + 1);
  lastTimestamp = next;
  return new Date(next).toISOString();
}

export function rebaseLibraryPageMutation(
  value: unknown,
  serverTime: unknown,
  ...seen: LibraryPageTimestampSource[]
): LibraryPageStateMutation | null {
  const mutation = parseLibraryPageMutation(value);
  const serverTimestamp = timestampValue(serverTime);
  if (!mutation || !serverTimestamp) return null;
  serverClockOffsetMs = serverTimestamp - Date.now();
  const seenTimestamp = seen.flatMap(timestampsFromSource).reduce((latest, current) => Math.max(latest, current), 0);
  const latestAllowed = serverTimestamp + 5 * 60_000;
  const rebasedTimestamp = Math.min(Math.max(serverTimestamp, seenTimestamp + 1), latestAllowed);
  lastTimestamp = rebasedTimestamp;
  return {
    ...mutation,
    updated_at: new Date(rebasedTimestamp).toISOString(),
    event_id: nextLibraryPageEventID(),
  };
}

export function nextLibraryPageEventID(): string {
  try {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") return crypto.randomUUID();
  } catch {
    // Fall through to a collision-resistant local event ID.
  }
  lastEventSequence += 1;
  return `library-page-${Date.now().toString(36)}-${lastEventSequence.toString(36)}-${Math.random().toString(36).slice(2)}`;
}

export function compareLibraryPageEvents(left: LibraryPageEventClock, right: LibraryPageEventClock): number {
  const timeDelta = timestampValue(left.updated_at) - timestampValue(right.updated_at);
  if (timeDelta !== 0) return timeDelta < 0 ? -1 : 1;
  const leftEventID = eventIDText(left.event_id);
  const rightEventID = eventIDText(right.event_id);
  if (leftEventID === rightEventID) return 0;
  return leftEventID < rightEventID ? -1 : 1;
}

export function latestLibraryPageStateEvent(value: unknown): LibraryPageEventClock | null {
  const state = parseLibraryPageState(value);
  if (!state) return null;
  return [
    { updated_at: state.sort_updated_at, event_id: state.sort_event_id },
    ...CATALOG_MODES.map((mode) => ({
      updated_at: state.positions[mode].updated_at,
      event_id: state.positions[mode].event_id,
    })),
  ].reduce((latest, current) => compareLibraryPageEvents(current, latest) > 0 ? current : latest);
}

export function reconcileLibraryPageStates(incomingValue: unknown, currentValue: unknown): LibraryPageState | null {
  const incoming = parseLibraryPageState(incomingValue);
  const current = parseLibraryPageState(currentValue);
  if (!incoming) return current;
  if (!current) return incoming;

  const incomingSortEvent = { updated_at: incoming.sort_updated_at, event_id: incoming.sort_event_id };
  const currentSortEvent = { updated_at: current.sort_updated_at, event_id: current.sort_event_id };
  if (incoming.sort === current.sort && compareLibraryPageEvents(incomingSortEvent, currentSortEvent) === 0) {
    const positions = {} as LibraryPagePositions;
    for (const mode of CATALOG_MODES) {
      positions[mode] = compareLibraryPageEvents(incoming.positions[mode], current.positions[mode]) >= 0
        ? incoming.positions[mode]
        : current.positions[mode];
    }
    const merged: LibraryPageState = { ...incoming, positions };
    return { ...merged, updated_at: latestLibraryPageStateEvent(merged)?.updated_at || merged.updated_at };
  }

  const incomingLatest = latestLibraryPageStateEvent(incoming);
  const currentLatest = latestLibraryPageStateEvent(current);
  if (!incomingLatest || !currentLatest) return incoming;
  return compareLibraryPageEvents(incomingLatest, currentLatest) >= 0 ? incoming : current;
}

export function buildLibraryPageMutation(
  scopes: LibraryPageScopes,
  mode: CatalogMode,
  options: LibraryPageMutationOptions = {},
): LibraryPageStateMutation {
  const safeMode = catalogMode(mode);
  if (!safeMode) throw new TypeError("unsupported library page mode");
  const fallbackSort = catalogSort(scopes?.sort) || "added_desc";
  const safeScopes = parseLibraryPageScopes(JSON.stringify(scopes || {}), fallbackSort);
  const requestedOffset = options.offset === undefined ? safeScopes.offsets[safeMode] || 0 : options.offset;
  const offset = alignedOffset(requestedOffset);
  if (offset === null) throw new TypeError("invalid library page offset");
  const updatedAt = timestampText(options.updatedAt) || nextLibraryPageTimestamp();
  const eventID = eventIDText(options.eventID) || nextLibraryPageEventID();
  const initialOffsets: Required<LibraryPageInitialOffsets> = {
    all: safeScopes.offsets.all || 0,
    doujin: safeScopes.offsets.doujin || 0,
    series: safeScopes.offsets.series || 0,
  };
  initialOffsets[safeMode] = offset;
  return {
    sort: safeScopes.sort,
    mode: safeMode,
    offset,
    updated_at: updatedAt,
    event_id: eventID,
    initial_offsets: initialOffsets,
  };
}

export function explicitLibraryPageParameters(href: string | URL): ExplicitLibraryPageParameters {
  try {
    const url = href instanceof URL ? href : new URL(href, "http://bmanga.invalid");
    return {
      offset: url.searchParams.has("offset"),
      sort: url.searchParams.has("sort"),
    };
  } catch {
    return { offset: false, sort: false };
  }
}

export function hasExplicitLibraryPageParameters(href: string | URL): boolean {
  const explicit = explicitLibraryPageParameters(href);
  return explicit.offset || explicit.sort;
}
