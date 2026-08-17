package prototype

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleProgressSave(w, r)
		return
	}
	s.handleProgressGet(w, r)
}

func (s *Server) handleProgressGet(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	candidateID := strings.TrimSpace(r.URL.Query().Get("id"))
	if candidateID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing id")
		return
	}
	workRows, err := s.query("SELECT * FROM work_browse WHERE candidate_id = ?", candidateID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(workRows) == 0 {
		writeJSONError(w, http.StatusNotFound, "work not found")
		return
	}
	work := workRows[0]
	manifest, err := s.currentManifestForWork(r.Context(), work)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	workIdentityID := stringValue(work["work_identity_id"])
	var progress map[string]any
	if workIdentityID != "" {
		progressRows, err := s.query(`
			SELECT *
			FROM reading_progress
			WHERE reader_profile_key = 'default'
			  AND work_identity_id = ?
		`, workIdentityID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(progressRows) > 0 {
			progress = formatProgress(progressRows[0], manifest, stringValue(work["title"]))
		}
	}
	count := intValue(work["readable_page_count"])
	pageManifestID := ""
	manifestHash := ""
	if manifest != nil {
		count = intValue(manifest["page_count"])
		pageManifestID = stringValue(manifest["page_manifest_id"])
		manifestHash = stringValue(manifest["manifest_hash"])
	}
	writeJSON(w, map[string]any{
		"candidate_id":     candidateID,
		"work_identity_id": workIdentityID,
		"page_manifest_id": pageManifestID,
		"manifest_hash":    manifestHash,
		"count":            count,
		"progress":         progress,
	})
}

func (s *Server) handleProgressSave(w http.ResponseWriter, r *http.Request) {
	payload, err := readJSONBody(r, 64*1024)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	candidateID := strings.TrimSpace(stringValue(payload["candidate_id"]))
	if candidateID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing candidate_id")
		return
	}
	workRows, err := s.query("SELECT * FROM work_browse WHERE candidate_id = ?", candidateID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(workRows) == 0 {
		writeJSONError(w, http.StatusNotFound, "work not found")
		return
	}
	work := workRows[0]
	workIdentityID := stringValue(work["work_identity_id"])
	if workIdentityID == "" {
		writeJSONError(w, http.StatusConflict, "work identity missing")
		return
	}
	manifest, err := s.currentManifestForWork(r.Context(), work)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	requestedManifestHash := strings.TrimSpace(stringValue(payload["manifest_hash"]))
	requestedPageManifestID := strings.TrimSpace(stringValue(payload["page_manifest_id"]))
	if manifest != nil {
		currentManifestHash := stringValue(manifest["manifest_hash"])
		currentPageManifestID := stringValue(manifest["page_manifest_id"])
		if (requestedManifestHash != "" && requestedManifestHash != currentManifestHash) || (requestedPageManifestID != "" && requestedPageManifestID != currentPageManifestID) {
			writeJSONStatus(w, http.StatusConflict, map[string]any{
				"error":           "页面清单已变化，请重新打开或校准进度",
				"progress_status": "manifest_stale",
			})
			return
		}
	}
	requestedIndex := clampInt(stringValue(payload["index"]), 0, 0, 1_000_000)
	requestedCount := clampInt(stringValue(payload["count"]), 0, 0, 1_000_000)
	pageCount := requestedCount
	if manifest != nil {
		pageCount = intValue(manifest["page_count"])
	}
	if pageCount <= 0 {
		writeJSONError(w, http.StatusConflict, "page manifest missing")
		return
	}
	lastPageIndex := requestedIndex
	if lastPageIndex > pageCount-1 {
		lastPageIndex = pageCount - 1
	}
	completed := boolValue(payload["completed"])
	progressPercent := float64(lastPageIndex+1) / float64(pageCount) * 100
	now := nowISO()
	serverNow := time.Now().UTC()
	clientUpdatedAt, clientUpdatedTime, clientUpdatedAtProvided, clientUpdatedAtFuture := progressPayloadUpdatedAt(payload, serverNow)
	manifestHash := requestedManifestHash
	pageManifestID := requestedPageManifestID
	if manifest != nil {
		manifestHash = stringValue(manifest["manifest_hash"])
		pageManifestID = stringValue(manifest["page_manifest_id"])
	}
	readerFitMode := strings.TrimSpace(stringValue(payload["reader_fit_mode"]))
	switch readerFitMode {
	case "fit-page", "fit-width", "split-wide":
	default:
		readerFitMode = ""
	}
	readerSplitPanel := clampInt(stringValue(payload["reader_split_panel"]), 0, 0, 1)
	if readerFitMode != "split-wide" {
		readerSplitPanel = 0
	}
	if lastPageIndex+1 >= pageCount && (readerFitMode != "split-wide" || readerSplitPanel >= 1) {
		completed = true
	}
	stageScrollTop := clampInt(stringValue(payload["stage_scroll_top"]), 0, 0, 1_000_000)
	stageScrollLeft := clampInt(stringValue(payload["stage_scroll_left"]), 0, 0, 1_000_000)
	if readerFitMode != "fit-width" {
		stageScrollTop = 0
		stageScrollLeft = 0
	}

	tx, err := s.db.Begin()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO reader_profiles (key, display_name, created_at, updated_at)
		VALUES ('default', 'Default', ?, ?)
	`, now, now); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var resetAt string
	resetExists := false
	if err := tx.QueryRow(`
		SELECT reset_at
		FROM reading_progress_resets
		WHERE reader_profile_key = 'default'
		  AND work_identity_id = ?
	`, workIdentityID).Scan(&resetAt); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		resetExists = true
	}
	resetTime, resetClockUntrusted := persistedClientClock(resetAt, serverNow)

	var existingProgressLastReadAt, existingProgressUpdatedAt string
	existingProgressClockUntrusted := false
	if err := tx.QueryRow(`
		SELECT COALESCE(last_read_at, ''), updated_at
		FROM reading_progress
		WHERE reader_profile_key = 'default'
		  AND work_identity_id = ?
	`, workIdentityID).Scan(&existingProgressLastReadAt, &existingProgressUpdatedAt); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		existingProgressTime, untrusted := persistedClientClock(existingProgressLastReadAt, serverNow)
		if existingProgressTime.IsZero() && !untrusted {
			_, untrusted = persistedClientClock(existingProgressUpdatedAt, serverNow)
		}
		existingProgressClockUntrusted = untrusted
	}

	var currentReadStatus, currentReadStatusClientUpdatedAt string
	if err := tx.QueryRow(`
		SELECT read_status, read_status_client_updated_at
		FROM work_user_marks
		WHERE reader_profile_key = 'default'
		  AND work_identity_id = ?
	`, workIdentityID).Scan(&currentReadStatus, &currentReadStatusClientUpdatedAt); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	readStatusTime, readStatusClockUntrusted := persistedClientClock(currentReadStatusClientUpdatedAt, serverNow)
	if readStatusClockUntrusted {
		if _, err := tx.Exec(`
			UPDATE work_user_marks
			SET read_status_client_updated_at = ''
			WHERE reader_profile_key = 'default'
			  AND work_identity_id = ?
			  AND read_status_client_updated_at = ?
		`, workIdentityID, currentReadStatusClientUpdatedAt); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		readStatusTime = time.Time{}
	}

	blockedByReset := false
	blockedByReadStatus := false
	rejectedReason := ""
	if clientUpdatedAtFuture {
		rejectedReason = "future_timestamp"
	}
	if resetExists {
		if resetClockUntrusted {
			blockedByReset = !clientUpdatedAtProvided
		} else {
			blockedByReset = !clientUpdatedAtProvided || !clientUpdatedTime.After(resetTime)
		}
		if blockedByReset && rejectedReason == "" {
			rejectedReason = "progress_reset_newer"
		}
	}
	if !readStatusTime.IsZero() {
		sameCompletedEvent := completed && currentReadStatus == "completed" && clientUpdatedAtProvided && clientUpdatedTime.Equal(readStatusTime)
		blockedByReadStatus = !clientUpdatedAtProvided || (!clientUpdatedTime.After(readStatusTime) && !sameCompletedEvent)
		if blockedByReadStatus && rejectedReason == "" {
			rejectedReason = "read_status_newer"
		}
	}

	stored := false
	if !blockedByReset && !blockedByReadStatus && rejectedReason == "" {
		writeResult, err := tx.Exec(`
			INSERT INTO reading_progress (
				reader_profile_key, work_identity_id, candidate_id, page_manifest_id,
				manifest_hash_snapshot, progress_status, last_page_index, progress_percent,
				completed, page_count_snapshot, reader_fit_mode, reader_split_panel,
				stage_scroll_top, stage_scroll_left, last_read_at, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, 'normal', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(reader_profile_key, work_identity_id) DO UPDATE SET
				candidate_id = excluded.candidate_id,
				page_manifest_id = excluded.page_manifest_id,
				manifest_hash_snapshot = excluded.manifest_hash_snapshot,
				progress_status = 'normal',
				last_page_index = excluded.last_page_index,
				progress_percent = excluded.progress_percent,
				completed = excluded.completed,
				page_count_snapshot = excluded.page_count_snapshot,
				reader_fit_mode = excluded.reader_fit_mode,
				reader_split_panel = excluded.reader_split_panel,
				stage_scroll_top = excluded.stage_scroll_top,
				stage_scroll_left = excluded.stage_scroll_left,
				last_read_at = excluded.last_read_at,
				updated_at = excluded.updated_at
			WHERE ? = 1
				AND (
					? = 1
					OR COALESCE(julianday(excluded.updated_at), 0)
						> COALESCE(julianday(reading_progress.last_read_at), julianday(reading_progress.updated_at), 0)
				)
		`, "default", workIdentityID, candidateID, pageManifestID, manifestHash, lastPageIndex, progressPercent, boolInt(completed), pageCount, readerFitMode, readerSplitPanel, stageScrollTop, stageScrollLeft, clientUpdatedAt, now, clientUpdatedAt, boolInt(clientUpdatedAtProvided), boolInt(existingProgressClockUntrusted))
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		affected, err := writeResult.RowsAffected()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		stored = affected > 0
		if !stored {
			if clientUpdatedAtProvided {
				rejectedReason = "stale_progress"
			} else {
				rejectedReason = "untimed_existing_progress"
			}
		}
		if stored && resetExists {
			if _, err := tx.Exec(`
				DELETE FROM reading_progress_resets
				WHERE reader_profile_key = 'default'
				  AND work_identity_id = ?
				  AND reset_at = ?
			`, workIdentityID, resetAt); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	if stored {
		progressReadStatus := "reading"
		if completed {
			progressReadStatus = "completed"
		}
		if err := markWorkReadStatusFromProgressTx(tx, workIdentityID, candidateID, progressReadStatus, now, formatOrderedClientTime(clientUpdatedTime)); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	progressRows, err := s.query(`
		SELECT *
		FROM reading_progress
		WHERE reader_profile_key = 'default'
		  AND work_identity_id = ?
	`, workIdentityID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var progress map[string]any
	if len(progressRows) > 0 {
		progress = formatProgress(progressRows[0], manifest, stringValue(work["title"]))
	}
	writeJSON(w, map[string]any{
		"ok":                     true,
		"stored":                 stored,
		"blocked_by_reset":       blockedByReset,
		"blocked_by_read_status": blockedByReadStatus,
		"timestamp_rejected":     clientUpdatedAtFuture,
		"rejected_reason":        rejectedReason,
		"discard_pending":        !stored && rejectedReason != "",
		"progress":               progress,
	})
}

func markWorkReadStatusFromProgressTx(tx *sql.Tx, workIdentityID, candidateID, readStatus, markedAt, clientUpdatedAt string) error {
	workIdentityID = strings.TrimSpace(workIdentityID)
	candidateID = strings.TrimSpace(candidateID)
	if workIdentityID == "" || candidateID == "" {
		return nil
	}
	if readStatus != "reading" && readStatus != "completed" {
		return fmt.Errorf("invalid progress read status")
	}
	if strings.TrimSpace(markedAt) == "" {
		markedAt = nowISO()
	}
	parsedClientUpdatedAt := browseStateTime(clientUpdatedAt)
	if parsedClientUpdatedAt.IsZero() {
		parsedClientUpdatedAt = time.Now().UTC()
	}
	clientUpdatedAt = formatOrderedClientTime(parsedClientUpdatedAt)
	serverNow := time.Now().UTC()
	var existingMarkUpdatedAt, existingHiddenReason, existingNotes string
	var existingPersonalRating, existingTranslationQuality, existingImageQuality sql.NullInt64
	var existingFavorite, existingRereadPriority, existingHidden int
	if err := tx.QueryRow(`
		SELECT
			personal_rating, favorite, reread_priority,
			translation_quality, image_quality, hidden, hidden_reason, notes, updated_at
		FROM work_user_marks
		WHERE reader_profile_key = 'default'
		  AND work_identity_id = ?
	`, workIdentityID).Scan(
		&existingPersonalRating,
		&existingFavorite,
		&existingRereadPriority,
		&existingTranslationQuality,
		&existingImageQuality,
		&existingHidden,
		&existingHiddenReason,
		&existingNotes,
		&existingMarkUpdatedAt,
	); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	} else {
		legacyProtectedFields := []string{}
		if existingPersonalRating.Valid {
			legacyProtectedFields = append(legacyProtectedFields, "personal_rating")
		}
		if existingFavorite != 0 {
			legacyProtectedFields = append(legacyProtectedFields, "favorite")
		}
		if existingRereadPriority != 0 {
			legacyProtectedFields = append(legacyProtectedFields, "reread_priority")
		}
		if existingTranslationQuality.Valid {
			legacyProtectedFields = append(legacyProtectedFields, "translation_quality")
		}
		if existingImageQuality.Valid {
			legacyProtectedFields = append(legacyProtectedFields, "image_quality")
		}
		if existingHidden != 0 {
			legacyProtectedFields = append(legacyProtectedFields, "hidden")
		}
		if existingHiddenReason != "" {
			legacyProtectedFields = append(legacyProtectedFields, "hidden_reason")
		}
		if existingNotes != "" {
			legacyProtectedFields = append(legacyProtectedFields, "notes")
		}
		if err := seedLegacyUserMarkFieldClocksTx(
			tx,
			"work",
			workIdentityID,
			existingMarkUpdatedAt,
			legacyProtectedFields,
			serverNow,
			markedAt,
		); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO work_user_marks (
			reader_profile_key, work_identity_id, candidate_id, read_status, read_status_client_updated_at,
			favorite, reread_priority, hidden, hidden_reason, notes,
			marked_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 0, 0, 0, '', '', ?, ?, ?)
	`, "default", workIdentityID, candidateID, readStatus, clientUpdatedAt, markedAt, markedAt, markedAt); err != nil {
		return err
	}
	_, err := tx.Exec(`
		UPDATE work_user_marks
		SET candidate_id = ?,
			read_status = ?,
			read_status_client_updated_at = ?,
			marked_at = ?,
			updated_at = ?
		WHERE reader_profile_key = 'default'
		  AND work_identity_id = ?
		  AND (
			COALESCE(read_status_client_updated_at, '') = ''
			OR ? > read_status_client_updated_at
		  )
	`, candidateID, readStatus, clientUpdatedAt, markedAt, markedAt, workIdentityID, clientUpdatedAt)
	return err
}

func progressPayloadUpdatedAt(payload map[string]any, serverNow time.Time) (string, time.Time, bool, bool) {
	updatedAt := strings.TrimSpace(stringValue(payload["updated_at"]))
	parsed := browseStateTime(updatedAt)
	if parsed.IsZero() {
		serverNow = serverNow.UTC()
		return serverNow.Format(time.RFC3339Nano), serverNow, false, false
	}
	if parsed.After(serverNow.UTC().Add(maxClientMutationFutureSkew)) {
		return "", time.Time{}, false, true
	}
	return updatedAt, parsed, true, false
}

func progressResetPayloadTime(payload map[string]any, serverNow time.Time) (string, time.Time, bool, bool) {
	for _, key := range []string{"client_updated_at", "updated_at"} {
		raw := strings.TrimSpace(stringValue(payload[key]))
		if raw == "" {
			continue
		}
		parsed := browseStateTime(raw)
		if parsed.IsZero() {
			return "", time.Time{}, false, false
		}
		if parsed.After(serverNow.UTC().Add(maxClientMutationFutureSkew)) {
			return "", time.Time{}, false, true
		}
		return formatOrderedClientTime(parsed), parsed, true, false
	}
	return "", time.Time{}, false, false
}

func formatOrderedClientTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func persistedClientClock(raw string, serverNow time.Time) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	parsed := browseStateTime(raw)
	if parsed.IsZero() || parsed.After(serverNow.UTC().Add(maxClientMutationFutureSkew)) {
		return time.Time{}, true
	}
	return parsed, false
}

