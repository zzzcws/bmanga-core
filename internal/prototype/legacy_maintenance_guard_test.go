package prototype

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewServerRejectsActiveLegacyMaintenanceStateWithoutChangingDatabase(t *testing.T) {
	tests := []struct {
		name        string
		setupSQL    string
		verifySQL   string
		verifyValue string
		message     string
	}{
		{
			name: "orphaned deletion candidate",
			setupSQL: `
				CREATE TABLE deletion_candidates (candidate_id TEXT PRIMARY KEY);
				INSERT INTO deletion_candidates VALUES ('orphan-work');
			`,
			verifySQL:   `SELECT candidate_id FROM deletion_candidates`,
			verifyValue: "orphan-work",
			message:     "deletion candidates",
		},
		{
			name: "orphaned active quarantine",
			setupSQL: `
				CREATE TABLE cleanup_quarantine_records (candidate_id TEXT PRIMARY KEY, status TEXT);
				INSERT INTO cleanup_quarantine_records VALUES
					('orphan-work', 'quarantined'),
					('deleted-work', 'final_deleted');
			`,
			verifySQL:   `SELECT CAST(COUNT(*) AS TEXT) FROM cleanup_quarantine_records`,
			verifyValue: "2",
			message:     "quarantined or deleted works",
		},
		{
			name: "orphaned cleanup correction",
			setupSQL: `
				CREATE TABLE local_corrections (
					target_type TEXT, target_id TEXT, correction_type TEXT, correction_value TEXT
				);
				INSERT INTO local_corrections VALUES ('work', 'orphan-work', 'review_status', 'cleanup_confirmed');
			`,
			verifySQL:   `SELECT correction_value FROM local_corrections`,
			verifyValue: "cleanup_confirmed",
			message:     "legacy visibility corrections",
		},
		{
			name: "orphaned delete correction",
			setupSQL: `
				CREATE TABLE local_corrections (
					target_type TEXT, target_id TEXT, correction_type TEXT, correction_value TEXT
				);
				INSERT INTO local_corrections VALUES ('work', 'orphan-work', 'review_status', 'delete_candidate');
			`,
			verifySQL:   `SELECT correction_value FROM local_corrections`,
			verifyValue: "delete_candidate",
			message:     "legacy visibility corrections",
		},
		{
			name: "orphaned keep correction",
			setupSQL: `
				CREATE TABLE local_corrections (
					target_type TEXT, target_id TEXT, correction_type TEXT, correction_value TEXT
				);
				INSERT INTO local_corrections VALUES ('work', 'orphan-work', 'review_status', 'keep');
			`,
			verifySQL:   `SELECT correction_value FROM local_corrections`,
			verifyValue: "keep",
			message:     "legacy visibility corrections",
		},
		{
			name: "orphaned translation maintenance action",
			setupSQL: `
				CREATE TABLE translation_items (candidate_id TEXT, action TEXT);
				INSERT INTO translation_items VALUES ('orphan-work', 'delete_candidate');
			`,
			verifySQL:   `SELECT action FROM translation_items`,
			verifyValue: "delete_candidate",
			message:     "translation maintenance actions",
		},
		{
			name: "enabled author exclusion rule",
			setupSQL: `
				CREATE TABLE author_blacklist_rules (normalized_author TEXT, enabled INTEGER);
				INSERT INTO author_blacklist_rules VALUES ('blocked creator', 1);
			`,
			verifySQL:   `SELECT normalized_author FROM author_blacklist_rules`,
			verifyValue: "blocked creator",
			message:     "enabled author-exclusion rules",
		},
		{
			name: "materialized work browse table",
			setupSQL: `
				CREATE TABLE work_browse (candidate_id TEXT PRIMARY KEY, local_action TEXT);
			`,
			verifySQL:   `SELECT type FROM sqlite_master WHERE name = 'work_browse'`,
			verifyValue: "table",
			message:     "materialized work_browse table",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
			db, err := sql.Open("sqlite", dbPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(test.setupSQL); err != nil {
				_ = db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}

			server, err := NewServer(dbPath)
			if server != nil {
				_ = server.Close()
				t.Fatal("NewServer returned a server for active legacy maintenance state")
			}
			if !errors.Is(err, errUnsupportedLegacyMaintenanceState) {
				t.Fatalf("NewServer error = %v, want unsupported legacy state", err)
			}
			if !strings.Contains(err.Error(), test.message) || !strings.Contains(err.Error(), "no database rows were changed") {
				t.Fatalf("NewServer error = %q, want category %q and non-mutation notice", err, test.message)
			}
			for _, sensitiveValue := range []string{"orphan-work", "deleted-work", "blocked creator"} {
				if strings.Contains(err.Error(), sensitiveValue) {
					t.Fatalf("NewServer error disclosed legacy value %q: %q", sensitiveValue, err)
				}
			}

			db, err = sql.Open("sqlite", dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var got string
			if err := db.QueryRow(test.verifySQL).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != test.verifyValue {
				t.Fatalf("legacy state after rejected startup = %q, want %q", got, test.verifyValue)
			}
			var localSchemaCount int
			if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'reader_profiles'`).Scan(&localSchemaCount); err != nil {
				t.Fatal(err)
			}
			if localSchemaCount != 0 {
				t.Fatalf("rejected startup created %d local schema tables, want 0", localSchemaCount)
			}
		})
	}
}

func TestNewServerAllowsEmptyOrInactiveLegacyStateAndRebuildsLegacyView(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	server, err := NewServer(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		DROP VIEW work_browse;
		CREATE VIEW work_browse AS
		SELECT candidate_id, 'keep' AS local_action
		FROM work_candidates;
		CREATE TABLE deletion_candidates (candidate_id TEXT PRIMARY KEY);
		INSERT INTO deletion_candidates VALUES ('');
		CREATE TABLE cleanup_quarantine_records (candidate_id TEXT PRIMARY KEY, status TEXT);
		INSERT INTO cleanup_quarantine_records VALUES ('', 'quarantined'), ('inactive-work', 'restored');
		ALTER TABLE translation_items ADD COLUMN action TEXT NOT NULL DEFAULT '';
		INSERT INTO translation_items (candidate_id, translation_group, action)
		VALUES ('', 'legacy-source', 'delete_candidate'), ('inactive-work', 'legacy-source', 'keep');
		INSERT INTO local_corrections (
			target_type, target_id, correction_type, correction_value, note, created_at, updated_at
		) VALUES
			('work', '', 'review_status', 'cleanup_confirmed', '', '2026-08-16T00:00:00Z', '2026-08-16T00:00:00Z'),
			('work', 'inactive-work', 'review_status', 'ok', '', '2026-08-16T00:00:00Z', '2026-08-16T00:00:00Z');
		CREATE TABLE author_blacklist_rules (normalized_author TEXT, enabled INTEGER);
		INSERT INTO author_blacklist_rules VALUES ('disabled creator', 0), ('', 1);
	`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	server, err = NewServer(dbPath)
	if err != nil {
		t.Fatalf("empty/inactive legacy state blocked startup: %v", err)
	}
	defer server.Close()

	var objectType, viewSQL string
	if err := server.db.QueryRow(`SELECT type, sql FROM sqlite_master WHERE name = 'work_browse'`).Scan(&objectType, &viewSQL); err != nil {
		t.Fatal(err)
	}
	if objectType != "view" || strings.Contains(viewSQL, "local_action") {
		t.Fatalf("legacy work_browse was not safely rebuilt: type=%q sql=%s", objectType, viewSQL)
	}
	for _, tableName := range []string{
		"deletion_candidates",
		"cleanup_quarantine_records",
		"author_blacklist_rules",
	} {
		var count int
		if err := server.db.QueryRow(`SELECT COUNT(*) FROM "` + tableName + `"`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			t.Fatalf("orphaned legacy rows in %s were removed", tableName)
		}
	}
}

func TestNewServerAllowsFreshDatabase(t *testing.T) {
	server, err := NewServer(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
}

func TestNewServerAllowsPublicTranslationItemsWithoutLegacyAction(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	server, err := NewServer(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	seedCatalogWork(t, server, "public-work", "Public Work", "public-work.cbz", "")
	if _, err := server.db.Exec(`
		INSERT INTO translation_items (candidate_id, translation_group)
		VALUES ('public-work', 'local-source')
	`); err != nil {
		_ = server.Close()
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}

	server, err = NewServer(dbPath)
	if err != nil {
		t.Fatalf("public translation row without legacy action blocked startup: %v", err)
	}
	defer server.Close()
}

func TestLegacyMaintenanceAPIInputsAreRejected(t *testing.T) {
	server, err := NewServer(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	seedCatalogWork(t, server, "public-work", "Public Work", "public-work.cbz", "")

	for _, path := range []string{
		"/api/works?action=keep",
		"/api/shelf?action=delete_candidate",
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		server.Routes().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s returned %d, want %d: %s", path, recorder.Code, http.StatusBadRequest, recorder.Body.String())
		}
	}

	for _, legacyValue := range []string{"cleanup_confirmed", "delete_candidate", "keep"} {
		body, err := json.Marshal(map[string]any{
			"target_type":      "work",
			"target_id":        "public-work",
			"correction_type":  "review_status",
			"correction_value": legacyValue,
		})
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/corrections", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(writeIntentHeader, writeIntentValue)
		server.Routes().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("legacy correction %q returned %d, want %d: %s", legacyValue, recorder.Code, http.StatusBadRequest, recorder.Body.String())
		}
	}

	payload := getJSON(t, server, "/api/works?limit=12")
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("works item count = %d, want 1", len(items))
	}
	item := items[0].(map[string]any)
	for _, removedField := range []string{"local_action", "quarantine_status"} {
		if _, present := item[removedField]; present {
			t.Fatalf("works response retained legacy field %q: %#v", removedField, item)
		}
	}
}
