package prototype

import (
	"fmt"
	"sort"
	"strings"
)

func browseSort(raw string) string {
	switch strings.TrimSpace(raw) {
	case "title_asc", "title_desc", "added_desc", "added_asc", "updated_desc", "updated_asc", "pages_desc", "pages_asc":
		return strings.TrimSpace(raw)
	default:
		return ""
	}
}

func workOrderSQL(sortKey string) string {
	effectiveTitle := "COALESCE(mfo_title.field_value, wb.title)"
	title := "COALESCE(si.series_title, " + effectiveTitle + ") COLLATE NOCASE, " + effectiveTitle + " COLLATE NOCASE, wb.relative_path COLLATE NOCASE"
	addedAt := workAddedSQL("wb", "COALESCE((SELECT wi_added.first_seen_at FROM work_identities wi_added WHERE wi_added.current_candidate_id = wb.candidate_id), '')")
	switch sortKey {
	case "title_asc":
		return "ORDER BY " + title
	case "title_desc":
		return "ORDER BY COALESCE(si.series_title, " + effectiveTitle + ") COLLATE NOCASE DESC, " + effectiveTitle + " COLLATE NOCASE DESC, wb.relative_path COLLATE NOCASE DESC"
	case "added_desc":
		return "ORDER BY " + addedAt + " DESC, " + title
	case "added_asc":
		return "ORDER BY CASE WHEN " + addedAt + " = '' THEN 1 ELSE 0 END, " + addedAt + " ASC, " + title
	case "updated_desc":
		return "ORDER BY COALESCE(wb.modified_utc, '') DESC, " + title
	case "updated_asc":
		return "ORDER BY CASE WHEN COALESCE(wb.modified_utc, '') = '' THEN 1 ELSE 0 END, COALESCE(wb.modified_utc, '') ASC, " + title
	case "pages_desc":
		return "ORDER BY CAST(COALESCE(wb.readable_page_count, 0) AS INTEGER) DESC, " + title
	case "pages_asc":
		return "ORDER BY CASE WHEN CAST(COALESCE(wb.readable_page_count, 0) AS INTEGER) > 0 THEN 0 ELSE 1 END, CAST(COALESCE(wb.readable_page_count, 0) AS INTEGER) ASC, " + title
	default:
		return "ORDER BY wb.library_key, COALESCE(si.series_title, " + effectiveTitle + "), " + effectiveTitle
	}
}

func fastWorkOrderSQL(sortKey string) string {
	effectiveTitle := "COALESCE(mfo_title.field_value, wc.title)"
	title := "COALESCE(si.series_title, " + effectiveTitle + ") COLLATE NOCASE, " + effectiveTitle + " COLLATE NOCASE, wc.relative_path COLLATE NOCASE"
	addedAt := workAddedSQL("wc", "COALESCE(wi.first_seen_at, '')")
	switch sortKey {
	case "title_asc":
		return "ORDER BY " + title
	case "title_desc":
		return "ORDER BY COALESCE(si.series_title, " + effectiveTitle + ") COLLATE NOCASE DESC, " + effectiveTitle + " COLLATE NOCASE DESC, wc.relative_path COLLATE NOCASE DESC"
	case "added_desc":
		return "ORDER BY " + addedAt + " DESC, " + title
	case "added_asc":
		return "ORDER BY CASE WHEN " + addedAt + " = '' THEN 1 ELSE 0 END, " + addedAt + " ASC, " + title
	case "updated_desc":
		return "ORDER BY COALESCE(wc.modified_utc, '') DESC, " + title
	case "updated_asc":
		return "ORDER BY CASE WHEN COALESCE(wc.modified_utc, '') = '' THEN 1 ELSE 0 END, COALESCE(wc.modified_utc, '') ASC, " + title
	case "pages_desc":
		return "ORDER BY CAST(COALESCE(pc.readable_page_count, 0) AS INTEGER) DESC, " + title
	case "pages_asc":
		return "ORDER BY CASE WHEN CAST(COALESCE(pc.readable_page_count, 0) AS INTEGER) > 0 THEN 0 ELSE 1 END, CAST(COALESCE(pc.readable_page_count, 0) AS INTEGER) ASC, " + title
	default:
		return "ORDER BY wc.library_key, COALESCE(si.series_title, " + effectiveTitle + "), " + effectiveTitle
	}
}

