import { useId, type CSSProperties } from "react";

import "./PersonalMarkPanel.css";

import { useI18n, type LocalizedText } from "../i18n";
import type { TargetType, UserMark, UserMarkSavePayload } from "../types";
import {
  PERSONAL_RATING_OPTIONS,
  QUALITY_RATING_OPTIONS,
  READ_STATUS_OPTIONS,
  REREAD_PRIORITY_OPTIONS,
  createUserMarkPatch,
  currentPersonalMarkValue,
  type PersonalMarkField,
  type PersonalMarkOption,
} from "../lib/userMarks";

const copy = {
  savingSuffix: { "zh-CN": " · 保存中…", en: " · Saving…", ja: " · 保存中…" },
  signals: { "zh-CN": "PERSONAL SIGNALS", en: "PERSONAL SIGNALS", ja: "個人設定" },
  title: { "zh-CN": "个人标记", en: "Personal markers", ja: "個人マーク" },
  description: {
    "zh-CN": "只保存到 bmanga 的私人状态，不会改动或删除漫画源文件。",
    en: "Saved only in bmanga's private state; source files are never changed or deleted.",
    ja: "bmanga の個人用状態にのみ保存され、漫画の元ファイルを変更・削除することはありません。",
  },
  personalRating: { "zh-CN": "个人评分", en: "Personal rating", ja: "個人評価" },
  personalRatingRange: { "zh-CN": "个人评分 · {minimum}–{maximum}", en: "Personal rating · {minimum}–{maximum}", ja: "個人評価 · {minimum}～{maximum}" },
  readStatus: { "zh-CN": "阅读状态", en: "Reading status", ja: "読書状態" },
  rereadPriority: { "zh-CN": "重看优先", en: "Reread priority", ja: "再読優先度" },
  translationQuality: { "zh-CN": "翻译质量", en: "Translation quality", ja: "翻訳品質" },
  translationQualityRange: { "zh-CN": "翻译质量 · {minimum}–{maximum}", en: "Translation quality · {minimum}–{maximum}", ja: "翻訳品質 · {minimum}～{maximum}" },
  imageQuality: { "zh-CN": "图片质量", en: "Image quality", ja: "画像品質" },
  imageQualityRange: { "zh-CN": "图片质量 · {minimum}–{maximum}", en: "Image quality · {minimum}–{maximum}", ja: "画像品質 · {minimum}～{maximum}" },
  versionQuality: { "zh-CN": "版本质量", en: "Edition quality", ja: "版の品質" },
  unreadWarning: {
    "zh-CN": "改为“未读”会清除这本书已保存的阅读进度。",
    en: "Changing to “Unread” clears the saved reading progress for this book.",
    ja: "「未読」に変更すると、この本の保存済み読書進捗が消去されます。",
  },
  savingField: { "zh-CN": "正在保存{field}…", en: "Saving {field}…", ja: "{field}を保存しています…" },
  unread: { "zh-CN": "未读", en: "Unread", ja: "未読" },
  reading: { "zh-CN": "在读", en: "Reading", ja: "読書中" },
  completed: { "zh-CN": "已读", en: "Read", ja: "読了" },
  abandoned: { "zh-CN": "搁置", en: "On hold", ja: "保留" },
  clear: { "zh-CN": "清除", en: "Clear", ja: "クリア" },
  clearPersonalRating: { "zh-CN": "清除个人评分，恢复为未评分", en: "Clear the personal rating and return to unrated", ja: "個人評価を消去して未評価に戻す" },
  personalScore: { "zh-CN": "个人评分 {score} 分", en: "Personal rating: {score}", ja: "個人評価 {score} 点" },
  none: { "zh-CN": "无", en: "None", ja: "なし" },
  low: { "zh-CN": "低", en: "Low", ja: "低" },
  medium: { "zh-CN": "中", en: "Medium", ja: "中" },
  high: { "zh-CN": "高", en: "High", ja: "高" },
  rereadNone: { "zh-CN": "不设置重看优先级", en: "Do not set a reread priority", ja: "再読優先度を設定しない" },
  rereadLow: { "zh-CN": "低重看优先级", en: "Low reread priority", ja: "再読優先度：低" },
  rereadMedium: { "zh-CN": "中重看优先级", en: "Medium reread priority", ja: "再読優先度：中" },
  rereadHigh: { "zh-CN": "高重看优先级", en: "High reread priority", ja: "再読優先度：高" },
  clearQuality: { "zh-CN": "清除质量评分，恢复为未评分", en: "Clear the quality rating and return to unrated", ja: "品質評価を消去して未評価に戻す" },
  qualityScore: { "zh-CN": "质量评分 {score} 分", en: "Quality rating: {score}", ja: "品質評価 {score} 点" },
} satisfies Record<string, LocalizedText>;

