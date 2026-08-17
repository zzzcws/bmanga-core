import { useEffect, useState } from "react";

import { apiErrorText, apiGet, isAbortError } from "../lib/api";
import {
  formatRuntimeBytes,
  formatRuntimeUptime,
  type RuntimeDiagnosticsLite as RuntimeDiagnosticsLiteResponse,
} from "../lib/runtimeDiagnostics";
import "./RuntimeDiagnosticsLite.css";

const runtimeDiagnosticsRequestTimeoutMs = 5_000;
const runtimeDiagnosticsTimeoutText = "运行状态读取超时，请稍后重试。";

export function RuntimeDiagnosticsLite() {
  const [diagnostics, setDiagnostics] = useState<RuntimeDiagnosticsLiteResponse | null>(null);
  const [error, setError] = useState("");
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
        setError("");
      })
      .catch((reason: unknown) => {
        if (!active) return;
        if (timedOut && isAbortError(reason)) {
          setError(runtimeDiagnosticsTimeoutText);
        } else if (!isAbortError(reason)) {
          setError(apiErrorText(reason));
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
    setError("");
    setRequestRevision((revision) => revision + 1);
  };

  return (
    <section className="runtime-diagnostics-lite" aria-labelledby="runtime-diagnostics-title" aria-busy={!diagnostics && !error}>
      <span>LOCAL DIAGNOSTICS</span>
      <h2 id="runtime-diagnostics-title">运行状态</h2>
      <p>只读取本服务、数据库与应用缓存的聚合状态；不会探测外网、运行系统命令或执行维护操作。</p>
      {!diagnostics && !error ? <p className="runtime-diagnostics-message" role="status">正在读取本地状态…</p> : null}
      {error ? <p className="runtime-diagnostics-message is-error" role="alert">运行状态暂时不可用：{error}</p> : null}
      {error ? <button type="button" className="button runtime-diagnostics-retry" onClick={retryDiagnostics}>重新读取</button> : null}
      {diagnostics ? (
        <dl className="runtime-diagnostics-grid">
          <div><dt>版本</dt><dd>{diagnostics.version}</dd></div>
          <div><dt>已运行</dt><dd>{formatRuntimeUptime(diagnostics.uptime_seconds)}</dd></div>
          <div><dt>数据库</dt><dd>{diagnostics.database.status === "healthy" ? "正常" : "不可用"}</dd></div>
          <div><dt>缓存文件</dt><dd>{Math.max(0, diagnostics.cache.file_count).toLocaleString("zh-CN")}</dd></div>
          <div><dt>缓存大小</dt><dd>{formatRuntimeBytes(diagnostics.cache.bytes)}</dd></div>
          <div>
            <dt>缓存扫描</dt>
            <dd>{diagnostics.cache.complete ? "完整" : `部分完成 · ${Math.max(0, diagnostics.cache.scan_errors)} 个错误`}</dd>
          </div>
        </dl>
      ) : null}
    </section>
  );
}