func fastDefaultWorkOrderSQLNoTitleOverride() string {
	return "ORDER BY wc.library_key, COALESCE(si.series_title, wc.title), wc.title"
}

func fastShelfWorkOrderSQL(sortKey string) string {
	effectiveTitle := "COALESCE(mfo_title.field_value, wc.title)"
	title := effectiveTitle + " COLLATE NOCASE, wc.relative_path COLLATE NOCASE, wc.candidate_id, wc.library_key"
	addedAt := joinedWorkAddedSQL("sft_added", "wi.first_seen_at")
	switch sortKey {
	case "title_asc":
		return "ORDER BY " + title
	case "title_desc":
		return "ORDER BY " + effectiveTitle + " COLLATE NOCASE DESC, wc.relative_path COLLATE NOCASE DESC, wc.candidate_id DESC, wc.library_key DESC"
	case "added_desc":
		return "ORDER BY " + addedAt + " DESC, " + title
	case "added_asc":
		return "ORDER BY CASE WHEN " + addedAt + " = '' THEN 1 ELSE 0 END, " + addedAt + " ASC, " + title
	case "updated_desc":
		return "ORDER BY COALESCE(wc.modified_utc, '') DESC, " + title
	case "updated_asc":
		return "ORDER BY CASE WHEN COALESCE(wc.modified_utc, '') = '' THEN 1 ELSE 0 END, COALESCE(wc.modified_utc, '') ASC, " + title
	case "pages_desc":
		return "ORDER BY CAST(COALESCE(pc.readable_page_count, 0) AS INTEGER) DESC, " + title
	case "pages_asc":
		return "ORDER BY CASE WHEN CAST(COALESCE(pc.readable_page_count, 0) AS INTEGER) > 0 THEN 0 ELSE 1 END, CAST(COALESCE(pc.readable_page_count, 0) AS INTEGER) ASC, " + title
	default:
		return "ORDER BY wc.library_key, " + title
	}
}

func sqliteNoCaseKey(value string) string {
	data := []byte(value)
	for index, current := range data {
		if current >= 'A' && current <= 'Z' {
			data[index] = current + ('a' - 'A')
		}
	}
	return string(data)
}

func seriesOrderSQL(sortKey string) string {
	title := "sg.series_title COLLATE NOCASE, sg.group_path COLLATE NOCASE"
	addedAt := seriesAddedSQL()
	switch sortKey {
	case "title_asc":
		return "ORDER BY " + title
	case "title_desc":
		return "ORDER BY sg.series_title COLLATE NOCASE DESC, sg.group_path COLLATE NOCASE DESC"
	case "added_desc":
		return "ORDER BY " + addedAt + " DESC, " + title
	case "added_asc":
		return "ORDER BY CASE WHEN " + addedAt + " = '' THEN 1 ELSE 0 END, " + addedAt + " ASC, " + title
	case "updated_desc":
		return "ORDER BY COALESCE(stats.latest_modified_utc, '') DESC, " + title
	case "updated_asc":
		return "ORDER BY CASE WHEN COALESCE(stats.latest_modified_utc, '') = '' THEN 1 ELSE 0 END, COALESCE(stats.latest_modified_utc, '') ASC, " + title
	case "pages_desc":
		return "ORDER BY COALESCE(stats.counted_pages, 0) DESC, " + title
	case "pages_asc":
		return "ORDER BY CASE WHEN COALESCE(stats.counted_pages, 0) > 0 THEN 0 ELSE 1 END, COALESCE(stats.counted_pages, 0) ASC, " + title
	default:
		return "ORDER BY sg.library_key, sg.series_title"
	}
}

func workAddedSQL(workAlias string, fallbackSQL string) string {
	return "COALESCE((SELECT NULLIF(sft_added.source_created_utc, '') FROM source_filesystem_times sft_added WHERE sft_added.target_type = 'work' AND sft_added.target_id = " + workAlias + ".candidate_id AND sft_added.status = 'ok'), CASE WHEN EXISTS (SELECT 1 FROM source_filesystem_times sft_any WHERE sft_any.target_type = 'work' AND sft_any.target_id = " + workAlias + ".candidate_id) THEN '' ELSE " + fallbackSQL + " END, '')"
}

