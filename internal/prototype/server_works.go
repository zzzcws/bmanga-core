package prototype

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) handleWorks(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	query := r.URL.Query()
	if _, present := query["action"]; present {
		writeJSONError(w, http.StatusBadRequest, "action filter is not supported")
		return
	}
	limit := clampInt(query.Get("limit"), defaultLimit, minLimit, maxLimit)
	offset := clampInt(query.Get("offset"), 0, 0, 10_000_000)
	sortKey := browseSort(query.Get("sort"))
	q := strings.TrimSpace(query.Get("q"))
	mark := strings.TrimSpace(query.Get("mark"))
	tagKey := tagKeyFromQuery(query.Get("tag"))
	if mark == "" && tagKey == "" {
		total, rows, err := s.queryFastWorks(query, sortKey, limit, offset)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{
			"total":  total,
			"limit":  limit,
			"offset": offset,
			"items":  rows,
		})
		return
	}
	filters := []string{}
	args := []any{}

	addFilter := func(column, key string) {
		value := strings.TrimSpace(query.Get(key))
		if value != "" {
			filters = append(filters, column+" = ?")
			args = append(args, value)
		}
	}
	addFilter("wb.library_key", "library")
	addFilter("wb.candidate_type", "type")
	addFilter("wb.source_kind", "source")
	addFilter("wb.page_count_status", "pageStatus")
	addUserMarkFilter(&filters, mark, "wum")
	addWorkTagFilter(&filters, &args, tagKey)

	if q != "" {
		like := "%" + q + "%"
		filters = append(filters, `(
			COALESCE(mfo_title.field_value, wb.title) LIKE ?
			OR wb.title LIKE ?
			OR wb.relative_path LIKE ?
			OR COALESCE(mfo_sources.field_value, wb.translation_sources) LIKE ?
			OR wb.translation_sources LIKE ?
			OR `+metadataOverrideSearchSQL()+`
			OR si.series_title LIKE ?
		)`)
		args = append(args, like, like, like, like, like, like, like)
	}

	where := whereClause(filters)
	baseFrom := "FROM work_browse wb " + seriesJoinSQL() + " " + workUserMarkJoinSQL() + " " + metadataOverrideJoinSQL() + " " + workListProgressJoinSQL("wb.work_identity_id")
	totalRows, err := s.query("SELECT COUNT(*) AS total "+baseFrom+where, args...)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	selectArgs := append([]any{}, args...)
	selectArgs = append(selectArgs, limit, offset)
	rows, err := s.query(fmt.Sprintf(`
		SELECT
			wb.candidate_id, wb.work_identity_id, wb.library_key, wb.library_name, wb.candidate_type, wb.source_kind, wb.title,
			wb.relative_path, wb.size_bytes, wb.modified_utc, wb.extension, wb.page_count_status, wb.readable_page_count,
			wb.cover_status, wb.cover_kind, wb.translation_sources,
			si.series_title, si.item_role, si.sequence_number,
			%s AS added_utc,
			%s,
			%s,
			%s
		%s
		%s
		%s
		LIMIT ? OFFSET ?
	`, workAddedSQL("wb", "COALESCE((SELECT wi_added.first_seen_at FROM work_identities wi_added WHERE wi_added.current_candidate_id = wb.candidate_id), '')"), workUserMarkSelectSQL(), tagSelectSQL("work"), workListProgressSelectSQL(), baseFrom, where, workOrderSQL(sortKey)), selectArgs...)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.applyMetadataOverrides(rows); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, row := range rows {
		enrichWork(row)
		attachWorkListProgress(row)
	}
	writeJSON(w, map[string]any{
		"total":  intValue(firstRow(totalRows)["total"]),
		"limit":  limit,
		"offset": offset,
		"items":  rows,
	})
}

