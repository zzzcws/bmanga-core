import { BookCard } from "./CatalogCard";
import { relatedGroupLabel, uniqueRelatedWorks } from "../lib/relatedWorksPresentation";
import type { CatalogItem, WorkSummary } from "../types";

import "./RelatedWorks.css";

export interface RelatedWorksProps {
  currentID: string;
  editions?: WorkSummary[];
  series?: WorkSummary[];
  creators?: WorkSummary[];
  onOpen: (item: CatalogItem) => void;
}

export function RelatedWorks({ currentID, editions, series, creators, onOpen }: RelatedWorksProps) {
  const editionItems = uniqueRelatedWorks(editions, currentID);
  const seriesItems = uniqueRelatedWorks(series, currentID);
  const creatorItems = uniqueRelatedWorks(creators, currentID);
  const seriesLabel = relatedGroupLabel(seriesItems);
  const creatorLabel = relatedGroupLabel(creatorItems);
  if (!editionItems.length && !seriesItems.length && !creatorItems.length) return null;

  return (
    <section className="detail-related" aria-labelledby="detail-related-title">
      <header className="detail-related-heading">
        <span>RELATED EDITIONS</span>
        <h2 id="detail-related-title">相关作品</h2>
        <p>先核对同作品的其他版本，也可以沿着系列或作者继续翻阅。</p>
      </header>

      {editionItems.length ? (
        <section className="detail-related-group" aria-labelledby="detail-related-editions-title">
          <div className="detail-related-group-title">
            <h3 id="detail-related-editions-title" tabIndex={-1}>同作品／其他版本</h3>
            <span>{editionItems.length} 本</span>
          </div>
          <p className="detail-related-group-note">标题与页数接近，但可能是不同翻译、增补页或真正的重复副本，请按内容确认。</p>
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
            <h3 id="detail-related-series-title" tabIndex={-1}>同系列{seriesLabel ? <small> · {seriesLabel}</small> : null}</h3>
            <span>{seriesItems.length} 本</span>
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
            <h3 id="detail-related-creators-title" tabIndex={-1}>同作者{creatorLabel ? <small> · {creatorLabel}</small> : null}</h3>
            <span>{creatorItems.length} 本</span>
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