func joinedWorkAddedSQL(sourceTimeAlias string, fallbackSQL string) string {
	return "CASE WHEN " + sourceTimeAlias + ".target_id IS NULL THEN COALESCE(" + fallbackSQL + ", '') WHEN " + sourceTimeAlias + ".status = 'ok' THEN COALESCE(NULLIF(" + sourceTimeAlias + ".source_created_utc, ''), '') ELSE '' END"
}

func seriesAddedSQL() string {
	return "COALESCE((SELECT NULLIF(sft_added.source_created_utc, '') FROM source_filesystem_times sft_added WHERE sft_added.target_type = 'series' AND sft_added.target_id = sg.group_id AND sft_added.status = 'ok'), CASE WHEN EXISTS (SELECT 1 FROM source_filesystem_times sft_any WHERE sft_any.target_type = 'series' AND sft_any.target_id = sg.group_id) THEN '' ELSE COALESCE(sid.first_seen_at, '') END, '')"
}

func sortShelfItems(items []map[string]any, sortKey string) {
	title := func(item map[string]any) string {
		return sqliteNoCaseKey(shelfTitle(item))
	}
	pathKey := func(item map[string]any) string {
		return sqliteNoCaseKey(coalesceString(item["group_path"], item["relative_path"], item["path"]))
	}
	stableID := func(item map[string]any) string {
		return coalesceString(item["group_id"], item["candidate_id"])
	}
	canonicalLess := func(a, b map[string]any, descending bool) bool {
		valuesA := []string{title(a), pathKey(a), stableID(a), stringValue(a["library_key"])}
		valuesB := []string{title(b), pathKey(b), stableID(b), stringValue(b["library_key"])}
		for index := range valuesA {
			if valuesA[index] == valuesB[index] {
				continue
			}
			if descending {
				return valuesA[index] > valuesB[index]
			}
			return valuesA[index] < valuesB[index]
		}
		return false
	}
	modified := func(item map[string]any) string {
		value := stringValue(item["latest_modified_utc"])
		if value == "" {
			value = stringValue(item["modified_utc"])
		}
		return value
	}
	added := func(item map[string]any) string {
		value := stringValue(item["added_utc"])
		if value == "" {
			value = stringValue(item["first_seen_at"])
		}
		return value
	}
	pages := func(item map[string]any) int {
		value := intValue(item["counted_pages"])
		if value == 0 {
			value = intValue(item["readable_page_count"])
		}
		return value
	}
	sort.SliceStable(items, func(i, j int) bool {
		a := items[i]
		b := items[j]
		switch sortKey {
		case "title_asc":
			return canonicalLess(a, b, false)
		case "title_desc":
			return canonicalLess(a, b, true)
		case "added_desc":
			if added(a) != added(b) {
				return added(a) > added(b)
			}
			return canonicalLess(a, b, false)
		case "added_asc":
			aa := added(a)
			ba := added(b)
			if (aa == "") != (ba == "") {
				return aa != ""
			}
			if aa != ba {
				return aa < ba
			}
			return canonicalLess(a, b, false)
		case "updated_desc":
			if modified(a) != modified(b) {
				return modified(a) > modified(b)
			}
			return canonicalLess(a, b, false)
		case "updated_asc":
			am := modified(a)
			bm := modified(b)
			if (am == "") != (bm == "") {
				return am != ""
			}
			if am != bm {
				return am < bm
			}
			return canonicalLess(a, b, false)
		case "pages_desc":
			if pages(a) != pages(b) {
				return pages(a) > pages(b)
			}
			return canonicalLess(a, b, false)
		case "pages_asc":
			ap := pages(a)
			bp := pages(b)
			if (ap > 0) != (bp > 0) {
				return ap > 0
			}
			if ap != bp {
				return ap < bp
			}
			return canonicalLess(a, b, false)
		default:
			if stringValue(a["library_key"]) != stringValue(b["library_key"]) {
				return stringValue(a["library_key"]) < stringValue(b["library_key"])
			}
			return canonicalLess(a, b, false)
		}
	})
}

