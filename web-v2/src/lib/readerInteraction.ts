export const READER_PREVIOUS_ZONE_END = 0.32;
export const READER_NEXT_ZONE_START = 0.68;

export type ReaderStageClickAction =
  | { type: "navigate"; direction: -1 | 1; revealChrome: false }
  | { type: "chrome"; visible: boolean };

export function readerStageClickAction(ratio: number, chromeVisible: boolean): ReaderStageClickAction {
  const safeRatio = Number.isFinite(ratio) ? ratio : 0.5;
  if (safeRatio < READER_PREVIOUS_ZONE_END) return { type: "navigate", direction: -1, revealChrome: false };
  if (safeRatio > READER_NEXT_ZONE_START) return { type: "navigate", direction: 1, revealChrome: false };
  return { type: "chrome", visible: !chromeVisible };
}

export function shouldRefreshReaderChromeOnPointerMove(chromeVisible: boolean | undefined): boolean {
  return chromeVisible === true;
}

export function readerImageRequestKey(
  candidateID: string,
  manifestID: string,
  requestedIndex: number,
  pageRevision: number,
  imageLoading: boolean,
  ending: boolean,
): string {
  if (!candidateID || !imageLoading || ending) return "";
  return [candidateID, manifestID, requestedIndex, pageRevision].join(":");
}

export function showReaderVisualLoading(
  imageLoading: boolean,
  imageURL: string,
  requestKey: string,
  slowLoadingKey: string,
): boolean {
  return Boolean(imageLoading && (!imageURL || (requestKey && slowLoadingKey === requestKey)));
}

export function requestedSplitPanelForPage(
  targetIndex: number,
  displayedIndex: number,
  requestedIndex: number,
  displayedPanel: 0 | 1,
  pendingPanel: 0 | 1,
  explicitPanel?: 0 | 1,
): 0 | 1 {
  if (explicitPanel !== undefined) return explicitPanel;
  if (targetIndex === requestedIndex) return pendingPanel;
  if (targetIndex === displayedIndex) return displayedPanel;
  return 0;
}
