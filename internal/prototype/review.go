package prototype

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

type correctionKey struct {
	targetType     string
	targetID       string
	correctionType string
}

func (s *Server) handleReviewSummary(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	items, err := s.buildReviewItemsCached(r.URL.Query())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, reviewSummary(items))
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	query := r.URL.Query()
	limit := clampInt(query.Get("limit"), defaultLimit, minLimit, maxLimit)
	offset := clampInt(query.Get("offset"), 0, 0, 10_000_000)
	issueFilter := strings.TrimSpace(query.Get("issue"))
	subtypeFilter := strings.TrimSpace(query.Get("subtype"))
	statusFilter := strings.TrimSpace(query.Get("status"))
	if statusFilter == "" {
		statusFilter = "open"
	}

	allItems, err := s.buildReviewItemsCached(query)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := filterReviewItems(allItems, issueFilter, subtypeFilter, statusFilter)
	total := len(items)
	end := offset + limit
	if offset > total {
		offset = total
	}
	if end > total {
		end = total
	}
	writeJSON(w, map[string]any{
		"total":   total,
		"limit":   limit,
		"offset":  offset,
		"summary": reviewSummary(allItems),
		"items":   items[offset:end],
	})
}

func (s *Server) buildReviewItems(query url.Values) ([]map[string]any, error) {
	q := strings.TrimSpace(query.Get("q"))
	library := strings.TrimSpace(query.Get("library"))
	items := []map[string]any{}

	corrections, err := s.readCorrectionMap()
	if err != nil {
		return nil, err
	}

	seriesParams := []any{}
	seriesWhere := []string{"sg.group_type = 'series_candidate'", "sg.library_key IN ('commercial-manga', 'manga-test')"}
	if library != "" {
		seriesWhere = append(seriesWhere, "sg.library_key = ?")
		seriesParams = append(seriesParams, library)
	}
	if q != "" {
		like := "%" + q + "%"
		seriesWhere = append(seriesWhere, "(sg.series_title LIKE ? OR sg.group_path LIKE ?)")
		seriesParams = append(seriesParams, like, like)
	}

	seriesRows, err := s.query(fmt.Sprintf(`
		SELECT
			sg.group_id,
			sg.library_key,
			sg.series_title,
			sg.group_path,
			sg.group_type,
			sg.confidence,
			COALESCE(stats.item_count, sg.candidate_count) AS item_count,
			COALESCE(stats.unique_sequence_count, sg.candidate_count) AS unique_sequence_count,
			COALESCE(stats.counted_pages, 0) AS counted_pages,
			COALESCE(stats.counted_items, 0) AS counted_items,
			COALESCE(stats.content_review_items, 0) AS content_review_items,
			COALESCE(stats.parser_required_items, 0) AS parser_required_items,
			COALESCE(stats.source_unreadable_items, 0) AS source_unreadable_items,
			COALESCE(stats.failed_page_items, 0) AS failed_page_items,
			COALESCE(stats.mixed_role_count, 0) AS mixed_role_count,
			COALESCE(section_stats.section_count, 1) AS section_count,
			COALESCE(section_stats.multi_section_count, 0) AS multi_section_count,
			COALESCE(section_stats.special_section_count, 0) AS special_section_count,
			COALESCE(dup.duplicate_sequence_count, 0) AS duplicate_sequence_count,
			COALESCE(cover_override.candidate_id, safe_cover.selected_candidate_id, scc.selected_candidate_id) AS selected_candidate_id,
			COALESCE(cover_override.cover_status, safe_cover.cover_status, scc.cover_status) AS cover_status,
			COALESCE(cover_override.cover_kind, safe_cover.cover_kind, scc.cover_kind) AS cover_kind,
			COALESCE(cover_override.cover_source_path, safe_cover.cover_source_path, scc.cover_source_path) AS cover_source_path,
			COALESCE(cover_override.cover_source_relative_path, safe_cover.cover_source_relative_path, scc.cover_source_relative_path) AS cover_source_relative_path
		FROM series_groups sg
		LEFT JOIN series_cover_candidates scc ON scc.group_id = sg.group_id
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
				COUNT(DISTINCT si.item_role) AS mixed_role_count,
				SUM(CASE WHEN pc.page_count_status = 'counted' THEN CAST(pc.readable_page_count AS INTEGER) ELSE 0 END) AS counted_pages,
				SUM(CASE WHEN pc.page_count_status = 'counted' THEN 1 ELSE 0 END) AS counted_items,
				SUM(CASE
					WHEN pc.page_count_status <> 'counted'
					 AND pc.reason IN ('zip_no_image_entries_from_sample', 'archive_no_image_entries_from_sample')
					THEN 1 ELSE 0 END
				) AS content_review_items,
				SUM(CASE
					WHEN pc.page_count_status = 'parser_required'
					  OR pc.reason IN ('zip_contains_pdf', 'zip_contains_pdf_from_sample')
					THEN 1 ELSE 0 END
				) AS parser_required_items,
				SUM(CASE
					WHEN pc.page_count_status = 'not_attempted'
					 AND pc.reason = 'archive_attempt_limit'
					THEN 1 ELSE 0 END
				) AS source_unreadable_items,
				SUM(CASE WHEN pc.page_count_status = 'failed' THEN 1 ELSE 0 END) AS failed_page_items
			FROM series_items si
			LEFT JOIN page_counts pc ON pc.candidate_id = si.candidate_id
			GROUP BY si.group_id
		) stats ON stats.group_id = sg.group_id
		%s
		LEFT JOIN (
			SELECT group_id, SUM(extra_count) AS duplicate_sequence_count
			FROM (
				SELECT group_id, sequence_number, COUNT(*) - 1 AS extra_count
				FROM series_items
				WHERE sequence_number IS NOT NULL AND sequence_number <> ''
				GROUP BY group_id, sequence_number
				HAVING COUNT(*) > 1
			) duplicated
			GROUP BY group_id
		) dup ON dup.group_id = sg.group_id
		WHERE %s
	`, safeSeriesCoverJoinSQL(), seriesCoverOverrideJoinSQL(), seriesSectionStatsJoinSQL(), strings.Join(seriesWhere, " AND ")), seriesParams...)
	if err != nil {
		return nil, err
	}

	for _, row := range seriesRows {
		enrichSeries(row)
		title := stringValue(row["display_title"])
		coverPath := coalesceString(row["cover_source_relative_path"], row["cover_source_path"])
		targetID := stringValue(row["group_id"])
		libraryName := displayLibraryName(row)
		coverCandidateID := ""
		if stringValue(row["cover_status"]) == "ready" {
			coverCandidateID = stringValue(row["selected_candidate_id"])
		}
		if stringValue(row["cover_status"]) != "ready" || stringValue(row["selected_candidate_id"]) == "" || hasCoverMarker(coverPath) {
			items = append(items, makeReviewItem(
				corrections,
				"cover",
				1,
				"series",
				targetID,
				title,
				stringValue(row["group_path"]),
				libraryName,
				"封面需要确认",
				"当前封面缺失、待抽取，或命中了免责声明/credit 等排除词。",
				"打开详情检查封面；必要时后续手动指定封面。",
				coverCandidateID,
				"",
			))
		}

		itemCount := intValue(row["item_count"])
		uniqueCount := intValue(row["unique_sequence_count"])
		if uniqueCount == 0 {
			uniqueCount = itemCount
		}
		sectionCount := intValue(row["section_count"])
		if sectionCount == 0 {
			sectionCount = 1
		}
		multiSectionCount := intValue(row["multi_section_count"])
		specialSectionCount := intValue(row["special_section_count"])
		duplicateCount := intValue(row["duplicate_sequence_count"])
		mixedRoleCount := intValue(row["mixed_role_count"])
		structureReasons := []string{}
		if sectionCount > 1 && (multiSectionCount >= 2 || specialSectionCount > 0) {
			structureReasons = append(structureReasons, fmt.Sprintf("%d 个分组", sectionCount))
		}
		if uniqueCount < itemCount {
			structureReasons = append(structureReasons, fmt.Sprintf("%d 个章节位 / %d 个条目", uniqueCount, itemCount))
		}
		if duplicateCount > 0 {
			structureReasons = append(structureReasons, fmt.Sprintf("%d 个重复编号", duplicateCount))
		}
		if mixedRoleCount > 1 {
			structureReasons = append(structureReasons, "卷/话/番外混合")
		}
		if len(structureReasons) > 0 {
			items = append(items, makeReviewItem(
				corrections,
				"structure",
				2,
				"series",
				targetID,
				title,
				stringValue(row["group_path"]),
				libraryName,
				"卷 / 话结构待校对",
				strings.Join(structureReasons, "；"),
				"确认它是普通连载、合集，还是需要拆分。",
				coverCandidateID,
				"",
			))
		}

		countedItems := intValue(row["counted_items"])
		if itemCount > 0 && countedItems < itemCount {
			issueLabel, issueDetail, suggestedAction, issueSubtype := seriesPageReviewText(row, itemCount, countedItems)
			items = append(items, makeReviewItem(
				corrections,
				"page",
				3,
				"series",
				targetID,
				title,
				stringValue(row["group_path"]),
				libraryName,
				issueLabel,
				issueDetail,
				suggestedAction,
				coverCandidateID,
				issueSubtype,
			))
		}
	}

	workWhere := []string{
		"(wb.candidate_type = 'doujin' OR NOT EXISTS (SELECT 1 FROM series_items si WHERE si.candidate_id = wb.candidate_id))",
	}
	workParams := []any{}
	if library != "" {
		workWhere = append(workWhere, "wb.library_key = ?")
		workParams = append(workParams, library)
	}
	if q != "" {
		like := "%" + q + "%"
		workWhere = append(workWhere, "(wb.title LIKE ? OR wb.relative_path LIKE ? OR wb.translation_sources LIKE ?)")
		workParams = append(workParams, like, like, like)
	}

	workRows, err := s.query(fmt.Sprintf(`
		SELECT
			wb.candidate_id, wb.library_key, wb.library_name, wb.candidate_type, wb.source_kind, wb.title,
			wb.relative_path, wb.size_bytes, wb.extension, wb.page_count_status, wb.page_count_reason,
			wb.readable_page_count,
			wb.cover_status, wb.cover_kind, wb.translation_sources,
			'' AS series_title, '' AS item_role, '' AS sequence_number,
			wcc.cover_source_path, wcc.cover_source_relative_path
		FROM work_browse wb
		LEFT JOIN work_cover_candidates wcc ON wcc.candidate_id = wb.candidate_id
		WHERE %s
		  AND (
			wb.page_count_status IN ('parser_required', 'unknown', 'failed')
			OR (wb.page_count_status = 'not_attempted' AND wb.page_count_reason = 'archive_attempt_limit')
			OR (wb.source_kind = 'image_folder' AND wb.cover_status <> 'ready')
			OR LOWER(COALESCE(wcc.cover_source_relative_path, '')) LIKE '%%免责%%'
			OR LOWER(COALESCE(wcc.cover_source_relative_path, '')) LIKE '%%免责声明%%'
			OR LOWER(COALESCE(wcc.cover_source_relative_path, '')) LIKE '%%credit%%'
			OR LOWER(COALESCE(wcc.cover_source_path, '')) LIKE '%%免责%%'
			OR LOWER(COALESCE(wcc.cover_source_path, '')) LIKE '%%免责声明%%'
			OR LOWER(COALESCE(wcc.cover_source_path, '')) LIKE '%%credit%%'
		  )
	`, strings.Join(workWhere, " AND ")), workParams...)
	if err != nil {
		return nil, err
	}

	for _, row := range workRows {
		enrichWork(row)
		targetID := stringValue(row["candidate_id"])
		title := stringValue(row["display_title"])
		libraryName := displayLibraryName(row)
		coverPath := coalesceString(row["cover_source_relative_path"], row["cover_source_path"])
		pageStatus := stringValue(row["page_count_status"])
		pageReason := stringValue(row["page_count_reason"])
		if pageStatus == "parser_required" || pageStatus == "unknown" || pageStatus == "failed" ||
			(pageStatus == "not_attempted" && pageReason == "archive_attempt_limit") {
			issueLabel, issueDetail, suggestedAction, issueSubtype := pageReviewText(row)
			coverCandidateID := ""
			if stringValue(row["cover_status"]) == "ready" {
				coverCandidateID = targetID
			}
			items = append(items, makeReviewItem(
				corrections,
				"page",
				3,
				"work",
				targetID,
				title,
				stringValue(row["relative_path"]),
				libraryName,
				issueLabel,
				issueDetail,
				suggestedAction,
				coverCandidateID,
				issueSubtype,
			))
		}
		if (stringValue(row["source_kind"]) == "image_folder" && stringValue(row["cover_status"]) != "ready") || hasCoverMarker(coverPath) {
			coverCandidateID := ""
			if stringValue(row["cover_status"]) == "ready" {
				coverCandidateID = targetID
			}
			items = append(items, makeReviewItem(
				corrections,
				"cover",
				1,
				"work",
				targetID,
				title,
				stringValue(row["relative_path"]),
				libraryName,
				"封面需要确认",
				"单本封面缺失或命中了异常文件名。",
				"打开详情确认来源。",
				coverCandidateID,
				"",
			))
		}
	}

	reviewOrder := map[string]int{"cover": 1, "structure": 2, "page": 3}
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		if intValue(left["severity"]) != intValue(right["severity"]) {
			return intValue(left["severity"]) < intValue(right["severity"])
		}
		leftOrder, ok := reviewOrder[stringValue(left["issue_type"])]
		if !ok {
			leftOrder = 9
		}
		rightOrder, ok := reviewOrder[stringValue(right["issue_type"])]
		if !ok {
			rightOrder = 9
		}
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		if stringValue(left["library_name"]) != stringValue(right["library_name"]) {
			return stringValue(left["library_name"]) < stringValue(right["library_name"])
		}
		return stringValue(left["title"]) < stringValue(right["title"])
	})
	return items, nil
}