type shelfLiteItem struct {
	shelfType   string
	candidateID string
	groupID     string
	libraryKey  string
	title       string
	titleLower  string
	pathKey     string
	modified    string
	added       string
	pages       int
	data        map[string]any
}

func shelfLiteFromMap(item map[string]any) shelfLiteItem {
	title := shelfTitle(item)
	modified := stringValue(item["latest_modified_utc"])
	if modified == "" {
		modified = stringValue(item["modified_utc"])
	}
	added := stringValue(item["added_utc"])
	if added == "" {
		added = stringValue(item["first_seen_at"])
	}
	pages := intValue(item["counted_pages"])
	if pages == 0 {
		pages = intValue(item["readable_page_count"])
	}
	return shelfLiteItem{
		shelfType:   stringValue(item["shelf_type"]),
		candidateID: stringValue(item["candidate_id"]),
		groupID:     stringValue(item["group_id"]),
		libraryKey:  stringValue(item["library_key"]),
		title:       title,
		titleLower:  sqliteNoCaseKey(title),
		pathKey:     sqliteNoCaseKey(coalesceString(item["group_path"], item["relative_path"], item["path"])),
		modified:    modified,
		added:       added,
		pages:       pages,
		data:        item,
	}
}

func sortShelfLiteItems(items []shelfLiteItem, sortKey string) {
	canonicalLess := func(a, b shelfLiteItem, descending bool) bool {
		stableIDA := coalesceString(a.groupID, a.candidateID)
		stableIDB := coalesceString(b.groupID, b.candidateID)
		valuesA := []string{a.titleLower, a.pathKey, stableIDA, a.libraryKey}
		valuesB := []string{b.titleLower, b.pathKey, stableIDB, b.libraryKey}
		for index := range valuesA {
			if valuesA[index] == valuesB[index] {
				continue
			}
			if descending {
				return valuesA[index] > valuesB[index]
			}
			return valuesA[index] < valuesB[index]
		}
		return false
	}
	sort.SliceStable(items, func(i, j int) bool {
		a := items[i]
		b := items[j]
		switch sortKey {
		case "title_asc":
			return canonicalLess(a, b, false)
		case "title_desc":
			return canonicalLess(a, b, true)
		case "added_desc":
			if a.added != b.added {
				return a.added > b.added
			}
			return canonicalLess(a, b, false)
		case "added_asc":
			if (a.added == "") != (b.added == "") {
				return a.added != ""
			}
			if a.added != b.added {
				return a.added < b.added
			}
			return canonicalLess(a, b, false)
		case "updated_desc":
			if a.modified != b.modified {
				return a.modified > b.modified
			}
			return canonicalLess(a, b, false)
		case "updated_asc":
			if (a.modified == "") != (b.modified == "") {
				return a.modified != ""
			}
			if a.modified != b.modified {
				return a.modified < b.modified
			}
			return canonicalLess(a, b, false)
		case "pages_desc":
			if a.pages != b.pages {
				return a.pages > b.pages
			}
			return canonicalLess(a, b, false)
		case "pages_asc":
			if (a.pages > 0) != (b.pages > 0) {
				return a.pages > 0
			}
			if a.pages != b.pages {
				return a.pages < b.pages
			}
			return canonicalLess(a, b, false)
		default:
			if a.libraryKey != b.libraryKey {
				return a.libraryKey < b.libraryKey
			}
			return canonicalLess(a, b, false)
		}
	})
}

func seriesJoinSQL() string {
	return `
		LEFT JOIN (
			SELECT
				candidate_id,
				MAX(series_title) AS series_title,
				MAX(item_role) AS item_role,
				MAX(sequence_number) AS sequence_number
			FROM series_items
			GROUP BY candidate_id
		) si ON si.candidate_id = wb.candidate_id
	`
}

func workUserMarkJoinSQL() string {
	return `
		LEFT JOIN work_user_marks wum
			ON wum.reader_profile_key = 'default'
		   AND wum.work_identity_id = wb.work_identity_id
	`
}

func metadataOverrideJoinSQL() string {
	return `
		LEFT JOIN metadata_field_overrides mfo_title
			ON mfo_title.work_identity_id = wb.work_identity_id
		   AND mfo_title.field_name = 'title'
		   AND mfo_title.override_status = 'active'
		LEFT JOIN metadata_field_overrides mfo_sources
			ON mfo_sources.work_identity_id = wb.work_identity_id
		   AND mfo_sources.field_name = 'translation_sources'
		   AND mfo_sources.override_status = 'active'
	`
}

