import type { WorkSummary } from "../types";

export function uniqueRelatedWorks(items: WorkSummary[] | undefined, currentID: string): WorkSummary[] {
  const seen = new Set<string>();
  return (items || []).filter((item) => {
    const id = String(item.candidate_id || "").trim();
    if (!id || id === currentID || seen.has(id)) return false;
    seen.add(id);
    return true;
  });
}

export function relatedGroupLabel(items: WorkSummary[]): string {
  if (!items.length) return "";
  const labels = items.map((item) => String(item.relation_label || "").trim());
  const first = labels[0] || "";
  return first && labels.every((label) => label === first) ? first : "";
}