func reviewItemsCacheKey(query url.Values) string {
	return strings.TrimSpace(query.Get("library")) + "\x00" + strings.TrimSpace(query.Get("q"))
}

func (s *Server) buildReviewItemsCached(query url.Values) ([]map[string]any, error) {
	key := reviewItemsCacheKey(query)
	s.reviewCacheMu.Lock()
	if cached, ok := s.reviewItemsCache[key]; ok {
		s.reviewCacheMu.Unlock()
		return cached, nil
	}
	s.reviewCacheMu.Unlock()

	items, err := s.buildReviewItems(query)
	if err != nil {
		return nil, err
	}
	s.reviewCacheMu.Lock()
	s.reviewItemsCache[key] = items
	s.reviewCacheMu.Unlock()
	return items, nil
}

func (s *Server) clearReviewItemsCache() {
	s.reviewCacheMu.Lock()
	s.reviewItemsCache = map[string][]map[string]any{}
	s.reviewCacheMu.Unlock()
}

func (s *Server) readCorrectionMap() (map[correctionKey]string, error) {
	rows, err := s.query(`
		SELECT target_type, target_id, correction_type, correction_value
		FROM local_corrections
		WHERE correction_type <> 'review_status'
		   OR correction_value IN ('open', 'ok', 'needs_fix')
	`)
	if err != nil {
		return nil, err
	}
	corrections := map[correctionKey]string{}
	for _, row := range rows {
		corrections[correctionKey{
			targetType:     stringValue(row["target_type"]),
			targetID:       stringValue(row["target_id"]),
			correctionType: stringValue(row["correction_type"]),
		}] = stringValue(row["correction_value"])
	}
	return corrections, nil
}

