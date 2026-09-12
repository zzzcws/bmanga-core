package prototype

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func seedStoredSeriesProgress(t *testing.T, s *Server) {
	t.Helper()
	seedSeriesWork(t, s, "summary-work", "Synthetic Chapter", "", "2026-09-01T00:00:00Z")
	if _, err := s.db.Exec(`
 INSERT INTO series_groups(group_id,library_key,series_title,group_path,group_type,candidate_count,confidence,notes)
 VALUES('summary-series','commercial-manga','Synthetic Series','Synthetic','series_candidate','1','1','');
 INSERT INTO series_items(group_id,library_key,series_title,candidate_id,candidate_type,source_kind,title,item_role,sequence_number,sort_key,relative_path,parent_relative_path,page_file_count,confidence)
 VALUES('summary-series','commercial-manga','Synthetic Series','summary-work','manga','archive','Synthetic Chapter','chapter','3','003','Synthetic/03.cbz','Synthetic','12','1');
 `); err != nil {
		t.Fatal(err)
	}
}

func TestSeriesProgressStoredSummaryDoesNotDiscoverPages(t *testing.T) {
	s := newCatalogTestServer(t)
	defer s.Close()
	seedStoredSeriesProgress(t, s)
	if _, err := s.db.Exec(`DELETE FROM page_manifests;
 UPDATE work_candidates SET extension='.cbz',path=? WHERE candidate_id='summary-work'`, filepath.Join(t.TempDir(), "missing-synthetic.cbz")); err != nil {
		t.Fatal(err)
	}
	before, err := s.query("SELECT * FROM reading_progress")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/series-progress?id=summary-series&manifest=stored", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	result := getJSON(t, s, "/api/series-progress?id=summary-series&manifest=stored")
	p := result["progress"].(map[string]any)
	if p["candidate_id"] != "summary-work" || intValue(p["index"]) != 1 || intValue(p["count"]) != 12 || p["manifest_hash"] != "hash-summary-work" {
		t.Fatalf("saved snapshot changed: %v", p)
	}
	for _, phase := range []string{"progressQuery;dur=", "progressManifest;dur=", "app;dur="} {
		if !strings.Contains(rec.Header().Get("Server-Timing"), phase) {
			t.Errorf("missing %s", phase)
		}
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("saved position must remain fresh")
	}
	after, err := s.query("SELECT * FROM reading_progress")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("summary mutated saved position: %v", err)
	}
	manifests, err := s.query("SELECT * FROM page_manifests")
	if err != nil || len(manifests) != 0 {
		t.Fatalf("summary discovered pages: %v %v", manifests, err)
	}
	// The old endpoint still performs strict archive validation and fails for
	// this deliberately absent source; the new summary cannot mask that failure.
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/series-progress?id=summary-series", nil))
	if rec.Code == http.StatusOK {
		t.Fatal("strict manifest discovery was bypassed")
	}
}

