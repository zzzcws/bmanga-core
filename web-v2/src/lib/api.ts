import type {
  BrowseQuery,
  DiscoverPayload,
  DiscoverQuery,
  CorrectionSavePayload,
  CorrectionSaveResponse,
  ContinueTargetResponse,
  MetadataOverrideResponse,
  MetadataOverrideSavePayload,
  PagesResponse,
  ProgressResponse,
  ProgressSavePayload,
  ProgressSaveResponse,
  RandomWorkResponse,
  ReadingHistoryQuery,
  ReadingHistoryResponse,
  SeriesDetailResponse,
  SeriesProgressResponse,
  SeriesResponse,
  ShelfResponse,
  TargetType,
  UserMarkResponse,
  UserMarkSavePayload,
  UserMarkSaveResponse,
  WorkDetailResponse,
  WorksResponse,
} from "../types";
import { READER_IMAGE_MAX_DIMENSION } from "./readerImage.ts";
import type {
  LibraryPageStateBatchSaveResponse,
  LibraryPageStateMutation,
  LibraryPageStateResponse,
  LibraryPageStateSaveResponse,
} from "./libraryPageState.ts";

export type ApiQueryValue = string | number | boolean | null | undefined;
export type ApiQuery = Record<string, ApiQueryValue | readonly ApiQueryValue[]>;

export interface ApiRequestOptions {
  signal?: AbortSignal;
  params?: ApiQuery;
  headers?: HeadersInit;
  keepalive?: boolean;
}

interface ApiErrorInit {
  status: number;
  statusText?: string;
  url: string;
  serverMessage?: string;
  payload?: unknown;
  cause?: unknown;
}

export class ApiError extends Error {
  readonly status: number;
  readonly statusText: string;
  readonly url: string;
  readonly serverMessage: string;
  readonly payload: unknown;

  constructor(message: string, init: ApiErrorInit) {
    super(message, init.cause === undefined ? undefined : { cause: init.cause });
    this.name = "ApiError";
    this.status = init.status;
    this.statusText = init.statusText ?? "";
    this.url = init.url;
    this.serverMessage = init.serverMessage ?? "";
    this.payload = init.payload;
  }
}

const localOrigin = "http://bmanga.invalid";

function appendQuery(url: URL, params?: ApiQuery): void {
  if (!params) return;
  for (const [key, rawValue] of Object.entries(params)) {
    const values = Array.isArray(rawValue) ? rawValue : [rawValue];
    for (const value of values) {
      if (value === undefined || value === null || value === "") continue;
      url.searchParams.append(key, String(value));
    }
  }
}

function requestUrl(path: string, params?: ApiQuery): string {
  const url = new URL(path, localOrigin);
  if (url.origin !== localOrigin) {
    throw new ApiError("仅允许访问 bmanga 的同源接口。", {
      status: 0,
      url: path,
    });
  }
  appendQuery(url, params);
  return `${url.pathname}${url.search}${url.hash}`;
}

function readCookie(name: string): string {
  if (typeof document === "undefined") return "";
  const prefix = `${name}=`;
  for (const part of document.cookie.split(";")) {
    const value = part.trim();
    if (!value.startsWith(prefix)) continue;
    try {
      return decodeURIComponent(value.slice(prefix.length));
    } catch {
      return value.slice(prefix.length);
    }
  }
  return "";
}

function hasChineseText(value: string): boolean {
  return /[\u3400-\u9fff]/u.test(value);
}