func seedLegacyUserMarkFieldClocksTx(
	tx *sql.Tx,
	targetType string,
	identityID string,
	existingMarkUpdatedAt string,
	legacyProtectedFields []string,
	serverNow time.Time,
	now string,
) error {
	if len(legacyProtectedFields) == 0 {
		return nil
	}
	rows, err := tx.Query(`
		SELECT field_name, client_updated_at
		FROM user_mark_field_clocks
		WHERE reader_profile_key = 'default'
		  AND target_type = ?
		  AND identity_id = ?
	`, targetType, identityID)
	if err != nil {
		return err
	}
	trustedClocks := map[string]bool{}
	untrustedFields := []string{}
	for rows.Next() {
		var fieldName, rawClock string
		if err := rows.Scan(&fieldName, &rawClock); err != nil {
			_ = rows.Close()
			return err
		}
		clockTime, untrusted := persistedClientClock(rawClock, serverNow)
		if untrusted {
			untrustedFields = append(untrustedFields, fieldName)
		} else if !clockTime.IsZero() {
			trustedClocks[fieldName] = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, fieldName := range untrustedFields {
		if _, err := tx.Exec(`
			DELETE FROM user_mark_field_clocks
			WHERE reader_profile_key = 'default'
			  AND target_type = ?
			  AND identity_id = ?
			  AND field_name = ?
		`, targetType, identityID, fieldName); err != nil {
			return err
		}
	}
	legacyTime, _ := persistedClientClock(existingMarkUpdatedAt, serverNow)
	if legacyTime.IsZero() {
		return nil
	}
	legacyClock := formatOrderedClientTime(legacyTime)
	for _, fieldName := range legacyProtectedFields {
		if trustedClocks[fieldName] {
			continue
		}
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO user_mark_field_clocks (
				reader_profile_key, target_type, identity_id, field_name,
				client_updated_at, created_at, updated_at
			) VALUES ('default', ?, ?, ?, ?, ?, ?)
		`, targetType, identityID, fieldName, legacyClock, now, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleProgressMigration(w http.ResponseWriter, r *http.Request) {
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
	items := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		if item, ok := normalizeMigrationItem(raw); ok {
			items = append(items, item)
		}
	}
	now := nowISO()
	result := map[string]any{
		"ok":                true,
		"received":          len(rawItems),
		"valid":             len(items),
		"imported":          0,
		"skipped":           0,
		"needs_calibration": 0,
		"failed":            0,
		"items":             []map[string]any{},
	}
	addItemResult := func(item map[string]any) {
		result["items"] = append(result["items"].([]map[string]any), item)
	}

	if _, err := s.db.Exec(`
		INSERT OR IGNORE INTO reader_profiles (key, display_name, created_at, updated_at)
		VALUES ('default', 'Default', ?, ?)
	`, now, now); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, item := range items {
		candidateID := stringValue(item["candidate_id"])
		rowResult := map[string]any{"candidate_id": candidateID}
		workRows, err := s.query("SELECT * FROM work_browse WHERE candidate_id = ?", candidateID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(workRows) == 0 {
			result["failed"] = intValue(result["failed"]) + 1
			rowResult["status"] = "failed"
			rowResult["reason"] = "work not found"
			addItemResult(rowResult)
			continue
		}
		work := workRows[0]
		workIdentityID := stringValue(work["work_identity_id"])
		if workIdentityID == "" {
			result["failed"] = intValue(result["failed"]) + 1
			rowResult["status"] = "failed"
			rowResult["reason"] = "work identity missing"
			addItemResult(rowResult)
			continue
		}
		manifest, err := s.currentManifestForWork(r.Context(), work)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		pageCount := 0
		if manifest != nil {
			pageCount = intValue(manifest["page_count"])
		}
		if pageCount <= 0 {
			result["failed"] = intValue(result["failed"]) + 1
			rowResult["status"] = "failed"
			rowResult["reason"] = "page manifest missing"
			addItemResult(rowResult)
			continue
		}
		requestedIndex := intValue(item["index"])
		requestedCount := intValue(item["count"])
		lastPageIndex := requestedIndex
		if lastPageIndex > pageCount-1 {
			lastPageIndex = pageCount - 1
		}
		manifestHash := stringValue(item["manifest_hash"])
		progressStatus := "normal"
		if manifestHash != "" && manifestHash != stringValue(manifest["manifest_hash"]) {
			progressStatus = "manifest_stale"
		} else if requestedCount != 0 && requestedCount != pageCount {
			progressStatus = "needs_calibration"
		} else if manifestHash == "" && requestedCount == 0 {
			progressStatus = "needs_calibration"
		}
		completed := boolValue(item["completed"]) || lastPageIndex+1 >= pageCount
		progressPercent := math.Round((float64(lastPageIndex+1)/float64(pageCount)*100)*100) / 100
		lastReadAt, itemUpdatedTime, itemUpdatedAtProvided, itemUpdatedAtFuture := progressPayloadUpdatedAt(item, time.Now().UTC())
		if itemUpdatedAtFuture {
			result["skipped"] = intValue(result["skipped"]) + 1
			rowResult["status"] = "skipped"
			rowResult["reason"] = "future timestamp"
			addItemResult(rowResult)
			continue
		}
		snapshotHash := manifestHash
		if snapshotHash == "" {
			snapshotHash = stringValue(manifest["manifest_hash"])
		}
		pageCountSnapshot := requestedCount
		if pageCountSnapshot == 0 {
			pageCountSnapshot = pageCount
		}
		stored, skipReason, err := s.storeMigratedProgress(
			workIdentityID,
			candidateID,
			stringValue(manifest["page_manifest_id"]),
			snapshotHash,
			progressStatus,
			lastPageIndex,
			progressPercent,
			completed,
			pageCountSnapshot,
			lastReadAt,
			itemUpdatedTime,
			itemUpdatedAtProvided,
			now,
		)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !stored {
			result["skipped"] = intValue(result["skipped"]) + 1
			rowResult["status"] = "skipped"
			rowResult["reason"] = skipReason
			addItemResult(rowResult)
			continue
		}
		result["imported"] = intValue(result["imported"]) + 1
		if progressStatus != "normal" {
			result["needs_calibration"] = intValue(result["needs_calibration"]) + 1
		}
		rowResult["status"] = "imported"
		rowResult["progress_status"] = progressStatus
		rowResult["index"] = lastPageIndex
		rowResult["count"] = pageCount
		rowResult["title"] = stringValue(work["title"])
		addItemResult(rowResult)
	}
	writeJSON(w, result)
}

func (s *Server) storeMigratedProgress(
	workIdentityID string,
	candidateID string,
	pageManifestID string,
	manifestHash string,
	progressStatus string,
	lastPageIndex int,
	progressPercent float64,
	completed bool,
	pageCountSnapshot int,
	lastReadAt string,
	itemUpdatedTime time.Time,
	itemUpdatedAtProvided bool,
	now string,
) (bool, string, error) {
	serverNow := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return false, "", err
	}
	defer tx.Rollback()

	// Acquire the SQLite writer lock before reading the clocks used for the decision.
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO reader_profiles (key, display_name, created_at, updated_at)
		VALUES ('default', 'Default', ?, ?)
	`, now, now); err != nil {
		return false, "", err
	}

	var resetAt string
	resetExists := false
	if err := tx.QueryRow(`
		SELECT reset_at
		FROM reading_progress_resets
		WHERE reader_profile_key = 'default'
		  AND work_identity_id = ?
	`, workIdentityID).Scan(&resetAt); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return false, "", err
		}
	} else {
		resetExists = true
		resetTime, resetClockUntrusted := persistedClientClock(resetAt, serverNow)
		if !itemUpdatedAtProvided || (!resetClockUntrusted && !itemUpdatedTime.After(resetTime)) {
			return false, "progress reset is newer", nil
		}
	}

	var currentReadStatus, currentReadStatusClientUpdatedAt string
	if err := tx.QueryRow(`
		SELECT read_status, read_status_client_updated_at
		FROM work_user_marks
		WHERE reader_profile_key = 'default'
		  AND work_identity_id = ?
	`, workIdentityID).Scan(&currentReadStatus, &currentReadStatusClientUpdatedAt); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return false, "", err
		}
	}
	readStatusTime, readStatusClockUntrusted := persistedClientClock(currentReadStatusClientUpdatedAt, serverNow)
	if readStatusClockUntrusted {
		if _, err := tx.Exec(`
			UPDATE work_user_marks
			SET read_status_client_updated_at = ''
			WHERE reader_profile_key = 'default'
			  AND work_identity_id = ?
			  AND read_status_client_updated_at = ?
		`, workIdentityID, currentReadStatusClientUpdatedAt); err != nil {
			return false, "", err
		}
		readStatusTime = time.Time{}
	}
	if !readStatusTime.IsZero() {
		sameCompletedEvent := completed && currentReadStatus == "completed" && itemUpdatedAtProvided && itemUpdatedTime.Equal(readStatusTime)
		if !itemUpdatedAtProvided || (!itemUpdatedTime.After(readStatusTime) && !sameCompletedEvent) {
			return false, "read status is newer", nil
		}
	}

	var existingLastReadAt, existingUpdatedAt string
	existingProgress := false
	if err := tx.QueryRow(`
		SELECT COALESCE(last_read_at, ''), updated_at
		FROM reading_progress
		WHERE reader_profile_key = 'default'
		  AND work_identity_id = ?
	`, workIdentityID).Scan(&existingLastReadAt, &existingUpdatedAt); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return false, "", err
		}
	} else {
		existingProgress = true
	}
	if existingProgress {
		if !itemUpdatedAtProvided {
			return false, "untimed sqlite progress cannot replace existing progress", nil
		}
		existingTime, existingClockUntrusted := persistedClientClock(existingLastReadAt, serverNow)
		if existingTime.IsZero() && !existingClockUntrusted {
			existingTime, existingClockUntrusted = persistedClientClock(existingUpdatedAt, serverNow)
		}
		if !existingClockUntrusted && !existingTime.IsZero() && !itemUpdatedTime.After(existingTime) {
			return false, "sqlite progress is newer", nil
		}
	}

	if _, err := tx.Exec(`
		INSERT INTO reading_progress (
			reader_profile_key, work_identity_id, candidate_id, page_manifest_id,
			manifest_hash_snapshot, progress_status, last_page_index, progress_percent,
			completed, page_count_snapshot, last_read_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(reader_profile_key, work_identity_id) DO UPDATE SET
			candidate_id = excluded.candidate_id,
			page_manifest_id = excluded.page_manifest_id,
			manifest_hash_snapshot = excluded.manifest_hash_snapshot,
			progress_status = excluded.progress_status,
			last_page_index = excluded.last_page_index,
			progress_percent = excluded.progress_percent,
			completed = excluded.completed,
			page_count_snapshot = excluded.page_count_snapshot,
			last_read_at = excluded.last_read_at,
			updated_at = excluded.updated_at
	`, "default", workIdentityID, candidateID, pageManifestID, manifestHash, progressStatus, lastPageIndex, progressPercent, boolInt(completed), pageCountSnapshot, lastReadAt, now, lastReadAt); err != nil {
		return false, "", err
	}
	if resetExists {
		if _, err := tx.Exec(`
			DELETE FROM reading_progress_resets
			WHERE reader_profile_key = 'default'
			  AND work_identity_id = ?
			  AND reset_at = ?
		`, workIdentityID, resetAt); err != nil {
			return false, "", err
		}
	}
	progressReadStatus := "reading"
	if completed {
		progressReadStatus = "completed"
	}
	if err := markWorkReadStatusFromProgressTx(tx, workIdentityID, candidateID, progressReadStatus, now, formatOrderedClientTime(itemUpdatedTime)); err != nil {
		return false, "", err
	}
	if err := tx.Commit(); err != nil {
		return false, "", err
	}
	return true, "", nil
}

