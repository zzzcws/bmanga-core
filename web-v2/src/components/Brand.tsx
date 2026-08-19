import { useI18n, type LocalizedText } from "../i18n";

const copy = {
  label: {
    "zh-CN": "bmanga 私人漫画馆",
    en: "bmanga private comics library",
    ja: "bmanga プライベート漫画ライブラリ",
  },
  subtitle: {
    "zh-CN": "PRIVATE MANGA LIBRARY",
    en: "PRIVATE MANGA LIBRARY",
    ja: "プライベート漫画ライブラリ",
  },
} satisfies Record<string, LocalizedText>;

export function Brand() {
  const { text } = useI18n();
  return (
    <div className="brand" aria-label={text(copy.label)}>
      <span className="brand-mark" aria-hidden="true"><i /><i /></span>
      <span className="brand-copy">
        <strong>bmanga</strong>
        <small>{text(copy.subtitle)}</small>
      </span>
    </div>
  );
}
