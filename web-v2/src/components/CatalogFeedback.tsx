export function Status({ kind = "loading", children }: { kind?: "loading" | "error" | "empty"; children: string }) {
  return <div className={`status ${kind}`} role={kind === "error" ? "alert" : "status"}>{children}</div>;
}

export function CatalogSkeleton({ count = 12, compact = false, className = "" }: { count?: number; compact?: boolean; className?: string }) {
  return (
    <>
      <span className="sr-only" role="status">正在加载作品…</span>
      <div className={`book-grid catalog-grid catalog-skeleton ${compact ? "compact-grid" : ""} ${className}`.trim()} aria-hidden="true">
        {Array.from({ length: count }, (_, index) => (
          <div className="skeleton-book" key={index}>
            <span className="skeleton-cover" />
            <span className="skeleton-line" />
            <span className="skeleton-line short" />
          </div>
        ))}
      </div>
    </>
  );
}