function translatedServerMessage(value: string): string {
  const lower = value.toLowerCase();
  const translations: Array<[string, string]> = [
    ["missing candidate_id", "请求缺少作品 ID。"],
    ["missing id", "请求缺少必要的 ID。"],
    ["work not found", "没有找到这部作品。"],
    ["series not found", "没有找到这个系列。"],
    ["unknown endpoint", "当前版本尚未提供这个功能。"],
    ["method not allowed", "当前操作方式不受支持。"],
    ["work identity missing", "作品身份信息不完整，暂时无法保存。"],
    ["page manifest missing", "页面清单不可用，请重新打开作品。"],
    ["invalid image data", "图片数据无效或文件已经损坏。"],
    ["invalid json", "请求内容格式不正确。"],
    ["query is required", "请输入搜索关键词。"],
    ["local identity not found", "本地系列已经变化，请重新搜索后确认。"],
  ];
  for (const [needle, message] of translations) {
    if (lower.includes(needle)) return message;
  }
  return "";
}

function localizedErrorMessage(status: number, serverMessage: string, rawBody: string): string {
  const message = serverMessage.trim();
  if (message && hasChineseText(message)) return message;

  const translated = translatedServerMessage(message || rawBody);
  if (translated) return translated;

  const lower = rawBody.toLowerCase();
  if (lower.includes("<!doctype html") || lower.includes("<html")) {
    return `服务器返回了网页错误（HTTP ${status}），请刷新后重试。`;
  }

  switch (status) {
    case 400:
      return "请求参数不正确，请检查后重试。";
    case 401:
      return "登录已过期，请重新登录。";
    case 403:
      return "当前请求未通过安全校验，请刷新页面后重试。";
    case 404:
      return "没有找到请求的内容。";
    case 409:
      return "内容状态已经变化，请刷新后重试。";
    case 413:
      return "提交的内容过大，请缩小后重试。";
    case 415:
      return "提交的文件格式不受支持。";
    case 429:
      return "请求过于频繁，请稍后再试。";
    default:
      return status >= 500
        ? "bmanga 服务暂时无法完成请求，请稍后重试。"
        : `请求失败（HTTP ${status}）。`;
  }
}

function parsePayload(rawBody: string, contentType: string): unknown {
  if (!rawBody) return undefined;
  if (contentType.includes("json") || rawBody.trimStart().startsWith("{")) {
    try {
      return JSON.parse(rawBody) as unknown;
    } catch {
      return rawBody;
    }
  }
  return rawBody;
}

function serverMessageFrom(payload: unknown): string {
  if (!payload || typeof payload !== "object") return typeof payload === "string" ? payload : "";
  const record = payload as Record<string, unknown>;
  for (const key of ["error", "message", "detail"]) {
    if (typeof record[key] === "string") return record[key];
  }
  return "";
}

export function isAbortError(error: unknown): boolean {
  return Boolean(error && typeof error === "object" && "name" in error && error.name === "AbortError");
}

export function apiErrorText(error: unknown): string {
  if (error instanceof ApiError) return error.message;
  if (isAbortError(error)) return "请求已取消。";
  if (error instanceof Error && hasChineseText(error.message)) return error.message;
  return "无法连接到 bmanga，请检查网络后重试。";
}

const readRetryDelayMs = 140;

function retryableReadError(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 0 || error.status >= 500);
}

