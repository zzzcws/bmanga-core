import { itemCreatorLabel } from "./catalogPresentation.ts";
import type { WorkDetailResponse } from "../types";

export const WORK_METADATA_OVERRIDE_FIELDS = ["title", "creator", "series", "language"] as const;
export type WorkMetadataOverrideField = typeof WORK_METADATA_OVERRIDE_FIELDS[number];
export type WorkMetadataOverrideValues = Record<WorkMetadataOverrideField, string>;

function uniqueMetadataLabels(values: unknown[]): string[] {
  const seen = new Set<string>();
  const labels: string[] = [];
  for (const value of values) {
    const label = String(value || "").trim();
    const key = label.toLocaleLowerCase();
    if (!label || seen.has(key)) continue;
    seen.add(key);
    labels.push(label);
  }
  return labels;
}

export function workMetadataOverrideValue(detail: WorkDetailResponse, field: string): string {
  const overrides = detail.work.metadata_overrides;
  if (!overrides || typeof overrides !== "object" || Array.isArray(overrides)) return "";
  const entry = (overrides as Record<string, unknown>)[field];
  if (!entry || typeof entry !== "object" || Array.isArray(entry)) return "";
  return String((entry as Record<string, unknown>).field_value || "").trim();
}

export function workCreatorNames(detail: WorkDetailResponse): string[] {
  const creator = workMetadataOverrideValue(detail, "creator");
  if (creator) return [creator];
  const author = workMetadataOverrideValue(detail, "author");
  const circle = workMetadataOverrideValue(detail, "circle");
  if (author || circle) {
    if (author && circle && author.toLocaleLowerCase() !== circle.toLocaleLowerCase()) return [`${circle} (${author})`];
    return [author || circle];
  }
  const structured = uniqueMetadataLabels(detail.creators?.map((creator) => creator.creator_display || creator.name) || []);
  if (structured.length) return structured;
  const displayCreator = itemCreatorLabel(detail.work);
  if (displayCreator) return [displayCreator];
  return uniqueMetadataLabels(detail.title_hints?.creators || []);
}

export function workLanguageNames(detail: WorkDetailResponse): string[] {
  const overridden = workMetadataOverrideValue(detail, "language");
  if (overridden) return uniqueMetadataLabels(overridden.split(/[,，;；/]+/u));
  return uniqueMetadataLabels([
    detail.work.metadata_source_language,
    detail.work.language,
  ]);
}

export function workMetadataOverrideValues(detail: WorkDetailResponse): WorkMetadataOverrideValues {
  return {
    title: workMetadataOverrideValue(detail, "title"),
    creator: workMetadataOverrideValue(detail, "creator"),
    series: workMetadataOverrideValue(detail, "series"),
    language: workMetadataOverrideValue(detail, "language"),
  };
}

export function workMetadataOriginalValues(detail: WorkDetailResponse): WorkMetadataOverrideValues {
  const titleOverride = workMetadataOverrideValue(detail, "title");
  const structuredCreators = uniqueMetadataLabels(
    detail.creators?.map((creator) => creator.creator_display || creator.name) || [],
  );
  const creatorFallback = uniqueMetadataLabels(detail.title_hints?.creators || []);
  const structuredSeries = uniqueMetadataLabels([
    ...(detail.series?.map((series) => series.series_title) || []),
    ...(detail.doujin_series?.map((series) => series.series_title) || []),
  ]);
  return {
    title: String(detail.work.metadata_source_title || (titleOverride ? "" : detail.work.title) || "").trim(),
    creator: (structuredCreators[0] || creatorFallback[0] || "").trim(),
    series: (structuredSeries[0] || String(detail.title_hints?.series || "")).trim(),
    language: String(detail.work.metadata_source_language || detail.work.language || "").trim(),
  };
}

export function workSeriesNames(detail: WorkDetailResponse): string[] {
  const overridden = workMetadataOverrideValue(detail, "series");
  if (overridden) return [overridden];
  const structured = uniqueMetadataLabels([
    ...(detail.series?.map((series) => series.series_title) || []),
    ...(detail.doujin_series?.map((series) => series.series_title) || []),
  ]);
  if (structured.length) return structured;
  return uniqueMetadataLabels([detail.title_hints?.series]);
}

export function workTranslationNames(detail: WorkDetailResponse): string[] {
  const aggregate = String(detail.work.translation_sources || "").split(/[,，;；]+/u);
  return uniqueMetadataLabels([
    ...(detail.translations?.map((translation) => translation.translation_group) || []),
    ...aggregate,
  ]);
}
