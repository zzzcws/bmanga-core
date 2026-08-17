package prototype

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

func (s *Server) handleCorrections(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleCorrectionsGet(w, r)
	case http.MethodPost:
		s.handleCorrectionSave(w, r)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCorrectionsGet(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	targetType := strings.TrimSpace(query.Get("target_type"))
	targetID := strings.TrimSpace(query.Get("target_id"))
	filters := []string{}
	args := []any{}
	filters = append(filters, "(correction_type <> 'review_status' OR correction_value IN ('open', 'ok', 'needs_fix'))")
	if targetType != "" {
		filters = append(filters, "target_type = ?")
		args = append(args, targetType)
	}
	if targetID != "" {
		filters = append(filters, "target_id = ?")
		args = append(args, targetID)
	}
	rows, err := s.query("SELECT * FROM local_corrections"+whereClause(filters)+" ORDER BY updated_at DESC", args...)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"items": rows})
}

func (s *Server) handleCorrectionSave(w http.ResponseWriter, r *http.Request) {
	payload, err := readJSONBody(r, 64*1024)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	targetType := strings.TrimSpace(stringValue(payload["target_type"]))
	targetID := strings.TrimSpace(stringValue(payload["target_id"]))
	correctionType := strings.TrimSpace(stringValue(payload["correction_type"]))
	correctionValue := strings.TrimSpace(stringValue(payload["correction_value"]))
	note := strings.TrimSpace(stringValue(payload["note"]))
	if !validCorrectionTarget(targetType) || targetID == "" || !validCorrectionType(correctionType) {
		writeJSONError(w, http.StatusBadRequest, "invalid correction")
		return
	}
	if !validCorrectionValue(correctionType, correctionValue) {
		writeJSONError(w, http.StatusBadRequest, "invalid correction value")
		return
	}
	exists, err := s.correctionTargetExists(targetType, targetID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !exists {
		writeJSONError(w, http.StatusNotFound, "target not found")
		return
	}
	if correctionType == "cover_candidate_id" {
		if targetType != "series" || correctionValue == "" {
			writeJSONError(w, http.StatusBadRequest, "series cover candidate required")
			return
		}
		cover, err := s.query(`
			SELECT 1
			FROM series_items si
			JOIN work_cover_candidates wcc ON wcc.candidate_id = si.candidate_id
			WHERE si.group_id = ?
			  AND si.candidate_id = ?
			  AND wcc.cover_status = 'ready'
			  AND wcc.cover_kind IN ('page_image', 'archive', 'pdf', 'ebook')
		`, targetID, correctionValue)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(cover) == 0 {
			writeJSONError(w, http.StatusBadRequest, "cover candidate not usable")
			return
		}
	}
	if correctionType == "series_unit" && (targetType != "series" || normalizeSeriesUnit(correctionValue) == "") {
		writeJSONError(w, http.StatusBadRequest, "series unit must be chapter or volume")
		return
	}
	now := nowISO()
	if _, err := s.db.Exec(`
		INSERT INTO local_corrections (
			target_type, target_id, correction_type, correction_value, note, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(target_type, target_id, correction_type) DO UPDATE SET
			correction_value = excluded.correction_value,
			note = excluded.note,
			updated_at = excluded.updated_at
	`, targetType, targetID, correctionType, correctionValue, note, now, now); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.checkpointSQLiteWALBestEffort("correction_save")
	s.clearReviewItemsCache()
	s.clearCoverDuplicateResponseCache()
	rows, err := s.query(`
		SELECT *
		FROM local_corrections
		WHERE target_type = ? AND target_id = ? AND correction_type = ?
	`, targetType, targetID, correctionType)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "correction": firstRow(rows)})
}

