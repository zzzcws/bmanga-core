package prototype

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleShelf(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	started := time.Now()
	phaseStarted := started
	timingParts := []string{}
	recordTiming := func(name string) {
		elapsedMs := float64(time.Since(phaseStarted).Microseconds()) / 1000.0
		timingParts = append(timingParts, fmt.Sprintf("%s;dur=%.1f", name, elapsedMs))
		phaseStarted = time.Now()
	}
	finishTiming := func() {
		timingParts = append([]string{fmt.Sprintf("app;dur=%.1f", float64(time.Since(started).Microseconds())/1000.0)}, timingParts...)
		w.Header().Set("Server-Timing", strings.Join(timingParts, ", "))
	}
	query := r.URL.Query()
	if _, present := query["action"]; present {
		writeJSONError(w, http.StatusBadRequest, "action filter is not supported")
		return
	}
	limit := clampInt(query.Get("limit"), defaultLimit, minLimit, maxLimit)
	offset := clampInt(query.Get("offset"), 0, 0, 10_000_000)
	q := strings.TrimSpace(query.Get("q"))
	library := strings.TrimSpace(query.Get("library"))
	source := strings.TrimSpace(query.Get("source"))
	pageStatus := strings.TrimSpace(query.Get("pageStatus"))
	mark := strings.TrimSpace(query.Get("mark"))
	sortKey := browseSort(query.Get("sort"))
	tagKey := tagKeyFromQuery(query.Get("tag"))

	if mark == "" && tagKey == "" {
		s.handleFastShelf(w, limit, offset, library, source, pageStatus, q, sortKey, recordTiming, finishTiming)
		return
	}

	items := []map[string]any{}
	if library != "doujin-lanraragi" && (source == "" || source == "image_folder") {
		rows, err := s.queryShelfSeries(library, q, pageStatus, mark, tagKey)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		recordTiming("shelfSeries")
		for _, row := range rows {
			enrichSeries(row)
			row["shelf_type"] = "series"
			items = append(items, row)
		}
	}

	rows, err := s.queryShelfWorks(library, source, pageStatus, q, mark, tagKey)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	recordTiming("shelfWorks")
	if err := s.applyMetadataOverrides(rows); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	recordTiming("shelfOverrides")
	for _, row := range rows {
		enrichWork(row)
		attachWorkListProgress(row)
		row["shelf_type"] = "work"
		items = append(items, row)
	}

	sortShelfItems(items, sortKey)
	recordTiming("shelfSort")

	total := len(items)
	end := offset + limit
	if offset > total {
		offset = total
	}
	if end > total {
		end = total
	}
	pageItems := items[offset:end]
	if err := s.fillMissingStructuredDisplayCreators(pageItems); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	recordTiming("shelfCreators")
	finishTiming()
	writeJSON(w, map[string]any{
		"total":  total,
		"limit":  limit,
		"offset": offset,
		"items":  pageItems,
	})
}

func (s *Server) handleFastShelf(w http.ResponseWriter, limit, offset int, library, source, pageStatus, q, sortKey string, recordTiming func(string), finishTiming func()) {
	pageItems, total, effectiveOffset, err := s.queryShelfPageLiteItems(library, source, pageStatus, q, sortKey, limit, offset, recordTiming)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	seriesIDs := []string{}
	fastIDs := []string{}
	for _, item := range pageItems {
		if item.shelfType == "series" && item.groupID != "" {
			seriesIDs = append(seriesIDs, item.groupID)
		}
		if item.shelfType == "work" && item.candidateID != "" {
			fastIDs = append(fastIDs, item.candidateID)
		}
	}
	seriesByID := map[string]map[string]any{}
	if len(seriesIDs) > 0 {
		details, err := s.loadShelfSeriesDetails(seriesIDs)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, detail := range details {
			detail["shelf_type"] = "series"
			seriesByID[stringValue(detail["group_id"])] = detail
		}
	}
	recordTiming("shelfSeriesDetails")
	detailByID := map[string]map[string]any{}
	if len(fastIDs) > 0 {
		details, err := s.loadWorkListDetails(fastIDs)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, detail := range details {
			detail["shelf_type"] = "work"
			detailByID[stringValue(detail["candidate_id"])] = detail
		}
	}
	recordTiming("shelfWorkDetails")
	responseItems := make([]map[string]any, 0, len(pageItems))
	for _, item := range pageItems {
		if item.shelfType == "series" {
			if detail := seriesByID[item.groupID]; detail != nil {
				responseItems = append(responseItems, detail)
			} else if item.data != nil {
				responseItems = append(responseItems, item.data)
			}
			continue
		}
		if detail := detailByID[item.candidateID]; detail != nil {
			responseItems = append(responseItems, detail)
			continue
		}
		responseItems = append(responseItems, map[string]any{
			"shelf_type":          "work",
			"candidate_id":        item.candidateID,
			"library_key":         item.libraryKey,
			"title":               item.title,
			"display_title":       item.title,
			"modified_utc":        item.modified,
			"added_utc":           item.added,
			"readable_page_count": item.pages,
		})
	}
	recordTiming("shelfAssemble")
	finishTiming()
	writeJSON(w, map[string]any{
		"total":  total,
		"limit":  limit,
		"offset": effectiveOffset,
		"items":  responseItems,
	})
}

func (s *Server) queryShelfSeries(library, q, pageStatus, mark, tagKey string) ([]map[string]any, error) {
	filters, args := shelfSeriesFilters(library, q, pageStatus, mark, tagKey)
	return s.queryShelfSeriesRows(filters, args)
}

