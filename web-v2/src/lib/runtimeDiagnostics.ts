export interface RuntimeDiagnosticsLite {
  ok: boolean;
  version: string;
  uptime_seconds: number;
  database: {
    status: "healthy" | "unavailable";
  };
  cache: {
    file_count: number;
    bytes: number;
    scan_errors: number;
    complete: boolean;
  };
}

function finiteNonNegative(value: number): number {
  return Number.isFinite(value) && value > 0 ? value : 0;
}

export function formatRuntimeBytes(value: number): string {
  const bytes = finiteNonNegative(value);
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let amount = bytes;
  let unitIndex = 0;
  while (amount >= 1024 && unitIndex < units.length - 1) {
    amount /= 1024;
    unitIndex += 1;
  }
  const digits = unitIndex === 0 || amount >= 100 ? 0 : amount >= 10 ? 1 : 2;
  return `${amount.toFixed(digits)} ${units[unitIndex]}`;
}

export function formatRuntimeUptime(value: number): string {
  const seconds = Math.floor(finiteNonNegative(value));
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} 分钟`;
  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  if (hours < 24) return remainingMinutes ? `${hours} 小时 ${remainingMinutes} 分钟` : `${hours} 小时`;
  const days = Math.floor(hours / 24);
  const remainingHours = hours % 24;
  return remainingHours ? `${days} 天 ${remainingHours} 小时` : `${days} 天`;
}
