package prototype

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	editionFolderRe       = regexp.MustCompile(`(?i)正篇|本篇|番外|外传|外傳|特典|全彩|黑白|单行本|單行本|卷全|巻全|台版|港版|中文版|汉化|漢化|扫图|掃圖|完结|完結|vol(?:ume)?|darkness|toloveru`)
	sourceTagTextRe       = regexp.MustCompile(`(?i)汉化|漢化|翻译|翻訳|中文|中国|中國|扫图|掃圖|修图|修圖|嵌字|水晶海`)
	bracketTagRe          = regexp.MustCompile(`\[([^\]]{1,48})\]`)
	leadingBracketTagsRe  = regexp.MustCompile(`^(?:\[[^\]]+\]\s*)+`)
	multiSpaceRe          = regexp.MustCompile(`\s{2,}`)
	numberishTitleRe      = regexp.MustCompile(`^[0-9０-９_.\-\s]+$`)
	specialCatalogRe      = regexp.MustCompile(`(?i)资料集|資料集|公式合集|公式书|公式書|公式|guidebook|fanbook|outside|inside`)
	extraNumberRe         = regexp.MustCompile(`(?i)(?:番外篇?|番外|extra|special)\s*[_ .:-]*([0-9０-９]+(?:\.[0-9０-９]+)?)`)
	explicitUnitRe        = regexp.MustCompile(`第\s*([0-9０-９]+(?:\.[0-9０-９]+)?)\s*(卷|巻|集|话|話|回)`)
	volNumberRe           = regexp.MustCompile(`(?i)(?:vol(?:ume)?|卷|巻|集)\s*[_ .-]*([0-9０-９]+(?:\.[0-9０-９]+)?)`)
	chapterNumberRe       = regexp.MustCompile(`(?i)(?:ch(?:apter)?|话|話|回)\s*[_ .-]*([0-9０-９]+(?:\.[0-9０-９]+)?)`)
	leadingUnitRe         = regexp.MustCompile(`(?:^|[_ .])([0-9０-９]{1,4}(?:\.[0-9０-９]+)?)\s*(卷|巻|集|话|話|回)([^\\/]*)$`)
	suffixNumberRe        = regexp.MustCompile(`(?:^|[_ .-])([0-9０-９]{1,4})(?:\s*(?:\[[^\]]+\]|\([^)]*\)))*$`)
	anyNumberRe           = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)`)
	compactBracketStripRe = regexp.MustCompile(`\[[^\]]+\]`)
)

func (s *Server) handleSeriesDetail(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	groupID := strings.TrimSpace(r.URL.Query().Get("id"))
	if groupID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing id")
		return
	}

	seriesRows, err := s.query(fmt.Sprintf(`
		SELECT
			sg.group_id,
			sg.library_key,
			sg.series_title,
			sg.group_path,
			sg.group_type,
			sg.candidate_count,
			COALESCE(stats.unique_sequence_count, sg.candidate_count) AS unique_sequence_count,
			COALESCE(stats.item_count, sg.candidate_count) AS item_count,
			COALESCE(section_stats.section_count, 1) AS section_count,
			COALESCE(section_stats.multi_section_count, 0) AS multi_section_count,
			COALESCE(section_stats.special_section_count, 0) AS special_section_count,
			sg.confidence,
			COALESCE(cover_override.candidate_id, safe_cover.selected_candidate_id, scc.selected_candidate_id) AS selected_candidate_id,
			cover_choice.correction_value AS manual_cover_candidate_id,
			kind_choice.correction_value AS series_kind,
			unit_choice.correction_value AS series_unit,
			COALESCE(cover_override.cover_status, safe_cover.cover_status, scc.cover_status) AS cover_status,
			COALESCE(cover_override.cover_kind, safe_cover.cover_kind, scc.cover_kind) AS cover_kind,
			COALESCE(cover_override.cover_source_path, safe_cover.cover_source_path, scc.cover_source_path) AS cover_source_path,
			COALESCE(cover_override.requires_extraction, safe_cover.requires_extraction, scc.requires_extraction) AS requires_extraction
		FROM series_groups sg
		LEFT JOIN series_cover_candidates scc
			ON scc.group_id = sg.group_id
		   AND %s
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
				END) AS unique_sequence_count
			FROM series_items si
			JOIN work_browse stats_wb ON stats_wb.candidate_id = si.candidate_id
			GROUP BY si.group_id
		) stats ON stats.group_id = sg.group_id
		%s
		WHERE sg.group_id = ?
		  AND %s
	`, visibleWorkCandidateExistsSQL("scc.selected_candidate_id"), safeSeriesCoverJoinSQL(), seriesCoverOverrideJoinSQL(), seriesKindJoinSQL(), seriesSectionStatsJoinSQL(), seriesHasVisibleMemberSQL("sg")), groupID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(seriesRows) == 0 {
		writeJSONError(w, http.StatusNotFound, "series not found")
		return
	}

	itemRows, err := s.query(`
		SELECT
			wb.candidate_id,
			wb.work_identity_id,
			wb.library_key,
			wb.library_name,
			wb.candidate_type,
			wb.source_kind,
			COALESCE(wc_source.source_record_id, '') AS source_record_id,
			wb.title,
			wb.path,
			wb.relative_path,
			wb.size_bytes,
			wb.extension,
			wb.page_count_status,
			wb.readable_page_count,
			wb.cover_status,
			wb.cover_kind,
			wb.translation_sources,
			si.series_title,
			si.item_role,
			si.sequence_number,
			si.sort_key,
			`+workListProgressSelectSQL()+`
		FROM series_items si
		JOIN work_browse wb ON wb.candidate_id = si.candidate_id
		LEFT JOIN work_candidates wc_source ON wc_source.candidate_id = si.candidate_id
		`+workListProgressJoinSQL("wb.work_identity_id")+`
		WHERE si.group_id = ?
		ORDER BY si.sort_key, wb.title, wb.candidate_id
	`, groupID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	coverRows, err := s.query(`
		SELECT
			wb.candidate_id,
			wb.work_identity_id,
			wb.library_key,
			wb.library_name,
			wb.candidate_type,
			wb.source_kind,
			wb.title,
			wb.path,
			wb.relative_path,
			wb.size_bytes,
			wb.extension,
			wb.page_count_status,
			wb.readable_page_count,
			wb.cover_status,
			wb.cover_kind,
			wb.translation_sources,
			si.series_title,
			si.item_role,
			si.sequence_number,
			si.sort_key,
			wcc.cover_source_path,
			wcc.cover_source_relative_path
		FROM series_items si
		JOIN work_browse wb ON wb.candidate_id = si.candidate_id
		JOIN work_cover_candidates wcc ON wcc.candidate_id = si.candidate_id
		WHERE si.group_id = ?
		  AND wcc.cover_status = 'ready'
		  AND wcc.cover_kind IN ('page_image', 'archive', 'pdf', 'ebook')
		ORDER BY si.sort_key, wb.title, wb.candidate_id
	`, groupID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	mark, err := s.getSeriesUserMark(groupID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	series := seriesRows[0]
	enrichSeries(series)
	if err := s.applyMetadataOverrides(itemRows); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.applyMetadataOverrides(coverRows); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(itemRows))
	for _, row := range itemRows {
		enrichWork(row)
		attachWorkListProgress(row)
		row["item_label"] = chapterLabelFor(row, series)
		items = append(items, row)
	}
	sections, sectioned, sectionSummary := buildSeriesSections(items, series)
	primaryCoverIDs := primaryIDsFromSections(sections)
	coverGroupMeta := coverGroupMetaFromSections(sections)
	manualCoverID := stringValue(series["manual_cover_candidate_id"])
	coverCandidates := make([]map[string]any, 0, len(coverRows))
	for _, row := range coverRows {
		candidateID := stringValue(row["candidate_id"])
		if len(primaryCoverIDs) > 0 && !primaryCoverIDs[candidateID] && candidateID != manualCoverID {
			continue
		}
		enrichWork(row)
		coverSource := coalesceString(row["cover_source_relative_path"], row["cover_source_path"])
		if hasCoverMarker(coverSource) {
			row["cover_warning"] = "可能是说明页"
		} else {
			row["cover_warning"] = ""
		}
		meta := coverGroupMeta[candidateID]
		row["cover_section_label"] = meta["section"]
		if meta["label"] != "" {
			row["chapter_label"] = meta["label"]
		} else {
			row["chapter_label"] = chapterLabelFor(row, series)
		}
		coverCandidates = append(coverCandidates, row)
	}
	disambiguateCoverCandidateLabels(coverCandidates, series)

	writeJSON(w, map[string]any{
		"series":           series,
		"items":            items,
		"sections":         sections,
		"sectioned":        sectioned,
		"section_summary":  sectionSummary,
		"cover_candidates": coverCandidates,
		"mark":             mark,
	})
}

func effectiveSeriesKind(series map[string]any) string {
	value := stringValue(series["series_kind"])
	if value == "normal_manga" || value == "collection" {
		return value
	}
	return ""
}

func pathParts(value any) []string {
	parts := regexp.MustCompile(`[\\/]+`).Split(stringValue(value), -1)
	filtered := []string{}
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return filtered
}

func itemPartsUnderSeries(item map[string]any, series map[string]any) []string {
	itemParts := pathParts(item["relative_path"])
	seriesParts := pathParts(series["group_path"])
	index := 0
	for index < len(seriesParts) && index < len(itemParts) && itemParts[index] == seriesParts[index] {
		index++
	}
	return itemParts[index:]
}

func isCollectionTitle(series map[string]any) bool {
	switch effectiveSeriesKind(series) {
	case "normal_manga":
		return false
	case "collection":
		return true
	}
	return collectionTitleRe.MatchString(coalesceString(series["display_title"], series["series_title"]))
}

func isEditionFolder(name any) bool {
	return editionFolderRe.MatchString(stringValue(name))
}

func isSpecialSectionTitle(title any) bool {
	return sectionSpecialLikeRe.MatchString(stringValue(title))
}

func collectionLabelFor(item map[string]any, series map[string]any) string {
	parts := itemPartsUnderSeries(item, series)
	if len(parts) <= 1 {
		return "本篇"
	}
	folders := parts[:len(parts)-1]
	if len(folders) == 0 {
		return "本篇"
	}
	if len(folders) >= 2 && folders[0] == folders[1] {
		return folders[0]
	}
	if isCollectionTitle(series) {
		if folders[0] != "" {
			return folders[0]
		}
		return "本篇"
	}
	depth := 1
	maxDepth := len(folders)
	if maxDepth > 3 {
		maxDepth = 3
	}
	for index := 1; index < maxDepth; index++ {
		previous := folders[index-1]
		current := folders[index]
		if current == "" || current == previous {
			continue
		}
		if stringValue(item["item_role"]) == "chapter" || stringValue(item["item_role"]) == "special" || isEditionFolder(previous) || isEditionFolder(current) {
			depth = index + 1
		}
	}
	if depth > len(folders) {
		depth = len(folders)
	}
	label := strings.Join(folders[:depth], " / ")
	if label == "" {
		return "本篇"
	}
	return label
}

func repeatedLabelsAcrossSections(sections []map[string]any) int {
	seen := map[string]map[string]bool{}
	for _, section := range sections {
		title := stringValue(section["title"])
		for _, group := range sectionGroups(section) {
			label := stringValue(group["label"])
			if seen[label] == nil {
				seen[label] = map[string]bool{}
			}
			seen[label][title] = true
		}
	}
	count := 0
	for _, sectionNames := range seen {
		if len(sectionNames) > 1 {
			count++
		}
	}
	return count
}

func hasCollectionSectionsBackend(sections []map[string]any, series map[string]any) bool {
	switch effectiveSeriesKind(series) {
	case "normal_manga":
		return false
	case "collection":
		return len(sections) > 0
	}
	if len(sections) <= 1 {
		return false
	}
	if isCollectionTitle(series) {
		return true
	}
	for _, section := range sections {
		if stringValue(section["title"]) != "本篇" && isSpecialSectionTitle(section["title"]) {
			return true
		}
	}
	multiItemSections := 0
	for _, section := range sections {
		if len(sectionGroups(section)) > 1 {
			multiItemSections++
		}
	}
	if multiItemSections >= 2 {
		return true
	}
	return repeatedLabelsAcrossSections(sections) > 0 && multiItemSections >= 2
}

func sectionSummaryBackend(series map[string]any, sections []map[string]any, itemCount int) string {
	noun := "分组"
	if effectiveSeriesKind(series) == "collection" || isCollectionTitle(series) {
		noun = "收录作品"
	}
	return fmt.Sprintf("%d 个%s / %d 个条目", len(sections), noun, itemCount)
}

func trimNumberText(value any) string {
	normalized := normalizeDigits(stringValue(value))
	parsed, err := strconv.ParseFloat(normalized, 64)
	if err != nil {
		return ""
	}
	if parsed == float64(int64(parsed)) {
		return strconv.FormatInt(int64(parsed), 10)
	}
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(parsed, 'f', -1, 64), "0"), ".")
}

func itemTitleLooksNumberOnly(title any) bool {
	return numberishTitleRe.MatchString(stringValue(title))
}

func stripLeadingBracketTags(value any) string {
	return strings.TrimSpace(leadingBracketTagsRe.ReplaceAllString(stringValue(value), ""))
}

func isSourceTagText(value any) bool {
	return sourceTagTextRe.MatchString(stringValue(value))
}

func sourceTagFor(item map[string]any) string {
	text := stringValue(item["title"]) + " " + stringValue(item["relative_path"])
	for _, match := range bracketTagRe.FindAllStringSubmatch(text, -1) {
		tag := strings.TrimSpace(match[1])
		if isSourceTagText(tag) {
			return tag
		}
	}
	return ""
}

func stripSourceTags(value any) string {
	replaced := bracketTagRe.ReplaceAllStringFunc(stringValue(value), func(match string) string {
		sub := bracketTagRe.FindStringSubmatch(match)
		if len(sub) > 1 && isSourceTagText(sub[1]) {
			return ""
		}
		return match
	})
	return strings.TrimSpace(multiSpaceRe.ReplaceAllString(replaced, " "))
}

func cleanChapterFallbackTitle(value any, item map[string]any, series map[string]any) string {
	seriesTitle := coalesceString(series["display_title"], series["series_title"])
	title := strings.TrimSpace(strings.ReplaceAll(stripSourceTags(stripLeadingBracketTags(value)), "_", " "))
	if seriesTitle != "" && strings.HasPrefix(title, seriesTitle) {
		title = strings.TrimSpace(strings.TrimPrefix(title, seriesTitle))
	}
	tag := sourceTagFor(item)
	if tag != "" {
		title = strings.ReplaceAll(title, tag, "")
		title = strings.ReplaceAll(title, "[]", "")
		title = strings.TrimSpace(multiSpaceRe.ReplaceAllString(title, " "))
	}
	return title
}

func specialCatalogLabel(item map[string]any, series map[string]any) string {
	text := stringValue(item["title"]) + " " + stringValue(item["relative_path"])
	if !specialCatalogRe.MatchString(text) {
		return ""
	}
	cleaned := cleanChapterFallbackTitle(item["title"], item, series)
	cleaned = regexp.MustCompile(`(?i)eighttalesofthezqn01end`).ReplaceAllString(cleaned, "Eight Tales")
	cleaned = strings.Trim(cleaned, " -_")
	if cleaned == "" {
		return "资料/公式"
	}
	if len([]rune(cleaned)) > 22 {
		switch {
		case regexp.MustCompile(`公式合集|公式书|公式書|公式`).MatchString(cleaned):
			return "公式书"
		case regexp.MustCompile(`(?i)outside`).MatchString(cleaned):
			return "资料集 OUTSIDE"
		case regexp.MustCompile(`(?i)inside`).MatchString(cleaned):
			return "资料集 INSIDE"
		case regexp.MustCompile(`资料集|資料集`).MatchString(cleaned):
			return "资料集"
		}
	}
	return cleaned
}

func parseNumberedLabel(value any, role string, seriesTitle string, preferredUnit string) string {
	text := stringValue(value)
	if match := extraNumberRe.FindStringSubmatch(text); len(match) > 0 {
		return "番外" + trimNumberText(match[1])
	}
	if match := explicitUnitRe.FindStringSubmatch(text); len(match) > 0 {
		return "第" + trimNumberText(match[1]) + normalizeNumberUnit(match[2])
	}
	if match := volNumberRe.FindStringSubmatch(text); len(match) > 0 {
		unit := "卷"
		if strings.Contains(text, "集") {
			unit = "集"
		}
		return "第" + trimNumberText(match[1]) + unit
	}
	if match := chapterNumberRe.FindStringSubmatch(text); len(match) > 0 {
		return "第" + trimNumberText(match[1]) + "话"
	}
	if match := leadingUnitRe.FindStringSubmatch(text); len(match) > 0 {
		label := "第" + trimNumberText(match[1]) + normalizeNumberUnit(match[2])
		tail := strings.Trim(match[3], " _.-")
		if tail != "" && !strings.HasPrefix(tail, "[") && !strings.HasPrefix(tail, "(") && !strings.HasPrefix(tail, "（") && len([]rune(tail)) <= 24 {
			label += tail
		}
		return label
	}
	if regexp.MustCompile(`(?i)番外篇?|extra|special`).MatchString(text) {
		return "番外"
	}
	parts := regexp.MustCompile(`[\\/]`).Split(text, -1)
	leaf := ""
	if len(parts) > 0 {
		leaf = parts[len(parts)-1]
	}
	if match := suffixNumberRe.FindStringSubmatch(leaf); len(match) > 0 {
		number := trimNumberText(match[1])
		if number != "" {
			unit := preferredUnit
			if unit == "" {
				unit = defaultNumberUnit(role, seriesTitle, text)
			}
			return "第" + number + unit
		}
	}
	return ""
}

func isSeriesRootItem(item map[string]any, series map[string]any) bool {
	return stringValue(item["relative_path"]) == stringValue(series["group_path"])
}

func chapterLabelFor(item map[string]any, series map[string]any) string {
	seriesTitle := coalesceString(series["display_title"], series["series_title"])
	rawTitle := strings.TrimSpace(coalesceString(item["title"], item["display_title"]))
	cleanedTitle := stripLeadingBracketTags(rawTitle)
	role := stringValue(item["item_role"])
	preferredUnit := normalizeSeriesUnit(stringValue(series["series_unit"]))
	if isSeriesRootItem(item, series) && intValue(item["readable_page_count"]) <= 2 {
		return "目录页/说明页"
	}
	for _, value := range []string{rawTitle, cleanedTitle, stringValue(item["relative_path"])} {
		if parsed := parseNumberedLabel(value, role, seriesTitle, preferredUnit); parsed != "" {
			return parsed
		}
	}
	if special := specialCatalogLabel(item, series); special != "" {
		return special
	}
	sequenceNumber := stringValue(item["sequence_number"])
	if sequenceNumber != "" {
		if number := trimNumberText(sequenceNumber); number != "" {
			unit := preferredUnit
			if unit == "" {
				unit = defaultNumberUnit(role, seriesTitle, rawTitle, stringValue(item["relative_path"]))
			}
			return "第" + number + unit
		}
	}
	fallbackCleaned := cleanChapterFallbackTitle(cleanedTitle, item, series)
	if fallbackCleaned != "" && !itemTitleLooksNumberOnly(fallbackCleaned) {
		return fallbackCleaned
	}
	fallbackRaw := cleanChapterFallbackTitle(rawTitle, item, series)
	if fallbackRaw != "" && !itemTitleLooksNumberOnly(fallbackRaw) {
		return fallbackRaw
	}
	title := coalesceString(item["display_title"], item["title"])
	if seriesTitle != "" && strings.HasPrefix(title, seriesTitle) {
		title = strings.TrimSpace(strings.TrimPrefix(title, seriesTitle))
	}
	if title != "" {
		return title
	}
	return coalesceString(item["display_title"], item["title"])
}

func chapterSortNumber(item map[string]any, label string) float64 {
	if match := anyNumberRe.FindStringSubmatch(normalizeDigits(label)); len(match) > 0 {
		if parsed, err := strconv.ParseFloat(match[1], 64); err == nil {
			return parsed
		}
	}
	if fromItem := trimNumberText(item["sequence_number"]); fromItem != "" {
		if value, err := strconv.ParseFloat(fromItem, 64); err == nil && value > 0 {
			return value
		}
	}
	return 1_000_000_000.0
}

func buildSeriesSections(items []map[string]any, series map[string]any) ([]map[string]any, bool, string) {
	forceNormal := effectiveSeriesKind(series) == "normal_manga"
	sectionMap := map[string]map[string]any{}
	sectionOrder := []string{}
	for index, item := range items {
		sectionTitle := "本篇"
		if !forceNormal {
			sectionTitle = collectionLabelFor(item, series)
		}
		if sectionMap[sectionTitle] == nil {
			sectionMap[sectionTitle] = map[string]any{"title": sectionTitle, "sort": index, "groups_map": map[string]map[string]any{}}
			sectionOrder = append(sectionOrder, sectionTitle)
		}
		label := chapterLabelFor(item, series)
		groupKey := strings.Join([]string{sectionTitle, stringValue(item["item_role"]), label}, "|")
		if forceNormal {
			groupKey = strings.Join([]string{"本篇", stringValue(item["item_role"]), label}, "|")
		}
		groups := sectionMap[sectionTitle]["groups_map"].(map[string]map[string]any)
		if groups[groupKey] == nil {
			groups[groupKey] = map[string]any{
				"key":      groupKey,
				"label":    label,
				"sort":     index,
				"sequence": chapterSortNumber(item, label),
				"items":    []map[string]any{},
				"primary":  nil,
			}
		}
		group := groups[groupKey]
		group["items"] = append(group["items"].([]map[string]any), item)
		primary, _ := group["primary"].(map[string]any)
		if primary == nil || (boolValue(item["can_read"]) && !boolValue(primary["can_read"])) {
			group["primary"] = item
		}
	}

	sections := make([]map[string]any, 0, len(sectionMap))
	for _, title := range sectionOrder {
		section := sectionMap[title]
		groupsMap := section["groups_map"].(map[string]map[string]any)
		groups := make([]map[string]any, 0, len(groupsMap))
		for _, group := range groupsMap {
			groups = append(groups, group)
		}
		sort.SliceStable(groups, func(i, j int) bool {
			ai, aj := numericFloat(groups[i]["sequence"], 1_000_000_000.0), numericFloat(groups[j]["sequence"], 1_000_000_000.0)
			if ai != aj {
				return ai < aj
			}
			si, sj := intValue(groups[i]["sort"]), intValue(groups[j]["sort"])
			if si != sj {
				return si < sj
			}
			return stringValue(groups[i]["label"]) < stringValue(groups[j]["label"])
		})
		sections = append(sections, map[string]any{"title": section["title"], "sort": section["sort"], "groups": groups})
	}

	sectioned := false
	if !forceNormal {
		sectioned = hasCollectionSectionsBackend(sections, series)
		if sectioned {
			sort.SliceStable(sections, func(i, j int) bool {
				si, sj := intValue(sections[i]["sort"]), intValue(sections[j]["sort"])
				if si != sj {
					return si < sj
				}
				return stringValue(sections[i]["title"]) < stringValue(sections[j]["title"])
			})
		}
	}
	return sections, sectioned, sectionSummaryBackend(series, sections, len(items))
}

func numericFloat(value any, fallback float64) float64 {
	raw := strings.TrimSpace(fmt.Sprint(value))
	if raw == "" || raw == "<nil>" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func primaryIDsFromSections(sections []map[string]any) map[string]bool {
	ids := map[string]bool{}
	for _, section := range sections {
		for _, group := range sectionGroups(section) {
			if primary, ok := group["primary"].(map[string]any); ok {
				if id := stringValue(primary["candidate_id"]); id != "" {
					ids[id] = true
				}
			}
		}
	}
	return ids
}

func coverGroupMetaFromSections(sections []map[string]any) map[string]map[string]string {
	meta := map[string]map[string]string{}
	for _, section := range sections {
		sectionTitle := stringValue(section["title"])
		for _, group := range sectionGroups(section) {
			label := stringValue(group["label"])
			for _, item := range groupItems(group) {
				if id := stringValue(item["candidate_id"]); id != "" {
					meta[id] = map[string]string{"section": sectionTitle, "label": label}
				}
			}
			if primary, ok := group["primary"].(map[string]any); ok {
				if id := stringValue(primary["candidate_id"]); id != "" {
					meta[id] = map[string]string{"section": sectionTitle, "label": label}
				}
			}
		}
	}
	return meta
}

func compactCoverSectionLabel(value any, series map[string]any) string {
	text := strings.TrimSpace(stringValue(value))
	if text == "" || text == "本篇" {
		return ""
	}
	known := []struct {
		pattern string
		label   string
	}{
		{`(?i)before\s*the\s*fall`, "Before the fall"},
		{`(?i)lost\s*girl`, "Lost Girl"},
		{`无悔|悔いなき`, "无悔的选择"},
		{`汉化组版本|漢化組版本|按话分类|按話分類`, "汉化组版本"},
		{`外传\+资料集|外傳\+資料集`, "外传/资料"},
	}
	for _, item := range known {
		if regexp.MustCompile(item.pattern).MatchString(text) {
			return item.label
		}
	}
	seriesTitle := coalesceString(series["display_title"], series["series_title"])
	cleaned := strings.ReplaceAll(stripSourceTags(stripLeadingBracketTags(text)), "_", " ")
	parts := []string{}
	for _, rawPart := range strings.Split(cleaned, "/") {
		rawPart = strings.TrimSpace(rawPart)
		if rawPart == "" {
			continue
		}
		withoutTags := strings.Trim(compactBracketStripRe.ReplaceAllString(rawPart, ""), " -_[]()（）")
		if withoutTags != "" {
			parts = append(parts, withoutTags)
		} else if !regexp.MustCompile(`^(?:\[[^\]]+\]\s*)+$`).MatchString(rawPart) {
			parts = append(parts, strings.Trim(rawPart, " -_[]()（）"))
		}
	}
	if len(parts) > 0 {
		cleaned = parts[len(parts)-1]
	} else {
		cleaned = strings.Trim(cleaned, " -_[]()（）")
	}
	if seriesTitle != "" && cleaned == seriesTitle {
		return "本篇"
	}
	if seriesTitle != "" && strings.HasPrefix(cleaned, seriesTitle) {
		suffix := strings.Trim(strings.TrimPrefix(cleaned, seriesTitle), " -_[]()（）")
		if len([]rune(suffix)) >= 3 {
			cleaned = suffix
		}
	}
	cleaned = strings.TrimSpace(multiSpaceRe.ReplaceAllString(cleaned, " "))
	if len([]rune(cleaned)) > 18 {
		cleaned = string([]rune(cleaned)[:18]) + "..."
	}
	return cleaned
}

func compactCoverItemContextLabel(item map[string]any, series map[string]any) string {
	if special := specialCatalogLabel(item, series); special != "" {
		return special
	}
	title := cleanChapterFallbackTitle(item["title"], item, series)
	title = strings.Trim(compactBracketStripRe.ReplaceAllString(title, ""), " -_[]()（）")
	title = strings.TrimSpace(multiSpaceRe.ReplaceAllString(title, " "))
	if title == "" || itemTitleLooksNumberOnly(title) {
		return ""
	}
	if len([]rune(title)) > 18 {
		title = string([]rune(title)[:18]) + "..."
	}
	return title
}

func disambiguateCoverCandidateLabels(candidates []map[string]any, series map[string]any) {
	counts := map[string]int{}
	for _, item := range candidates {
		counts[stringValue(item["chapter_label"])]++
	}
	for _, item := range candidates {
		label := stringValue(item["chapter_label"])
		if counts[label] <= 1 {
			continue
		}
		sectionLabel := stringValue(item["cover_section_label"])
		context := compactCoverSectionLabel(sectionLabel, series)
		if context == "" && sectionLabel == "本篇" {
			context = "本篇"
		}
		if context == "" {
			context = compactCoverItemContextLabel(item, series)
		}
		if context != "" && context != label && !strings.HasPrefix(label, context+" / ") {
			item["chapter_label"] = context + " / " + label
		}
	}
	finalCounts := map[string]int{}
	for _, item := range candidates {
		finalCounts[stringValue(item["chapter_label"])]++
	}
	seen := map[string]int{}
	for _, item := range candidates {
		label := stringValue(item["chapter_label"])
		if finalCounts[label] <= 1 {
			continue
		}
		seen[label]++
		item["chapter_label"] = fmt.Sprintf("%s（版本%d）", label, seen[label])
	}
}

func hasCoverMarker(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range coverExcludeMarkers {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func sectionGroups(section map[string]any) []map[string]any {
	switch groups := section["groups"].(type) {
	case []map[string]any:
		return groups
	case []any:
		result := []map[string]any{}
		for _, item := range groups {
			if group, ok := item.(map[string]any); ok {
				result = append(result, group)
			}
		}
		return result
	default:
		return nil
	}
}

func groupItems(group map[string]any) []map[string]any {
	switch items := group["items"].(type) {
	case []map[string]any:
		return items
	case []any:
		result := []map[string]any{}
		for _, item := range items {
			if row, ok := item.(map[string]any); ok {
				result = append(result, row)
			}
		}
		return result
	default:
		return nil
	}
}

func boolValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case int64:
		return v != 0
	case string:
		return v == "1" || strings.EqualFold(v, "true")
	default:
		return false
	}
}
