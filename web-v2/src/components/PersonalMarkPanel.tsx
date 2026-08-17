import { useId, type CSSProperties } from "react";

import "./PersonalMarkPanel.css";

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
  return (
    <fieldset className={`personal-mark-fieldset personal-mark-${field}`} disabled={disabled}>
      <legend>{label}{saving ? <span aria-hidden="true"> · 保存中…</span> : null}</legend>
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
  const panelID = useId();
  const titleID = `${panelID}-title`;
  const readWarningID = `${panelID}-read-warning`;
  const busy = savingField !== null;
  const controlsDisabled = disabled || busy || !targetID.trim();
  const select = (field: PersonalMarkField, value: string | number | null) => {
    onPatch(createUserMarkPatch(targetType, targetID, field, value, new Date().toISOString()), field);
  };
  const savingLabel = savingField ? {
    read_status: "阅读状态",
    personal_rating: "个人评分",
    reread_priority: "重看优先",
    translation_quality: "翻译质量",
    image_quality: "图片质量",
  }[savingField] : "";

  return (
    <section
      className={`detail-section personal-mark-panel editorial-mark ${className}`.trim()}
      aria-labelledby={titleID}
      aria-busy={busy}
    >
      <header>
        <span>PERSONAL SIGNALS</span>
        <h2 id={titleID}>个人标记</h2>
        <p>只保存到 bmanga 的私人状态，不会改动或删除漫画源文件。</p>
      </header>

      <MarkOptionGroup
        field="personal_rating"
        label="个人评分 · 0–10"
        options={PERSONAL_RATING_OPTIONS}
        current={currentPersonalMarkValue(mark, "personal_rating")}
        disabled={controlsDisabled}
        saving={savingField === "personal_rating"}
        onSelect={select}
      />

      <MarkOptionGroup
        field="read_status"
        label="阅读状态"
        options={READ_STATUS_OPTIONS}
        current={currentPersonalMarkValue(mark, "read_status")}
        disabled={controlsDisabled}
        saving={savingField === "read_status"}
        descriptionID={targetType === "work" ? readWarningID : undefined}
        onSelect={select}
      />
      {targetType === "work" ? <p id={readWarningID} className="personal-mark-warning">改为“未读”会清除这本书已保存的阅读进度。</p> : null}

      <MarkOptionGroup
        field="reread_priority"
        label="重看优先"
        options={REREAD_PRIORITY_OPTIONS}
        current={currentPersonalMarkValue(mark, "reread_priority")}
        disabled={controlsDisabled}
        saving={savingField === "reread_priority"}
        onSelect={select}
      />

      {targetType === "work" ? (
        <div className="personal-mark-quality-grid" aria-label="版本质量">
          <MarkOptionGroup
            field="translation_quality"
            label="翻译质量 · 1–5"
            options={QUALITY_RATING_OPTIONS}
            current={currentPersonalMarkValue(mark, "translation_quality")}
            disabled={controlsDisabled}
            saving={savingField === "translation_quality"}
            onSelect={select}
          />
          <MarkOptionGroup
            field="image_quality"
            label="图片质量 · 1–5"
            options={QUALITY_RATING_OPTIONS}
            current={currentPersonalMarkValue(mark, "image_quality")}
            disabled={controlsDisabled}
            saving={savingField === "image_quality"}
            onSelect={select}
          />
        </div>
      ) : null}

      <p className="personal-mark-status" role="status" aria-live="polite" aria-atomic="true">
        {busy ? `正在保存${savingLabel}…` : statusMessage}
      </p>
    </section>
  );
}