export interface PersonalMarkPanelProps {
  targetType: TargetType;
  targetID: string;
  mark: UserMark | null;
  disabled?: boolean;
  savingField?: PersonalMarkField | null;
  statusMessage?: string;
  className?: string;
  onPatch: (payload: UserMarkSavePayload, field: PersonalMarkField) => void;
}

interface MarkOptionGroupProps {
  field: PersonalMarkField;
  label: string;
  options: readonly PersonalMarkOption[];
  current: string | number | null;
  disabled: boolean;
  saving: boolean;
  descriptionID?: string;
  onSelect: (field: PersonalMarkField, value: string | number | null) => void;
}

const optionStyle: CSSProperties = {
  minWidth: 44,
  minHeight: 44,
  padding: "8px 12px",
  touchAction: "manipulation",
};

const readStatusCopy: Readonly<Record<string, LocalizedText>> = {
  unread: copy.unread,
  reading: copy.reading,
  completed: copy.completed,
  abandoned: copy.abandoned,
};

function MarkOptionGroup({
  field,
  label,
  options,
  current,
  disabled,
  saving,
  descriptionID,
  onSelect,
}: MarkOptionGroupProps) {
  const { text } = useI18n();
  return (
    <fieldset className={`personal-mark-fieldset personal-mark-${field}`} disabled={disabled}>
      <legend>{label}{saving ? <span aria-hidden="true">{text(copy.savingSuffix)}</span> : null}</legend>
      <div className="personal-mark-option-row" role="group" aria-label={label} aria-describedby={descriptionID}>
        {options.map((option) => {
          const selected = Object.is(current, option.value);
          const dataValue = option.value === null ? "clear" : String(option.value);
          return (
            <button
              key={dataValue}
              className={`personal-mark-option ${selected ? "is-active" : ""} ${option.value === null ? "is-clear" : ""}`.trim()}
              type="button"
              aria-pressed={selected}
              aria-label={option.accessibleLabel || option.label}
              data-mark-field={field}
              data-mark-value={dataValue}
              disabled={disabled}
              style={optionStyle}
              onClick={() => {
                if (!selected) onSelect(field, option.value);
              }}
            >
              {option.label}
            </button>
          );
        })}
      </div>
    </fieldset>
  );
}

