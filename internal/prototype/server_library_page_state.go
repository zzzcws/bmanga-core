package prototype

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	libraryPageStateKey         = "v2.library.page-scopes.v1"
	libraryPageStateVersion     = 1
	libraryPageStatePageSize    = 18
	libraryPageStateMaxOffset   = 1_000_000
	libraryPageStateMaxEventID  = 100
	libraryPageStateMaxBatch    = 24
	libraryPageStateCASAttempts = 32
)

var libraryPageStateModes = []string{"all", "doujin", "series"}

var errLibraryPageFutureTimestamp = errors.New("updated_at is too far in the future")

type libraryPagePosition struct {
	Offset    int    `json:"offset"`
	UpdatedAt string `json:"updated_at"`
	EventID   string `json:"event_id"`
}

type libraryPageState struct {
	Version       int                            `json:"version"`
	Sort          string                         `json:"sort"`
	SortUpdatedAt string                         `json:"sort_updated_at"`
	SortEventID   string                         `json:"sort_event_id"`
	Positions     map[string]libraryPagePosition `json:"positions"`
	UpdatedAt     string                         `json:"updated_at"`
}

type libraryPageMutation struct {
	Sort           string
	Mode           string
	Offset         int
	UpdatedAt      string
	UpdatedTime    time.Time
	EventID        string
	InitialOffsets map[string]int
}

func (s *Server) handleLibraryPageState(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleLibraryPageStateSave(w, r)
		return
	}
	s.handleLibraryPageStateGet(w, r)
}

func (s *Server) handleLibraryPageStateGet(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	state, _, found, err := s.loadLibraryPageState()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeJSON(w, map[string]any{"state": nil, "updated_at": ""})
		return
	}
	writeJSON(w, map[string]any{"state": state, "updated_at": state.UpdatedAt})
}

func (s *Server) handleLibraryPageStateSave(w http.ResponseWriter, r *http.Request) {
	payload, err := readJSONBody(r, 64*1024)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	rawStates, err := libraryPageMutationPayloads(payload)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	serverNow := time.Now().UTC()
	mutations := make([]libraryPageMutation, 0, len(rawStates))
	seenEventIDs := make(map[string]struct{}, len(rawStates))
	for _, rawState := range rawStates {
		mutation, err := normalizeLibraryPageMutation(rawState, serverNow)
		if err != nil {
			if errors.Is(err, errLibraryPageFutureTimestamp) {
				writeJSONStatus(w, http.StatusBadRequest, map[string]any{
					"error":       err.Error(),
					"code":        "future_timestamp",
					"server_time": formatOrderedClientTime(serverNow),
				})
				return
			}
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, exists := seenEventIDs[mutation.EventID]; exists {
			writeJSONError(w, http.StatusBadRequest, "duplicate event_id in states batch")
			return
		}
		seenEventIDs[mutation.EventID] = struct{}{}
		mutations = append(mutations, mutation)
	}
	sort.SliceStable(mutations, func(left, right int) bool {
		if mutations[left].UpdatedTime.Equal(mutations[right].UpdatedTime) {
			return mutations[left].EventID < mutations[right].EventID
		}
		return mutations[left].UpdatedTime.Before(mutations[right].UpdatedTime)
	})

	var state libraryPageState
	stored := false
	acknowledged := make([]string, 0, len(mutations))
	for _, mutation := range mutations {
		var mutationStored bool
		state, mutationStored, err = s.storeLibraryPageMutation(mutation)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		stored = stored || mutationStored
		acknowledged = append(acknowledged, mutation.EventID)
	}
	writeJSON(w, map[string]any{
		"ok":                     true,
		"stored":                 stored,
		"state":                  state,
		"updated_at":             state.UpdatedAt,
		"acknowledged_event_ids": acknowledged,
	})
}

func libraryPageMutationPayloads(payload map[string]any) ([]map[string]any, error) {
	if rawBatch, exists := payload["states"]; exists {
		if _, ambiguous := payload["state"]; ambiguous {
			return nil, fmt.Errorf("state and states cannot be combined")
		}
		items, ok := rawBatch.([]any)
		if !ok || len(items) == 0 || len(items) > libraryPageStateMaxBatch {
			return nil, fmt.Errorf("invalid states batch")
		}
		states := make([]map[string]any, 0, len(items))
		for _, item := range items {
			state, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid state in batch")
			}
			states = append(states, state)
		}
		return states, nil
	}
	state, ok := payload["state"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing state")
	}
	return []map[string]any{state}, nil
}

