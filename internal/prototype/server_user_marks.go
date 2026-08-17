package prototype

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Server) handleUserMark(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleUserMarkSave(w, r)
		return
	}
	s.handleUserMarkGet(w, r)
}

func normalizeTagName(value any) (string, string, error) {
	display := strings.Join(strings.Fields(stringValue(value)), " ")
	if display == "" {
		return "", "", errors.New("tag name required")
	}
	if utf8.RuneCountInString(display) > 48 {
		return "", "", errors.New("tag name too long")
	}
	if strings.Contains(display, ",") {
		return "", "", errors.New("tag name cannot contain comma")
	}
	return strings.ToLower(display), display, nil
}

func normalizeTagColor(value any) (string, error) {
	raw := strings.TrimSpace(stringValue(value))
	if raw == "" {
		return "", nil
	}
	if !tagColorRe.MatchString(raw) {
		return "", errors.New("invalid tag color")
	}
	return strings.ToLower(raw), nil
}

func tagKeyFromQuery(value string) string {
	key, _, err := normalizeTagName(value)
	if err != nil {
		return ""
	}
	return key
}

func addWorkTagFilter(filters *[]string, args *[]any, tagKey string) {
	if tagKey == "" {
		return
	}
	*filters = append(*filters, `
		EXISTS (
			SELECT 1
			FROM work_tag_links wtl
			WHERE wtl.reader_profile_key = 'default'
			  AND wtl.work_identity_id = wb.work_identity_id
			  AND wtl.tag_key = ?
		)
	`)
	*args = append(*args, tagKey)
}

func addSeriesTagFilter(filters *[]string, args *[]any, tagKey string) {
	if tagKey == "" {
		return
	}
	*filters = append(*filters, `
		EXISTS (
			SELECT 1
			FROM series_identities sid_tag
			JOIN series_tag_links stl ON stl.series_identity_id = sid_tag.series_identity_id
			WHERE sid_tag.current_group_id = sg.group_id
			  AND stl.reader_profile_key = 'default'
			  AND stl.tag_key = ?
		)
	`)
	*args = append(*args, tagKey)
}

func tagSelectSQL(targetType string) string {
	if targetType == "series" {
		return `
			(
				SELECT GROUP_CONCAT(lt.display_name, ',')
				FROM series_identities sid_tag_display
				JOIN series_tag_links stl ON stl.series_identity_id = sid_tag_display.series_identity_id
				JOIN local_tags lt ON lt.tag_key = stl.tag_key
				WHERE sid_tag_display.current_group_id = sg.group_id
				  AND stl.reader_profile_key = 'default'
			) AS user_tags
		`
	}
	return `
		(
			SELECT GROUP_CONCAT(lt.display_name, ',')
			FROM work_tag_links wtl
			JOIN local_tags lt ON lt.tag_key = wtl.tag_key
			WHERE wtl.work_identity_id = wb.work_identity_id
			  AND wtl.reader_profile_key = 'default'
		) AS user_tags
	`
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleTagSave(w, r)
		return
	}
	s.handleTagsGet(w, r)
}

func (s *Server) allTags() ([]map[string]any, error) {
	return s.query(`
		WITH work_counts AS (
			SELECT tag_key, COUNT(*) AS work_count
			FROM work_tag_links
			WHERE reader_profile_key = 'default'
			GROUP BY tag_key
		),
		visible_work_counts AS (
			SELECT wtl.tag_key, COUNT(DISTINCT wtl.work_identity_id) AS visible_work_count
			FROM work_tag_links wtl
			JOIN work_identities wi ON wi.work_identity_id = wtl.work_identity_id
			JOIN work_candidates wc ON wc.candidate_id = wi.current_candidate_id
			WHERE wtl.reader_profile_key = 'default'
			  AND (wc.candidate_type = 'doujin' OR NOT EXISTS (
				SELECT 1 FROM series_items si
				WHERE si.candidate_id = wc.candidate_id
			))
			GROUP BY wtl.tag_key
		),
		series_counts AS (
			SELECT tag_key, COUNT(*) AS series_count
			FROM series_tag_links
			WHERE reader_profile_key = 'default'
			GROUP BY tag_key
		)
		SELECT
			lt.tag_key,
			lt.display_name,
			lt.color,
			COALESCE(wc.work_count, 0) AS work_count,
			COALESCE(vwc.visible_work_count, 0) AS visible_work_count,
			COALESCE(sc.series_count, 0) AS series_count
		FROM local_tags lt
		LEFT JOIN work_counts wc ON wc.tag_key = lt.tag_key
		LEFT JOIN visible_work_counts vwc ON vwc.tag_key = lt.tag_key
		LEFT JOIN series_counts sc ON sc.tag_key = lt.tag_key
		ORDER BY lt.display_name COLLATE NOCASE
	`)
}

func (s *Server) assignedTags(targetType, targetID string) ([]map[string]any, error) {
	target, err := s.resolveUserMarkTarget(targetType, targetID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []map[string]any{}, nil
		}
		return nil, err
	}
	table := "work_tag_links"
	identityColumn := "work_identity_id"
	if targetType == "series" {
		table = "series_tag_links"
		identityColumn = "series_identity_id"
	}
	return s.query(fmt.Sprintf(`
		SELECT lt.tag_key, lt.display_name, lt.color
		FROM %s link
		JOIN local_tags lt ON lt.tag_key = link.tag_key
		WHERE link.reader_profile_key = 'default'
		  AND link.%s = ?
		ORDER BY lt.display_name COLLATE NOCASE
	`, table, identityColumn), target.identityID)
}

