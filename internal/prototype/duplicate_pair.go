package prototype

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"math"
	"math/bits"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

const duplicatePairEvidenceMode = "dry-run evidence only"
const duplicatePairCandidateSource = "duplicate_pair_evidence"
const duplicatePairCandidateReason = "manual_pair_judgment"

func duplicatePairCandidateBookIDs(leftID string, rightID string) (string, string) {
	ids := []string{strings.TrimSpace(leftID), strings.TrimSpace(rightID)}
	sort.Strings(ids)
	return ids[0], ids[1]
}

func duplicatePairCandidateID(leftID string, rightID string) string {
	leftBookID, rightBookID := duplicatePairCandidateBookIDs(leftID, rightID)
	sum := sha1.Sum([]byte(leftBookID + "\x00" + rightBookID))
	return "duplicate-pair:" + fmt.Sprintf("%x", sum)
}

func (s *Server) duplicatePairCandidateInfo(leftID string, rightID string) (map[string]any, error) {
	leftBookID, rightBookID := duplicatePairCandidateBookIDs(leftID, rightID)
	candidateID := duplicatePairCandidateID(leftBookID, rightBookID)
	info := map[string]any{
		"candidate_id":        candidateID,
		"left_book_id":        leftBookID,
		"right_book_id":       rightBookID,
		"status":              "pending",
		"source":              duplicatePairCandidateSource,
		"reason":              duplicatePairCandidateReason,
		"tracked":             false,
		"local_only":          true,
		"local_preference":    duplicatePairPreferenceInfo(""),
		"applies_actions":     false,
		"source_path_written": false,
	}
	if !s.localTableExists("duplicate_candidates") {
		return info, nil
	}
	rows, err := s.query(`
		SELECT candidate_id, left_book_id, right_book_id, score, reason, evidence_json, source, status, created_at, updated_at
		FROM duplicate_candidates
		WHERE candidate_id = ?
		LIMIT 1
	`, candidateID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return info, nil
	}
	row := rows[0]
	info["candidate_id"] = stringValue(row["candidate_id"])
	info["left_book_id"] = stringValue(row["left_book_id"])
	info["right_book_id"] = stringValue(row["right_book_id"])
	info["score"] = floatValue(row["score"])
	info["reason"] = stringValue(row["reason"])
	info["source"] = stringValue(row["source"])
	info["status"] = stringValue(row["status"])
	info["created_at"] = stringValue(row["created_at"])
	info["updated_at"] = stringValue(row["updated_at"])
	info["tracked"] = true
	var evidence map[string]any
	if raw := strings.TrimSpace(stringValue(row["evidence_json"])); raw != "" {
		_ = json.Unmarshal([]byte(raw), &evidence)
	}
	if evidence != nil {
		preference, _ := evidence["local_preference"].(map[string]any)
		info["local_preference"] = duplicatePairPreferenceInfo(stringValue(preference["key"]))
	}
	return info, nil
}

func duplicatePairEvidenceScore(payload map[string]any) float64 {
	score := 0.0
	if cover, _ := payload["cover_hash_evidence"].(map[string]any); cover != nil {
		if distance := intValue(cover["primary_distance"]); cover["primary_distance"] != nil && distance >= 0 && distance <= 64 {
			coverScore := 1.0 - float64(distance)/64.0
			if coverScore > score {
				score = coverScore
			}
		}
	}
	return round5(score)
}

func duplicatePairStatusLabel(status string) string {
	switch strings.TrimSpace(status) {
	case "different":
		return "不是重复"
	case "version":
		return "不同版本"
	case "ignored":
		return "已忽略"
	default:
		return "待复核"
	}
}

func duplicateCandidateLocalStatusAllowed(status string) bool {
	switch strings.TrimSpace(status) {
	case "pending", "different", "version", "ignored":
		return true
	default:
		return false
	}
}

func duplicatePairPreferenceAllowed(preference string) bool {
	switch strings.TrimSpace(preference) {
	case "", "none", "prefer_left", "prefer_right", "keep_both":
		return true
	default:
		return false
	}
}

func duplicatePairPreferenceKey(preference string) string {
	preference = strings.TrimSpace(preference)
	if preference == "none" {
		return ""
	}
	if !duplicatePairPreferenceAllowed(preference) {
		return ""
	}
	return preference
}

func duplicatePairPreferenceLabel(preference string) string {
	switch duplicatePairPreferenceKey(preference) {
	case "prefer_left":
		return "优先 A"
	case "prefer_right":
		return "优先 B"
	case "keep_both":
		return "两者都保留"
	default:
		return "未选择"
	}
}