func normalizeLibraryPageMutation(raw map[string]any, serverNow time.Time) (libraryPageMutation, error) {
	sortKey := strings.TrimSpace(stringValue(raw["sort"]))
	if !validLibraryPageSort(sortKey) {
		return libraryPageMutation{}, fmt.Errorf("unsupported library sort")
	}
	mode := strings.TrimSpace(stringValue(raw["mode"]))
	if !validLibraryPageMode(mode) {
		return libraryPageMutation{}, fmt.Errorf("unsupported library mode")
	}
	rawUpdatedAt := strings.TrimSpace(stringValue(raw["updated_at"]))
	updatedTime := browseStateTime(rawUpdatedAt)
	if updatedTime.IsZero() {
		return libraryPageMutation{}, fmt.Errorf("invalid updated_at")
	}
	if updatedTime.After(serverNow.UTC().Add(maxClientMutationFutureSkew)) {
		return libraryPageMutation{}, errLibraryPageFutureTimestamp
	}
	rawEventID, ok := raw["event_id"].(string)
	if !ok {
		return libraryPageMutation{}, fmt.Errorf("invalid event_id")
	}
	eventID := strings.TrimSpace(rawEventID)
	if eventID == "" {
		return libraryPageMutation{}, fmt.Errorf("missing event_id")
	}
	if !validLibraryPageEventID(eventID) {
		return libraryPageMutation{}, fmt.Errorf("invalid event_id")
	}
	offset, err := normalizeLibraryPageOffset(raw["offset"])
	if err != nil {
		return libraryPageMutation{}, fmt.Errorf("invalid offset")
	}

	initialOffsets := map[string]int{}
	if rawInitialOffsets, exists := raw["initial_offsets"]; exists && rawInitialOffsets != nil {
		initialMap, ok := rawInitialOffsets.(map[string]any)
		if !ok {
			return libraryPageMutation{}, fmt.Errorf("invalid initial_offsets")
		}
		for _, itemMode := range libraryPageStateModes {
			if value, exists := initialMap[itemMode]; exists {
				initialOffset, err := normalizeLibraryPageOffset(value)
				if err != nil {
					return libraryPageMutation{}, fmt.Errorf("invalid initial offset for %s", itemMode)
				}
				initialOffsets[itemMode] = initialOffset
			}
		}
	}
	return libraryPageMutation{
		Sort:           sortKey,
		Mode:           mode,
		Offset:         offset,
		UpdatedAt:      formatOrderedClientTime(updatedTime),
		UpdatedTime:    updatedTime.UTC(),
		EventID:        eventID,
		InitialOffsets: initialOffsets,
	}, nil
}

func validLibraryPageSort(value string) bool {
	switch value {
	case "added_desc", "title_asc", "pages_desc":
		return true
	default:
		return false
	}
}

func validLibraryPageMode(value string) bool {
	switch value {
	case "all", "doujin", "series":
		return true
	default:
		return false
	}
}