func metadataOverrideSearchSQL() string {
	return `
		EXISTS (
			SELECT 1
			FROM metadata_field_overrides mfo_search
			WHERE mfo_search.work_identity_id = wb.work_identity_id
			  AND mfo_search.override_status = 'active'
			  AND mfo_search.field_value LIKE ?
		)
	`
}

func (s *Server) localSearchIndexAvailable() bool {
	rows, err := s.query(`
		SELECT 1
		FROM sqlite_master
		WHERE type = 'table'
		  AND name = 'local_search_index'
		LIMIT 1
	`)
	if err != nil || len(rows) == 0 {
		return false
	}
	rows, err = s.query(`
		SELECT
			(SELECT COUNT(*) FROM local_search_index) AS indexed_rows,
			(SELECT COUNT(*) FROM work_candidates) +
			(SELECT COUNT(*) FROM series_groups) AS target_rows
	`)
	if err != nil || len(rows) == 0 {
		return false
	}
	indexedRows := intValue(rows[0]["indexed_rows"])
	targetRows := intValue(rows[0]["target_rows"])
	return indexedRows > 0 && indexedRows == targetRows
}

func (s *Server) fastWorkSearchJoin(q string) (string, []any) {
	return s.fastWorkSearchJoinForAlias(q, "wc")
}

func (s *Server) fastWorkSearchJoinForAlias(q string, candidateAlias string) (string, []any) {
	if strings.TrimSpace(q) == "" {
		return "", []any{}
	}
	if candidateAlias != "wc" && candidateAlias != "wb" {
		candidateAlias = "wc"
	}
	searchSQL, args := s.fastWorkSearchMatchQuery(q)
	if strings.TrimSpace(searchSQL) == "" {
		return "", args
	}
	return `
			JOIN (
				` + searchSQL + `
			) search_match ON search_match.candidate_id = ` + candidateAlias + `.candidate_id
		`, args
}

func (s *Server) fastWorkSearchMatchQuery(q string) (string, []any) {
	if strings.TrimSpace(q) == "" {
		return "", []any{}
	}
	like := "%" + q + "%"
	creatorUnion := ""
	creatorArgs := []any{}
	if s.localTableExists("doujin_creator_items") {
		creatorUnion = `
				UNION
				SELECT creator_search.candidate_id AS candidate_id
				FROM doujin_creator_items creator_search
				WHERE creator_search.creator_display LIKE ?
		`
		creatorArgs = append(creatorArgs, like)
	}
	if s.localSearchIndexAvailable() {
		indexLike := "%" + strings.ToLower(q) + "%"
		query := `
				SELECT search_idx.target_id AS candidate_id
				FROM local_search_index search_idx
				WHERE search_idx.target_type = 'work'
				  AND search_idx.search_text LIKE ?
				UNION
				SELECT wi_search.current_candidate_id AS candidate_id
				FROM work_identities wi_search
				JOIN metadata_field_overrides mfo_search
				  ON mfo_search.work_identity_id = wi_search.work_identity_id
				WHERE mfo_search.override_status = 'active'
				  AND mfo_search.field_value LIKE ?
		` + creatorUnion
		args := []any{indexLike, like}
		args = append(args, creatorArgs...)
		return query, args
	}
	query := `
			SELECT candidate_id
			FROM work_candidates
			WHERE title LIKE ? OR relative_path LIKE ?
			UNION
			SELECT candidate_id
			FROM translation_items
			WHERE translation_group LIKE ?
			UNION
			SELECT wi_search.current_candidate_id AS candidate_id
			FROM work_identities wi_search
			JOIN metadata_field_overrides mfo_search
			  ON mfo_search.work_identity_id = wi_search.work_identity_id
			WHERE mfo_search.override_status = 'active'
			  AND mfo_search.field_value LIKE ?
			UNION
			SELECT candidate_id
			FROM series_items
			WHERE series_title LIKE ?
	` + creatorUnion
	args := []any{like, like, like, like, like}
	args = append(args, creatorArgs...)
	return query, args
}

