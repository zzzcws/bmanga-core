package prototype

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetadataOverrideLiteSavesDisplaysSearchesAndClearsWithoutChangingSourceMetadata(t *testing.T) {
	s, err := NewServer(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`
		INSERT INTO work_candidates (
			candidate_id, library_key, library_name, candidate_type, source_kind,
			title, root, path, relative_path, parent_relative_path, source_record_id,
			source_status, source_reason, size_bytes, modified_utc, extension,
			page_file_count, confidence, notes
		) VALUES (
			'work-one', 'synthetic', 'Synthetic Library', 'doujin', 'archive',
			'Scanned Work Title', '/library', '/library/work-one.cbz', 'work-one.cbz', '', 'record-one',
			'ok', '', '100', '2026-08-17T00:00:00Z', '.cbz', '12', 'high', ''
		);
		INSERT INTO work_identities (
			work_identity_id, library_key, current_candidate_id, identity_type,
			display_title, canonical_relative_path, match_status, identity_version,
			first_seen_at, last_seen_at, updated_at
		) VALUES (
			'identity-one', 'synthetic', 'work-one', 'path',
			'Scanned Work Title', 'work-one.cbz', 'matched', 'v1',
			'2026-08-17T00:00:00Z', '2026-08-17T00:00:00Z', '2026-08-17T00:00:00Z'
		)
	`); err != nil {
		t.Fatal(err)
	}

	values := map[string]string{
		"title":    "Local Display Title",
		"creator":  "Synthetic Creator",
		"series":   "Synthetic Series",
		"language": "Synthetic Language",
	}
	for fieldName, fieldValue := range values {
		response := postJSON(t, s, "/api/metadata-overrides", map[string]any{
			"target_type": "work",
			"target_id":   "work-one",
			"field_name":  fieldName,
			"field_value": fieldValue,
		})
		if !boolValue(response["ok"]) {
			t.Fatalf("save %s response = %#v", fieldName, response)
		}
	}

	stored := getJSON(t, s, "/api/metadata-overrides?target_type=work&target_id=work-one")
	overrides, _ := stored["overrides"].(map[string]any)
	if len(overrides) != len(values) {
		t.Fatalf("stored overrides = %#v, want %d fields", overrides, len(values))
	}
	for fieldName, fieldValue := range values {
		entry, _ := overrides[fieldName].(map[string]any)
		if stringValue(entry["field_value"]) != fieldValue {
			t.Fatalf("stored %s = %#v, want %q", fieldName, entry, fieldValue)
		}
	}

	works := getJSON(t, s, "/api/works?limit=10")
	items, _ := works["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("works = %#v, want one item", works)
	}
	work, _ := items[0].(map[string]any)
	if stringValue(work["title"]) != values["title"] || stringValue(work["display_title"]) != values["title"] {
		t.Fatalf("effective title not applied: %#v", work)
	}
	if stringValue(work["metadata_source_title"]) != "Scanned Work Title" {
		t.Fatalf("source title = %q, want preserved scanned title", stringValue(work["metadata_source_title"]))
	}
	if stringValue(work["display_creator"]) != values["creator"] {
		t.Fatalf("display creator = %q, want %q", stringValue(work["display_creator"]), values["creator"])
	}
	visibleOverrides, _ := work["metadata_overrides"].(map[string]any)
	for fieldName := range values {
		if _, ok := visibleOverrides[fieldName]; !ok {
			t.Fatalf("work response omitted %s override: %#v", fieldName, visibleOverrides)
		}
	}

	search := getJSON(t, s, "/api/works?q=Synthetic%20Creator&limit=10")
	if intValue(search["total"]) != 1 {
		t.Fatalf("override search response = %#v, want one result", search)
	}

	var sourceTitle string
	if err := s.db.QueryRow(`SELECT title FROM work_candidates WHERE candidate_id = 'work-one'`).Scan(&sourceTitle); err != nil {
		t.Fatal(err)
	}
	if sourceTitle != "Scanned Work Title" {
		t.Fatalf("source metadata changed to %q", sourceTitle)
	}
	var proposalID, sourceFieldID any
	if err := s.db.QueryRow(`
		SELECT source_proposal_id, source_field_id
		FROM metadata_field_overrides
		WHERE work_identity_id = 'identity-one' AND field_name = 'title'
	`).Scan(&proposalID, &sourceFieldID); err != nil {
		t.Fatal(err)
	}
	if proposalID != nil || sourceFieldID != nil {
		t.Fatalf("public override accepted provenance: proposal=%v field=%v", proposalID, sourceFieldID)
	}

	postJSON(t, s, "/api/metadata-overrides", map[string]any{
		"target_type": "work",
		"target_id":   "work-one",
		"field_name":  "title",
		"field_value": "",
	})
	works = getJSON(t, s, "/api/works?limit=10")
	items, _ = works["items"].([]any)
	work, _ = items[0].(map[string]any)
	if stringValue(work["title"]) != "Scanned Work Title" || stringValue(work["display_title"]) != "Scanned Work Title" {
		t.Fatalf("cleared title did not restore scanned metadata: %#v", work)
	}
	var status string
	if err := s.db.QueryRow(`
		SELECT override_status
		FROM metadata_field_overrides
		WHERE work_identity_id = 'identity-one' AND field_name = 'title'
	`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "reverted" {
		t.Fatalf("cleared override status = %q, want reverted", status)
	}
}

func TestMetadataOverrideLiteRejectsNonStringAndOutOfBoundaryInput(t *testing.T) {
	s, err := NewServer(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`
		INSERT INTO work_candidates (candidate_id, library_key, title, path, relative_path)
		VALUES ('work-one', 'synthetic', 'Scanned Work Title', '/library/work-one.cbz', 'work-one.cbz');
		INSERT INTO work_identities (
			work_identity_id, library_key, current_candidate_id, identity_type,
			display_title, identity_version, first_seen_at, last_seen_at, updated_at
		) VALUES (
			'identity-one', 'synthetic', 'work-one', 'path',
			'Scanned Work Title', 'v1', '2026-08-17T00:00:00Z', '2026-08-17T00:00:00Z', '2026-08-17T00:00:00Z'
		)
	`); err != nil {
		t.Fatal(err)
	}

	valid := map[string]any{
		"target_type": "work",
		"target_id":   "work-one",
		"field_name":  "title",
		"field_value": "Local Title",
	}
	tests := []struct {
		name    string
		payload map[string]any
		status  int
	}{
		{name: "number target type", payload: cloneMetadataOverridePayload(valid, "target_type", 1), status: http.StatusBadRequest},
		{name: "boolean target id", payload: cloneMetadataOverridePayload(valid, "target_id", true), status: http.StatusBadRequest},
		{name: "object field name", payload: cloneMetadataOverridePayload(valid, "field_name", map[string]any{"name": "title"}), status: http.StatusBadRequest},
		{name: "number field value", payload: cloneMetadataOverridePayload(valid, "field_value", 7), status: http.StatusBadRequest},
		{name: "unknown provenance field", payload: metadataOverridePayloadWithExtra(valid, "source_proposal_id", 99), status: http.StatusBadRequest},
		{name: "unsupported target", payload: cloneMetadataOverridePayload(valid, "target_type", "series"), status: http.StatusBadRequest},
		{name: "unsupported field", payload: cloneMetadataOverridePayload(valid, "field_name", "provider"), status: http.StatusBadRequest},
		{name: "control character", payload: cloneMetadataOverridePayload(valid, "field_value", "line one\nline two"), status: http.StatusBadRequest},
		{name: "too long", payload: cloneMetadataOverridePayload(valid, "field_value", strings.Repeat("x", 501)), status: http.StatusBadRequest},
		{name: "missing work", payload: cloneMetadataOverridePayload(valid, "target_id", "missing-work"), status: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if status := metadataOverridePostStatus(t, s, test.payload); status != test.status {
				t.Fatalf("status = %d, want %d", status, test.status)
			}
		})
	}
	if status := metadataOverridePostStatusWithoutIntent(t, s, valid); status != http.StatusForbidden {
		t.Fatalf("write without intent status = %d, want %d", status, http.StatusForbidden)
	}
	var overrideCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM metadata_field_overrides`).Scan(&overrideCount); err != nil {
		t.Fatal(err)
	}
	if overrideCount != 0 {
		t.Fatalf("rejected payloads stored %d override rows", overrideCount)
	}
}

func TestMetadataOverrideLiteFollowsStableIdentityAfterCandidateReplacement(t *testing.T) {
	s, err := NewServer(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`
		INSERT INTO work_candidates (candidate_id, library_key, title, path, relative_path)
		VALUES
			('work-before', 'synthetic', 'Scanned Title Before', '/library/before.cbz', 'before.cbz'),
			('work-after', 'synthetic', 'Scanned Title After', '/library/after.cbz', 'after.cbz');
		INSERT INTO work_identities (
			work_identity_id, library_key, current_candidate_id, identity_type,
			display_title, identity_version, first_seen_at, last_seen_at, updated_at
		) VALUES (
			'identity-stable', 'synthetic', 'work-before', 'path',
			'Scanned Title Before', 'v1', '2026-08-17T00:00:00Z', '2026-08-17T00:00:00Z', '2026-08-17T00:00:00Z'
		)
	`); err != nil {
		t.Fatal(err)
	}
	postJSON(t, s, "/api/metadata-overrides", map[string]any{
		"target_type": "work",
		"target_id":   "work-before",
		"field_name":  "title",
		"field_value": "Stable Local Title",
	})
	if _, err := s.db.Exec(`
		UPDATE work_identities
		SET current_candidate_id = 'work-after', display_title = 'Scanned Title After', updated_at = '2026-08-17T01:00:00Z'
		WHERE work_identity_id = 'identity-stable'
	`); err != nil {
		t.Fatal(err)
	}

	response := getJSON(t, s, "/api/metadata-overrides?target_type=work&target_id=work-after")
	if stringValue(response["work_identity_id"]) != "identity-stable" {
		t.Fatalf("override identity after replacement = %#v", response)
	}
	overrides, _ := response["overrides"].(map[string]any)
	title, _ := overrides["title"].(map[string]any)
	if stringValue(title["field_value"]) != "Stable Local Title" {
		t.Fatalf("title override after replacement = %#v", title)
	}

	works := getJSON(t, s, "/api/works?q=Stable%20Local%20Title&limit=10")
	items, _ := works["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("replacement search items = %#v, want one", items)
	}
	work, _ := items[0].(map[string]any)
	if stringValue(work["candidate_id"]) != "work-after" || stringValue(work["display_title"]) != "Stable Local Title" {
		t.Fatalf("replacement work did not inherit display override: %#v", work)
	}
	var sourceTitle string
	if err := s.db.QueryRow(`SELECT title FROM work_candidates WHERE candidate_id = 'work-after'`).Scan(&sourceTitle); err != nil {
		t.Fatal(err)
	}
	if sourceTitle != "Scanned Title After" {
		t.Fatalf("replacement source title changed to %q", sourceTitle)
	}
}

func cloneMetadataOverridePayload(source map[string]any, key string, value any) map[string]any {
	cloned := make(map[string]any, len(source))
	for sourceKey, sourceValue := range source {
		cloned[sourceKey] = sourceValue
	}
	cloned[key] = value
	return cloned
}

func metadataOverridePayloadWithExtra(source map[string]any, key string, value any) map[string]any {
	cloned := make(map[string]any, len(source)+1)
	for sourceKey, sourceValue := range source {
		cloned[sourceKey] = sourceValue
	}
	cloned[key] = value
	return cloned
}

func metadataOverridePostStatus(t *testing.T, s *Server, payload map[string]any) int {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/metadata-overrides", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(writeIntentHeader, writeIntentValue)
	recorder := httptest.NewRecorder()
	s.Routes().ServeHTTP(recorder, req)
	return recorder.Code
}

func metadataOverridePostStatusWithoutIntent(t *testing.T, s *Server, payload map[string]any) int {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/metadata-overrides", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	s.Routes().ServeHTTP(recorder, req)
	return recorder.Code
}