export function PersonalMarkPanel({
  targetType,
  targetID,
  mark,
  disabled = false,
  savingField = null,
  statusMessage = "",
  className = "",
  onPatch,
}: PersonalMarkPanelProps) {
  const { number, text } = useI18n();
  const panelID = useId();
  const titleID = `${panelID}-title`;
  const readWarningID = `${panelID}-read-warning`;
  const busy = savingField !== null;
  const controlsDisabled = disabled || busy || !targetID.trim();
  const select = (field: PersonalMarkField, value: string | number | null) => {
    onPatch(createUserMarkPatch(targetType, targetID, field, value, new Date().toISOString()), field);
  };
  const savingLabel = savingField ? text({
    read_status: copy.readStatus,
    personal_rating: copy.personalRating,
    reread_priority: copy.rereadPriority,
    translation_quality: copy.translationQuality,
    image_quality: copy.imageQuality,
  }[savingField]) : "";
  const readStatusOptions: readonly PersonalMarkOption[] = READ_STATUS_OPTIONS.map((option) => ({
    ...option,
    label: text(readStatusCopy[String(option.value)] ?? copy.unread),
    accessibleLabel: undefined,
  }));
  const personalRatingOptions: readonly PersonalMarkOption[] = PERSONAL_RATING_OPTIONS.map((option) => option.value === null
    ? { ...option, label: text(copy.clear), accessibleLabel: text(copy.clearPersonalRating) }
    : { ...option, label: number(Number(option.value)), accessibleLabel: text(copy.personalScore, { score: number(Number(option.value)) }) });
  const rereadPriorityOptions: readonly PersonalMarkOption[] = REREAD_PRIORITY_OPTIONS.map((option) => {
    const labels = [copy.none, copy.low, copy.medium, copy.high];
    const accessibleLabels = [copy.rereadNone, copy.rereadLow, copy.rereadMedium, copy.rereadHigh];
    const index = typeof option.value === "number" ? option.value : 0;
    return { ...option, label: text(labels[index] || copy.none), accessibleLabel: text(accessibleLabels[index] || copy.rereadNone) };
  });
  const qualityRatingOptions: readonly PersonalMarkOption[] = QUALITY_RATING_OPTIONS.map((option) => option.value === null
    ? { ...option, label: text(copy.clear), accessibleLabel: text(copy.clearQuality) }
    : { ...option, label: number(Number(option.value)), accessibleLabel: text(copy.qualityScore, { score: number(Number(option.value)) }) });

  return (
    <section
      className={`detail-section personal-mark-panel editorial-mark ${className}`.trim()}
      aria-labelledby={titleID}
      aria-busy={busy}
    >
      <header>
        <span>{text(copy.signals)}</span>
        <h2 id={titleID}>{text(copy.title)}</h2>
        <p>{text(copy.description)}</p>
      </header>

      <MarkOptionGroup
        field="personal_rating"
        label={text(copy.personalRatingRange, { minimum: number(0), maximum: number(10) })}
        options={personalRatingOptions}
        current={currentPersonalMarkValue(mark, "personal_rating")}
        disabled={controlsDisabled}
        saving={savingField === "personal_rating"}
        onSelect={select}
      />

      <MarkOptionGroup
        field="read_status"
        label={text(copy.readStatus)}
        options={readStatusOptions}
        current={currentPersonalMarkValue(mark, "read_status")}
        disabled={controlsDisabled}
        saving={savingField === "read_status"}
        descriptionID={targetType === "work" ? readWarningID : undefined}
        onSelect={select}
      />
      {targetType === "work" ? <p id={readWarningID} className="personal-mark-warning">{text(copy.unreadWarning)}</p> : null}

      <MarkOptionGroup
        field="reread_priority"
        label={text(copy.rereadPriority)}
        options={rereadPriorityOptions}
        current={currentPersonalMarkValue(mark, "reread_priority")}
        disabled={controlsDisabled}
        saving={savingField === "reread_priority"}
        onSelect={select}
      />

      {targetType === "work" ? (
        <div className="personal-mark-quality-grid" aria-label={text(copy.versionQuality)}>
          <MarkOptionGroup
            field="translation_quality"
            label={text(copy.translationQualityRange, { minimum: number(1), maximum: number(5) })}
            options={qualityRatingOptions}
            current={currentPersonalMarkValue(mark, "translation_quality")}
            disabled={controlsDisabled}
            saving={savingField === "translation_quality"}
            onSelect={select}
          />
          <MarkOptionGroup
            field="image_quality"
            label={text(copy.imageQualityRange, { minimum: number(1), maximum: number(5) })}
            options={qualityRatingOptions}
            current={currentPersonalMarkValue(mark, "image_quality")}
            disabled={controlsDisabled}
            saving={savingField === "image_quality"}
            onSelect={select}
          />
        </div>
      ) : null}

      <p className="personal-mark-status" role="status" aria-live="polite" aria-atomic="true">
        {busy ? text(copy.savingField, { field: savingLabel }) : statusMessage}
      </p>
    </section>
  );
}