func (s *Server) handleTagsGet(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	query := r.URL.Query()
	targetType := strings.TrimSpace(query.Get("target_type"))
	targetID := strings.TrimSpace(query.Get("target_id"))
	if targetID == "" {
		targetID = strings.TrimSpace(query.Get("id"))
	}
	assignedOnly := strings.EqualFold(strings.TrimSpace(query.Get("assigned_only")), "1") ||
		strings.EqualFold(strings.TrimSpace(query.Get("assigned_only")), "true")
	payload := map[string]any{}
	if !assignedOnly || targetType == "" || targetID == "" {
		tags, err := s.allTags()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		payload["tags"] = tags
	}
	if targetType != "" && targetID != "" {
		assigned, err := s.assignedTags(targetType, targetID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		payload["target_type"] = targetType
		payload["target_id"] = targetID
		payload["assigned"] = assigned
	}
	writeJSON(w, payload)
}

func (s *Server) handleTagSave(w http.ResponseWriter, r *http.Request) {
	payload, err := readJSONBody(r, 64*1024)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	targetType := strings.TrimSpace(stringValue(payload["target_type"]))
	targetID := strings.TrimSpace(stringValue(payload["target_id"]))
	action := strings.TrimSpace(stringValue(payload["action"]))
	if action == "" {
		action = "add"
	}
	if action == "rename" || action == "update" || action == "update_color" || action == "delete" {
		s.handleTagManage(w, payload, action)
		return
	}
	if targetType == "" || targetID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing target")
		return
	}
	rawTag := payload["tag_name"]
	if stringValue(rawTag) == "" {
		rawTag = payload["tag_key"]
	}
	tagKey, displayName, err := normalizeTagName(rawTag)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	tagColor, err := normalizeTagColor(payload["color"])
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if action != "add" && action != "remove" {
		writeJSONError(w, http.StatusBadRequest, "invalid action")
		return
	}
	target, err := s.resolveUserMarkTarget(targetType, targetID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSONError(w, http.StatusNotFound, "target not found")
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	table := "work_tag_links"
	identityColumn := "work_identity_id"
	targetColumn := "candidate_id"
	if targetType == "series" {
		table = "series_tag_links"
		identityColumn = "series_identity_id"
		targetColumn = "group_id"
	}
	now := nowISO()
	if _, err := s.db.Exec(`
		INSERT OR IGNORE INTO reader_profiles (key, display_name, created_at, updated_at)
		VALUES ('default', 'Default', ?, ?)
	`, now, now); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if action == "add" {
		if tagColor != "" {
			if _, err := s.db.Exec(`
				INSERT INTO local_tags (tag_key, display_name, color, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(tag_key) DO UPDATE SET
					display_name = excluded.display_name,
					color = excluded.color,
					updated_at = excluded.updated_at
			`, tagKey, displayName, tagColor, now, now); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
		} else {
			if _, err := s.db.Exec(`
				INSERT INTO local_tags (tag_key, display_name, created_at, updated_at)
				VALUES (?, ?, ?, ?)
				ON CONFLICT(tag_key) DO UPDATE SET
					display_name = excluded.display_name,
					updated_at = excluded.updated_at
			`, tagKey, displayName, now, now); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if _, err := s.db.Exec(fmt.Sprintf(`
			INSERT OR IGNORE INTO %s (
				reader_profile_key, %s, %s, tag_key, created_at
			) VALUES ('default', ?, ?, ?, ?)
		`, table, identityColumn, targetColumn), target.identityID, target.storedTargetID, tagKey, now); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		if _, err := s.db.Exec(fmt.Sprintf(`
			DELETE FROM %s
			WHERE reader_profile_key = 'default'
			  AND %s = ?
			  AND tag_key = ?
		`, table, identityColumn), target.identityID, tagKey); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, err := s.db.Exec(`
			DELETE FROM local_tags
			WHERE tag_key = ?
			  AND NOT EXISTS (SELECT 1 FROM work_tag_links WHERE tag_key = ?)
			  AND NOT EXISTS (SELECT 1 FROM series_tag_links WHERE tag_key = ?)
		`, tagKey, tagKey, tagKey); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	assigned, err := s.assignedTags(targetType, targetID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tags, err := s.allTags()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"ok":          true,
		"target_type": targetType,
		"target_id":   targetID,
		"assigned":    assigned,
		"tags":        tags,
	})
}

func (s *Server) handleTagManage(w http.ResponseWriter, payload map[string]any, action string) {
	rawTag := payload["tag_name"]
	if stringValue(rawTag) == "" {
		rawTag = payload["tag_key"]
	}
	oldKey, _, err := normalizeTagName(rawTag)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	hasColor := false
	if _, ok := payload["color"]; ok {
		hasColor = true
	}
	newRawTag := payload["new_tag_name"]
	if stringValue(newRawTag) == "" {
		newRawTag = payload["display_name"]
	}
	if stringValue(newRawTag) == "" {
		newRawTag = rawTag
	}
	newKey, newDisplay, err := normalizeTagName(newRawTag)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	tagColor := ""
	if hasColor {
		tagColor, err = normalizeTagColor(payload["color"])
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

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

	var oldDisplay, oldColor string
	err = tx.QueryRow(`SELECT display_name, color FROM local_tags WHERE tag_key = ?`, oldKey).Scan(&oldDisplay, &oldColor)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "tag not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := nowISO()
	changedTag := map[string]any(nil)

	if action == "delete" {
		if _, err := tx.Exec(`DELETE FROM work_tag_links WHERE tag_key = ?`, oldKey); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, err := tx.Exec(`DELETE FROM series_tag_links WHERE tag_key = ?`, oldKey); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, err := tx.Exec(`DELETE FROM local_tags WHERE tag_key = ?`, oldKey); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else if newKey == oldKey {
		nextColor := oldColor
		if hasColor {
			nextColor = tagColor
		}
		if _, err := tx.Exec(`
			UPDATE local_tags
			SET display_name = ?, color = ?, updated_at = ?
			WHERE tag_key = ?
		`, newDisplay, nextColor, now, oldKey); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		changedTag = map[string]any{"tag_key": oldKey, "display_name": newDisplay, "color": nextColor}
	} else {
		var existingColor string
		existingErr := tx.QueryRow(`SELECT color FROM local_tags WHERE tag_key = ?`, newKey).Scan(&existingColor)
		if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
			writeJSONError(w, http.StatusInternalServerError, existingErr.Error())
			return
		}
		nextColor := oldColor
		if existingErr == nil {
			nextColor = existingColor
		}
		if hasColor {
			nextColor = tagColor
		}
		if _, err := tx.Exec(`
			INSERT INTO local_tags (tag_key, display_name, color, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(tag_key) DO UPDATE SET
				display_name = excluded.display_name,
				color = excluded.color,
				updated_at = excluded.updated_at
		`, newKey, newDisplay, nextColor, now, now); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO work_tag_links (
				reader_profile_key, work_identity_id, candidate_id, tag_key, created_at
			)
			SELECT reader_profile_key, work_identity_id, candidate_id, ?, created_at
			FROM work_tag_links
			WHERE tag_key = ?
		`, newKey, oldKey); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO series_tag_links (
				reader_profile_key, series_identity_id, group_id, tag_key, created_at
			)
			SELECT reader_profile_key, series_identity_id, group_id, ?, created_at
			FROM series_tag_links
			WHERE tag_key = ?
		`, newKey, oldKey); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, err := tx.Exec(`DELETE FROM work_tag_links WHERE tag_key = ?`, oldKey); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, err := tx.Exec(`DELETE FROM series_tag_links WHERE tag_key = ?`, oldKey); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, err := tx.Exec(`DELETE FROM local_tags WHERE tag_key = ?`, oldKey); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		changedTag = map[string]any{"tag_key": newKey, "display_name": newDisplay, "color": nextColor}
	}

	if err := tx.Commit(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	committed = true
	tags, err := s.allTags()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"ok":     true,
		"action": action,
		"tag":    changedTag,
		"tags":   tags,
	})
}

func (s *Server) handleUserMarkGet(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	query := r.URL.Query()
	targetType := strings.TrimSpace(query.Get("target_type"))
	targetID := strings.TrimSpace(query.Get("target_id"))
	if targetID == "" {
		targetID = strings.TrimSpace(query.Get("id"))
	}
	if targetType == "" || targetID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing target")
		return
	}
	var mark map[string]any
	switch targetType {
	case "work":
		workRows, err := s.query("SELECT * FROM work_browse WHERE candidate_id = ?", targetID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(workRows) == 0 {
			writeJSONError(w, http.StatusNotFound, "work not found")
			return
		}
		var markErr error
		mark, markErr = s.getWorkUserMark(workRows[0])
		if markErr != nil {
			writeJSONError(w, http.StatusInternalServerError, markErr.Error())
			return
		}
	case "series":
		exists, err := s.query("SELECT 1 FROM series_groups WHERE group_id = ?", targetID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(exists) == 0 {
			writeJSONError(w, http.StatusNotFound, "series not found")
			return
		}
		var markErr error
		mark, markErr = s.getSeriesUserMark(targetID)
		if markErr != nil {
			writeJSONError(w, http.StatusInternalServerError, markErr.Error())
			return
		}
	default:
		writeJSONError(w, http.StatusBadRequest, "invalid target_type")
		return
	}
	writeJSON(w, map[string]any{"target_type": targetType, "target_id": targetID, "mark": mark})
}

func userMarkClockedFieldNames(targetType string) []string {
	fields := []string{"personal_rating", "favorite", "reread_priority", "hidden", "hidden_reason", "notes"}
	if targetType == "work" {
		fields = []string{"personal_rating", "favorite", "reread_priority", "translation_quality", "image_quality", "hidden", "hidden_reason", "notes"}
	}
	return fields
}

func userMarkClockedPatchFields(payload map[string]any, targetType string) []string {
	fields := userMarkClockedFieldNames(targetType)
	present := make([]string, 0, len(fields))
	for _, field := range fields {
		if _, ok := payload[field]; ok {
			present = append(present, field)
		}
	}
	return present
}

func resolveUserMarkFieldUpdatesTx(
	tx *sql.Tx,
	targetType string,
	identityID string,
	requestedFields []string,
	clientUpdatedAt string,
	clientTime time.Time,
	clientTimeProvided bool,
	clientTimeRejected bool,
	markExists bool,
	legacyProtectedFields []string,
	existingMarkUpdatedAt string,
	serverNow time.Time,
	now string,
) (map[string]bool, []string, []string, error) {
	accepted := make(map[string]bool, len(requestedFields))
	storedFields := make([]string, 0, len(requestedFields))
	rejectedFields := make([]string, 0, len(requestedFields))
	if len(requestedFields) == 0 {
		return accepted, storedFields, rejectedFields, nil
	}

	rows, err := tx.Query(`
		SELECT field_name, client_updated_at
		FROM user_mark_field_clocks
		WHERE reader_profile_key = 'default'
		  AND target_type = ?
		  AND identity_id = ?
	`, targetType, identityID)
	if err != nil {
		return nil, nil, nil, err
	}
	clocks := map[string]time.Time{}
	untrustedFields := []string{}
	for rows.Next() {
		var fieldName, rawClock string
		if err := rows.Scan(&fieldName, &rawClock); err != nil {
			_ = rows.Close()
			return nil, nil, nil, err
		}
		clockTime, untrusted := persistedClientClock(rawClock, serverNow)
		if untrusted {
			untrustedFields = append(untrustedFields, fieldName)
			continue
		}
		if !clockTime.IsZero() {
			clocks[fieldName] = clockTime
		}
	}
	if err := rows.Close(); err != nil {
		return nil, nil, nil, err
	}
	for _, fieldName := range untrustedFields {
		if _, err := tx.Exec(`
			DELETE FROM user_mark_field_clocks
			WHERE reader_profile_key = 'default'
			  AND target_type = ?
			  AND identity_id = ?
			  AND field_name = ?
		`, targetType, identityID, fieldName); err != nil {
			return nil, nil, nil, err
		}
	}

	upsertClock := func(fieldName, clock string) error {
		_, err := tx.Exec(`
			INSERT INTO user_mark_field_clocks (
				reader_profile_key, target_type, identity_id, field_name,
				client_updated_at, created_at, updated_at
			) VALUES ('default', ?, ?, ?, ?, ?, ?)
			ON CONFLICT(reader_profile_key, target_type, identity_id, field_name) DO UPDATE SET
				client_updated_at = excluded.client_updated_at,
				updated_at = excluded.updated_at
		`, targetType, identityID, fieldName, clock, now, now)
		return err
	}
	if markExists && len(legacyProtectedFields) > 0 && clientTimeProvided && !clientTimeRejected {
		legacyLowerBound, _ := persistedClientClock(existingMarkUpdatedAt, serverNow)
		if !legacyLowerBound.IsZero() {
			legacyClock := formatOrderedClientTime(legacyLowerBound)
			for _, fieldName := range legacyProtectedFields {
				if !clocks[fieldName].IsZero() {
					continue
				}
				if err := upsertClock(fieldName, legacyClock); err != nil {
					return nil, nil, nil, err
				}
				clocks[fieldName] = legacyLowerBound
			}
		}
	}

	for _, fieldName := range requestedFields {
		fieldClock := clocks[fieldName]
		fieldAccepted := false
		switch {
		case clientTimeRejected:
			fieldAccepted = false
		case clientTimeProvided:
			switch {
			case !fieldClock.IsZero():
				fieldAccepted = clientTime.After(fieldClock)
			default:
				fieldAccepted = true
			}
		case !fieldClock.IsZero():
			fieldAccepted = false
		default:
			fieldAccepted = true
		}
		if fieldAccepted {
			accepted[fieldName] = true
			storedFields = append(storedFields, fieldName)
			if clientTimeProvided {
				if err := upsertClock(fieldName, clientUpdatedAt); err != nil {
					return nil, nil, nil, err
				}
			}
		} else {
			rejectedFields = append(rejectedFields, fieldName)
		}
	}
	return accepted, storedFields, rejectedFields, nil
}

func (s *Server) handleUserMarkSave(w http.ResponseWriter, r *http.Request) {
	payload, err := readJSONBody(r, 64*1024)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	targetType := strings.TrimSpace(stringValue(payload["target_type"]))
	targetID := strings.TrimSpace(stringValue(payload["target_id"]))
	if targetType == "" || targetID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing target")
		return
	}

	target, err := s.resolveUserMarkTarget(targetType, targetID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSONError(w, http.StatusNotFound, "target not found")
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	readStatus := strings.TrimSpace(stringValue(payload["read_status"]))
	_, readStatusProvided := payload["read_status"]
	if readStatus == "" {
		readStatus = "unread"
	}
	if !validReadStatus(readStatus) {
		writeJSONError(w, http.StatusBadRequest, "invalid read_status")
		return
	}
	personalRating, err := optionalInt(payload["personal_rating"], 0, 10, "personal_rating")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	rereadPriority, err := optionalIntDefault(payload["reread_priority"], 0, 0, 3, "reread_priority")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	translationQuality, err := optionalInt(payload["translation_quality"], 1, 5, "translation_quality")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	imageQuality, err := optionalInt(payload["image_quality"], 1, 5, "image_quality")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	favorite := boolInt(boolValue(payload["favorite"]))
	hidden := boolInt(boolValue(payload["hidden"]))
	hiddenReason := stringValue(payload["hidden_reason"])
	notes := stringValue(payload["notes"])
	serverNow := time.Now().UTC()
	readStatusClientUpdatedAt, readStatusClientTime, readStatusClientTimeProvided, readStatusClientTimeFuture := progressResetPayloadTime(payload, serverNow)
	rawMarkClientTime := strings.TrimSpace(coalesceString(payload["client_updated_at"], payload["updated_at"]))
	markFieldClientTimeRejected := readStatusClientTimeFuture || (rawMarkClientTime != "" && !readStatusClientTimeProvided)
	now := serverNow.Local().Format("2006-01-02T15:04:05-07:00")
	readStatusStored := false
	resetStored := false
	requestedMarkFields := userMarkClockedPatchFields(payload, targetType)
	storedFields := []string{}
	rejectedFields := []string{}

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

	if targetType == "work" {
		var existingReadStatusClientUpdatedAt, existingMarkedAt, existingMarkUpdatedAt string
		var existingPersonalRating, existingTranslationQuality, existingImageQuality sql.NullInt64
		var existingFavorite, existingRereadPriority, existingHidden int
		var existingHiddenReason, existingNotes string
		markExists := true
		if err := tx.QueryRow(`
			SELECT
				read_status_client_updated_at, marked_at, updated_at,
				personal_rating, favorite, reread_priority,
				translation_quality, image_quality, hidden, hidden_reason, notes
			FROM work_user_marks
			WHERE reader_profile_key = 'default'
			  AND work_identity_id = ?
		`, target.identityID).Scan(
			&existingReadStatusClientUpdatedAt,
			&existingMarkedAt,
			&existingMarkUpdatedAt,
			&existingPersonalRating,
			&existingFavorite,
			&existingRereadPriority,
			&existingTranslationQuality,
			&existingImageQuality,
			&existingHidden,
			&existingHiddenReason,
			&existingNotes,
		); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			markExists = false
		}
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

		var progressLastReadAt, progressUpdatedAt string
		var progressCompleted int
		progressExists := true
		if err := tx.QueryRow(`
			SELECT COALESCE(last_read_at, ''), updated_at, completed
			FROM reading_progress
			WHERE reader_profile_key = 'default'
			  AND work_identity_id = ?
		`, target.identityID).Scan(&progressLastReadAt, &progressUpdatedAt, &progressCompleted); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			progressExists = false
		}
		progressTime, progressClockUntrusted := persistedClientClock(progressLastReadAt, serverNow)
		if progressTime.IsZero() && !progressClockUntrusted {
			progressTime, _ = persistedClientClock(progressUpdatedAt, serverNow)
		}

		var existingResetAt string
		resetExists := true
		if readStatusProvided && readStatusClientTimeProvided {
			if err := tx.QueryRow(`
				SELECT reset_at
				FROM reading_progress_resets
				WHERE reader_profile_key = 'default'
				  AND work_identity_id = ?
			`, target.identityID).Scan(&existingResetAt); err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					writeJSONError(w, http.StatusInternalServerError, err.Error())
					return
				}
				resetExists = false
			}
		} else {
			resetExists = false
		}
		existingResetTime, resetClockUntrusted := persistedClientClock(existingResetAt, serverNow)
		if resetExists && resetClockUntrusted {
			if _, err := tx.Exec(`
				DELETE FROM reading_progress_resets
				WHERE reader_profile_key = 'default'
				  AND work_identity_id = ?
				  AND reset_at = ?
			`, target.identityID, existingResetAt); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			resetExists = false
			existingResetTime = time.Time{}
		}

		existingReadStatusTime, readStatusClockUntrusted := persistedClientClock(existingReadStatusClientUpdatedAt, serverNow)
		if readStatusClockUntrusted {
			if _, err := tx.Exec(`
				UPDATE work_user_marks
				SET read_status_client_updated_at = ''
				WHERE reader_profile_key = 'default'
				  AND work_identity_id = ?
				  AND read_status_client_updated_at = ?
			`, target.identityID, existingReadStatusClientUpdatedAt); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			existingReadStatusClientUpdatedAt = ""
			existingReadStatusTime = time.Time{}
		}
		if existingReadStatusTime.IsZero() && markExists && progressTime.IsZero() && existingResetTime.IsZero() {
			legacyMarkedTime, legacyMarkedUntrusted := persistedClientClock(existingMarkedAt, serverNow)
			legacyUpdatedTime, legacyUpdatedUntrusted := persistedClientClock(existingMarkUpdatedAt, serverNow)
			if !legacyMarkedUntrusted && !legacyUpdatedUntrusted && legacyUpdatedTime.After(legacyMarkedTime) {
				legacyMarkedTime = legacyUpdatedTime
			}
			existingReadStatusTime = legacyMarkedTime
			if readStatusProvided && readStatusClientTimeProvided && !existingReadStatusTime.IsZero() {
				existingReadStatusClientUpdatedAt = formatOrderedClientTime(existingReadStatusTime)
				if _, err := tx.Exec(`
					UPDATE work_user_marks
					SET read_status_client_updated_at = ?
					WHERE reader_profile_key = 'default'
					  AND work_identity_id = ?
					  AND read_status_client_updated_at = ''
				`, existingReadStatusClientUpdatedAt, target.identityID); err != nil {
					writeJSONError(w, http.StatusInternalServerError, err.Error())
					return
				}
			}
		}

		readStatusEventAccepted := readStatusProvided && !markFieldClientTimeRejected
		if readStatusEventAccepted && readStatusClientTimeProvided {
			if (!existingReadStatusTime.IsZero() && !readStatusClientTime.After(existingReadStatusTime)) ||
				(!progressTime.IsZero() && !readStatusClientTime.After(progressTime)) ||
				(!existingResetTime.IsZero() && !readStatusClientTime.After(existingResetTime)) {
				readStatusEventAccepted = false
			}
		} else if readStatusEventAccepted && strings.TrimSpace(existingReadStatusClientUpdatedAt) != "" {
			// Once a timed status event exists, an untimed legacy event cannot safely supersede it.
			readStatusEventAccepted = false
		}
		readStatusStored = readStatusEventAccepted
		if readStatusEventAccepted {
			if err := seedLegacyUserMarkFieldClocksTx(
				tx,
				"work",
				target.identityID,
				existingMarkUpdatedAt,
				legacyProtectedFields,
				serverNow,
				now,
			); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		acceptedFields, branchStoredFields, branchRejectedFields, err := resolveUserMarkFieldUpdatesTx(
			tx,
			"work",
			target.identityID,
			requestedMarkFields,
			readStatusClientUpdatedAt,
			readStatusClientTime,
			readStatusClientTimeProvided,
			markFieldClientTimeRejected,
			markExists,
			legacyProtectedFields,
			existingMarkUpdatedAt,
			serverNow,
			now,
		)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		storedFields = branchStoredFields
		rejectedFields = branchRejectedFields

		initialReadStatus := "unread"
		initialReadStatusClientUpdatedAt := ""
		if !markExists {
			switch {
			case readStatusEventAccepted:
				initialReadStatus = readStatus
				if readStatusClientTimeProvided {
					initialReadStatusClientUpdatedAt = readStatusClientUpdatedAt
				}
			case progressExists && !progressTime.IsZero():
				initialReadStatus = "reading"
				if progressCompleted != 0 {
					initialReadStatus = "completed"
				}
				initialReadStatusClientUpdatedAt = formatOrderedClientTime(progressTime)
			case !existingResetTime.IsZero():
				initialReadStatusClientUpdatedAt = formatOrderedClientTime(existingResetTime)
			}
		}
		if markExists || readStatusEventAccepted || len(storedFields) > 0 {
			if _, err := tx.Exec(`
				INSERT OR IGNORE INTO work_user_marks (
					reader_profile_key, work_identity_id, candidate_id, read_status, read_status_client_updated_at,
					favorite, reread_priority, hidden, hidden_reason, notes,
					marked_at, created_at, updated_at
				) VALUES (?, ?, ?, ?, ?, 0, 0, 0, '', '', ?, ?, ?)
			`, "default", target.identityID, target.storedTargetID, initialReadStatus, initialReadStatusClientUpdatedAt, now, now, now); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		updates := []string{"candidate_id = ?"}
		args := []any{target.storedTargetID}
		if readStatusEventAccepted {
			updates = append(updates, "read_status = ?")
			args = append(args, readStatus)
			if readStatusClientTimeProvided {
				updates = append(updates, "read_status_client_updated_at = ?")
				args = append(args, readStatusClientUpdatedAt)
			}
		}
		if acceptedFields["personal_rating"] {
			updates = append(updates, "personal_rating = ?")
			args = append(args, personalRating)
		}
		if acceptedFields["favorite"] {
			updates = append(updates, "favorite = ?")
			args = append(args, favorite)
		}
		if acceptedFields["reread_priority"] {
			updates = append(updates, "reread_priority = ?")
			args = append(args, rereadPriority)
		}
		if acceptedFields["translation_quality"] {
			updates = append(updates, "translation_quality = ?")
			args = append(args, translationQuality)
		}
		if acceptedFields["image_quality"] {
			updates = append(updates, "image_quality = ?")
			args = append(args, imageQuality)
		}
		if acceptedFields["hidden"] {
			updates = append(updates, "hidden = ?")
			args = append(args, hidden)
		}
		if acceptedFields["hidden_reason"] {
			updates = append(updates, "hidden_reason = ?")
			args = append(args, hiddenReason)
		}
		if acceptedFields["notes"] {
			updates = append(updates, "notes = ?")
			args = append(args, notes)
		}
		if readStatusEventAccepted || len(storedFields) > 0 {
			updates = append(updates, "marked_at = ?", "updated_at = ?")
			args = append(args, now, now)
		}
		args = append(args, target.identityID)
		if _, err := tx.Exec(fmt.Sprintf(`
			UPDATE work_user_marks
			SET %s
			WHERE reader_profile_key = 'default'
			  AND work_identity_id = ?
		`, strings.Join(updates, ", ")), args...); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if readStatusEventAccepted && readStatus == "unread" && readStatusClientTimeProvided {
			if _, err := tx.Exec(`
				INSERT INTO reading_progress_resets (
					reader_profile_key, work_identity_id, reset_at, created_at, updated_at
				) VALUES ('default', ?, ?, ?, ?)
				ON CONFLICT(reader_profile_key, work_identity_id) DO UPDATE SET
					reset_at = excluded.reset_at,
					updated_at = excluded.updated_at
				WHERE excluded.reset_at > reading_progress_resets.reset_at
			`, target.identityID, readStatusClientUpdatedAt, now, now); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			resetStored = true
			if _, err := tx.Exec(`
				DELETE FROM reading_progress
				WHERE reader_profile_key = 'default'
				  AND work_identity_id = ?
			`, target.identityID); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	} else {
		var existingSeriesReadStatusClientUpdatedAt, existingSeriesMarkedAt, existingSeriesMarkUpdatedAt string
		var existingSeriesPersonalRating sql.NullInt64
		var existingSeriesFavorite, existingSeriesRereadPriority, existingSeriesHidden int
		var existingSeriesHiddenReason, existingSeriesNotes string
		seriesMarkExists := true
		if err := tx.QueryRow(`
			SELECT
				read_status_client_updated_at, marked_at, updated_at,
				personal_rating, favorite, reread_priority, hidden, hidden_reason, notes
			FROM series_user_marks
			WHERE reader_profile_key = 'default'
			  AND series_identity_id = ?
		`, target.identityID).Scan(
			&existingSeriesReadStatusClientUpdatedAt,
			&existingSeriesMarkedAt,
			&existingSeriesMarkUpdatedAt,
			&existingSeriesPersonalRating,
			&existingSeriesFavorite,
			&existingSeriesRereadPriority,
			&existingSeriesHidden,
			&existingSeriesHiddenReason,
			&existingSeriesNotes,
		); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			seriesMarkExists = false
		}
		legacyProtectedFields := []string{}
		if existingSeriesPersonalRating.Valid {
			legacyProtectedFields = append(legacyProtectedFields, "personal_rating")
		}
		if existingSeriesFavorite != 0 {
			legacyProtectedFields = append(legacyProtectedFields, "favorite")
		}
		if existingSeriesRereadPriority != 0 {
			legacyProtectedFields = append(legacyProtectedFields, "reread_priority")
		}
		if existingSeriesHidden != 0 {
			legacyProtectedFields = append(legacyProtectedFields, "hidden")
		}
		if existingSeriesHiddenReason != "" {
			legacyProtectedFields = append(legacyProtectedFields, "hidden_reason")
		}
		if existingSeriesNotes != "" {
			legacyProtectedFields = append(legacyProtectedFields, "notes")
		}

		existingSeriesReadStatusTime, seriesReadStatusClockUntrusted := persistedClientClock(existingSeriesReadStatusClientUpdatedAt, serverNow)
		if seriesReadStatusClockUntrusted {
			if _, err := tx.Exec(`
				UPDATE series_user_marks
				SET read_status_client_updated_at = ''
				WHERE reader_profile_key = 'default'
				  AND series_identity_id = ?
				  AND read_status_client_updated_at = ?
			`, target.identityID, existingSeriesReadStatusClientUpdatedAt); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			existingSeriesReadStatusClientUpdatedAt = ""
			existingSeriesReadStatusTime = time.Time{}
		}
		if existingSeriesReadStatusTime.IsZero() && seriesMarkExists {
			legacyMarkedTime, legacyMarkedUntrusted := persistedClientClock(existingSeriesMarkedAt, serverNow)
			legacyUpdatedTime, legacyUpdatedUntrusted := persistedClientClock(existingSeriesMarkUpdatedAt, serverNow)
			if !legacyMarkedUntrusted && !legacyUpdatedUntrusted && legacyUpdatedTime.After(legacyMarkedTime) {
				legacyMarkedTime = legacyUpdatedTime
			}
			existingSeriesReadStatusTime = legacyMarkedTime
			if readStatusProvided && readStatusClientTimeProvided && !existingSeriesReadStatusTime.IsZero() {
				existingSeriesReadStatusClientUpdatedAt = formatOrderedClientTime(existingSeriesReadStatusTime)
				if _, err := tx.Exec(`
					UPDATE series_user_marks
					SET read_status_client_updated_at = ?
					WHERE reader_profile_key = 'default'
					  AND series_identity_id = ?
					  AND read_status_client_updated_at = ''
				`, existingSeriesReadStatusClientUpdatedAt, target.identityID); err != nil {
					writeJSONError(w, http.StatusInternalServerError, err.Error())
					return
				}
			}
		}

		seriesReadStatusEventAccepted := readStatusProvided && !markFieldClientTimeRejected
		if seriesReadStatusEventAccepted && readStatusClientTimeProvided {
			if !existingSeriesReadStatusTime.IsZero() && !readStatusClientTime.After(existingSeriesReadStatusTime) {
				seriesReadStatusEventAccepted = false
			}
		} else if seriesReadStatusEventAccepted && strings.TrimSpace(existingSeriesReadStatusClientUpdatedAt) != "" {
			// Once a timed status event exists, an untimed legacy event cannot safely supersede it.
			seriesReadStatusEventAccepted = false
		}
		readStatusStored = seriesReadStatusEventAccepted
		if seriesReadStatusEventAccepted {
			if err := seedLegacyUserMarkFieldClocksTx(
				tx,
				"series",
				target.identityID,
				existingSeriesMarkUpdatedAt,
				legacyProtectedFields,
				serverNow,
				now,
			); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		acceptedFields, branchStoredFields, branchRejectedFields, err := resolveUserMarkFieldUpdatesTx(
			tx,
			"series",
			target.identityID,
			requestedMarkFields,
			readStatusClientUpdatedAt,
			readStatusClientTime,
			readStatusClientTimeProvided,
			markFieldClientTimeRejected,
			seriesMarkExists,
			legacyProtectedFields,
			existingSeriesMarkUpdatedAt,
			serverNow,
			now,
		)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		storedFields = branchStoredFields
		rejectedFields = branchRejectedFields
		if seriesMarkExists || seriesReadStatusEventAccepted || len(storedFields) > 0 {
			if _, err := tx.Exec(`
				INSERT OR IGNORE INTO series_user_marks (
					reader_profile_key, series_identity_id, group_id, read_status, read_status_client_updated_at,
					favorite, reread_priority, hidden, hidden_reason, notes,
					marked_at, created_at, updated_at
				) VALUES (?, ?, ?, 'unread', '', 0, 0, 0, '', '', ?, ?, ?)
			`, "default", target.identityID, target.storedTargetID, now, now, now); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		updates := []string{"group_id = ?"}
		args := []any{target.storedTargetID}
		if seriesReadStatusEventAccepted {
			updates = append(updates, "read_status = ?")
			args = append(args, readStatus)
			if readStatusClientTimeProvided {
				updates = append(updates, "read_status_client_updated_at = ?")
				args = append(args, readStatusClientUpdatedAt)
			}
		}
		if acceptedFields["personal_rating"] {
			updates = append(updates, "personal_rating = ?")
			args = append(args, personalRating)
		}
		if acceptedFields["favorite"] {
			updates = append(updates, "favorite = ?")
			args = append(args, favorite)
		}
		if acceptedFields["reread_priority"] {
			updates = append(updates, "reread_priority = ?")
			args = append(args, rereadPriority)
		}
		if acceptedFields["hidden"] {
			updates = append(updates, "hidden = ?")
			args = append(args, hidden)
		}
		if acceptedFields["hidden_reason"] {
			updates = append(updates, "hidden_reason = ?")
			args = append(args, hiddenReason)
		}
		if acceptedFields["notes"] {
			updates = append(updates, "notes = ?")
			args = append(args, notes)
		}
		if seriesReadStatusEventAccepted || len(storedFields) > 0 {
			updates = append(updates, "marked_at = ?", "updated_at = ?")
			args = append(args, now, now)
		}
		args = append(args, target.identityID)
		if _, err := tx.Exec(fmt.Sprintf(`
			UPDATE series_user_marks
			SET %s
			WHERE reader_profile_key = 'default'
			  AND series_identity_id = ?
		`, strings.Join(updates, ", ")), args...); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var mark map[string]any
	var markErr error
	if targetType == "work" {
		workRows, err := s.query("SELECT * FROM work_browse WHERE candidate_id = ?", targetID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(workRows) == 0 {
			writeJSONError(w, http.StatusInternalServerError, "work missing after user mark save")
			return
		}
		mark, markErr = s.getWorkUserMark(workRows[0])
	} else {
		mark, markErr = s.getSeriesUserMark(targetID)
	}
	if markErr != nil {
		writeJSONError(w, http.StatusInternalServerError, markErr.Error())
		return
	}
	if mark == nil {
		writeJSONError(w, http.StatusInternalServerError, "user mark missing after save")
		return
	}
	writeJSON(w, map[string]any{
		"ok":                 true,
		"target_type":        targetType,
		"target_id":          targetID,
		"read_status_stored": readStatusStored,
		"reset_stored":       resetStored,
		"stored_fields":      storedFields,
		"rejected_fields":    rejectedFields,
		"mark":               mark,
	})
}

func (s *Server) resolveUserMarkTarget(targetType string, targetID string) (userMarkTarget, error) {
	switch targetType {
	case "work":
		rows, err := s.query(`
			SELECT work_identity_id, candidate_id
			FROM work_browse
			WHERE candidate_id = ?
		`, targetID)
		if err != nil {
			return userMarkTarget{}, err
		}
		if len(rows) == 0 {
			return userMarkTarget{}, os.ErrNotExist
		}
		identityID := stringValue(rows[0]["work_identity_id"])
		if identityID == "" {
			return userMarkTarget{}, errors.New("work identity missing")
		}
		return userMarkTarget{identityID: identityID, storedTargetID: stringValue(rows[0]["candidate_id"])}, nil
	case "series":
		rows, err := s.query(`
			SELECT series_identity_id, current_group_id
			FROM series_identities
			WHERE current_group_id = ?
			LIMIT 1
		`, targetID)
		if err != nil {
			return userMarkTarget{}, err
		}
		if len(rows) == 0 {
			return userMarkTarget{}, os.ErrNotExist
		}
		identityID := stringValue(rows[0]["series_identity_id"])
		if identityID == "" {
			return userMarkTarget{}, errors.New("series identity missing")
		}
		return userMarkTarget{identityID: identityID, storedTargetID: stringValue(rows[0]["current_group_id"])}, nil
	default:
		return userMarkTarget{}, errors.New("invalid target_type")
	}
}

func (s *Server) getWorkUserMark(work map[string]any) (map[string]any, error) {
	workIdentityID := stringValue(work["work_identity_id"])
	candidateID := stringValue(work["candidate_id"])
	if workIdentityID == "" {
		return formatUserMark(nil, "work", candidateID, ""), nil
	}
	rows, err := s.query(`
		SELECT *
		FROM work_user_marks
		WHERE reader_profile_key = 'default'
		  AND work_identity_id = ?
	`, workIdentityID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return formatUserMark(nil, "work", candidateID, workIdentityID), nil
	}
	return formatUserMark(rows[0], "work", candidateID, workIdentityID), nil
}

func (s *Server) getSeriesUserMark(groupID string) (map[string]any, error) {
	identityRows, err := s.query(`
		SELECT series_identity_id
		FROM series_identities
		WHERE current_group_id = ?
		LIMIT 1
	`, groupID)
	if err != nil {
		return nil, err
	}
	if len(identityRows) == 0 {
		return formatUserMark(nil, "series", groupID, ""), nil
	}
	seriesIdentityID := stringValue(identityRows[0]["series_identity_id"])
	rows, err := s.query(`
		SELECT *
		FROM series_user_marks
		WHERE reader_profile_key = 'default'
		  AND series_identity_id = ?
	`, seriesIdentityID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return formatUserMark(nil, "series", groupID, seriesIdentityID), nil
	}
	return formatUserMark(rows[0], "series", groupID, seriesIdentityID), nil
}

func formatUserMark(row map[string]any, targetType, targetID, identityID string) map[string]any {
	if row == nil {
		return map[string]any{
			"target_type":                   targetType,
			"target_id":                     targetID,
			"identity_id":                   identityID,
			"read_status":                   "unread",
			"read_status_client_updated_at": "",
			"personal_rating":               nil,
			"favorite":                      false,
			"reread_priority":               0,
			"translation_quality":           nil,
			"image_quality":                 nil,
			"hidden":                        false,
			"hidden_reason":                 "",
			"notes":                         "",
			"marked_at":                     "",
			"updated_at":                    "",
		}
	}
	return map[string]any{
		"target_type":                   targetType,
		"target_id":                     targetID,
		"identity_id":                   identityID,
		"read_status":                   row["read_status"],
		"read_status_client_updated_at": stringValue(row["read_status_client_updated_at"]),
		"personal_rating":               row["personal_rating"],
		"favorite":                      intValue(row["favorite"]) != 0,
		"reread_priority":               intValue(row["reread_priority"]),
		"translation_quality":           row["translation_quality"],
		"image_quality":                 row["image_quality"],
		"hidden":                        intValue(row["hidden"]) != 0,
		"hidden_reason":                 stringValue(row["hidden_reason"]),
		"notes":                         stringValue(row["notes"]),
		"marked_at":                     stringValue(row["marked_at"]),
		"updated_at":                    stringValue(row["updated_at"]),
	}
}
