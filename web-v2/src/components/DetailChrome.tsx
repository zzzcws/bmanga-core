import { forwardRef, type ReactNode } from "react";

import { useI18n, type LocalizedText } from "../i18n";

const copy = {
  seriesEdition: {
    "zh-CN": "SERIES EDITION",
    en: "SERIES EDITION",
    ja: "シリーズ版",
  },
  privateEdition: {
    "zh-CN": "PRIVATE EDITION",
    en: "PRIVATE EDITION",
    ja: "プライベート版",
  },
  seriesFolio: { "zh-CN": "{number} / SERIES", en: "{number} / SERIES", ja: "{number} / シリーズ" },
  bookFolio: { "zh-CN": "{number} / BOOK", en: "{number} / BOOK", ja: "{number} / BOOK" },
  closeDetails: {
    "zh-CN": "关闭详情",
    en: "Close details",
    ja: "詳細を閉じる",
  },
  marginalia: {
    "zh-CN": "PRIVATE MARGINALIA",
    en: "PRIVATE MARGINALIA",
    ja: "プライベートメモ",
  },
  unsavedCount: {
    "zh-CN": "有未保存修改 · {length} / {maximum}",
    en: "Unsaved changes · {length} / {maximum}",
    ja: "未保存の変更あり · {length} / {maximum}",
  },
  count: {
    "zh-CN": "{length} / {maximum}",
    en: "{length} / {maximum}",
    ja: "{length} / {maximum}",
  },
  reset: {
    "zh-CN": "撤销修改",
    en: "Discard changes",
    ja: "変更を取り消す",
  },
  saving: {
    "zh-CN": "保存中…",
    en: "Saving…",
    ja: "保存中…",
  },
  saveNote: {
    "zh-CN": "保存备注",
    en: "Save note",
    ja: "メモを保存",
  },
} satisfies Record<string, LocalizedText>;

interface DetailHeaderProps {
  kind: "work" | "series";
  title: string;
  busy: boolean;
  onClose: () => void;
}

export const DetailHeader = forwardRef<HTMLButtonElement, DetailHeaderProps>(function DetailHeader({ kind, title, busy, onClose }, ref) {
  const { number, text } = useI18n();
  return (
    <header className="detail-header editorial-detail-header">
      <div className="detail-header-copy">
        <span className="eyebrow">{text(kind === "series" ? copy.seriesEdition : copy.privateEdition)}</span>
        <strong title={title}>{title}</strong>
      </div>
      <span className="detail-header-folio" aria-hidden="true">{text(kind === "series" ? copy.seriesFolio : copy.bookFolio, { number: number(kind === "series" ? 2 : 1, { minimumIntegerDigits: 2, useGrouping: false }) })}</span>
      <button ref={ref} type="button" className="icon-button" aria-label={text(copy.closeDetails)} disabled={busy} onClick={onClose}>×</button>
    </header>
  );
});

interface DetailCoverFrameProps {
  children: ReactNode;
  kind: string;
  state: string;
}

export function DetailCoverFrame({ children, kind, state }: DetailCoverFrameProps) {
  return (
    <figure className="detail-cover-shell">
      <div className="detail-cover">{children}</div>
      <figcaption><span>{kind}</span><em>{state}</em></figcaption>
    </figure>
  );
}

interface PersonalNoteEditorProps {
  title: string;
  placeholder: string;
  value: string;
  savedValue: string;
  saving: boolean;
  onChange: (value: string) => void;
  onReset: () => void;
  onSave: () => void;
}

export function PersonalNoteEditor({
  title,
  placeholder,
  value,
  savedValue,
  saving,
  onChange,
  onReset,
  onSave,
}: PersonalNoteEditorProps) {
  const dirty = value !== savedValue;
  const { number, text } = useI18n();
  const countParameters = { length: number(value.length), maximum: number(4000) };
  return (
    <section className={`detail-section personal-note editorial-note ${dirty ? "is-dirty" : ""}`}>
      <header><span>{text(copy.marginalia)}</span><h2>{title}</h2></header>
      <textarea
        value={value}
        maxLength={4000}
        disabled={saving}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
      />
      <div>
        <small aria-live="polite">{text(dirty ? copy.unsavedCount : copy.count, countParameters)}</small>
        <span className="personal-note-actions">
          {dirty ? <button type="button" disabled={saving} onClick={onReset}>{text(copy.reset)}</button> : null}
          <button type="button" disabled={saving || !dirty} onClick={onSave}>{text(saving ? copy.saving : copy.saveNote)}</button>
        </span>
      </div>
    </section>
  );
}
