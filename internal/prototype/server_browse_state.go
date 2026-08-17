package prototype

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleBrowseState(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleBrowseStateSave(w, r)
		return
	}
	s.handleBrowseStateGet(w, r)
}

func (s *Server) handleBrowseStateGet(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	state, updatedAt, err := s.loadBrowseState()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"state":      state,
		"updated_at": updatedAt,
	})
}

func (s *Server) handleBrowseStateSave(w http.ResponseWriter, r *http.Request) {
	payload, err := readJSONBody(r, 64*1024)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	rawState, ok := payload["state"].(map[string]any)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "missing state")
		return
	}
	statePayload, clientUpdatedAt, err := normalizeBrowseStatePayload(rawState)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	stateJSON, err := json.Marshal(statePayload)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := nowISO()
	if _, err := s.db.Exec(`
		INSERT OR IGNORE INTO reader_profiles (key, display_name, created_at, updated_at)
		VALUES ('default', 'Default', ?, ?)
	`, now, now); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	currentRows, err := s.query(`
		SELECT state_json, client_updated_at
		FROM browse_states
		WHERE reader_profile_key = 'default'
		  AND state_key = 'browse'
	`)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(currentRows) > 0 {
		currentUpdatedAt := stringValue(currentRows[0]["client_updated_at"])
		if browseStateTime(clientUpdatedAt).Before(browseStateTime(currentUpdatedAt)) {
			state, updatedAt, err := browseStateFromRow(currentRows[0])
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, map[string]any{"ok": true, "stored": false, "state": state, "updated_at": updatedAt})
			return
		}
	}

	if _, err := s.db.Exec(`
		INSERT INTO browse_states (
			reader_profile_key, state_key, state_json, client_updated_at, created_at, updated_at
		) VALUES ('default', 'browse', ?, ?, ?, ?)
		ON CONFLICT(reader_profile_key, state_key) DO UPDATE SET
			state_json = excluded.state_json,
			client_updated_at = excluded.client_updated_at,
			updated_at = excluded.updated_at
	`, string(stateJSON), clientUpdatedAt, now, now); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "stored": true, "state": statePayload, "updated_at": clientUpdatedAt})
}

func (s *Server) loadBrowseState() (map[string]any, string, error) {
	rows, err := s.query(`
		SELECT state_json, client_updated_at
		FROM browse_states
		WHERE reader_profile_key = 'default'
		  AND state_key = 'browse'
	`)
	if err != nil || len(rows) == 0 {
		return nil, "", err
	}
	return browseStateFromRow(rows[0])
}

func browseStateFromRow(row map[string]any) (map[string]any, string, error) {
	updatedAt := stringValue(row["client_updated_at"])
	var state map[string]any
	if err := json.Unmarshal([]byte(stringValue(row["state_json"])), &state); err != nil {
		return nil, "", err
	}
	if state == nil {
		state = map[string]any{}
	}
	return state, updatedAt, nil
}

func normalizeBrowseStatePayload(raw map[string]any) (map[string]any, string, error) {
	view := browseStateLimitedString(raw["bmangaView"], 32)
	if !map[string]bool{"shelf": true, "works": true, "series": true}[view] {
		return nil, "", fmt.Errorf("unsupported browse view")
	}
	browseType := browseStateLimitedString(raw["bmangaType"], 32)
	if view != "works" {
		browseType = ""
	} else if !map[string]bool{"": true, "doujin": true, "manga_file": true}[browseType] {
		browseType = ""
	}
	limit := clampInt(stringValue(raw["bmangaLimit"]), 60, 1, 500)
	offset := clampInt(stringValue(raw["bmangaOffset"]), 0, 0, 1_000_000_000)
	page := clampInt(stringValue(raw["bmangaPage"]), 1, 1, 1_000_000)
	scrollY := clampInt(stringValue(raw["bmangaBrowseScrollY"]), 0, 0, 10_000_000)
	anchorType := browseStateLimitedString(raw["bmangaBrowseAnchorType"], 32)
	if anchorType != "work" && anchorType != "series" {
		anchorType = ""
	}
	anchorID := browseStateLimitedString(raw["bmangaBrowseAnchorId"], 160)
	if anchorType == "" {
		anchorID = ""
	}
	anchorTop := clampInt(stringValue(raw["bmangaBrowseAnchorTop"]), 0, -1_000_000, 1_000_000)
	clientUpdatedAt := browseStateLimitedString(raw["updated_at"], 64)
	if browseStateTime(clientUpdatedAt).IsZero() {
		clientUpdatedAt = nowISO()
	}
	state := map[string]any{
		"bmangaBrowse":           true,
		"bmangaView":             view,
		"bmangaType":             browseType,
		"bmangaOffset":           offset,
		"bmangaLimit":            limit,
		"bmangaPage":             page,
		"bmangaBrowseScrollY":    scrollY,
		"bmangaBrowseAnchorType": anchorType,
		"bmangaBrowseAnchorId":   anchorID,
		"bmangaBrowseAnchorTop":  anchorTop,
		"bmangaSearch":           browseStateLimitedString(raw["bmangaSearch"], 500),
		"bmangaLibrary":          browseStateLimitedString(raw["bmangaLibrary"], 120),
		"bmangaSource":           browseStateLimitedString(raw["bmangaSource"], 120),
		"bmangaPageStatus":       browseStateLimitedString(raw["bmangaPageStatus"], 80),
		"bmangaAction":           browseStateLimitedString(raw["bmangaAction"], 80),
		"bmangaUserMark":         browseStateLimitedString(raw["bmangaUserMark"], 80),
		"bmangaTag":              browseStateLimitedString(raw["bmangaTag"], 120),
		"bmangaTagQuick":         browseStateLimitedString(raw["bmangaTagQuick"], 120),
		"bmangaSort":             browseStateLimitedString(raw["bmangaSort"], 80),
		"bmangaBrowseReason":     browseStateLimitedString(raw["bmangaBrowseReason"], 80),
		"updated_at":             clientUpdatedAt,
	}
	return state, clientUpdatedAt, nil
}

func browseStateLimitedString(value any, limit int) string {
	text := strings.TrimSpace(stringValue(value))
	if limit > 0 && len([]rune(text)) > limit {
		runes := []rune(text)
		text = string(runes[:limit])
	}
	return text
}

func browseStateTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed
}