func reviewStatusFor(corrections map[correctionKey]string, targetType string, targetID string) string {
	value := corrections[correctionKey{targetType: targetType, targetID: targetID, correctionType: "review_status"}]
	if value == "" {
		return "open"
	}
	return value
}

func correctionValueFor(corrections map[correctionKey]string, targetType string, targetID string, correctionType string) string {
	return corrections[correctionKey{targetType: targetType, targetID: targetID, correctionType: correctionType}]
}

func makeReviewItem(
	corrections map[correctionKey]string,
	issueType string,
	severity int,
	targetType string,
	targetID string,
	title string,
	subtitle string,
	libraryName string,
	issueLabel string,
	issueDetail string,
	suggestedAction string,
	coverCandidateID string,
	issueSubtype string,
) map[string]any {
	status := reviewStatusFor(corrections, targetType, targetID)
	seriesKind := correctionValueFor(corrections, targetType, targetID, "series_kind")
	if issueType == "structure" && targetType == "series" && (status == "" || status == "open") && (seriesKind == "normal_manga" || seriesKind == "collection") {
		status = "ok"
	}
	if issueSubtype == "" {
		issueSubtype = issueType
	}
	return map[string]any{
		"review_id":          fmt.Sprintf("%s:%s:%s", issueType, targetType, targetID),
		"issue_type":         issueType,
		"severity":           severity,
		"target_type":        targetType,
		"target_id":          targetID,
		"title":              title,
		"subtitle":           subtitle,
		"library_name":       libraryName,
		"issue_label":        issueLabel,
		"issue_subtype":      issueSubtype,
		"issue_detail":       issueDetail,
		"suggested_action":   suggestedAction,
		"cover_candidate_id": coverCandidateID,
		"review_status":      status,
		"series_kind":        seriesKind,
	}
}

