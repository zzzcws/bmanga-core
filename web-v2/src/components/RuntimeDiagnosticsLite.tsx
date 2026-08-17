import { useEffect, useState } from "react";

import { useI18n, type LocalizedText } from "../i18n";
import { apiErrorText, apiGet, isAbortError } from "../lib/api";
import {
  formatRuntimeBytes,
  type RuntimeDiagnosticsLite as RuntimeDiagnosticsLiteResponse,
} from "../lib/runtimeDiagnostics";
import "./RuntimeDiagnosticsLite.css";

const runtimeDiagnosticsRequestTimeoutMs = 5_000;

const copy = {
  timeout: { "zh-CN": "运行状态读取超时，请稍后重试。", en: "Runtime status timed out. Please try again later.", ja: "実行状態の読み込みがタイムアウトしました。しばらくしてから再試行してください。" },
  kicker: { "zh-CN": "LOCAL DIAGNOSTICS", en: "LOCAL DIAGNOSTICS", ja: "ローカル診断" },
  title: { "zh-CN": "运行状态", en: "Runtime status", ja: "実行状態" },
  description: {
    "zh-CN": "只读取本服务、数据库与应用缓存的聚合状态；不会探测外网、运行系统命令或执行维护操作。",
    en: "Reads only aggregate status for this service, its database, and application cache; it does not probe external networks, run system commands, or perform maintenance.",
    ja: "このサービス、データベース、アプリキャッシュの集約状態だけを読み取ります。外部ネットワークの調査、システムコマンド、保守処理は実行しません。",
  },
  loading: { "zh-CN": "正在读取本地状态…", en: "Reading local status…", ja: "ローカル状態を読み込んでいます…" },
  unavailable: { "zh-CN": "运行状态暂时不可用：{error}", en: "Runtime status is temporarily unavailable: {error}", ja: "実行状態を一時的に取得できません：{error}" },
  retry: { "zh-CN": "重新读取", en: "Retry", ja: "再読み込み" },
  version: { "zh-CN": "版本", en: "Version", ja: "バージョン" },
  uptime: { "zh-CN": "已运行", en: "Uptime", ja: "稼働時間" },
  database: { "zh-CN": "数据库", en: "Database", ja: "データベース" },
  healthy: { "zh-CN": "正常", en: "Healthy", ja: "正常" },
  databaseUnavailable: { "zh-CN": "不可用", en: "Unavailable", ja: "利用不可" },
  cacheFiles: { "zh-CN": "缓存文件", en: "Cache files", ja: "キャッシュファイル" },
  cacheSize: { "zh-CN": "缓存大小", en: "Cache size", ja: "キャッシュサイズ" },
  cacheScan: { "zh-CN": "缓存扫描", en: "Cache scan", ja: "キャッシュスキャン" },
  complete: { "zh-CN": "完整", en: "Complete", ja: "完了" },
  partial: { "zh-CN": "部分完成 · {count} 个错误", en: "Partial · {count} errors", ja: "一部完了 · エラー {count} 件" },
  seconds: { "zh-CN": "{count} 秒", en: "{count} seconds", ja: "{count} 秒" },
  minutes: { "zh-CN": "{count} 分钟", en: "{count} minutes", ja: "{count} 分" },
  hours: { "zh-CN": "{count} 小时", en: "{count} hours", ja: "{count} 時間" },
  hoursMinutes: { "zh-CN": "{hours} 小时 {minutes} 分钟", en: "{hours} hours {minutes} minutes", ja: "{hours} 時間 {minutes} 分" },
  days: { "zh-CN": "{count} 天", en: "{count} days", ja: "{count} 日" },
  daysHours: { "zh-CN": "{days} 天 {hours} 小时", en: "{days} days {hours} hours", ja: "{days} 日 {hours} 時間" },
} satisfies Record<string, LocalizedText>;

type RuntimeError = { kind: "timeout" } | { kind: "api"; reason: unknown };

