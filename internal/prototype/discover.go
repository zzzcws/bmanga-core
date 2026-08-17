package prototype

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	rand "math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"
)

type discoverTiming struct {
	name     string
	duration time.Duration
}

func discoverQueryBranchEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func setDiscoverServerTiming(w http.ResponseWriter, started time.Time, timings []discoverTiming) {
	parts := []string{fmt.Sprintf("app;dur=%.1f", float64(time.Since(started).Microseconds())/1000.0)}
	for _, timing := range timings {
		if strings.TrimSpace(timing.name) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s;dur=%.1f", timing.name, float64(timing.duration.Microseconds())/1000.0))
	}
	w.Header().Set("Server-Timing", strings.Join(parts, ", "))
}

func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	started := time.Now()
	query := r.URL.Query()
	randomMode := strings.TrimSpace(query.Get("randomMode"))
	if randomMode == "" {
		randomMode = "unread"
	}
	library := strings.TrimSpace(query.Get("library"))
	search := strings.TrimSpace(query.Get("q"))
	randomLimit := clampInt(query.Get("randomLimit"), 8, 1, 24)
	historyLimit := clampInt(query.Get("historyLimit"), 12, 1, 36)
	includeHistory := discoverQueryBranchEnabled(query.Get("includeHistory"))
	includeStats := discoverQueryBranchEnabled(query.Get("includeStats"))

	var randomItems []map[string]any
	var historyItems []map[string]any
	var stats map[string]any
	var randomDuration time.Duration
	var historyDuration time.Duration
	var statsDuration time.Duration
	var randomPhaseTimings []discoverTiming
	var randomErr error
	var historyErr error
	var statsErr error
	var queries sync.WaitGroup
	queryCount := 1
	if includeHistory {
		queryCount++
	}
	if includeStats {
		queryCount++
	}
	queries.Add(queryCount)
	go func() {
		defer queries.Done()
		queryStarted := time.Now()
		randomItems, randomErr = s.queryDiscoverRandomWorksWithTiming(randomMode, library, search, randomLimit, func(name string, duration time.Duration) {
			randomPhaseTimings = append(randomPhaseTimings, discoverTiming{name: name, duration: duration})
		})
		randomDuration = time.Since(queryStarted)
	}()
	if includeHistory {
		go func() {
			defer queries.Done()
			queryStarted := time.Now()
			historyItems, historyErr = s.queryReadingHistoryWorks(library, search, historyLimit)
			historyDuration = time.Since(queryStarted)
		}()
	}
	if includeStats {
		go func() {
			defer queries.Done()
			queryStarted := time.Now()
			stats, statsErr = s.queryDiscoverStats()
			statsDuration = time.Since(queryStarted)
		}()
	}
	queries.Wait()
	timings := []discoverTiming{{name: "discoverRandom", duration: randomDuration}}
	timings = append(timings, randomPhaseTimings...)
	if includeHistory {
		timings = append(timings, discoverTiming{name: "discoverHistory", duration: historyDuration})
	}
	if includeStats {
		timings = append(timings, discoverTiming{name: "discoverStats", duration: statsDuration})
	}
	for _, err := range []error{randomErr, historyErr, statsErr} {
		if err != nil {
			setDiscoverServerTiming(w, started, timings)
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	payload := map[string]any{
		"total":        len(randomItems) + len(historyItems),
		"random_mode":  randomMode,
		"random_items": randomItems,
	}
	if includeHistory {
		payload["history"] = historyItems
	}
	if includeStats {
		payload["stats"] = stats
	}
	setDiscoverServerTiming(w, started, timings)
	writeJSON(w, payload)
}

func (s *Server) handleRandomWork(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	started := time.Now()
	query := r.URL.Query()
	randomMode := strings.TrimSpace(query.Get("mode"))
	if randomMode == "" {
		randomMode = strings.TrimSpace(query.Get("randomMode"))
	}
	if randomMode == "" {
		randomMode = "unread"
	}
	library := strings.TrimSpace(query.Get("library"))
	search := strings.TrimSpace(query.Get("q"))
	queryStarted := time.Now()
	timings := []discoverTiming{}
	items, err := s.queryDiscoverRandomWorksWithTiming(randomMode, library, search, 1, func(name string, duration time.Duration) {
		timings = append(timings, discoverTiming{name: name, duration: duration})
	})
	timings = append([]discoverTiming{{name: "discoverRandom", duration: time.Since(queryStarted)}}, timings...)
	if err != nil {
		setDiscoverServerTiming(w, started, timings)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(items) == 0 {
		setDiscoverServerTiming(w, started, timings)
		writeJSONError(w, http.StatusNotFound, "当前条件下没有可随机抽取的作品。")
		return
	}
	setDiscoverServerTiming(w, started, timings)
	writeJSON(w, map[string]any{
		"mode": randomMode,
		"item": items[0],
	})
}

func (s *Server) handleReadingHistory(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	started := time.Now()
	query := r.URL.Query()
	limit := clampInt(query.Get("limit"), 36, 1, 120)
	library := strings.TrimSpace(query.Get("library"))
	search := strings.TrimSpace(query.Get("q"))
	queryStarted := time.Now()
	items, err := s.queryReadingHistoryWorks(library, search, limit)
	timings := []discoverTiming{{name: "discoverHistory", duration: time.Since(queryStarted)}}
	if err != nil {
		setDiscoverServerTiming(w, started, timings)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setDiscoverServerTiming(w, started, timings)
	writeJSON(w, map[string]any{
		"total": len(items),
		"items": items,
	})
}

func discoverWorkSelectSQL() string {
	return `
		SELECT
			wb.candidate_id, wb.work_identity_id, wb.library_key, wb.library_name, wb.candidate_type, wb.source_kind, wb.title,
			wb.relative_path, wb.size_bytes, wb.modified_utc, wb.extension, wb.page_count_status, wb.readable_page_count,
			wb.cover_status, wb.cover_kind, wb.translation_sources,
			si.series_title, si.item_role, si.sequence_number,
			` + workAddedSQL("wb", "COALESCE((SELECT wi_added.first_seen_at FROM work_identities wi_added WHERE wi_added.current_candidate_id = wb.candidate_id), '')") + ` AS added_utc,
			` + workUserMarkSelectSQL() + `,
			` + tagSelectSQL("work") + `,
			` + workListProgressSelectSQL()
}

func discoverWorkBaseSQL() string {
	return discoverWorkBaseSQLWithProgressJoin(workListProgressJoinSQL("wb.work_identity_id"))
}

func discoverHistoryWorkBaseSQL() string {
	return discoverWorkBaseSQLWithProgressJoin(workListRequiredProgressJoinSQL("wb.work_identity_id"))
}

func discoverWorkBaseSQLWithProgressJoin(progressJoin string) string {
	return `
		FROM work_browse wb
		` + seriesJoinSQL() + `
		` + workUserMarkJoinSQL() + `
		` + metadataOverrideJoinSQL() + `
		` + progressJoin
}

func addDiscoverWorkFilters(filters *[]string, args *[]any, library string, search string) {
	*filters = append(*filters,
		"wb.page_count_status = 'counted'",
		"CAST(COALESCE(wb.readable_page_count, 0) AS INTEGER) > 0",
	)
	if library != "" {
		*filters = append(*filters, "wb.library_key = ?")
		*args = append(*args, library)
	}
	if search != "" {
		like := "%" + search + "%"
		*filters = append(*filters, `(
			COALESCE(mfo_title.field_value, wb.title) LIKE ?
			OR wb.title LIKE ?
			OR wb.relative_path LIKE ?
			OR COALESCE(mfo_sources.field_value, wb.translation_sources) LIKE ?
			OR wb.translation_sources LIKE ?
			OR `+metadataOverrideSearchSQL()+`
			OR si.series_title LIKE ?
		)`)
		*args = append(*args, like, like, like, like, like, like, like)
	}
}

func addDiscoverRandomModeFilter(filters *[]string, mode string) {
	switch strings.TrimSpace(mode) {
	case "any", "":
	case "unread":
		*filters = append(*filters, `
			COALESCE(wum.read_status, '') <> 'abandoned'
			AND COALESCE(rp.completed, 0) = 0
			AND COALESCE(wum.read_status, 'unread') NOT IN ('reading', 'completed')
			AND rp.work_identity_id IS NULL
		`)
	case "reading":
		*filters = append(*filters, `
			COALESCE(wum.read_status, '') <> 'abandoned'
			AND COALESCE(rp.completed, 0) = 0
			AND COALESCE(wum.read_status, '') <> 'completed'
			AND (rp.work_identity_id IS NOT NULL OR wum.read_status = 'reading')
		`)
	case "completed":
		*filters = append(*filters, `
			COALESCE(wum.read_status, '') <> 'abandoned'
			AND (COALESCE(rp.completed, 0) = 1 OR wum.read_status = 'completed')
		`)
	case "abandoned":
		*filters = append(*filters, "wum.read_status = 'abandoned'")
	case "liked":
		addUserMarkFilter(filters, "liked", "wum")
	case "strong-liked":
		addUserMarkFilter(filters, "strong-liked", "wum")
	case "rated":
		addUserMarkFilter(filters, "rated", "wum")
	case "reread":
		addUserMarkFilter(filters, "reread", "wum")
	default:
		*filters = append(*filters, "1 = 0")
	}
}

func addDiscoverFastRandomWorkFilters(filters *[]string, args *[]any, library string) {
	*filters = append(*filters,
		"pc.page_count_status = 'counted'",
		"CAST(COALESCE(pc.readable_page_count, 0) AS INTEGER) > 0",
	)
	if library != "" {
		*filters = append(*filters, "wc.library_key = ?")
		*args = append(*args, library)
	}
}

func discoverFastRandomFilterPlan(mode string, library string, search string) ([]string, []any) {
	if search != "" {
		return []string{}, []any{}
	}
	filters := []string{}
	args := []any{}
	addDiscoverFastRandomWorkFilters(&filters, &args, library)
	addDiscoverRandomModeFilter(&filters, mode)
	return filters, args
}

func discoverSparseMarkRandomMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case "abandoned", "liked", "strong-liked", "rated", "reread":
		return true
	default:
		return false
	}
}

func discoverFastRandomCandidateWindowSQL(mode string, filters []string, wrap bool) string {
	queryFilters := append([]string{}, filters...)
	boundary := "wc.candidate_id >= ?"
	if wrap {
		boundary = "wc.candidate_id < ?"
	}
	queryFilters = append(queryFilters, boundary)
	fromSQL := `
		FROM work_candidates wc INDEXED BY idx_work_candidates_candidate_id
		CROSS JOIN page_counts pc INDEXED BY idx_page_counts_candidate_status_reason
			ON pc.candidate_id = wc.candidate_id
		LEFT JOIN work_identities wi ON wi.current_candidate_id = wc.candidate_id
		LEFT JOIN work_user_marks wum
			ON wum.reader_profile_key = 'default'
		   AND wum.work_identity_id = wi.work_identity_id
		LEFT JOIN reading_progress rp
			ON rp.reader_profile_key = 'default'
		   AND rp.work_identity_id = wi.work_identity_id
	`
	if discoverSparseMarkRandomMode(mode) {
		queryFilters = append(queryFilters, "wum.reader_profile_key = 'default'")
		fromSQL = `
			FROM work_user_marks wum
			CROSS JOIN work_identities wi
				ON wi.work_identity_id = wum.work_identity_id
			CROSS JOIN work_candidates wc INDEXED BY idx_work_candidates_candidate_id
				ON wc.candidate_id = wi.current_candidate_id
			CROSS JOIN page_counts pc INDEXED BY idx_page_counts_candidate_status_reason
				ON pc.candidate_id = wc.candidate_id
			LEFT JOIN reading_progress rp
				ON rp.reader_profile_key = 'default'
			   AND rp.work_identity_id = wi.work_identity_id
		`
	}
	return `
		SELECT DISTINCT wc.candidate_id
	` + fromSQL + whereClause(queryFilters) + `
		ORDER BY wc.candidate_id
		LIMIT ?
	`
}

func discoverWideRandomCandidateSelectionSQL(filters []string) string {
	return `
		SELECT wb.candidate_id
	` + discoverWorkBaseSQL() + whereClause(filters) + `
		ORDER BY RANDOM()
		LIMIT ?
	`
}

const (
	discoverRandomCandidatePivotBytes = 12
	discoverRandomCandidatePoolMin    = 64
	discoverRandomCandidatePoolFactor = 8
	discoverRandomCandidatePoolMax    = 256
)

func discoverRandomCandidatePivot(reader io.Reader) (string, error) {
	if reader == nil {
		reader = cryptorand.Reader
	}
	buffer := make([]byte, discoverRandomCandidatePivotBytes)
	if _, err := io.ReadFull(reader, buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func discoverRandomCandidatePoolLimit(limit int) int {
	if limit <= 0 {
		return 0
	}
	poolLimit := limit * discoverRandomCandidatePoolFactor
	if poolLimit < discoverRandomCandidatePoolMin {
		poolLimit = discoverRandomCandidatePoolMin
	}
	if poolLimit > discoverRandomCandidatePoolMax {
		poolLimit = discoverRandomCandidatePoolMax
	}
	return poolLimit
}

func discoverSampleCandidatePool(pool []string, limit int, randomN func(int) int) []string {
	if limit <= 0 || len(pool) == 0 {
		return []string{}
	}
	if randomN == nil {
		randomN = rand.IntN
	}
	items := append([]string{}, pool...)
	if limit > len(items) {
		limit = len(items)
	}
	for index := 0; index < limit; index++ {
		swapIndex := index + randomN(len(items)-index)
		items[index], items[swapIndex] = items[swapIndex], items[index]
	}
	return items[:limit]
}

func (s *Server) queryDiscoverFastRandomCandidatePool(mode string, filters []string, args []any, pivot string, poolLimit int) ([]string, error) {
	if poolLimit <= 0 || strings.TrimSpace(pivot) == "" {
		return []string{}, nil
	}
	fetch := func(wrap bool, limit int) ([]map[string]any, error) {
		if limit <= 0 {
			return []map[string]any{}, nil
		}
		queryArgs := append([]any{}, args...)
		queryArgs = append(queryArgs, pivot, limit)
		return s.query(discoverFastRandomCandidateWindowSQL(mode, filters, wrap), queryArgs...)
	}
	rows, err := fetch(false, poolLimit)
	if err != nil {
		return nil, err
	}
	if len(rows) < poolLimit {
		wrapped, err := fetch(true, poolLimit-len(rows))
		if err != nil {
			return nil, err
		}
		rows = append(rows, wrapped...)
	}
	ids := make([]string, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		candidateID := stringValue(row["candidate_id"])
		if candidateID == "" || seen[candidateID] {
			continue
		}
		seen[candidateID] = true
		ids = append(ids, candidateID)
	}
	return ids, nil
}

func (s *Server) queryDiscoverFastRandomCandidateIDs(mode string, filters []string, args []any, limit int) ([]string, error) {
	// Candidate IDs are fixed-width 96-bit hex keys in the catalog. A uniform
	// 96-bit pivot therefore lands on the candidate-id index without scanning or
	// assigning a random value to every eligible row. Reading a bounded cyclic
	// window and sampling it without replacement also smooths individual hash-gap
	// variance while keeping the query work independent of catalog size.
	pivot, err := discoverRandomCandidatePivot(nil)
	if err != nil {
		return nil, err
	}
	pool, err := s.queryDiscoverFastRandomCandidatePool(mode, filters, args, pivot, discoverRandomCandidatePoolLimit(limit))
	if err != nil {
		return nil, err
	}
	return discoverSampleCandidatePool(pool, limit, nil), nil
}

func discoverFastRandomDetailVisible(row map[string]any, mode string, library string) bool {
	if library != "" && stringValue(row["library_key"]) != library {
		return false
	}
	if stringValue(row["page_count_status"]) != "counted" || intValue(row["readable_page_count"]) <= 0 {
		return false
	}

	readStatus := stringValue(row["user_read_status"])
	if readStatus == "" {
		readStatus = "unread"
	}
	progressExists := stringValue(row["progress_updated_at"]) != "" || stringValue(row["progress_last_read_at"]) != ""
	completed := intValue(row["progress_completed"]) != 0
	ratingPresent := row["user_personal_rating"] != nil
	rating := intValue(row["user_personal_rating"])
	favorite := intValue(row["user_favorite"]) != 0

	switch strings.TrimSpace(mode) {
	case "any", "":
		return true
	case "unread":
		return readStatus != "abandoned" && !completed && readStatus != "reading" && readStatus != "completed" && !progressExists
	case "reading":
		return readStatus != "abandoned" && !completed && readStatus != "completed" && (progressExists || readStatus == "reading")
	case "completed":
		return readStatus != "abandoned" && (completed || readStatus == "completed")
	case "abandoned":
		return readStatus == "abandoned"
	case "liked":
		return favorite || (ratingPresent && rating >= 7)
	case "strong-liked":
		return ratingPresent && rating >= 8
	case "rated":
		return ratingPresent
	case "reread":
		return intValue(row["user_reread_priority"]) > 0
	default:
		return false
	}
}

func discoverUsesTargetedListDetails(mode string, search string) bool {
	if search != "" {
		return false
	}
	switch strings.TrimSpace(mode) {
	case "liked", "strong-liked", "rated", "reread":
		return true
	default:
		return false
	}
}

func (s *Server) queryDiscoverRandomWorks(mode string, library string, search string, limit int) ([]map[string]any, error) {
	return s.queryDiscoverRandomWorksWithTiming(mode, library, search, limit, nil)
}

func (s *Server) queryDiscoverRandomWorksWithTiming(mode string, library string, search string, limit int, recordTiming func(string, time.Duration)) ([]map[string]any, error) {
	record := func(name string, started time.Time) {
		if recordTiming != nil {
			recordTiming(name, time.Since(started))
		}
	}
	detailFilters := []string{}
	detailArgs := []any{}
	addDiscoverWorkFilters(&detailFilters, &detailArgs, library, search)
	addDiscoverRandomModeFilter(&detailFilters, mode)
	useFastCandidates := search == ""
	useTargetedDetails := discoverUsesTargetedListDetails(mode, search)

	fastFilters, fastArgs := discoverFastRandomFilterPlan(mode, library, search)
	selectionStarted := time.Now()
	candidateIDs := []string{}
	var err error
	if useFastCandidates {
		candidateIDs, err = s.queryDiscoverFastRandomCandidateIDs(mode, fastFilters, fastArgs, limit)
	} else {
		selectionArgs := append(append([]any{}, detailArgs...), limit)
		selected := []map[string]any{}
		selected, err = s.query(discoverWideRandomCandidateSelectionSQL(detailFilters), selectionArgs...)
		if err == nil {
			for _, row := range selected {
				candidateID := stringValue(row["candidate_id"])
				if candidateID != "" {
					candidateIDs = append(candidateIDs, candidateID)
				}
			}
		}
	}
	record("discoverSelect", selectionStarted)
	if err != nil {
		return nil, err
	}
	if len(candidateIDs) == 0 {
		return []map[string]any{}, nil
	}
	detailArgs = append([]any{}, detailArgs...)
	for _, candidateID := range candidateIDs {
		detailArgs = append(detailArgs, candidateID)
	}
	if useTargetedDetails {
		detailStarted := time.Now()
		rows, err := s.loadWorkListDetails(candidateIDs)
		record("discoverDetail", detailStarted)
		if err != nil {
			return nil, err
		}
		filterStarted := time.Now()
		visibleByID := make(map[string]map[string]any, len(rows))
		for _, row := range rows {
			if discoverFastRandomDetailVisible(row, mode, library) {
				visibleByID[stringValue(row["candidate_id"])] = row
			}
		}
		visible := make([]map[string]any, 0, len(visibleByID))
		for _, candidateID := range candidateIDs {
			if row := visibleByID[candidateID]; row != nil {
				visible = append(visible, row)
			}
		}
		record("discoverEnrich", filterStarted)
		return visible, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(candidateIDs)), ",")
	detailFilters = append(append([]string{}, detailFilters...), "wb.candidate_id IN ("+placeholders+")")
	detailStarted := time.Now()
	rows, err := s.query(discoverWorkSelectSQL()+discoverWorkBaseSQL()+whereClause(detailFilters), detailArgs...)
	record("discoverDetail", detailStarted)
	if err != nil {
		return nil, err
	}
	enrichStarted := time.Now()
	rows, err = s.finishDiscoverWorkRows(rows)
	record("discoverEnrich", enrichStarted)
	if err != nil {
		return nil, err
	}
	rowByID := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		rowByID[stringValue(row["candidate_id"])] = row
	}
	ordered := make([]map[string]any, 0, len(candidateIDs))
	for _, candidateID := range candidateIDs {
		if row := rowByID[candidateID]; row != nil {
			ordered = append(ordered, row)
		}
	}
	return ordered, nil
}

func (s *Server) queryReadingHistoryWorks(library string, search string, limit int) ([]map[string]any, error) {
	filters := []string{}
	args := []any{}
	addDiscoverWorkFilters(&filters, &args, library, search)
	args = append(args, limit)
	rows, err := s.query(discoverWorkSelectSQL()+discoverHistoryWorkBaseSQL()+whereClause(filters)+`
		ORDER BY julianday(rp.last_read_at) DESC, julianday(rp.updated_at) DESC, wb.title
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	return s.finishDiscoverWorkRows(rows)
}

func (s *Server) finishDiscoverWorkRows(rows []map[string]any) ([]map[string]any, error) {
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
	return rows, nil
}

func (s *Server) queryDiscoverStats() (map[string]any, error) {
	rows, err := s.query(`
		SELECT
			(SELECT COUNT(*) FROM reading_progress WHERE reader_profile_key = 'default' AND COALESCE(last_read_at, '') <> '') AS history_count,
			(SELECT COUNT(*) FROM work_user_marks WHERE reader_profile_key = 'default' AND personal_rating IS NOT NULL) AS rated_count,
			(SELECT COUNT(*) FROM work_user_marks WHERE reader_profile_key = 'default' AND COALESCE(favorite, 0) = 1) AS favorite_count,
			(SELECT COUNT(*) FROM work_user_marks WHERE reader_profile_key = 'default' AND (COALESCE(favorite, 0) = 1 OR COALESCE(personal_rating, -1) >= 7)) AS liked_count,
			(SELECT COUNT(*) FROM work_user_marks WHERE reader_profile_key = 'default' AND COALESCE(reread_priority, 0) > 0) AS reread_count
	`)
	if err != nil {
		return nil, err
	}
	row := firstRow(rows)
	return map[string]any{
		"history_count":  intValue(row["history_count"]),
		"rated_count":    intValue(row["rated_count"]),
		"favorite_count": intValue(row["favorite_count"]),
		"liked_count":    intValue(row["liked_count"]),
		"reread_count":   intValue(row["reread_count"]),
	}, nil
}
