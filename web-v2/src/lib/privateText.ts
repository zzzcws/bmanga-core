const EXTERNAL_SCHEME = /\bhttps?:\/\/[^\s，。；、,;!?！？"'“”‘’<>]+/giu;
const LOCAL_SCHEME = /\b(?:file|smb):\/\/[^\r\n\t，。；、,;!?！？"'“”‘’<>|·]+/giu;
const WINDOWS_PATH = /(?:[a-z]:[\\/]|\\\\)[^\r\n\t，。；、,;!?！？"'“”‘’<>|·]+/giu;
const POSIX_PATH = /(^|[\s"'“‘（(\[【{：:=＝,，;；])\/(?!\/|\s)[^\r\n\t，。；、,;!?！？"'“”‘’<>|·]+/giu;

const OPEN_BRACKETS: Record<string, string> = {
  "(": ")",
  "（": "）",
  "[": "]",
  "【": "】",
  "{": "}",
};

function localPathLength(value: string): number {
  const depths = new Map<string, number>();
  const closingToOpening = new Map(Object.entries(OPEN_BRACKETS).map(([opening, closing]) => [closing, opening]));
  for (let index = 0; index < value.length; index += 1) {
    const character = value[index]!;
    if (OPEN_BRACKETS[character]) {
      depths.set(character, (depths.get(character) || 0) + 1);
      continue;
    }
    const opening = closingToOpening.get(character);
    if (!opening) continue;
    const depth = depths.get(opening) || 0;
    if (depth <= 0) return index;
    depths.set(opening, depth - 1);
  }
  return value.length;
}

function redactLocalPath(value: string): string {
  const length = localPathLength(value);
  return `本地路径${value.slice(length)}`;
}

export function sanitizePrivateText(value: unknown, fallback = "", maxLength = 420): string {
  const text = String(value || fallback).trim();
  if (!text) return fallback;
  return text
    .replace(EXTERNAL_SCHEME, "外部地址")
    .replace(LOCAL_SCHEME, redactLocalPath)
    .replace(WINDOWS_PATH, redactLocalPath)
    .replace(POSIX_PATH, (match, prefix: string) => `${prefix}${redactLocalPath(match.slice(prefix.length))}`)
    .slice(0, maxLength);
}
