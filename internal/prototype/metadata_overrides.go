package prototype

import (
	"net/http"
	"strings"
	"unicode"
)

// publicMetadataOverrideFields is deliberately smaller than the compatibility
// fields that the catalog can read from metadata_field_overrides. The public
// write API only owns local, presentation-only values and never accepts import
// provenance or provider metadata.
var publicMetadataOverrideFields = map[string]int{
	"title":    500,
	"creator":  300,
	"series":   500,
	"language": 80,
}

func (s *Server) handleMetadataOverrides(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleMetadataOverridesGet(w, r)
	case http.MethodPost:
		s.handleMetadataOverrideSave(w, r)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleMetadataOverridesGet(w http.ResponseWriter, r *http.Request) {
	targetType := strings.TrimSpace(r.URL.Query().Get("target_type"))
	targetID := strings.TrimSpace(r.URL.Query().Get("target_id"))
	if targetType != "work" || targetID == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid metadata override target")
		return
	}
	identityID, currentCandidateID, found, err := s.resolveMetadataOverrideWork(targetID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "work not found")
		return
	}
	response, err := s.metadataOverrideResponse(identityID, currentCandidateID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, response)
}

func (s *Server) handleMetadataOverrideSave(w http.ResponseWriter, r *http.Request) {
	payload, err := readJSONBody(r, 16*1024)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !metadataOverridePayloadHasExactKeys(payload) {
		writeJSONError(w, http.StatusBadRequest, "invalid metadata override payload")
		return
	}
	targetType, targetTypeOK := metadataOverridePayloadString(payload, "target_type")
	targetID, targetIDOK := metadataOverridePayloadString(payload, "target_id")
	fieldName, fieldNameOK := metadataOverridePayloadString(payload, "field_name")
	fieldValue, fieldValueOK := metadataOverridePayloadString(payload, "field_value")
	targetType = strings.TrimSpace(targetType)
	targetID = strings.TrimSpace(targetID)
	fieldName = strings.TrimSpace(fieldName)
	fieldValue = strings.TrimSpace(fieldValue)
	maxRunes, allowed := publicMetadataOverrideFields[fieldName]
	if !targetTypeOK || !targetIDOK || !fieldNameOK || !fieldValueOK || targetType != "work" || targetID == "" || !allowed {
		writeJSONError(w, http.StatusBadRequest, "invalid metadata override")
		return
	}
	if !validPublicMetadataOverrideValue(fieldValue, maxRunes) {
		writeJSONError(w, http.StatusBadRequest, "invalid metadata override value")
		return
	}
	identityID, currentCandidateID, found, err := s.resolveMetadataOverrideWork(targetID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "work not found")
		return
	}

	now := nowISO()
	if fieldValue == "" {
		_, err = s.db.Exec(`
			UPDATE metadata_field_overrides
			SET override_status = 'reverted',
				reverted_at = ?,
				updated_at = ?
			WHERE work_identity_id = ?
			  AND field_name = ?
		`, now, now, identityID, fieldName)
	} else {
		_, err = s.db.Exec(`
			INSERT INTO metadata_field_overrides (
				work_identity_id, candidate_id, field_name, field_value,
				source_proposal_id, source_field_id, override_status,
				reviewer_note, applied_at, reverted_at, created_at, updated_at
			) VALUES (?, ?, ?, ?, NULL, NULL, 'active', '', ?, NULL, ?, ?)
			ON CONFLICT(work_identity_id, field_name) DO UPDATE SET
				candidate_id = excluded.candidate_id,
				field_value = excluded.field_value,
				source_proposal_id = NULL,
				source_field_id = NULL,
				override_status = 'active',
				reviewer_note = '',
				applied_at = excluded.applied_at,
				reverted_at = NULL,
				updated_at = excluded.updated_at
		`, identityID, currentCandidateID, fieldName, fieldValue, now, now, now)
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.checkpointSQLiteWALBestEffort("metadata_override_save")
	s.clearReviewItemsCache()
	s.clearCoverDuplicateResponseCache()
	response, err := s.metadataOverrideResponse(identityID, currentCandidateID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response["ok"] = true
	writeJSON(w, response)
}

func metadataOverridePayloadString(payload map[string]any, key string) (string, bool) {
	value, ok := payload[key].(string)
	return value, ok
}

func metadataOverridePayloadHasExactKeys(payload map[string]any) bool {
	if len(payload) != 4 {
		return false
	}
	for _, key := range []string{"target_type", "target_id", "field_name", "field_value"} {
		if _, present := payload[key]; !present {
			return false
		}
	}
	return true
}

func validPublicMetadataOverrideValue(value string, maxRunes int) bool {
	if len([]rune(value)) > maxRunes {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (s *Server) resolveMetadataOverrideWork(candidateID string) (string, string, bool, error) {
	rows, err := s.query(`
		SELECT wi.work_identity_id, wi.current_candidate_id
		FROM work_identities wi
		JOIN work_browse wb ON wb.candidate_id = wi.current_candidate_id
		WHERE wi.current_candidate_id = ?
		LIMIT 1
	`, candidateID)
	if err != nil {
		return "", "", false, err
	}
	if len(rows) == 0 {
		return "", "", false, nil
	}
	identityID := strings.TrimSpace(stringValue(rows[0]["work_identity_id"]))
	currentCandidateID := strings.TrimSpace(stringValue(rows[0]["current_candidate_id"]))
	return identityID, currentCandidateID, identityID != "" && currentCandidateID != "", nil
}

func (s *Server) metadataOverrideResponse(identityID, candidateID string) (map[string]any, error) {
	rows, err := s.query(`
		SELECT field_name, field_value, applied_at, updated_at
		FROM metadata_field_overrides
		WHERE work_identity_id = ?
		  AND override_status = 'active'
		  AND field_name IN ('title', 'creator', 'series', 'language')
		ORDER BY field_name
	`, identityID)
	if err != nil {
		return nil, err
	}
	overrides := map[string]any{}
	for _, row := range rows {
		fieldName := stringValue(row["field_name"])
		if _, allowed := publicMetadataOverrideFields[fieldName]; !allowed {
			continue
		}
		overrides[fieldName] = map[string]any{
			"field_name":  fieldName,
			"field_value": stringValue(row["field_value"]),
			"applied_at":  row["applied_at"],
			"updated_at":  row["updated_at"],
		}
	}
	return map[string]any{
		"target_type":      "work",
		"target_id":        candidateID,
		"work_identity_id": identityID,
		"overrides":        overrides,
	}, nil
}