func (s *Server) handleCorrectionsBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	payload, err := readJSONBody(r, 512*1024)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	rawItems, ok := payload["items"].([]any)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "items list required")
		return
	}
	if len(rawItems) == 0 {
		writeJSON(w, map[string]any{"ok": true, "applied": 0, "items": []map[string]any{}})
		return
	}
	if len(rawItems) > 500 {
		writeJSONError(w, http.StatusBadRequest, "too many correction items")
		return
	}
	items := make([]map[string]any, 0, len(rawItems))
	for index, raw := range rawItems {
		item, ok := raw.(map[string]any)
		if !ok {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("invalid correction item at %d", index))
			return
		}
		normalized, err := s.validateCorrectionPayload(item)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("item %d: %s", index, err.Error()))
			return
		}
		items = append(items, normalized)
	}
	now := nowISO()
	tx, err := s.db.Begin()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmt, err := tx.Prepare(`
		INSERT INTO local_corrections (
			target_type, target_id, correction_type, correction_value, note, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(target_type, target_id, correction_type) DO UPDATE SET
			correction_value = excluded.correction_value,
			note = excluded.note,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() {
		if stmt != nil {
			_ = stmt.Close()
		}
	}()
	for _, item := range items {
		if _, err := stmt.Exec(item["target_type"], item["target_id"], item["correction_type"], item["correction_value"], item["note"], now, now); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := stmt.Close(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	stmt = nil
	if err := tx.Commit(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	committed = true
	s.checkpointSQLiteWALBestEffort("corrections_batch")
	s.clearReviewItemsCache()
	s.clearCoverDuplicateResponseCache()
	writeJSON(w, map[string]any{"ok": true, "applied": len(items), "items": items})
}

func (s *Server) validateCorrectionPayload(payload map[string]any) (map[string]any, error) {
	targetType := strings.TrimSpace(stringValue(payload["target_type"]))
	targetID := strings.TrimSpace(stringValue(payload["target_id"]))
	correctionType := strings.TrimSpace(stringValue(payload["correction_type"]))
	correctionValue := strings.TrimSpace(stringValue(payload["correction_value"]))
	note := strings.TrimSpace(stringValue(payload["note"]))
	if !validCorrectionTarget(targetType) || targetID == "" || !validCorrectionType(correctionType) {
		return nil, fmt.Errorf("invalid correction")
	}
	if !validCorrectionValue(correctionType, correctionValue) {
		return nil, fmt.Errorf("invalid correction value")
	}
	exists, err := s.correctionTargetExists(targetType, targetID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("target not found")
	}
	if correctionType == "cover_candidate_id" {
		if targetType != "series" || correctionValue == "" {
			return nil, fmt.Errorf("series cover candidate required")
		}
		cover, err := s.query(`
			SELECT 1
			FROM series_items si
			JOIN work_cover_candidates wcc ON wcc.candidate_id = si.candidate_id
			WHERE si.group_id = ?
			  AND si.candidate_id = ?
			  AND wcc.cover_status = 'ready'
			  AND wcc.cover_kind IN ('page_image', 'archive', 'pdf', 'ebook')
		`, targetID, correctionValue)
		if err != nil {
			return nil, err
		}
		if len(cover) == 0 {
			return nil, fmt.Errorf("cover candidate not usable")
		}
	}
	if correctionType == "series_unit" && (targetType != "series" || normalizeSeriesUnit(correctionValue) == "") {
		return nil, fmt.Errorf("series unit must be chapter or volume")
	}
	return map[string]any{
		"target_type":      targetType,
		"target_id":        targetID,
		"correction_type":  correctionType,
		"correction_value": correctionValue,
		"note":             note,
	}, nil
}

func (s *Server) checkpointSQLiteWAL() error {
	rows, err := s.db.Query("PRAGMA wal_checkpoint(TRUNCATE)")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var busy, logFrames, checkpointedFrames int
		if err := rows.Scan(&busy, &logFrames, &checkpointedFrames); err != nil {
			return err
		}
		if busy != 0 {
			return fmt.Errorf("sqlite wal checkpoint busy: busy=%d log_frames=%d checkpointed_frames=%d", busy, logFrames, checkpointedFrames)
		}
	}
	return rows.Err()
}

func (s *Server) checkpointSQLiteWALBestEffort(operation string) {
	if err := s.checkpointSQLiteWAL(); err != nil {
		fmt.Fprintf(os.Stderr, "bmanga sqlite wal checkpoint deferred operation=%s: %v\n", operation, err)
	}
}

func validCorrectionTarget(value string) bool {
	switch value {
	case "work", "series", "translation_group", "cover_hash":
		return true
	default:
		return false
	}
}

func validCorrectionType(value string) bool {
	switch value {
	case "review_status", "series_kind", "series_unit", "cover_candidate_id", "note":
		return true
	default:
		return false
	}
}

func validCorrectionValue(correctionType string, value string) bool {
	if correctionType != "review_status" {
		return true
	}
	switch value {
	case "open", "ok", "needs_fix":
		return true
	default:
		return false
	}
}

func (s *Server) correctionTargetExists(targetType string, targetID string) (bool, error) {
	var rows []map[string]any
	var err error
	switch targetType {
	case "work":
		rows, err = s.query("SELECT 1 FROM work_browse WHERE candidate_id = ?", targetID)
	case "series":
		rows, err = s.query("SELECT 1 FROM series_groups WHERE group_id = ?", targetID)
	case "translation_group":
		rows, err = s.query("SELECT 1 FROM translation_groups WHERE translation_group_id = ?", targetID)
	case "cover_hash":
		rows, err = s.query("SELECT 1 FROM cover_image_hashes WHERE difference_hash = ? LIMIT 1", targetID)
	default:
		return false, nil
	}
	return len(rows) > 0, err
}

type userMarkTarget struct {
	identityID     string
	storedTargetID string
}