func pageReviewText(row map[string]any) (string, string, string, string) {
	status := stringValue(row["page_count_status"])
	reason := stringValue(row["page_count_reason"])
	sourceKind := stringValue(row["source_kind"])
	switch {
	case reason == "zip_no_image_entries_from_sample" || reason == "archive_no_image_entries_from_sample":
		return "内容需要复核", "压缩包里没有直接图片页，可能是嵌套包、说明文件或异常内容。", "打开详情确认内容；这里只记录本地复核状态。", "content"
	case reason == "zip_contains_pdf" || reason == "zip_contains_pdf_from_sample":
		return "不支持的格式", "ZIP 里装的是 PDF，公开核心不会解析或渲染这类内容。", "请转换为纯图片 ZIP/CBZ 后重新扫描。", "unsupported"
	case status == "parser_required":
		return "需要解析器", fmt.Sprintf("%s 需要额外解析器才能统计页数或抽封面。", sourceKind), "只在尚未接入的格式上保留，例如未来出现真实 RAR/CBR 样本时再处理。", "parser"
	case status == "failed":
		detail := reason
		if detail == "" {
			detail = fmt.Sprintf("%s 页数统计失败。", sourceKind)
		}
		return "页数统计失败", detail, "打开详情确认，必要时标为待修。", "failed"
	case status == "not_attempted" && reason == "archive_attempt_limit":
		return "来源暂不可读", "自动处理没有拿到可读内容，可能是源文件暂时不可访问或需要人工确认。", "稍后可重试；如果持续存在，再标为待修。", "source"
	default:
		detail := fmt.Sprintf("%s / %s", sourceKind, status)
		if reason != "" {
			detail += " / " + reason
		}
		return "页数需要处理", detail, "根据具体原因重跑对应本地审计 / 导入工具，或保留为待修证据。", "page"
	}
}

