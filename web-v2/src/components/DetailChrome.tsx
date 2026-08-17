import { forwardRef, type ReactNode } from "react";

interface DetailHeaderProps {
  kind: "work" | "series";
  title: string;
  busy: boolean;
  onClose: () => void;
}

export const DetailHeader = forwardRef<HTMLButtonElement, DetailHeaderProps>(function DetailHeader({ kind, title, busy, onClose }, ref) {
  return (
    <header className="detail-header editorial-detail-header">
      <div className="detail-header-copy">
        <span className="eyebrow">{kind === "series" ? "SERIES EDITION" : "PRIVATE EDITION"}</span>
        <strong title={title}>{title}</strong>
      </div>
      <span className="detail-header-folio" aria-hidden="true">{kind === "series" ? "02 / SERIES" : "01 / BOOK"}</span>
      <button ref={ref} type="button" className="icon-button" aria-label="关闭详情" disabled={busy} onClick={onClose}>×</button>
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
  return (
    <section className={`detail-section personal-note editorial-note ${dirty ? "is-dirty" : ""}`}>
      <header><span>PRIVATE MARGINALIA</span><h2>{title}</h2></header>
      <textarea
        value={value}
        maxLength={4000}
        disabled={saving}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
      />
      <div>
        <small aria-live="polite">{dirty ? `有未保存修改 · ${value.length} / 4000` : `${value.length} / 4000`}</small>
        <span className="personal-note-actions">
          {dirty ? <button type="button" disabled={saving} onClick={onReset}>撤销修改</button> : null}
          <button type="button" disabled={saving || !dirty} onClick={onSave}>{saving ? "保存中…" : "保存备注"}</button>
        </span>
      </div>
    </section>
  );
}
