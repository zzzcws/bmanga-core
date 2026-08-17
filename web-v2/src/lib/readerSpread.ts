import type { ReaderFitMode } from "../types";

export const SPLIT_WIDE_MIN_RATIO = 1.15;

export function splitWideActive(mode: ReaderFitMode, width: number, height: number): boolean {
  return mode === "split-wide" && width > 0 && height > 0 && width / height >= SPLIT_WIDE_MIN_RATIO;
}

export function splitWidePanelStep(
  panel: number,
  direction: -1 | 1,
  active: boolean,
): { handled: boolean; panel: 0 | 1 } {
  const current: 0 | 1 = Number(panel) >= 1 ? 1 : 0;
  if (!active) return { handled: false, panel: current };
  if (direction > 0 && current === 0) return { handled: true, panel: 1 };
  if (direction < 0 && current === 1) return { handled: true, panel: 0 };
  return { handled: false, panel: current };
}