func TestSeriesProgressStoredSummaryStaleEmptyInvalidAndCancelled(t *testing.T) {
	s := newCatalogTestServer(t)
	defer s.Close()
	seedStoredSeriesProgress(t, s)
	if _, err := s.db.Exec("UPDATE page_manifests SET manifest_hash='current-hash'"); err != nil {
		t.Fatal(err)
	}
	p := getJSON(t, s, "/api/series-progress?id=summary-series&manifest=stored")["progress"].(map[string]any)
	if p["progress_status"] != "manifest_stale" || p["manifest_hash"] != "hash-summary-work" {
		t.Fatalf("lost stale marker: %v", p)
	}
	if result := getJSON(t, s, "/api/series-progress?id=unread&manifest=stored"); result["progress"] != nil {
		t.Fatalf("unread must be null: %v", result)
	}
	for _, target := range []string{"/api/series-progress?id=summary-series&manifest=unknown", "/api/series-progress?manifest=stored"} {
		rec := httptest.NewRecorder()
		s.handleSeriesProgressGet(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid mode/id returned %d", rec.Code)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.queryContext(ctx, "SELECT 1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("query ignored cancellation: %v", err)
	}
	if _, err := s.getCurrentManifestContext(ctx, "summary-work"); !errors.Is(err, context.Canceled) {
		t.Fatalf("manifest query ignored cancellation: %v", err)
	}
	rec := httptest.NewRecorder()
	s.handleSeriesProgressGet(rec, httptest.NewRequest(http.MethodGet, "/api/series-progress?id=summary-series&manifest=stored", nil).WithContext(ctx))
	if rec.Code == http.StatusOK {
		t.Fatal("cancelled summary succeeded")
	}
}

func TestSeriesProgressStoredSummaryProjectsOnlySameIdentityInSeries(t *testing.T) {
	cases := []struct {
		name, oldGroup, currentGroup, want          string
		removeOld, removeCurrent, differentIdentity bool
	}{
		{"current member", "summary-series", "summary-series", "current-work", false, false, false},
		{"old member moved", "other-series", "summary-series", "current-work", false, false, false},
		{"current outside series", "summary-series", "other-series", "summary-work", false, false, false},
		{"saved candidate removed", "summary-series", "summary-series", "", true, false, false},
		{"current removed old valid", "summary-series", "summary-series", "summary-work", false, true, false},
		{"both outside", "other-series", "other-series", "", false, false, false},
		{"unrelated replacement", "other-series", "summary-series", "", false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newCatalogTestServer(t)
			defer s.Close()
			seedStoredSeriesProgress(t, s)
			seedCatalogWork(t, s, "current-work", "Current Synthetic Chapter", "Synthetic/03-new.bin", "")
			if _, err := s.db.Exec(`
    UPDATE work_candidates SET library_key='commercial-manga',candidate_type='manga',extension='.bin' WHERE candidate_id='current-work';
				UPDATE series_items SET group_id=?1 WHERE candidate_id='summary-work';
				INSERT INTO series_items(group_id,candidate_id,title,item_role,sequence_number,sort_key) VALUES(?2,'current-work','Current Synthetic Chapter','chapter','3','003');
   `, tc.oldGroup, tc.currentGroup); err != nil {
				t.Fatal(err)
			}
			if !tc.differentIdentity {
				if _, err := s.db.Exec("DELETE FROM work_identities WHERE work_identity_id='identity-current-work'; UPDATE work_identities SET current_candidate_id='current-work' WHERE work_identity_id='identity-summary-work'"); err != nil {
					t.Fatal(err)
				}
			}
			if tc.removeOld {
				if _, err := s.db.Exec("DELETE FROM work_candidates WHERE candidate_id='summary-work'"); err != nil {
					t.Fatal(err)
				}
			}
			if tc.removeCurrent {
				if _, err := s.db.Exec("DELETE FROM work_candidates WHERE candidate_id='current-work'"); err != nil {
					t.Fatal(err)
				}
			}
			before, err := s.query("SELECT * FROM reading_progress")
			if err != nil {
				t.Fatal(err)
			}
			result := getJSON(t, s, "/api/series-progress?id=summary-series&manifest=stored")
			if tc.want == "" {
				if result["progress"] != nil {
					t.Fatalf("unrelated/removed progress leaked: %v", result)
				}
			} else {
				p, ok := result["progress"].(map[string]any)
				if !ok {
					visible, _ := s.query("SELECT candidate_id,work_identity_id FROM work_browse")
					t.Fatalf("missing progress: %v; visible=%v", result, visible)
				}
				if p["candidate_id"] != tc.want || p["work_identity_id"] != "identity-summary-work" || p["page_manifest_id"] != "manifest-summary-work" || intValue(p["index"]) != 1 || intValue(p["count"]) != 12 {
					t.Fatalf("projection changed identity/snapshot: %v", p)
				}
				// Projection is additive: callers that omit manifest=stored retain the
				// original saved candidate response rather than silently changing targets.
				legacy := getJSON(t, s, "/api/series-progress?id=summary-series")["progress"].(map[string]any)
				if legacy["candidate_id"] != "summary-work" {
					t.Fatalf("legacy contract changed: %v", legacy)
				}
			}
			after, err := s.query("SELECT * FROM reading_progress")
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("projection wrote progress: %v", err)
			}
		})
	}
}