func duplicatePairPreferenceInfo(preference string) map[string]any {
	key := duplicatePairPreferenceKey(preference)
	return map[string]any{
		"key":                   key,
		"label":                 duplicatePairPreferenceLabel(key),
		"local_only":            true,
		"applies_actions":       false,
		"source_library_action": false,
	}
}

func duplicatePairCandidateEvidence(payload map[string]any, status string) (string, error) {
	candidate, _ := payload["duplicate_candidate"].(map[string]any)
	preference := duplicatePairPreferenceInfo("")
	if candidate != nil {
		localPreference, _ := candidate["local_preference"].(map[string]any)
		preference = duplicatePairPreferenceInfo(stringValue(localPreference["key"]))
	}
	return jsonString(map[string]any{
		"mode":                     payload["mode"],
		"dry_run":                  true,
		"applies_actions":          false,
		"source_library_written":   false,
		"source_library_scope":     payload["source_library_scope"],
		"human_confirmation":       payload["human_confirmation"],
		"local_duplicate_status":   status,
		"left":                     payload["left"],
		"right":                    payload["right"],
		"file_evidence":            payload["file_evidence"],
		"cover_hash_evidence":      payload["cover_hash_evidence"],
		"weak_assessment":          payload["weak_assessment"],
		"duplicate_candidate":      payload["duplicate_candidate"],
		"local_preference":         preference,
		"source_archive_rewritten": false,
	})
}

func duplicatePairFileFacts(work map[string]any) map[string]any {
	return map[string]any{
		"candidate_id":        stringValue(work["candidate_id"]),
		"library":             stringValue(coalesceAny(work["display_library_name"], work["library_name"], work["library_key"])),
		"title":               stringValue(coalesceAny(work["display_title"], work["title"])),
		"source_kind":         stringValue(work["source_kind"]),
		"extension":           stringValue(work["extension"]),
		"page_count_status":   stringValue(work["page_count_status"]),
		"readable_pages":      intValue(work["readable_page_count"]),
		"size_bytes":          int64Value(work["size_bytes"]),
		"size_display":        byteDisplay(int64Value(work["size_bytes"])),
		"modified_utc":        stringValue(work["modified_utc"]),
		"relative_path":       stringValue(work["relative_path"]),
		"cover_status":        stringValue(work["cover_status"]),
		"cover_kind":          stringValue(work["cover_kind"]),
		"series_title":        stringValue(work["series_title"]),
		"item_role":           stringValue(work["item_role"]),
		"sequence_number":     stringValue(work["sequence_number"]),
		"translation_sources": stringValue(work["translation_sources"]),
	}
}

func duplicatePairFileEvidence(left map[string]any, right map[string]any) map[string]any {
	leftPages := intValue(left["readable_page_count"])
	rightPages := intValue(right["readable_page_count"])
	leftSize := int64Value(left["size_bytes"])
	rightSize := int64Value(right["size_bytes"])
	return map[string]any{
		"left":                duplicatePairFileFacts(left),
		"right":               duplicatePairFileFacts(right),
		"same_library":        stringValue(left["library_key"]) != "" && stringValue(left["library_key"]) == stringValue(right["library_key"]),
		"same_extension":      stringValue(left["extension"]) != "" && strings.EqualFold(stringValue(left["extension"]), stringValue(right["extension"])),
		"page_count_delta":    absInt(leftPages - rightPages),
		"size_delta_bytes":    absInt64(leftSize - rightSize),
		"size_delta_display":  byteDisplay(absInt64(leftSize - rightSize)),
		"same_page_count":     leftPages > 0 && leftPages == rightPages,
		"same_title_literal":  strings.EqualFold(strings.TrimSpace(displayTitle(left)), strings.TrimSpace(displayTitle(right))) && strings.TrimSpace(displayTitle(left)) != "",
		"source_path_written": false,
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func round5(value float64) float64 {
	return math.Round(value*100000) / 100000
}

func jsonString(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(typed, 10, 64)
		return parsed
	default:
		return 0
	}
}

func byteDisplay(value int64) string {
	if value <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	size := float64(value)
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", value, units[unit])
	}
	return fmt.Sprintf("%.1f %s", size, units[unit])
}

func displayTitle(item map[string]any) string {
	return stringValue(coalesceAny(item["display_title"], item["title"]))
}

func hashDistance(left any, right any) int {
	leftHash, leftErr := strconv.ParseUint(stringValue(left), 16, 64)
	rightHash, rightErr := strconv.ParseUint(stringValue(right), 16, 64)
	if leftErr != nil || rightErr != nil {
		return 65
	}
	return bits.OnesCount64(leftHash ^ rightHash)
}

func (s *Server) coverHashHasPerceptualHash() bool {
	rows, err := s.query(`PRAGMA table_info(cover_image_hashes)`)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if stringValue(row["name"]) == "perceptual_hash" {
			return true
		}
	}
	return false
}