function waitForReadRetry(signal?: AbortSignal): Promise<void> {
  if (signal?.aborted) {
    const aborted = new ApiError("请求已取消。", { status: 0, url: "" });
    aborted.name = "AbortError";
    return Promise.reject(aborted);
  }
  return new Promise((resolve, reject) => {
    const timer = globalThis.setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, readRetryDelayMs);
    const onAbort = () => {
      globalThis.clearTimeout(timer);
      const aborted = new ApiError("请求已取消。", { status: 0, url: "" });
      aborted.name = "AbortError";
      reject(aborted);
    };
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

async function request<T>(method: "GET" | "POST", path: string, body: unknown, options: ApiRequestOptions): Promise<T> {
  const url = requestUrl(path, options.params);
  const headers = new Headers(options.headers);
  headers.set("Accept", "application/json");

  let serializedBody: string | undefined;
  if (method !== "GET") {
    if (body !== undefined) {
      serializedBody = JSON.stringify(body);
      headers.set("Content-Type", "application/json");
    }
    headers.set("X-Bmanga-Write", "same-origin");
    const token = readCookie("bmanga_write_token");
    if (token) headers.set("X-Bmanga-Write-Token", token);
  }

  let response: Response;
  try {
    response = await fetch(url, {
      method,
      headers,
      body: serializedBody,
      signal: options.signal,
      credentials: "same-origin",
      keepalive: options.keepalive ?? (serializedBody !== undefined && serializedBody.length < 60_000),
    });
  } catch (error) {
    if (isAbortError(error)) {
      const aborted = new ApiError("请求已取消。", { status: 0, url, cause: error });
      aborted.name = "AbortError";
      throw aborted;
    }
    throw new ApiError("无法连接到 bmanga，请检查网络后重试。", {
      status: 0,
      url,
      cause: error,
    });
  }

  const rawBody = response.status === 204 ? "" : await response.text();
  const payload = parsePayload(rawBody, response.headers.get("content-type") ?? "");
  if (!response.ok) {
    const serverMessage = serverMessageFrom(payload);
    throw new ApiError(localizedErrorMessage(response.status, serverMessage, rawBody), {
      status: response.status,
      statusText: response.statusText,
      url,
      serverMessage,
      payload,
    });
  }
  return payload as T;
}

export async function apiGet<T>(path: string, options: ApiRequestOptions = {}): Promise<T> {
  try {
    return await request<T>("GET", path, undefined, options);
  } catch (error) {
    if (!retryableReadError(error) || isAbortError(error) || options.signal?.aborted) throw error;
    await waitForReadRetry(options.signal);
    return request<T>("GET", path, undefined, options);
  }
}

export function apiPost<T>(path: string, body: unknown, options: ApiRequestOptions = {}): Promise<T> {
  return request<T>("POST", path, body, options);
}

function objectParams(source: object): ApiQuery {
  const params: ApiQuery = {};
  for (const [key, value] of Object.entries(source as Record<string, unknown>)) {
    if (
      value === undefined ||
      value === null ||
      typeof value === "string" ||
      typeof value === "number" ||
      typeof value === "boolean" ||
      Array.isArray(value)
    ) {
      params[key] = value as ApiQuery[string];
    }
  }
  return params;
}

function withParams(options: ApiRequestOptions, query: object): ApiRequestOptions {
  return {
    ...options,
    params: {
      ...options.params,
      ...objectParams(query),
    },
  };
}

export function coverUrl(id: string, size?: number): string {
  return requestUrl("/cover", {
    id,
    size: size && size > 0 ? Math.min(1600, Math.round(size)) : undefined,
  });
}

export function pageUrl(
  id: string,
  index: number,
  manifest?: string,
  max?: number,
  preserveSource = false,
): string {
  return requestUrl("/page", {
    id,
    index: Math.max(0, Math.round(index)),
    manifest,
    max: max && max > 0 ? Math.min(READER_IMAGE_MAX_DIMENSION, Math.round(max)) : undefined,
    quality: preserveSource ? "source" : undefined,
  });
}

export function getShelf(query: BrowseQuery = {}, options: ApiRequestOptions = {}): Promise<ShelfResponse> {
  return apiGet<ShelfResponse>("/api/shelf", withParams(options, query));
}

export function getWorks(query: BrowseQuery = {}, options: ApiRequestOptions = {}): Promise<WorksResponse> {
  return apiGet<WorksResponse>("/api/works", withParams(options, query));
}

export function getSeries(query: BrowseQuery = {}, options: ApiRequestOptions = {}): Promise<SeriesResponse> {
  return apiGet<SeriesResponse>("/api/series", withParams(options, query));
}

export function getWork(id: string, options: ApiRequestOptions = {}): Promise<WorkDetailResponse> {
  return apiGet<WorkDetailResponse>("/api/work", withParams(options, { id }));
}

export function getMetadataOverrides(
  targetID: string,
  options: ApiRequestOptions = {},
): Promise<MetadataOverrideResponse> {
  return apiGet<MetadataOverrideResponse>(
    "/api/metadata-overrides",
    withParams(options, { target_type: "work", target_id: targetID }),
  );
}

export function saveMetadataOverride(
  payload: MetadataOverrideSavePayload,
  options: ApiRequestOptions = {},
): Promise<MetadataOverrideResponse> {
  return apiPost<MetadataOverrideResponse>("/api/metadata-overrides", payload, options);
}

export function getSeriesDetail(id: string, options: ApiRequestOptions = {}): Promise<SeriesDetailResponse> {
  return apiGet<SeriesDetailResponse>("/api/series-detail", withParams(options, { id }));
}

export function getSeriesProgress(id: string, options: ApiRequestOptions = {}): Promise<SeriesProgressResponse> {
  return apiGet<SeriesProgressResponse>("/api/series-progress", withParams(options, { id }));
}

export function getReadingHistory(
  query: ReadingHistoryQuery = {},
  options: ApiRequestOptions = {},
): Promise<ReadingHistoryResponse> {
  return apiGet<ReadingHistoryResponse>("/api/reading-history", withParams(options, query));
}

export function getContinueTarget(options: ApiRequestOptions = {}): Promise<ContinueTargetResponse> {
  return apiGet<ContinueTargetResponse>("/api/continue-target", options);
}

export function getDiscover(query: DiscoverQuery = {}, options: ApiRequestOptions = {}): Promise<DiscoverPayload> {
  return apiGet<DiscoverPayload>("/api/discover", withParams(options, query));
}

export function getRandomWork(query: DiscoverQuery = {}, options: ApiRequestOptions = {}): Promise<RandomWorkResponse> {
  return apiGet<RandomWorkResponse>("/api/random-work", withParams(options, query));
}

export function getProgress(id: string, options: ApiRequestOptions = {}): Promise<ProgressResponse> {
  return apiGet<ProgressResponse>("/api/progress", withParams(options, { id }));
}

export function getLibraryPageState(options: ApiRequestOptions = {}): Promise<LibraryPageStateResponse> {
  return apiGet<LibraryPageStateResponse>("/api/library-page-state", options);
}

export function saveLibraryPageState(
  payload: LibraryPageStateMutation,
  options: ApiRequestOptions = {},
): Promise<LibraryPageStateSaveResponse> {
  return apiPost<LibraryPageStateSaveResponse>("/api/library-page-state", { state: payload }, options);
}

export function saveLibraryPageStates(
  payloads: LibraryPageStateMutation[],
  options: ApiRequestOptions = {},
): Promise<LibraryPageStateBatchSaveResponse> {
  return apiPost<LibraryPageStateBatchSaveResponse>("/api/library-page-state", { states: payloads }, options);
}

export function getPages(id: string, options: ApiRequestOptions = {}): Promise<PagesResponse> {
  return apiGet<PagesResponse>("/api/pages", withParams(options, { id }));
}

export function saveProgress(payload: ProgressSavePayload, options: ApiRequestOptions = {}): Promise<ProgressSaveResponse> {
  return apiPost<ProgressSaveResponse>("/api/progress", payload, options);
}

export function getUserMark(
  targetType: TargetType,
  targetID: string,
  options: ApiRequestOptions = {},
): Promise<UserMarkResponse> {
  return apiGet<UserMarkResponse>("/api/user-mark", withParams(options, { target_type: targetType, target_id: targetID }));
}

export function saveUserMark(
  payload: UserMarkSavePayload,
  options: ApiRequestOptions = {},
): Promise<UserMarkSaveResponse> {
  return apiPost<UserMarkSaveResponse>("/api/user-mark", payload, options);
}

export function saveCorrection(
  payload: CorrectionSavePayload,
  options: ApiRequestOptions = {},
): Promise<CorrectionSaveResponse> {
  return apiPost<CorrectionSaveResponse>("/api/corrections", payload, options);
}
