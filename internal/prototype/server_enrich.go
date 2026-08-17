package prototype

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// These fields are read from existing local overrides for catalog display.
// The public core does not expose metadata import or override write routes.
var metadataOverrideFieldNames = []string{
	"title",
	"series",
	"circle",
	"author",
	"event",
	"parody",
	"volume",
	"language",
	"translation_sources",
	"possible_source_tags",
}

func (s *Server) applyMetadataOverrides(rows []map[string]any) error {
	identitySet := map[string]bool{}
	for _, row := range rows {
		identityID := stringValue(row["work_identity_id"])
		if identityID != "" {
			identitySet[identityID] = true
		}
	}
	if len(identitySet) == 0 {
		return nil
	}
	identities := make([]string, 0, len(identitySet))
	for identityID := range identitySet {
		identities = append(identities, identityID)
	}
	sort.Strings(identities)
	identityPlaceholders := strings.TrimRight(strings.Repeat("?,", len(identities)), ",")
	fieldPlaceholders := strings.TrimRight(strings.Repeat("?,", len(metadataOverrideFieldNames)), ",")
	args := make([]any, 0, len(metadataOverrideFieldNames)+len(identities))
	for _, fieldName := range metadataOverrideFieldNames {
		args = append(args, fieldName)
	}
	for _, identityID := range identities {
		args = append(args, identityID)
	}
	overrideRows, err := s.query(`
		SELECT id, work_identity_id, field_name, field_value, source_proposal_id, source_field_id, applied_at
		FROM metadata_field_overrides
		WHERE override_status = 'active'
		  AND field_name IN (`+fieldPlaceholders+`)
		  AND work_identity_id IN (`+identityPlaceholders+`)
		ORDER BY updated_at DESC, id DESC
	`, args...)
	if err != nil {
		return err
	}
	overrides := map[string]map[string]map[string]any{}
	for _, row := range overrideRows {
		identityID := stringValue(row["work_identity_id"])
		fieldName := stringValue(row["field_name"])
		if identityID == "" || fieldName == "" {
			continue
		}
		if overrides[identityID] == nil {
			overrides[identityID] = map[string]map[string]any{}
		}
		if overrides[identityID][fieldName] == nil {
			overrides[identityID][fieldName] = row
		}
	}
	for _, item := range rows {
		active := overrides[stringValue(item["work_identity_id"])]
		if len(active) == 0 {
			continue
		}
		fields := []string{}
		visible := map[string]map[string]any{}
		for _, fieldName := range metadataOverrideFieldNames {
			override := active[fieldName]
			if override == nil {
				continue
			}
			item["metadata_source_"+fieldName] = stringValue(item[fieldName])
			if fieldName == "title" || fieldName == "translation_sources" {
				item[fieldName] = stringValue(override["field_value"])
			}
			visible[fieldName] = map[string]any{
				"id":                 override["id"],
				"field_value":        override["field_value"],
				"source_proposal_id": override["source_proposal_id"],
				"source_field_id":    override["source_field_id"],
				"applied_at":         override["applied_at"],
			}
			fields = append(fields, fieldName)
		}
		if len(fields) > 0 {
			item["metadata_overridden_fields"] = fields
			item["metadata_overrides"] = visible
		}
	}
	return nil
}

func enrichWork(item map[string]any) {
	rawTitle := stringValue(item["title"])
	seriesTitle := stringValue(item["series_title"])
	itemRole := stringValue(item["item_role"])
	sequenceNumber := stringValue(item["sequence_number"])
	candidateType := stringValue(item["candidate_type"])

	itemTitle := cleanItemTitle(rawTitle, itemRole, sequenceNumber, seriesTitle)
	displayTitle := rawTitle
	displaySubtitle := seriesTitle
	if seriesTitle != "" && candidateType != "doujin" {
		if strings.Contains(rawTitle, seriesTitle) {
			displayTitle = rawTitle
		} else {
			displayTitle = strings.TrimSpace(seriesTitle + " " + itemTitle)
		}
		if rawTitle != itemTitle {
			displaySubtitle = rawTitle
		} else {
			displaySubtitle = seriesTitle
		}
	}

	item["display_title"] = displayTitle
	item["display_subtitle"] = displaySubtitle
	item["display_creator"] = workDisplayCreator(item)
	if strings.TrimSpace(stringValue(item["translation_sources"])) == "" && !metadataOverridePresent(item, "translation_sources") {
		item["translation_sources"] = strings.Join(titleTranslationSources(rawTitle), ", ")
	}
	item["display_library_name"] = displayLibraryName(item)
	item["can_read"] = canReadWork(item)
}

func workDisplayCreator(item map[string]any) string {
	author := metadataOverrideValue(item, "author")
	circle := metadataOverrideValue(item, "circle")
	if author != "" && circle != "" && !strings.EqualFold(author, circle) {
		return circle + " (" + author + ")"
	}
	if author != "" {
		return author
	}
	if circle != "" {
		return circle
	}
	if relationKind := stringValue(item["relation_kind"]); relationKind == "creator" || relationKind == "metadata_author" || relationKind == "title_creator" {
		if relationLabel := strings.TrimSpace(stringValue(item["relation_label"])); relationLabel != "" {
			return relationLabel
		}
	}
	return strings.Join(titleHintsFromWork(item).creators, " / ")
}