func coverHashEvidenceColumns(hasPerceptualHash bool) string {
	perceptual := "'' AS perceptual_hash"
	if hasPerceptualHash {
		perceptual = "cih.perceptual_hash"
	}
	return "cih.average_hash, cih.difference_hash, " + perceptual + ", cih.width, cih.height"
}

func (s *Server) duplicatePairCoverHashEvidence(leftID string, rightID string) (map[string]any, error) {
	evidence := map[string]any{
		"indexed": false,
		"source":  "cover_image_hashes",
	}
	if !s.localTableExists("cover_image_hashes") {
		return evidence, nil
	}

	hasPerceptualHash := s.coverHashHasPerceptualHash()
	rows, err := s.query(`
		SELECT
			stable_key, candidate_id, work_identity_id, cache_path, hash_version,
			`+coverHashEvidenceColumns(hasPerceptualHash)+`,
			cache_size_bytes, cache_mtime_ns, computed_at, updated_at
		FROM cover_image_hashes cih
		WHERE candidate_id IN (?, ?)
		ORDER BY candidate_id, stable_key
	`, leftID, rightID)
	if err != nil {
		return nil, err
	}

	byID := map[string]map[string]any{}
	for _, row := range rows {
		candidateID := stringValue(row["candidate_id"])
		if _, exists := byID[candidateID]; exists {
			continue
		}
		byID[candidateID] = row
	}
	left := byID[leftID]
	right := byID[rightID]
	evidence["indexed"] = true
	evidence["left_indexed"] = left != nil
	evidence["right_indexed"] = right != nil
	evidence["left"] = left
	evidence["right"] = right
	if left == nil || right == nil {
		return evidence, nil
	}

	differenceDistance := hashDistance(left["difference_hash"], right["difference_hash"])
	averageDistance := hashDistance(left["average_hash"], right["average_hash"])
	perceptualDistance := 65
	if stringValue(left["perceptual_hash"]) != "" && stringValue(right["perceptual_hash"]) != "" {
		perceptualDistance = hashDistance(left["perceptual_hash"], right["perceptual_hash"])
	}
	primaryDistance := differenceDistance
	if perceptualDistance < primaryDistance {
		primaryDistance = perceptualDistance
	}
	evidence["difference_distance"] = differenceDistance
	evidence["average_distance"] = averageDistance
	evidence["perceptual_distance"] = perceptualDistance
	evidence["primary_distance"] = primaryDistance
	evidence["same_difference_hash"] = differenceDistance == 0
	evidence["same_perceptual_hash"] = perceptualDistance == 0
	return evidence, nil
}

func duplicatePairWeakAssessment(fileEvidence map[string]any, coverEvidence map[string]any) map[string]any {
	coverPrimaryDistance := intValue(coverEvidence["primary_distance"])
	if coverPrimaryDistance == 0 && coverEvidence["primary_distance"] == nil {
		coverPrimaryDistance = 65
	}
	samePages := fileEvidence["same_page_count"] == true

	level := "manual_review"
	note := "仅提供本地文件信息与封面哈希，结论必须由人工确认。"
	if coverPrimaryDistance == 0 && samePages {
		level = "exact_cover_match"
		note = "封面哈希与页数一致，但仍需人工核对正文和版本差异。"
	} else if coverPrimaryDistance <= 6 || samePages {
		level = "possible_match"
		note = "存在有限相似证据，建议人工对照内容。"
	}

	return map[string]any{
		"weak_label":          level,
		"human_review_note":   note,
		"no_automatic_action": true,
		"no_delete_decision":  true,
		"requires_human":      true,
	}
}

func (s *Server) duplicatePairDetailRows(leftID string, rightID string) (map[string]map[string]any, error) {
	rows, err := s.query(`
		SELECT
			wb.*,
			si.series_title,
			si.item_role,
			si.sequence_number
		FROM work_browse wb
		`+seriesJoinSQL()+`
		WHERE wb.candidate_id IN (?, ?)
	`, leftID, rightID)
	if err != nil {
		return nil, err
	}
	if err := s.applyMetadataOverrides(rows); err != nil {
		return nil, err
	}
	result := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		enrichWork(row)
		result[stringValue(row["candidate_id"])] = row
	}
	return result, nil
}