func validLibraryPageEventID(value string) bool {
	if value == "" || len(value) > libraryPageStateMaxEventID {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	return true
}

func normalizeLibraryPageOffset(value any) (int, error) {
	var numeric float64
	switch typed := value.(type) {
	case float64:
		numeric = typed
	case float32:
		numeric = float64(typed)
	case int:
		numeric = float64(typed)
	case int32:
		numeric = float64(typed)
	case int64:
		numeric = float64(typed)
	default:
		return 0, fmt.Errorf("offset must be a number")
	}
	if math.IsNaN(numeric) || math.IsInf(numeric, 0) || numeric < 0 || math.Trunc(numeric) != numeric {
		return 0, fmt.Errorf("offset must be a finite non-negative integer")
	}
	if numeric > libraryPageStateMaxOffset {
		numeric = libraryPageStateMaxOffset
	}
	offset := int(numeric)
	return offset / libraryPageStatePageSize * libraryPageStatePageSize, nil
}

func alignLibraryPageOffset(value any) int {
	numeric := floatValue(value)
	if math.IsNaN(numeric) || numeric <= 0 {
		return 0
	}
	if math.IsInf(numeric, 1) || numeric > libraryPageStateMaxOffset {
		numeric = libraryPageStateMaxOffset
	}
	offset := int(numeric)
	return offset / libraryPageStatePageSize * libraryPageStatePageSize
}

func (s *Server) loadLibraryPageState() (libraryPageState, string, bool, error) {
	var rawStateJSON string
	err := s.db.QueryRow(`
		SELECT state_json
		FROM browse_states
		WHERE reader_profile_key = 'default'
		  AND state_key = ?
	`, libraryPageStateKey).Scan(&rawStateJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return libraryPageState{}, "", false, nil
	}
	if err != nil {
		return libraryPageState{}, "", false, err
	}
	state, err := decodeLibraryPageState(rawStateJSON)
	if err != nil {
		return libraryPageState{}, "", false, err
	}
	return state, rawStateJSON, true, nil
}

func decodeLibraryPageState(rawStateJSON string) (libraryPageState, error) {
	var state libraryPageState
	if err := json.Unmarshal([]byte(rawStateJSON), &state); err != nil {
		return libraryPageState{}, fmt.Errorf("decode library page state: %w", err)
	}
	if state.Version != libraryPageStateVersion || !validLibraryPageSort(state.Sort) {
		return libraryPageState{}, fmt.Errorf("invalid stored library page state")
	}
	if browseStateTime(state.SortUpdatedAt).IsZero() || !validLibraryPageEventID(state.SortEventID) {
		return libraryPageState{}, fmt.Errorf("invalid stored library sort clock")
	}
	if state.Positions == nil {
		return libraryPageState{}, fmt.Errorf("invalid stored library positions")
	}
	for _, mode := range libraryPageStateModes {
		position, ok := state.Positions[mode]
		if !ok || browseStateTime(position.UpdatedAt).IsZero() || !validLibraryPageEventID(position.EventID) {
			return libraryPageState{}, fmt.Errorf("invalid stored library position %s", mode)
		}
		if position.Offset != alignLibraryPageOffset(position.Offset) {
			return libraryPageState{}, fmt.Errorf("invalid stored library position %s", mode)
		}
	}
	state.UpdatedAt = libraryPageStateLatestTime(state)
	return state, nil
}

func (s *Server) storeLibraryPageMutation(mutation libraryPageMutation) (libraryPageState, bool, error) {
	now := nowISO()
	if _, err := s.db.Exec(`
		INSERT INTO reader_profiles (key, display_name, created_at, updated_at)
		VALUES ('default', 'Default', ?, ?)
		ON CONFLICT(key) DO NOTHING
	`, now, now); err != nil {
		return libraryPageState{}, false, err
	}

	for attempt := 0; attempt < libraryPageStateCASAttempts; attempt++ {
		current, rawCurrentJSON, found, err := s.loadLibraryPageState()
		if err != nil {
			return libraryPageState{}, false, err
		}
		if !found {
			next := initialLibraryPageState(mutation)
			nextJSON, err := json.Marshal(next)
			if err != nil {
				return libraryPageState{}, false, err
			}
			result, err := s.db.Exec(`
				INSERT INTO browse_states (
					reader_profile_key, state_key, state_json, client_updated_at, created_at, updated_at
				) VALUES ('default', ?, ?, ?, ?, ?)
				ON CONFLICT(reader_profile_key, state_key) DO NOTHING
			`, libraryPageStateKey, string(nextJSON), next.UpdatedAt, nowISO(), nowISO())
			if err != nil {
				return libraryPageState{}, false, err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return libraryPageState{}, false, err
			}
			if affected > 0 {
				return next, true, nil
			}
			continue
		}

		next, accepted := mergeLibraryPageMutation(current, mutation)
		if !accepted {
			return current, false, nil
		}
		nextJSON, err := json.Marshal(next)
		if err != nil {
			return libraryPageState{}, false, err
		}
		result, err := s.db.Exec(`
			UPDATE browse_states
			SET state_json = ?, client_updated_at = ?, updated_at = ?
			WHERE reader_profile_key = 'default'
			  AND state_key = ?
			  AND state_json = ?
		`, string(nextJSON), next.UpdatedAt, nowISO(), libraryPageStateKey, rawCurrentJSON)
		if err != nil {
			return libraryPageState{}, false, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return libraryPageState{}, false, err
		}
		if affected > 0 {
			return next, true, nil
		}
	}
	return libraryPageState{}, false, fmt.Errorf("library page state update contention")
}

func initialLibraryPageState(mutation libraryPageMutation) libraryPageState {
	positions := make(map[string]libraryPagePosition, len(libraryPageStateModes))
	for _, mode := range libraryPageStateModes {
		positions[mode] = libraryPagePosition{
			Offset:    mutation.InitialOffsets[mode],
			UpdatedAt: mutation.UpdatedAt,
			EventID:   mutation.EventID,
		}
	}
	position := positions[mutation.Mode]
	position.Offset = mutation.Offset
	positions[mutation.Mode] = position
	return libraryPageState{
		Version:       libraryPageStateVersion,
		Sort:          mutation.Sort,
		SortUpdatedAt: mutation.UpdatedAt,
		SortEventID:   mutation.EventID,
		Positions:     positions,
		UpdatedAt:     mutation.UpdatedAt,
	}
}

func mergeLibraryPageMutation(current libraryPageState, mutation libraryPageMutation) (libraryPageState, bool) {
	if mutation.Sort != current.Sort {
		latestTime, latestEventID := libraryPageStateLatestEvent(current)
		if !libraryPageEventAfter(mutation.UpdatedTime, mutation.EventID, formatOrderedClientTime(latestTime), latestEventID) {
			return current, false
		}
		positions := make(map[string]libraryPagePosition, len(libraryPageStateModes))
		for _, mode := range libraryPageStateModes {
			positions[mode] = libraryPagePosition{UpdatedAt: mutation.UpdatedAt, EventID: mutation.EventID}
		}
		position := positions[mutation.Mode]
		position.Offset = mutation.Offset
		positions[mutation.Mode] = position
		current.Sort = mutation.Sort
		current.SortUpdatedAt = mutation.UpdatedAt
		current.SortEventID = mutation.EventID
		current.Positions = positions
		current.UpdatedAt = mutation.UpdatedAt
		return current, true
	}

	position := current.Positions[mutation.Mode]
	if !libraryPageEventAfter(mutation.UpdatedTime, mutation.EventID, position.UpdatedAt, position.EventID) {
		return current, false
	}
	position.Offset = mutation.Offset
	position.UpdatedAt = mutation.UpdatedAt
	position.EventID = mutation.EventID
	current.Positions[mutation.Mode] = position
	current.UpdatedAt = libraryPageStateLatestTime(current)
	return current, true
}

func libraryPageEventAfter(incomingTime time.Time, incomingEventID, storedUpdatedAt, storedEventID string) bool {
	storedTime := browseStateTime(storedUpdatedAt)
	if incomingTime.After(storedTime) {
		return true
	}
	return incomingTime.Equal(storedTime) && incomingEventID > storedEventID
}

func libraryPageStateLatestTime(state libraryPageState) string {
	latest, _ := libraryPageStateLatestEvent(state)
	return formatOrderedClientTime(latest)
}

func libraryPageStateLatestEvent(state libraryPageState) (time.Time, string) {
	latest := browseStateTime(state.SortUpdatedAt)
	latestEventID := state.SortEventID
	for _, mode := range libraryPageStateModes {
		position := state.Positions[mode]
		candidate := browseStateTime(position.UpdatedAt)
		if candidate.After(latest) || (candidate.Equal(latest) && position.EventID > latestEventID) {
			latest = candidate
			latestEventID = position.EventID
		}
	}
	return latest, latestEventID
}
