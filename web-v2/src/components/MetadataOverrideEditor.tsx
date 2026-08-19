import { useEffect, useId, useMemo, useState, type FormEvent } from "react";

import "./MetadataOverrideEditor.css";

import { useI18n, type LocalizedText } from "../i18n";
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

const copy = {
  localDisplay: { "zh-CN": "LOCAL DISPLAY", en: "LOCAL DISPLAY", ja: "ローカル表示" },
  title: { "zh-CN": "本地元数据显示", en: "Local metadata display", ja: "ローカルメタデータ表示" },
  description: {
    "zh-CN": "仅改变 bmanga 中的显示值；扫描结果和漫画源文件始终保留原样。",
    en: "Changes only the value displayed in bmanga; scan results and source files remain untouched.",
    ja: "bmanga 上の表示だけを変更します。スキャン結果と漫画の元ファイルは変更されません。",
  },
  displayTitle: { "zh-CN": "显示标题", en: "Display title", ja: "表示タイトル" },
  displayTitlePlaceholder: { "zh-CN": "输入本地显示标题", en: "Enter a local display title", ja: "ローカル表示タイトルを入力" },
  creator: { "zh-CN": "作者／社团", en: "Creator / circle", ja: "作者／サークル" },
  creatorPlaceholder: { "zh-CN": "输入本地作者或社团", en: "Enter a local creator or circle", ja: "ローカルの作者またはサークルを入力" },
  series: { "zh-CN": "系列", en: "Series", ja: "シリーズ" },
  seriesPlaceholder: { "zh-CN": "输入本地系列名称", en: "Enter a local series name", ja: "ローカルのシリーズ名を入力" },
  language: { "zh-CN": "语言", en: "Language", ja: "言語" },
  languagePlaceholder: { "zh-CN": "输入语言标签", en: "Enter a language label", ja: "言語ラベルを入力" },
  fieldSaved: { "zh-CN": "{field}已保存。", en: "{field} saved.", ja: "{field}を保存しました。" },
  fieldRestored: { "zh-CN": "{field}已恢复扫描值。", en: "{field} restored to the scanned value.", ja: "{field}をスキャン値に戻しました。" },
  savedRefreshFailed: {
    "zh-CN": "覆盖值已保存，但详情暂时无法刷新：{error}",
    en: "The override was saved, but details could not be refreshed: {error}",
    ja: "上書き値は保存されましたが、詳細を更新できませんでした：{error}",
  },
  saveFailed: { "zh-CN": "保存失败：{error}", en: "Save failed: {error}", ja: "保存に失敗しました：{error}" },
  scannedValue: { "zh-CN": "扫描值：{value}", en: "Scanned value: {value}", ja: "スキャン値：{value}" },
  unidentified: { "zh-CN": "未识别", en: "Not identified", ja: "未検出" },
  resetInput: { "zh-CN": "撤销输入", en: "Reset input", ja: "入力を元に戻す" },
  clearOverride: { "zh-CN": "清除覆盖", en: "Clear override", ja: "上書きを消去" },
  saving: { "zh-CN": "保存中…", en: "Saving…", ja: "保存中…" },
  save: { "zh-CN": "保存", en: "Save", ja: "保存" },
} satisfies Record<string, LocalizedText>;

const fieldPresentation: Record<WorkMetadataOverrideField, { label: LocalizedText; placeholder: LocalizedText; maxLength: number }> = {
  title: { label: copy.displayTitle, placeholder: copy.displayTitlePlaceholder, maxLength: 500 },
  creator: { label: copy.creator, placeholder: copy.creatorPlaceholder, maxLength: 300 },
  series: { label: copy.series, placeholder: copy.seriesPlaceholder, maxLength: 500 },
  language: { label: copy.language, placeholder: copy.languagePlaceholder, maxLength: 80 },
};

type EditorStatus =
  | { kind: "saved" | "restored"; field: WorkMetadataOverrideField }
  | { kind: "saved-refresh-failed" | "save-failed"; error: unknown };

export function MetadataOverrideEditor({ detail, disabled = false, onUpdated }: MetadataOverrideEditorProps) {
  const { locale, text } = useI18n();
  const panelID = useId();
  const targetID = detail.work.candidate_id;
  const savedValues = useMemo(() => workMetadataOverrideValues(detail), [detail]);
  const originalValues = useMemo(() => workMetadataOriginalValues(detail), [detail]);
  const [drafts, setDrafts] = useState<WorkMetadataOverrideValues>(savedValues);
  const [savingField, setSavingField] = useState<WorkMetadataOverrideField | null>(null);
  const [status, setStatus] = useState<EditorStatus | null>(null);
  const [statusKind, setStatusKind] = useState<"success" | "error">("success");

  useEffect(() => {
    setDrafts(savedValues);
    setStatus(null);
  }, [targetID, savedValues.title, savedValues.creator, savedValues.series, savedValues.language]);

  const save = async (field: WorkMetadataOverrideField, requestedValue: string) => {
    if (savingField || disabled || !targetID) return;
    const fieldValue = requestedValue.trim();
    setSavingField(field);
    setStatus(null);
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
      setStatus({ kind: fieldValue ? "saved" : "restored", field });
    } catch (reason) {
      setStatusKind("error");
      setStatus({ kind: stored ? "saved-refresh-failed" : "save-failed", error: reason });
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
        <span>{text(copy.localDisplay)}</span>
        <h2 id={`${panelID}-title`}>{text(copy.title)}</h2>
        <p>{text(copy.description)}</p>
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
              <label htmlFor={`${panelID}-${field}`}>{text(presentation.label)}</label>
              <input
                id={`${panelID}-${field}`}
                value={draft}
                maxLength={presentation.maxLength}
                placeholder={text(presentation.placeholder)}
                disabled={controlsDisabled}
                autoComplete="off"
                onChange={(event) => setDrafts((current) => ({ ...current, [field]: event.target.value }))}
              />
              <small>{text(copy.scannedValue, { value: originalValues[field] || text(copy.unidentified) })}</small>
              <div>
                <button type="button" disabled={controlsDisabled || !dirty} onClick={() => setDrafts((current) => ({ ...current, [field]: saved }))}>{text(copy.resetInput)}</button>
                <button type="button" disabled={controlsDisabled || !saved} onClick={() => { setDrafts((current) => ({ ...current, [field]: "" })); void save(field, ""); }}>{text(copy.clearOverride)}</button>
                <button className="primary" type="submit" disabled={controlsDisabled || !dirty}>{text(busy ? copy.saving : copy.save)}</button>
              </div>
            </form>
          );
        })}
      </div>
      <p className={`metadata-override-status ${statusKind === "error" ? "is-error" : ""}`.trim()} role={statusKind === "error" ? "alert" : "status"} aria-live="polite" aria-atomic="true">{status
        ? "error" in status
          ? text(status.kind === "saved-refresh-failed" ? copy.savedRefreshFailed : copy.saveFailed, { error: apiErrorText(status.error, locale) })
          : text(status.kind === "saved" ? copy.fieldSaved : copy.fieldRestored, { field: text(fieldPresentation[status.field].label) })
        : ""}</p>
    </section>
  );
}