export function RuntimeDiagnosticsLite() {
  const { locale, number, text } = useI18n();
  const [diagnostics, setDiagnostics] = useState<RuntimeDiagnosticsLiteResponse | null>(null);
  const [error, setError] = useState<RuntimeError | null>(null);
  const [requestRevision, setRequestRevision] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    let timedOut = false;
    const timeout = globalThis.setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, runtimeDiagnosticsRequestTimeoutMs);

    void apiGet<RuntimeDiagnosticsLiteResponse>("/api/runtime-diagnostics", { signal: controller.signal })
      .then((response) => {
        if (!active) return;
        setDiagnostics(response);
        setError(null);
      })
      .catch((reason: unknown) => {
        if (!active) return;
        if (timedOut && isAbortError(reason)) {
          setError({ kind: "timeout" });
        } else if (!isAbortError(reason)) {
          setError({ kind: "api", reason });
        }
      })
      .finally(() => {
        globalThis.clearTimeout(timeout);
      });

    return () => {
      active = false;
      globalThis.clearTimeout(timeout);
      controller.abort();
    };
  }, [requestRevision]);

  const retryDiagnostics = () => {
    setDiagnostics(null);
    setError(null);
    setRequestRevision((revision) => revision + 1);
  };

  const errorText = error?.kind === "timeout" ? text(copy.timeout) : error?.kind === "api" ? apiErrorText(error.reason, locale) : "";
  const formatUptime = (value: number): string => {
    const seconds = Math.floor(Number.isFinite(value) && value > 0 ? value : 0);
    if (seconds < 60) return text(copy.seconds, { count: number(seconds) });
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return text(copy.minutes, { count: number(minutes) });
    const hours = Math.floor(minutes / 60);
    const remainingMinutes = minutes % 60;
    if (hours < 24) return remainingMinutes
      ? text(copy.hoursMinutes, { hours: number(hours), minutes: number(remainingMinutes) })
      : text(copy.hours, { count: number(hours) });
    const days = Math.floor(hours / 24);
    const remainingHours = hours % 24;
    return remainingHours
      ? text(copy.daysHours, { days: number(days), hours: number(remainingHours) })
      : text(copy.days, { count: number(days) });
  };

  return (
    <section className="runtime-diagnostics-lite" aria-labelledby="runtime-diagnostics-title" aria-busy={!diagnostics && !error}>
      <span>{text(copy.kicker)}</span>
      <h2 id="runtime-diagnostics-title">{text(copy.title)}</h2>
      <p>{text(copy.description)}</p>
      {!diagnostics && !error ? <p className="runtime-diagnostics-message" role="status">{text(copy.loading)}</p> : null}
      {error ? <p className="runtime-diagnostics-message is-error" role="alert">{text(copy.unavailable, { error: errorText })}</p> : null}
      {error ? <button type="button" className="button runtime-diagnostics-retry" onClick={retryDiagnostics}>{text(copy.retry)}</button> : null}
      {diagnostics ? (
        <dl className="runtime-diagnostics-grid">
          <div><dt>{text(copy.version)}</dt><dd>{diagnostics.version}</dd></div>
          <div><dt>{text(copy.uptime)}</dt><dd>{formatUptime(diagnostics.uptime_seconds)}</dd></div>
          <div><dt>{text(copy.database)}</dt><dd>{diagnostics.database.status === "healthy" ? text(copy.healthy) : text(copy.databaseUnavailable)}</dd></div>
          <div><dt>{text(copy.cacheFiles)}</dt><dd>{number(Math.max(0, diagnostics.cache.file_count))}</dd></div>
          <div><dt>{text(copy.cacheSize)}</dt><dd>{formatRuntimeBytes(diagnostics.cache.bytes, locale)}</dd></div>
          <div>
            <dt>{text(copy.cacheScan)}</dt>
            <dd>{diagnostics.cache.complete ? text(copy.complete) : text(copy.partial, { count: number(Math.max(0, diagnostics.cache.scan_errors)) })}</dd>
          </div>
        </dl>
      ) : null}
    </section>
  );
}
