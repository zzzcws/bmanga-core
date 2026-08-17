export type ReaderPreparationCacheOptions = {
  maxEntries?: number;
  timeoutMs?: number;
  ttlMs?: number;
  now?: () => number;
};

type ReaderPreparationEntry<T> = {
  controller: AbortController;
  expiresAt: number;
  promise: Promise<T>;
  settled: boolean;
  timeout?: ReturnType<typeof setTimeout>;
};

function abortError(message = "Reader preparation was cancelled"): Error {
  if (typeof DOMException !== "undefined") return new DOMException(message, "AbortError");
  const error = new Error(message);
  error.name = "AbortError";
  return error;
}

/**
 * A deliberately small cache for manifests requested while a detail panel is open.
 * In-flight requests are shared with the reader; successful values only live long
 * enough to bridge the detail-to-reader transition.
 */
export class ReaderPreparationCache<T> {
  private readonly entries = new Map<string, ReaderPreparationEntry<T>>();
  private readonly loader: (key: string, signal: AbortSignal) => Promise<T>;
  private readonly maxEntries: number;
  private readonly now: () => number;
  private readonly timeoutMs: number;
  private readonly ttlMs: number;

  constructor(
    loader: (key: string, signal: AbortSignal) => Promise<T>,
    options: ReaderPreparationCacheOptions = {},
  ) {
    this.loader = loader;
    this.maxEntries = Math.max(1, Math.round(options.maxEntries ?? 1));
    this.timeoutMs = Math.max(0, Math.round(options.timeoutMs ?? 20_000));
    this.ttlMs = Math.max(0, Math.round(options.ttlMs ?? 12_000));
    this.now = options.now ?? Date.now;
  }

  get size(): number {
    return this.entries.size;
  }

  has(key: string): boolean {
    const normalizedKey = String(key || "").trim();
    const entry = this.entries.get(normalizedKey);
    if (!entry) return false;
    if (!entry.settled || entry.expiresAt > this.now()) return true;
    this.evict(normalizedKey);
    return false;
  }

  load(key: string): Promise<T> {
    const normalizedKey = String(key || "").trim();
    if (!normalizedKey) return Promise.reject(new Error("Reader preparation cache key is required"));
    const existing = this.entries.get(normalizedKey);
    if (existing && (!existing.settled || existing.expiresAt > this.now())) return existing.promise;
    if (existing) this.evict(normalizedKey);

    const controller = new AbortController();
    let entry: ReaderPreparationEntry<T>;
    const promise = Promise.resolve()
      .then(() => this.loader(normalizedKey, controller.signal))
      .then((value) => {
        this.clearEntryTimeout(entry);
        if (this.entries.get(normalizedKey) !== entry) throw abortError();
        entry.settled = true;
        entry.expiresAt = this.now() + this.ttlMs;
        return value;
      }, (reason: unknown) => {
        this.clearEntryTimeout(entry);
        if (this.entries.get(normalizedKey) === entry) this.entries.delete(normalizedKey);
        throw reason;
      });
    entry = {
      controller,
      expiresAt: Number.POSITIVE_INFINITY,
      promise,
      settled: false,
    };
    this.entries.set(normalizedKey, entry);
    if (this.timeoutMs > 0) {
      entry.timeout = setTimeout(() => {
        entry.timeout = undefined;
        if (this.entries.get(normalizedKey) !== entry) return;
        this.entries.delete(normalizedKey);
        controller.abort();
      }, this.timeoutMs);
    }
    this.trim(normalizedKey);
    return promise;
  }

  invalidate(key: string): void {
    const normalizedKey = String(key || "").trim();
    if (normalizedKey) this.evict(normalizedKey);
  }

  clear(): void {
    for (const key of [...this.entries.keys()]) this.evict(key);
  }

  private trim(currentKey: string): void {
    while (this.entries.size > this.maxEntries) {
      const oldestKey = this.entries.keys().next().value as string | undefined;
      if (!oldestKey) return;
      if (oldestKey === currentKey && this.entries.size > 1) {
        const nextKey = [...this.entries.keys()].find((key) => key !== currentKey);
        if (!nextKey) return;
        this.evict(nextKey);
      } else {
        this.evict(oldestKey);
      }
    }
  }

  private evict(key: string): void {
    const entry = this.entries.get(key);
    if (!entry) return;
    this.entries.delete(key);
    this.clearEntryTimeout(entry);
    if (!entry.settled) entry.controller.abort();
  }

  private clearEntryTimeout(entry: ReaderPreparationEntry<T>): void {
    if (entry.timeout === undefined) return;
    clearTimeout(entry.timeout);
    entry.timeout = undefined;
  }
}

export function waitForReaderPreparation<T>(promise: Promise<T>, signal: AbortSignal): Promise<T> {
  if (signal.aborted) return Promise.reject(abortError());
  return new Promise<T>((resolve, reject) => {
    const abort = () => {
      cleanup();
      reject(abortError());
    };
    const cleanup = () => signal.removeEventListener("abort", abort);
    signal.addEventListener("abort", abort, { once: true });
    promise.then((value) => {
      cleanup();
      resolve(value);
    }, (reason: unknown) => {
      cleanup();
      reject(reason);
    });
  });
}

export type ReaderWarmProgress = {
  index?: unknown;
  page_manifest_id?: unknown;
  manifest_hash?: unknown;
};

export type ReaderWarmManifest = {
  count: number;
  readable: boolean;
  page_manifest_id?: unknown;
  manifest_hash?: unknown;
};

function manifestsMatch(left: ReaderWarmManifest, right: ReaderWarmProgress): boolean {
  const leftID = String(left.page_manifest_id || "");
  const rightID = String(right.page_manifest_id || "");
  const leftHash = String(left.manifest_hash || "");
  const rightHash = String(right.manifest_hash || "");
  let compared = false;
  if (leftID && rightID) {
    compared = true;
    if (leftID !== rightID) return false;
  }
  if (leftHash && rightHash) {
    compared = true;
    if (leftHash !== rightHash) return false;
  }
  return compared;
}

/** Returns null when a resume page cannot be proven to belong to this manifest. */
export function safeReaderWarmIndex(
  pages: ReaderWarmManifest,
  progress: ReaderWarmProgress | null,
  requestedIndex?: number,
): number | null {
  if (!pages.readable || pages.count < 1) return null;
  if (requestedIndex !== undefined) return Math.max(0, Math.min(pages.count - 1, Math.round(requestedIndex)));
  if (!progress) return 0;
  if (!manifestsMatch(pages, progress)) return null;
  const parsed = Number(progress.index);
  const index = Number.isFinite(parsed) ? Math.round(parsed) : 0;
  return Math.max(0, Math.min(pages.count - 1, index));
}