func (s *Server) queryFastWorks(query url.Values, sortKey string, limit int, offset int) (int, []map[string]any, error) {
	filters := []string{}
	args := []any{}
	q := strings.TrimSpace(query.Get("q"))
	addFilter := func(column, key string) {
		value := strings.TrimSpace(query.Get(key))
		if value != "" {
			filters = append(filters, column+" = ?")
			args = append(args, value)
		}
	}
	addFilter("wc.library_key", "library")
	addFilter("wc.candidate_type", "type")
	addFilter("wc.source_kind", "source")
	countBaseFilters := append([]string{}, filters...)
	countBaseArgs := append([]any{}, args...)
	pageStatus := strings.TrimSpace(query.Get("pageStatus"))
	if pageStatus != "" {
		filters = append(filters, "COALESCE(pc.page_count_status, '') = ?")
		args = append(args, pageStatus)
	}
	where := whereClause(filters)
	countFrom := `
		FROM work_candidates wc
		LEFT JOIN page_counts pc ON pc.candidate_id = wc.candidate_id
	`
	searchJoin := ""
	searchArgs := []any{}
	if q != "" {
		searchJoin, searchArgs = s.fastWorkSearchJoin(q)
	}
	queryArgs := append([]any{}, searchArgs...)
	queryArgs = append(queryArgs, args...)
	workIdentityTitleJoin := `
		LEFT JOIN work_identities wi ON wi.current_candidate_id = wc.candidate_id
		LEFT JOIN metadata_field_overrides mfo_title
			ON mfo_title.work_identity_id = wi.work_identity_id
		   AND mfo_title.field_name = 'title'
		   AND mfo_title.override_status = 'active'
	`
	total := 0
	counted := false
	if q == "" && pageStatus == "" {
		simpleTotal, ok, err := s.countSimpleWorkCandidates(countBaseFilters, countBaseArgs)
		if err != nil {
			return 0, nil, err
		}
		if ok {
			total = simpleTotal
			counted = true
		}
	}
	if !counted {
		countQueryFrom := countFrom + searchJoin
		countRows, err := s.query("SELECT COUNT(*) AS total "+countQueryFrom+where, queryArgs...)
		if err != nil {
			return 0, nil, err
		}
		total = intValue(firstRow(countRows)["total"])
	}

	candidateIDs := []string{}
	usedDefaultIDs := false
	usedAddedSortedIDs := false
	canUseFastDefault := q == "" && sortKey == "" && s.canUseFastDefaultWorkOrder()
	if canUseFastDefault && pageStatus == "" {
		ids, ok, err := s.fetchFastDefaultWorkIDs(countBaseFilters, countBaseArgs, limit, offset)
		if err != nil {
			return 0, nil, err
		}
		if ok {
			candidateIDs = ids
			usedDefaultIDs = true
		}
	}
	if !usedDefaultIDs {
		if canUseFastWorksAddedDescWindow(query, sortKey, offset) {
			ids, ok, err := s.fetchAddedDescWindowedWorkIDs(filters, args, total, limit, offset)
			if err != nil {
				return 0, nil, err
			}
			if ok {
				candidateIDs = ids
				usedAddedSortedIDs = true
			}
		}
	}
	if !usedDefaultIDs && !usedAddedSortedIDs {
		allowAddedFastPath := sortKey == "added_desc" || sortKey == "added_asc"
		includeSeriesTitle := true
		if allowAddedFastPath {
			allowAddedFastPath, includeSeriesTitle = s.addedSortedWorkFastPathOptions(query)
		}
		if allowAddedFastPath {
			ids, ok, err := s.fetchAddedSortedWorkIDs(filters, args, countBaseFilters, countBaseArgs, searchJoin, searchArgs, sortKey, limit, offset, includeSeriesTitle)
			if err != nil {
				return 0, nil, err
			}
			if ok {
				candidateIDs = ids
				usedAddedSortedIDs = true
			}
		}
	}
	if !usedDefaultIDs && !usedAddedSortedIDs {
		selectFrom := countFrom + searchJoin + workIdentityTitleJoin + `
			LEFT JOIN (
				SELECT candidate_id, MAX(series_title) AS series_title
				FROM series_items
				GROUP BY candidate_id
			) si ON si.candidate_id = wc.candidate_id
		`
		orderSQL := fastWorkOrderSQL(sortKey)
		if canUseFastDefault {
			selectFrom = countFrom + searchJoin + `
				LEFT JOIN series_items si ON si.candidate_id = wc.candidate_id
			`
			orderSQL = fastDefaultWorkOrderSQLNoTitleOverride()
		}
		selectArgs := append([]any{}, queryArgs...)
		selectArgs = append(selectArgs, limit, offset)
		idRows, err := s.query(`
			SELECT wc.candidate_id
			`+selectFrom+`
			`+where+`
			`+orderSQL+`
			LIMIT ? OFFSET ?
		`, selectArgs...)
		if err != nil {
			return 0, nil, err
		}
		candidateIDs = make([]string, 0, len(idRows))
		for _, row := range idRows {
			if id := stringValue(row["candidate_id"]); id != "" {
				candidateIDs = append(candidateIDs, id)
			}
		}
	}
	rows, err := s.loadWorkListDetails(candidateIDs)
	if err != nil {
		return 0, nil, err
	}
	return total, rows, nil
}

func (s *Server) fetchFastDefaultWorkIDs(filters []string, args []any, limit int, offset int) ([]string, bool, error) {
	normalFilters := append([]string{}, filters...)
	normalArgs := append([]any{}, args...)
	normalArgs = append(normalArgs, limit, offset)
	rows, err := s.query(`
		SELECT wc.candidate_id
		FROM work_candidates wc INDEXED BY idx_work_candidates_shelf_default_order
		LEFT JOIN series_items si ON si.candidate_id = wc.candidate_id
		`+whereClause(normalFilters)+`
		ORDER BY
			wc.library_key,
			COALESCE(si.series_title, wc.title),
			wc.title
		LIMIT ? OFFSET ?
	`, normalArgs...)
	if err != nil {
		return nil, false, err
	}
	pageIDs := []string{}
	for _, row := range rows {
		if candidateID := stringValue(row["candidate_id"]); candidateID != "" {
			pageIDs = append(pageIDs, candidateID)
		}
	}
	return pageIDs, true, nil
}

