import { memo } from "react";

import { coverUrl } from "../lib/api";
import {
  cleanTitle,
  isSeries,
  itemCoverID,
  itemContextLabel,
  itemCreatorLabel,
  itemKindDisplayLabel,
  itemTitle,
  pageMeta,
  progressFor,
  type CatalogCardContext,
} from "../lib/catalogPresentation";
import type { CatalogItem } from "../types";

export interface CoverProps {
  item: CatalogItem;
  size?: number;
  eager?: boolean;
  decorative?: boolean;
}

export function Cover({ item, size = 640, eager = false, decorative = false }: CoverProps) {
  const id = itemCoverID(item);
  const progress = progressFor(item);
  const responsiveCardCover = size === 640;
  return (
    <div className="book-cover">
      {id ? (
        <img
          key={`${id}:${size}`}
          src={coverUrl(id, responsiveCardCover ? 420 : size)}
          srcSet={responsiveCardCover ? `${coverUrl(id, 420)} 1x, ${coverUrl(id, 640)} 2x` : undefined}
          width={3}
          height={4}
          alt={decorative ? "" : `${itemTitle(item)}封面`}
          loading={eager ? "eager" : "lazy"}
          decoding="async"
          fetchPriority={eager ? "high" : "auto"}
          onLoad={(event) => {
            const image = event.currentTarget;
            const reveal = () => {
              image.classList.remove("image-failed");
              image.classList.add("image-ready");
            };
            try {
              void image.decode().then(reveal, reveal);
            } catch {
              reveal();
            }
          }}
          onError={(event) => {
            event.currentTarget.classList.remove("image-ready");
            event.currentTarget.classList.add("image-failed");
          }}
        />
      ) : null}
      <span className="cover-placeholder" aria-hidden="true">BM</span>
      {progress ? <span className="cover-progress" style={{ width: `${progress.percent}%` }} /> : null}
    </div>
  );
}

export type BookCardContext = CatalogCardContext;

export interface BookCardProps {
  item: CatalogItem;
  index: number;
  onOpen: (item: CatalogItem) => void;
  context?: BookCardContext;
  priority?: boolean;
}

export const BookCard = memo(function BookCard({ item, index, onOpen, context = "default", priority = false }: BookCardProps) {
  const progress = progressFor(item);
  const supplementalContext = itemContextLabel(item, context);
  const readingState = progress ? (progress.completed ? "已读 · 100%" : `读到第 ${progress.index + 1} 页 · ${Math.round(progress.percent)}%`) : `${pageMeta(item)} · ${context === "new" ? "NEW" : "未读"}`;
  const candidateType = String(item.candidate_type || "").toLowerCase();
  const kindLabel = itemKindDisplayLabel(item);
  const creatorLabel = itemCreatorLabel(item);
  return (
    <button className="book-card" type="button" data-kind={isSeries(item) ? "series" : candidateType || "work"} onClick={() => onOpen(item)}>
      <Cover item={item} eager={priority} decorative />
      <span className="book-info">
        <span className="book-title-row"><strong className="book-title">{cleanTitle(itemTitle(item))}</strong><span className="book-index" aria-hidden="true">{String(index + 1).padStart(2, "0")}</span></span>
        {creatorLabel ? <small className="book-attribution" title={`作者／社团：${creatorLabel}`}><span>作者／社团</span><strong>{creatorLabel}</strong></small> : null}
        <small className="book-meta"><span className="book-kind">类型 · {kindLabel}</span><span className="book-reading-state">{readingState}</span></small>
        {supplementalContext ? <small className="book-match">{supplementalContext}</small> : null}
      </span>
    </button>
  );
});
