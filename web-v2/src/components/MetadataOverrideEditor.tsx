import { useEffect, useId, useMemo, useState, type FormEvent } from "react";

import "./MetadataOverrideEditor.css";

import { apiErrorText, getWork, saveMetadataOverride } from "../lib/api";
import {
  WORK_METADATA_OVERRIDE_FIELDS,
  workMetadataOriginalValues,
  workMetadataOverrideValues,
  type WorkMetadataOverrideField,
  type WorkMetadataOverrideValues,
} from "../lib/workMetadataPresentation";
import type { WorkDetailResponse } from "../types";

export interface MetadataOverrideEditorProps {
  detail: WorkDetailResponse;
  disabled?: boolean;
  onUpdated: (detail: WorkDetailResponse) => void;
}

const fieldPresentation: Record<WorkMetadataOverrideField, { label: string; placeholder: string; maxLength: number }> = {
  title: { label: "显示标题", placeholder: "输入本地显示标题", maxLength: 500 },
  creator: { label: "作者／社团", placeholder: "输入本地作者或社团", maxLength: 300 },
  series: { label: "系列", placeholder: "输入本地系列名称", maxLength: 500 },
  language: { label: "语言", placeholder: "输入语言标签", maxLength: 80 },
};

export function MetadataOverrideEditor({ detail, disabled = false, onUpdated }: MetadataOverrideEditorProps) {
  const panelID = useId();
  const targetID = detail.work.candidate_id;
  const savedValues = useMemo(() => workMetadataOverrideValues(detail), [detail]);
  const originalValues = useMemo(() => workMetadataOriginalValues(detail), [detail]);
  const [drafts, setDrafts] = useState<WorkMetadataOverrideValues>(savedValues);
  const [savingField, setSavingField] = useState<WorkMetadataOverrideField | null>(null);
  const [status, setStatus] = useState("");
  const [statusKind, setStatusKind] = useState<"success" | "error">("success");

  useEffect(() => {
    setDrafts(savedValues);
    setStatus("");
  }, [targetID, savedValues.title, savedValues.creator, savedValues.series, savedValues.language]);

  const save = async (field: WorkMetadataOverrideField, requestedValue: string) => {
    if (savingField || disabled || !targetID) return;
    const fieldValue = requestedValue.trim();
    setSavingField(field);
    setStatus("");
    let stored = false;
    try {
      await saveMetadataOverride({
        target_type: "work",
        target_id: targetID,
        field_name: field,
        field_value: fieldValue,
      });
      stored = true;
      const refreshed = await getWork(targetID);
      onUpdated(refreshed);
      setStatusKind("success");
      setStatus(fieldValue ? `${fieldPresentation[field].label}已保存。` : `${fieldPresentation[field].label}已恢复扫描值。`);
    } catch (reason) {
      setStatusKind("error");
      setStatus(stored
        ? `覆盖值已保存，但详情暂时无法刷新：${apiErrorText(reason)}`
        : `保存失败：${apiErrorText(reason)}`,
      );
    } finally {
      setSavingField(null);
    }
  };

  const submit = (event: FormEvent<HTMLFormElement>, field: WorkMetadataOverrideField) => {
    event.preventDefault();
    void save(field, drafts[field]);
  };

  return (
    <section className="detail-section metadata-override-editor" aria-labelledby={`${panelID}-title`} aria-busy={savingField !== null}>
      <header>
        <span>LOCAL DISPLAY</span>
        <h2 id={`${panelID}-title`}>本地元数据显示</h2>
        <p>仅改变 bmanga 中的显示值；扫描结果和漫画源文件始终保留原样。</p>
      </header>
      <div className="metadata-override-fields">
        {WORK_METADATA_OVERRIDE_FIELDS.map((field) => {
          const presentation = fieldPresentation[field];
          const saved = savedValues[field];
          const draft = drafts[field];
          const busy = savingField === field;
          const controlsDisabled = disabled || savingField !== null;
          const dirty = draft.trim() !== saved;
          return (
            <form className="metadata-override-field" key={field} onSubmit={(event) => submit(event, field)}>
              <label htmlFor={`${panelID}-${field}`}>{presentation.label}</label>
              <input
                id={`${panelID}-${field}`}
                value={draft}
                maxLength={presentation.maxLength}
                placeholder={presentation.placeholder}
                disabled={controlsDisabled}
                autoComplete="off"
                onChange={(event) => setDrafts((current) => ({ ...current, [field]: event.target.value }))}
              />
              <small>扫描值：{originalValues[field] || "未识别"}</small>
              <div>
                <button type="button" disabled={controlsDisabled || !dirty} onClick={() => setDrafts((current) => ({ ...current, [field]: saved }))}>撤销输入</button>
                <button type="button" disabled={controlsDisabled || !saved} onClick={() => { setDrafts((current) => ({ ...current, [field]: "" })); void save(field, ""); }}>清除覆盖</button>
                <button className="primary" type="submit" disabled={controlsDisabled || !dirty}>{busy ? "保存中…" : "保存"}</button>
              </div>
            </form>
          );
        })}
      </div>
      <p className={`metadata-override-status ${statusKind === "error" ? "is-error" : ""}`.trim()} role={statusKind === "error" ? "alert" : "status"} aria-live="polite" aria-atomic="true">{status}</p>
    </section>
  );
}