func seriesUserMarkJoinSQL() string {
	return `
		LEFT JOIN series_identities sid
			ON sid.current_group_id = sg.group_id
		LEFT JOIN series_user_marks sumark
			ON sumark.reader_profile_key = 'default'
		   AND sumark.series_identity_id = sid.series_identity_id
	`
}

func workUserMarkSelectSQL() string {
	return `
		COALESCE(wum.read_status, 'unread') AS user_read_status,
		wum.personal_rating AS user_personal_rating,
		COALESCE(wum.favorite, 0) AS user_favorite,
		COALESCE(wum.reread_priority, 0) AS user_reread_priority,
		CASE WHEN COALESCE(wum.notes, '') <> '' THEN 1 ELSE 0 END AS user_has_notes
	`
}

func seriesUserMarkSelectSQL() string {
	return `
		COALESCE(sumark.read_status, 'unread') AS user_read_status,
		sumark.personal_rating AS user_personal_rating,
		COALESCE(sumark.favorite, 0) AS user_favorite,
		COALESCE(sumark.reread_priority, 0) AS user_reread_priority,
		CASE WHEN COALESCE(sumark.notes, '') <> '' THEN 1 ELSE 0 END AS user_has_notes
	`
}

func addUserMarkFilter(filters *[]string, mark string, alias string) {
	switch strings.TrimSpace(mark) {
	case "favorite":
		*filters = append(*filters, fmt.Sprintf("COALESCE(%s.favorite, 0) = 1", alias))
	case "liked":
		*filters = append(*filters, fmt.Sprintf("(COALESCE(%s.favorite, 0) = 1 OR COALESCE(%s.personal_rating, -1) >= 7)", alias, alias))
	case "strong-liked":
		*filters = append(*filters, fmt.Sprintf("COALESCE(%s.personal_rating, -1) >= 8", alias))
	case "favorite-unrated":
		*filters = append(*filters, fmt.Sprintf("COALESCE(%s.favorite, 0) = 1 AND %s.personal_rating IS NULL", alias, alias))
	case "rating-liked":
		*filters = append(*filters, fmt.Sprintf("COALESCE(%s.favorite, 0) = 0 AND COALESCE(%s.personal_rating, -1) >= 7", alias, alias))
	case "favorite-low":
		*filters = append(*filters, fmt.Sprintf("COALESCE(%s.favorite, 0) = 1 AND COALESCE(%s.personal_rating, 99) <= 5", alias, alias))
	case "reading", "completed", "abandoned", "unread":
		*filters = append(*filters, fmt.Sprintf("%s.read_status = '%s'", alias, mark))
	case "rated":
		*filters = append(*filters, fmt.Sprintf("%s.personal_rating IS NOT NULL", alias))
	case "reread":
		*filters = append(*filters, fmt.Sprintf("COALESCE(%s.reread_priority, 0) > 0", alias))
	case "notes":
		*filters = append(*filters, fmt.Sprintf("COALESCE(%s.notes, '') <> ''", alias))
	}
}

func visibleWorkCandidateExistsSQL(candidateExpression string) string {
	return `EXISTS (
		SELECT 1
		FROM work_browse visible_wb
		WHERE visible_wb.candidate_id = ` + candidateExpression + `
	)`
}

func seriesHasVisibleMemberSQL(seriesAlias string) string {
	return `EXISTS (
		SELECT 1
		FROM series_items visible_si
		WHERE visible_si.group_id = ` + seriesAlias + `.group_id
		  AND ` + visibleWorkCandidateExistsSQL("visible_si.candidate_id") + `
	)`
}

func seriesBaseFromSQL() string {
	return fmt.Sprintf(`
		FROM series_groups sg
		LEFT JOIN series_cover_candidates scc
			ON scc.group_id = sg.group_id
		   AND %s
		%s
		%s
		%s
		%s
		LEFT JOIN (
			SELECT
				si.group_id,
				COUNT(*) AS item_count,
				COUNT(DISTINCT CASE
					WHEN si.sequence_number IS NOT NULL AND si.sequence_number <> '' THEN si.sequence_number
					ELSE si.candidate_id
				END) AS unique_sequence_count,
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
	`, visibleWorkCandidateExistsSQL("scc.selected_candidate_id"), seriesUserMarkJoinSQL(), safeSeriesCoverJoinSQL(), seriesCoverOverrideJoinSQL(), seriesKindJoinSQL(), seriesSectionStatsJoinSQL())
}