func normalizeMigrationItem(raw any) (map[string]any, bool) {
	source, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	candidateID := strings.TrimSpace(stringValue(source["candidate_id"]))
	if candidateID == "" {
		return nil, false
	}
	updatedAt := strings.TrimSpace(coalesceString(source["updated_at"], source["last_read_at"]))
	if len(updatedAt) > 80 {
		updatedAt = ""
	}
	return map[string]any{
		"candidate_id":     candidateID,
		"index":            clampInt(stringValue(source["index"]), 0, 0, 1_000_000),
		"count":            clampInt(stringValue(source["count"]), 0, 0, 1_000_000),
		"manifest_hash":    strings.TrimSpace(stringValue(source["manifest_hash"])),
		"page_manifest_id": strings.TrimSpace(stringValue(source["page_manifest_id"])),
		"title":            strings.TrimSpace(stringValue(source["title"])),
		"completed":        boolValue(source["completed"]),
		"updated_at":       updatedAt,
	}, true
}

func (s *Server) handleSeriesProgressGet(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	groupID := strings.TrimSpace(r.URL.Query().Get("id"))
	if groupID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing id")
		return
	}
	rows, err := s.query(`
		SELECT
			rp.*,
			COALESCE(progress_wb.title, current_wb.title, '') AS title,
			COALESCE(NULLIF(progress_si.sort_key, ''), NULLIF(current_si.sort_key, ''), '') AS series_progress_sort_key,
			COALESCE(NULLIF(progress_si.sequence_number, ''), NULLIF(current_si.sequence_number, ''), '') AS series_progress_sequence
		FROM reading_progress rp
		JOIN work_identities wi ON wi.work_identity_id = rp.work_identity_id
		LEFT JOIN series_items progress_si ON progress_si.candidate_id = rp.candidate_id
		LEFT JOIN series_items current_si ON current_si.candidate_id = wi.current_candidate_id
		LEFT JOIN work_browse progress_wb ON progress_wb.candidate_id = rp.candidate_id
		LEFT JOIN work_browse current_wb ON current_wb.candidate_id = wi.current_candidate_id
		WHERE rp.reader_profile_key = 'default'
		  AND (
			  progress_si.group_id = ?
			  OR (
				  current_si.group_id = ?
				  AND `+visibleWorkCandidateExistsSQL("wi.current_candidate_id")+`
			  )
		  )
		  AND `+visibleWorkCandidateExistsSQL("rp.candidate_id")+`
	`, groupID, groupID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var progress map[string]any
	var progressRow map[string]any
	for _, row := range rows {
		if seriesResumeProgressRowBetter(row, progressRow) {
			progressRow = row
		}
	}
	if progressRow != nil {
		manifest, err := s.currentManifestForCandidate(r.Context(), stringValue(progressRow["candidate_id"]))
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		progress = formatProgress(progressRow, manifest, stringValue(progressRow["title"]))
	}
	writeJSON(w, map[string]any{"group_id": groupID, "progress": progress})
}

