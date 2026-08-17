export function readerForwardPrefetchIndices(
  currentIndex: number,
  pageCount: number,
  depth: number,
): number[] {
  const current = Math.max(0, Math.round(Number(currentIndex) || 0));
  const count = Math.max(0, Math.round(Number(pageCount) || 0));
  const limit = Math.min(4, Math.max(0, Math.round(Number(depth) || 0)));
  const indices: number[] = [];
  for (let offset = 1; offset <= limit && current + offset < count; offset += 1) {
    indices.push(current + offset);
  }
  return indices;
}