// fillMissingStructuredDisplayCreators adds a read-only presentation fallback
// after enrichWork has exhausted metadata overrides and title-derived creators.
// It intentionally leaves every existing display_creator untouched.
func (s *Server) fillMissingStructuredDisplayCreators(rows []map[string]any) error {
	missingIDs := make([]string, 0, len(rows))
	missingRows := map[string][]map[string]any{}
	for _, row := range rows {
		if strings.TrimSpace(stringValue(row["display_creator"])) != "" {
			continue
		}
		candidateID := strings.TrimSpace(stringValue(row["candidate_id"]))
		if candidateID == "" {
			continue
		}
		if _, exists := missingRows[candidateID]; !exists {
			missingIDs = append(missingIDs, candidateID)
		}
		missingRows[candidateID] = append(missingRows[candidateID], row)
	}
	if len(missingIDs) == 0 || !s.localTableExists("doujin_creator_items") {
		return nil
	}

	placeholders := strings.TrimRight(strings.Repeat("?,", len(missingIDs)), ",")
	args := make([]any, 0, len(missingIDs))
	for _, candidateID := range missingIDs {
		args = append(args, candidateID)
	}
	creatorRows, err := s.query(`
		SELECT candidate_id, creator_display
		FROM doujin_creator_items
		WHERE candidate_id IN (`+placeholders+`)
		  AND TRIM(COALESCE(creator_display, '')) <> ''
		ORDER BY candidate_id, creator_display COLLATE NOCASE, creator_display
	`, args...)
	if err != nil {
		return err
	}

	creatorsByID := map[string][]string{}
	for _, creatorRow := range creatorRows {
		candidateID := strings.TrimSpace(stringValue(creatorRow["candidate_id"]))
		creator := strings.TrimSpace(stringValue(creatorRow["creator_display"]))
		if candidateID == "" || creator == "" {
			continue
		}
		creatorsByID[candidateID] = append(creatorsByID[candidateID], creator)
	}
	for candidateID, creators := range creatorsByID {
		sort.SliceStable(creators, func(i, j int) bool {
			left := strings.ToLower(creators[i])
			right := strings.ToLower(creators[j])
			if left == right {
				return creators[i] < creators[j]
			}
			return left < right
		})
		unique := creators[:0]
		seen := map[string]bool{}
		for _, creator := range creators {
			key := strings.ToLower(creator)
			if seen[key] {
				continue
			}
			seen[key] = true
			unique = append(unique, creator)
		}
		displayCreator := strings.Join(unique, " / ")
		if displayCreator == "" {
			continue
		}
		for _, row := range missingRows[candidateID] {
			if strings.TrimSpace(stringValue(row["display_creator"])) == "" {
				row["display_creator"] = displayCreator
			}
		}
	}
	return nil
}

func canReadWork(item map[string]any) bool {
	if !publicReaderSourceSupported(item) {
		return false
	}
	switch stringValue(item["source_kind"]) {
	case "image_folder":
		return true
	case "archive":
		return stringValue(item["page_count_status"]) == "counted" && intValue(item["readable_page_count"]) > 0
	case "ebook":
		return stringValue(item["page_count_status"]) == "counted" && intValue(item["readable_page_count"]) > 0
	default:
		return false
	}
}

func publicReaderSourceSupported(item map[string]any) bool {
	switch stringValue(item["source_kind"]) {
	case "image_folder":
		return true
	case "archive":
		if !isZipCBZExtension(stringValue(item["extension"])) {
			return false
		}
		reason := strings.ToLower(strings.TrimSpace(stringValue(item["page_count_reason"])))
		return reason != "zip_contains_pdf" && reason != "zip_contains_pdf_from_sample"
	case "ebook":
		return isEPUBExtension(stringValue(item["extension"]))
	default:
		return false
	}
}

