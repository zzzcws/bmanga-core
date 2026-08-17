import type { DiscoverMode } from "./browseRoute";
import type {
  DiscoverPayload,
  DiscoverQuery,
  DiscoverResponse,
  DiscoverStats,
} from "../types";

export const DISCOVER_RANDOM_LIMIT = 12;
export const DISCOVER_HISTORY_LIMIT = 6;

export interface DiscoverRequestPlan {
  includeAuxiliary: boolean;
  query: DiscoverQuery;
}

function emptyDiscoverStats(): DiscoverStats {
  return {
    favorite_count: 0,
    history_count: 0,
    liked_count: 0,
    rated_count: 0,
    reread_count: 0,
  };
}

export function planDiscoverRequest(
  mode: DiscoverMode,
  dataRevision: number,
  auxiliaryRevision: number | null,
): DiscoverRequestPlan {
  const includeAuxiliary = auxiliaryRevision !== dataRevision;
  return {
    includeAuxiliary,
    query: {
      randomMode: mode,
      randomLimit: DISCOVER_RANDOM_LIMIT,
      historyLimit: DISCOVER_HISTORY_LIMIT,
      includeHistory: includeAuxiliary ? undefined : 0,
      includeStats: includeAuxiliary ? undefined : 0,
      lean: true,
    },
  };
}

export function hasDiscoverAuxiliary(payload: DiscoverPayload): boolean {
  return Array.isArray(payload.history) && payload.stats !== undefined && payload.stats !== null;
}

export function mergeDiscoverPayload(
  current: DiscoverResponse | null,
  payload: DiscoverPayload,
): DiscoverResponse {
  const history = payload.history ?? current?.history ?? [];
  return {
    ...payload,
    total: payload.history === undefined
      ? payload.random_items.length + history.length
      : payload.total,
    history,
    stats: payload.stats ?? current?.stats ?? emptyDiscoverStats(),
  };
}