func shelfSeriesFilters(library, q, pageStatus, mark, tagKey string) ([]string, []any) {
	filters := []string{"sg.group_type = 'series_candidate'", seriesHasVisibleMemberSQL("sg")}
	args := []any{}
	if library != "" {
		filters = append(filters, "sg.library_key = ?")
		args = append(args, library)
	} else {
		addDefaultSeriesLibraryFilter(&filters, &args, "sg")
	}
	if q != "" {
		like := "%" + q + "%"
		filters = append(filters, "(sg.series_title LIKE ? OR sg.group_path LIKE ?)")
		args = append(args, like, like)
	}
	if pageStatus == "counted" {
		filters = append(filters, "COALESCE(stats.counted_items, 0) > 0")
	} else if pageStatus != "" {
		filters = append(filters, "1 = 0")
	}
	addUserMarkFilter(&filters, mark, "sumark")
	addSeriesTagFilter(&filters, &args, tagKey)
	return filters, args
}

func (s *Server) queryShelfSeriesRows(filters []string, args []any) ([]map[string]any, error) {
	return s.query(shelfSeriesSelectSQL("", seriesBaseFromSQL(), filters), args...)
}

func shelfSeriesSelectSQL(prefixSQL string, baseFromSQL string, filters []string) string {
	return fmt.Sprintf(`
		%s
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
	`, prefixSQL, seriesAddedSQL(), seriesUserMarkSelectSQL(), tagSelectSQL("series"), baseFromSQL, whereClause(filters))
}

func (s *Server) loadShelfSeriesDetails(groupIDs []string) ([]map[string]any, error) {
	if len(groupIDs) == 0 {
		return []map[string]any{}, nil
	}
	valuePlaceholders := strings.TrimRight(strings.Repeat("(?),", len(groupIDs)), ",")
	filters := []string{
		"sg.group_type = 'series_candidate'",
		seriesHasVisibleMemberSQL("sg"),
	}
	args := make([]any, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		args = append(args, groupID)
	}
	rows, err := s.query(shelfSeriesSelectSQL(
		"WITH selected_groups(group_id) AS (VALUES "+valuePlaceholders+")",
		selectedSeriesBaseFromSQL(),
		filters,
	), args...)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		enrichSeries(row)
	}
	return rows, nil
}

func shelfIncludesSeries(library, source string) bool {
	return library != "doujin-lanraragi" &&
		(source == "" || source == "image_folder")
}

func (s *Server) queryShelfSeriesLiteItems(library, q, pageStatus string) ([]shelfLiteItem, error) {
	filters, args := shelfSeriesFilters(library, q, pageStatus, "", "")
	rows, err := s.query(fmt.Sprintf(`
		SELECT
			sg.group_id,
			sg.library_key,
			sg.series_title,
			sg.group_path,
			COALESCE(stats.counted_pages, 0) AS counted_pages,
			COALESCE(stats.latest_modified_utc, '') AS latest_modified_utc,
			%s AS added_utc
		FROM series_groups sg
		LEFT JOIN series_identities sid ON sid.current_group_id = sg.group_id
		LEFT JOIN (
			SELECT
				si.group_id,
				SUM(CASE WHEN pc.page_count_status = 'counted' THEN CAST(pc.readable_page_count AS INTEGER) ELSE 0 END) AS counted_pages,
				SUM(CASE WHEN pc.page_count_status = 'counted' THEN 1 ELSE 0 END) AS counted_items,
				MAX(wc.modified_utc) AS latest_modified_utc
			FROM series_items si
			JOIN work_browse stats_wb ON stats_wb.candidate_id = si.candidate_id
			LEFT JOIN page_counts pc ON pc.candidate_id = si.candidate_id
			LEFT JOIN work_candidates wc ON wc.candidate_id = si.candidate_id
			GROUP BY si.group_id
		) stats ON stats.group_id = sg.group_id
		%s
	`, seriesAddedSQL(), whereClause(filters)), args...)
	if err != nil {
		return nil, err
	}
	items := make([]shelfLiteItem, 0, len(rows))
	for _, row := range rows {
		groupID := stringValue(row["group_id"])
		title := stringValue(row["series_title"])
		row["display_title"] = title
		row["shelf_type"] = "series"
		items = append(items, shelfLiteItem{
			shelfType:  "series",
			groupID:    groupID,
			libraryKey: stringValue(row["library_key"]),
			title:      title,
			titleLower: sqliteNoCaseKey(title),
			pathKey:    sqliteNoCaseKey(stringValue(row["group_path"])),
			modified:   stringValue(row["latest_modified_utc"]),
			added:      stringValue(row["added_utc"]),
			pages:      intValue(row["counted_pages"]),
			data:       row,
		})
	}
	return items, nil
}