func seriesPageReviewText(row map[string]any, itemCount int, countedItems int) (string, string, string, string) {
	missingCount := itemCount - countedItems
	if missingCount < 0 {
		missingCount = 0
	}
	type bucket struct {
		key   string
		label string
		count int
	}
	buckets := []bucket{
		{key: "content", label: "内容复核", count: intValue(row["content_review_items"])},
		{key: "parser", label: "需要解析器", count: intValue(row["parser_required_items"])},
		{key: "source", label: "来源暂不可读", count: intValue(row["source_unreadable_items"])},
		{key: "failed", label: "统计失败", count: intValue(row["failed_page_items"])},
	}
	visibleBuckets := []bucket{}
	bucketTotal := 0
	for _, item := range buckets {
		if item.count > 0 {
			visibleBuckets = append(visibleBuckets, item)
			bucketTotal += item.count
		}
	}
	detail := fmt.Sprintf("%d / %d 个条目已有页数", countedItems, itemCount)
	if len(visibleBuckets) > 0 {
		parts := []string{}
		for _, item := range visibleBuckets {
			parts = append(parts, fmt.Sprintf("%d 个%s", item.count, item.label))
		}
		detail += "；" + strings.Join(parts, "；")
	} else {
		detail += "。"
	}

	issueLabel := "页数未完全统计"
	issueSubtype := "page"
	if len(visibleBuckets) == 1 && bucketTotal >= missingCount {
		issueSubtype = visibleBuckets[0].key
		if issueSubtype == "content" {
			issueLabel = "内容需要复核"
		} else {
			issueLabel = visibleBuckets[0].label
		}
	}
	return issueLabel, detail, "打开系列定位未完成条目；这里只记录本地复核状态。", issueSubtype
}

