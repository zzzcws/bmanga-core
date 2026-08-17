package prototype

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const coverDuplicateResponseCacheTTL = 30 * time.Second

type coverDuplicateResponseCache struct {
	version   string
	body      []byte
	expiresAt time.Time
}

func (s *Server) coverHashIndexAvailable() (bool, error) {
	rows, err := s.query(`
		SELECT 1
		FROM sqlite_master
		WHERE type = 'table'
		  AND name = 'cover_image_hashes'
		LIMIT 1
	`)
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

func (s *Server) coverDuplicateResponseCacheVersion() (string, error) {
	rows, err := s.query(`
		SELECT
			(SELECT COUNT(*) FROM cover_image_hashes) AS hash_count,
			COALESCE((SELECT MAX(updated_at) FROM cover_image_hashes), '') AS hash_updated_at,
			(
				SELECT COUNT(*)
				FROM local_corrections
				WHERE correction_type = 'review_status'
				  AND target_type IN ('cover_hash', 'work')
			) AS review_count,
			COALESCE((
				SELECT MAX(updated_at)
				FROM local_corrections
				WHERE correction_type = 'review_status'
				  AND target_type IN ('cover_hash', 'work')
			), '') AS review_updated_at
	`)
	if err != nil {
		return "", err
	}
	row := firstRow(rows)
	return fmt.Sprintf(
		"%d|%s|%d|%s",
		intValue(row["hash_count"]),
		stringValue(row["hash_updated_at"]),
		intValue(row["review_count"]),
		stringValue(row["review_updated_at"]),
	), nil
}

func (s *Server) getCoverDuplicateResponseCache(key, version string) ([]byte, bool) {
	now := time.Now()
	s.coverDuplicateMu.Lock()
	defer s.coverDuplicateMu.Unlock()
	cached, ok := s.coverDuplicateCache[key]
	if !ok {
		return nil, false
	}
	if cached.version != version || now.After(cached.expiresAt) {
		delete(s.coverDuplicateCache, key)
		return nil, false
	}
	return cached.body, true
}

func (s *Server) writeCoverDuplicateResponse(w http.ResponseWriter, key, version string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.coverDuplicateMu.Lock()
	if len(s.coverDuplicateCache) >= 64 {
		s.coverDuplicateCache = map[string]coverDuplicateResponseCache{}
	}
	s.coverDuplicateCache[key] = coverDuplicateResponseCache{
		version:   version,
		body:      body,
		expiresAt: time.Now().Add(coverDuplicateResponseCacheTTL),
	}
	s.coverDuplicateMu.Unlock()
	writeJSONBytes(w, body)
}

func (s *Server) clearCoverDuplicateResponseCache() {
	s.coverDuplicateMu.Lock()
	s.coverDuplicateCache = map[string]coverDuplicateResponseCache{}
	s.coverDuplicateMu.Unlock()
}

func duplicateCoverArgs(args []any, extra ...any) []any {
	result := make([]any, 0, len(args)+len(extra))
	result = append(result, args...)
	result = append(result, extra...)
	return result
}

func coverDuplicatePriority(row map[string]any) string {
	coverCount := intValue(row["cover_count"])
	libraryCount := intValue(row["library_count"])
	distinctTitleCount := intValue(row["distinct_title_count"])
	if libraryCount > 1 {
		return "cross_library"
	}
	if coverCount >= 10 {
		return "large_same_library"
	}
	if distinctTitleCount <= 1 {
		return "same_title"
	}
	return "small_mixed"
}

func (s *Server) handleCoverDuplicates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	query := r.URL.Query()
	limit := clampInt(query.Get("limit"), 24, 6, 60)
	offset := clampInt(query.Get("offset"), 0, 0, 10_000_000)
	minSize := clampInt(query.Get("minSize"), 2, 2, 50)
	perGroup := clampInt(query.Get("perGroup"), 8, 2, 24)
	reviewStatus := strings.TrimSpace(query.Get("reviewStatus"))
	priority := strings.TrimSpace(query.Get("priority"))
	if priority != "" && priority != "cross_library" && priority != "large_same_library" && priority != "same_title" && priority != "small_mixed" {
		priority = ""
	}
	sortKey := strings.TrimSpace(query.Get("sort"))
	coverDuplicateSortSQL := map[string]string{
		"count_desc":   "cover_count DESC, sample_title COLLATE NOCASE",
		"count_asc":    "cover_count ASC, sample_title COLLATE NOCASE",
		"library_desc": "library_count DESC, cover_count DESC, sample_title COLLATE NOCASE",
		"title_asc":    "sample_title COLLATE NOCASE, cover_count DESC",
	}
	if _, ok := coverDuplicateSortSQL[sortKey]; !ok {
		sortKey = "count_desc"
	}

	available, err := s.coverHashIndexAvailable()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !available {
		writeJSON(w, map[string]any{
			"indexed": false,
			"total":   0,
			"summary": map[string]any{
				"statuses": map[string]int{"all": 0, "open": 0, "ok": 0, "needs_fix": 0},
				"priorities": map[string]int{
					"all":                0,
					"cross_library":      0,
					"large_same_library": 0,
					"same_title":         0,
					"small_mixed":        0,
				},
			},
			"limit":          limit,
			"offset":         offset,
			"min_group_size": minSize,
			"per_group":      perGroup,
			"priority":       priority,
			"sort":           sortKey,
			"items":          []map[string]any{},
		})
		return
	}

	cacheVersion, err := s.coverDuplicateResponseCacheVersion()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cacheKey := r.URL.Query().Encode()
	if body, ok := s.getCoverDuplicateResponseCache(cacheKey, cacheVersion); ok {
		writeJSONBytes(w, body)
		return
	}

	filters := []string{}
	args := []any{}
	library := strings.TrimSpace(query.Get("library"))
	if library != "" {
		filters = append(filters, "wb.library_key = ?")
		args = append(args, library)
	}
	q := strings.TrimSpace(query.Get("q"))

	coverReviewJoin := `
		LEFT JOIN local_corrections chrev
			ON chrev.target_type = 'cover_hash'
		   AND chrev.target_id = cih.difference_hash
		   AND chrev.correction_type = 'review_status'
	`
	searchJoin := ""
	searchArgs := []any{}
	if q != "" {
		searchJoin, searchArgs = s.fastWorkSearchJoinForAlias(q, "wb")
	}
	baseArgs := duplicateCoverArgs(searchArgs, args...)
	baseFrom := `
		FROM cover_image_hashes cih
		JOIN work_candidates wb ON wb.candidate_id = cih.candidate_id
	` + searchJoin + coverReviewJoin
	itemBaseFrom := `
		FROM cover_image_hashes cih
		JOIN work_browse wb ON wb.candidate_id = cih.candidate_id
	` + searchJoin + seriesJoinSQL() + coverReviewJoin
	statuses := map[string]int{"all": 0, "open": 0, "ok": 0, "needs_fix": 0}
	allGroupRows, err := s.query(fmt.Sprintf(`
		SELECT
			cih.difference_hash,
			COUNT(*) AS cover_count,
			COUNT(DISTINCT wb.library_key) AS library_count,
			COUNT(DISTINCT LOWER(TRIM(wb.title))) AS distinct_title_count,
			MIN(wb.title) AS sample_title,
			COALESCE(MAX(chrev.correction_value), 'open') AS review_status,
			COALESCE(MAX(chrev.note), '') AS review_note,
			COALESCE(MAX(chrev.updated_at), '') AS reviewed_at
		%s
		%s
		GROUP BY cih.difference_hash
		HAVING COUNT(*) >= ?
		ORDER BY `+coverDuplicateSortSQL[sortKey]+`
	`, baseFrom, whereClause(filters)), duplicateCoverArgs(baseArgs, minSize)...)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, row := range allGroupRows {
		row["priority"] = coverDuplicatePriority(row)
	}
	filteredRows := allGroupRows
	if reviewStatus == "open" || reviewStatus == "ok" || reviewStatus == "needs_fix" {
		filteredRows = []map[string]any{}
	}
	for _, row := range allGroupRows {
		status := stringValue(row["review_status"])
		if status == "" {
			status = "open"
		}
		statuses[status]++
		statuses["all"]++
		if reviewStatus == "open" || reviewStatus == "ok" || reviewStatus == "needs_fix" {
			if status == reviewStatus {
				filteredRows = append(filteredRows, row)
			}
		}
	}
	reviewFilteredRows := filteredRows
	priorities := map[string]int{"all": 0, "cross_library": 0, "large_same_library": 0, "same_title": 0, "small_mixed": 0}
	for _, row := range reviewFilteredRows {
		priorityValue := stringValue(row["priority"])
		if priorityValue == "" {
			priorityValue = "small_mixed"
		}
		priorities[priorityValue]++
		priorities["all"]++
	}
	if priority != "" {
		filteredRows = []map[string]any{}
		for _, row := range reviewFilteredRows {
			if stringValue(row["priority"]) == priority {
				filteredRows = append(filteredRows, row)
			}
		}
	}
	total := len(filteredRows)
	groupRows := []map[string]any{}
	if offset < len(filteredRows) {
		end := offset + limit
		if end > len(filteredRows) {
			end = len(filteredRows)
		}
		groupRows = filteredRows[offset:end]
	}

	groupHashes := make([]string, 0, len(groupRows))
	for _, group := range groupRows {
		groupHashes = append(groupHashes, stringValue(group["difference_hash"]))
	}
	coversByHash := map[string][]map[string]any{}
	for _, hashValue := range groupHashes {
		coversByHash[hashValue] = []map[string]any{}
	}
	if len(groupHashes) > 0 {
		hashPlaceholders := make([]string, 0, len(groupHashes))
		hashArgs := make([]any, 0, len(groupHashes))
		for _, hashValue := range groupHashes {
			hashPlaceholders = append(hashPlaceholders, "?")
			hashArgs = append(hashArgs, hashValue)
		}
		itemFilters := append([]string{}, filters...)
		itemFilters = append(itemFilters, "cih.difference_hash IN ("+strings.Join(hashPlaceholders, ",")+")")
		itemArgs := duplicateCoverArgs(baseArgs, hashArgs...)
		covers, err := s.query(fmt.Sprintf(`
			SELECT
				wb.candidate_id, wb.library_key, wb.library_name, wb.candidate_type, wb.source_kind, wb.title,
				wb.relative_path, wb.size_bytes, wb.modified_utc, wb.extension, wb.page_count_status, wb.readable_page_count,
				wb.cover_status, wb.cover_kind, wb.translation_sources,
				si.series_title, si.item_role, si.sequence_number,
				cih.average_hash, cih.difference_hash, cih.width, cih.height
			%s
			%s
			ORDER BY cih.difference_hash, wb.library_key, COALESCE(si.series_title, wb.title), wb.title, wb.candidate_id
		`, itemBaseFrom, whereClause(itemFilters)), itemArgs...)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, cover := range covers {
			enrichWork(cover)
			hashValue := stringValue(cover["difference_hash"])
			if len(coversByHash[hashValue]) < perGroup {
				coversByHash[hashValue] = append(coversByHash[hashValue], cover)
			}
		}
	}

	groups := make([]map[string]any, 0, len(groupRows))
	for _, group := range groupRows {
		hashValue := stringValue(group["difference_hash"])
		groups = append(groups, map[string]any{
			"difference_hash":      group["difference_hash"],
			"cover_count":          group["cover_count"],
			"library_count":        group["library_count"],
			"distinct_title_count": group["distinct_title_count"],
			"priority":             group["priority"],
			"sample_title":         group["sample_title"],
			"review_status":        group["review_status"],
			"review_note":          group["review_note"],
			"reviewed_at":          group["reviewed_at"],
			"covers":               coversByHash[hashValue],
		})
	}

	s.writeCoverDuplicateResponse(w, cacheKey, cacheVersion, map[string]any{
		"indexed":        true,
		"total":          total,
		"summary":        map[string]any{"statuses": statuses, "priorities": priorities},
		"limit":          limit,
		"offset":         offset,
		"min_group_size": minSize,
		"per_group":      perGroup,
		"priority":       priority,
		"sort":           sortKey,
		"items":          groups,
	})
}