func (s *Server) queryShelfPageLiteItems(library, source, pageStatus, q, sortKey string, limit, requestedOffset int, recordTiming func(string)) ([]shelfLiteItem, int, int, error) {
	record := func(name string) {
		if recordTiming != nil {
			recordTiming(name)
		}
	}
	seriesItems := []shelfLiteItem{}
	if shelfIncludesSeries(library, source) {
		var err error
		seriesItems, err = s.queryShelfSeriesLiteItems(library, q, pageStatus)
		if err != nil {
			return nil, 0, 0, err
		}
	}
	record("shelfSeriesLite")

	seriesTotal := len(seriesItems)
	workOffset := requestedOffset - seriesTotal
	if workOffset < 0 {
		workOffset = 0
	}
	workLimit := limit + seriesTotal
	if workLimit < limit {
		workLimit = limit
	}
	workItems, workTotal, err := s.queryShelfWorkLiteItems(library, source, pageStatus, q, sortKey, workLimit, workOffset)
	if err != nil {
		return nil, 0, 0, err
	}
	total := workTotal + seriesTotal
	effectiveOffset := requestedOffset
	if effectiveOffset > total {
		effectiveOffset = total
	}
	effectiveWorkOffset := effectiveOffset - seriesTotal
	if effectiveWorkOffset < 0 {
		effectiveWorkOffset = 0
	}
	if effectiveWorkOffset != workOffset {
		workOffset = effectiveWorkOffset
		workItems, workTotal, err = s.queryShelfWorkLiteItems(library, source, pageStatus, q, sortKey, workLimit, workOffset)
		if err != nil {
			return nil, 0, 0, err
		}
		total = workTotal + seriesTotal
	}
	record("shelfWorkWindow")

	merged := make([]shelfLiteItem, 0, len(seriesItems)+len(workItems))
	merged = append(merged, seriesItems...)
	merged = append(merged, workItems...)
	sortShelfLiteItems(merged, sortKey)
	pageStart := effectiveOffset - workOffset
	if pageStart < 0 {
		pageStart = 0
	}
	if pageStart > len(merged) {
		pageStart = len(merged)
	}
	pageEnd := pageStart + limit
	if pageEnd > len(merged) {
		pageEnd = len(merged)
	}
	page := append([]shelfLiteItem{}, merged[pageStart:pageEnd]...)
	record("shelfMerge")
	return page, total, effectiveOffset, nil
}

func (s *Server) queryShelfWorkLiteItems(library, source, pageStatus, q, sortKey string, limit, offset int) ([]shelfLiteItem, int, error) {
	return s.queryShelfWorkLiteItemsWithFastPath(library, source, pageStatus, q, sortKey, limit, offset, true)
}