func formatProgress(row map[string]any, manifest map[string]any, title string) map[string]any {
	status := stringValue(row["progress_status"])
	if status == "" {
		status = "normal"
	}
	if manifest != nil && stringValue(row["manifest_hash_snapshot"]) != "" && stringValue(row["manifest_hash_snapshot"]) != stringValue(manifest["manifest_hash"]) {
		status = "manifest_stale"
	}
	count := intValue(row["page_count_snapshot"])
	if count == 0 && manifest != nil {
		count = intValue(manifest["page_count"])
	}
	return map[string]any{
		"candidate_id":       row["candidate_id"],
		"work_identity_id":   row["work_identity_id"],
		"page_manifest_id":   row["page_manifest_id"],
		"manifest_hash":      row["manifest_hash_snapshot"],
		"index":              intValue(row["last_page_index"]),
		"count":              count,
		"progress_percent":   floatValue(row["progress_percent"]),
		"completed":          intValue(row["completed"]) != 0,
		"progress_status":    status,
		"reader_fit_mode":    stringValue(row["reader_fit_mode"]),
		"reader_split_panel": intValue(row["reader_split_panel"]),
		"stage_scroll_top":   intValue(row["stage_scroll_top"]),
		"stage_scroll_left":  intValue(row["stage_scroll_left"]),
		"updated_at":         row["updated_at"],
		"last_read_at":       row["last_read_at"],
		"title":              title,
	}
}