func enrichSeries(item map[string]any) {
	if stringValue(item["display_title"]) == "" {
		item["display_title"] = coalesceString(item["series_title"], item["title"])
	}
	item["display_library_name"] = displayLibraryName(item)

	itemCount := intValue(coalesceAny(item["item_count"], item["candidate_count"]))
	uniqueCount := intValue(item["unique_sequence_count"])
	if uniqueCount == 0 {
		uniqueCount = itemCount
	}
	sectionCount := intValue(item["section_count"])
	if sectionCount == 0 {
		sectionCount = 1
	}
	multiSectionCount := intValue(item["multi_section_count"])
	specialSectionCount := intValue(item["special_section_count"])
	seriesTitle := stringValue(item["series_title"])
	seriesKind := stringValue(item["series_kind"])
	seriesUnit := normalizeSeriesUnit(stringValue(item["series_unit"]))
	rangeCount, rangeUnit := seriesRangeCount(seriesTitle)
	if rangeCount > 0 && rangeCount <= itemCount && (uniqueCount <= 1 || uniqueCount <= maxInt(3, itemCount/10)) {
		uniqueCount = rangeCount
	}
	isCollectionTitle := collectionTitleRe.MatchString(seriesTitle)
	switch {
	case seriesUnit != "":
		item["item_label"] = seriesUnit
	case rangeUnit != "":
		item["item_label"] = rangeUnit
	case textHasVolumeMarker(seriesTitle):
		item["item_label"] = "卷"
	case textHasChapterMarker(seriesTitle):
		item["item_label"] = "话"
	default:
		item["item_label"] = "卷/话"
	}
	label := stringValue(item["item_label"])
	switch {
	case seriesKind == "normal_manga":
		item["item_summary"] = fmt.Sprintf("%d %s", firstPositive(uniqueCount, itemCount), label)
	case seriesKind == "collection" || (sectionCount > 1 && (isCollectionTitle || multiSectionCount >= 2 || specialSectionCount > 0)):
		noun := "分组"
		if seriesKind == "collection" || isCollectionTitle {
			noun = "收录作品"
		}
		item["item_summary"] = fmt.Sprintf("%d 个%s / %d 个条目", sectionCount, noun, itemCount)
	case isCollectionTitle:
		item["item_summary"] = fmt.Sprintf("%d 个收录条目", itemCount)
	case uniqueCount > 0 && uniqueCount < itemCount:
		item["item_summary"] = fmt.Sprintf("%d %s / %d 个条目", uniqueCount, label, itemCount)
	default:
		item["item_summary"] = fmt.Sprintf("%d %s", itemCount, label)
	}
}

func normalizeSeriesUnit(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "chapter", "话", "話", "回":
		return "话"
	case "volume", "卷", "巻", "册", "冊":
		return "卷"
	default:
		return ""
	}
}

func cleanItemTitle(title, itemRole, sequenceNumber, seriesTitle string) string {
	value := strings.TrimSpace(strings.ReplaceAll(title, "_", " "))
	if loc := leadingNumberRe.FindStringIndex(value); loc != nil && loc[1] < len(value) && strings.TrimSpace(value[loc[1]:]) != "" {
		value = strings.TrimSpace(value[loc[1]:])
	}
	if numberOnlyRe.MatchString(value) {
		if number, err := strconv.Atoi(value); err == nil && number > 0 {
			return fmt.Sprintf("第%d%s", number, defaultNumberUnit(itemRole, seriesTitle, value))
		}
	}
	if value != "" {
		return value
	}
	if sequenceNumber != "" {
		if number, err := strconv.ParseFloat(normalizeDigits(sequenceNumber), 64); err == nil {
			return fmt.Sprintf("第%d%s", int(number), defaultNumberUnit(itemRole, seriesTitle, title))
		}
	}
	return title
}

func displayLibraryName(row map[string]any) string {
	key := stringValue(row["library_key"])
	name := stringValue(row["library_name"])
	switch key {
	case "commercial-manga":
		return "商业漫画"
	case "doujin-lanraragi":
		return "同人本"
	case "manga-test":
		return "测试库"
	default:
		if name != "" {
			return name
		}
		return key
	}
}

func defaultSeriesLibraryKeys() []string {
	return []string{"commercial-manga", "manga-test"}
}

func addDefaultSeriesLibraryFilter(filters *[]string, args *[]any, alias string) {
	keys := defaultSeriesLibraryKeys()
	placeholders := make([]string, 0, len(keys))
	for range keys {
		placeholders = append(placeholders, "?")
	}
	prefix := strings.TrimSpace(alias)
	if prefix != "" {
		prefix += "."
	}
	*filters = append(*filters, prefix+"library_key IN ("+strings.Join(placeholders, ", ")+")")
	for _, key := range keys {
		*args = append(*args, key)
	}
}

func defaultNumberUnit(role string, seriesTitle string, values ...string) string {
	switch role {
	case "chapter":
		return "话"
	case "volume":
		return "卷"
	}
	joined := strings.Join(append([]string{seriesTitle}, values...), " ")
	if textHasVolumeMarker(joined) {
		return "卷"
	}
	if textHasChapterMarker(joined) {
		return "话"
	}
	return "卷"
}

func textHasChapterMarker(value string) bool {
	return chapterMarkerRe.MatchString(value)
}

func textHasVolumeMarker(value string) bool {
	return volumeMarkerRe.MatchString(value)
}

func seriesRangeCount(value string) (int, string) {
	match := rangeCountRe.FindStringSubmatch(value)
	if len(match) == 0 {
		return 0, ""
	}
	start, err1 := strconv.Atoi(normalizeDigits(match[1]))
	end, err2 := strconv.Atoi(normalizeDigits(match[2]))
	if err1 != nil || err2 != nil || start <= 0 || end < start || end-start > 1000 {
		return 0, ""
	}
	return end - start + 1, normalizeNumberUnit(match[3])
}

func normalizeNumberUnit(unit string) string {
	switch unit {
	case "巻", "卷", "册", "冊":
		return "卷"
	case "話":
		return "话"
	default:
		if unit == "" {
			return "卷"
		}
		return unit
	}
}

func normalizeDigits(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if r >= '０' && r <= '９' {
			builder.WriteRune('0' + (r - '０'))
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}
