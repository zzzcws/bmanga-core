import { itemCreatorLabel } from "./catalogPresentation.ts";
import type { WorkDetailResponse } from "../types";

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

function workMetadataOverrideValue(detail: WorkDetailResponse, field: string): string {
  const overrides = detail.work.metadata_overrides;
  if (!overrides || typeof overrides !== "object" || Array.isArray(overrides)) return "";
  const entry = (overrides as Record<string, unknown>)[field];
  if (!entry || typeof entry !== "object" || Array.isArray(entry)) return "";
  return String((entry as Record<string, unknown>).field_value || "").trim();
}

export function workCreatorNames(detail: WorkDetailResponse): string[] {
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