func filterReviewItems(items []map[string]any, issueFilter string, subtypeFilter string, statusFilter string) []map[string]any {
	filtered := []map[string]any{}
	for _, item := range items {
		if issueFilter != "" && stringValue(item["issue_type"]) != issueFilter {
			continue
		}
		if subtypeFilter != "" && stringValue(item["issue_subtype"]) != subtypeFilter {
			continue
		}
		status := stringValue(item["review_status"])
		switch {
		case statusFilter == "open":
			if status != "" && status != "open" {
				continue
			}
		case statusFilter == "reviewed":
			if status == "" || status == "open" || status == "needs_fix" {
				continue
			}
		case statusFilter != "" && statusFilter != "all":
			if status != statusFilter {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func reviewStatusBucket(status any) string {
	value := stringValue(status)
	if value == "" || value == "open" {
		return "open"
	}
	if value == "needs_fix" {
		return "needs_fix"
	}
	return "reviewed"
}

func reviewSummary(items []map[string]any) map[string]any {
	statuses := map[string]int{"all": 0, "open": 0, "needs_fix": 0, "reviewed": 0}
	issues := map[string]map[string]int{}
	subtypes := map[string]map[string]int{}
	for _, item := range items {
		bucket := reviewStatusBucket(item["review_status"])
		issue := stringValue(item["issue_type"])
		if issue == "" {
			issue = "review"
		}
		subtype := stringValue(item["issue_subtype"])
		if subtype == "" {
			subtype = issue
		}
		statuses["all"]++
		statuses[bucket]++

		if _, ok := issues[issue]; !ok {
			issues[issue] = map[string]int{"all": 0, "open": 0, "needs_fix": 0, "reviewed": 0}
		}
		issues[issue]["all"]++
		issues[issue][bucket]++

		if _, ok := subtypes[subtype]; !ok {
			subtypes[subtype] = map[string]int{"all": 0, "open": 0, "needs_fix": 0, "reviewed": 0}
		}
		subtypes[subtype]["all"]++
		subtypes[subtype][bucket]++
	}
	return map[string]any{
		"total":    statuses["all"],
		"statuses": statuses,
		"issues":   issues,
		"subtypes": subtypes,
	}
}