func workListProgressSelectSQL() string {
	return `
		COALESCE(rp.page_manifest_id, '') AS progress_page_manifest_id,
		COALESCE(rp.manifest_hash_snapshot, '') AS progress_manifest_hash,
		COALESCE(rp.progress_status, '') AS progress_status,
		COALESCE(rp.last_page_index, 0) AS progress_index,
		COALESCE(rp.progress_percent, 0) AS progress_percent,
		COALESCE(rp.completed, 0) AS progress_completed,
		COALESCE(rp.page_count_snapshot, 0) AS progress_count,
		COALESCE(rp.reader_fit_mode, '') AS progress_reader_fit_mode,
		COALESCE(rp.reader_split_panel, 0) AS progress_reader_split_panel,
		COALESCE(rp.stage_scroll_top, 0) AS progress_stage_scroll_top,
		COALESCE(rp.stage_scroll_left, 0) AS progress_stage_scroll_left,
		COALESCE(rp.last_read_at, '') AS progress_last_read_at,
		COALESCE(rp.updated_at, '') AS progress_updated_at`
}

func workListProgressJoinSQL(workIdentityExpr string) string {
	return `
		LEFT JOIN reading_progress rp
			ON rp.reader_profile_key = 'default'
		   AND rp.work_identity_id = ` + workIdentityExpr
}