func selectedSeriesBaseFromSQL() string {
	return fmt.Sprintf(`
		FROM selected_groups selected
		JOIN series_groups sg ON sg.group_id = selected.group_id
		LEFT JOIN series_cover_candidates scc
			ON scc.group_id = sg.group_id
		   AND %s
		%s
		%s
		%s
		%s
		LEFT JOIN (
			SELECT
				si.group_id,
				COUNT(*) AS item_count,
				COUNT(DISTINCT CASE
					WHEN si.sequence_number IS NOT NULL AND si.sequence_number <> '' THEN si.sequence_number
					ELSE si.candidate_id
				END) AS unique_sequence_count,
				SUM(CASE WHEN pc.page_count_status = 'counted' THEN CAST(pc.readable_page_count AS INTEGER) ELSE 0 END) AS counted_pages,
				SUM(CASE WHEN pc.page_count_status = 'counted' THEN 1 ELSE 0 END) AS counted_items,
				MAX(wc.modified_utc) AS latest_modified_utc
			FROM selected_groups selected_stats
			JOIN series_items si ON si.group_id = selected_stats.group_id
			JOIN work_browse stats_wb ON stats_wb.candidate_id = si.candidate_id
			LEFT JOIN page_counts pc ON pc.candidate_id = si.candidate_id
			LEFT JOIN work_candidates wc ON wc.candidate_id = si.candidate_id
			GROUP BY si.group_id
		) stats ON stats.group_id = sg.group_id
		%s
	`, visibleWorkCandidateExistsSQL("scc.selected_candidate_id"), seriesUserMarkJoinSQL(), safeSeriesCoverJoinSQLForSelected(true), seriesCoverOverrideJoinSQL(), seriesKindJoinSQL(), seriesSectionStatsJoinSQLForSelected(true))
}

func safeSeriesCoverJoinSQL() string {
	return safeSeriesCoverJoinSQLForSelected(false)
}

func safeSeriesCoverJoinSQLForSelected(selectedOnly bool) string {
	parts := []string{}
	for _, marker := range coverExcludeMarkers {
		m := strings.ToLower(marker)
		parts = append(parts, fmt.Sprintf(`
			LOWER(COALESCE(wcc.cover_source_relative_path, '')) NOT LIKE '%%%s%%'
			AND LOWER(COALESCE(wcc.cover_source_path, '')) NOT LIKE '%%%s%%'
		`, m, m))
	}
	filters := strings.Join(parts, " AND ")
	selectedJoin := ""
	if selectedOnly {
		selectedJoin = "JOIN selected_groups selected_cover ON selected_cover.group_id = si.group_id"
	}
	return fmt.Sprintf(`
		LEFT JOIN (
			SELECT *
			FROM (
				SELECT
					si.group_id,
					wcc.candidate_id AS selected_candidate_id,
					wcc.cover_status,
					wcc.cover_kind,
					wcc.cover_source_path,
					wcc.cover_source_relative_path,
					wcc.requires_extraction,
					ROW_NUMBER() OVER (
						PARTITION BY si.group_id
						ORDER BY
							CASE WHEN wc.relative_path = sg2.group_path THEN 1 ELSE 0 END,
							CASE
								WHEN si.item_role = 'volume' THEN 0
								WHEN si.item_role = 'chapter' THEN 1
								ELSE 2
							END,
							si.sort_key,
							wc.relative_path
					) AS cover_rank
				FROM series_items si
				%s
				JOIN series_groups sg2 ON sg2.group_id = si.group_id
				JOIN work_candidates wc ON wc.candidate_id = si.candidate_id
				JOIN work_cover_candidates wcc INDEXED BY idx_work_cover_candidates_candidate_id ON wcc.candidate_id = si.candidate_id
				WHERE wcc.cover_status = 'ready'
				  AND wcc.cover_kind IN ('page_image', 'archive', 'pdf', 'ebook')
				  AND %s
			) ranked_safe_covers
			WHERE cover_rank = 1
		) safe_cover ON safe_cover.group_id = sg.group_id
	`, selectedJoin, filters)
}