func (s *Server) queryShelfWorkLiteItemsWithFastPath(library, source, pageStatus, q, sortKey string, limit, offset int, allowFastPath bool) ([]shelfLiteItem, int, error) {
	filters := []string{"(wc.candidate_type = 'doujin' OR NOT EXISTS (SELECT 1 FROM series_items si WHERE si.candidate_id = wc.candidate_id))"}
	args := []any{}
	if library != "" {
		filters = append(filters, "wc.library_key = ?")
		args = append(args, library)
	}
	if source != "" {
		filters = append(filters, "wc.source_kind = ?")
		args = append(args, source)
	}
	countBaseFilters := append([]string{}, filters...)
	countBaseArgs := append([]any{}, args...)
	if pageStatus != "" {
		filters = append(filters, "COALESCE(pc.page_count_status, '') = ?")
		args = append(args, pageStatus)
	}

	searchJoin := ""
	searchArgs := []any{}
	if q != "" {
		searchJoin, searchArgs = s.fastWorkSearchJoin(q)
	}
	queryArgs := append([]any{}, searchArgs...)
	queryArgs = append(queryArgs, args...)

	var total int
	counted := false
	if q != "" {
		searchTotal, err := s.countSearchShelfWorkLiteItems(filters, args, q)
		if err != nil {
			return nil, 0, err
		}
		total = searchTotal
		counted = true
	}
	if !counted && q == "" && pageStatus == "" {
		if library == "" && source == "" {
			simpleTotal, err := s.countDefaultShelfWorkCandidates()
			if err != nil {
				return nil, 0, err
			}
			total = simpleTotal
			counted = true
		} else {
			simpleTotal, ok, err := s.countSimpleWorkCandidates(countBaseFilters, countBaseArgs)
			if err != nil {
				return nil, 0, err
			}
			if ok {
				total = simpleTotal
				counted = true
			}
		}
	}
	if !counted {
		if err := s.db.QueryRow(`
			SELECT COUNT(*)
			FROM work_candidates wc
			LEFT JOIN page_counts pc ON pc.candidate_id = wc.candidate_id
			`+searchJoin+`
			`+whereClause(filters), queryArgs...).Scan(&total); err != nil {
			return nil, 0, err
		}
	}

	if allowFastPath && q == "" && pageStatus == "" {
		items, usedFastPath, err := s.queryFastShelfSortedKeepItems(countBaseFilters, countBaseArgs, sortKey, limit, offset)
		if err != nil {
			return nil, 0, err
		}
		if usedFastPath {
			return items, total, nil
		}
	}

	queryArgsWithLimit := append([]any{}, queryArgs...)
	queryArgsWithLimit = append(queryArgsWithLimit, limit)
	limitSQL := "LIMIT ?"
	if offset > 0 {
		limitSQL += " OFFSET ?"
		queryArgsWithLimit = append(queryArgsWithLimit, offset)
	}
	needsAddedSort := sortKey == "added_desc" || sortKey == "added_asc"
	sourceTimeJoin := ""
	addedSelectSQL := "COALESCE(wi.first_seen_at, '')"
	if needsAddedSort {
		if q != "" {
			addedItems, usedAddedSort, err := s.fetchAddedSortedSearchShelfWorkItems(filters, args, q, sortKey, limit, offset)
			if err != nil {
				return nil, 0, err
			}
			if usedAddedSort {
				return addedItems, total, nil
			}
		}
		sourceTimeJoin = `
		LEFT JOIN source_filesystem_times sft_added
			ON sft_added.target_type = 'work'
		   AND sft_added.target_id = wc.candidate_id`
		addedSelectSQL = joinedWorkAddedSQL("sft_added", "wi.first_seen_at")
	}
	addedItems, usedAddedSort, err := s.fetchAddedSortedShelfWorkItems(filters, args, searchJoin, searchArgs, sortKey, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	if usedAddedSort {
		return addedItems, total, nil
	}
	rows, err := s.db.Query(`
		SELECT
			wc.candidate_id,
			wc.library_key,
			COALESCE(mfo_title.field_value, wc.title) AS sort_title,
			wc.relative_path,
			wc.modified_utc,
			`+addedSelectSQL+` AS added_utc,
			COALESCE(pc.readable_page_count, 0) AS readable_page_count
		FROM work_candidates wc
		LEFT JOIN page_counts pc ON pc.candidate_id = wc.candidate_id
		LEFT JOIN work_identities wi ON wi.current_candidate_id = wc.candidate_id
		`+sourceTimeJoin+`
		LEFT JOIN metadata_field_overrides mfo_title
			ON mfo_title.work_identity_id = wi.work_identity_id
		   AND mfo_title.field_name = 'title'
		   AND mfo_title.override_status = 'active'
		`+searchJoin+`
		`+whereClause(filters)+`
		`+fastShelfWorkOrderSQL(sortKey)+`
		`+limitSQL, queryArgsWithLimit...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := []shelfLiteItem{}
	for rows.Next() {
		var candidateID sql.NullString
		var libraryKey sql.NullString
		var title sql.NullString
		var relativePath sql.NullString
		var modified sql.NullString
		var added sql.NullString
		var pages any
		if err := rows.Scan(&candidateID, &libraryKey, &title, &relativePath, &modified, &added, &pages); err != nil {
			return nil, 0, err
		}
		titleValue := title.String
		items = append(items, shelfLiteItem{
			shelfType:   "work",
			candidateID: candidateID.String,
			libraryKey:  libraryKey.String,
			title:       titleValue,
			titleLower:  sqliteNoCaseKey(titleValue),
			pathKey:     sqliteNoCaseKey(relativePath.String),
			modified:    modified.String,
			added:       added.String,
			pages:       intValue(normalizeDBValue(pages)),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Server) countSearchShelfWorkLiteItems(filters []string, args []any, q string) (int, error) {
	searchSQL, searchArgs := s.fastWorkSearchMatchQuery(q)
	if strings.TrimSpace(searchSQL) == "" {
		return 0, nil
	}
	queryArgs := append([]any{}, searchArgs...)
	queryArgs = append(queryArgs, args...)
	var total int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM (`+searchSQL+`) search_match
		JOIN work_candidates wc ON wc.candidate_id = search_match.candidate_id
		LEFT JOIN page_counts pc ON pc.candidate_id = wc.candidate_id
		`+whereClause(filters), queryArgs...).Scan(&total)
	return total, err
}

func (s *Server) shelfScopeHasTitleOverrides(filters []string, args []any) (bool, error) {
	scopePredicate := strings.TrimPrefix(strings.TrimSpace(whereClause(filters)), "WHERE ")
	if scopePredicate == "" {
		scopePredicate = "1 = 1"
	}
	var exists int
	err := s.db.QueryRow(`
		SELECT EXISTS (
			SELECT 1
			FROM metadata_field_overrides mfo_scope
			JOIN work_identities wi_scope ON wi_scope.work_identity_id = mfo_scope.work_identity_id
			JOIN work_candidates wc ON wc.candidate_id = wi_scope.current_candidate_id
			WHERE mfo_scope.field_name = 'title'
			  AND mfo_scope.override_status = 'active'
			  AND `+scopePredicate+`
		) AS has_title_overrides
	`, args...).Scan(&exists)
	return exists != 0, err
}

func (s *Server) workPageCountCacheComplete() bool {
	rows, err := s.query(`
		SELECT
			(SELECT COUNT(*) FROM work_candidates) =
				(SELECT COUNT(*) FROM page_counts) AS row_counts_match,
			NOT EXISTS (
				SELECT 1
				FROM work_candidates wc
				LEFT JOIN page_counts pc ON pc.candidate_id = wc.candidate_id
				WHERE pc.candidate_id IS NULL
				LIMIT 1
			) AS covers_works
	`)
	if err != nil || len(rows) == 0 {
		return false
	}
	row := rows[0]
	return intValue(row["row_counts_match"]) == 1 && intValue(row["covers_works"]) == 1
}

func (s *Server) queryFastShelfSortedKeepItems(filters []string, args []any, sortKey string, limit, offset int) ([]shelfLiteItem, bool, error) {
	hasTitleOverrides, err := s.shelfScopeHasTitleOverrides(filters, args)
	if err != nil {
		return nil, false, err
	}
	if hasTitleOverrides {
		return nil, false, nil
	}
	switch sortKey {
	case "":
		items, err := s.queryFastShelfDefaultWorkLiteItems(filters, args, limit, offset)
		return items, true, err
	case "title_asc", "title_desc":
		items, err := s.queryFastShelfTitleKeepItems(filters, args, sortKey, limit, offset)
		return items, true, err
	case "added_desc", "added_asc":
		return s.queryFastShelfAddedKeepItems(filters, args, sortKey, limit, offset)
	case "pages_desc":
		if !s.workPageCountCacheComplete() {
			return nil, false, nil
		}
		items, err := s.queryFastShelfPagesDescKeepItems(filters, args, limit, offset)
		return items, true, err
	default:
		return nil, false, nil
	}
}

func fastShelfVisibleKeepFilters(filters []string) []string {
	return append([]string{}, filters...)
}

func fastShelfKeepFilterPlan(filters []string, offset int) (string, []string) {
	_ = offset
	return "", fastShelfVisibleKeepFilters(filters)
}

func shelfLimitOffsetSQL(args []any, limit, offset int) ([]any, string) {
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit)
	limitSQL := "LIMIT ?"
	if offset > 0 {
		limitSQL += " OFFSET ?"
		queryArgs = append(queryArgs, offset)
	}
	return queryArgs, limitSQL
}

func (s *Server) queryFastShelfTitleKeepItems(filters []string, args []any, sortKey string, limit, offset int) ([]shelfLiteItem, error) {
	if limit <= 0 {
		return []shelfLiteItem{}, nil
	}
	prefixSQL, filters := fastShelfKeepFilterPlan(filters, offset)
	queryArgs, limitSQL := shelfLimitOffsetSQL(args, limit, offset)
	orderSQL := "ORDER BY wc.title COLLATE NOCASE, wc.relative_path COLLATE NOCASE, wc.candidate_id, wc.library_key"
	if sortKey == "title_desc" {
		orderSQL = "ORDER BY wc.title COLLATE NOCASE DESC, wc.relative_path COLLATE NOCASE DESC, wc.candidate_id DESC, wc.library_key DESC"
	}
	rows, err := s.db.Query(prefixSQL+`
		SELECT
			wc.candidate_id,
			wc.library_key,
			wc.title AS sort_title,
			wc.relative_path,
			wc.modified_utc,
			'' AS added_utc,
			0 AS readable_page_count
		FROM work_candidates wc INDEXED BY idx_work_candidates_shelf_title_order
		`+whereClause(filters)+`
		`+orderSQL+`
		`+limitSQL, queryArgs...)
	if err != nil {
		return nil, err
	}
	return scanShelfLiteRows(rows)
}

func fastShelfAddedKeepFromSQL() string {
	return `
		FROM source_filesystem_times sft_added INDEXED BY idx_source_filesystem_times_work_added
		CROSS JOIN work_candidates wc INDEXED BY idx_work_candidates_candidate_id
			ON wc.candidate_id = sft_added.target_id
	`
}

// fastShelfAddedExceptionalLimit bounds the side stream used when a small
// number of works do not have a usable source-filesystem timestamp. The main
// stream remains index-backed; larger gaps deliberately fall back to the
// legacy query instead of turning this optimization into an unbounded scan.
const fastShelfAddedExceptionalLimit = 1024

func (s *Server) fastShelfAddedExceptionalWorkIDs() ([]string, bool, error) {
	collect := func(query string, limit int) ([]string, error) {
		rows, err := s.db.Query(query, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		ids := make([]string, 0, limit)
		for rows.Next() {
			var candidateID string
			if err := rows.Scan(&candidateID); err != nil {
				return nil, err
			}
			ids = append(ids, candidateID)
		}
		return ids, rows.Err()
	}

	// Existing-but-invalid rows are ordered by the covering source-time index.
	// Joining work_candidates excludes irrelevant orphan cache rows.
	ids, err := collect(`
		SELECT sft_added.target_id
		FROM source_filesystem_times sft_added INDEXED BY idx_source_filesystem_times_work_added
		CROSS JOIN work_candidates wc INDEXED BY idx_work_candidates_candidate_id
			ON wc.candidate_id = sft_added.target_id
		WHERE sft_added.target_type = 'work'
		  AND (sft_added.status <> 'ok' OR NULLIF(sft_added.source_created_utc, '') IS NULL)
		LIMIT ?
	`, fastShelfAddedExceptionalLimit+1)
	if err != nil {
		return nil, false, err
	}
	if len(ids) > fastShelfAddedExceptionalLimit {
		return nil, false, nil
	}

	// EXCEPT walks the two candidate-id indexes in set order and avoids one
	// random source-time lookup per work, which is costly on the large catalog.
	missingLimit := fastShelfAddedExceptionalLimit - len(ids) + 1
	missingIDs, err := collect(`
		SELECT candidate_id FROM work_candidates
		EXCEPT
		SELECT target_id FROM source_filesystem_times WHERE target_type = 'work'
		LIMIT ?
	`, missingLimit)
	if err != nil {
		return nil, false, err
	}
	ids = append(ids, missingIDs...)
	if len(ids) > fastShelfAddedExceptionalLimit {
		return nil, false, nil
	}
	return ids, true, nil
}

func (s *Server) queryFastShelfAddedKeepItems(filters []string, args []any, sortKey string, limit, offset int) ([]shelfLiteItem, bool, error) {
	if limit <= 0 {
		return []shelfLiteItem{}, true, nil
	}
	prefixSQL, filters := fastShelfKeepFilterPlan(filters, offset)
	direction := "DESC"
	if sortKey == "added_asc" {
		direction = "ASC"
	}

	// Discover exceptional IDs with a cheap global coverage scan first. Only
	// the bounded result is then subjected to the shelf's correlated visibility,
	// series, library, and source filters.
	exceptionalIDs, bounded, err := s.fastShelfAddedExceptionalWorkIDs()
	if err != nil {
		return nil, false, err
	}
	if !bounded {
		return nil, false, nil
	}
	exceptionalItems := []shelfLiteItem{}
	if len(exceptionalIDs) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(exceptionalIDs)), ",")
		exceptionalFilters := []string{
			"wc.candidate_id IN (" + placeholders + ")",
			"(sft_added.target_id IS NULL OR sft_added.status <> 'ok' OR NULLIF(sft_added.source_created_utc, '') IS NULL)",
		}
		exceptionalArgs := make([]any, 0, len(exceptionalIDs)+len(args))
		for _, candidateID := range exceptionalIDs {
			exceptionalArgs = append(exceptionalArgs, candidateID)
		}
		exceptionalArgs = append(exceptionalArgs, args...)
		exceptionalRows, err := s.db.Query(prefixSQL+`
			SELECT
				wc.candidate_id,
				wc.library_key,
				wc.title AS sort_title,
				wc.relative_path,
				wc.modified_utc,
				`+joinedWorkAddedSQL("sft_added", "wi.first_seen_at")+` AS added_utc,
				0 AS readable_page_count
			FROM work_candidates wc INDEXED BY idx_work_candidates_candidate_id
			LEFT JOIN source_filesystem_times sft_added
				ON sft_added.target_type = 'work'
			   AND sft_added.target_id = wc.candidate_id
			LEFT JOIN work_identities wi ON wi.current_candidate_id = wc.candidate_id
			`+whereClause(mergeWhereFilters(exceptionalFilters, filters)), exceptionalArgs...)
		if err != nil {
			return nil, false, err
		}
		exceptionalItems, err = scanShelfLiteRows(exceptionalRows)
		if err != nil {
			return nil, false, err
		}
	}

	fromSQL := fastShelfAddedKeepFromSQL()
	okFilters := []string{
		"sft_added.target_type = 'work'",
		"sft_added.status = 'ok'",
		"NULLIF(sft_added.source_created_utc, '') IS NOT NULL",
	}
	fetch := func(rowLimit, rowOffset int) ([]shelfLiteItem, error) {
		if rowLimit <= 0 {
			return []shelfLiteItem{}, nil
		}
		queryArgs, limitSQL := shelfLimitOffsetSQL(args, rowLimit, rowOffset)
		rows, err := s.db.Query(prefixSQL+`
			SELECT
				wc.candidate_id,
				wc.library_key,
				wc.title AS sort_title,
				wc.relative_path,
				wc.modified_utc,
				sft_added.source_created_utc AS added_utc,
				0 AS readable_page_count
			`+fromSQL+`
			`+whereClause(mergeWhereFilters(okFilters, filters))+`
			ORDER BY sft_added.source_created_utc `+direction+`, wc.title COLLATE NOCASE, wc.relative_path COLLATE NOCASE, wc.candidate_id, wc.library_key
			`+limitSQL, queryArgs...)
		if err != nil {
			return nil, err
		}
		return scanShelfLiteRows(rows)
	}

	// With E exceptional rows, a valid row on the requested page can move by
	// at most E positions. Reading [offset-E, offset+limit) from the indexed
	// stream is therefore sufficient for an exact stable merge at any depth.
	validOffset := offset - len(exceptionalItems)
	if validOffset < 0 {
		validOffset = 0
	}
	validLimit := offset + limit - validOffset
	validItems, err := fetch(validLimit, validOffset)
	if err != nil {
		return nil, false, err
	}
	merged := make([]shelfLiteItem, 0, len(validItems)+len(exceptionalItems))
	merged = append(merged, validItems...)
	merged = append(merged, exceptionalItems...)
	sortShelfLiteItems(merged, sortKey)
	pageStart := offset - validOffset
	if pageStart < 0 {
		pageStart = 0
	}
	if pageStart > len(merged) {
		pageStart = len(merged)
	}
	pageEnd := pageStart + limit
	if pageEnd > len(merged) {
		pageEnd = len(merged)
	}
	return append([]shelfLiteItem{}, merged[pageStart:pageEnd]...), true, nil
}

func fastShelfPagesDescKeepFromSQL() string {
	return `
		FROM page_counts pc INDEXED BY idx_page_counts_shelf_pages_desc_v2
		CROSS JOIN work_candidates wc INDEXED BY idx_work_candidates_candidate_id
			ON wc.candidate_id = pc.candidate_id
	`
}

func (s *Server) queryFastShelfPagesDescKeepItems(filters []string, args []any, limit, offset int) ([]shelfLiteItem, error) {
	if limit <= 0 {
		return []shelfLiteItem{}, nil
	}
	prefixSQL, filters := fastShelfKeepFilterPlan(filters, offset)
	queryArgs, limitSQL := shelfLimitOffsetSQL(args, limit, offset)
	rows, err := s.db.Query(prefixSQL+`
		SELECT
			wc.candidate_id,
			wc.library_key,
			wc.title AS sort_title,
			wc.relative_path,
			wc.modified_utc,
			'' AS added_utc,
			COALESCE(pc.readable_page_count, 0) AS readable_page_count
		`+fastShelfPagesDescKeepFromSQL()+`
		`+whereClause(filters)+`
		ORDER BY
			CAST(COALESCE(pc.readable_page_count, 0) AS INTEGER) DESC,
			wc.title COLLATE NOCASE,
			wc.relative_path COLLATE NOCASE,
			wc.candidate_id,
			wc.library_key
		`+limitSQL, queryArgs...)
	if err != nil {
		return nil, err
	}
	return scanShelfLiteRows(rows)
}

func (s *Server) queryFastShelfDefaultWorkLiteItems(filters []string, args []any, limit, offset int) ([]shelfLiteItem, error) {
	if limit <= 0 {
		return []shelfLiteItem{}, nil
	}
	return s.queryFastShelfDefaultKeepItems(filters, args, limit, offset)
}

func (s *Server) queryFastShelfDefaultKeepItems(filters []string, args []any, limit, offset int) ([]shelfLiteItem, error) {
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit)
	limitSQL := "LIMIT ?"
	if offset > 0 {
		limitSQL += " OFFSET ?"
		queryArgs = append(queryArgs, offset)
	}
	rows, err := s.db.Query(fastShelfDefaultKeepSQL(filters, offset, limitSQL), queryArgs...)
	if err != nil {
		return nil, err
	}
	return scanShelfLiteRows(rows)
}

func fastShelfDefaultKeepSQL(filters []string, offset int, limitSQL string) string {
	prefixSQL, visibleFilters := fastShelfKeepFilterPlan(filters, offset)
	return prefixSQL + `
		SELECT
			wc.candidate_id,
			wc.library_key,
			wc.title AS sort_title,
			wc.relative_path,
			wc.modified_utc,
			'' AS added_utc,
			0 AS readable_page_count
		FROM work_candidates wc INDEXED BY idx_work_candidates_shelf_default_order
		` + whereClause(visibleFilters) + `
		ORDER BY
			wc.library_key,
			wc.title COLLATE NOCASE,
			wc.relative_path COLLATE NOCASE,
			wc.candidate_id
		` + limitSQL
}

func scanShelfLiteRows(rows *sql.Rows) ([]shelfLiteItem, error) {
	defer rows.Close()
	items := []shelfLiteItem{}
	for rows.Next() {
		var candidateID sql.NullString
		var libraryKey sql.NullString
		var title sql.NullString
		var relativePath sql.NullString
		var modified sql.NullString
		var added sql.NullString
		var pages any
		if err := rows.Scan(&candidateID, &libraryKey, &title, &relativePath, &modified, &added, &pages); err != nil {
			return nil, err
		}
		titleValue := title.String
		items = append(items, shelfLiteItem{
			shelfType:   "work",
			candidateID: candidateID.String,
			libraryKey:  libraryKey.String,
			title:       titleValue,
			titleLower:  sqliteNoCaseKey(titleValue),
			pathKey:     sqliteNoCaseKey(relativePath.String),
			modified:    modified.String,
			added:       added.String,
			pages:       intValue(normalizeDBValue(pages)),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Server) fetchAddedSortedSearchShelfWorkItems(filters []string, args []any, q string, sortKey string, limit, offset int) ([]shelfLiteItem, bool, error) {
	if sortKey != "added_desc" && sortKey != "added_asc" {
		return nil, false, nil
	}
	if !s.workSourceTimeCacheComplete() {
		return nil, false, nil
	}
	searchSQL, searchArgs := s.fastWorkSearchMatchQuery(q)
	if strings.TrimSpace(searchSQL) == "" {
		return nil, false, nil
	}
	direction := "DESC"
	if sortKey == "added_asc" {
		direction = "ASC"
	}
	effectiveTitle := "COALESCE(mfo_title.field_value, wc.title)"
	titleSQL := effectiveTitle + " COLLATE NOCASE, wc.relative_path COLLATE NOCASE, wc.candidate_id, wc.library_key"
	addedSQL := joinedWorkAddedSQL("sft_added", "wi.first_seen_at")
	orderSQL := "ORDER BY " + addedSQL + " " + direction + ", " + titleSQL
	if sortKey == "added_asc" {
		orderSQL = "ORDER BY CASE WHEN " + addedSQL + " = '' THEN 1 ELSE 0 END, " + addedSQL + " ASC, " + titleSQL
	}
	queryArgs := append([]any{}, searchArgs...)
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, limit)
	limitSQL := "LIMIT ?"
	if offset > 0 {
		limitSQL += " OFFSET ?"
		queryArgs = append(queryArgs, offset)
	}
	rows, err := s.db.Query(`
		SELECT
			wc.candidate_id,
			wc.library_key,
			`+effectiveTitle+` AS sort_title,
			wc.relative_path,
			wc.modified_utc,
			`+addedSQL+` AS added_utc,
			COALESCE(pc.readable_page_count, 0) AS readable_page_count
		FROM (`+searchSQL+`) search_match
		JOIN work_candidates wc ON wc.candidate_id = search_match.candidate_id
		LEFT JOIN source_filesystem_times sft_added
			ON sft_added.target_type = 'work'
		   AND sft_added.target_id = wc.candidate_id
		LEFT JOIN page_counts pc ON pc.candidate_id = wc.candidate_id
		LEFT JOIN work_identities wi ON wi.current_candidate_id = wc.candidate_id
		LEFT JOIN metadata_field_overrides mfo_title
			ON mfo_title.work_identity_id = wi.work_identity_id
		   AND mfo_title.field_name = 'title'
		   AND mfo_title.override_status = 'active'
		`+whereClause(filters)+`
		`+orderSQL+`
		`+limitSQL, queryArgs...)
	if err != nil {
		return nil, true, err
	}
	items, err := scanShelfLiteRows(rows)
	return items, true, err
}

func (s *Server) fetchAddedSortedShelfWorkItems(filters []string, args []any, searchJoin string, searchArgs []any, sortKey string, limit, offset int) ([]shelfLiteItem, bool, error) {
	if sortKey != "added_desc" && sortKey != "added_asc" {
		return nil, false, nil
	}
	if !s.workSourceTimeCacheComplete() {
		return nil, false, nil
	}
	direction := "DESC"
	if sortKey == "added_asc" {
		direction = "ASC"
	}
	effectiveTitle := "COALESCE(mfo_title.field_value, wc.title)"
	titleSQL := effectiveTitle + " COLLATE NOCASE, wc.relative_path COLLATE NOCASE, wc.candidate_id, wc.library_key"
	sourceFrom := `
		FROM source_filesystem_times sft_added INDEXED BY idx_source_filesystem_times_added
		JOIN work_candidates wc ON wc.candidate_id = sft_added.target_id
		LEFT JOIN page_counts pc ON pc.candidate_id = wc.candidate_id
		LEFT JOIN work_identities wi ON wi.current_candidate_id = wc.candidate_id
		LEFT JOIN metadata_field_overrides mfo_title
			ON mfo_title.work_identity_id = wi.work_identity_id
		   AND mfo_title.field_name = 'title'
		   AND mfo_title.override_status = 'active'
		` + searchJoin + `
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
	selectSQL := `
		SELECT
			wc.candidate_id,
			wc.library_key,
			` + effectiveTitle + ` AS sort_title,
			wc.relative_path,
			wc.modified_utc,
			%s AS added_utc,
			COALESCE(pc.readable_page_count, 0) AS readable_page_count
		` + sourceFrom + `
		%s
		%s
		%s
	`
	fetch := func(extraFilters []string, addedExpr string, orderSQL string, rowLimit, rowOffset int) ([]shelfLiteItem, error) {
		if rowLimit <= 0 {
			return []shelfLiteItem{}, nil
		}
		fetchArgs := append([]any{}, queryArgs...)
		fetchArgs = append(fetchArgs, rowLimit)
		limitSQL := "LIMIT ?"
		if rowOffset > 0 {
			limitSQL += " OFFSET ?"
			fetchArgs = append(fetchArgs, rowOffset)
		}
		rows, err := s.query(fmt.Sprintf(selectSQL, addedExpr, whereClause(mergeWhereFilters(extraFilters, filters)), orderSQL, limitSQL), fetchArgs...)
		if err != nil {
			return nil, err
		}
		items := make([]shelfLiteItem, 0, len(rows))
		for _, row := range rows {
			title := stringValue(row["sort_title"])
			items = append(items, shelfLiteItem{
				shelfType:   "work",
				candidateID: stringValue(row["candidate_id"]),
				libraryKey:  stringValue(row["library_key"]),
				title:       title,
				titleLower:  sqliteNoCaseKey(title),
				pathKey:     sqliteNoCaseKey(stringValue(row["relative_path"])),
				modified:    stringValue(row["modified_utc"]),
				added:       stringValue(row["added_utc"]),
				pages:       intValue(row["readable_page_count"]),
			})
		}
		return items, nil
	}
	items := []shelfLiteItem{}
	okItems, err := fetch(okFilters, "sft_added.source_created_utc", "ORDER BY sft_added.source_created_utc "+direction+", "+titleSQL, limit, offset)
	if err != nil {
		return nil, true, err
	}
	items = append(items, okItems...)
	if len(items) < limit {
		okRows, err := s.query("SELECT COUNT(*) AS count "+sourceFrom+whereClause(mergeWhereFilters(okFilters, filters)), queryArgs...)
		if err != nil {
			return nil, true, err
		}
		missingOffset := offset - intValue(firstRow(okRows)["count"])
		if missingOffset < 0 {
			missingOffset = 0
		}
		missingItems, err := fetch(missingFilters, "''", "ORDER BY "+titleSQL, limit-len(items), missingOffset)
		if err != nil {
			return nil, true, err
		}
		items = append(items, missingItems...)
	}
	return items, true, nil
}

func (s *Server) queryShelfWorks(library, source, pageStatus, q, mark, tagKey string) ([]map[string]any, error) {
	filters := []string{"(wb.candidate_type = 'doujin' OR NOT EXISTS (SELECT 1 FROM series_items si WHERE si.candidate_id = wb.candidate_id))"}
	args := []any{}
	if library != "" {
		filters = append(filters, "wb.library_key = ?")
		args = append(args, library)
	}
	if source != "" {
		filters = append(filters, "wb.source_kind = ?")
		args = append(args, source)
	}
	if pageStatus != "" {
		filters = append(filters, "wb.page_count_status = ?")
		args = append(args, pageStatus)
	}
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
		)`)
		args = append(args, like, like, like, like, like, like)
	}

	return s.query(fmt.Sprintf(`
		SELECT
			wb.candidate_id, wb.work_identity_id, wb.library_key, wb.library_name, wb.candidate_type, wb.source_kind, wb.title,
			wb.relative_path, wb.size_bytes, wb.modified_utc, wb.extension, wb.page_count_status, wb.readable_page_count,
			wb.cover_status, wb.cover_kind, wb.translation_sources,
			'' AS series_title, '' AS item_role, '' AS sequence_number,
			%s AS added_utc,
			%s,
			%s,
			%s
		FROM work_browse wb
		%s
		%s
		%s
		%s
	`, workAddedSQL("wb", "COALESCE((SELECT wi_added.first_seen_at FROM work_identities wi_added WHERE wi_added.current_candidate_id = wb.candidate_id), '')"), workUserMarkSelectSQL(), tagSelectSQL("work"), workListProgressSelectSQL(), workUserMarkJoinSQL(), metadataOverrideJoinSQL(), workListProgressJoinSQL("wb.work_identity_id"), whereClause(filters)), args...)
}
