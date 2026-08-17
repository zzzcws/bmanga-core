export type PaginationToken = number | "ellipsis";

export function clampPaginationPage(value: number, totalPages: number): number {
  const safeTotal = Number.isFinite(totalPages) ? Math.max(1, Math.floor(totalPages)) : 1;
  if (!Number.isFinite(value)) return 1;
  return Math.min(safeTotal, Math.max(1, Math.floor(value)));
}

export function paginationTokens(currentPage: number, totalPages: number): PaginationToken[] {
  const total = Number.isFinite(totalPages) ? Math.max(1, Math.floor(totalPages)) : 1;
  const current = clampPaginationPage(currentPage, total);
  if (total <= 7) return Array.from({ length: total }, (_, index) => index + 1);
  if (current <= 4) return [1, 2, 3, 4, 5, "ellipsis", total];
  if (current >= total - 3) return [1, "ellipsis", total - 4, total - 3, total - 2, total - 1, total];
  return [1, "ellipsis", current - 1, current, current + 1, "ellipsis", total];
}
