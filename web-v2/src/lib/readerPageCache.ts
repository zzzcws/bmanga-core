export class ReaderPageCacheTimeoutError extends Error {
  constructor(message = "Reader page request timed out") {
    super(message);
    this.name = "ReaderPageCacheTimeoutError";
  }
}

type ReaderPageCacheEntry<T> = {
  controller: AbortController;
  lastUsed: number;
  promise: Promise<T>;
  rejectPending: (reason: unknown) => void;
  settled: boolean;
  timeout?: ReturnType<typeof setTimeout>;
  value?: T;
};

export type ReaderPageCacheOptions<T> = {
  dispose?: (value: T) => void;
  maxEntries?: number;
  timeoutMs?: number;
};

function abortError(): Error {
  if (typeof DOMException !== "undefined") return new DOMException("Reader page request was cancelled", "AbortError");
  const error = new Error("Reader page request was cancelled");
  error.name = "AbortError";
  return error;
}

export class ReaderPageCache<T> {
  private readonly entries = new Map<string, ReaderPageCacheEntry<T>>();
  private readonly dispose: (value: T) => void;
  private readonly loader: (key: string, signal: AbortSignal) => Promise<T>;
  private readonly maxEntries: number;
  private readonly timeoutMs: number;
  private clock = 0;
  private pinnedKey = "";

  constructor(
    loader: (key: string, signal: AbortSignal) => Promise<T>,
    options: ReaderPageCacheOptions<T> = {},
  ) {
    this.loader = loader;
    this.dispose = options.dispose ?? (() => undefined);
    this.maxEntries = Math.max(1, Math.round(options.maxEntries ?? 3));
    this.timeoutMs = Math.max(0, Math.round(options.timeoutMs ?? 25_000));
  }

  get size(): number {
    return this.entries.size;
  }

  has(key: string): boolean {
    return this.entries.has(key);
  }

  load(key: string): Promise<T> {
    const normalizedKey = String(key || "").trim();
    if (!normalizedKey) return Promise.reject(new Error("Reader page cache key is required"));
    const existing = this.entries.get(normalizedKey);
    if (existing) {
      this.touch(existing);
      return existing.promise;
    }

    const controller = new AbortController();
    let entry: ReaderPageCacheEntry<T>;
    let rejectPending: (reason: unknown) => void = () => undefined;
    let lateValueDisposed = false;
    const cancellation = new Promise<never>((_resolve, reject) => {
      rejectPending = reject;
    });
    const loaderPromise = Promise.resolve().then(() => this.loader(normalizedKey, controller.signal));
    const disposeLateValue = (value: T) => {
      if (lateValueDisposed) return;
      lateValueDisposed = true;
      this.disposeSafely(value);
    };
    const promise = Promise.race([loaderPromise, cancellation])
      .then((value) => {
        this.clearEntryTimeout(entry);
        if (this.entries.get(normalizedKey) !== entry) {
          disposeLateValue(value);
          throw abortError();
        }
        entry.settled = true;
        entry.value = value;
        this.touch(entry);
        this.trim();
        return value;
      }, (reason: unknown) => {
        this.clearEntryTimeout(entry);
        if (this.entries.get(normalizedKey) === entry) this.entries.delete(normalizedKey);
        throw reason;
      });
    entry = {
      controller,
      lastUsed: ++this.clock,
      promise,
      rejectPending,
      settled: false,
    };
    void loaderPromise.then((value) => {
      if (this.entries.get(normalizedKey) !== entry) disposeLateValue(value);
    }, () => undefined);
    this.entries.set(normalizedKey, entry);
    if (this.timeoutMs > 0) {
      entry.timeout = setTimeout(() => {
        entry.timeout = undefined;
        if (this.entries.get(normalizedKey) === entry) this.entries.delete(normalizedKey);
        controller.abort();
        rejectPending(new ReaderPageCacheTimeoutError());
      }, this.timeoutMs);
    }
    this.trim();
    return promise;
  }

  invalidate(key: string): void {
    const normalizedKey = String(key || "").trim();
    if (!normalizedKey) return;
    this.evict(normalizedKey);
  }

  setPinnedKey(key: string): void {
    this.pinnedKey = String(key || "").trim();
    const entry = this.entries.get(this.pinnedKey);
    if (entry) this.touch(entry);
    this.trim();
  }

  clear(): void {
    this.pinnedKey = "";
    const keys = [...this.entries.keys()];
    for (const key of keys) this.evict(key);
  }

  private touch(entry: ReaderPageCacheEntry<T>): void {
    entry.lastUsed = ++this.clock;
  }

  private trim(): void {
    while (this.entries.size > this.maxEntries) {
      let oldestKey = "";
      let oldest = Number.POSITIVE_INFINITY;
      for (const [key, entry] of this.entries) {
        if (key === this.pinnedKey || entry.lastUsed >= oldest) continue;
        oldestKey = key;
        oldest = entry.lastUsed;
      }
      if (!oldestKey) return;
      this.evict(oldestKey);
    }
  }

  private evict(key: string): void {
    const entry = this.entries.get(key);
    if (!entry) return;
    this.entries.delete(key);
    this.clearEntryTimeout(entry);
    if (entry.settled && entry.value !== undefined) this.disposeSafely(entry.value);
    else {
      entry.controller.abort();
      entry.rejectPending(abortError());
    }
  }

  private clearEntryTimeout(entry: ReaderPageCacheEntry<T>): void {
    if (entry.timeout === undefined) return;
    clearTimeout(entry.timeout);
    entry.timeout = undefined;
  }

  private disposeSafely(value: T): void {
    try {
      this.dispose(value);
    } catch {
      // Cleanup must never turn an otherwise successful page transition into a failure.
    }
  }
}