func (s *Server) duplicatePairEvidencePayload(leftID string, rightID string, _ int, _ int) (map[string]any, int, error) {
	leftID = strings.TrimSpace(leftID)
	rightID = strings.TrimSpace(rightID)
	if leftID == "" || rightID == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("missing left or right id")
	}
	if leftID == rightID {
		return nil, http.StatusBadRequest, fmt.Errorf("left and right must be different works")
	}

	details, err := s.duplicatePairDetailRows(leftID, rightID)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	left := details[leftID]
	right := details[rightID]
	if left == nil || right == nil {
		return nil, http.StatusNotFound, fmt.Errorf("work not found")
	}

	fileEvidence := duplicatePairFileEvidence(left, right)
	coverEvidence, err := s.duplicatePairCoverHashEvidence(leftID, rightID)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	duplicateCandidate, err := s.duplicatePairCandidateInfo(leftID, rightID)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	return map[string]any{
		"ok":                     true,
		"mode":                   duplicatePairEvidenceMode,
		"dry_run":                true,
		"applies_actions":        false,
		"source_library_written": false,
		"created_at":             nowISO(),
		"query": map[string]any{
			"left":  leftID,
			"right": rightID,
		},
		"left":                 left,
		"right":                right,
		"file_evidence":        fileEvidence,
		"cover_hash_evidence":  coverEvidence,
		"weak_assessment":      duplicatePairWeakAssessment(fileEvidence, coverEvidence),
		"duplicate_candidate":  duplicateCandidate,
		"human_confirmation":   "required before changing the local duplicate status",
		"source_library_scope": "read-only",
	}, http.StatusOK, nil
}

func (s *Server) handleDuplicatePairEvidence(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	query := r.URL.Query()
	payload, status, err := s.duplicatePairEvidencePayload(query.Get("left"), query.Get("right"), 0, 0)
	if err != nil {
		writeJSONError(w, status, err.Error())
		return
	}
	writeJSON(w, payload)
}

func (s *Server) handleDuplicatePairStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	payload, err := readJSONBody(r, 64*1024)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	leftID := strings.TrimSpace(stringValue(payload["left"]))
	rightID := strings.TrimSpace(stringValue(payload["right"]))
	status := strings.TrimSpace(stringValue(payload["status"]))
	if !duplicateCandidateLocalStatusAllowed(status) {
		writeJSONError(w, http.StatusBadRequest, "invalid local duplicate pair status")
		return
	}
	preferenceRaw, preferenceProvided := payload["preference"]
	preference := ""
	if preferenceProvided {
		preference = strings.TrimSpace(stringValue(preferenceRaw))
		if !duplicatePairPreferenceAllowed(preference) {
			writeJSONError(w, http.StatusBadRequest, "invalid local duplicate pair preference")
			return
		}
		preference = duplicatePairPreferenceKey(preference)
	}

	evidence, statusCode, err := s.duplicatePairEvidencePayload(leftID, rightID, 12, 12)
	if err != nil {
		writeJSONError(w, statusCode, err.Error())
		return
	}
	existingCandidate, _ := evidence["duplicate_candidate"].(map[string]any)
	if !preferenceProvided && status == "version" {
		existingPreference, _ := existingCandidate["local_preference"].(map[string]any)
		preference = duplicatePairPreferenceKey(stringValue(existingPreference["key"]))
	}
	if preference != "" {
		status = "version"
	}
	leftBookID, rightBookID := duplicatePairCandidateBookIDs(leftID, rightID)
	candidateID := duplicatePairCandidateID(leftBookID, rightBookID)
	now := nowISO()
	candidateInfo := map[string]any{
		"candidate_id":        candidateID,
		"left_book_id":        leftBookID,
		"right_book_id":       rightBookID,
		"status":              status,
		"source":              duplicatePairCandidateSource,
		"reason":              duplicatePairCandidateReason,
		"tracked":             true,
		"local_only":          true,
		"local_preference":    duplicatePairPreferenceInfo(preference),
		"applies_actions":     false,
		"source_path_written": false,
		"updated_at":          now,
	}
	evidence["duplicate_candidate"] = candidateInfo
	evidenceJSON, err := duplicatePairCandidateEvidence(evidence, status)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	score := duplicatePairEvidenceScore(evidence)
	if _, err := s.db.Exec(`
		INSERT INTO duplicate_candidates (
			candidate_id, left_book_id, right_book_id, score, reason, evidence_json,
			source, status, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(candidate_id) DO UPDATE SET
			left_book_id = excluded.left_book_id,
			right_book_id = excluded.right_book_id,
			score = excluded.score,
			reason = excluded.reason,
			evidence_json = excluded.evidence_json,
			source = excluded.source,
			status = excluded.status,
			updated_at = excluded.updated_at
	`, candidateID, leftBookID, rightBookID, score, duplicatePairCandidateReason, evidenceJSON, duplicatePairCandidateSource, status, now, now); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	candidateInfo["score"] = score
	evidence["duplicate_candidate"] = candidateInfo
	evidence["local_status_written"] = true
	evidence["applies_actions"] = false
	evidence["source_library_written"] = false
	writeJSON(w, evidence)
}