// A minimal legacy schema has no reader positioning extensions or modern
// catalog metadata. SQL NULL values and missing optional fields are not 500s.
func TestSeriesProgressStoredSummaryLegacyNullableSchema(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{db: db}
	if _, err = db.Exec(`
 CREATE TABLE work_browse(candidate_id TEXT,title TEXT);
 CREATE TABLE work_identities(work_identity_id TEXT,current_candidate_id TEXT);
 CREATE TABLE series_items(group_id TEXT,candidate_id TEXT,sort_key TEXT,sequence_number TEXT);
 CREATE TABLE reading_progress(reader_profile_key TEXT,work_identity_id TEXT,candidate_id TEXT,last_page_index INTEGER,page_count_snapshot INTEGER,completed INTEGER,last_read_at TEXT,updated_at TEXT);
 CREATE TABLE page_manifests(page_manifest_id TEXT,work_identity_id TEXT,candidate_id TEXT,manifest_hash TEXT,page_count INTEGER,manifest_status TEXT,builder_version TEXT,built_at TEXT);
 INSERT INTO work_browse VALUES('legacy-work',NULL);
 INSERT INTO work_identities VALUES('legacy-identity','legacy-work');
 INSERT INTO series_items VALUES('legacy-series','legacy-work',NULL,NULL);
 INSERT INTO reading_progress VALUES('default','legacy-identity','legacy-work',NULL,NULL,NULL,NULL,NULL);
 `); err != nil {
		t.Fatal(err)
	}
	for _, empty := range []bool{false, true} {
		if empty {
			if _, err := db.Exec("DELETE FROM reading_progress"); err != nil {
				t.Fatal(err)
			}
		}
		rec := httptest.NewRecorder()
		s.handleSeriesProgressGet(rec, httptest.NewRequest(http.MethodGet, "/api/series-progress?id=legacy-series&manifest=stored", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("nullable/empty schema: %d %s", rec.Code, rec.Body.String())
		}
		if empty && !strings.Contains(rec.Body.String(), `"progress":null`) {
			t.Fatalf("empty must be null: %s", rec.Body.String())
		}
	}
}

func TestProgressAutoModePersistsWithoutChangingLegacyDefaults(t *testing.T) {
	for _, mode := range []string{"auto", "", "unknown"} {
		t.Run("mode="+mode, func(t *testing.T) {
			s := newCatalogTestServer(t)
			defer s.Close()
			seedStoredSeriesProgress(t, s)
			if _, err := s.db.Exec("DELETE FROM reading_progress"); err != nil {
				t.Fatal(err)
			}
			response := postJSON(t, s, "/api/progress", map[string]any{
				"candidate_id": "summary-work", "page_manifest_id": "manifest-summary-work", "manifest_hash": "hash-summary-work",
				"index": 4, "count": 12, "reader_fit_mode": mode, "reader_split_panel": 1,
				"stage_scroll_top": 321, "stage_scroll_left": 12,
			})
			wantMode, wantTop, wantLeft := "", 0, 0
			if mode == "auto" {
				wantMode, wantTop, wantLeft = "auto", 321, 12
			}
			for _, payload := range []map[string]any{response, getJSON(t, s, "/api/progress?id=summary-work"), getJSON(t, s, "/api/series-progress?id=summary-series&manifest=stored")} {
				p := payload["progress"].(map[string]any)
				if p["reader_fit_mode"] != wantMode || intValue(p["stage_scroll_top"]) != wantTop || intValue(p["stage_scroll_left"]) != wantLeft || intValue(p["reader_split_panel"]) != 0 {
					t.Fatalf("auto/legacy display state did not round trip: %v", p)
				}
			}
		})
	}
}

func TestSeriesProgressStoredSummaryRanksCurrentDirectorySequence(t *testing.T) {
	s := newCatalogTestServer(t)
	defer s.Close()
	seedStoredSeriesProgress(t, s)
	seedCatalogWork(t, s, "current-work", "Current Chapter", "Synthetic/current.bin", "")
	seedSeriesWork(t, s, "later-work", "Later Chapter", "", "2026-09-01T00:00:00Z")
	if _, err := s.db.Exec(`
		UPDATE series_items SET sequence_number='99',sort_key='999',group_id='other-series' WHERE candidate_id='summary-work';
		INSERT INTO series_items(group_id,candidate_id,title,item_role,sequence_number,sort_key) VALUES
		('summary-series','current-work','Current Chapter','chapter','1','001'),
		('summary-series','later-work','Later Chapter','chapter','2','002');
		DELETE FROM work_identities WHERE work_identity_id='identity-current-work';
		UPDATE work_identities SET current_candidate_id='current-work' WHERE work_identity_id='identity-summary-work';
	`); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"", "&manifest=stored"} {
		p := getJSON(t, s, "/api/series-progress?id=summary-series"+mode)["progress"].(map[string]any)
		want := "summary-work"
		if mode != "" {
			want = "later-work"
		}
		if p["candidate_id"] != want {
			t.Fatalf("mode=%q ranked the saved instead of current directory position: %v", mode, p)
		}
	}
}
