package prototype

import (
	"net/http"
	"sort"
	"strings"
)

type continueTargetSeriesEntry struct {
	item     map[string]any
	eligible bool
	state    string
}

type continueTargetSeriesGroup struct {
	entries   []continueTargetSeriesEntry
	primaryID string
}

func (s *Server) handleContinueTarget(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	target, err := s.queryContinueTarget()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"target": target})
}

func (s *Server) queryContinueTarget() (map[string]any, error) {
	anchors, err := s.query(`
		SELECT
			rp.work_identity_id,
			rp.candidate_id AS progress_candidate_id,
			rp.completed,
			rp.last_page_index,
			rp.reader_split_panel,
			rp.stage_scroll_top,
			rp.stage_scroll_left,
			rp.last_read_at,
			rp.updated_at,
			wi.current_candidate_id,
			CASE
				WHEN COALESCE(wum.hidden, 0) = 0
				THEN 1
				ELSE 0
			END AS eligible
		FROM reading_progress rp
		JOIN work_identities wi ON wi.work_identity_id = rp.work_identity_id
		JOIN work_browse wb ON wb.candidate_id = wi.current_candidate_id
		LEFT JOIN work_user_marks wum
			ON wum.reader_profile_key = 'default'
		   AND wum.work_identity_id = rp.work_identity_id
		WHERE rp.reader_profile_key = 'default'
		  AND COALESCE(NULLIF(rp.last_read_at, ''), NULLIF(rp.updated_at, ''), '') <> ''
		ORDER BY
			datetime(COALESCE(NULLIF(rp.last_read_at, ''), rp.updated_at)) DESC,
			datetime(rp.updated_at) DESC,
			rp.work_identity_id
	`)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(anchors, func(left, right int) bool {
		return compareSeriesResumeRanks(
			seriesResumeRankFromRow(anchors[left]),
			seriesResumeRankFromRow(anchors[right]),
		) > 0
	})

	seenSeries := map[string]bool{}
	for _, anchor := range anchors {
		currentCandidateID := stringValue(anchor["current_candidate_id"])
		progressCandidateID := stringValue(anchor["progress_candidate_id"])
		memberships, err := s.continueTargetSeriesMemberships(currentCandidateID, progressCandidateID)
		if err != nil {
			return nil, err
		}
		if len(memberships) > 0 {
			for _, membership := range memberships {
				groupID := stringValue(membership["group_id"])
				if groupID == "" || seenSeries[groupID] {
					continue
				}
				seenSeries[groupID] = true
				target, err := s.continueTargetForSeries(groupID, stringValue(membership["series_title"]))
				if err != nil {
					return nil, err
				}
				if target != nil {
					return target, nil
				}
			}
			continue
		}

		if !boolValue(anchor["eligible"]) || currentCandidateID == "" {
			continue
		}
		items, err := s.loadWorkListDetails([]string{currentCandidateID})
		if err != nil {
			return nil, err
		}
		if len(items) == 0 || !boolValue(items[0]["can_read"]) {
			continue
		}
		progress := continueTargetProgress(items[0])
		if progress == nil || continueTargetEffectiveReadState(items[0]) != "reading" {
			continue
		}
		return map[string]any{
			"item":      items[0],
			"progress":  progress,
			"series":    nil,
			"next_item": nil,
		}, nil
	}
	return nil, nil
}

func (s *Server) continueTargetSeriesMemberships(currentCandidateID, progressCandidateID string) ([]map[string]any, error) {
	if currentCandidateID == "" && progressCandidateID == "" {
		return []map[string]any{}, nil
	}
	return s.query(`
		SELECT DISTINCT
			si.group_id,
			COALESCE(NULLIF(sg.series_title, ''), si.series_title, '') AS series_title,
			CASE WHEN si.candidate_id = ? THEN 0 ELSE 1 END AS membership_rank
		FROM series_items si
		LEFT JOIN series_groups sg ON sg.group_id = si.group_id
		WHERE si.group_id <> ''
		  AND (si.candidate_id = ? OR si.candidate_id = ?)
		ORDER BY membership_rank, si.group_id
	`, currentCandidateID, currentCandidateID, progressCandidateID)
}