func (s *Server) countSimpleWorkCandidates(filters []string, args []any) (int, bool, error) {
	filters = append([]string{}, filters...)
	rows, err := s.query(`
		SELECT COUNT(*) AS total
		FROM work_candidates wc
		`+whereClause(filters), args...)
	if err != nil {
		return 0, true, err
	}
	return intValue(firstRow(rows)["total"]), true, nil
}

func (s *Server) countDefaultShelfWorkCandidates() (int, error) {
	var total int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM work_candidates wc
		WHERE wc.candidate_type = 'doujin'
		   OR NOT EXISTS (
			  SELECT 1
			  FROM series_items si
			  WHERE si.candidate_id = wc.candidate_id
		   )
	`).Scan(&total)
	return total, err
}

func (s *Server) canUseFastDefaultWorkOrder() bool {
	rows, err := s.query(`
		SELECT
			EXISTS (
				SELECT 1
				FROM metadata_field_overrides
				WHERE field_name = 'title'
				  AND override_status = 'active'
			) AS has_title_overrides,
			EXISTS (
				SELECT 1
				FROM (
					SELECT candidate_id
					FROM series_items
					GROUP BY candidate_id
					HAVING COUNT(*) > 1
					LIMIT 1
				)
			) AS has_multi_series_items
	`)
	if err != nil || len(rows) == 0 {
		return false
	}
	row := rows[0]
	return intValue(row["has_title_overrides"]) == 0 && intValue(row["has_multi_series_items"]) == 0
}

func (s *Server) workSourceTimeCacheComplete() bool {
	rows, err := s.query(`
		SELECT
			EXISTS (SELECT 1 FROM work_candidates) AS has_targets,
			(SELECT COUNT(*) FROM source_filesystem_times WHERE target_type = 'work') =
				(SELECT COUNT(*) FROM work_candidates) AS row_counts_match,
			NOT EXISTS (
				SELECT 1
				FROM work_candidates wc
				LEFT JOIN source_filesystem_times sft
				  ON sft.target_type = 'work'
				 AND sft.target_id = wc.candidate_id
				WHERE sft.target_id IS NULL
				LIMIT 1
			) AS covers_works
	`)
	if err != nil || len(rows) == 0 {
		return false
	}
	row := rows[0]
	return intValue(row["has_targets"]) == 1 && intValue(row["row_counts_match"]) == 1 && intValue(row["covers_works"]) == 1
}

func (s *Server) workSourceTimeCacheCoversFilters(filters []string, args []any) bool {
	where := whereClause(filters)
	indexedFilters := append([]string{"sft_coverage.target_type = 'work'"}, filters...)
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, args...)
	rows, err := s.query(`
		SELECT (
			SELECT COUNT(*)
			FROM work_candidates wc
			`+where+`
		) = (
			SELECT COUNT(*)
			FROM source_filesystem_times sft_coverage
			JOIN work_candidates wc ON wc.candidate_id = sft_coverage.target_id
			`+whereClause(indexedFilters)+`
		) AS complete
	`, queryArgs...)
	if err != nil || len(rows) == 0 {
		return false
	}
	return intValue(rows[0]["complete"]) == 1
}

func (s *Server) addedSortedWorkFastPathOptions(query url.Values) (bool, bool) {
	if strings.TrimSpace(query.Get("type")) != "doujin" {
		return true, true
	}
	rows, err := s.query(`
		SELECT EXISTS (
			SELECT 1
			FROM series_items si
			JOIN work_candidates wc ON wc.candidate_id = si.candidate_id
			WHERE wc.candidate_type = 'doujin'
			LIMIT 1
		) AS has_doujin_series
	`)
	if err != nil || len(rows) == 0 {
		return false, false
	}
	if intValue(rows[0]["has_doujin_series"]) != 0 {
		return false, false
	}
	return true, false
}

func mergeWhereFilters(extra []string, filters []string) []string {
	merged := make([]string, 0, len(extra)+len(filters))
	merged = append(merged, extra...)
	merged = append(merged, filters...)
	return merged
}

const (
	fastWorksAddedDescMaxOffset     = 512
	fastWorksAddedDescInitialWindow = 64
	fastWorksAddedDescMaxWindow     = 8192
)

type fastWorksAddedDescLimits struct {
	initialWindow int
	maxWindow     int
}

type fastWorksAddedDescItem struct {
	candidateID string
	addedUTC    string
}

func canUseFastWorksAddedDescWindow(query url.Values, sortKey string, offset int) bool {
	if sortKey != "added_desc" || strings.TrimSpace(query.Get("q")) != "" || offset < 0 || offset > fastWorksAddedDescMaxOffset {
		return false
	}
	if _, present := query["action"]; present {
		return false
	}
	// Sparse scopes often exhaust the bounded recent window before falling
	// back to the legacy query. Keep the optimization on the catalog's dense
	// default/type views so no supported filter pays for both paths.
	for _, key := range []string{"library", "source", "pageStatus"} {
		if strings.TrimSpace(query.Get(key)) != "" {
			return false
		}
	}
	switch strings.TrimSpace(query.Get("type")) {
	case "", "doujin":
		return true
	default:
		return false
	}
}

func (s *Server) fetchAddedDescWindowedWorkIDs(filters []string, args []any, total int, limit int, offset int) ([]string, bool, error) {
	return s.fetchAddedDescWindowedWorkIDsWithLimits(filters, args, total, limit, offset, fastWorksAddedDescLimits{
		initialWindow: fastWorksAddedDescInitialWindow,
		maxWindow:     fastWorksAddedDescMaxWindow,
	})
}

// fetchAddedDescWindowedWorkIDsWithLimits scans the source-time index first,
// then applies the full works filters and title ordering only to a bounded
// recent window. It returns used=false unless the requested prefix is proven
// complete: the cutoff timestamp tie is read in full and every unscanned valid
// source timestamp is strictly older than the last selected item.
func (s *Server) fetchAddedDescWindowedWorkIDsWithLimits(filters []string, args []any, total int, limit int, offset int, limits fastWorksAddedDescLimits) ([]string, bool, error) {
	if limit <= 0 || total <= offset {
		return []string{}, true, nil
	}
	if offset < 0 || limits.initialWindow <= 0 || limits.maxWindow < limits.initialWindow {
		return nil, false, nil
	}
	target := offset + limit
	if target > total {
		target = total
	}
	if target <= 0 || target > limits.maxWindow {
		return nil, false, nil
	}

	exceptionalIDs, bounded, err := s.fastShelfAddedExceptionalWorkIDs()
	if err != nil {
		return nil, false, err
	}
	if !bounded {
		return nil, false, nil
	}

	window := limits.initialWindow
	for window < target && window < limits.maxWindow {
		window *= 2
		if window > limits.maxWindow {
			window = limits.maxWindow
		}
	}
	for {
		cutoff, hasOlder, rawWindowRows, err := s.fastWorksAddedDescBoundary(window)
		if err != nil {
			return nil, false, err
		}
		if rawWindowRows > limits.maxWindow {
			return nil, false, nil
		}
		items, err := s.queryFastWorksAddedDescWindow(filters, args, exceptionalIDs, cutoff, target)
		if err != nil {
			return nil, false, err
		}

		proven := !hasOlder
		if len(items) >= target && cutoff != "" && items[target-1].addedUTC >= cutoff {
			proven = true
		}
		if proven {
			start := offset
			if start > len(items) {
				start = len(items)
			}
			end := start + limit
			if end > len(items) {
				end = len(items)
			}
			ids := make([]string, 0, end-start)
			for _, item := range items[start:end] {
				ids = append(ids, item.candidateID)
			}
			return ids, true, nil
		}
		if window >= limits.maxWindow {
			return nil, false, nil
		}
		nextWindow := window * 2
		if nextWindow > limits.maxWindow {
			nextWindow = limits.maxWindow
		}
		if nextWindow <= window {
			return nil, false, nil
		}
		window = nextWindow
	}
}

func (s *Server) fastWorksAddedDescBoundary(rowLimit int) (string, bool, int, error) {
	if rowLimit <= 0 {
		return "", false, 0, nil
	}
	var cutoff sql.NullString
	var seedCount int
	var seedBoundaryCount int
	var fullBoundaryCount int
	var hasOlder int
	err := s.db.QueryRow(`
		WITH recent_seed AS MATERIALIZED (
			SELECT source_created_utc
			FROM source_filesystem_times INDEXED BY idx_source_filesystem_times_work_added
			WHERE target_type = 'work'
			  AND status = 'ok'
			  AND source_created_utc <> ''
			ORDER BY source_created_utc DESC, target_id DESC
			LIMIT ?
		), boundary AS (
			SELECT COUNT(*) AS seed_count, MIN(source_created_utc) AS cutoff
			FROM recent_seed
		)
		SELECT
			boundary.seed_count,
			boundary.cutoff,
			COALESCE((
				SELECT COUNT(*) FROM recent_seed seed_boundary
				WHERE seed_boundary.source_created_utc = boundary.cutoff
			), 0) AS seed_boundary_count,
			COALESCE((
				SELECT COUNT(*)
				FROM source_filesystem_times full_boundary INDEXED BY idx_source_filesystem_times_work_added
				WHERE full_boundary.target_type = 'work'
				  AND full_boundary.status = 'ok'
				  AND full_boundary.source_created_utc = boundary.cutoff
			), 0) AS full_boundary_count,
			CASE WHEN boundary.cutoff IS NULL THEN 0 ELSE EXISTS (
				SELECT 1
				FROM source_filesystem_times older INDEXED BY idx_source_filesystem_times_work_added
				WHERE older.target_type = 'work'
				  AND older.status = 'ok'
				  AND older.source_created_utc <> ''
				  AND older.source_created_utc < boundary.cutoff
				LIMIT 1
			) END AS has_older
		FROM boundary
	`, rowLimit).Scan(&seedCount, &cutoff, &seedBoundaryCount, &fullBoundaryCount, &hasOlder)
	if err != nil {
		return "", false, 0, err
	}
	rawWindowRows := seedCount - seedBoundaryCount + fullBoundaryCount
	if rawWindowRows < 0 {
		rawWindowRows = 0
	}
	return cutoff.String, hasOlder != 0, rawWindowRows, nil
}

func fastWorksAddedDescWindowSQL(filters []string, exceptionalCount int, hasValidWindow bool) string {
	parts := make([]string, 0, 2)
	if hasValidWindow {
		parts = append(parts, `
			SELECT sft_window.target_id AS candidate_id, sft_window.source_created_utc AS added_utc
			FROM source_filesystem_times sft_window INDEXED BY idx_source_filesystem_times_work_added
			WHERE sft_window.target_type = 'work'
			  AND sft_window.status = 'ok'
			  AND sft_window.source_created_utc <> ''
			  AND sft_window.source_created_utc >= ?
		`)
	}
	if exceptionalCount > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", exceptionalCount), ",")
		parts = append(parts, `
			SELECT
				wc_exception.candidate_id,
				`+joinedWorkAddedSQL("sft_exception", "COALESCE(wi_exception.first_seen_at, '')")+` AS added_utc
			FROM work_candidates wc_exception
			LEFT JOIN source_filesystem_times sft_exception
				ON sft_exception.target_type = 'work'
			   AND sft_exception.target_id = wc_exception.candidate_id
			LEFT JOIN work_identities wi_exception
				ON wi_exception.current_candidate_id = wc_exception.candidate_id
			WHERE wc_exception.candidate_id IN (`+placeholders+`)
			  AND (
				sft_exception.target_id IS NULL
				OR sft_exception.status <> 'ok'
				OR NULLIF(sft_exception.source_created_utc, '') IS NULL
			  )
		`)
	}
	if len(parts) == 0 {
		parts = append(parts, "SELECT '' AS candidate_id, '' AS added_utc WHERE 1 = 0")
	}
	effectiveTitle := "COALESCE(mfo_title.field_value, wc.title)"
	titleSQL := "COALESCE(si.series_title, " + effectiveTitle + ") COLLATE NOCASE, " + effectiveTitle + " COLLATE NOCASE, wc.relative_path COLLATE NOCASE"
	return `
		WITH candidate_window(candidate_id, added_utc) AS MATERIALIZED (
			` + strings.Join(parts, "\nUNION ALL\n") + `
		), series_window AS MATERIALIZED (
			SELECT si_window.candidate_id, MAX(si_window.series_title) AS series_title
			FROM series_items si_window
			WHERE si_window.candidate_id IN (SELECT candidate_id FROM candidate_window)
			GROUP BY si_window.candidate_id
		)
		SELECT wc.candidate_id, candidate_window.added_utc
		FROM candidate_window
		JOIN work_candidates wc ON wc.candidate_id = candidate_window.candidate_id
		LEFT JOIN page_counts pc ON pc.candidate_id = wc.candidate_id
		LEFT JOIN work_identities wi ON wi.current_candidate_id = wc.candidate_id
		LEFT JOIN metadata_field_overrides mfo_title
			ON mfo_title.work_identity_id = wi.work_identity_id
		   AND mfo_title.field_name = 'title'
		   AND mfo_title.override_status = 'active'
		LEFT JOIN series_window si ON si.candidate_id = wc.candidate_id
		` + whereClause(filters) + `
		ORDER BY candidate_window.added_utc DESC, ` + titleSQL + `
		LIMIT ?
	`
}

func (s *Server) queryFastWorksAddedDescWindow(filters []string, args []any, exceptionalIDs []string, cutoff string, target int) ([]fastWorksAddedDescItem, error) {
	if target <= 0 {
		return []fastWorksAddedDescItem{}, nil
	}
	hasValidWindow := cutoff != ""
	queryArgs := make([]any, 0, 1+len(exceptionalIDs)+len(args)+1)
	if hasValidWindow {
		queryArgs = append(queryArgs, cutoff)
	}
	for _, candidateID := range exceptionalIDs {
		queryArgs = append(queryArgs, candidateID)
	}
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, target)
	rows, err := s.db.Query(fastWorksAddedDescWindowSQL(filters, len(exceptionalIDs), hasValidWindow), queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]fastWorksAddedDescItem, 0, target)
	for rows.Next() {
		var candidateID sql.NullString
		var addedUTC sql.NullString
		if err := rows.Scan(&candidateID, &addedUTC); err != nil {
			return nil, err
		}
		items = append(items, fastWorksAddedDescItem{candidateID: candidateID.String, addedUTC: addedUTC.String})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Server) fetchAddedSortedWorkIDs(filters []string, args []any, coverageFilters []string, coverageArgs []any, searchJoin string, searchArgs []any, sortKey string, limit int, offset int, includeSeriesTitle bool) ([]string, bool, error) {
	if sortKey != "added_desc" && sortKey != "added_asc" {
		return nil, false, nil
	}
	if !s.workSourceTimeCacheCoversFilters(coverageFilters, coverageArgs) {
		return nil, false, nil
	}
	direction := "DESC"
	if sortKey == "added_asc" {
		direction = "ASC"
	}
	effectiveTitle := "COALESCE(mfo_title.field_value, wc.title)"
	titleSQL := effectiveTitle + " COLLATE NOCASE, wc.relative_path COLLATE NOCASE"
	seriesJoin := ""
	if includeSeriesTitle {
		titleSQL = "COALESCE(si.series_title, " + effectiveTitle + ") COLLATE NOCASE, " + effectiveTitle + " COLLATE NOCASE, wc.relative_path COLLATE NOCASE"
		seriesJoin = `
			LEFT JOIN (
				SELECT candidate_id, MAX(series_title) AS series_title
				FROM series_items
				GROUP BY candidate_id
			) si ON si.candidate_id = wc.candidate_id
		`
	}
	sourceFrom := `
		FROM source_filesystem_times sft_added
		JOIN work_candidates wc ON wc.candidate_id = sft_added.target_id
		LEFT JOIN page_counts pc ON pc.candidate_id = wc.candidate_id
		LEFT JOIN work_identities wi ON wi.current_candidate_id = wc.candidate_id
		LEFT JOIN metadata_field_overrides mfo_title
			ON mfo_title.work_identity_id = wi.work_identity_id
		   AND mfo_title.field_name = 'title'
		   AND mfo_title.override_status = 'active'
		` + searchJoin + `
		` + seriesJoin + `
	`
	queryArgs := append([]any{}, searchArgs...)
	queryArgs = append(queryArgs, args...)
	okFilters := []string{
		"sft_added.target_type = 'work'",
		"sft_added.status = 'ok'",
		"NULLIF(sft_added.source_created_utc, '') IS NOT NULL",
	}
	missingFilters := []string{
		"sft_added.target_type = 'work'",
		"(sft_added.status <> 'ok' OR NULLIF(sft_added.source_created_utc, '') IS NULL)",
	}
	fetch := func(extraFilters []string, orderSQL string, rowLimit int, rowOffset int) ([]string, error) {
		if rowLimit <= 0 {
			return []string{}, nil
		}
		fetchArgs := append([]any{}, queryArgs...)
		fetchArgs = append(fetchArgs, rowLimit, rowOffset)
		rows, err := s.query(`
			SELECT wc.candidate_id
			`+sourceFrom+`
			`+whereClause(mergeWhereFilters(extraFilters, filters))+`
			`+orderSQL+`
			LIMIT ? OFFSET ?
		`, fetchArgs...)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			if id := stringValue(row["candidate_id"]); id != "" {
				ids = append(ids, id)
			}
		}
		return ids, nil
	}
	candidateIDs, err := fetch(okFilters, "ORDER BY sft_added.source_created_utc "+direction+", "+titleSQL, limit, offset)
	if err != nil {
		return nil, true, err
	}
	if len(candidateIDs) < limit {
		okRows, err := s.query("SELECT COUNT(*) AS count "+sourceFrom+whereClause(mergeWhereFilters(okFilters, filters)), queryArgs...)
		if err != nil {
			return nil, true, err
		}
		okCount := intValue(firstRow(okRows)["count"])
		missingOffset := offset - okCount
		if missingOffset < 0 {
			missingOffset = 0
		}
		ids, err := fetch(missingFilters, "ORDER BY "+titleSQL, limit-len(candidateIDs), missingOffset)
		if err != nil {
			return nil, true, err
		}
		candidateIDs = append(candidateIDs, ids...)
	}
	return candidateIDs, true, nil
}

func (s *Server) loadWorkListDetails(candidateIDs []string) ([]map[string]any, error) {
	if len(candidateIDs) == 0 {
		return []map[string]any{}, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(candidateIDs)), ",")
	args := make([]any, 0, len(candidateIDs)*3)
	for _, id := range candidateIDs {
		args = append(args, id)
	}
	for _, id := range candidateIDs {
		args = append(args, id)
	}
	for _, id := range candidateIDs {
		args = append(args, id)
	}
	rows, err := s.query(`
		SELECT
			wc.candidate_id,
			wi.work_identity_id,
			wc.library_key,
			wc.library_name,
			wc.candidate_type,
			wc.source_kind,
			wc.title,
			wc.relative_path,
			wc.size_bytes,
			wc.modified_utc,
			`+workAddedSQL("wc", "COALESCE(wi.first_seen_at, '')")+` AS added_utc,
			wc.extension,
			COALESCE(pc.page_count_status, '') AS page_count_status,
			COALESCE(pc.readable_page_count, '') AS readable_page_count,
			COALESCE(wcc.cover_status, '') AS cover_status,
			COALESCE(wcc.cover_kind, '') AS cover_kind,
			COALESCE(wts.translation_sources, '') AS translation_sources,
			COALESCE(si.series_title, '') AS series_title,
			COALESCE(si.item_role, '') AS item_role,
			COALESCE(si.sequence_number, '') AS sequence_number,
			COALESCE(wum.read_status, 'unread') AS user_read_status,
			wum.personal_rating AS user_personal_rating,
			COALESCE(wum.favorite, 0) AS user_favorite,
			COALESCE(wum.reread_priority, 0) AS user_reread_priority,
			CASE WHEN COALESCE(wum.notes, '') <> '' THEN 1 ELSE 0 END AS user_has_notes,
			(
				SELECT GROUP_CONCAT(lt.display_name, ',')
				FROM work_tag_links wtl
				JOIN local_tags lt ON lt.tag_key = wtl.tag_key
				WHERE wtl.work_identity_id = wi.work_identity_id
				  AND wtl.reader_profile_key = 'default'
			) AS user_tags,
			`+workListProgressSelectSQL()+`
		FROM work_candidates wc
		LEFT JOIN page_counts pc ON pc.candidate_id = wc.candidate_id
		LEFT JOIN work_cover_candidates wcc INDEXED BY idx_work_cover_candidates_candidate_id ON wcc.candidate_id = wc.candidate_id
		LEFT JOIN (
			SELECT candidate_id, GROUP_CONCAT(DISTINCT translation_group) AS translation_sources
			FROM translation_items
			WHERE candidate_id IN (`+placeholders+`)
			GROUP BY candidate_id
		) wts ON wts.candidate_id = wc.candidate_id
		LEFT JOIN work_identities wi ON wi.current_candidate_id = wc.candidate_id
		LEFT JOIN (
			SELECT candidate_id, MAX(series_title) AS series_title, MAX(item_role) AS item_role, MAX(sequence_number) AS sequence_number
			FROM series_items
			WHERE candidate_id IN (`+placeholders+`)
			GROUP BY candidate_id
		) si ON si.candidate_id = wc.candidate_id
		LEFT JOIN work_user_marks wum
			ON wum.reader_profile_key = 'default'
		   AND wum.work_identity_id = wi.work_identity_id
		`+workListProgressJoinSQL("wi.work_identity_id")+`
		WHERE wc.candidate_id IN (`+placeholders+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	if err := s.applyMetadataOverrides(rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		enrichWork(row)
		attachWorkListProgress(row)
	}
	if err := s.fillMissingStructuredDisplayCreators(rows); err != nil {
		return nil, err
	}
	detailByID := map[string]map[string]any{}
	for _, row := range rows {
		detailByID[stringValue(row["candidate_id"])] = row
	}
	ordered := make([]map[string]any, 0, len(candidateIDs))
	for _, id := range candidateIDs {
		if row := detailByID[id]; row != nil {
			ordered = append(ordered, row)
		}
	}
	return ordered, nil
}

func (s *Server) loadRelatedWorkCardDetails(candidateIDs []string) ([]map[string]any, error) {
	if len(candidateIDs) == 0 {
		return []map[string]any{}, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(candidateIDs)), ",")
	args := make([]any, 0, len(candidateIDs)*2)
	for _, id := range candidateIDs {
		args = append(args, id)
	}
	for _, id := range candidateIDs {
		args = append(args, id)
	}
	rows, err := s.query(`
		SELECT
			wc.candidate_id,
			wi.work_identity_id,
			wc.library_key,
			wc.library_name,
			wc.candidate_type,
			wc.source_kind,
			wc.title,
			wc.relative_path,
			wc.modified_utc,
			COALESCE(NULLIF(sft_added.source_created_utc, ''), wi.first_seen_at, '') AS added_utc,
			wc.extension,
			COALESCE(pc.page_count_status, '') AS page_count_status,
			COALESCE(pc.readable_page_count, '') AS readable_page_count,
			COALESCE(wcc.cover_status, '') AS cover_status,
			COALESCE(wcc.cover_kind, '') AS cover_kind,
			COALESCE(si.series_title, '') AS series_title,
			COALESCE(si.item_role, '') AS item_role,
			COALESCE(si.sequence_number, '') AS sequence_number,
			`+workListProgressSelectSQL()+`
		FROM work_candidates wc
		LEFT JOIN page_counts pc ON pc.candidate_id = wc.candidate_id
		LEFT JOIN work_cover_candidates wcc INDEXED BY idx_work_cover_candidates_candidate_id ON wcc.candidate_id = wc.candidate_id
		LEFT JOIN work_identities wi ON wi.current_candidate_id = wc.candidate_id
		LEFT JOIN source_filesystem_times sft_added
			ON sft_added.target_type = 'work'
		   AND sft_added.target_id = wc.candidate_id
		   AND sft_added.status = 'ok'
		LEFT JOIN (
			SELECT candidate_id, MAX(series_title) AS series_title, MAX(item_role) AS item_role, MAX(sequence_number) AS sequence_number
			FROM series_items
			WHERE candidate_id IN (`+placeholders+`)
			GROUP BY candidate_id
		) si ON si.candidate_id = wc.candidate_id
		`+workListProgressJoinSQL("wi.work_identity_id")+`
		WHERE wc.candidate_id IN (`+placeholders+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	if err := s.applyMetadataOverrides(rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		enrichWork(row)
		attachWorkListProgress(row)
	}
	if err := s.fillMissingStructuredDisplayCreators(rows); err != nil {
		return nil, err
	}
	detailByID := map[string]map[string]any{}
	for _, row := range rows {
		detailByID[stringValue(row["candidate_id"])] = row
	}
	ordered := make([]map[string]any, 0, len(candidateIDs))
	for _, id := range candidateIDs {
		if row := detailByID[id]; row != nil {
			ordered = append(ordered, row)
		}
	}
	return ordered, nil
}

func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	query := r.URL.Query()
	limit := clampInt(query.Get("limit"), defaultLimit, minLimit, maxLimit)
	offset := clampInt(query.Get("offset"), 0, 0, 10_000_000)
	sortKey := browseSort(query.Get("sort"))
	filters := []string{"sg.group_type = 'series_candidate'", seriesHasVisibleMemberSQL("sg")}
	args := []any{}

	if library := strings.TrimSpace(query.Get("library")); library != "" {
		filters = append(filters, "sg.library_key = ?")
		args = append(args, library)
	} else {
		addDefaultSeriesLibraryFilter(&filters, &args, "sg")
	}
	if q := strings.TrimSpace(query.Get("q")); q != "" {
		like := "%" + q + "%"
		filters = append(filters, "(sg.series_title LIKE ? OR sg.group_path LIKE ?)")
		args = append(args, like, like)
	}
	addUserMarkFilter(&filters, query.Get("mark"), "sumark")
	addSeriesTagFilter(&filters, &args, tagKeyFromQuery(query.Get("tag")))

	where := whereClause(filters)
	baseFrom := seriesBaseFromSQL()
	countFrom := "FROM series_groups sg " + seriesUserMarkJoinSQL()
	totalRows, err := s.query("SELECT COUNT(*) AS total "+countFrom+" "+where, args...)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	selectArgs := append([]any{}, args...)
	selectArgs = append(selectArgs, limit, offset)
	rows, err := s.query(fmt.Sprintf(`
		SELECT
			sg.group_id,
			sg.library_key,
			sg.series_title,
			sg.group_path,
			sg.group_type,
			sg.confidence,
			COALESCE(stats.item_count, sg.candidate_count) AS item_count,
			COALESCE(stats.unique_sequence_count, sg.candidate_count) AS unique_sequence_count,
			COALESCE(section_stats.section_count, 1) AS section_count,
			COALESCE(section_stats.multi_section_count, 0) AS multi_section_count,
			COALESCE(section_stats.special_section_count, 0) AS special_section_count,
			COALESCE(stats.counted_pages, 0) AS counted_pages,
			COALESCE(stats.counted_items, 0) AS counted_items,
			COALESCE(stats.latest_modified_utc, '') AS latest_modified_utc,
			%s AS added_utc,
			COALESCE(cover_override.candidate_id, safe_cover.selected_candidate_id, scc.selected_candidate_id) AS selected_candidate_id,
			cover_choice.correction_value AS manual_cover_candidate_id,
			kind_choice.correction_value AS series_kind,
			unit_choice.correction_value AS series_unit,
			COALESCE(cover_override.cover_status, safe_cover.cover_status, scc.cover_status) AS cover_status,
			COALESCE(cover_override.cover_kind, safe_cover.cover_kind, scc.cover_kind) AS cover_kind,
			COALESCE(cover_override.cover_source_path, safe_cover.cover_source_path, scc.cover_source_path) AS cover_source_path,
			COALESCE(cover_override.requires_extraction, safe_cover.requires_extraction, scc.requires_extraction) AS requires_extraction,
			%s,
			%s
		%s
		%s
		%s
		LIMIT ? OFFSET ?
	`, seriesAddedSQL(), seriesUserMarkSelectSQL(), tagSelectSQL("series"), baseFrom, where, seriesOrderSQL(sortKey)), selectArgs...)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, row := range rows {
		enrichSeries(row)
	}
	writeJSON(w, map[string]any{
		"total":  intValue(firstRow(totalRows)["total"]),
		"limit":  limit,
		"offset": offset,
		"items":  rows,
	})
}
