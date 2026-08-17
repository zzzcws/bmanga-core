export interface SectionHeaderProps {
  title: string;
  eyebrow?: string;
  action?: string;
  onAction?: () => void;
}

export function SectionHeader({ title, eyebrow, action, onAction }: SectionHeaderProps) {
  return (
    <header className="section-head">
      <div><h2>{title}</h2>{eyebrow ? <span>{eyebrow}</span> : null}</div>
      {action && onAction ? <button type="button" className="text-button" onClick={onAction}>{action}<span aria-hidden="true">→</span></button> : null}
    </header>
  );
}
