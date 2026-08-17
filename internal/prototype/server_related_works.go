package prototype

import (
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Server) handleWork(w http.ResponseWriter, r *http.Request) {
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
	candidateID := strings.TrimSpace(r.URL.Query().Get("id"))
	if candidateID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing id")
		return
	}

	rows, err := s.query(
		`SELECT
			wb.*,
			si.series_title,
			si.item_role,
			si.sequence_number,
			`+workListProgressSelectSQL()+`
		FROM work_browse wb
		`+seriesJoinSQL()+`
		`+workListProgressJoinSQL("wb.work_identity_id")+`
		WHERE wb.candidate_id = ?`,
		candidateID,
	)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(rows) == 0 {
		writeJSONError(w, http.StatusNotFound, "work not found")
		return
	}
	recordTiming("workMain")
	work := rows[0]
	if err := s.applyMetadataOverrides([]map[string]any{work}); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	enrichWork(work)
	attachWorkListProgress(work)
	recordTiming("workMeta")

	translations, err := s.query(`
		SELECT translation_group, action, action_reason
		FROM translation_items
		WHERE candidate_id = ?
		ORDER BY translation_group
	`, candidateID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	recordTiming("workTranslations")
	series, err := s.query(`
		SELECT group_id, series_title, item_role, sequence_number, sort_key
		FROM series_items
		WHERE candidate_id = ?
	`, candidateID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	recordTiming("workSeries")
	doujinSeries, err := s.query(`
		SELECT group_id, creator_display, series_title, sequence_label, sequence_kind
		FROM doujin_series_items
		WHERE candidate_id = ?
	`, candidateID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	recordTiming("workDoujinSeries")
	creators, err := s.query(`
		SELECT creator_group_id, creator_display, parsed_title, event, parody
		FROM doujin_creator_items
		WHERE candidate_id = ?
	`, candidateID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	recordTiming("workCreators")
	mark, err := s.getWorkUserMark(work)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	recordTiming("workMark")
	related, err := s.relatedWorks(candidateID, work, recordTiming)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	recordTiming("workRelated")
	titleHints := titleHintsFromWork(work)
	recordTiming("workHints")
	timingParts = append([]string{fmt.Sprintf("app;dur=%.1f", float64(time.Since(started).Microseconds())/1000.0)}, timingParts...)
	w.Header().Set("Server-Timing", strings.Join(timingParts, ", "))

	writeJSON(w, map[string]any{
		"work":          work,
		"translations":  translations,
		"series":        series,
		"doujin_series": doujinSeries,
		"creators":      creators,
		"mark":          mark,
		"related":       related,
		"title_hints":   titleHintsPayload(titleHints),
	})
}

func (s *Server) relatedWorks(candidateID string, work map[string]any, timing ...func(string)) (map[string]any, error) {
	recordTiming := func(name string) {
		if len(timing) > 0 && timing[0] != nil {
			timing[0](name)
		}
	}
	seriesItems, err := s.relatedSeriesWorks(candidateID)
	if err != nil {
		return nil, err
	}
	recordTiming("workRelatedSeries")
	creatorItems, err := s.relatedCreatorWorks(candidateID, work, recordTiming)
	if err != nil {
		return nil, err
	}
	recordTiming("workRelatedCreators")

	editionCandidates := make([]map[string]any, 0, len(seriesItems)+len(creatorItems))
	editionCandidates = append(editionCandidates, seriesItems...)
	editionCandidates = append(editionCandidates, creatorItems...)
	editionItems := relatedEditionVariantsForCurrent(editionCandidates, work)
	recordTiming("workRelatedEditionsFilter")

	seriesItems = filterRelatedWorksForCurrent(seriesItems, work)
	recordTiming("workRelatedSeriesFilter")
	creatorItems = filterRelatedWorksForCurrent(creatorItems, work)
	creatorItems = filterRelatedWorksExcluding(creatorItems, relatedWorkIDSet(seriesItems))
	recordTiming("workRelatedCreatorFilter")
	return map[string]any{
		"editions": editionItems,
		"series":   seriesItems,
		"creators": creatorItems,
	}, nil
}

const relatedWorkLimit = 12

var (
	titleEventPrefixRe       = regexp.MustCompile(`^\s*[\(（][^\)）]{1,100}[\)）]\s*([\[［【][^\]］】]{1,140}[\]］】]\s*.+)$`)
	titleTrailingBracketRe   = regexp.MustCompile(`\s*[\[［【(（][^\]］】)）]{1,100}[\]］】)）]\s*$`)
	titleSequenceSuffixRe    = regexp.MustCompile(`(?i)\s*(?:単行本|单行本|單行本|連載|连载)?\s*(?:第\s*)?(?:vol(?:ume)?\.?\s*)?[0-9０-９]+(?:\s*[-ー~〜～至到]\s*[0-9０-９]+)?\s*(?:巻|卷|冊|册|集|話|话|回|章).*$`)
	titleCreatorSplitRe      = regexp.MustCompile(`\s*(?:,|，|、|／|/|&|＆|;|；|\+|＋|×|\sx\s)\s*`)
	titleJunkCreatorRe       = regexp.MustCompile(`(?i)(翻訳|翻译|漢化|汉化|中国|DL版|無修正|无修正|限定|掃圖|扫图|嵌字)`)
	titleAIMetadataCreatorRe = regexp.MustCompile(`(?i)^\s*AI(?:[\s._-]*(?:translation|translated|翻訳|翻译|漢化|汉化|生成|机翻|機翻))?\s*$`)
	titleNumericCreatorRe    = regexp.MustCompile(`^[0-9０-９]{2,6}$`)
	titleTranslationTermRe   = regexp.MustCompile(`(?i)(?:汉化|漢化|翻译|翻譯|翻訳|中文|中国語|chinese|机翻|機翻)`)
	titleBareTranslationRe   = regexp.MustCompile(`中文(?:翻译|翻譯)`)
	titleTranslationTagRes   = []*regexp.Regexp{
		regexp.MustCompile(`\[([^\]]{1,160})\]`),
		regexp.MustCompile(`［([^］]{1,160})］`),
		regexp.MustCompile(`【([^】]{1,160})】`),
		regexp.MustCompile(`\(([^)]{1,160})\)`),
		regexp.MustCompile(`（([^）]{1,160})）`),
	}
	compactSpaceRe                  = regexp.MustCompile(`\s+`)
	relatedFileExtRe                = regexp.MustCompile(`(?i)\.(?:zip|cbz|epub)$`)
	relatedTrailingBracketCaptureRe = regexp.MustCompile(`\s*[\[［【(（]([^\]］】)）]{1,100})[\]］】)）]\s*$`)
	relatedSequenceMetadataRe       = regexp.MustCompile(`(?i)(?:第\s*)?[0-9０-９①-⑳]+(?:\s*[-ー~〜～至到]\s*[0-9０-９①-⑳]+)?\s*(?:巻|卷|冊|册|集|話|话|回|章)|vol(?:ume)?\.?\s*[0-9０-９]+|ch(?:apter)?\.?\s*[0-9０-９]+`)
	relatedTrailingVersionRe        = regexp.MustCompile(`(?i)\s*[\[［【(（]?\s*v(?:er(?:sion)?)?\.?\s*([0-9]{1,3})\s*[\]］】)）]?\s*$`)
	relatedCollectionMarkerRe       = regexp.MustCompile(`(?i)[\[［【(（][^\]］】)）]{0,40}(?:全話|全话|全集|全巻|全卷)[^\]］】)）]{0,40}[\]］】)）]`)
	relatedPartialCollectionRe      = regexp.MustCompile(`^(.*?)(\s*(?:第\s*)?[0-9０-９①-⑳]+\s*(?:[-ー~〜～－—至到]\s*[0-9０-９①-⑳]+)?)\s*$`)
	relatedExplicitPartialMarkerRe  = regexp.MustCompile(`[-ー~〜～－—至到①-⑳]`)
	relatedFirstOnlyMarkerRe        = regexp.MustCompile(`^\s*(?:第\s*)?[1１]\s*$`)
	relatedVolumeOrderRe            = regexp.MustCompile(`(?i)(?:単行本|单行本|單行本)?\s*(?:第\s*)?([0-9０-９]+(?:\.[0-9０-９]+)?)(?:\s*[-ー~〜～至到]\s*([0-9０-９]+(?:\.[0-9０-９]+)?))?\s*(?:巻|卷|冊|册|集)`)
	relatedChapterOrderRe           = regexp.MustCompile(`(?i)(?:連載|连载)?\s*(?:第\s*)?([0-9０-９]+(?:\.[0-9０-９]+)?)(?:\s*[-ー~〜～至到]\s*([0-9０-９]+(?:\.[0-9０-９]+)?))?\s*(?:話|话|回|章|ch(?:apter)?)`)
)

type titleRelatedHints struct {
	creators []string
	series   string
}

func titleHintsPayload(hints titleRelatedHints) map[string]any {
	payload := map[string]any{
		"creators": hints.creators,
		"series":   hints.series,
	}
	if payload["creators"] == nil {
		payload["creators"] = []string{}
	}
	return payload
}

func (s *Server) relatedSeriesWorks(candidateID string) ([]map[string]any, error) {
	items := []map[string]any{}
	hasStructuredSeries := false
	if s.localTableExists("series_items") {
		currentRows, err := s.query(`SELECT 1 FROM series_items WHERE candidate_id = ? LIMIT 1`, candidateID)
		if err != nil {
			return nil, err
		}
		hasStructuredSeries = len(currentRows) > 0
		rows, err := s.query(`
			SELECT DISTINCT
				wb.*,
				si.series_title,
				si.item_role,
				si.sequence_number,
				`+workListProgressSelectSQL()+`,
				si.group_id AS relation_group_id,
				si.series_title AS relation_label,
				'series' AS relation_kind
			FROM series_items current
			JOIN series_items si ON si.group_id = current.group_id
			JOIN work_browse wb ON wb.candidate_id = si.candidate_id
			`+workListProgressJoinSQL("wb.work_identity_id")+`
			WHERE current.candidate_id = ?
			  AND wb.candidate_id <> ?
			  AND `+visibleWorkCandidateExistsSQL("wb.candidate_id")+`
			ORDER BY si.sort_key, wb.title, wb.candidate_id
			LIMIT ?
		`, candidateID, candidateID, relatedWorkLimit)
		if err != nil {
			return nil, err
		}
		items = appendRelatedWorks(items, rows, relatedWorkLimit)
	}
	if len(items) < relatedWorkLimit && s.localTableExists("doujin_series_items") {
		currentRows, err := s.query(`SELECT 1 FROM doujin_series_items WHERE candidate_id = ? LIMIT 1`, candidateID)
		if err != nil {
			return nil, err
		}
		hasStructuredSeries = hasStructuredSeries || len(currentRows) > 0
		rows, err := s.query(`
			SELECT DISTINCT
				wb.*,
				dsi.series_title,
				'' AS item_role,
				dsi.sequence_label AS sequence_number,
				`+workListProgressSelectSQL()+`,
				dsi.group_id AS relation_group_id,
				COALESCE(NULLIF(dsi.series_title, ''), dsi.creator_display) AS relation_label,
				'doujin_series' AS relation_kind
			FROM doujin_series_items current
			JOIN doujin_series_items dsi ON dsi.group_id = current.group_id
			JOIN work_browse wb ON wb.candidate_id = dsi.candidate_id
			`+workListProgressJoinSQL("wb.work_identity_id")+`
			WHERE current.candidate_id = ?
			  AND wb.candidate_id <> ?
			  AND `+visibleWorkCandidateExistsSQL("wb.candidate_id")+`
			ORDER BY dsi.sort_key, wb.title, wb.candidate_id
			LIMIT ?
		`, candidateID, candidateID, relatedWorkLimit-len(items))
		if err != nil {
			return nil, err
		}
		items = appendRelatedWorks(items, rows, relatedWorkLimit)
	}
	if len(items) < relatedWorkLimit && !hasStructuredSeries {
		rows, err := s.relatedTitleSeriesWorks(candidateID, nil, relatedWorkLimit-len(items))
		if err != nil {
			return nil, err
		}
		items = appendRelatedWorks(items, rows, relatedWorkLimit)
	}
	if err := s.applyMetadataOverrides(items); err != nil {
		return nil, err
	}
	for _, item := range items {
		enrichWork(item)
		attachWorkListProgress(item)
	}
	if err := s.fillMissingStructuredDisplayCreators(items); err != nil {
		return nil, err
	}
	sortRelatedSeriesWorks(items)
	return items, nil
}

func (s *Server) relatedCreatorWorks(candidateID string, work map[string]any, timing ...func(string)) ([]map[string]any, error) {
	recordTiming := func(name string) {
		if len(timing) > 0 && timing[0] != nil {
			timing[0](name)
		}
	}
	items := []map[string]any{}
	hasStructuredCreator := false
	hasMetadataAuthor := false
	if s.localTableExists("doujin_creator_items") {
		currentRows, err := s.query(`SELECT 1 FROM doujin_creator_items WHERE candidate_id = ? LIMIT 1`, candidateID)
		if err != nil {
			return nil, err
		}
		hasStructuredCreator = len(currentRows) > 0
		rows, err := s.query(`
			SELECT DISTINCT
				wb.*,
				COALESCE((SELECT MAX(si.series_title) FROM series_items si WHERE si.candidate_id = wb.candidate_id), '') AS series_title,
				COALESCE((SELECT MAX(si.item_role) FROM series_items si WHERE si.candidate_id = wb.candidate_id), '') AS item_role,
				COALESCE((SELECT MAX(si.sequence_number) FROM series_items si WHERE si.candidate_id = wb.candidate_id), '') AS sequence_number,
				`+workListProgressSelectSQL()+`,
				dci.creator_group_id AS relation_group_id,
				dci.creator_display AS relation_label,
				'creator' AS relation_kind
			FROM doujin_creator_items current
			JOIN doujin_creator_items dci ON dci.creator_group_id = current.creator_group_id
			JOIN work_browse wb ON wb.candidate_id = dci.candidate_id
			`+workListProgressJoinSQL("wb.work_identity_id")+`
			WHERE current.candidate_id = ?
			  AND wb.candidate_id <> ?
			  AND `+visibleWorkCandidateExistsSQL("wb.candidate_id")+`
			ORDER BY wb.title
			LIMIT ?
		`, candidateID, candidateID, relatedWorkLimit)
		if err != nil {
			return nil, err
		}
		items = appendRelatedWorks(items, rows, relatedWorkLimit)
	}
	recordTiming("workRelatedCreatorsStructured")
	if len(items) < relatedWorkLimit && s.localTableExists("metadata_field_overrides") {
		author := metadataOverrideValue(work, "author")
		if author != "" {
			hasMetadataAuthor = true
			rows, err := s.query(`
				SELECT DISTINCT
					wb.*,
					COALESCE((SELECT MAX(si.series_title) FROM series_items si WHERE si.candidate_id = wb.candidate_id), '') AS series_title,
					COALESCE((SELECT MAX(si.item_role) FROM series_items si WHERE si.candidate_id = wb.candidate_id), '') AS item_role,
					COALESCE((SELECT MAX(si.sequence_number) FROM series_items si WHERE si.candidate_id = wb.candidate_id), '') AS sequence_number,
					`+workListProgressSelectSQL()+`,
					author.field_value AS relation_label,
					'metadata_author' AS relation_kind
				FROM metadata_field_overrides author
				JOIN work_browse wb ON wb.work_identity_id = author.work_identity_id
				`+workListProgressJoinSQL("wb.work_identity_id")+`
				WHERE author.field_name = 'author'
				  AND author.override_status = 'active'
				  AND author.field_value = ?
				  AND wb.candidate_id <> ?
				  AND `+visibleWorkCandidateExistsSQL("wb.candidate_id")+`
				ORDER BY wb.title
				LIMIT ?
			`, author, candidateID, relatedWorkLimit-len(items))
			if err != nil {
				return nil, err
			}
			items = appendRelatedWorks(items, rows, relatedWorkLimit)
		}
	}
	recordTiming("workRelatedCreatorsMetadata")
	if len(items) < relatedWorkLimit && !hasStructuredCreator && !hasMetadataAuthor {
		rows, err := s.relatedTitleCreatorWorks(candidateID, work, relatedWorkLimit-len(items))
		if err != nil {
			return nil, err
		}
		items = appendRelatedWorks(items, rows, relatedWorkLimit)
	}
	recordTiming("workRelatedCreatorsTitle")
	if err := s.applyMetadataOverrides(items); err != nil {
		return nil, err
	}
	recordTiming("workRelatedCreatorsOverrides")
	for _, item := range items {
		enrichWork(item)
		attachWorkListProgress(item)
	}
	if err := s.fillMissingStructuredDisplayCreators(items); err != nil {
		return nil, err
	}
	recordTiming("workRelatedCreatorsEnrich")
	return items, nil
}

func (s *Server) relatedTitleSeriesWorks(candidateID string, work map[string]any, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		return nil, nil
	}
	if work == nil {
		rows, err := s.query("SELECT title, relative_path FROM work_browse WHERE candidate_id = ?", candidateID)
		if err != nil || len(rows) == 0 {
			return nil, err
		}
		work = rows[0]
	}
	hints := titleHintsFromWork(work)
	if hints.series == "" {
		return nil, nil
	}
	if !s.localTableExists("work_candidates") {
		return s.relatedTitleSeriesWorksFromBrowse(candidateID, hints, limit)
	}
	seriesPattern := sqliteLikeContains(hints.series)
	args := []any{
		candidateID,
		seriesPattern,
		seriesPattern,
	}
	creatorFilter := ""
	if len(hints.creators) > 0 {
		parts := make([]string, 0, len(hints.creators))
		for _, creator := range hints.creators {
			pattern := sqliteLikeContains(creator)
			parts = append(parts, "(wc.title LIKE ? ESCAPE '\\' OR wc.relative_path LIKE ? ESCAPE '\\')")
			args = append(args, pattern, pattern)
		}
		creatorFilter = " AND (" + strings.Join(parts, " OR ") + ")"
	}
	args = append(args, seriesPattern, limit)
	idRows, err := s.query(`
		SELECT wc.candidate_id
		FROM work_candidates wc
		WHERE wc.candidate_id <> ?
		  AND (wc.title LIKE ? ESCAPE '\' OR wc.relative_path LIKE ? ESCAPE '\')
		  `+creatorFilter+`
		  AND `+visibleWorkCandidateExistsSQL("wc.candidate_id")+`
		ORDER BY CASE WHEN wc.title LIKE ? ESCAPE '\' THEN 0 ELSE 1 END, wc.title
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	ids := candidateIDsFromRows(idRows)
	rows, err := s.loadRelatedWorkCardDetails(ids)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if stringValue(row["series_title"]) == "" {
			row["series_title"] = hints.series
		}
		row["relation_group_id"] = ""
		row["relation_label"] = hints.series
		row["relation_kind"] = "title_series"
	}
	return rows, nil
}

func (s *Server) relatedTitleCreatorWorks(candidateID string, work map[string]any, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		return nil, nil
	}
	hints := titleHintsFromWork(work)
	if len(hints.creators) == 0 {
		return nil, nil
	}
	if !s.localTableExists("work_candidates") {
		return s.relatedTitleCreatorWorksFromBrowse(candidateID, hints, limit)
	}
	parts := make([]string, 0, len(hints.creators))
	args := []any{candidateID}
	for _, creator := range hints.creators {
		creatorParts := []string{}
		for _, pattern := range titleCreatorLeadingLikePatterns(creator) {
			creatorParts = append(creatorParts, "(wc.title LIKE ? ESCAPE '\\' OR wc.relative_path LIKE ? ESCAPE '\\')")
			args = append(args, pattern, pattern)
		}
		if len(creatorParts) > 0 {
			parts = append(parts, "("+strings.Join(creatorParts, " OR ")+")")
		}
	}
	if len(parts) == 0 {
		return nil, nil
	}
	queryLimit := limit * 8
	if queryLimit < 24 {
		queryLimit = 24
	}
	if queryLimit > 120 {
		queryLimit = 120
	}
	args = append(args, queryLimit)
	idRows, err := s.query(`
		SELECT wc.candidate_id
		FROM work_candidates wc
		WHERE wc.candidate_id <> ?
		  AND (`+strings.Join(parts, " OR ")+`)
		  AND `+visibleWorkCandidateExistsSQL("wc.candidate_id")+`
		ORDER BY wc.title
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	rows, err := s.loadRelatedWorkCardDetails(candidateIDsFromRows(idRows))
	if err != nil {
		return nil, err
	}
	rows = filterExactTitleCreatorRows(hints, rows, limit)
	for _, row := range rows {
		row["relation_group_id"] = ""
		row["relation_label"] = strings.Join(hints.creators, " / ")
		row["relation_kind"] = "title_creator"
	}
	return rows, nil
}

func (s *Server) relatedTitleSeriesWorksFromBrowse(candidateID string, hints titleRelatedHints, limit int) ([]map[string]any, error) {
	seriesPattern := sqliteLikeContains(hints.series)
	args := []any{
		hints.series,
		hints.series,
		candidateID,
		seriesPattern,
		seriesPattern,
	}
	creatorFilter := ""
	if len(hints.creators) > 0 {
		parts := make([]string, 0, len(hints.creators))
		for _, creator := range hints.creators {
			pattern := sqliteLikeContains(creator)
			parts = append(parts, "(wb.title LIKE ? ESCAPE '\\' OR wb.relative_path LIKE ? ESCAPE '\\')")
			args = append(args, pattern, pattern)
		}
		creatorFilter = " AND (" + strings.Join(parts, " OR ") + ")"
	}
	args = append(args, seriesPattern, limit)
	return s.query(`
		SELECT DISTINCT
			wb.*,
			? AS series_title,
			'' AS item_role,
			'' AS sequence_number,
			`+workListProgressSelectSQL()+`,
			'' AS relation_group_id,
			? AS relation_label,
			'title_series' AS relation_kind
		FROM work_browse wb
		`+workListProgressJoinSQL("wb.work_identity_id")+`
		WHERE wb.candidate_id <> ?
		  AND (wb.title LIKE ? ESCAPE '\' OR wb.relative_path LIKE ? ESCAPE '\')
		  `+creatorFilter+`
		  AND `+visibleWorkCandidateExistsSQL("wb.candidate_id")+`
		ORDER BY CASE WHEN wb.title LIKE ? ESCAPE '\' THEN 0 ELSE 1 END, wb.title
		LIMIT ?
	`, args...)
}

func (s *Server) relatedTitleCreatorWorksFromBrowse(candidateID string, hints titleRelatedHints, limit int) ([]map[string]any, error) {
	parts := make([]string, 0, len(hints.creators))
	args := []any{
		strings.Join(hints.creators, " / "),
		candidateID,
	}
	for _, creator := range hints.creators {
		creatorParts := []string{}
		for _, pattern := range titleCreatorBracketLikePatterns(creator) {
			creatorParts = append(creatorParts, "(wb.title LIKE ? ESCAPE '\\' OR wb.relative_path LIKE ? ESCAPE '\\')")
			args = append(args, pattern, pattern)
		}
		if len(creatorParts) > 0 {
			parts = append(parts, "("+strings.Join(creatorParts, " OR ")+")")
		}
	}
	if len(parts) == 0 {
		return nil, nil
	}
	queryLimit := limit * 8
	if queryLimit < 24 {
		queryLimit = 24
	}
	if queryLimit > 120 {
		queryLimit = 120
	}
	args = append(args, queryLimit)
	rows, err := s.query(`
		SELECT DISTINCT
			wb.*,
			'' AS series_title,
			'' AS item_role,
			'' AS sequence_number,
			`+workListProgressSelectSQL()+`,
			? AS relation_label,
			'title_creator' AS relation_kind
		FROM work_browse wb
		`+workListProgressJoinSQL("wb.work_identity_id")+`
		WHERE wb.candidate_id <> ?
		  AND (`+strings.Join(parts, " OR ")+`)
		  AND `+visibleWorkCandidateExistsSQL("wb.candidate_id")+`
		ORDER BY wb.title
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	return filterExactTitleCreatorRows(hints, rows, limit), nil
}

func candidateIDsFromRows(rows []map[string]any) []string {
	ids := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		id := stringValue(row["candidate_id"])
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

func titleHintsFromWork(work map[string]any) titleRelatedHints {
	title := strings.TrimSpace(stringValue(work["title"]))
	if title == "" {
		title = strings.TrimSpace(stringValue(work["display_title"]))
	}
	if title == "" {
		title = strings.TrimSpace(stringValue(work["relative_path"]))
	}
	if match := titleEventPrefixRe.FindStringSubmatch(title); len(match) == 2 {
		title = match[1]
	}
	creatorPart, rest, ok := leadingTitleBracket(title)
	if !ok {
		return titleRelatedHints{}
	}
	creators := splitTitleCreators(creatorPart)
	series := titleSeriesStem(rest)
	return titleRelatedHints{creators: creators, series: series}
}

func leadingTitleBracket(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) < 3 {
		return "", "", false
	}
	var close rune
	roundBracket := false
	switch runes[0] {
	case '[':
		close = ']'
	case '［':
		close = '］'
	case '【':
		close = '】'
	case '(':
		close = ')'
		roundBracket = true
	case '（':
		close = '）'
		roundBracket = true
	default:
		return "", "", false
	}
	for index := 1; index < len(runes); index++ {
		if runes[index] != close {
			continue
		}
		inside := strings.TrimSpace(string(runes[1:index]))
		rest := strings.TrimSpace(string(runes[index+1:]))
		if inside == "" || rest == "" || utf8.RuneCountInString(inside) > 140 {
			return "", "", false
		}
		if roundBracket && strings.ContainsRune("[［【", []rune(inside)[0]) {
			return "", "", false
		}
		return inside, rest, true
	}
	return "", "", false
}

func splitTitleCreators(value string) []string {
	value = strings.TrimSpace(compactSpaceRe.ReplaceAllString(value, " "))
	if titleNumericCreatorRe.MatchString(value) {
		return []string{value}
	}
	seen := map[string]bool{}
	creators := []string{}
	parts := titleCreatorSplitRe.Split(value, -1)
	for _, part := range parts {
		part = strings.TrimSpace(compactSpaceRe.ReplaceAllString(part, " "))
		if part != "" && utf8.RuneCountInString(part) < 2 {
			value = strings.TrimSpace(compactSpaceRe.ReplaceAllString(value, " "))
			if usefulTitleHint(value, 2, 48) && !junkTitleCreator(value) {
				return []string{value}
			}
			break
		}
	}
	for _, part := range parts {
		part = strings.TrimSpace(compactSpaceRe.ReplaceAllString(part, " "))
		if !usefulTitleHint(part, 2, 48) || junkTitleCreator(part) || seen[part] {
			continue
		}
		seen[part] = true
		creators = append(creators, part)
		if len(creators) >= 4 {
			break
		}
	}
	return creators
}

func junkTitleCreator(value string) bool {
	return titleJunkCreatorRe.MatchString(value) || titleAIMetadataCreatorRe.MatchString(value)
}

func titleTranslationTagNeedsReview(value string) bool {
	if strings.ContainsAny(value, "[]［］【】") {
		return true
	}
	stack := []rune{}
	for _, character := range value {
		switch character {
		case '(', '（':
			stack = append(stack, character)
		case ')', '）':
			if len(stack) == 0 {
				return true
			}
			opening := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if (character == ')' && opening != '(') || (character == '）' && opening != '（') {
				return true
			}
		}
	}
	return len(stack) != 0
}

func titleTranslationSources(value string) []string {
	type indexedTag struct {
		index int
		value string
	}
	tags := []indexedTag{}
	rejected := false
	for _, pattern := range titleTranslationTagRes {
		for _, match := range pattern.FindAllStringSubmatchIndex(value, -1) {
			if len(match) < 4 || match[2] < 0 || match[3] < 0 {
				continue
			}
			tag := strings.TrimSpace(value[match[2]:match[3]])
			if tag != "" && titleTranslationTermRe.MatchString(tag) {
				if titleTranslationTagNeedsReview(tag) {
					rejected = true
					continue
				}
				tags = append(tags, indexedTag{index: match[0], value: tag})
			}
		}
	}
	sort.SliceStable(tags, func(i, j int) bool { return tags[i].index < tags[j].index })
	seen := map[string]bool{}
	out := []string{}
	for _, tag := range tags {
		key := strings.ToLower(compactSpaceRe.ReplaceAllString(tag.value, ""))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, tag.value)
	}
	if len(out) > 0 {
		return out
	}
	if rejected {
		return nil
	}
	if bare := titleBareTranslationRe.FindString(value); bare != "" {
		return []string{bare}
	}
	return nil
}

func titleSeriesStem(value string) string {
	value = strings.TrimSpace(value)
	for {
		next := strings.TrimSpace(titleTrailingBracketRe.ReplaceAllString(value, ""))
		if next == value {
			break
		}
		value = next
	}
	if !titleSequenceSuffixRe.MatchString(value) {
		return ""
	}
	value = strings.TrimSpace(titleSequenceSuffixRe.ReplaceAllString(value, ""))
	value = strings.Trim(value, " -_·:：,，、")
	value = compactSpaceRe.ReplaceAllString(value, " ")
	if !usefulTitleHint(value, 3, 80) {
		return ""
	}
	return value
}

func usefulTitleHint(value string, minRunes int, maxRunes int) bool {
	value = strings.TrimSpace(value)
	runes := utf8.RuneCountInString(value)
	if runes < minRunes || runes > maxRunes {
		return false
	}
	trimmed := strings.Trim(value, "0123456789０１２３４５６７８９ -_.,，、")
	return trimmed != ""
}

func filterExactTitleCreatorRows(current titleRelatedHints, rows []map[string]any, limit int) []map[string]any {
	if limit <= 0 || len(rows) == 0 {
		return nil
	}
	out := []map[string]any{}
	for _, row := range rows {
		if !titleCreatorsOverlap(current.creators, titleHintsFromWork(row).creators) {
			continue
		}
		out = append(out, row)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func titleCreatorsOverlap(left []string, right []string) bool {
	leftSet := map[string]bool{}
	for _, creator := range left {
		normalized := normalizeTitleCreatorHint(creator)
		if normalized != "" {
			leftSet[normalized] = true
		}
	}
	if len(leftSet) == 0 {
		return false
	}
	for _, creator := range right {
		if leftSet[normalizeTitleCreatorHint(creator)] {
			return true
		}
	}
	return false
}

func normalizeTitleCreatorHint(value string) string {
	value = strings.TrimSpace(compactSpaceRe.ReplaceAllString(value, " "))
	return strings.ToLower(value)
}

func titleCreatorBracketLikePatterns(value string) []string {
	escaped := sqliteLikeEscape(value)
	if escaped == "" {
		return nil
	}
	return []string{
		"%[" + escaped + "%]%",
		"%［" + escaped + "%］%",
		"%【" + escaped + "%】%",
		"%(" + escaped + "%)%",
		"%（" + escaped + "%）%",
	}
}

func titleCreatorLeadingLikePatterns(value string) []string {
	escaped := sqliteLikeEscape(value)
	if escaped == "" {
		return nil
	}
	return []string{
		"[" + escaped + "]%",
		"［" + escaped + "］%",
		"【" + escaped + "】%",
		"(" + escaped + ")%",
		"（" + escaped + "）%",
	}
}

func sqliteLikeEscape(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

func sqliteLikeContains(value string) string {
	return "%" + sqliteLikeEscape(value) + "%"
}

func metadataOverrideValue(work map[string]any, fieldName string) string {
	switch raw := work["metadata_overrides"].(type) {
	case map[string]any:
		item, ok := raw[fieldName].(map[string]any)
		if !ok {
			return ""
		}
		return strings.TrimSpace(stringValue(item["field_value"]))
	case map[string]map[string]any:
		item := raw[fieldName]
		if item == nil {
			return ""
		}
		return strings.TrimSpace(stringValue(item["field_value"]))
	default:
		return ""
	}
}

func metadataOverridePresent(work map[string]any, fieldName string) bool {
	switch raw := work["metadata_overrides"].(type) {
	case map[string]any:
		_, ok := raw[fieldName]
		return ok
	case map[string]map[string]any:
		_, ok := raw[fieldName]
		return ok
	default:
		return false
	}
}

func appendRelatedWorks(items []map[string]any, rows []map[string]any, limit int) []map[string]any {
	seen := map[string]bool{}
	for _, item := range items {
		if id := stringValue(item["candidate_id"]); id != "" {
			seen[id] = true
		}
	}
	for _, row := range rows {
		if len(items) >= limit {
			break
		}
		id := stringValue(row["candidate_id"])
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		items = append(items, row)
	}
	return items
}

func relatedWorkIDSet(items []map[string]any) map[string]bool {
	ids := map[string]bool{}
	for _, item := range items {
		if id := stringValue(item["candidate_id"]); id != "" {
			ids[id] = true
		}
	}
	return ids
}

func filterRelatedWorksExcluding(items []map[string]any, excluded map[string]bool) []map[string]any {
	if len(items) == 0 || len(excluded) == 0 {
		return items
	}
	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		id := stringValue(item["candidate_id"])
		if id == "" || excluded[id] {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func filterRelatedWorksForCurrent(items []map[string]any, current map[string]any) []map[string]any {
	if len(items) == 0 {
		return items
	}
	items = filterRelatedDuplicateVariantsForCurrent(items, current)
	items = filterRelatedCoveredCollectionParts(items, current)
	return dedupeRelatedWorkVariants(items)
}

func relatedEditionVariantsForCurrent(items []map[string]any, current map[string]any) []map[string]any {
	if len(items) == 0 || current == nil {
		return []map[string]any{}
	}
	currentID := stringValue(current["candidate_id"])
	seenIDs := map[string]bool{}
	seenIdentities := map[string]bool{}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		id := stringValue(item["candidate_id"])
		if id == "" || id == currentID || seenIDs[id] || !relatedDuplicateVariant(current, item) {
			continue
		}
		identityID := stringValue(item["work_identity_id"])
		if identityID != "" && seenIdentities[identityID] {
			continue
		}
		seenIDs[id] = true
		if identityID != "" {
			seenIdentities[identityID] = true
		}
		out = append(out, item)
		if len(out) >= relatedWorkLimit {
			break
		}
	}
	return out
}

func filterRelatedDuplicateVariantsForCurrent(items []map[string]any, current map[string]any) []map[string]any {
	if len(items) == 0 || current == nil {
		return items
	}
	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if !relatedDuplicateVariant(current, item) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func dedupeRelatedWorkVariants(items []map[string]any) []map[string]any {
	if len(items) == 0 {
		return items
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		id := stringValue(item["candidate_id"])
		if id == "" {
			continue
		}
		duplicateIndex := -1
		for index, existing := range out {
			if id == stringValue(existing["candidate_id"]) {
				duplicateIndex = index
				break
			}
			identityID := stringValue(existing["work_identity_id"])
			if identityID != "" && identityID == stringValue(item["work_identity_id"]) {
				duplicateIndex = index
				break
			}
			if relatedDuplicateVariant(existing, item) {
				duplicateIndex = index
				break
			}
		}
		if duplicateIndex < 0 {
			out = append(out, item)
			continue
		}
		out[duplicateIndex] = preferRelatedWorkVariant(out[duplicateIndex], item)
	}
	return out
}

func preferRelatedWorkVariant(existing map[string]any, incoming map[string]any) map[string]any {
	existingKey, existingVersion := relatedVersionTitleInfo(existing)
	incomingKey, incomingVersion := relatedVersionTitleInfo(incoming)
	if existingKey != "" && existingKey == incomingKey && existingVersion != incomingVersion && incomingVersion > existingVersion {
		return incoming
	}
	return existing
}

func relatedDuplicateVariant(left map[string]any, right map[string]any) bool {
	leftKey := relatedDuplicateTitleKey(left)
	rightKey := relatedDuplicateTitleKey(right)
	if leftKey == "" || rightKey == "" || leftKey != rightKey {
		return false
	}
	leftPages := relatedReadablePageCount(left)
	rightPages := relatedReadablePageCount(right)
	if leftPages > 0 && rightPages > 0 {
		diff := int(math.Abs(float64(leftPages - rightPages)))
		maxPages := leftPages
		if rightPages > maxPages {
			maxPages = rightPages
		}
		pageLimit := int(math.Ceil(float64(maxPages) * 0.12))
		if pageLimit < 4 {
			pageLimit = 4
		}
		return diff <= pageLimit
	}
	return true
}

func relatedDuplicateTitleKey(item map[string]any) string {
	title := relatedItemTitle(item)
	if title == "" {
		return ""
	}
	title = relatedFileExtRe.ReplaceAllString(title, "")
	if match := titleEventPrefixRe.FindStringSubmatch(title); len(match) == 2 {
		title = match[1]
	}
	title = stripRelatedTrailingMetadata(title, true)
	title = relatedTrailingVersionRe.ReplaceAllString(title, "")
	title = compactSpaceRe.ReplaceAllString(strings.TrimSpace(title), " ")
	title = strings.Trim(title, " -_·:：,，、")
	return strings.ToLower(title)
}

func relatedVersionTitleInfo(item map[string]any) (string, int) {
	title := relatedItemTitle(item)
	if title == "" {
		return "", 0
	}
	title = compactSpaceRe.ReplaceAllString(strings.TrimSpace(relatedFileExtRe.ReplaceAllString(title, "")), " ")
	version := 0
	if match := relatedTrailingVersionRe.FindStringSubmatch(title); len(match) == 2 {
		version = intValue(match[1])
		title = strings.TrimSpace(title[:len(title)-len(match[0])])
	}
	title = strings.Trim(title, " -_·:：,，、")
	return strings.ToLower(title), version
}

func relatedItemTitle(item map[string]any) string {
	for _, key := range []string{"title", "display_title", "relative_path"} {
		if value := strings.TrimSpace(stringValue(item[key])); value != "" {
			return value
		}
	}
	return ""
}

func relatedReadablePageCount(item map[string]any) int {
	if value := intValue(item["readable_page_count"]); value > 0 {
		return value
	}
	return intValue(item["page_file_count"])
}

func stripRelatedTrailingMetadata(value string, preserveSequence bool) string {
	value = strings.TrimSpace(value)
	for {
		match := relatedTrailingBracketCaptureRe.FindStringSubmatchIndex(value)
		if len(match) != 4 {
			break
		}
		content := value[match[2]:match[3]]
		if preserveSequence && relatedSequenceMetadataRe.MatchString(content) {
			break
		}
		next := strings.TrimSpace(value[:match[0]])
		if next == value {
			break
		}
		value = next
	}
	return value
}

func filterRelatedCoveredCollectionParts(items []map[string]any, current map[string]any) []map[string]any {
	if len(items) == 0 {
		return items
	}
	completeKeys := map[string]bool{}
	for _, item := range append([]map[string]any{current}, items...) {
		if item == nil || !relatedHasCompleteCollectionMarker(item) {
			continue
		}
		if key := relatedCollectionBaseKey(item); key != "" {
			completeKeys[key] = true
		}
	}
	if len(completeKeys) == 0 {
		return items
	}
	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		baseKey, partial := relatedPartialCollectionInfo(item)
		if partial && completeKeys[baseKey] {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func relatedHasCompleteCollectionMarker(item map[string]any) bool {
	title := relatedFileExtRe.ReplaceAllString(relatedItemTitle(item), "")
	return relatedCollectionMarkerRe.MatchString(title)
}

func relatedCollectionBaseKey(item map[string]any) string {
	body := relatedTitleBody(item)
	if body == "" {
		return ""
	}
	baseKey, partial := relatedPartialCollectionInfo(item)
	if partial {
		body = baseKey
	}
	body = strings.Trim(body, " -_·:：,，、~〜～－—")
	if utf8.RuneCountInString(body) < 3 {
		return ""
	}
	return strings.ToLower(body)
}

func relatedPartialCollectionInfo(item map[string]any) (string, bool) {
	body := relatedTitleBody(item)
	if body == "" {
		return "", false
	}
	match := relatedPartialCollectionRe.FindStringSubmatch(body)
	if len(match) != 3 {
		return "", false
	}
	marker := match[2]
	explicitPartial := relatedExplicitPartialMarkerRe.MatchString(marker) || relatedFirstOnlyMarkerRe.MatchString(marker)
	baseKey := strings.Trim(match[1], " -_·:：,，、~〜～－—")
	if !explicitPartial || utf8.RuneCountInString(baseKey) < 3 {
		return "", false
	}
	return strings.ToLower(baseKey), true
}

func relatedTitleBody(item map[string]any) string {
	title := strings.TrimSpace(relatedFileExtRe.ReplaceAllString(relatedItemTitle(item), ""))
	if title == "" {
		return ""
	}
	if match := titleEventPrefixRe.FindStringSubmatch(title); len(match) == 2 {
		title = match[1]
	}
	if _, rest, ok := leadingTitleBracket(title); ok {
		title = rest
	}
	title = stripRelatedTrailingMetadata(title, false)
	return compactSpaceRe.ReplaceAllString(strings.TrimSpace(title), " ")
}

type relatedSeriesOrderKey struct {
	has   bool
	kind  int
	start float64
	end   float64
	title string
	id    string
}

func sortRelatedSeriesWorks(items []map[string]any) {
	sort.SliceStable(items, func(i, j int) bool {
		left := relatedSeriesOrderKeyFor(items[i])
		right := relatedSeriesOrderKeyFor(items[j])
		if left.has != right.has {
			return left.has
		}
		if !left.has && !right.has {
			return false
		}
		if left.kind != right.kind {
			return left.kind < right.kind
		}
		if left.start != right.start {
			return left.start < right.start
		}
		if left.end != right.end {
			return left.end < right.end
		}
		if left.title != right.title {
			return left.title < right.title
		}
		return left.id < right.id
	})
}

func relatedSeriesOrderKeyFor(item map[string]any) relatedSeriesOrderKey {
	key := relatedSeriesOrderKey{
		kind:  2,
		title: strings.ToLower(normalizeDigits(coalesceString(item["display_title"], item["title"]))),
		id:    stringValue(item["candidate_id"]),
	}
	if sequenceNumber := stringValue(item["sequence_number"]); sequenceNumber != "" {
		if number, ok := parseRelatedOrderNumber(sequenceNumber); ok {
			key.has = true
			key.start = number
			key.end = number
			if strings.EqualFold(stringValue(item["item_role"]), "chapter") {
				key.kind = 1
			} else {
				key.kind = 0
			}
			return key
		}
	}
	values := []string{
		stringValue(item["display_title"]),
		stringValue(item["title"]),
		stringValue(item["relative_path"]),
		stringValue(item["path"]),
	}
	for _, value := range values {
		if start, end, ok := parseRelatedOrderRange(relatedVolumeOrderRe, value); ok {
			key.has = true
			key.kind = 0
			key.start = start
			key.end = end
			return key
		}
	}
	for _, value := range values {
		if start, end, ok := parseRelatedOrderRange(relatedChapterOrderRe, value); ok {
			key.has = true
			key.kind = 1
			key.start = start
			key.end = end
			return key
		}
	}
	return key
}

func parseRelatedOrderRange(pattern *regexp.Regexp, value string) (float64, float64, bool) {
	match := pattern.FindStringSubmatch(value)
	if len(match) == 0 {
		return 0, 0, false
	}
	start, ok := parseRelatedOrderNumber(match[1])
	if !ok || start <= 0 {
		return 0, 0, false
	}
	end := start
	if len(match) > 2 && strings.TrimSpace(match[2]) != "" {
		if parsedEnd, endOK := parseRelatedOrderNumber(match[2]); endOK && parsedEnd >= start {
			end = parsedEnd
		}
	}
	return start, end, true
}

func parseRelatedOrderNumber(value string) (float64, bool) {
	number, err := strconv.ParseFloat(strings.TrimSpace(normalizeDigits(value)), 64)
	if err != nil || number <= 0 {
		return 0, false
	}
	return number, true
}
