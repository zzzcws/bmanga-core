import { BookCard } from "./CatalogCard";
import { useI18n, type LocalizedText } from "../i18n";
import { relatedGroupLabel, uniqueRelatedWorks } from "../lib/relatedWorksPresentation";
import type { CatalogItem, WorkSummary } from "../types";

import "./RelatedWorks.css";

const copy = {
  kicker: { "zh-CN": "RELATED EDITIONS", en: "RELATED EDITIONS", ja: "関連する版" },
  title: { "zh-CN": "相关作品", en: "Related works", ja: "関連作品" },
  description: {
    "zh-CN": "先核对同作品的其他版本，也可以沿着系列或作者继续翻阅。",
    en: "Compare other editions of the same work, or continue browsing by series or creator.",
    ja: "同じ作品の別版を確認したり、シリーズや作者からさらに作品を探したりできます。",
  },
  otherEditions: { "zh-CN": "同作品／其他版本", en: "Same work / other editions", ja: "同じ作品／別版" },
  bookCount: { "zh-CN": "{count} 本", en: "{count} books", ja: "{count} 冊" },
  editionNote: {
    "zh-CN": "标题与页数接近，但可能是不同翻译、增补页或真正的重复副本，请按内容确认。",
    en: "Titles and page counts are similar, but these may be different translations, expanded editions, or true duplicates. Verify by content.",
    ja: "タイトルとページ数は近いものの、別翻訳・増補版・実際の重複である可能性があります。内容で確認してください。",
  },
  sameSeries: { "zh-CN": "同系列", en: "Same series", ja: "同じシリーズ" },
  sameCreator: { "zh-CN": "同作者", en: "Same creator", ja: "同じ作者" },
} satisfies Record<string, LocalizedText>;

export interface RelatedWorksProps {
  currentID: string;
  editions?: WorkSummary[];
  series?: WorkSummary[];
  creators?: WorkSummary[];
  onOpen: (item: CatalogItem) => void;
}

export function RelatedWorks({ currentID, editions, series, creators, onOpen }: RelatedWorksProps) {
  const { number, text } = useI18n();
  const editionItems = uniqueRelatedWorks(editions, currentID);
  const seriesItems = uniqueRelatedWorks(series, currentID);
  const creatorItems = uniqueRelatedWorks(creators, currentID);
  const seriesLabel = relatedGroupLabel(seriesItems);
  const creatorLabel = relatedGroupLabel(creatorItems);
  if (!editionItems.length && !seriesItems.length && !creatorItems.length) return null;

  return (
    <section className="detail-related" aria-labelledby="detail-related-title">
      <header className="detail-related-heading">
        <span>{text(copy.kicker)}</span>
        <h2 id="detail-related-title">{text(copy.title)}</h2>
        <p>{text(copy.description)}</p>
      </header>

      {editionItems.length ? (
        <section className="detail-related-group" aria-labelledby="detail-related-editions-title">
          <div className="detail-related-group-title">
            <h3 id="detail-related-editions-title" tabIndex={-1}>{text(copy.otherEditions)}</h3>
            <span>{text(copy.bookCount, { count: number(editionItems.length) })}</span>
          </div>
          <p className="detail-related-group-note">{text(copy.editionNote)}</p>
          <div className="detail-related-grid">
            {editionItems.map((item, index) => (
              <BookCard key={item.candidate_id} item={item} index={index} context="related" onOpen={onOpen} />
            ))}
          </div>
        </section>
      ) : null}

      {seriesItems.length ? (
        <section className="detail-related-group" aria-labelledby="detail-related-series-title">
          <div className="detail-related-group-title">
            <h3 id="detail-related-series-title" tabIndex={-1}>{text(copy.sameSeries)}{seriesLabel ? <small> · {seriesLabel}</small> : null}</h3>
            <span>{text(copy.bookCount, { count: number(seriesItems.length) })}</span>
          </div>
          <div className="detail-related-grid">
            {seriesItems.map((item, index) => (
              <BookCard key={item.candidate_id} item={item} index={index} context="related" onOpen={onOpen} />
            ))}
          </div>
        </section>
      ) : null}

      {creatorItems.length ? (
        <section className="detail-related-group" aria-labelledby="detail-related-creators-title">
          <div className="detail-related-group-title">
            <h3 id="detail-related-creators-title" tabIndex={-1}>{text(copy.sameCreator)}{creatorLabel ? <small> · {creatorLabel}</small> : null}</h3>
            <span>{text(copy.bookCount, { count: number(creatorItems.length) })}</span>
          </div>
          <div className="detail-related-grid">
            {creatorItems.map((item, index) => (
              <BookCard key={item.candidate_id} item={item} index={index} context="related" onOpen={onOpen} />
            ))}
          </div>
        </section>
      ) : null}
    </section>
  );
}