func seriesCoverOverrideJoinSQL() string {
	return `
		LEFT JOIN local_corrections cover_choice
			ON cover_choice.target_type = 'series'
		   AND cover_choice.target_id = sg.group_id
		   AND cover_choice.correction_type = 'cover_candidate_id'
		LEFT JOIN work_cover_candidates cover_override INDEXED BY idx_work_cover_candidates_candidate_id
			ON cover_override.candidate_id = cover_choice.correction_value
		   AND cover_override.cover_status = 'ready'
		   AND cover_override.cover_kind IN ('page_image', 'archive', 'pdf', 'ebook')
		   AND EXISTS (
			   SELECT 1
			   FROM series_items cover_item
			   WHERE cover_item.group_id = sg.group_id
				 AND cover_item.candidate_id = cover_override.candidate_id
		   )
		   AND ` + visibleWorkCandidateExistsSQL("cover_override.candidate_id") + `
	`
}

func seriesKindJoinSQL() string {
	return `
		LEFT JOIN local_corrections kind_choice
			ON kind_choice.target_type = 'series'
		   AND kind_choice.target_id = sg.group_id
		   AND kind_choice.correction_type = 'series_kind'
		LEFT JOIN local_corrections unit_choice
			ON unit_choice.target_type = 'series'
		   AND unit_choice.target_id = sg.group_id
		   AND unit_choice.correction_type = 'series_unit'
	`
}

func seriesSectionStatsJoinSQL() string {
	return seriesSectionStatsJoinSQLForSelected(false)
}

func seriesSectionStatsJoinSQLForSelected(selectedOnly bool) string {
	selectedJoin := ""
	if selectedOnly {
		selectedJoin = "JOIN selected_groups selected_section ON selected_section.group_id = si.group_id"
	}
	return `
		LEFT JOIN (
			SELECT
				section_counts.group_id,
				COUNT(*) AS section_count,
				SUM(CASE WHEN section_counts.section_items > 1 THEN 1 ELSE 0 END) AS multi_section_count,
				SUM(CASE
					WHEN section_counts.section_label <> '本篇'
					 AND (
						section_counts.section_label LIKE '%外传%'
						OR section_counts.section_label LIKE '%外傳%'
						OR section_counts.section_label LIKE '%番外%'
						OR section_counts.section_label LIKE '%特典%'
						OR section_counts.section_label LIKE '%资料%'
						OR section_counts.section_label LIKE '%資料%'
						OR section_counts.section_label LIKE '%设定%'
						OR section_counts.section_label LIKE '%設定%'
						OR section_counts.section_label LIKE '%公式%'
						OR section_counts.section_label LIKE '%导读%'
						OR section_counts.section_label LIKE '%導讀%'
						OR section_counts.section_label LIKE '%周年%'
						OR section_counts.section_label LIKE '%纪念%'
						OR section_counts.section_label LIKE '%紀念%'
						OR section_counts.section_label LIKE '%周边%'
						OR section_counts.section_label LIKE '%周邊%'
						OR section_counts.section_label LIKE '%附录%'
						OR section_counts.section_label LIKE '%附錄%'
					 )
					THEN 1 ELSE 0
				END) AS special_section_count
			FROM (
				SELECT
					section_labels.group_id,
					section_labels.section_label,
					COUNT(*) AS section_items
				FROM (
					SELECT
						si.group_id,
						CASE
							WHEN instr(substr(si.relative_path, length(sg2.group_path) + 2), char(92)) > 0
								THEN substr(
									substr(si.relative_path, length(sg2.group_path) + 2),
									1,
									instr(substr(si.relative_path, length(sg2.group_path) + 2), char(92)) - 1
								)
							ELSE '本篇'
						END AS section_label
					FROM series_items si
					` + selectedJoin + `
					JOIN series_groups sg2 ON sg2.group_id = si.group_id
					JOIN work_browse section_wb ON section_wb.candidate_id = si.candidate_id
				) section_labels
				GROUP BY section_labels.group_id, section_labels.section_label
			) section_counts
			GROUP BY section_counts.group_id
		) section_stats ON section_stats.group_id = sg.group_id
	`
}