func (s *Server) continueTargetForSeries(groupID, fallbackTitle string) (map[string]any, error) {
	rows, err := s.query(`
		SELECT
			si.candidate_id,
			COALESCE(wb.work_identity_id, rp.work_identity_id, '') AS work_identity_id,
			wb.title,
			wb.relative_path,
			COALESCE(NULLIF(sg.series_title, ''), NULLIF(si.series_title, ''), ?) AS series_title,
			si.item_role,
			si.sequence_number,
			si.sort_key,
			COALESCE(wum.read_status, '') AS user_read_status,
			COALESCE(sumark.read_status, '') AS series_read_status,
			CASE
				WHEN COALESCE(wum.hidden, 0) = 0
				 AND COALESCE(sumark.hidden, 0) = 0
				THEN 1
				ELSE 0
			END AS eligible,
			`+workListProgressSelectSQL()+`
		FROM series_items si
		JOIN work_browse wb ON wb.candidate_id = si.candidate_id
		LEFT JOIN series_groups sg ON sg.group_id = si.group_id
		LEFT JOIN reading_progress rp
			ON rp.reader_profile_key = 'default'
		   AND (
				rp.work_identity_id = wb.work_identity_id
				OR rp.candidate_id = si.candidate_id
		   )
		LEFT JOIN work_user_marks wum
			ON wum.reader_profile_key = 'default'
		   AND wum.work_identity_id = COALESCE(wb.work_identity_id, rp.work_identity_id)
		LEFT JOIN series_identities sid ON sid.current_group_id = si.group_id
		LEFT JOIN series_user_marks sumark
			ON sumark.reader_profile_key = 'default'
		   AND sumark.series_identity_id = sid.series_identity_id
		WHERE si.group_id = ?
		ORDER BY si.sort_key, wb.title, wb.candidate_id
	`, fallbackTitle, groupID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	for _, row := range rows {
		seriesState := strings.ToLower(strings.TrimSpace(stringValue(row["series_read_status"])))
		if seriesState == "abandoned" || seriesState == "completed" {
			return nil, nil
		}
	}

	series := map[string]any{
		"group_id":     groupID,
		"series_title": strings.TrimSpace(fallbackTitle),
		"group_path":   "",
		"series_kind":  "",
		"series_unit":  "",
	}
	seriesRows, err := s.query(`
		SELECT
			sg.group_id,
			sg.series_title,
			sg.group_path,
			sg.group_type,
			COALESCE((
				SELECT correction_value
				FROM local_corrections
				WHERE target_type = 'series'
				  AND target_id = sg.group_id
				  AND correction_type = 'series_kind'
				ORDER BY updated_at DESC, id DESC
				LIMIT 1
			), '') AS series_kind,
			COALESCE((
				SELECT correction_value
				FROM local_corrections
				WHERE target_type = 'series'
				  AND target_id = sg.group_id
				  AND correction_type = 'series_unit'
				ORDER BY updated_at DESC, id DESC
				LIMIT 1
			), '') AS series_unit
		FROM series_groups sg
		WHERE sg.group_id = ?
		LIMIT 1
	`, groupID)
	if err != nil {
		return nil, err
	}
	if len(seriesRows) > 0 {
		series = seriesRows[0]
	}
	if strings.TrimSpace(stringValue(series["series_title"])) == "" {
		series["series_title"] = strings.TrimSpace(fallbackTitle)
	}

	candidateIDs := make([]string, 0, len(rows))
	orderedRows := make([]map[string]any, 0, len(rows))
	seenCandidates := map[string]bool{}
	for _, row := range rows {
		candidateID := stringValue(row["candidate_id"])
		if candidateID == "" || seenCandidates[candidateID] {
			continue
		}
		seenCandidates[candidateID] = true
		attachWorkListProgress(row)
		candidateIDs = append(candidateIDs, candidateID)
		orderedRows = append(orderedRows, row)
	}
	details, err := s.loadWorkListDetails(candidateIDs)
	if err != nil {
		return nil, err
	}
	detailByID := make(map[string]map[string]any, len(details))
	for _, item := range details {
		detailByID[stringValue(item["candidate_id"])] = item
	}

	entriesByID := make(map[string]continueTargetSeriesEntry, len(orderedRows))
	sectionItems := make([]map[string]any, 0, len(orderedRows))
	for _, row := range orderedRows {
		candidateID := stringValue(row["candidate_id"])
		item := detailByID[candidateID]
		if item == nil {
			continue
		}
		item["series_title"] = row["series_title"]
		item["item_role"] = row["item_role"]
		item["sequence_number"] = row["sequence_number"]
		item["sort_key"] = row["sort_key"]
		item["user_read_status"] = row["user_read_status"]
		if stringValue(item["relative_path"]) == "" {
			item["relative_path"] = row["relative_path"]
		}
		if stringValue(item["work_identity_id"]) == "" {
			item["work_identity_id"] = row["work_identity_id"]
		}
		if progress := continueTargetProgress(row); progress != nil {
			item["progress"] = progress
		}
		enrichWork(item)
		entry := continueTargetSeriesEntry{
			item:     item,
			eligible: boolValue(row["eligible"]),
			state:    continueTargetEffectiveReadState(item),
		}
		entriesByID[candidateID] = entry
		sectionItems = append(sectionItems, item)
	}
	if len(entriesByID) == 0 {
		return nil, nil
	}

	sections, sectioned, _ := buildSeriesSections(sectionItems, series)
	groups := make([]map[string]any, 0, len(sectionItems))
	for _, section := range sections {
		groups = append(groups, sectionGroups(section)...)
	}
	if !sectioned {
		sort.SliceStable(groups, func(i, j int) bool {
			leftSequence := numericFloat(groups[i]["sequence"], 1_000_000_000.0)
			rightSequence := numericFloat(groups[j]["sequence"], 1_000_000_000.0)
			if leftSequence != rightSequence {
				return leftSequence < rightSequence
			}
			return intValue(groups[i]["sort"]) < intValue(groups[j]["sort"])
		})
	}
	orderedGroups := make([]continueTargetSeriesGroup, 0, len(groups))
	for _, group := range groups {
		entryGroup := continueTargetSeriesGroup{}
		if primary, ok := group["primary"].(map[string]any); ok {
			entryGroup.primaryID = stringValue(primary["candidate_id"])
		}
		for _, item := range groupItems(group) {
			if entry, ok := entriesByID[stringValue(item["candidate_id"])]; ok {
				entryGroup.entries = append(entryGroup.entries, entry)
			}
		}
		if len(entryGroup.entries) > 0 {
			orderedGroups = append(orderedGroups, entryGroup)
		}
	}
	if len(orderedGroups) == 0 {
		return nil, nil
	}

	cursorGroupIndex := -1
	cursorEntryIndex := -1
	var cursorProgress map[string]any
	for groupIndex, group := range orderedGroups {
		if progressIndex := continueTargetLatestProgressEntry(group.entries); progressIndex >= 0 {
			progress := continueTargetProgress(group.entries[progressIndex].item)
			comparison := compareSeriesResumeProgress(progress, cursorProgress)
			if comparison > 0 || (comparison == 0 && groupIndex > cursorGroupIndex) {
				cursorProgress = progress
				cursorGroupIndex = groupIndex
				cursorEntryIndex = progressIndex
			}
		}
	}
	if cursorGroupIndex < 0 || cursorEntryIndex < 0 {
		return nil, nil
	}

	targetGroupIndex := cursorGroupIndex
	targetEntryIndex := cursorEntryIndex
	cursorEntry := orderedGroups[cursorGroupIndex].entries[cursorEntryIndex]
	if !continueTargetEntryTargetable(cursorEntry) || cursorEntry.state != "reading" {
		targetGroupIndex, targetEntryIndex = continueTargetNextGroupTarget(orderedGroups, cursorGroupIndex+1)
	}
	if targetGroupIndex < 0 || targetEntryIndex < 0 {
		return nil, nil
	}
	targetEntry := orderedGroups[targetGroupIndex].entries[targetEntryIndex]
	nextGroupIndex, nextEntryIndex := continueTargetNextGroupTarget(orderedGroups, targetGroupIndex+1)

	var nextItem any
	if nextGroupIndex >= 0 && nextEntryIndex >= 0 {
		nextItem = orderedGroups[nextGroupIndex].entries[nextEntryIndex].item
	}
	seriesTitle := strings.TrimSpace(stringValue(targetEntry.item["series_title"]))
	if seriesTitle == "" {
		seriesTitle = strings.TrimSpace(stringValue(series["series_title"]))
	}
	return map[string]any{
		"item":     targetEntry.item,
		"progress": continueTargetProgress(targetEntry.item),
		"series": map[string]any{
			"group_id":     groupID,
			"series_title": seriesTitle,
		},
		"next_item": nextItem,
	}, nil
}

func continueTargetProgress(item map[string]any) map[string]any {
	if item == nil {
		return nil
	}
	progress, _ := item["progress"].(map[string]any)
	return progress
}

func continueTargetEffectiveReadState(item map[string]any) string {
	status := strings.ToLower(strings.TrimSpace(coalesceString(item["user_read_status"], item["read_status"])))
	if status == "abandoned" {
		return "abandoned"
	}
	if status == "completed" {
		return "completed"
	}
	progress := continueTargetProgress(item)
	if progress != nil && boolValue(progress["completed"]) {
		return "completed"
	}
	if progress != nil || status == "reading" {
		return "reading"
	}
	return "unread"
}

func continueTargetEntryTargetable(entry continueTargetSeriesEntry) bool {
	return entry.eligible && boolValue(entry.item["can_read"]) && entry.state != "completed" && entry.state != "abandoned"
}

func continueTargetLatestProgressEntry(entries []continueTargetSeriesEntry) int {
	bestIndex := -1
	var bestProgress map[string]any
	for index, entry := range entries {
		progress := continueTargetProgress(entry.item)
		if progress == nil {
			continue
		}
		comparison := compareSeriesResumeProgress(progress, bestProgress)
		if comparison > 0 || (comparison == 0 && index > bestIndex) {
			bestIndex = index
			bestProgress = progress
		}
	}
	return bestIndex
}

func continueTargetGroupTarget(group continueTargetSeriesGroup) int {
	for index, entry := range group.entries {
		if stringValue(entry.item["candidate_id"]) == group.primaryID && continueTargetEntryTargetable(entry) {
			return index
		}
	}
	for index, entry := range group.entries {
		if continueTargetEntryTargetable(entry) {
			return index
		}
	}
	return -1
}

func continueTargetNextGroupTarget(groups []continueTargetSeriesGroup, start int) (int, int) {
	for groupIndex := start; groupIndex < len(groups); groupIndex++ {
		if entryIndex := continueTargetGroupTarget(groups[groupIndex]); entryIndex >= 0 {
			return groupIndex, entryIndex
		}
	}
	return -1, -1
}