func workListRequiredProgressJoinSQL(workIdentityExpr string) string {
	return `
		JOIN reading_progress rp INDEXED BY idx_reading_progress_recent_julianday
			ON rp.reader_profile_key = 'default'
		   AND rp.last_read_at <> ''
		   AND rp.work_identity_id = ` + workIdentityExpr
}

func attachWorkListProgress(row map[string]any) {
	if stringValue(row["progress_updated_at"]) == "" && stringValue(row["progress_last_read_at"]) == "" {
		return
	}
	status := stringValue(row["progress_status"])
	if status == "" {
		status = "normal"
	}
	row["progress"] = map[string]any{
		"candidate_id":       row["candidate_id"],
		"work_identity_id":   row["work_identity_id"],
		"page_manifest_id":   row["progress_page_manifest_id"],
		"manifest_hash":      row["progress_manifest_hash"],
		"index":              intValue(row["progress_index"]),
		"count":              intValue(row["progress_count"]),
		"progress_percent":   floatValue(row["progress_percent"]),
		"completed":          intValue(row["progress_completed"]) != 0,
		"progress_status":    status,
		"reader_fit_mode":    stringValue(row["progress_reader_fit_mode"]),
		"reader_split_panel": intValue(row["progress_reader_split_panel"]),
		"stage_scroll_top":   intValue(row["progress_stage_scroll_top"]),
		"stage_scroll_left":  intValue(row["progress_stage_scroll_left"]),
		"updated_at":         row["progress_updated_at"],
		"last_read_at":       row["progress_last_read_at"],
		"title":              shelfTitle(row),
	}
}
