package prototype

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCleanItemTitle(t *testing.T) {
	got := cleanItemTitle("001", "unknown", "1", "Synthetic Chapter Series 第9001-9167话")
	if got != "第1话" {
		t.Fatalf("cleaned chapter title = %q, want 第1话", got)
	}
	got = cleanItemTitle("9003_第9001卷", "volume", "9001", "")
	if got != "第9001卷" {
		t.Fatalf("cleaned volume title = %q, want 第9001卷", got)
	}
}

func TestStaticRelativePathSupportsV2EntryAndClientRoutes(t *testing.T) {
	cases := map[string]string{
		"/v2":                  "v2/index.html",
		"/v2/":                 "v2/index.html",
		"/v2/library":          "v2/index.html",
		"/v2/series/abc123":    "v2/index.html",
		"/v2/assets/app.js":    "v2/assets/app.js",
		"/v2/assets/theme.css": "v2/assets/theme.css",
	}
	for input, want := range cases {
		if got := staticRelativePath(input); got != want {
			t.Errorf("staticRelativePath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRootRedirectsToCanonicalV2AndPreservesQueries(t *testing.T) {
	server := &Server{}
	cases := []struct {
		name     string
		method   string
		target   string
		location string
	}{
		{name: "get", method: http.MethodGet, target: "/", location: "/v2/"},
		{name: "head", method: http.MethodHead, target: "/", location: "/v2/"},
		{name: "query", method: http.MethodGet, target: "/?view=library&kind=doujin", location: "/v2/?view=library&kind=doujin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, nil)
			rec := httptest.NewRecorder()
			server.handleRoot(rec, req)
			if rec.Code != http.StatusTemporaryRedirect {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusTemporaryRedirect)
			}
			if got := rec.Header().Get("Location"); got != tc.location {
				t.Fatalf("location = %q, want %q", got, tc.location)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("cache control = %q, want no-store", got)
			}
		})
	}
}

func TestTitleSeriesStemRequiresSequenceMarker(t *testing.T) {
	got := titleSeriesStem("Synthetic Orbit Chronicle 第9009话 [Synthetic汉化Alpha]")
	if got != "Synthetic Orbit Chronicle" {
		t.Fatalf("series stem = %q, want Synthetic Orbit Chronicle", got)
	}
	got = titleSeriesStem("Synthetic Orbit Chronicle 第9009巻 [Synthetic翻訳Beta]")
	if got != "Synthetic Orbit Chronicle" {
		t.Fatalf("Japanese volume series stem = %q, want Synthetic Orbit Chronicle", got)
	}
	got = titleSeriesStem("Synthetic Orbit Chronicle Special Edition (Synthetic 2024 No. 80)")
	if got != "" {
		t.Fatalf("series stem without sequence marker = %q, want empty", got)
	}
}

func TestSameOriginWriteGuardRequiresSourceOrIntent(t *testing.T) {
	s := newTestServer(t)
	defer s.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/health", nil)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST without source or intent returned %d, want %d", rec.Code, http.StatusForbidden)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/health", nil)
	req.Header.Set(writeIntentHeader, writeIntentValue)
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST with write intent returned %d, want method guard %d", rec.Code, http.StatusMethodNotAllowed)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/health", nil)
	req.Host = "127.0.0.1:8765"
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set(writeIntentHeader, writeIntentValue)
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST with hostile origin returned %d, want %d", rec.Code, http.StatusForbidden)
	}

	t.Setenv("BMANGA_ALLOWED_ORIGINS", "https://reader.example.invalid:8443")
	req = httptest.NewRequest(http.MethodPost, "/api/health", nil)
	req.Host = "192.0.2.6:8765"
	req.Header.Set("Origin", "https://reader.example.invalid:8443")
	req.Header.Set(writeIntentHeader, writeIntentValue)
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST from configured public origin returned %d, want method guard %d", rec.Code, http.StatusMethodNotAllowed)
	}
	t.Setenv("BMANGA_ALLOWED_ORIGINS", "")

	req = httptest.NewRequest(http.MethodPost, "/api/health", nil)
	req.Host = "127.0.0.1:8765"
	req.Header.Set("Origin", "http://127.0.0.1:8765")
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST with same-origin source but no write intent returned %d, want %d", rec.Code, http.StatusForbidden)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/health", nil)
	req.Host = "127.0.0.1:8765"
	req.Header.Set("Origin", "http://127.0.0.1:8765")
	req.Header.Set(writeIntentHeader, writeIntentValue)
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST with same-origin source and write intent returned %d, want method guard %d", rec.Code, http.StatusMethodNotAllowed)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/health", nil)
	req.Host = "127.0.0.1:8765"
	req.Header.Set("Origin", "http://127.0.0.1:8765")
	req.AddCookie(&http.Cookie{Name: writeTokenCookie, Value: "token-a"})
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST with write token cookie but no token header returned %d, want %d", rec.Code, http.StatusForbidden)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/health", nil)
	req.Host = "127.0.0.1:8765"
	req.Header.Set("Origin", "http://127.0.0.1:8765")
	req.Header.Set(writeTokenHeader, "token-a")
	req.AddCookie(&http.Cookie{Name: writeTokenCookie, Value: "token-a"})
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST with matching write token returned %d, want method guard %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestDashboardAgainstLocalDB(t *testing.T) {
	s := newTestServer(t)
	defer s.Close()

	payload := getJSON(t, s, "/api/dashboard")
	totals := payload["totals"].(map[string]any)
	expectedRows, err := s.query(`
		SELECT
			(SELECT COUNT(*) FROM work_candidates) AS works,
			(SELECT COUNT(*) FROM page_counts WHERE page_count_status = 'counted') AS page_counted
	`)
	if err != nil {
		t.Fatal(err)
	}
	expected := firstRow(expectedRows)
	if int(totals["works"].(float64)) != intValue(expected["works"]) {
		t.Fatalf("works = %v, want %v", totals["works"], expected["works"])
	}
	if int(totals["page_counted"].(float64)) != intValue(expected["page_counted"]) {
		t.Fatalf("page_counted = %v, want %v", totals["page_counted"], expected["page_counted"])
	}
}

func TestSeriesDefaultAgainstLocalDB(t *testing.T) {
	s := newTestServer(t)
	defer s.Close()

	payload := getJSON(t, s, "/api/series?limit=12")
	if int(payload["total"].(float64)) != 46 {
		t.Fatalf("total = %v, want 46", payload["total"])
	}
	items := payload["items"].([]any)
	if len(items) != 12 {
		t.Fatalf("len(items) = %d, want 12", len(items))
	}
	first := items[0].(map[string]any)
	if first["group_id"] != "6e76489491be94c44eaabac9" {
		t.Fatalf("first group = %v, want 炎拳 group", first["group_id"])
	}
}

func TestShelfDefaultAgainstLocalDB(t *testing.T) {
	s := newTestServer(t)
	defer s.Close()

	payload := getJSON(t, s, "/api/shelf?limit=12")
	expectedTotal := expectedShelfTotal(t, s)
	if int(payload["total"].(float64)) != expectedTotal {
		t.Fatalf("total = %v, want %d", payload["total"], expectedTotal)
	}
	items := payload["items"].([]any)
	if len(items) != 12 {
		t.Fatalf("len(items) = %d, want 12", len(items))
	}
}

func TestShelfFastPaginationMatchesLegacyMixedOrderAgainstLocalDB(t *testing.T) {
	s := newTestServer(t)
	defer s.Close()

	for _, sortKey := range []string{"added_desc", "title_asc", "pages_desc"} {
		for _, offset := range []int{0, 2448, 30000} {
			t.Run(sortKey+"/offset_"+strconv.Itoa(offset), func(t *testing.T) {
				query := url.Values{"sort": []string{sortKey}}
				expectedTotal, expectedOffset, expectedIDs := legacyShelfPageIDsForTest(t, s, query, 18, offset)
				payload := getJSON(t, s, "/api/shelf?limit=18&offset="+strconv.Itoa(offset)+"&sort="+url.QueryEscape(sortKey))
				if got := int(payload["total"].(float64)); got != expectedTotal {
					t.Fatalf("total = %d, want %d", got, expectedTotal)
				}
				if got := int(payload["offset"].(float64)); got != expectedOffset {
					t.Fatalf("offset = %d, want %d", got, expectedOffset)
				}
				gotIDs := shelfPayloadIDsForTest(payload)
				if strings.Join(gotIDs, ",") != strings.Join(expectedIDs, ",") {
					t.Fatalf("mixed page IDs =\n got: %v\nwant: %v", gotIDs, expectedIDs)
				}
			})
		}
	}
}

func TestShelfFastPaginationMatchesLegacyFilteredScopesAgainstLocalDB(t *testing.T) {
	s := newTestServer(t)
	defer s.Close()

	cases := []struct {
		name   string
		query  url.Values
		offset int
	}{
		{name: "doujin_library", query: url.Values{"library": []string{"doujin-lanraragi"}, "sort": []string{"added_desc"}}, offset: 2448},
		{name: "image_folder_source", query: url.Values{"source": []string{"image_folder"}, "sort": []string{"title_asc"}}, offset: 90},
		{name: "counted_pages", query: url.Values{"pageStatus": []string{"counted"}, "sort": []string{"pages_desc"}}, offset: 36},
		{name: "search", query: url.Values{"q": []string{"炎拳"}, "sort": []string{"added_desc"}}, offset: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectedTotal, expectedOffset, expectedIDs := legacyShelfPageIDsForTest(t, s, tc.query, 18, tc.offset)
			requestQuery := cloneURLValuesForTest(tc.query)
			requestQuery.Set("limit", "18")
			requestQuery.Set("offset", strconv.Itoa(tc.offset))
			payload := getJSON(t, s, "/api/shelf?"+requestQuery.Encode())
			if got := int(payload["total"].(float64)); got != expectedTotal {
				t.Fatalf("total = %d, want %d", got, expectedTotal)
			}
			if got := int(payload["offset"].(float64)); got != expectedOffset {
				t.Fatalf("offset = %d, want %d", got, expectedOffset)
			}
			gotIDs := shelfPayloadIDsForTest(payload)
			if strings.Join(gotIDs, ",") != strings.Join(expectedIDs, ",") {
				t.Fatalf("filtered page IDs =\n got: %v\nwant: %v", gotIDs, expectedIDs)
			}
		})
	}
}

func TestShelfFastPaginationClampsOffsetAndPreservesItemContractAgainstLocalDB(t *testing.T) {
	s := newTestServer(t)
	defer s.Close()

	first := getJSON(t, s, "/api/shelf?limit=18&offset=0&sort=added_desc")
	for _, raw := range first["items"].([]any) {
		item := raw.(map[string]any)
		switch stringValue(item["shelf_type"]) {
		case "series":
			if stringValue(item["group_id"]) == "" || stringValue(item["display_title"]) == "" {
				t.Fatalf("series item lost detail contract: %#v", item)
			}
		case "work":
			if stringValue(item["candidate_id"]) == "" || stringValue(item["display_title"]) == "" {
				t.Fatalf("work item lost detail contract: %#v", item)
			}
		default:
			t.Fatalf("unexpected shelf_type: %#v", item)
		}
	}
	total := int(first["total"].(float64))
	beyond := getJSON(t, s, "/api/shelf?limit=18&offset="+strconv.Itoa(total+180)+"&sort=added_desc")
	if got := int(beyond["offset"].(float64)); got != total {
		t.Fatalf("clamped offset = %d, want total %d", got, total)
	}
	if got := len(beyond["items"].([]any)); got != 0 {
		t.Fatalf("items beyond total = %d, want 0", got)
	}
}

func TestShelfFastPaginationPreservesLegacyJSONPayloadAgainstLocalDB(t *testing.T) {
	s := newTestServer(t)
	defer s.Close()

	cases := []struct {
		sortKey string
		offset  int
	}{
		{sortKey: "added_desc", offset: 0},
		{sortKey: "pages_desc", offset: 0},
		{sortKey: "title_asc", offset: 30000},
	}
	for _, tc := range cases {
		t.Run(tc.sortKey+"/offset_"+strconv.Itoa(tc.offset), func(t *testing.T) {
			query := url.Values{"sort": []string{tc.sortKey}}
			expected := legacyShelfPayloadForTest(t, s, query, 18, tc.offset)
			actual := getJSON(t, s, "/api/shelf?limit=18&offset="+strconv.Itoa(tc.offset)+"&sort="+url.QueryEscape(tc.sortKey))
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("JSON payload changed for %s offset %d", tc.sortKey, tc.offset)
			}
		})
	}
}

func TestShelfFastPaginationExposesBoundedPageTimingStages(t *testing.T) {
	s := newTestServer(t)
	defer s.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/shelf?limit=18&offset=2448&sort=added_desc", nil)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("shelf returned %d: %s", rec.Code, rec.Body.String())
	}
	timing := rec.Header().Get("Server-Timing")
	for _, phase := range []string{"shelfSeriesLite;dur=", "shelfWorkWindow;dur=", "shelfMerge;dur=", "shelfSeriesDetails;dur=", "shelfWorkDetails;dur=", "shelfAssemble;dur="} {
		if !strings.Contains(timing, phase) {
			t.Fatalf("Server-Timing = %q, missing %q", timing, phase)
		}
	}
}

func TestReaderRequestLoggingPolicyAlwaysIncludesServerErrors(t *testing.T) {
	if !readerTimingTrackedPath("/api/progress") {
		t.Fatal("/api/progress must remain covered by request timing and error logging")
	}
	for _, test := range []struct {
		name      string
		status    int
		duration  time.Duration
		threshold time.Duration
		want      bool
	}{
		{name: "fast server error", status: http.StatusInternalServerError, duration: time.Millisecond, threshold: 10 * time.Second, want: true},
		{name: "server error with disabled slow logging", status: http.StatusServiceUnavailable, duration: 0, threshold: 0, want: true},
		{name: "slow success", status: http.StatusOK, duration: 2 * time.Second, threshold: time.Second, want: true},
		{name: "fast success", status: http.StatusOK, duration: time.Millisecond, threshold: time.Second, want: false},
		{name: "success with disabled slow logging", status: http.StatusOK, duration: time.Hour, threshold: 0, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldLogReaderRequest(test.status, test.duration, test.threshold); got != test.want {
				t.Fatalf("shouldLogReaderRequest(%d, %s, %s) = %v, want %v", test.status, test.duration, test.threshold, got, test.want)
			}
		})
	}
}

func TestShelfFastSortPerformanceIndexesAgainstLocalDB(t *testing.T) {
	s := newTestServer(t)
	defer s.Close()

	for _, indexName := range []string{
		"idx_work_candidates_shelf_title_order",
		"idx_page_counts_shelf_pages_desc_v2",
	} {
		var found string
		if err := s.db.QueryRow(`
			SELECT name
			FROM sqlite_master
			WHERE type = 'index' AND name = ?
		`, indexName).Scan(&found); err != nil {
			t.Fatalf("performance index %s unavailable: %v", indexName, err)
		}
	}
}

func TestShelfFilteredFastSortPlansKeepIndexedDriverAgainstLocalDB(t *testing.T) {
	s := newTestServer(t)
	defer s.Close()

	baseFilters := []string{
		"(wc.candidate_type = 'doujin' OR NOT EXISTS (SELECT 1 FROM series_items si WHERE si.candidate_id = wc.candidate_id))",
		"wc.library_key = ?",
	}
	assertDriverPlan := func(t *testing.T, rows []map[string]any, driverIndex, lookupIndex string) {
		t.Helper()
		driverAt := -1
		lookupAt := -1
		for index, row := range rows {
			detail := stringValue(row["detail"])
			if strings.Contains(detail, driverIndex) && driverAt < 0 {
				driverAt = index
			}
			if strings.Contains(detail, lookupIndex) && lookupAt < 0 {
				lookupAt = index
			}
			if strings.Contains(detail, "USE TEMP B-TREE FOR ORDER BY") {
				t.Fatalf("filtered fast sort regressed to a full ORDER BY sort: %v", rows)
			}
		}
		if driverAt < 0 || lookupAt < 0 || driverAt >= lookupAt {
			t.Fatalf("driver index %s must precede lookup index %s: %v", driverIndex, lookupIndex, rows)
		}
	}

	t.Run("added", func(t *testing.T) {
		_, filters := fastShelfKeepFilterPlan(baseFilters, 2448)
		filters = mergeWhereFilters([]string{
			"sft_added.target_type = 'work'",
			"sft_added.status = 'ok'",
			"NULLIF(sft_added.source_created_utc, '') IS NOT NULL",
		}, filters)
		rows, err := s.query(`
			EXPLAIN QUERY PLAN
			SELECT wc.candidate_id
			`+fastShelfAddedKeepFromSQL()+`
			`+whereClause(filters)+`
			ORDER BY sft_added.source_created_utc DESC,
				wc.title COLLATE NOCASE,
				wc.relative_path COLLATE NOCASE
			LIMIT ? OFFSET ?
		`, "doujin-lanraragi", 64, 2448)
		if err != nil {
			t.Fatal(err)
		}
		assertDriverPlan(t, rows, "idx_source_filesystem_times_work_added", "idx_work_candidates_candidate_id")
	})

	t.Run("pages", func(t *testing.T) {
		_, filters := fastShelfKeepFilterPlan(baseFilters, 2448)
		rows, err := s.query(`
			EXPLAIN QUERY PLAN
			SELECT wc.candidate_id
			`+fastShelfPagesDescKeepFromSQL()+`
			`+whereClause(filters)+`
			ORDER BY CAST(COALESCE(pc.readable_page_count, 0) AS INTEGER) DESC,
				wc.title COLLATE NOCASE,
				wc.relative_path COLLATE NOCASE
			LIMIT ? OFFSET ?
		`, "doujin-lanraragi", 64, 2448)
		if err != nil {
			t.Fatal(err)
		}
		assertDriverPlan(t, rows, "idx_page_counts_shelf_pages_desc_v2", "idx_work_candidates_candidate_id")
	})
}

func TestShelfFastPageCountCoverageGuardRejectsBalancedOrphans(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE work_candidates (candidate_id TEXT);
		CREATE TABLE page_counts (candidate_id TEXT);
		CREATE INDEX idx_page_counts_candidate_id ON page_counts (candidate_id);
		INSERT INTO work_candidates (candidate_id) VALUES ('work-a'), ('work-b');
		INSERT INTO page_counts (candidate_id) VALUES ('work-a'), ('orphan');
	`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db}
	if s.workPageCountCacheComplete() {
		t.Fatal("page-count coverage accepted one orphan balanced by one missing work")
	}
	if _, err := db.Exec(`
		DELETE FROM page_counts WHERE candidate_id = 'orphan';
		INSERT INTO page_counts (candidate_id) VALUES ('work-b');
	`); err != nil {
		t.Fatal(err)
	}
	if !s.workPageCountCacheComplete() {
		t.Fatal("page-count coverage rejected an exact one-to-one cache")
	}
}

func TestDuplicatePairStatusIsLocalOnly(t *testing.T) {
	s := newCatalogTestServer(t)
	defer s.Close()
	seedCatalogWork(t, s, "duplicate-left", "Duplicate Left", "duplicate-left.cbz", "")
	seedCatalogWork(t, s, "duplicate-right", "Duplicate Right", "duplicate-right.cbz", "")

	response := postJSON(t, s, "/api/duplicate-pair/status", map[string]any{
		"left":       "duplicate-left",
		"right":      "duplicate-right",
		"status":     "version",
		"preference": "prefer_right",
	})
	if response["local_status_written"] != true || response["applies_actions"] != false || response["source_library_written"] != false {
		t.Fatalf("duplicate pair response escaped local-only scope: %#v", response)
	}
	candidate, _ := response["duplicate_candidate"].(map[string]any)
	if stringValue(candidate["status"]) != "version" {
		t.Fatalf("duplicate candidate = %#v, want version", candidate)
	}
	preference, _ := candidate["local_preference"].(map[string]any)
	if stringValue(preference["key"]) != "prefer_right" {
		t.Fatalf("local preference = %#v, want prefer_right", preference)
	}

	var status string
	if err := s.db.QueryRow(`
		SELECT status
		FROM duplicate_candidates
		WHERE candidate_id = ?
	`, duplicatePairCandidateID("duplicate-left", "duplicate-right")).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "version" {
		t.Fatalf("stored local status = %q, want version", status)
	}

	body, err := json.Marshal(map[string]any{
		"left":   "duplicate-left",
		"right":  "duplicate-right",
		"status": "confirmed_same",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/duplicate-pair/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(writeIntentHeader, writeIntentValue)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("confirmed_same returned %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestNewServerEnsuresLocalSQLiteTables(t *testing.T) {
	dbDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := newServerWithoutCatalogForTest(filepath.Join(dbDir, "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for _, table := range []string{
		"reader_profiles",
		"local_corrections",
		"work_user_marks",
		"series_user_marks",
		"user_mark_field_clocks",
		"reading_progress",
		"reading_progress_resets",
		"page_manifests",
		"page_manifest_items",
	} {
		var count int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
			table,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("table %s count = %d, want 1", table, count)
		}
	}
	for _, removedTable := range []string{
		"local_task_runs",
		"quality_findings",
		"review_candidates",
		"cover_visual_embeddings",
		"metadata_scrape_results",
	} {
		var count int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
			removedTable,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("removed table %s unexpectedly exists", removedTable)
		}
	}

	var profileCount int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM reader_profiles WHERE key = 'default'").Scan(&profileCount); err != nil {
		t.Fatal(err)
	}
	if profileCount != 1 {
		t.Fatalf("default reader profile count = %d, want 1", profileCount)
	}
}

func TestEnsurePageManifestSerializesConcurrentCreation(t *testing.T) {
	t.Setenv("BMANGA_SQLITE_MAX_OPEN_CONNS", "16")
	s, err := newServerWithoutCatalogForTest(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	work := map[string]any{
		"candidate_id":     "work-concurrent-manifest",
		"work_identity_id": "identity-concurrent-manifest",
		"library_key":      "test-library",
		"source_kind":      "archive",
		"path":             filepath.Join(t.TempDir(), "work-concurrent-manifest.cbz"),
		"relative_path":    "work-concurrent-manifest.cbz",
	}
	rows := []map[string]any{
		{
			"page_index":        0,
			"library_key":       "test-library",
			"path":              work["path"],
			"relative_path":     "001.jpg",
			"source_inner_path": "001.jpg",
			"extension":         ".jpg",
			"mime_type":         "image/jpeg",
			"size_bytes":        101,
		},
		{
			"page_index":        1,
			"library_key":       "test-library",
			"path":              work["path"],
			"relative_path":     "002.jpg",
			"source_inner_path": "002.jpg",
			"extension":         ".jpg",
			"mime_type":         "image/jpeg",
			"size_bytes":        102,
		},
		{
			"page_index":        2,
			"library_key":       "test-library",
			"path":              work["path"],
			"relative_path":     "003.jpg",
			"source_inner_path": "003.jpg",
			"extension":         ".jpg",
			"mime_type":         "image/jpeg",
			"size_bytes":        103,
		},
	}

	type manifestResult struct {
		manifest map[string]any
		err      error
	}
	const workers = 24
	start := make(chan struct{})
	results := make(chan manifestResult, workers)
	for index := 0; index < workers; index++ {
		go func() {
			<-start
			manifest, err := s.ensurePageManifest(context.Background(), work, rows)
			results <- manifestResult{manifest: manifest, err: err}
		}()
	}
	close(start)

	manifestID := ""
	for index := 0; index < workers; index++ {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent ensurePageManifest returned error: %v", result.err)
		}
		gotID := stringValue(result.manifest["page_manifest_id"])
		if gotID == "" {
			t.Fatal("concurrent ensurePageManifest returned an empty manifest id")
		}
		if manifestID == "" {
			manifestID = gotID
		} else if gotID != manifestID {
			t.Fatalf("concurrent manifest id = %q, want %q", gotID, manifestID)
		}
	}

	var manifestCount int
	if err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM page_manifests
		WHERE work_identity_id = ?
	`, work["work_identity_id"]).Scan(&manifestCount); err != nil {
		t.Fatal(err)
	}
	if manifestCount != 1 {
		t.Fatalf("manifest count = %d, want 1", manifestCount)
	}

	var itemCount int
	if err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM page_manifest_items
		WHERE page_manifest_id = ?
	`, manifestID).Scan(&itemCount); err != nil {
		t.Fatal(err)
	}
	if itemCount != len(rows) {
		t.Fatalf("manifest item count = %d, want %d", itemCount, len(rows))
	}
}

func TestPageRowsAndManifestSerializesArchiveDiscovery(t *testing.T) {
	t.Setenv("BMANGA_SQLITE_MAX_OPEN_CONNS", "16")
	tempDir := t.TempDir()
	archivePath := filepath.Join(tempDir, "concurrent-book.cbz")
	if err := writeTestCoverArchive(archivePath, "001.jpg"); err != nil {
		t.Fatal(err)
	}
	s, err := newServerWithoutCatalogForTest(filepath.Join(tempDir, "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`
		CREATE TABLE libraries (key TEXT PRIMARY KEY, root_path TEXT NOT NULL);
		CREATE TABLE work_identities (work_identity_id TEXT PRIMARY KEY, current_candidate_id TEXT);
		INSERT INTO libraries (key, root_path) VALUES ('test-library', ?);
		INSERT INTO work_identities (work_identity_id, current_candidate_id)
		VALUES ('identity-concurrent-discovery', 'work-concurrent-discovery');
	`, tempDir); err != nil {
		t.Fatal(err)
	}
	work := map[string]any{
		"candidate_id":     "work-concurrent-discovery",
		"work_identity_id": "identity-concurrent-discovery",
		"library_key":      "test-library",
		"source_kind":      "archive",
		"extension":        ".cbz",
		"path":             archivePath,
		"relative_path":    "concurrent-book.cbz",
	}

	type result struct {
		count      int
		manifestID string
		err        error
	}
	const workers = 16
	start := make(chan struct{})
	results := make(chan result, workers)
	for index := 0; index < workers; index++ {
		go func() {
			<-start
			rows, manifest, err := s.pageRowsAndManifestForWork(context.Background(), work, true)
			results <- result{count: len(rows), manifestID: stringValue(manifest["page_manifest_id"]), err: err}
		}()
	}
	close(start)

	manifestID := ""
	for index := 0; index < workers; index++ {
		got := <-results
		if got.err != nil {
			t.Fatalf("concurrent page discovery returned error: %v", got.err)
		}
		if got.count != 1 || got.manifestID == "" {
			t.Fatalf("concurrent page discovery = count %d manifest %q", got.count, got.manifestID)
		}
		if manifestID == "" {
			manifestID = got.manifestID
		} else if got.manifestID != manifestID {
			t.Fatalf("concurrent manifest id = %q, want %q", got.manifestID, manifestID)
		}
	}

	var manifestCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM page_manifests WHERE work_identity_id = ?`, work["work_identity_id"]).Scan(&manifestCount); err != nil {
		t.Fatal(err)
	}
	if manifestCount != 1 {
		t.Fatalf("manifest count = %d, want 1", manifestCount)
	}
	s.renderLockMu.Lock()
	remainingLocks := len(s.renderLocks)
	s.renderLockMu.Unlock()
	if remainingLocks != 0 {
		t.Fatalf("render lock count = %d after discovery, want 0", remainingLocks)
	}
}

func TestPageManifestDiscoveryWaitCancellationReleasesLockReference(t *testing.T) {
	tempDir := t.TempDir()
	s, err := newServerWithoutCatalogForTest(filepath.Join(tempDir, "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`
		CREATE TABLE work_identities (work_identity_id TEXT PRIMARY KEY, current_candidate_id TEXT);
		INSERT INTO work_identities (work_identity_id, current_candidate_id)
		VALUES ('identity-cancel-discovery', 'work-cancel-discovery');
	`); err != nil {
		t.Fatal(err)
	}
	work := map[string]any{
		"candidate_id":     "work-cancel-discovery",
		"work_identity_id": "identity-cancel-discovery",
		"source_kind":      "archive",
		"extension":        ".cbz",
		"path":             filepath.Join(tempDir, "not-opened.cbz"),
	}
	lockKey := "page-manifest-discovery:" + stringValue(work["candidate_id"])
	releaseHolder, acquired := s.acquireRenderLock(context.Background(), lockKey)
	if !acquired {
		t.Fatal("failed to acquire discovery lock holder")
	}
	holderReleased := false
	defer func() {
		if !holderReleased {
			releaseHolder()
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, _, err := s.pageRowsAndManifestForWork(ctx, work, true)
		result <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		s.renderLockMu.Lock()
		refs := s.renderLocks[lockKey].refs
		s.renderLockMu.Unlock()
		if refs == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("discovery waiter did not register a lock reference; refs = %d", refs)
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled discovery returned %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled discovery did not return")
	}
	s.renderLockMu.Lock()
	refsAfterCancel := s.renderLocks[lockKey].refs
	s.renderLockMu.Unlock()
	if refsAfterCancel != 1 {
		t.Fatalf("discovery lock refs after waiter cancellation = %d, want holder only", refsAfterCancel)
	}

	releaseHolder()
	holderReleased = true
	s.renderLockMu.Lock()
	_, stillRegistered := s.renderLocks[lockKey]
	s.renderLockMu.Unlock()
	if stillRegistered {
		t.Fatal("discovery lock remained registered after holder release")
	}
}

func TestEnsurePageManifestWaitCancellationReleasesLockReference(t *testing.T) {
	s, err := newServerWithoutCatalogForTest(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	work := map[string]any{
		"candidate_id":     "work-cancel-manifest",
		"work_identity_id": "identity-cancel-manifest",
		"source_kind":      "archive",
		"path":             "cancel-manifest.cbz",
	}
	rows := []map[string]any{{
		"page_index":        0,
		"relative_path":     "001.jpg",
		"source_inner_path": "001.jpg",
		"extension":         ".jpg",
		"size_bytes":        32,
	}}
	manifest := virtualPageManifest(work, rows)
	lockKey := "page-manifest:" + stringValue(work["work_identity_id"]) + ":" + stringValue(manifest["manifest_hash"])
	releaseHolder, acquired := s.acquireRenderLock(context.Background(), lockKey)
	if !acquired {
		t.Fatal("failed to acquire manifest lock holder")
	}
	holderReleased := false
	defer func() {
		if !holderReleased {
			releaseHolder()
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := s.ensurePageManifest(ctx, work, rows)
		result <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		s.renderLockMu.Lock()
		refs := s.renderLocks[lockKey].refs
		s.renderLockMu.Unlock()
		if refs == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("manifest waiter did not register a lock reference; refs = %d", refs)
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled manifest ensure returned %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled manifest ensure did not return")
	}
	s.renderLockMu.Lock()
	refsAfterCancel := s.renderLocks[lockKey].refs
	s.renderLockMu.Unlock()
	if refsAfterCancel != 1 {
		t.Fatalf("manifest lock refs after waiter cancellation = %d, want holder only", refsAfterCancel)
	}

	releaseHolder()
	holderReleased = true
	s.renderLockMu.Lock()
	_, stillRegistered := s.renderLocks[lockKey]
	s.renderLockMu.Unlock()
	if stillRegistered {
		t.Fatal("manifest lock remained registered after holder release")
	}
}

func TestSQLiteDSNAppliesConnectionPragmasWithNoIdlePool(t *testing.T) {
	t.Setenv("BMANGA_SQLITE_MAX_OPEN_CONNS", "4")
	t.Setenv("BMANGA_SQLITE_MAX_IDLE_CONNS", "0")
	t.Setenv("BMANGA_SQLITE_CACHE_KIB", "8192")
	t.Setenv("BMANGA_SQLITE_MMAP_BYTES", "0")
	s, err := newServerWithoutCatalogForTest(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for attempt := 0; attempt < 3; attempt++ {
		var cacheSize int
		var tempStore int
		var mmapSize int64
		if err := s.db.QueryRow("PRAGMA cache_size").Scan(&cacheSize); err != nil {
			t.Fatal(err)
		}
		if err := s.db.QueryRow("PRAGMA temp_store").Scan(&tempStore); err != nil {
			t.Fatal(err)
		}
		if err := s.db.QueryRow("PRAGMA mmap_size").Scan(&mmapSize); err != nil {
			t.Fatal(err)
		}
		if cacheSize != -8192 || tempStore != 2 || mmapSize != 0 {
			t.Fatalf("connection pragmas = cache:%d temp:%d mmap:%d, want -8192/2/0", cacheSize, tempStore, mmapSize)
		}
	}
}

func TestAddedWorkFastPathUsesCoverageForCurrentType(t *testing.T) {
	s, err := newServerWithoutCatalogForTest(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`
		CREATE TABLE work_candidates (
			candidate_id TEXT PRIMARY KEY,
			library_key TEXT NOT NULL DEFAULT '',
			candidate_type TEXT NOT NULL DEFAULT '',
			source_kind TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			relative_path TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX idx_work_candidates_candidate_type ON work_candidates (candidate_type);
		CREATE TABLE page_counts (candidate_id TEXT PRIMARY KEY, page_count_status TEXT NOT NULL DEFAULT '', readable_page_count INTEGER NOT NULL DEFAULT 0);
		CREATE TABLE work_identities (work_identity_id TEXT PRIMARY KEY, current_candidate_id TEXT NOT NULL, first_seen_at TEXT NOT NULL DEFAULT '');
		CREATE TABLE series_items (candidate_id TEXT NOT NULL);
		INSERT INTO work_candidates (candidate_id, library_key, candidate_type, source_kind, title, relative_path) VALUES
			('doujin-old', 'doujin', 'doujin', 'archive', 'Old', 'old.zip'),
			('doujin-new', 'doujin', 'doujin', 'archive', 'New', 'new.zip'),
			('doujin-undated', 'doujin', 'doujin', 'archive', 'Undated', 'undated.zip'),
			('manga-missing', 'manga', 'manga_file', 'file', 'Missing', 'missing.cbz');
		INSERT INTO source_filesystem_times (target_type, target_id, source_created_utc, status, observed_at) VALUES
			('work', 'doujin-old', '2026-01-01T00:00:00Z', 'ok', '2026-01-01T00:00:00Z'),
			('work', 'doujin-new', '2026-02-01T00:00:00Z', 'ok', '2026-02-01T00:00:00Z'),
			('work', 'doujin-undated', '', 'missing', '2026-02-01T00:00:00Z');
	`); err != nil {
		t.Fatal(err)
	}

	coverageFilters := []string{"wc.candidate_type = ?"}
	coverageArgs := []any{"doujin"}
	if !s.workSourceTimeCacheCoversFilters(coverageFilters, coverageArgs) {
		t.Fatal("doujin coverage should be complete even when another candidate type is missing source time")
	}
	if s.workSourceTimeCacheCoversFilters(nil, nil) {
		t.Fatal("global coverage should report the unindexed manga work")
	}
	ids, used, err := s.fetchAddedSortedWorkIDs(
		coverageFilters,
		coverageArgs,
		coverageFilters,
		coverageArgs,
		"",
		nil,
		"added_desc",
		3,
		0,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !used || strings.Join(ids, ",") != "doujin-new,doujin-old,doujin-undated" {
		t.Fatalf("fast added ids = used:%v ids:%v, want doujin-new,doujin-old,doujin-undated", used, ids)
	}
	ascIDs, used, err := s.fetchAddedSortedWorkIDs(coverageFilters, coverageArgs, coverageFilters, coverageArgs, "", nil, "added_asc", 3, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !used || strings.Join(ascIDs, ",") != "doujin-old,doujin-new,doujin-undated" {
		t.Fatalf("fast ascending ids = used:%v ids:%v", used, ascIDs)
	}
	offsetIDs, used, err := s.fetchAddedSortedWorkIDs(coverageFilters, coverageArgs, coverageFilters, coverageArgs, "", nil, "added_desc", 1, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if !used || strings.Join(offsetIDs, ",") != "doujin-old" {
		t.Fatalf("fast offset ids = used:%v ids:%v", used, offsetIDs)
	}
	optionsQuery := url.Values{"type": []string{"doujin"}}
	allowed, includeSeries := s.addedSortedWorkFastPathOptions(optionsQuery)
	if !allowed || includeSeries {
		t.Fatalf("empty doujin series invariant = allowed:%v includeSeries:%v", allowed, includeSeries)
	}
	if _, err := s.db.Exec(`INSERT INTO series_items (candidate_id) VALUES ('doujin-new')`); err != nil {
		t.Fatal(err)
	}
	allowed, _ = s.addedSortedWorkFastPathOptions(optionsQuery)
	if allowed {
		t.Fatal("doujin added fast path should fall back when a series title can affect ordering")
	}
}

func TestWorkSourceTimeCacheCompleteRejectsBalancedOrphans(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE work_candidates (candidate_id TEXT);
		CREATE TABLE source_filesystem_times (target_type TEXT, target_id TEXT);
		INSERT INTO work_candidates (candidate_id) VALUES ('work-a'), ('work-b');
		INSERT INTO source_filesystem_times (target_type, target_id) VALUES ('work', 'work-a'), ('work', 'orphan');
	`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db}
	if s.workSourceTimeCacheComplete() {
		t.Fatal("source-time coverage accepted one orphan balanced by one missing work")
	}
	if _, err := db.Exec(`
		DELETE FROM source_filesystem_times WHERE target_id = 'orphan';
		INSERT INTO source_filesystem_times (target_type, target_id) VALUES ('work', 'work-b');
	`); err != nil {
		t.Fatal(err)
	}
	if !s.workSourceTimeCacheComplete() {
		t.Fatal("source-time coverage rejected an exact one-to-one cache")
	}
}

func TestEnsureOptimizedWorkBrowseViewReplacesLegacyMaintenanceColumns(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE work_candidates (
			candidate_id TEXT PRIMARY KEY,
			library_key TEXT NOT NULL DEFAULT '',
			library_name TEXT NOT NULL DEFAULT '',
			candidate_type TEXT NOT NULL DEFAULT '',
			source_kind TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL DEFAULT '',
			relative_path TEXT NOT NULL DEFAULT '',
			parent_relative_path TEXT NOT NULL DEFAULT '',
			size_bytes INTEGER NOT NULL DEFAULT 0,
			modified_utc TEXT NOT NULL DEFAULT '',
			extension TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE page_counts (
			candidate_id TEXT PRIMARY KEY,
			page_count_status TEXT NOT NULL DEFAULT '',
			readable_page_count INTEGER NOT NULL DEFAULT 0,
			reason TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE work_cover_candidates (
			candidate_id TEXT PRIMARY KEY,
			cover_status TEXT NOT NULL DEFAULT '',
			cover_kind TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE translation_items (
			candidate_id TEXT NOT NULL,
			translation_group TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX idx_translation_items_candidate_id ON translation_items (candidate_id);
		CREATE TABLE deletion_candidates (
			candidate_id TEXT PRIMARY KEY
		);
		CREATE TABLE work_identities (
			work_identity_id TEXT PRIMARY KEY,
			current_candidate_id TEXT NOT NULL
		);
		CREATE VIEW work_translation_summary AS
		SELECT candidate_id, GROUP_CONCAT(DISTINCT translation_group) AS translation_sources, MAX(action) AS strongest_action
		FROM translation_items
		GROUP BY candidate_id;
		CREATE VIEW work_browse AS
		SELECT
			wi.work_identity_id, wc.candidate_id, wc.library_key, wc.library_name, wc.candidate_type, wc.source_kind,
			wc.title, wc.path, wc.relative_path, wc.parent_relative_path, wc.size_bytes, wc.modified_utc, wc.extension,
			COALESCE(pc.page_count_status, '') AS page_count_status,
			COALESCE(pc.readable_page_count, '') AS readable_page_count,
			COALESCE(pc.reason, '') AS page_count_reason,
			COALESCE(wcc.cover_status, '') AS cover_status,
			COALESCE(wcc.cover_kind, '') AS cover_kind,
			COALESCE(wts.translation_sources, '') AS translation_sources,
			CASE WHEN dc.candidate_id IS NOT NULL THEN 'delete_candidate' ELSE COALESCE(wts.strongest_action, 'keep') END AS local_action
		FROM work_candidates wc
		LEFT JOIN page_counts pc ON pc.candidate_id = wc.candidate_id
		LEFT JOIN work_cover_candidates wcc ON wcc.candidate_id = wc.candidate_id
		LEFT JOIN work_translation_summary wts ON wts.candidate_id = wc.candidate_id
		LEFT JOIN deletion_candidates dc ON dc.candidate_id = wc.candidate_id
		LEFT JOIN work_identities wi ON wi.current_candidate_id = wc.candidate_id;
		INSERT INTO work_candidates (candidate_id, title) VALUES ('work-1', 'Work 1');
		INSERT INTO translation_items (candidate_id, translation_group, action) VALUES ('work-1', 'source-a', 'keep');
	`); err != nil {
		t.Fatal(err)
	}

	if err := ensureOptimizedWorkBrowseView(db); err != nil {
		t.Fatal(err)
	}
	var viewDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'view' AND name = 'work_browse'`).Scan(&viewDefinition); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(viewDefinition, "work_translation_summary") || !strings.Contains(viewDefinition, "SELECT GROUP_CONCAT(DISTINCT ti.translation_group)") {
		t.Fatalf("work_browse definition was not replaced with the optimized view:\n%s", viewDefinition)
	}
	for _, legacyFragment := range []string{"deletion_candidates", "local_action", "strongest_action"} {
		if strings.Contains(viewDefinition, legacyFragment) {
			t.Fatalf("work_browse retained legacy maintenance fragment %q:\n%s", legacyFragment, viewDefinition)
		}
	}
	var legacyTableCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'deletion_candidates'`).Scan(&legacyTableCount); err != nil {
		t.Fatal(err)
	}
	if legacyTableCount != 1 {
		t.Fatalf("legacy maintenance table count = %d, want preserved table", legacyTableCount)
	}

	rows, err := db.Query(`EXPLAIN QUERY PLAN SELECT * FROM work_browse WHERE candidate_id = ?`, "work-1")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan, "\n")
	if strings.Contains(joined, "MATERIALIZE work_translation_summary") {
		t.Fatalf("work_browse query plan still materializes work_translation_summary:\n%s", joined)
	}
	if !strings.Contains(joined, "CORRELATED SCALAR SUBQUERY") {
		t.Fatalf("work_browse query plan did not use per-work translation subqueries:\n%s", joined)
	}
}

func TestEnsureOptimizedWorkBrowseViewRollbackPreservesExistingView(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE work_candidates (
			candidate_id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO work_candidates (candidate_id, title) VALUES ('work-legacy', 'Legacy Work');
		CREATE VIEW work_browse AS
		SELECT candidate_id, 'legacy-view' AS view_generation
		FROM work_candidates;
	`); err != nil {
		t.Fatal(err)
	}

	var beforeDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'view' AND name = 'work_browse'`).Scan(&beforeDefinition); err != nil {
		t.Fatal(err)
	}
	err = ensureOptimizedWorkBrowseViewWithDDL(db, `CREATE VIEW work_browse AS SELECT FROM`)
	if err == nil {
		t.Fatal("invalid replacement DDL unexpectedly succeeded")
	}

	var afterDefinition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'view' AND name = 'work_browse'`).Scan(&afterDefinition); err != nil {
		t.Fatalf("work_browse disappeared after replacement failure: %v", err)
	}
	if afterDefinition != beforeDefinition {
		t.Fatalf("work_browse definition changed despite rollback:\nbefore: %s\nafter:  %s", beforeDefinition, afterDefinition)
	}
	var generation string
	if err := db.QueryRow(`SELECT view_generation FROM work_browse WHERE candidate_id = 'work-legacy'`).Scan(&generation); err != nil {
		t.Fatalf("preserved work_browse is not queryable: %v", err)
	}
	if generation != "legacy-view" {
		t.Fatalf("preserved work_browse generation = %q, want legacy-view", generation)
	}
}

func TestRunSQLiteMigrationRollsBackDDLAndBackfill(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE migration_anchor (
			id INTEGER PRIMARY KEY,
			value TEXT NOT NULL
		);
		INSERT INTO migration_anchor (id, value) VALUES (1, 'before');
	`); err != nil {
		t.Fatal(err)
	}

	err = runSQLiteMigration(db, "atomicity-failure-test", func(tx sqliteSchemaRunner) error {
		if _, err := tx.Exec(`CREATE TABLE migration_new_table (id INTEGER PRIMARY KEY)`); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE migration_anchor SET value = 'during' WHERE id = 1`); err != nil {
			return err
		}
		_, err := tx.Exec(`ALTER TABLE migration_anchor ADD COLUMN value TEXT`)
		return err
	})
	if err == nil {
		t.Fatal("invalid migration unexpectedly succeeded")
	}

	var value string
	if err := db.QueryRow(`SELECT value FROM migration_anchor WHERE id = 1`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "before" {
		t.Fatalf("migration backfill survived rollback: value = %q, want before", value)
	}
	var newTableCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'migration_new_table'`).Scan(&newTableCount); err != nil {
		t.Fatal(err)
	}
	if newTableCount != 0 {
		t.Fatalf("migration DDL survived rollback: table count = %d, want 0", newTableCount)
	}
}

func TestRunSQLiteMigrationSerializesWritersAndRecordsVersions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := configureSQLiteConnection(db); err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(2)

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondEntered := make(chan struct{})
	firstResult := make(chan error, 1)
	secondResult := make(chan error, 1)
	go func() {
		firstResult <- runSQLiteMigration(db, "concurrent-first", func(tx sqliteSchemaRunner) error {
			close(firstEntered)
			<-releaseFirst
			_, err := tx.Exec(`CREATE TABLE concurrent_first (id INTEGER PRIMARY KEY)`)
			return err
		})
	}()
	<-firstEntered
	go func() {
		secondResult <- runSQLiteMigration(db, "concurrent-second", func(tx sqliteSchemaRunner) error {
			close(secondEntered)
			_, err := tx.Exec(`CREATE TABLE concurrent_second (id INTEGER PRIMARY KEY)`)
			return err
		})
	}()
	select {
	case <-secondEntered:
		t.Fatal("second migration entered before the BEGIN IMMEDIATE writer lock was released")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-firstResult; err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("second migration did not enter after the first committed")
	}
	if err := <-secondResult; err != nil {
		t.Fatal(err)
	}

	var recorded int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM bmanga_schema_migrations
		WHERE name IN ('concurrent-first', 'concurrent-second')
	`).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 2 {
		t.Fatalf("recorded migrations = %d, want 2", recorded)
	}
}

func TestEnsureLocalSQLiteTablesRollsBackOnSchemaFailure(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE work_candidates (candidate_id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	if err := ensureLocalSQLiteTables(db); err == nil {
		t.Fatal("incompatible pre-existing schema unexpectedly migrated")
	}

	var readerProfilesCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'reader_profiles'`).Scan(&readerProfilesCount); err != nil {
		t.Fatal(err)
	}
	if readerProfilesCount != 0 {
		t.Fatalf("partial local schema survived failed startup migration: reader_profiles count = %d, want 0", readerProfilesCount)
	}
	var workCandidatesCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'work_candidates'`).Scan(&workCandidatesCount); err != nil {
		t.Fatal(err)
	}
	if workCandidatesCount != 1 {
		t.Fatalf("pre-existing schema changed during failed startup migration: work_candidates count = %d, want 1", workCandidatesCount)
	}
}

func TestEnsureOptimizedWorkBrowseViewLeavesExistingTableUntouched(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE work_candidates (candidate_id TEXT PRIMARY KEY);
		CREATE TABLE work_browse (
			candidate_id TEXT PRIMARY KEY,
			view_generation TEXT NOT NULL
		);
		INSERT INTO work_browse (candidate_id, view_generation)
		VALUES ('work-table', 'table-sentinel');
	`); err != nil {
		t.Fatal(err)
	}

	if err := ensureOptimizedWorkBrowseView(db); err != nil {
		t.Fatal(err)
	}
	var objectType string
	if err := db.QueryRow(`SELECT type FROM sqlite_master WHERE name = 'work_browse'`).Scan(&objectType); err != nil {
		t.Fatal(err)
	}
	if objectType != "table" {
		t.Fatalf("work_browse type = %q, want table", objectType)
	}
	var generation string
	if err := db.QueryRow(`SELECT view_generation FROM work_browse WHERE candidate_id = 'work-table'`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if generation != "table-sentinel" {
		t.Fatalf("work_browse table row changed to %q", generation)
	}
}

func TestCorrectionSaveSucceedsWhenWALCheckpointIsBusy(t *testing.T) {
	t.Setenv("BMANGA_SQLITE_MAX_OPEN_CONNS", "2")
	t.Setenv("BMANGA_SQLITE_MAX_IDLE_CONNS", "2")
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`
		CREATE TABLE work_browse (
			candidate_id TEXT PRIMARY KEY,
			work_identity_id TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO work_browse (candidate_id, work_identity_id, title)
		VALUES ('work-checkpoint-busy', 'identity-checkpoint-busy', 'Checkpoint Busy');
		INSERT INTO local_corrections (
			target_type, target_id, correction_type, correction_value, note, created_at, updated_at
		) VALUES (
			'work', 'work-checkpoint-busy', 'review_status', 'keep', 'seed',
			'2026-07-15T00:00:00Z', '2026-07-15T00:00:00Z'
		);
	`); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	readerConn, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer readerConn.Close()
	if _, err := readerConn.ExecContext(ctx, "PRAGMA busy_timeout = 1"); err != nil {
		t.Fatal(err)
	}
	writerConn, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writerConn.ExecContext(ctx, "PRAGMA busy_timeout = 1"); err != nil {
		_ = writerConn.Close()
		t.Fatal(err)
	}
	if err := writerConn.Close(); err != nil {
		t.Fatal(err)
	}

	readTx, err := readerConn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer readTx.Rollback()
	var pinnedValue string
	if err := readTx.QueryRowContext(ctx, `
		SELECT correction_value
		FROM local_corrections
		WHERE target_type = 'work'
		  AND target_id = 'work-checkpoint-busy'
		  AND correction_type = 'review_status'
	`).Scan(&pinnedValue); err != nil {
		t.Fatal(err)
	}
	if pinnedValue != "keep" {
		t.Fatalf("pinned correction = %q, want keep", pinnedValue)
	}

	response := postJSON(t, s, "/api/corrections", map[string]any{
		"target_type":      "work",
		"target_id":        "work-checkpoint-busy",
		"correction_type":  "review_status",
		"correction_value": "ok",
		"note":             "checkpoint busy regression",
	})
	if response["ok"] != true {
		t.Fatalf("correction response = %#v, want ok", response)
	}

	var storedValue string
	if err := s.db.QueryRow(`
		SELECT correction_value
		FROM local_corrections
		WHERE target_type = 'work'
		  AND target_id = 'work-checkpoint-busy'
		  AND correction_type = 'review_status'
	`).Scan(&storedValue); err != nil {
		t.Fatal(err)
	}
	if storedValue != "ok" {
		t.Fatalf("stored correction = %q, want ok", storedValue)
	}
	if err := readTx.QueryRowContext(ctx, `
		SELECT correction_value
		FROM local_corrections
		WHERE target_type = 'work'
		  AND target_id = 'work-checkpoint-busy'
		  AND correction_type = 'review_status'
	`).Scan(&pinnedValue); err != nil {
		t.Fatal(err)
	}
	if pinnedValue != "keep" {
		t.Fatalf("read transaction was not pinned: got %q, want keep", pinnedValue)
	}
	if err := readTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := s.checkpointSQLiteWAL(); err != nil {
		t.Fatalf("checkpoint after reader release failed: %v", err)
	}
}

func TestCorrectionsBatchPersistsAfterServerReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`
		CREATE TABLE correction_test_works (
			candidate_id TEXT PRIMARY KEY,
			work_identity_id TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO correction_test_works (candidate_id, work_identity_id, title)
		VALUES ('work-persist', 'identity-persist', 'Persist Test');
		CREATE VIEW work_browse AS
		SELECT candidate_id, work_identity_id, title
		FROM correction_test_works;
	`); err != nil {
		s.Close()
		t.Fatal(err)
	}

	response := postJSON(t, s, "/api/corrections/batch", map[string]any{
		"items": []map[string]any{
			{
				"target_type":      "work",
				"target_id":        "work-persist",
				"correction_type":  "review_status",
				"correction_value": "needs_fix",
				"note":             "durability regression",
			},
		},
	})
	if response["ok"] != true || int(response["applied"].(float64)) != 1 {
		s.Close()
		t.Fatalf("batch response = %#v, want one applied item", response)
	}
	s.Close()

	reopened, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var correctionValue string
	if err := reopened.db.QueryRow(`
		SELECT correction_value
		FROM local_corrections
		WHERE target_type = 'work'
		  AND target_id = 'work-persist'
		  AND correction_type = 'review_status'
	`).Scan(&correctionValue); err != nil {
		t.Fatal(err)
	}
	if correctionValue != "needs_fix" {
		t.Fatalf("correction_value = %q, want needs_fix", correctionValue)
	}
	var journalMode string
	if err := reopened.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if strings.ToLower(journalMode) != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
}

func TestRuntimePathMappingTranslatesWindowsAndUNCPaths(t *testing.T) {
	separator := string(rune(92))
	windowsRoot := "X:" + separator + strings.Join([]string{"synthetic", "workspace"}, separator)
	uncRoot := strings.Repeat(separator, 2) + strings.Join([]string{"nas.example.invalid", "library", "books"}, separator)
	mappingJSON, err := json.Marshal([]pathMapping{
		{From: windowsRoot, To: "/app"},
		{From: uncRoot, To: "/libraries/example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMANGA_PATH_MAP", string(mappingJSON))
	dbDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := newServerWithoutCatalogForTest(filepath.Join(dbDir, "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	windowsInput := windowsRoot + separator + strings.Join([]string{"cache", "cover", "a.jpg"}, separator)
	if got, want := s.remapRuntimePath(windowsInput), filepath.Join("/app", "cache", "cover", "a.jpg"); got != want {
		t.Fatalf("local remap = %q, want %q", got, want)
	}
	uncInput := uncRoot + separator + strings.Join([]string{"A", "B.zip"}, separator)
	if got, want := s.remapRuntimePath(uncInput), filepath.Join("/libraries/example", "A", "B.zip"); got != want {
		t.Fatalf("UNC remap = %q, want %q", got, want)
	}
	unmappedUNC := strings.Repeat(separator, 2) + strings.Join([]string{"unmapped.example.invalid", "other", "book.zip"}, separator)
	if got := s.remapRuntimePath(unmappedUNC); got != unmappedUNC {
		t.Fatalf("unexpected remap = %q", got)
	}
}

func TestResolvePathUnderRootRejectsSymlinkEscape(t *testing.T) {
	dbDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := newServerWithoutCatalogForTest(filepath.Join(dbDir, "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	root := filepath.Join(t.TempDir(), "root")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "victim.jpg")
	if err := os.WriteFile(victim, []byte("not really an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.jpg")
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("symlink unavailable on this platform: %v", err)
	}
	if resolved, err := s.resolvePathUnderRoot(link, root); err == nil {
		t.Fatalf("resolvePathUnderRoot symlink escape resolved to %q, want error", resolved)
	}
}

func TestSameOriginWriteAllowedUsesForwardedHTTPSHost(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/progress", nil)
	req.Host = "127.0.0.1:8765"
	req.Header.Set("Origin", "https://bmanga.example.com")
	req.Header.Set("X-Forwarded-Host", "bmanga.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set(writeIntentHeader, writeIntentValue)
	if !s.sameOriginWriteAllowed(req) {
		t.Fatal("forwarded https origin should be accepted")
	}

	hostile := httptest.NewRequest(http.MethodPost, "/api/progress", nil)
	hostile.Host = "127.0.0.1:8765"
	hostile.Header.Set("Origin", "https://evil.example.com")
	hostile.Header.Set("X-Forwarded-Host", "bmanga.example.com")
	hostile.Header.Set("X-Forwarded-Proto", "https")
	if s.sameOriginWriteAllowed(hostile) {
		t.Fatal("hostile forwarded origin should be rejected")
	}
}

func TestSameOriginWriteAllowedAcceptsProgressBeaconToken(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/progress?write_token=token-a", nil)
	req.Host = "bmanga.example.com"
	req.Header.Set("Origin", "https://bmanga.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.AddCookie(&http.Cookie{Name: writeTokenCookie, Value: "token-a"})
	if !s.sameOriginWriteAllowed(req) {
		t.Fatal("progress beacon token matching cookie should be accepted")
	}

	otherPath := httptest.NewRequest(http.MethodPost, "/api/corrections?write_token=token-a", nil)
	otherPath.Host = "bmanga.example.com"
	otherPath.Header.Set("Origin", "https://bmanga.example.com")
	otherPath.Header.Set("X-Forwarded-Proto", "https")
	otherPath.AddCookie(&http.Cookie{Name: writeTokenCookie, Value: "token-a"})
	if s.sameOriginWriteAllowed(otherPath) {
		t.Fatal("query token should not authorize non-progress writes")
	}
}

func TestCoverFallbackThumbnailCacheSkipsRepeatedZipRead(t *testing.T) {
	tempDir := t.TempDir()
	dbDir := filepath.Join(tempDir, "data")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	libraryRoot := filepath.Join(tempDir, "library")
	if err := os.MkdirAll(libraryRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(libraryRoot, "book.zip")
	if err := writeTestCoverArchive(archivePath, "cover.jpg"); err != nil {
		t.Fatal(err)
	}

	s, err := newServerWithoutCatalogForTest(filepath.Join(dbDir, "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`
		CREATE TABLE libraries (
			key TEXT PRIMARY KEY,
			root_path TEXT NOT NULL
		);
		CREATE TABLE cover_assets (
			candidate_id TEXT NOT NULL,
			asset_kind TEXT NOT NULL,
			cache_path TEXT NOT NULL,
			mime_type TEXT NOT NULL,
			source_path TEXT NOT NULL,
			source_inner_path TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			stable_key TEXT NOT NULL
		);
		CREATE TABLE work_cover_candidates (
			candidate_id TEXT PRIMARY KEY,
			library_key TEXT NOT NULL,
			source_kind TEXT NOT NULL,
			cover_status TEXT NOT NULL,
			cover_kind TEXT NOT NULL,
			cover_source_path TEXT NOT NULL,
			cover_source_relative_path TEXT NOT NULL
		);
	`); err != nil {
		t.Fatal(err)
	}
	missingCachePath := filepath.Join(s.localCacheRoot, "missing-cover.jpg")
	if _, err := s.db.Exec(`INSERT INTO libraries (key, root_path) VALUES ('test-library', ?)`, libraryRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`
		INSERT INTO work_cover_candidates (
			candidate_id, library_key, source_kind, cover_status, cover_kind,
			cover_source_path, cover_source_relative_path
		) VALUES ('candidate-zip', 'test-library', 'archive', 'ready', 'archive', ?, 'book.zip')
	`, archivePath); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`
		INSERT INTO cover_assets (
			candidate_id, asset_kind, cache_path, mime_type,
			source_path, source_inner_path,
			updated_at, stable_key
		) VALUES (
			'candidate-zip', 'extracted_cover', ?, 'image/jpeg',
			?, 'cover.jpg',
			'2026-05-23T00:00:00Z', 'cover-test'
		)
	`, missingCachePath, archivePath); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/cover?id=candidate-zip&size=64", nil)
	first := httptest.NewRecorder()
	s.Routes().ServeHTTP(first, req)
	if first.Code != http.StatusOK {
		t.Fatalf("first cover returned %d: %s", first.Code, first.Body.String())
	}
	if contentType := first.Header().Get("Content-Type"); contentType != "image/jpeg" {
		t.Fatalf("first content type = %q, want image/jpeg", contentType)
	}
	matches, err := filepath.Glob(filepath.Join(s.thumbnailCacheRoot, "*.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("thumbnail count = %d, want 1", len(matches))
	}

	stat, err := os.Stat(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := bytes.Repeat([]byte("x"), int(stat.Size()))
	if err := os.WriteFile(archivePath, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(archivePath, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatal(err)
	}

	second := httptest.NewRecorder()
	s.Routes().ServeHTTP(second, req)
	if second.Code != http.StatusOK {
		t.Fatalf("second cover returned %d after archive corruption: %s", second.Code, second.Body.String())
	}
	if contentType := second.Header().Get("Content-Type"); contentType != "image/jpeg" {
		t.Fatalf("second content type = %q, want image/jpeg", contentType)
	}
	if second.Body.Len() == 0 || !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatalf("second cover did not reuse cached thumbnail")
	}
}

func TestReadyCoverCandidateUsesCandidatePointLookup(t *testing.T) {
	s, err := newServerWithoutCatalogForTest(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`
		CREATE TABLE work_cover_candidates (
			candidate_id TEXT NOT NULL,
			library_key TEXT NOT NULL,
			cover_status TEXT NOT NULL,
			cover_kind TEXT NOT NULL,
			cover_source_path TEXT NOT NULL,
			cover_source_relative_path TEXT NOT NULL
		);
		CREATE INDEX idx_work_cover_candidates_candidate_id
			ON work_cover_candidates (candidate_id);
		CREATE INDEX idx_work_cover_candidates_cover_status
			ON work_cover_candidates (cover_status);
		WITH RECURSIVE sequence(value) AS (
			SELECT 1
			UNION ALL
			SELECT value + 1 FROM sequence WHERE value < 40000
		)
		INSERT INTO work_cover_candidates (
			candidate_id, library_key, cover_status, cover_kind,
			cover_source_path, cover_source_relative_path
		)
		SELECT printf('fixture-%06d', value), 'library', 'ready', 'archive', 'fixture.cbz', 'fixture.cbz'
		FROM sequence;
		INSERT INTO work_cover_candidates (
			candidate_id, library_key, cover_status, cover_kind,
			cover_source_path, cover_source_relative_path
		) VALUES ('candidate-point', 'library', 'ready', 'archive', 'book.cbz', 'book.cbz');
		ANALYZE;
	`); err != nil {
		t.Fatal(err)
	}

	row, err := s.readyCoverCandidate("candidate-point")
	if err != nil {
		t.Fatal(err)
	}
	if stringValue(row["library_key"]) != "library" || stringValue(row["cover_status"]) != "ready" {
		t.Fatalf("ready cover candidate = %#v", row)
	}

	planRows, err := s.query("EXPLAIN QUERY PLAN "+readyCoverCandidateQuery, "candidate-point")
	if err != nil {
		t.Fatal(err)
	}
	plan := ""
	for _, planRow := range planRows {
		plan += " " + strings.ToLower(stringValue(planRow["detail"]))
	}
	if !strings.Contains(plan, "idx_work_cover_candidates_candidate_id") {
		t.Fatalf("ready cover lookup plan = %q, want candidate index", strings.TrimSpace(plan))
	}
	if strings.Contains(plan, "idx_work_cover_candidates_cover_status") {
		t.Fatalf("ready cover lookup plan = %q, unexpectedly uses status index", strings.TrimSpace(plan))
	}
}

func TestArchivePageThumbnailCacheSkipsRepeatedEntryRead(t *testing.T) {
	tempDir := t.TempDir()
	dbDir := filepath.Join(tempDir, "data")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(tempDir, "book.zip")
	if err := writeTestCoverArchive(archivePath, "page.jpg"); err != nil {
		t.Fatal(err)
	}
	archiveReader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(archiveReader.File) != 1 {
		_ = archiveReader.Close()
		t.Fatalf("archive entry count = %d, want 1", len(archiveReader.File))
	}
	pageSize := int64(archiveReader.File[0].UncompressedSize64)
	_ = archiveReader.Close()

	s, err := newServerWithoutCatalogForTest(filepath.Join(dbDir, "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.zipOpenReader = zip.OpenReader

	req := httptest.NewRequest(http.MethodGet, "/page?id=test&index=0&max=64", nil)
	first := httptest.NewRecorder()
	s.sendArchivePage(first, req, archivePath, "page.jpg", pageSize, ".jpg", 64)
	if first.Code != http.StatusOK {
		t.Fatalf("first page returned %d: %s", first.Code, first.Body.String())
	}
	if cacheStatus := first.Header().Get("X-Bmanga-Cache"); cacheStatus != "miss" {
		t.Fatalf("first cache status = %q, want miss", cacheStatus)
	}

	stat, err := os.Stat(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := bytes.Repeat([]byte("x"), int(stat.Size()))
	if err := os.WriteFile(archivePath, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(archivePath, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatal(err)
	}

	second := httptest.NewRecorder()
	s.sendArchivePage(second, req, archivePath, "page.jpg", pageSize, ".jpg", 64)
	if second.Code != http.StatusOK {
		t.Fatalf("second page returned %d after entry corruption: %s", second.Code, second.Body.String())
	}
	if cacheStatus := second.Header().Get("X-Bmanga-Cache"); cacheStatus != "hit" {
		t.Fatalf("second cache status = %q, want hit", cacheStatus)
	}
	if second.Body.Len() == 0 || !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatalf("second page did not reuse cached thumbnail")
	}
}

func TestArchivePageColdConcurrentRequestsOpenZipOnce(t *testing.T) {
	tempDir := t.TempDir()
	dbDir := filepath.Join(tempDir, "data")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(tempDir, "concurrent-book.zip")
	if err := writeTestCoverArchive(archivePath, "folder/page.jpg"); err != nil {
		t.Fatal(err)
	}
	archiveReader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	pageSize := int64(archiveReader.File[0].UncompressedSize64)
	_ = archiveReader.Close()

	s, err := newServerWithoutCatalogForTest(filepath.Join(dbDir, "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	const workers = 12
	openStarted := make(chan struct{}, workers)
	releaseOpen := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(releaseOpen)
		}
	}()
	s.zipOpenReader = func(path string) (*zip.ReadCloser, error) {
		openStarted <- struct{}{}
		<-releaseOpen
		return zip.OpenReader(path)
	}

	type result struct {
		code        int
		cacheStatus string
		bodyLength  int
	}
	ready := make(chan struct{}, workers)
	start := make(chan struct{})
	results := make(chan result, workers)
	for index := 0; index < workers; index++ {
		go func() {
			ready <- struct{}{}
			<-start
			req := httptest.NewRequest(http.MethodGet, "/page?id=test&index=0&max=64", nil)
			recorder := httptest.NewRecorder()
			s.sendArchivePage(recorder, req, archivePath, `\folder\page.jpg`, pageSize, ".jpg", 64)
			results <- result{
				code:        recorder.Code,
				cacheStatus: recorder.Header().Get("X-Bmanga-Cache"),
				bodyLength:  recorder.Body.Len(),
			}
		}()
	}
	for index := 0; index < workers; index++ {
		<-ready
	}
	close(start)

	select {
	case <-openStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first archive open did not start")
	}
	lockKey := archivePageSourceLockKey(archivePath, normalizeArchiveEntryName(`\folder\page.jpg`), 64)
	allQueued := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.renderLockMu.Lock()
		lock := s.renderLocks[lockKey]
		refs := 0
		if lock != nil {
			refs = lock.refs
		}
		s.renderLockMu.Unlock()
		if refs == workers {
			allQueued = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	openCountBeforeRelease := len(openStarted) + 1
	close(releaseOpen)
	released = true

	misses := 0
	hits := 0
	for index := 0; index < workers; index++ {
		got := <-results
		if got.code != http.StatusOK || got.bodyLength == 0 {
			t.Errorf("concurrent archive page = status %d body %d", got.code, got.bodyLength)
		}
		switch got.cacheStatus {
		case "miss":
			misses++
		case "hit":
			hits++
		default:
			t.Errorf("concurrent archive cache status = %q", got.cacheStatus)
		}
	}
	if !allQueued {
		t.Fatal("concurrent requests did not queue on the stable pre-open archive lock")
	}
	if openCountBeforeRelease != 1 || len(openStarted) != 0 {
		t.Fatalf("archive open count = %d before release plus %d after, want exactly 1", openCountBeforeRelease, len(openStarted))
	}
	if misses != 1 || hits != workers-1 {
		t.Fatalf("concurrent cache results = %d miss %d hit, want 1 miss %d hit", misses, hits, workers-1)
	}
}

type blockingThumbnailResponseWriter struct {
	header  http.Header
	status  int
	entered chan struct{}
	release <-chan struct{}
	body    bytes.Buffer
}

func (w *blockingThumbnailResponseWriter) Header() http.Header {
	return w.header
}

func (w *blockingThumbnailResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *blockingThumbnailResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	select {
	case w.entered <- struct{}{}:
	default:
	}
	<-w.release
	return w.body.Write(data)
}

func waitForRenderLockRefs(t *testing.T, s *Server, key string, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		s.renderLockMu.Lock()
		refs := 0
		if lock := s.renderLocks[key]; lock != nil {
			refs = lock.refs
		}
		s.renderLockMu.Unlock()
		if refs >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("render lock %q refs = %d, want at least %d", key, refs, want)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestArchivePageSourceLockReleasedBeforeThumbnailResponse(t *testing.T) {
	tempDir := t.TempDir()
	dbDir := filepath.Join(tempDir, "data")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(tempDir, "book.zip")
	if err := writeTestCoverArchive(archivePath, "page.jpg"); err != nil {
		t.Fatal(err)
	}
	archiveReader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	pageSize := int64(archiveReader.File[0].UncompressedSize64)
	_ = archiveReader.Close()

	s, err := newServerWithoutCatalogForTest(filepath.Join(dbDir, "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.zipOpenReader = zip.OpenReader
	for index := 0; index < cap(s.thumbnailSem); index++ {
		s.thumbnailSem <- struct{}{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	releaseWrites := make(chan struct{})
	writesReleased := false
	defer func() {
		if !writesReleased {
			close(releaseWrites)
		}
	}()
	firstWriter := &blockingThumbnailResponseWriter{
		header:  make(http.Header),
		entered: make(chan struct{}, 1),
		release: releaseWrites,
	}
	firstDone := make(chan struct{})
	requestPath := "/page?id=test&index=0&max=64"
	go func() {
		req := httptest.NewRequest(http.MethodGet, requestPath, nil).WithContext(ctx)
		s.sendArchivePage(firstWriter, req, archivePath, "page.jpg", pageSize, ".jpg", 64)
		close(firstDone)
	}()

	lockKey := archivePageSourceLockKey(archivePath, normalizeArchiveEntryName("page.jpg"), 64)
	waitForRenderLockRefs(t, s, lockKey, 1)
	second := httptest.NewRecorder()
	secondDone := make(chan struct{})
	go func() {
		req := httptest.NewRequest(http.MethodGet, requestPath, nil).WithContext(ctx)
		s.sendArchivePage(second, req, archivePath, "page.jpg", pageSize, ".jpg", 64)
		close(secondDone)
	}()
	waitForRenderLockRefs(t, s, lockKey, 2)

	<-s.thumbnailSem
	select {
	case <-firstWriter.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first thumbnail response did not begin")
	}
	select {
	case <-secondDone:
	case <-time.After(3 * time.Second):
		t.Fatal("second thumbnail response stayed blocked behind the first response body")
	}
	if second.Code != http.StatusOK || second.Body.Len() == 0 {
		t.Fatalf("second thumbnail response = %d, bytes=%d", second.Code, second.Body.Len())
	}

	close(releaseWrites)
	writesReleased = true
	select {
	case <-firstDone:
	case <-time.After(3 * time.Second):
		t.Fatal("first thumbnail response did not finish after releasing the writer")
	}
	if firstWriter.status != http.StatusOK || firstWriter.body.Len() == 0 {
		t.Fatalf("first thumbnail response = %d, bytes=%d", firstWriter.status, firstWriter.body.Len())
	}
}

func writeTestCoverArchive(path string, innerPath string) error {
	var encoded bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 96, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 96; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(60 + x), G: uint8(40 + y), B: 120, A: 255})
		}
	}
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 88}); err != nil {
		return err
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	archive := zip.NewWriter(out)
	entry, err := archive.Create(innerPath)
	if err != nil {
		_ = archive.Close()
		return err
	}
	if _, err := entry.Write(encoded.Bytes()); err != nil {
		_ = archive.Close()
		return err
	}
	return archive.Close()
}

func TestPageRejectsInvalidIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for _, path := range []string{
		"/page?id=anything&index=-1",
		"/page?id=anything&index=not-a-number",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s returned %d, want %d", path, rec.Code, http.StatusBadRequest)
		}
		if got := rec.Header().Get("Server-Timing"); !strings.Contains(got, "app;dur=") {
			t.Fatalf("%s Server-Timing = %q, want app duration", path, got)
		}
	}
}

func TestSourcePageRejectsInvalidImageBytes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	root := filepath.Join(t.TempDir(), "library")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "bad.png")
	if err := os.WriteFile(source, []byte("not really a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS libraries (
			key TEXT PRIMARY KEY,
			root_path TEXT NOT NULL
		);
		INSERT INTO libraries (key, root_path) VALUES ('test-library', ?);
	`, root); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/page?id=work-invalid&index=0&max=1600", nil)
	rec := httptest.NewRecorder()
	work := map[string]any{
		"candidate_id":        "work-invalid",
		"work_identity_id":    "identity-invalid",
		"library_key":         "test-library",
		"source_kind":         "image_folder",
		"path":                root,
		"relative_path":       "",
		"readable_page_count": 1,
	}
	row := map[string]any{
		"library_key":   "test-library",
		"path":          source,
		"relative_path": "bad.png",
		"extension":     ".png",
	}
	s.sendSourcePageImage(rec, req, work, row, nil, 1600)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("invalid source image returned %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPageRejectsStaleRequestedManifest(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`
		CREATE TABLE work_browse (
			candidate_id TEXT PRIMARY KEY,
			work_identity_id TEXT NOT NULL,
			title TEXT NOT NULL,
			source_kind TEXT NOT NULL DEFAULT 'image_folder'
		);
		CREATE TABLE work_identities (
			work_identity_id TEXT PRIMARY KEY,
			current_candidate_id TEXT NOT NULL
		);
		INSERT INTO work_browse (candidate_id, work_identity_id, title, source_kind)
		VALUES ('work-1', 'identity-1', 'Test Work', 'image_folder');
		INSERT INTO work_identities (work_identity_id, current_candidate_id)
		VALUES ('identity-1', 'work-1');
		INSERT INTO page_manifests (
			page_manifest_id, work_identity_id, candidate_id, manifest_hash,
			page_count, source_kind, manifest_status, builder_version, built_at
		) VALUES (
			'manifest-current', 'identity-1', 'work-1', 'hash-current',
			1, 'image_folder', 'ready', 'go-reader-manifest-v3', '2026-06-04T00:00:00Z'
		);
	`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/page?id=work-1&index=0&manifest=manifest-old", nil)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale manifest returned %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["progress_status"] != "manifest_stale" {
		t.Fatalf("progress_status = %v, want manifest_stale", payload["progress_status"])
	}
}

func TestProgressRejectsStaleRequestedManifestWithoutOverwriting(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`
		CREATE TABLE work_browse (
			candidate_id TEXT PRIMARY KEY,
			work_identity_id TEXT NOT NULL,
			title TEXT NOT NULL,
			source_kind TEXT NOT NULL DEFAULT 'image_folder'
		);
		CREATE TABLE work_identities (
			work_identity_id TEXT PRIMARY KEY,
			current_candidate_id TEXT NOT NULL
		);
		INSERT INTO work_browse (candidate_id, work_identity_id, title, source_kind)
		VALUES ('work-1', 'identity-1', 'Test Work', 'image_folder');
		INSERT INTO work_identities (work_identity_id, current_candidate_id)
		VALUES ('identity-1', 'work-1');
		INSERT INTO page_manifests (
			page_manifest_id, work_identity_id, candidate_id, manifest_hash,
			page_count, source_kind, manifest_status, builder_version, built_at
		) VALUES (
			'manifest-current', 'identity-1', 'work-1', 'hash-current',
			12, 'image_folder', 'ready', 'go-reader-manifest-v3', '2026-06-04T00:00:00Z'
		);
	`); err != nil {
		t.Fatal(err)
	}

	postJSON(t, s, "/api/progress", map[string]any{
		"candidate_id":     "work-1",
		"page_manifest_id": "manifest-current",
		"manifest_hash":    "hash-current",
		"index":            4,
		"count":            12,
		"updated_at":       "2026-07-11T01:00:00Z",
	})

	staleRequests := []map[string]any{
		{
			"candidate_id":     "work-1",
			"page_manifest_id": "manifest-current",
			"manifest_hash":    "hash-old",
			"index":            11,
			"count":            12,
			"completed":        true,
			"updated_at":       "2026-07-11T02:00:00Z",
		},
		{
			"candidate_id":     "work-1",
			"page_manifest_id": "manifest-old",
			"manifest_hash":    "hash-current",
			"index":            11,
			"count":            12,
			"completed":        true,
			"updated_at":       "2026-07-11T03:00:00Z",
		},
	}
	for index, staleRequest := range staleRequests {
		body, err := json.Marshal(staleRequest)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/progress", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(writeIntentHeader, writeIntentValue)
		rec := httptest.NewRecorder()
		s.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("stale progress case %d returned %d: %s", index, rec.Code, rec.Body.String())
		}
		var conflict map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &conflict); err != nil {
			t.Fatal(err)
		}
		if conflict["progress_status"] != "manifest_stale" {
			t.Fatalf("stale progress case %d status = %v, want manifest_stale", index, conflict["progress_status"])
		}
	}

	payload := getJSON(t, s, "/api/progress?id=work-1")
	saved := payload["progress"].(map[string]any)
	if intValue(saved["index"]) != 4 || saved["page_manifest_id"] != "manifest-current" || saved["manifest_hash"] != "hash-current" || saved["updated_at"] != "2026-07-11T01:00:00Z" {
		t.Fatalf("stale write changed progress: %#v", saved)
	}
	markRows, err := s.query(`
		SELECT work_identity_id, read_status
		FROM work_user_marks
		WHERE reader_profile_key = 'default'
		  AND work_identity_id = 'identity-1'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(markRows) != 1 || markRows[0]["read_status"] != "reading" {
		t.Fatalf("stale completed write changed the accepted reading status: %#v", markRows)
	}
	legacy := postJSON(t, s, "/api/progress", map[string]any{
		"candidate_id": "work-1",
		"index":        5,
		"count":        12,
		"updated_at":   "2026-07-11T04:00:00Z",
	})
	legacyProgress := legacy["progress"].(map[string]any)
	if legacy["stored"] != true || intValue(legacyProgress["index"]) != 5 || legacyProgress["page_manifest_id"] != "manifest-current" || legacyProgress["manifest_hash"] != "hash-current" {
		t.Fatalf("legacy manifest-less progress write lost compatibility: %#v", legacy)
	}
}

func TestProgressPersistsReaderDisplayState(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`
		CREATE TABLE work_browse (
			candidate_id TEXT PRIMARY KEY,
			work_identity_id TEXT NOT NULL,
			title TEXT NOT NULL,
			source_kind TEXT NOT NULL DEFAULT 'image_folder'
		);
		CREATE TABLE work_identities (
			work_identity_id TEXT PRIMARY KEY,
			current_candidate_id TEXT NOT NULL
		);
		INSERT INTO work_browse (candidate_id, work_identity_id, title, source_kind)
		VALUES ('work-1', 'identity-1', 'Test Work', 'image_folder');
		INSERT INTO work_identities (work_identity_id, current_candidate_id)
		VALUES ('identity-1', 'work-1');
		INSERT INTO page_manifests (
			page_manifest_id, work_identity_id, candidate_id, manifest_hash,
			page_count, source_kind, manifest_status, builder_version, built_at
		) VALUES (
			'manifest-current', 'identity-1', 'work-1', 'hash-current',
			12, 'image_folder', 'ready', 'go-reader-manifest-v3', '2026-06-04T00:00:00Z'
		);
	`); err != nil {
		t.Fatal(err)
	}

	response := postJSON(t, s, "/api/progress", map[string]any{
		"candidate_id":       "work-1",
		"page_manifest_id":   "manifest-current",
		"manifest_hash":      "hash-current",
		"index":              4,
		"count":              12,
		"reader_fit_mode":    "fit-width",
		"reader_split_panel": 1,
		"stage_scroll_top":   321,
		"stage_scroll_left":  12,
	})
	progress := response["progress"].(map[string]any)
	if progress["reader_fit_mode"] != "fit-width" {
		t.Fatalf("reader_fit_mode = %v, want fit-width", progress["reader_fit_mode"])
	}
	if intValue(progress["reader_split_panel"]) != 0 {
		t.Fatalf("reader_split_panel = %v, want 0 outside split-wide", progress["reader_split_panel"])
	}
	if intValue(progress["stage_scroll_top"]) != 321 || intValue(progress["stage_scroll_left"]) != 12 {
		t.Fatalf("stage scroll = %v/%v, want 321/12", progress["stage_scroll_top"], progress["stage_scroll_left"])
	}

	payload := getJSON(t, s, "/api/progress?id=work-1")
	saved := payload["progress"].(map[string]any)
	if intValue(saved["index"]) != 4 || intValue(saved["count"]) != 12 {
		t.Fatalf("progress = %#v, want index 4 count 12", saved)
	}
	if saved["reader_fit_mode"] != "fit-width" || intValue(saved["stage_scroll_top"]) != 321 || intValue(saved["stage_scroll_left"]) != 12 {
		t.Fatalf("saved display state = %#v, want fit-width scroll 321/12", saved)
	}
}

func TestProgressFinalPageAutomaticallyMarksWorkCompleted(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`
		CREATE TABLE work_browse (
			candidate_id TEXT PRIMARY KEY,
			work_identity_id TEXT NOT NULL,
			title TEXT NOT NULL,
			source_kind TEXT NOT NULL DEFAULT 'image_folder'
		);
		CREATE TABLE work_identities (
			work_identity_id TEXT PRIMARY KEY,
			current_candidate_id TEXT NOT NULL
		);
		INSERT INTO work_browse (candidate_id, work_identity_id, title, source_kind)
		VALUES ('work-1', 'identity-1', 'Test Work', 'image_folder');
		INSERT INTO work_identities (work_identity_id, current_candidate_id)
		VALUES ('identity-1', 'work-1');
		INSERT INTO page_manifests (
			page_manifest_id, work_identity_id, candidate_id, manifest_hash,
			page_count, source_kind, manifest_status, builder_version, built_at
		) VALUES (
			'manifest-current', 'identity-1', 'work-1', 'hash-current',
			12, 'image_folder', 'ready', 'go-reader-manifest-v3', '2026-06-04T00:00:00Z'
		);
	`); err != nil {
		t.Fatal(err)
	}

	postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type":     "work",
		"target_id":       "work-1",
		"favorite":        true,
		"read_status":     "reading",
		"personal_rating": 8,
		"notes":           "keep me",
	})
	postJSON(t, s, "/api/progress", map[string]any{
		"candidate_id":     "work-1",
		"page_manifest_id": "manifest-current",
		"manifest_hash":    "hash-current",
		"index":            4,
		"count":            12,
		"updated_at":       "2026-07-11T01:00:00Z",
	})
	midPayload := getJSON(t, s, "/api/user-mark?target_type=work&target_id=work-1")
	midMark := midPayload["mark"].(map[string]any)
	if midMark["read_status"] != "reading" {
		t.Fatalf("mid-read read_status = %v, want reading", midMark["read_status"])
	}

	finalResponse := postJSON(t, s, "/api/progress", map[string]any{
		"candidate_id":     "work-1",
		"page_manifest_id": "manifest-current",
		"manifest_hash":    "hash-current",
		"index":            11,
		"count":            12,
		"updated_at":       "2026-07-11T02:00:00Z",
	})
	progress := finalResponse["progress"].(map[string]any)
	if progress["completed"] != true {
		t.Fatalf("progress completed = %v, want true", progress["completed"])
	}
	payload := getJSON(t, s, "/api/user-mark?target_type=work&target_id=work-1")
	mark := payload["mark"].(map[string]any)
	if mark["read_status"] != "completed" {
		t.Fatalf("read_status = %v, want completed", mark["read_status"])
	}
	if mark["favorite"] != true || int(mark["personal_rating"].(float64)) != 8 || mark["notes"] != "keep me" {
		t.Fatalf("auto-complete mark did not preserve existing fields: %#v", mark)
	}
}

func TestProgressSplitWideFinalPageCompletesOnlyAfterLeftPanel(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`
		CREATE TABLE work_browse (
			candidate_id TEXT PRIMARY KEY,
			work_identity_id TEXT NOT NULL,
			title TEXT NOT NULL,
			source_kind TEXT NOT NULL DEFAULT 'image_folder'
		);
		CREATE TABLE work_identities (
			work_identity_id TEXT PRIMARY KEY,
			current_candidate_id TEXT NOT NULL
		);
		INSERT INTO work_browse (candidate_id, work_identity_id, title, source_kind)
		VALUES ('work-1', 'identity-1', 'Wide Final Page', 'image_folder');
		INSERT INTO work_identities (work_identity_id, current_candidate_id)
		VALUES ('identity-1', 'work-1');
		INSERT INTO page_manifests (
			page_manifest_id, work_identity_id, candidate_id, manifest_hash,
			page_count, source_kind, manifest_status, builder_version, built_at
		) VALUES (
			'manifest-current', 'identity-1', 'work-1', 'hash-current',
			12, 'image_folder', 'ready', 'go-reader-manifest-v3', '2026-06-04T00:00:00Z'
		);
	`); err != nil {
		t.Fatal(err)
	}

	rightPanel := postJSON(t, s, "/api/progress", map[string]any{
		"candidate_id":       "work-1",
		"page_manifest_id":   "manifest-current",
		"manifest_hash":      "hash-current",
		"index":              11,
		"count":              12,
		"reader_fit_mode":    "split-wide",
		"reader_split_panel": 0,
		"updated_at":         "2026-07-11T02:00:00Z",
	})["progress"].(map[string]any)
	if rightPanel["completed"] == true || intValue(rightPanel["reader_split_panel"]) != 0 {
		t.Fatalf("right panel progress = %#v, want incomplete split panel 0", rightPanel)
	}

	leftPanel := postJSON(t, s, "/api/progress", map[string]any{
		"candidate_id":       "work-1",
		"page_manifest_id":   "manifest-current",
		"manifest_hash":      "hash-current",
		"index":              11,
		"count":              12,
		"reader_fit_mode":    "split-wide",
		"reader_split_panel": 1,
		"updated_at":         "2026-07-11T02:01:00Z",
	})["progress"].(map[string]any)
	if leftPanel["completed"] != true || intValue(leftPanel["reader_split_panel"]) != 1 {
		t.Fatalf("left panel progress = %#v, want completed split panel 1", leftPanel)
	}
}

func TestProgressIgnoresOlderClientUpdate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`
		CREATE TABLE work_browse (
			candidate_id TEXT PRIMARY KEY,
			work_identity_id TEXT NOT NULL,
			title TEXT NOT NULL,
			source_kind TEXT NOT NULL DEFAULT 'image_folder'
		);
		CREATE TABLE work_identities (
			work_identity_id TEXT PRIMARY KEY,
			current_candidate_id TEXT NOT NULL
		);
		INSERT INTO work_browse (candidate_id, work_identity_id, title, source_kind)
		VALUES ('work-1', 'identity-1', 'Test Work', 'image_folder');
		INSERT INTO work_identities (work_identity_id, current_candidate_id)
		VALUES ('identity-1', 'work-1');
		INSERT INTO page_manifests (
			page_manifest_id, work_identity_id, candidate_id, manifest_hash,
			page_count, source_kind, manifest_status, builder_version, built_at
		) VALUES (
			'manifest-current', 'identity-1', 'work-1', 'hash-current',
			102, 'image_folder', 'ready', 'go-reader-manifest-v3', '2026-06-04T00:00:00Z'
		);
	`); err != nil {
		t.Fatal(err)
	}

	latest := postJSON(t, s, "/api/progress", map[string]any{
		"candidate_id":       "work-1",
		"page_manifest_id":   "manifest-current",
		"manifest_hash":      "hash-current",
		"index":              101,
		"count":              102,
		"reader_fit_mode":    "fit-page",
		"reader_split_panel": 0,
		"updated_at":         "2026-06-18T16:19:05.446Z",
	})
	if latest["stored"] != true {
		t.Fatalf("latest stored = %v, want true", latest["stored"])
	}

	stale := postJSON(t, s, "/api/progress", map[string]any{
		"candidate_id":       "work-1",
		"page_manifest_id":   "manifest-current",
		"manifest_hash":      "hash-current",
		"index":              39,
		"count":              102,
		"reader_fit_mode":    "split-wide",
		"reader_split_panel": 1,
		"updated_at":         "2026-06-18T14:48:42.720Z",
	})
	if stale["stored"] != false {
		t.Fatalf("stale stored = %v, want false", stale["stored"])
	}
	progress := stale["progress"].(map[string]any)
	if intValue(progress["index"]) != 101 || progress["reader_fit_mode"] != "fit-page" {
		t.Fatalf("stale response progress = %#v, want final page fit-page", progress)
	}

	payload := getJSON(t, s, "/api/progress?id=work-1")
	saved := payload["progress"].(map[string]any)
	if intValue(saved["index"]) != 101 || saved["reader_fit_mode"] != "fit-page" || saved["updated_at"] != "2026-06-18T16:19:05.446Z" {
		t.Fatalf("saved progress = %#v, want latest progress preserved", saved)
	}
}

func TestProgressAtomicTimestampGuardRejectsOlderCompletedWrite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`
		CREATE TABLE work_browse (
			candidate_id TEXT PRIMARY KEY,
			work_identity_id TEXT NOT NULL,
			title TEXT NOT NULL,
			source_kind TEXT NOT NULL DEFAULT 'image_folder'
		);
		CREATE TABLE work_identities (
			work_identity_id TEXT PRIMARY KEY,
			current_candidate_id TEXT NOT NULL
		);
		INSERT INTO work_browse (candidate_id, work_identity_id, title, source_kind)
		VALUES ('work-atomic', 'identity-atomic', 'Atomic Progress Work', 'image_folder');
		INSERT INTO work_identities (work_identity_id, current_candidate_id)
		VALUES ('identity-atomic', 'work-atomic');
		INSERT INTO page_manifests (
			page_manifest_id, work_identity_id, candidate_id, manifest_hash,
			page_count, source_kind, manifest_status, builder_version, built_at
		) VALUES (
			'manifest-atomic', 'identity-atomic', 'work-atomic', 'hash-atomic',
			12, 'image_folder', 'ready', 'go-reader-manifest-v3', '2026-07-11T00:00:00Z'
		);
	`); err != nil {
		t.Fatal(err)
	}

	newer := postJSON(t, s, "/api/progress", map[string]any{
		"candidate_id":     "work-atomic",
		"page_manifest_id": "manifest-atomic",
		"manifest_hash":    "hash-atomic",
		"index":            4,
		"count":            12,
		"reader_fit_mode":  "fit-width",
		"stage_scroll_top": 360,
		"updated_at":       "2026-07-11T06:00:00.200Z",
	})
	if newer["stored"] != true {
		t.Fatalf("newer write stored = %v, want true: %#v", newer["stored"], newer)
	}

	for name, updatedAt := range map[string]string{
		"older": "2026-07-11T06:00:00.100Z",
		"equal": "2026-07-11T06:00:00.200Z",
	} {
		t.Run(name, func(t *testing.T) {
			rejected := postJSON(t, s, "/api/progress", map[string]any{
				"candidate_id":     "work-atomic",
				"page_manifest_id": "manifest-atomic",
				"manifest_hash":    "hash-atomic",
				"index":            11,
				"count":            12,
				"completed":        true,
				"reader_fit_mode":  "fit-page",
				"updated_at":       updatedAt,
			})
			if rejected["stored"] != false {
				t.Fatalf("%s completed write stored = %v, want false: %#v", name, rejected["stored"], rejected)
			}
			progress := rejected["progress"].(map[string]any)
			if intValue(progress["index"]) != 4 || boolValue(progress["completed"]) || progress["reader_fit_mode"] != "fit-width" || intValue(progress["stage_scroll_top"]) != 360 || progress["updated_at"] != "2026-07-11T06:00:00.200Z" {
				t.Fatalf("%s completed write changed current progress: %#v", name, progress)
			}
		})
	}

	progressRows, err := s.query(`
		SELECT last_page_index, completed, reader_fit_mode, stage_scroll_top, updated_at
		FROM reading_progress
		WHERE reader_profile_key = 'default'
		  AND work_identity_id = 'identity-atomic'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(progressRows) != 1 || intValue(progressRows[0]["last_page_index"]) != 4 || intValue(progressRows[0]["completed"]) != 0 || progressRows[0]["updated_at"] != "2026-07-11T06:00:00.200Z" {
		t.Fatalf("database progress regressed: %#v", progressRows)
	}
	markRows, err := s.query(`
		SELECT read_status
		FROM work_user_marks
		WHERE reader_profile_key = 'default'
		  AND work_identity_id = 'identity-atomic'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(markRows) != 1 || markRows[0]["read_status"] != "reading" {
		t.Fatalf("rejected completed write changed the accepted reading status: %#v", markRows)
	}
}

func TestSeriesProgressUsesSavedCandidateSeriesMembership(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`
		CREATE TABLE work_browse (
			candidate_id TEXT PRIMARY KEY,
			work_identity_id TEXT NOT NULL,
			title TEXT NOT NULL,
			source_kind TEXT NOT NULL DEFAULT 'archive'
		);
		CREATE TABLE work_identities (
			work_identity_id TEXT PRIMARY KEY,
			current_candidate_id TEXT NOT NULL
		);
		CREATE TABLE series_items (
			group_id TEXT NOT NULL,
			candidate_id TEXT NOT NULL,
			series_title TEXT NOT NULL DEFAULT '',
			item_role TEXT NOT NULL DEFAULT '',
			sequence_number TEXT NOT NULL DEFAULT '',
			sort_key TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO work_browse (candidate_id, work_identity_id, title, source_kind)
		VALUES
			('saved-chapter', 'identity-1', 'Saved Chapter', 'archive'),
			('current-chapter', 'identity-1', 'Current Chapter', 'archive');
		INSERT INTO work_identities (work_identity_id, current_candidate_id)
		VALUES ('identity-1', 'current-chapter');
		INSERT INTO series_items (group_id, candidate_id, series_title, item_role, sequence_number, sort_key)
		VALUES ('series-1', 'saved-chapter', 'Series One', 'chapter', '3', '003');
		INSERT INTO page_manifests (
			page_manifest_id, work_identity_id, candidate_id, manifest_hash,
			page_count, source_kind, manifest_status, builder_version, built_at
		) VALUES (
			'manifest-saved', 'identity-1', 'saved-chapter', 'hash-saved',
			11, 'archive', 'ready', 'go-reader-manifest-v3', '2026-06-04T00:00:00Z'
		);
		INSERT INTO reading_progress (
			reader_profile_key, work_identity_id, candidate_id, page_manifest_id,
			manifest_hash_snapshot, progress_status, last_page_index, progress_percent,
			completed, page_count_snapshot, last_read_at, created_at, updated_at
		) VALUES (
			'default', 'identity-1', 'saved-chapter', 'manifest-saved',
			'hash-saved', 'normal', 3, 36.36, 0, 11,
			'2026-07-05T01:00:00Z', '2026-07-05T01:00:00Z', '2026-07-05T01:00:00Z'
		);
	`); err != nil {
		t.Fatal(err)
	}

	payload := getJSON(t, s, "/api/series-progress?id=series-1")
	progress, ok := payload["progress"].(map[string]any)
	if !ok {
		t.Fatalf("series progress missing: %#v", payload)
	}
	if progress["candidate_id"] != "saved-chapter" || intValue(progress["index"]) != 3 || intValue(progress["count"]) != 11 {
		t.Fatalf("series progress = %#v, want saved chapter page 4/11", progress)
	}

	if _, err := s.db.Exec(`
		INSERT INTO work_browse (candidate_id, work_identity_id, title, source_kind)
		VALUES ('later-chapter', 'identity-2', 'Later Chapter', 'archive');
		INSERT INTO work_identities (work_identity_id, current_candidate_id)
		VALUES ('identity-2', 'later-chapter');
		INSERT INTO series_items (group_id, candidate_id, series_title, item_role, sequence_number, sort_key)
		VALUES ('series-1', 'later-chapter', 'Series One', 'chapter', '5', '005');
		INSERT INTO page_manifests (
			page_manifest_id, work_identity_id, candidate_id, manifest_hash,
			page_count, source_kind, manifest_status, builder_version, built_at
		) VALUES (
			'manifest-later', 'identity-2', 'later-chapter', 'hash-later',
			12, 'archive', 'ready', 'go-reader-manifest-v3', '2026-06-04T00:00:00Z'
		);
		INSERT INTO reading_progress (
			reader_profile_key, work_identity_id, candidate_id, page_manifest_id,
			manifest_hash_snapshot, progress_status, last_page_index, progress_percent,
			completed, page_count_snapshot, last_read_at, created_at, updated_at
		) VALUES (
			'default', 'identity-2', 'later-chapter', 'manifest-later',
			'hash-later', 'normal', 2, 25.0, 0, 12,
			'2026-07-04T01:00:00Z', '2026-07-04T01:00:00Z', '2026-07-04T01:00:00Z'
		);
	`); err != nil {
		t.Fatal(err)
	}

	payload = getJSON(t, s, "/api/series-progress?id=series-1")
	progress, ok = payload["progress"].(map[string]any)
	if !ok {
		t.Fatalf("series progress missing after later chapter insert: %#v", payload)
	}
	if progress["candidate_id"] != "saved-chapter" || intValue(progress["index"]) != 3 || intValue(progress["count"]) != 11 {
		t.Fatalf("series progress = %#v, want most recently read saved chapter page 4/11", progress)
	}

	if _, err := s.db.Exec(`
		UPDATE reading_progress
		SET last_page_index = 0,
			progress_percent = 8.33,
			last_read_at = '2026-07-06T01:00:00Z',
			updated_at = '2026-07-06T01:00:00Z'
		WHERE reader_profile_key = 'default'
		  AND candidate_id = 'later-chapter'
	`); err != nil {
		t.Fatal(err)
	}
	payload = getJSON(t, s, "/api/series-progress?id=series-1")
	progress, ok = payload["progress"].(map[string]any)
	if !ok {
		t.Fatalf("series progress missing after page-one update: %#v", payload)
	}
	if progress["candidate_id"] != "saved-chapter" || intValue(progress["index"]) != 3 {
		t.Fatalf("series progress = %#v, newer page-one open must not replace meaningful saved chapter", progress)
	}

}

func TestWorkDetailIncludesRelatedWorks(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`
		CREATE TABLE work_browse (
			candidate_id TEXT PRIMARY KEY,
			work_identity_id TEXT NOT NULL,
			library_key TEXT NOT NULL DEFAULT 'doujin-lanraragi',
			library_name TEXT NOT NULL DEFAULT '同人本',
			candidate_type TEXT NOT NULL DEFAULT 'doujin',
			source_kind TEXT NOT NULL DEFAULT 'archive',
			title TEXT NOT NULL,
			path TEXT NOT NULL DEFAULT '',
			relative_path TEXT NOT NULL DEFAULT '',
			size_bytes INTEGER NOT NULL DEFAULT 0,
			extension TEXT NOT NULL DEFAULT '.zip',
			page_count_status TEXT NOT NULL DEFAULT 'counted',
			readable_page_count INTEGER NOT NULL DEFAULT 10,
			cover_status TEXT NOT NULL DEFAULT 'missing',
			cover_kind TEXT NOT NULL DEFAULT 'none',
			translation_sources TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE series_items (
			group_id TEXT NOT NULL,
			candidate_id TEXT NOT NULL,
			series_title TEXT NOT NULL DEFAULT '',
			item_role TEXT NOT NULL DEFAULT '',
			sequence_number TEXT NOT NULL DEFAULT '',
			sort_key TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE doujin_series_items (
			group_id TEXT NOT NULL,
			candidate_id TEXT NOT NULL,
			creator_display TEXT NOT NULL DEFAULT '',
			series_title TEXT NOT NULL DEFAULT '',
			sequence_label TEXT NOT NULL DEFAULT '',
			sequence_kind TEXT NOT NULL DEFAULT '',
			sort_key TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE doujin_creator_items (
			creator_group_id TEXT NOT NULL,
			candidate_id TEXT NOT NULL,
			library_key TEXT NOT NULL DEFAULT '',
			creator_display TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			parsed_title TEXT NOT NULL DEFAULT '',
			event TEXT NOT NULL DEFAULT '',
			parody TEXT NOT NULL DEFAULT '',
			relative_path TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE translation_items (
			candidate_id TEXT NOT NULL,
			translation_group TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL DEFAULT '',
			action_reason TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO work_browse (candidate_id, work_identity_id, title, relative_path)
		VALUES
			('work-1', 'identity-1', 'Current', 'current.zip'),
			('work-series', 'identity-series', 'Same Series', 'same-series.zip'),
			('work-creator', 'identity-creator', 'Same Creator', 'same-creator.zip'),
			('work-title-current', 'identity-title-current', '(C104) [SyntheticCircleAlpha、SyntheticArtistBeta] Synthetic Orbit Chronicle 第9009话 [Synthetic汉化Alpha]', 'synthetic-title-current.zip'),
			('work-title-vol1', 'identity-title-vol1', '[SyntheticCircleAlpha] Synthetic Orbit Chronicle 第9001-9007话 [Synthetic汉化Alpha]', 'synthetic-title-vol1.zip'),
			('work-title-series', 'identity-title-series', '[SyntheticCircleAlpha] Synthetic Orbit Chronicle 第9008话至9094话 [Synthetic汉化Alpha]', 'synthetic-title-series.zip'),
			('work-title-ch96', 'identity-title-ch96', '[SyntheticCircleAlpha] Synthetic Orbit Chronicle 第9096话 [Synthetic汉化Alpha]', 'synthetic-title-ch9096.zip'),
			('work-title-ch99', 'identity-title-ch99', '[SyntheticCircleAlpha] Synthetic Orbit Chronicle 第9099话 [Synthetic汉化Alpha]', 'synthetic-title-ch9099.zip'),
			('work-title-author', 'identity-title-author', '[SyntheticCircleAlpha] Synthetic Side Story 第9001话', 'synthetic-title-author.zip'),
			('work-title-other-author', 'identity-title-other-author', '[SyntheticCircleOther] Synthetic Orbit Chronicle 第9010话', 'synthetic-title-other-author.zip'),
			('work-crimson-current', 'identity-crimson-current', '[SyntheticCrimsonCircle] Synthetic Puzzle', 'synthetic-crimson-current.zip'),
			('work-crimson-true-author', 'identity-crimson-true-author', '[SyntheticCrimsonCircle] Synthetic Alternate Story', 'synthetic-crimson-author.zip'),
			('work-crimson-title-hit', 'identity-crimson-title-hit', '[SyntheticOtherCircle] About SyntheticCrimsonCircle 9001', 'synthetic-crimson-title-hit.zip'),
			('work-doujin-current', 'identity-doujin-current', 'Doujin Current', 'doujin-current.zip'),
			('work-doujin-b', 'identity-doujin-b', 'Tie Related', 'doujin-b.zip'),
			('work-doujin-a', 'identity-doujin-a', 'Tie Related', 'doujin-a.zip');
		UPDATE work_browse SET readable_page_count = 14 WHERE candidate_id = 'work-1';
		UPDATE work_browse SET readable_page_count = 100 WHERE candidate_id = 'work-doujin-b';
		INSERT INTO series_items (group_id, candidate_id, series_title, item_role, sequence_number, sort_key)
		VALUES
			('series-1', 'work-1', 'Series One', 'volume', '1', '001'),
			('series-1', 'work-series', 'Series One', 'volume', '2', '002');
		INSERT INTO doujin_series_items (group_id, candidate_id, creator_display, series_title, sequence_label, sequence_kind, sort_key)
		VALUES
			('doujin-tie', 'work-doujin-current', 'Tie Creator', 'Tie Series', '', '', '000'),
			('doujin-tie', 'work-doujin-b', 'Tie Creator', 'Tie Series', '', '', '001'),
			('doujin-tie', 'work-doujin-a', 'Tie Creator', 'Tie Series', '', '', '001');
		INSERT INTO doujin_creator_items (creator_group_id, candidate_id, library_key, creator_display, title, parsed_title, event, parody, relative_path)
		VALUES
			('creator-1', 'work-1', 'doujin-lanraragi', 'Creator One', 'Current', '', '', '', 'current.zip'),
			('creator-1', 'work-creator', 'doujin-lanraragi', 'Creator One', 'Same Creator', '', '', '', 'same-creator.zip');
		INSERT INTO reading_progress (
			reader_profile_key, work_identity_id, candidate_id, page_manifest_id,
			manifest_hash_snapshot, progress_status, last_page_index, progress_percent,
			completed, page_count_snapshot, last_read_at, created_at, updated_at
		) VALUES
			(
				'default', 'identity-1', 'work-1', 'manifest-current',
				'hash-current', 'normal', 6, 50, 0, 14,
				'2026-06-06T11:00:00Z', '2026-06-06T11:00:00Z', '2026-06-06T11:00:00Z'
			),
			(
				'default', 'identity-series', 'work-series', 'manifest-series',
				'hash-series', 'normal', 3, 40, 0, 10,
				'2026-06-06T10:00:00Z', '2026-06-06T10:00:00Z', '2026-06-06T10:00:00Z'
			);
	`); err != nil {
		t.Fatal(err)
	}

	payload := getJSON(t, s, "/api/work?id=work-1")
	work := payload["work"].(map[string]any)
	currentProgress, ok := work["progress"].(map[string]any)
	if !ok {
		t.Fatalf("current work progress missing: %#v", work)
	}
	if intValue(currentProgress["index"]) != 6 || intValue(currentProgress["count"]) != 14 {
		t.Fatalf("current work progress = %#v, want page 7/14", currentProgress)
	}
	related := payload["related"].(map[string]any)
	editionItems := related["editions"].([]any)
	if len(editionItems) != 0 {
		t.Fatalf("edition related count = %d, want 0", len(editionItems))
	}
	seriesItems := related["series"].([]any)
	if len(seriesItems) != 1 {
		t.Fatalf("series related count = %d, want 1", len(seriesItems))
	}
	if got := seriesItems[0].(map[string]any)["candidate_id"]; got != "work-series" {
		t.Fatalf("series related candidate = %v, want work-series", got)
	}
	seriesProgress, ok := seriesItems[0].(map[string]any)["progress"].(map[string]any)
	if !ok {
		t.Fatalf("series related progress missing: %#v", seriesItems[0])
	}
	if intValue(seriesProgress["index"]) != 3 || intValue(seriesProgress["count"]) != 10 {
		t.Fatalf("series related progress = %#v, want index 3 count 10", seriesProgress)
	}
	creatorItems := related["creators"].([]any)
	if len(creatorItems) != 1 {
		t.Fatalf("creator related count = %d, want 1", len(creatorItems))
	}
	if got := creatorItems[0].(map[string]any)["candidate_id"]; got != "work-creator" {
		t.Fatalf("creator related candidate = %v, want work-creator", got)
	}

	payload = getJSON(t, s, "/api/work?id=work-title-current")
	titleHints := payload["title_hints"].(map[string]any)
	if got := titleHints["series"]; got != "Synthetic Orbit Chronicle" {
		t.Fatalf("title_hints.series = %v, want Synthetic Orbit Chronicle", got)
	}
	hintCreators := titleHints["creators"].([]any)
	if len(hintCreators) != 2 || hintCreators[0] != "SyntheticCircleAlpha" || hintCreators[1] != "SyntheticArtistBeta" {
		t.Fatalf("title_hints.creators = %#v, want the two synthetic creators", hintCreators)
	}
	related = payload["related"].(map[string]any)
	seriesItems = related["series"].([]any)
	gotTitleSeriesIDs := make([]string, 0, len(seriesItems))
	for _, raw := range seriesItems {
		gotTitleSeriesIDs = append(gotTitleSeriesIDs, stringValue(raw.(map[string]any)["candidate_id"]))
	}
	wantTitleSeriesIDs := []string{"work-title-vol1", "work-title-series", "work-title-ch96", "work-title-ch99"}
	if strings.Join(gotTitleSeriesIDs, "\n") != strings.Join(wantTitleSeriesIDs, "\n") {
		t.Fatalf("title fallback series order = %#v, want %#v", gotTitleSeriesIDs, wantTitleSeriesIDs)
	}
	foundTitleSeries := false
	for _, raw := range seriesItems {
		item := raw.(map[string]any)
		if item["candidate_id"] == "work-title-other-author" {
			t.Fatalf("title fallback series included another author's matching title")
		}
		if item["candidate_id"] == "work-title-series" {
			foundTitleSeries = true
			if item["relation_kind"] != "title_series" {
				t.Fatalf("title fallback series relation_kind = %v, want title_series", item["relation_kind"])
			}
		}
	}
	if !foundTitleSeries {
		t.Fatalf("title fallback series did not include work-title-series: %#v", seriesItems)
	}
	creatorItems = related["creators"].([]any)
	foundTitleAuthor := false
	for _, raw := range creatorItems {
		item := raw.(map[string]any)
		if item["candidate_id"] == "work-title-series" {
			t.Fatalf("creator related duplicated same-series candidate")
		}
		if item["candidate_id"] == "work-title-author" {
			foundTitleAuthor = true
			if item["relation_kind"] != "title_creator" {
				t.Fatalf("title fallback creator relation_kind = %v, want title_creator", item["relation_kind"])
			}
		}
	}
	if !foundTitleAuthor {
		t.Fatalf("title fallback creator did not include work-title-author: %#v", creatorItems)
	}

	payload = getJSON(t, s, "/api/work?id=work-doujin-current")
	related = payload["related"].(map[string]any)
	seriesItems = related["series"].([]any)
	gotDoujinSeriesIDs := make([]string, 0, len(seriesItems))
	for _, raw := range seriesItems {
		item := raw.(map[string]any)
		gotDoujinSeriesIDs = append(gotDoujinSeriesIDs, stringValue(item["candidate_id"]))
		if item["relation_kind"] != "doujin_series" {
			t.Fatalf("doujin series relation_kind = %v, want doujin_series", item["relation_kind"])
		}
	}
	wantDoujinSeriesIDs := []string{"work-doujin-a", "work-doujin-b"}
	if strings.Join(gotDoujinSeriesIDs, "\n") != strings.Join(wantDoujinSeriesIDs, "\n") {
		t.Fatalf("doujin series tie order = %#v, want %#v", gotDoujinSeriesIDs, wantDoujinSeriesIDs)
	}

	payload = getJSON(t, s, "/api/work?id=work-crimson-current")
	related = payload["related"].(map[string]any)
	creatorItems = related["creators"].([]any)
	foundExactCreator := false
	for _, raw := range creatorItems {
		item := raw.(map[string]any)
		switch item["candidate_id"] {
		case "work-crimson-true-author":
			foundExactCreator = true
			if item["relation_kind"] != "title_creator" {
				t.Fatalf("exact title creator relation_kind = %v, want title_creator", item["relation_kind"])
			}
		case "work-crimson-title-hit":
			t.Fatalf("title fallback creator included title-body substring hit: %#v", item)
		}
	}
	if !foundExactCreator {
		t.Fatalf("title fallback creator did not include exact leading creator match: %#v", creatorItems)
	}
}

func TestFastWorkSearchMatchesStructuredCreatorWithAndWithoutLocalIndex(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		name := "fallback"
		if indexed {
			name = "local_index"
		}
		t.Run(name, func(t *testing.T) {
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(`
				CREATE TABLE work_candidates (
					candidate_id TEXT PRIMARY KEY,
					title TEXT NOT NULL DEFAULT '',
					relative_path TEXT NOT NULL DEFAULT ''
				);
				CREATE TABLE series_groups (group_id TEXT PRIMARY KEY);
				CREATE TABLE local_search_index (
					target_type TEXT NOT NULL,
					target_id TEXT NOT NULL,
					search_text TEXT NOT NULL
				);
				CREATE TABLE work_identities (
					work_identity_id TEXT PRIMARY KEY,
					current_candidate_id TEXT NOT NULL
				);
				CREATE TABLE metadata_field_overrides (
					work_identity_id TEXT NOT NULL,
					override_status TEXT NOT NULL,
					field_value TEXT NOT NULL
				);
				CREATE TABLE translation_items (candidate_id TEXT NOT NULL, translation_group TEXT NOT NULL DEFAULT '');
				CREATE TABLE series_items (candidate_id TEXT NOT NULL, series_title TEXT NOT NULL DEFAULT '');
				CREATE TABLE doujin_creator_items (candidate_id TEXT NOT NULL, creator_display TEXT NOT NULL DEFAULT '');
				INSERT INTO work_candidates (candidate_id, title, relative_path) VALUES
					('creator-match', 'Opaque Work', 'opaque-work.zip'),
					('other-work', 'Other Work', 'other-work.zip');
				INSERT INTO doujin_creator_items (candidate_id, creator_display) VALUES
					('creator-match', '结构化作者'),
					('other-work', '另一位作者');
			`); err != nil {
				t.Fatal(err)
			}
			if indexed {
				if _, err := db.Exec(`
					INSERT INTO local_search_index (target_type, target_id, search_text) VALUES
						('work', 'creator-match', 'opaque work'),
						('work', 'other-work', 'other work');
				`); err != nil {
					t.Fatal(err)
				}
			}
			s := &Server{db: db}
			query, args := s.fastWorkSearchMatchQuery("结构化作者")
			rows, err := s.query(query, args...)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || stringValue(rows[0]["candidate_id"]) != "creator-match" {
				t.Fatalf("structured creator search rows = %#v, want creator-match", rows)
			}
		})
	}
}

func TestFillMissingStructuredDisplayCreators(t *testing.T) {
	t.Run("list details fill opaque titles without overwriting title creators", func(t *testing.T) {
		s := newCatalogTestServer(t)
		defer s.Close()

		seedCatalogWork(t, s, "opaque-work", "Opaque catalogue entry", "opaque-work.zip", "")
		seedCatalogWork(t, s, "title-work", "[Title Creator] Existing Work", "title-work.zip", "Structured Creator")
		if _, err := s.db.Exec(`
			INSERT INTO doujin_creator_items (candidate_id, creator_display) VALUES
				('opaque-work', 'Beta'),
				('opaque-work', 'Alpha'),
				('opaque-work', 'alpha'),
				('opaque-work', '乙社')
		`); err != nil {
			t.Fatal(err)
		}

		var beforeCount int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM doujin_creator_items`).Scan(&beforeCount); err != nil {
			t.Fatal(err)
		}
		rows, err := s.loadWorkListDetails([]string{"opaque-work", "title-work"})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("list detail count = %d, want 2", len(rows))
		}
		if got := stringValue(rows[0]["display_creator"]); got != "Alpha / Beta / 乙社" {
			t.Fatalf("opaque structured display_creator = %q, want stable deduplicated creators", got)
		}
		if got := stringValue(rows[1]["display_creator"]); got != "Title Creator" {
			t.Fatalf("title-derived display_creator overwritten with %q", got)
		}
		var afterCount int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM doujin_creator_items`).Scan(&afterCount); err != nil {
			t.Fatal(err)
		}
		if afterCount != beforeCount {
			t.Fatalf("structured creator fallback mutated database: count %d -> %d", beforeCount, afterCount)
		}

		correctedRows := []map[string]any{{
			"candidate_id": "opaque-work",
			"title":        "Opaque catalogue entry",
			"metadata_overrides": map[string]any{
				"author": map[string]any{"field_value": "Corrected Author"},
			},
		}}
		enrichWork(correctedRows[0])
		if err := s.fillMissingStructuredDisplayCreators(correctedRows); err != nil {
			t.Fatal(err)
		}
		if got := stringValue(correctedRows[0]["display_creator"]); got != "Corrected Author" {
			t.Fatalf("metadata-corrected display_creator overwritten with %q", got)
		}
	})

	t.Run("missing structured creator table is a no-op", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		s := &Server{db: db}
		rows := []map[string]any{{"candidate_id": "opaque-work", "display_creator": ""}}
		if err := s.fillMissingStructuredDisplayCreators(rows); err != nil {
			t.Fatal(err)
		}
		if got := stringValue(rows[0]["display_creator"]); got != "" {
			t.Fatalf("missing-table fallback changed display_creator to %q", got)
		}
	})
}

func TestTitleHintsSkipEventPrefix(t *testing.T) {
	hints := titleHintsFromWork(map[string]any{
		"title": "(C104) [SyntheticCircleAlpha、SyntheticArtistBeta] Synthetic Orbit Chronicle 第9009话 [Synthetic汉化Alpha]",
	})
	if got := strings.Join(hints.creators, " / "); got != "SyntheticCircleAlpha / SyntheticArtistBeta" {
		t.Fatalf("creators = %q, want the two synthetic creators", got)
	}
	if hints.series != "Synthetic Orbit Chronicle" {
		t.Fatalf("series = %q, want Synthetic Orbit Chronicle", hints.series)
	}

	hints = titleHintsFromWork(map[string]any{
		"title": "(C105) [SyntheticCircleGamma (SyntheticArtistDelta)] Synthetic Harbor Letters 第9002话 [Synthetic翻译Beta]",
	})
	if got := strings.Join(hints.creators, " / "); got != "SyntheticCircleGamma (SyntheticArtistDelta)" {
		t.Fatalf("parenthesized creator = %q, want the synthetic circle and artist", got)
	}
	if hints.series != "Synthetic Harbor Letters" {
		t.Fatalf("parenthesized creator series = %q, want Synthetic Harbor Letters", hints.series)
	}

	work := map[string]any{
		"title":          "[SyntheticCircleEpsilon (SyntheticArtistZeta)] Synthetic Short Story [Synthetic汉化Alpha]",
		"candidate_type": "doujin",
	}
	enrichWork(work)
	if got := stringValue(work["display_creator"]); got != "SyntheticCircleEpsilon (SyntheticArtistZeta)" {
		t.Fatalf("display creator = %q, want the synthetic title creator", got)
	}

	work = map[string]any{
		"title": "[SyntheticLegacyCircle] Synthetic Work",
		"metadata_overrides": map[string]any{
			"circle": map[string]any{"field_value": "SyntheticOverrideCircle"},
			"author": map[string]any{"field_value": "SyntheticOverrideAuthor"},
		},
	}
	enrichWork(work)
	if got := stringValue(work["display_creator"]); got != "SyntheticOverrideCircle (SyntheticOverrideAuthor)" {
		t.Fatalf("overridden display creator = %q, want the synthetic override pair", got)
	}
}

func TestTitleCreatorHintsKeepNamesContainingAIAndRejectMetadata(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{name: "latin name containing ai", title: "[Kairi Synthetic Studio] Synthetic Work", want: "Kairi Synthetic Studio"},
		{name: "latin name with alias", title: "[MAIKA Synthetic (Alias)] Synthetic Work", want: "MAIKA Synthetic (Alias)"},
		{name: "short name containing ai", title: "[Aiko Synthetic] Synthetic Work", want: "Aiko Synthetic"},
		{name: "bare AI translation metadata", title: "[AI translation] Synthetic Work", want: ""},
		{name: "AI inside longer name", title: "[Synthetic Studio AI Lab] Synthetic Work", want: "Synthetic Studio AI Lab"},
		{name: "translation metadata", title: "[Synthetic汉化Metadata] Synthetic Work", want: ""},
		{name: "numeric circle", title: "[9041] Synthetic Work", want: "9041"},
		{name: "nested round bracket", title: "([SyntheticCircleTheta] Synthetic Work) remainder", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hints := titleHintsFromWork(map[string]any{"title": test.title})
			if got := strings.Join(hints.creators, " / "); got != test.want {
				t.Fatalf("creator hints = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTitleTranslationSourcesCoverCommonMarkers(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{name: "square brackets", title: "Synthetic Work [Synthetic汉化Alpha]", want: "Synthetic汉化Alpha"},
		{name: "fullwidth brackets", title: "Synthetic Work ［Synthetic翻译Beta］", want: "Synthetic翻译Beta"},
		{name: "lenticular brackets", title: "Synthetic Work 【Synthetic中文Gamma】", want: "Synthetic中文Gamma"},
		{name: "parentheses", title: "Synthetic Work (Synthetic翻訳Delta)", want: "Synthetic翻訳Delta"},
		{name: "bare translation phrase", title: "Synthetic Work 正文中文" + "翻译尾注", want: "中文" + "翻译"},
		{name: "english marker", title: "Synthetic Work [Synthetic Chinese Team]", want: "Synthetic Chinese Team"},
		{name: "deduplicates identical tags", title: "Synthetic Work [Synthetic汉化Alpha][Synthetic汉化Alpha]", want: "Synthetic汉化Alpha"},
		{name: "keeps distinct tags in order", title: "Synthetic Work [Synthetic汉化Alpha][Synthetic翻译Beta]", want: "Synthetic汉化Alpha / Synthetic翻译Beta"},
		{name: "machine translation marker", title: "Synthetic Work [Synthetic机翻Epsilon]", want: "Synthetic机翻Epsilon"},
		{name: "nested tag requires review", title: "Synthetic Work [Synthetic[Inner]汉化]", want: ""},
		{name: "unclosed tag is ignored", title: "Synthetic Work [Synthetic汉化Open", want: ""},
		{name: "ordinary tag is ignored", title: "Synthetic Work [Synthetic Group]", want: ""},
		{name: "title without marker is ignored", title: "Synthetic Work", want: ""},
		{name: "explicit beats bare", title: "【Synthetic汉化Alpha】Synthetic Work 正文中文" + "翻译尾注", want: "Synthetic汉化Alpha"},
		{name: "deduplicate case", title: "[Synthetic Chinese Team]【SYNTHETIC CHINESE TEAM】Synthetic Work", want: "Synthetic Chinese Team"},
		{name: "balanced qualifier", title: "Synthetic Work 【Synthetic翻訳Beta (简体) 版】", want: "Synthetic翻訳Beta (简体) 版"},
		{name: "cross bracket rejected", title: "Synthetic Work 【Synthetic翻訳Beta] [Unrelated]", want: ""},
		{name: "rejected nested tag cannot fall back", title: "Synthetic Work [正文中文" + "翻译 [nested] marker]", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := strings.Join(titleTranslationSources(test.title), " / "); got != test.want {
				t.Fatalf("translation sources = %q, want %q", got, test.want)
			}
		})
	}

	work := map[string]any{"title": "Synthetic Work [Synthetic汉化Alpha]"}
	enrichWork(work)
	if got := stringValue(work["translation_sources"]); got != "Synthetic汉化Alpha" {
		t.Fatalf("enriched translation_sources = %q, want Synthetic汉化Alpha", got)
	}

	work = map[string]any{
		"title":               "Synthetic Work [Synthetic汉化Alpha]",
		"translation_sources": "",
		"metadata_overrides": map[string]any{
			"translation_sources": map[string]any{"field_value": ""},
		},
	}
	enrichWork(work)
	if got := stringValue(work["translation_sources"]); got != "" {
		t.Fatalf("explicit blank translation override was replaced with %q", got)
	}
}

func TestFilterRelatedWorksForCurrentRemovesDuplicateVersions(t *testing.T) {
	current := map[string]any{
		"candidate_id":        "current",
		"title":               "[SyntheticCircleAlpha (SyntheticArtistBeta)] Synthetic Moon Sample [Synthetic汉化Alpha] [v2]",
		"readable_page_count": 40,
		"work_identity_id":    "identity-current",
	}
	items := []map[string]any{
		{
			"candidate_id":        "duplicate-v3",
			"title":               "[SyntheticCircleAlpha (SyntheticArtistBeta)] Synthetic Moon Sample [Synthetic汉化Alpha] [v3]",
			"readable_page_count": 40,
		},
		{
			"candidate_id":        "same-creator-other",
			"title":               "[SyntheticCircleAlpha (SyntheticArtistBeta)] Synthetic Other Work [Synthetic汉化Alpha] [v2]",
			"readable_page_count": 37,
		},
	}

	filtered := filterRelatedWorksForCurrent(items, current)
	if len(filtered) != 1 || filtered[0]["candidate_id"] != "same-creator-other" {
		t.Fatalf("filtered related = %#v, want only same-creator-other", filtered)
	}
}

func TestRelatedEditionVariantsForCurrentKeepsCloseVersionsVisible(t *testing.T) {
	current := map[string]any{
		"candidate_id":        "current",
		"title":               "[作者] 同一作品 [中国翻訳]",
		"readable_page_count": 59,
	}
	items := []map[string]any{
		{
			"candidate_id":        "edition-a",
			"work_identity_id":    "identity-a",
			"title":               "[作者] 同一作品 [別翻訳] (1)",
			"readable_page_count": 53,
		},
		{
			"candidate_id":        "edition-a-repeat",
			"work_identity_id":    "identity-a",
			"title":               "[作者] 同一作品 [別翻訳] (2)",
			"readable_page_count": 53,
		},
		{
			"candidate_id":        "other",
			"work_identity_id":    "identity-other",
			"title":               "[作者] 別作品",
			"readable_page_count": 53,
		},
	}

	editions := relatedEditionVariantsForCurrent(items, current)
	if len(editions) != 1 || editions[0]["candidate_id"] != "edition-a" {
		t.Fatalf("edition variants = %#v, want only edition-a", editions)
	}
}

func TestFilterRelatedWorksForCurrentRemovesCoveredCollectionParts(t *testing.T) {
	current := map[string]any{
		"candidate_id":        "current-full",
		"title":               "[SyntheticCircleAlpha] Synthetic Star Collection [全话]",
		"readable_page_count": 76,
	}
	items := []map[string]any{
		{
			"candidate_id":        "partial",
			"title":               "[SyntheticCircleAlpha] Synthetic Star Collection 9001-9052 [Synthetic汉化Alpha]",
			"readable_page_count": 52,
		},
		{
			"candidate_id":        "other-series",
			"title":               "[SyntheticCircleAlpha] Synthetic Harbor Collection 9002-9045 [Synthetic汉化Alpha]",
			"readable_page_count": 45,
		},
	}

	filtered := filterRelatedWorksForCurrent(items, current)
	if len(filtered) != 1 || filtered[0]["candidate_id"] != "other-series" {
		t.Fatalf("filtered collection parts = %#v, want only other-series", filtered)
	}
}

func TestFilterRelatedWorksForCurrentKeepsDifferentVolumes(t *testing.T) {
	current := map[string]any{
		"candidate_id":        "current-vol1",
		"title":               "[作者] 作品 第1巻 [中国翻訳]",
		"readable_page_count": 120,
	}
	items := []map[string]any{
		{
			"candidate_id":        "vol2",
			"title":               "[作者] 作品 第2巻 [中国翻訳]",
			"readable_page_count": 118,
		},
	}

	filtered := filterRelatedWorksForCurrent(items, current)
	if len(filtered) != 1 || filtered[0]["candidate_id"] != "vol2" {
		t.Fatalf("filtered different volumes = %#v, want vol2 kept", filtered)
	}
}

func TestFilterRelatedWorksForCurrentKeepsDifferentBracketChapters(t *testing.T) {
	current := map[string]any{
		"candidate_id":        "current-ch3",
		"title":               "[SyntheticCircleAlpha] Synthetic Orbit Chronicle [第9003话] [Synthetic汉化Alpha] [v2]",
		"readable_page_count": 23,
	}
	items := []map[string]any{
		{
			"candidate_id":        "ch4",
			"title":               "[SyntheticCircleAlpha] Synthetic Orbit Chronicle [第9004话] [Synthetic汉化Alpha]",
			"readable_page_count": 23,
		},
		{
			"candidate_id":        "ch3-base",
			"title":               "[SyntheticCircleAlpha] Synthetic Orbit Chronicle [第9003话] [Synthetic汉化Alpha]",
			"readable_page_count": 23,
		},
	}

	filtered := filterRelatedWorksForCurrent(items, current)
	if len(filtered) != 1 || filtered[0]["candidate_id"] != "ch4" {
		t.Fatalf("filtered bracket chapters = %#v, want only ch4", filtered)
	}
}

func TestUserMarkPatchPreservesExistingFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`
		CREATE TABLE work_browse (
			candidate_id TEXT PRIMARY KEY,
			work_identity_id TEXT NOT NULL,
			title TEXT NOT NULL
		)
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`
		INSERT INTO work_browse (candidate_id, work_identity_id, title)
		VALUES ('work-1', 'identity-1', 'Test Work')
	`); err != nil {
		t.Fatal(err)
	}

	postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type":         "work",
		"target_id":           "work-1",
		"favorite":            true,
		"read_status":         "reading",
		"personal_rating":     8,
		"reread_priority":     2,
		"translation_quality": 4,
		"image_quality":       5,
		"notes":               "keep me",
	})
	postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work",
		"target_id":   "work-1",
		"read_status": "completed",
	})

	payload := getJSON(t, s, "/api/user-mark?target_type=work&target_id=work-1")
	mark := payload["mark"].(map[string]any)
	if mark["read_status"] != "completed" {
		t.Fatalf("read_status = %v, want completed", mark["read_status"])
	}
	if mark["favorite"] != true {
		t.Fatalf("favorite = %v, want true", mark["favorite"])
	}
	if int(mark["personal_rating"].(float64)) != 8 {
		t.Fatalf("personal_rating = %v, want 8", mark["personal_rating"])
	}
	if int(mark["reread_priority"].(float64)) != 2 {
		t.Fatalf("reread_priority = %v, want 2", mark["reread_priority"])
	}
	if int(mark["translation_quality"].(float64)) != 4 {
		t.Fatalf("translation_quality = %v, want 4", mark["translation_quality"])
	}
	if int(mark["image_quality"].(float64)) != 5 {
		t.Fatalf("image_quality = %v, want 5", mark["image_quality"])
	}
	if mark["notes"] != "keep me" {
		t.Fatalf("notes = %v, want keep me", mark["notes"])
	}
}

func TestExplicitUnreadClearsWorkProgressTransactionally(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`
		CREATE TABLE work_browse (
			candidate_id TEXT PRIMARY KEY,
			work_identity_id TEXT NOT NULL,
			title TEXT NOT NULL,
			source_kind TEXT NOT NULL DEFAULT 'image_folder'
		);
		CREATE TABLE work_identities (
			work_identity_id TEXT PRIMARY KEY,
			current_candidate_id TEXT NOT NULL
		);
		INSERT INTO work_browse (candidate_id, work_identity_id, title, source_kind)
		VALUES ('work-1', 'identity-1', 'Unread Reset Work', 'image_folder');
		INSERT INTO work_identities (work_identity_id, current_candidate_id)
		VALUES ('identity-1', 'work-1');
		INSERT INTO page_manifests (
			page_manifest_id, work_identity_id, candidate_id, manifest_hash,
			page_count, source_kind, manifest_status, builder_version, built_at
		) VALUES (
			'manifest-current', 'identity-1', 'work-1', 'hash-current',
			12, 'image_folder', 'ready', 'go-reader-manifest-v3', '2026-07-11T00:00:00Z'
		);
	`); err != nil {
		t.Fatal(err)
	}

	postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-1", "read_status": "reading", "notes": "keep me",
	})
	postJSON(t, s, "/api/progress", map[string]any{
		"candidate_id": "work-1", "page_manifest_id": "manifest-current", "manifest_hash": "hash-current", "index": 4, "count": 12,
		"updated_at": "2026-07-11T10:00:00Z",
	})
	postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-1", "favorite": true,
	})
	beforeReset := getJSON(t, s, "/api/progress?id=work-1")
	if beforeReset["progress"] == nil {
		t.Fatal("favorite-only patch cleared reading progress")
	}

	postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-1", "read_status": "unread", "updated_at": "2026-07-11T10:01:00Z",
	})
	afterReset := getJSON(t, s, "/api/progress?id=work-1")
	if afterReset["progress"] != nil {
		t.Fatalf("explicit unread preserved progress: %#v", afterReset["progress"])
	}
	markPayload := getJSON(t, s, "/api/user-mark?target_type=work&target_id=work-1")
	mark := markPayload["mark"].(map[string]any)
	if mark["read_status"] != "unread" || mark["favorite"] != true || mark["notes"] != "keep me" {
		t.Fatalf("explicit unread lost preserved mark fields: %#v", mark)
	}

	postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-1", "read_status": "reading", "updated_at": "2026-07-11T10:01:30Z",
	})
	postJSON(t, s, "/api/progress", map[string]any{
		"candidate_id": "work-1", "page_manifest_id": "manifest-current", "manifest_hash": "hash-current", "index": 5, "count": 12,
		"updated_at": "2026-07-11T10:02:00Z",
	})
	if _, err := s.db.Exec(`
		CREATE TRIGGER reject_unread_progress_delete
		BEFORE DELETE ON reading_progress
		BEGIN
			SELECT RAISE(ABORT, 'blocked progress delete');
		END
	`); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"target_type": "work", "target_id": "work-1", "read_status": "unread", "updated_at": "2026-07-11T10:03:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/user-mark", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(writeIntentHeader, writeIntentValue)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failed transactional unread status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	failedMarkPayload := getJSON(t, s, "/api/user-mark?target_type=work&target_id=work-1")
	failedMark := failedMarkPayload["mark"].(map[string]any)
	if failedMark["read_status"] != "reading" {
		t.Fatalf("failed unread partially updated mark: %#v", failedMark)
	}
	failedProgress := getJSON(t, s, "/api/progress?id=work-1")
	if failedProgress["progress"] == nil {
		t.Fatal("failed unread partially deleted progress")
	}
	failedResetRows, err := s.query(`
		SELECT reset_at FROM reading_progress_resets
		WHERE reader_profile_key = 'default' AND work_identity_id = 'identity-1'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(failedResetRows) != 0 {
		t.Fatalf("failed unread partially committed reset tombstone: %#v", failedResetRows)
	}
}

func newProgressClockTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := newServerWithoutCatalogForTest(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.db.Exec(`
		CREATE TABLE work_browse (
			candidate_id TEXT PRIMARY KEY,
			work_identity_id TEXT NOT NULL,
			title TEXT NOT NULL,
			source_kind TEXT NOT NULL DEFAULT 'image_folder'
		);
		CREATE TABLE work_identities (
			work_identity_id TEXT PRIMARY KEY,
			current_candidate_id TEXT NOT NULL
		);
		INSERT INTO work_browse (candidate_id, work_identity_id, title, source_kind)
		VALUES ('work-clock', 'identity-clock', 'Clocked Work', 'image_folder');
		INSERT INTO work_identities (work_identity_id, current_candidate_id)
		VALUES ('identity-clock', 'work-clock');
		INSERT INTO page_manifests (
			page_manifest_id, work_identity_id, candidate_id, manifest_hash,
			page_count, source_kind, manifest_status, builder_version, built_at
		) VALUES (
			'manifest-clock', 'identity-clock', 'work-clock', 'hash-clock',
			12, 'image_folder', 'ready', 'go-reader-manifest-v3', '2026-07-11T00:00:00Z'
		);
	`); err != nil {
		t.Fatal(err)
	}
	return s
}

func newUserMarkFieldClockTestServer(t *testing.T) *Server {
	t.Helper()
	s := newProgressClockTestServer(t)
	if _, err := s.db.Exec(`
		CREATE TABLE series_identities (
			series_identity_id TEXT PRIMARY KEY,
			current_group_id TEXT NOT NULL
		);
		INSERT INTO series_identities (series_identity_id, current_group_id)
		VALUES ('series-identity-clock', 'series-clock');
	`); err != nil {
		t.Fatal(err)
	}
	return s
}

func jsonStringList(t *testing.T, payload map[string]any, key string) []string {
	t.Helper()
	raw, ok := payload[key].([]any)
	if !ok {
		t.Fatalf("%s = %#v, want JSON array", key, payload[key])
	}
	values := make([]string, 0, len(raw))
	for _, value := range raw {
		values = append(values, stringValue(value))
	}
	return values
}

func TestProgressResetSchemaUpgradesExistingUserMarks(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE work_user_marks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			reader_profile_key TEXT NOT NULL,
			work_identity_id TEXT NOT NULL,
			candidate_id TEXT,
			read_status TEXT NOT NULL DEFAULT 'unread',
			personal_rating INTEGER,
			favorite INTEGER NOT NULL DEFAULT 0,
			reread_priority INTEGER NOT NULL DEFAULT 0,
			translation_quality INTEGER,
			image_quality INTEGER,
			hidden INTEGER NOT NULL DEFAULT 0,
			hidden_reason TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			marked_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (reader_profile_key, work_identity_id)
		);
		INSERT INTO work_user_marks (
			reader_profile_key, work_identity_id, candidate_id, read_status,
			marked_at, created_at, updated_at
		) VALUES ('default', 'legacy-identity', 'legacy-work', 'reading',
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		CREATE TABLE series_user_marks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			reader_profile_key TEXT NOT NULL,
			series_identity_id TEXT NOT NULL,
			group_id TEXT,
			read_status TEXT NOT NULL DEFAULT 'unread',
			personal_rating INTEGER,
			favorite INTEGER NOT NULL DEFAULT 0,
			reread_priority INTEGER NOT NULL DEFAULT 0,
			hidden INTEGER NOT NULL DEFAULT 0,
			hidden_reason TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			marked_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (reader_profile_key, series_identity_id)
		);
		INSERT INTO series_user_marks (
			reader_profile_key, series_identity_id, group_id, read_status,
			marked_at, created_at, updated_at
		) VALUES ('default', 'legacy-series-identity', 'legacy-series', 'completed',
			'2026-01-02T00:00:00Z', '2026-01-02T00:00:00Z', '2026-01-02T00:00:00Z');
	`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	columns, err := s.query(`PRAGMA table_info(work_user_marks)`)
	if err != nil {
		t.Fatal(err)
	}
	foundClock := false
	for _, column := range columns {
		if stringValue(column["name"]) == "read_status_client_updated_at" {
			foundClock = true
		}
	}
	if !foundClock {
		t.Fatal("existing work_user_marks schema was not upgraded with read_status_client_updated_at")
	}
	legacyRows, err := s.query(`
		SELECT read_status, read_status_client_updated_at
		FROM work_user_marks WHERE work_identity_id = 'legacy-identity'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacyRows) != 1 || legacyRows[0]["read_status"] != "reading" || stringValue(legacyRows[0]["read_status_client_updated_at"]) != "" {
		t.Fatalf("schema upgrade changed legacy mark: %#v", legacyRows)
	}
	seriesColumns, err := s.query(`PRAGMA table_info(series_user_marks)`)
	if err != nil {
		t.Fatal(err)
	}
	foundSeriesClock := false
	for _, column := range seriesColumns {
		if stringValue(column["name"]) == "read_status_client_updated_at" {
			foundSeriesClock = true
		}
	}
	if !foundSeriesClock {
		t.Fatal("existing series_user_marks schema was not upgraded with read_status_client_updated_at")
	}
	legacySeriesRows, err := s.query(`
		SELECT read_status, read_status_client_updated_at
		FROM series_user_marks WHERE series_identity_id = 'legacy-series-identity'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacySeriesRows) != 1 || legacySeriesRows[0]["read_status"] != "completed" || stringValue(legacySeriesRows[0]["read_status_client_updated_at"]) != "" {
		t.Fatalf("schema upgrade changed legacy series mark: %#v", legacySeriesRows)
	}
	resetTable, err := s.query(`
		SELECT name FROM sqlite_master
		WHERE type = 'table' AND name = 'reading_progress_resets'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(resetTable) != 1 {
		t.Fatal("reading_progress_resets table missing after compatible upgrade")
	}
	fieldClockTable, err := s.query(`
		SELECT name FROM sqlite_master
		WHERE type = 'table' AND name = 'user_mark_field_clocks'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(fieldClockTable) != 1 {
		t.Fatal("user_mark_field_clocks table missing after compatible upgrade")
	}
}

func TestUserMarkFieldClocksRejectStaleWorkFieldsIndependently(t *testing.T) {
	s := newUserMarkFieldClockTestServer(t)
	newerNotes := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock",
		"notes": "new notes", "client_updated_at": "2026-07-11T10:02:00Z",
	})
	if got := jsonStringList(t, newerNotes, "stored_fields"); !reflect.DeepEqual(got, []string{"notes"}) {
		t.Fatalf("stored_fields = %#v, want notes", got)
	}
	if got := jsonStringList(t, newerNotes, "rejected_fields"); len(got) != 0 {
		t.Fatalf("rejected_fields = %#v, want none", got)
	}
	equalNotes := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock",
		"notes": "equal replay", "client_updated_at": "2026-07-11T10:02:00Z",
	})
	if got := jsonStringList(t, equalNotes, "rejected_fields"); !reflect.DeepEqual(got, []string{"notes"}) || equalNotes["mark"].(map[string]any)["notes"] != "new notes" {
		t.Fatalf("equal notes replay was not rejected: %#v", equalNotes)
	}

	mixedOlder := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock",
		"notes": "old notes", "favorite": true, "personal_rating": 8,
		"client_updated_at": "2026-07-11T10:01:00Z",
	})
	if got := jsonStringList(t, mixedOlder, "stored_fields"); !reflect.DeepEqual(got, []string{"personal_rating", "favorite"}) {
		t.Fatalf("stored_fields = %#v, want independent rating/favorite", got)
	}
	if got := jsonStringList(t, mixedOlder, "rejected_fields"); !reflect.DeepEqual(got, []string{"notes"}) {
		t.Fatalf("rejected_fields = %#v, want only notes", got)
	}
	mark := mixedOlder["mark"].(map[string]any)
	if mark["notes"] != "new notes" || mark["favorite"] != true || intValue(mark["personal_rating"]) != 8 {
		t.Fatalf("mixed stale patch changed wrong fields: %#v", mark)
	}
	clockRows, err := s.query(`
		SELECT field_name, client_updated_at
		FROM user_mark_field_clocks
		WHERE target_type = 'work' AND identity_id = 'identity-clock'
		ORDER BY field_name
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(clockRows) != 3 {
		t.Fatalf("work field clocks = %#v, want 3", clockRows)
	}
}

func TestUserMarkFieldClocksCoverAllWorkFields(t *testing.T) {
	s := newUserMarkFieldClockTestServer(t)
	response := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock",
		"personal_rating": 9, "favorite": true, "reread_priority": 3,
		"translation_quality": 5, "image_quality": 4,
		"hidden": true, "hidden_reason": "private", "notes": "all fields",
		"client_updated_at": "2026-07-11T10:05:00Z",
	})
	wantFields := []string{
		"personal_rating", "favorite", "reread_priority", "translation_quality",
		"image_quality", "hidden", "hidden_reason", "notes",
	}
	if got := jsonStringList(t, response, "stored_fields"); !reflect.DeepEqual(got, wantFields) {
		t.Fatalf("stored_fields = %#v, want %#v", got, wantFields)
	}
	if got := jsonStringList(t, response, "rejected_fields"); len(got) != 0 {
		t.Fatalf("rejected_fields = %#v, want none", got)
	}
	clockRows, err := s.query(`
		SELECT field_name FROM user_mark_field_clocks
		WHERE target_type = 'work' AND identity_id = 'identity-clock'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(clockRows) != len(wantFields) {
		t.Fatalf("clock count = %d, want %d: %#v", len(clockRows), len(wantFields), clockRows)
	}
}

func TestUserMarkFieldClocksApplyToSeries(t *testing.T) {
	s := newUserMarkFieldClockTestServer(t)
	postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "series", "target_id": "series-clock",
		"notes": "new series notes", "client_updated_at": "2026-07-11T10:12:00Z",
	})
	mixedOlder := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "series", "target_id": "series-clock",
		"notes": "old series notes", "favorite": true,
		"client_updated_at": "2026-07-11T10:11:00Z",
	})
	if got := jsonStringList(t, mixedOlder, "stored_fields"); !reflect.DeepEqual(got, []string{"favorite"}) {
		t.Fatalf("series stored_fields = %#v, want favorite", got)
	}
	if got := jsonStringList(t, mixedOlder, "rejected_fields"); !reflect.DeepEqual(got, []string{"notes"}) {
		t.Fatalf("series rejected_fields = %#v, want notes", got)
	}
	mark := mixedOlder["mark"].(map[string]any)
	if mark["notes"] != "new series notes" || mark["favorite"] != true {
		t.Fatalf("series field clocks failed: %#v", mark)
	}
	clockRows, err := s.query(`
		SELECT field_name FROM user_mark_field_clocks
		WHERE target_type = 'series' AND identity_id = 'series-identity-clock'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(clockRows) != 2 {
		t.Fatalf("series field clocks = %#v, want notes and favorite", clockRows)
	}
	future := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "series", "target_id": "series-clock",
		"notes": "future series notes", "favorite": false,
		"client_updated_at": "2099-01-01T00:00:00Z",
	})
	if got := jsonStringList(t, future, "stored_fields"); len(got) != 0 {
		t.Fatalf("future series stored_fields = %#v, want none", got)
	}
	if got := jsonStringList(t, future, "rejected_fields"); !reflect.DeepEqual(got, []string{"favorite", "notes"}) {
		t.Fatalf("future series rejected_fields = %#v", got)
	}
	mark = future["mark"].(map[string]any)
	if mark["notes"] != "new series notes" || mark["favorite"] != true {
		t.Fatalf("future series patch changed mark: %#v", mark)
	}
}

func TestUserMarkFieldClocksRejectFutureAndProtectAgainstLegacyReplay(t *testing.T) {
	s := newUserMarkFieldClockTestServer(t)
	future := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock",
		"notes": "future notes", "favorite": true,
		"client_updated_at": "2099-01-01T00:00:00Z",
	})
	if got := jsonStringList(t, future, "stored_fields"); len(got) != 0 {
		t.Fatalf("future stored_fields = %#v, want none", got)
	}
	if got := jsonStringList(t, future, "rejected_fields"); !reflect.DeepEqual(got, []string{"favorite", "notes"}) {
		t.Fatalf("future rejected_fields = %#v", got)
	}
	mark := future["mark"].(map[string]any)
	if mark["notes"] != "" || mark["favorite"] != false {
		t.Fatalf("future patch changed mark: %#v", mark)
	}
	invalid := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock",
		"notes": "invalid timestamp notes", "client_updated_at": "not-a-time",
	})
	if got := jsonStringList(t, invalid, "rejected_fields"); !reflect.DeepEqual(got, []string{"notes"}) || invalid["mark"].(map[string]any)["notes"] != "" {
		t.Fatalf("invalid timestamp patch was not rejected: %#v", invalid)
	}

	postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock",
		"notes": "modern notes", "client_updated_at": "2026-07-11T10:22:00Z",
	})
	legacy := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock",
		"notes": "legacy replay", "favorite": true,
	})
	if got := jsonStringList(t, legacy, "stored_fields"); !reflect.DeepEqual(got, []string{"favorite"}) {
		t.Fatalf("legacy stored_fields = %#v, want unclocked favorite", got)
	}
	if got := jsonStringList(t, legacy, "rejected_fields"); !reflect.DeepEqual(got, []string{"notes"}) {
		t.Fatalf("legacy rejected_fields = %#v, want clocked notes", got)
	}
	mark = legacy["mark"].(map[string]any)
	if mark["notes"] != "modern notes" || mark["favorite"] != true {
		t.Fatalf("legacy replay changed clocked notes or lost favorite: %#v", mark)
	}
}

func TestUserMarkFieldClockUsesLegacyRowAsUpgradeLowerBound(t *testing.T) {
	s := newUserMarkFieldClockTestServer(t)
	if _, err := s.db.Exec(`
		INSERT INTO work_user_marks (
			reader_profile_key, work_identity_id, candidate_id, read_status,
			read_status_client_updated_at, favorite, reread_priority, hidden,
			hidden_reason, notes, marked_at, created_at, updated_at
		) VALUES (
			'default', 'identity-clock', 'work-clock', 'completed', '', 0, 0, 0,
			'', 'legacy newer notes', '2026-07-11T10:30:00Z',
			'2026-07-11T10:30:00Z', '2026-07-11T10:30:00Z'
		)
	`); err != nil {
		t.Fatal(err)
	}
	older := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock",
		"read_status": "unread", "notes": "offline old notes",
		"client_updated_at": "2026-07-11T10:29:00Z",
	})
	if got := jsonStringList(t, older, "rejected_fields"); !reflect.DeepEqual(got, []string{"notes"}) {
		t.Fatalf("upgrade rejected_fields = %#v, want notes", got)
	}
	if older["mark"].(map[string]any)["notes"] != "legacy newer notes" || older["mark"].(map[string]any)["read_status"] != "completed" {
		t.Fatalf("older timed note overwrote legacy row: %#v", older)
	}
	if older["read_status_stored"] != false {
		t.Fatalf("older timed read status crossed legacy row: %#v", older)
	}
	clockRows, err := s.query(`
		SELECT client_updated_at FROM user_mark_field_clocks
		WHERE target_type = 'work' AND identity_id = 'identity-clock' AND field_name = 'notes'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(clockRows) != 1 || clockRows[0]["client_updated_at"] != "2026-07-11T10:30:00.000000000Z" {
		t.Fatalf("legacy lower-bound clock = %#v", clockRows)
	}
	newer := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock",
		"notes": "modern newest notes", "client_updated_at": "2026-07-11T10:31:00Z",
	})
	if got := jsonStringList(t, newer, "stored_fields"); !reflect.DeepEqual(got, []string{"notes"}) {
		t.Fatalf("newer stored_fields = %#v, want notes", got)
	}
	if newer["mark"].(map[string]any)["notes"] != "modern newest notes" {
		t.Fatalf("newer note did not advance legacy row: %#v", newer)
	}
}

func TestUserMarkFieldClockLegacyLowerBoundSurvivesExistingProgress(t *testing.T) {
	s := newUserMarkFieldClockTestServer(t)
	if _, err := s.db.Exec(`
		INSERT INTO work_user_marks (
			reader_profile_key, work_identity_id, candidate_id, read_status,
			read_status_client_updated_at, favorite, reread_priority, hidden,
			hidden_reason, notes, marked_at, created_at, updated_at
		) VALUES (
			'default', 'identity-clock', 'work-clock', 'reading', '', 0, 0, 0,
			'', 'legacy notes at 10:30', '2026-07-11T10:30:00Z',
			'2026-07-11T10:30:00Z', '2026-07-11T10:30:00Z'
		)
	`); err != nil {
		t.Fatal(err)
	}
	progress := postJSON(t, s, "/api/progress", clockProgressPayload(4, "2026-07-11T10:50:00Z"))
	if progress["stored"] != true {
		t.Fatalf("progress setup failed: %#v", progress)
	}
	newerThanLegacy := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock",
		"notes": "offline notes at 10:40", "client_updated_at": "2026-07-11T10:40:00Z",
	})
	if got := jsonStringList(t, newerThanLegacy, "stored_fields"); !reflect.DeepEqual(got, []string{"notes"}) {
		t.Fatalf("notes newer than legacy value were blocked by progress: %#v", newerThanLegacy)
	}
	older := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock",
		"notes": "offline notes at 10:20", "client_updated_at": "2026-07-11T10:20:00Z",
	})
	if got := jsonStringList(t, older, "rejected_fields"); !reflect.DeepEqual(got, []string{"notes"}) {
		t.Fatalf("rejected_fields = %#v, want notes", got)
	}
	if older["mark"].(map[string]any)["notes"] != "offline notes at 10:40" {
		t.Fatalf("progress caused legacy lower bound to be skipped: %#v", older)
	}
	clockRows, err := s.query(`
		SELECT field_name FROM user_mark_field_clocks
		WHERE target_type = 'work' AND identity_id = 'identity-clock'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(clockRows) != 1 || clockRows[0]["field_name"] != "notes" {
		t.Fatalf("legacy work row protected fields = %#v, want notes only", clockRows)
	}
}

func TestUserMarkFieldClockTransactionRollback(t *testing.T) {
	s := newUserMarkFieldClockTestServer(t)
	postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock",
		"notes": "baseline", "client_updated_at": "2026-07-11T10:40:00Z",
	})
	if _, err := s.db.Exec(`
		CREATE TRIGGER reject_clocked_notes_update
		BEFORE UPDATE OF notes ON work_user_marks
		BEGIN
			SELECT RAISE(ABORT, 'blocked notes update');
		END
	`); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"target_type": "work", "target_id": "work-clock",
		"notes": "should rollback", "favorite": true,
		"client_updated_at": "2026-07-11T10:41:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/user-mark", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(writeIntentHeader, writeIntentValue)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("clock rollback status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	mark := getJSON(t, s, "/api/user-mark?target_type=work&target_id=work-clock")["mark"].(map[string]any)
	if mark["notes"] != "baseline" || mark["favorite"] != false {
		t.Fatalf("failed field patch partially changed mark: %#v", mark)
	}
	clockRows, err := s.query(`
		SELECT field_name, client_updated_at
		FROM user_mark_field_clocks
		WHERE target_type = 'work' AND identity_id = 'identity-clock'
		ORDER BY field_name
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(clockRows) != 1 || clockRows[0]["field_name"] != "notes" || clockRows[0]["client_updated_at"] != "2026-07-11T10:40:00.000000000Z" {
		t.Fatalf("failed field patch partially changed clocks: %#v", clockRows)
	}
}

func TestFavoriteOnlyMarkInheritsExistingProgressStatus(t *testing.T) {
	for name, completed := range map[string]int{"reading": 0, "completed": 1} {
		t.Run(name, func(t *testing.T) {
			s := newUserMarkFieldClockTestServer(t)
			if _, err := s.db.Exec(`
				INSERT INTO reading_progress (
					reader_profile_key, work_identity_id, candidate_id, page_manifest_id,
					manifest_hash_snapshot, progress_status, last_page_index, progress_percent,
					completed, page_count_snapshot, last_read_at, created_at, updated_at
				) VALUES (
					'default', 'identity-clock', 'work-clock', 'manifest-clock',
					'hash-clock', 'normal', 4, 41.67, ?, 12,
					'2026-07-11T10:50:00Z', '2026-07-11T10:50:00Z', '2026-07-11T10:50:00Z'
				)
			`, completed); err != nil {
				t.Fatal(err)
			}
			response := postJSON(t, s, "/api/user-mark", map[string]any{
				"target_type": "work", "target_id": "work-clock", "favorite": true,
				"client_updated_at": "2026-07-11T10:51:00Z",
			})
			mark := response["mark"].(map[string]any)
			if mark["read_status"] != name || mark["favorite"] != true || mark["read_status_client_updated_at"] != "2026-07-11T10:50:00.000000000Z" {
				t.Fatalf("favorite-only mark did not inherit %s progress: %#v", name, mark)
			}
		})
	}
	t.Run("progress API synthesized mark keeps fields independent", func(t *testing.T) {
		s := newUserMarkFieldClockTestServer(t)
		progress := postJSON(t, s, "/api/progress", clockProgressPayload(4, "2026-07-11T10:50:00Z"))
		if progress["stored"] != true {
			t.Fatalf("progress setup failed: %#v", progress)
		}
		favorite := postJSON(t, s, "/api/user-mark", map[string]any{
			"target_type": "work", "target_id": "work-clock", "favorite": true,
			"client_updated_at": "2026-07-11T10:40:00Z",
		})
		if got := jsonStringList(t, favorite, "stored_fields"); !reflect.DeepEqual(got, []string{"favorite"}) {
			t.Fatalf("progress status clock blocked independent favorite: %#v", favorite)
		}
		mark := favorite["mark"].(map[string]any)
		if mark["favorite"] != true || mark["read_status"] != "reading" || mark["read_status_client_updated_at"] != "2026-07-11T10:50:00.000000000Z" {
			t.Fatalf("favorite changed progress-derived status: %#v", mark)
		}
	})
}

func clockProgressPayload(index int, updatedAt string) map[string]any {
	payload := map[string]any{
		"candidate_id":     "work-clock",
		"page_manifest_id": "manifest-clock",
		"manifest_hash":    "hash-clock",
		"index":            index,
		"count":            12,
	}
	if updatedAt != "" {
		payload["updated_at"] = updatedAt
	}
	return payload
}

func TestProgressResetTombstoneOrdersOfflineWrites(t *testing.T) {
	s := newProgressClockTestServer(t)

	if saved := postJSON(t, s, "/api/progress", clockProgressPayload(2, "2026-07-11T10:00:00Z")); saved["stored"] != true {
		t.Fatalf("initial progress not stored: %#v", saved)
	}
	reset := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock", "read_status": "unread",
		"updated_at": "2026-07-11T10:01:00Z",
	})
	if reset["read_status_stored"] != true || reset["reset_stored"] != true {
		t.Fatalf("reset response = %#v, want accepted read status and tombstone", reset)
	}
	if current := getJSON(t, s, "/api/progress?id=work-clock"); current["progress"] != nil {
		t.Fatalf("reset preserved progress: %#v", current["progress"])
	}
	resetRows, err := s.query(`
		SELECT reset_at FROM reading_progress_resets
		WHERE reader_profile_key = 'default' AND work_identity_id = 'identity-clock'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(resetRows) != 1 || stringValue(resetRows[0]["reset_at"]) != "2026-07-11T10:01:00.000000000Z" {
		t.Fatalf("reset tombstone = %#v", resetRows)
	}

	for name, updatedAt := range map[string]string{
		"older replay": "2026-07-11T10:00:00Z",
		"equal replay": "2026-07-11T10:01:00Z",
	} {
		t.Run(name, func(t *testing.T) {
			replayed := postJSON(t, s, "/api/progress", clockProgressPayload(8, updatedAt))
			if replayed["stored"] != false || replayed["blocked_by_reset"] != true || replayed["discard_pending"] != true || replayed["progress"] != nil {
				t.Fatalf("replayed progress = %#v, want discardable reset rejection", replayed)
			}
		})
	}

	newer := postJSON(t, s, "/api/progress", clockProgressPayload(4, "2026-07-11T10:02:00Z"))
	if newer["stored"] != true || newer["blocked_by_reset"] != false {
		t.Fatalf("newer progress = %#v, want stored", newer)
	}
	resetRows, err = s.query(`SELECT reset_at FROM reading_progress_resets WHERE work_identity_id = 'identity-clock'`)
	if err != nil {
		t.Fatal(err)
	}
	if len(resetRows) != 0 {
		t.Fatalf("newer progress did not clear tombstone: %#v", resetRows)
	}

	reading := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock", "read_status": "reading",
		"updated_at": "2026-07-11T10:02:30Z",
	})
	if reading["read_status_stored"] != true {
		t.Fatalf("new reading status rejected: %#v", reading)
	}
	delayedUnread := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock", "read_status": "unread", "favorite": true,
		"updated_at": "2026-07-11T10:01:00Z",
	})
	if delayedUnread["read_status_stored"] != false || delayedUnread["reset_stored"] != false {
		t.Fatalf("delayed unread was accepted: %#v", delayedUnread)
	}
	delayedMark := delayedUnread["mark"].(map[string]any)
	if delayedMark["read_status"] != "reading" || delayedMark["favorite"] != true {
		t.Fatalf("delayed unread regressed status or lost independent fields: %#v", delayedMark)
	}
	current := getJSON(t, s, "/api/progress?id=work-clock")["progress"].(map[string]any)
	if intValue(current["index"]) != 4 {
		t.Fatalf("delayed unread changed newer progress: %#v", current)
	}
}

func TestLegacyUnreadAndProgressCannotDestroyModernResetState(t *testing.T) {
	s := newProgressClockTestServer(t)
	postJSON(t, s, "/api/progress", clockProgressPayload(3, "2026-07-11T11:00:00Z"))

	legacyUnread := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock", "read_status": "unread",
	})
	if legacyUnread["reset_stored"] != false {
		t.Fatalf("untimed unread created reset: %#v", legacyUnread)
	}
	if current := getJSON(t, s, "/api/progress?id=work-clock"); current["progress"] == nil {
		t.Fatal("untimed unread destructively deleted progress")
	}

	postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock", "read_status": "unread",
		"updated_at": "2026-07-11T11:01:00Z",
	})
	legacyProgress := postJSON(t, s, "/api/progress", clockProgressPayload(9, ""))
	if legacyProgress["stored"] != false || legacyProgress["blocked_by_reset"] != true || legacyProgress["progress"] != nil {
		t.Fatalf("untimed progress resurrected reset work: %#v", legacyProgress)
	}
}

func TestUntimedProgressOnlyAllowsFirstInsert(t *testing.T) {
	for name, firstTimestamp := range map[string]string{
		"missing timestamp": "",
		"invalid timestamp": "not-a-time",
	} {
		t.Run(name, func(t *testing.T) {
			s := newProgressClockTestServer(t)
			first := clockProgressPayload(1, "")
			if firstTimestamp != "" {
				first["updated_at"] = firstTimestamp
			}
			firstResponse := postJSON(t, s, "/api/progress", first)
			if firstResponse["stored"] != true {
				t.Fatalf("first untimed insert = %#v, want stored", firstResponse)
			}
			replay := clockProgressPayload(7, "")
			if firstTimestamp != "" {
				replay["updated_at"] = firstTimestamp
			}
			replayResponse := postJSON(t, s, "/api/progress", replay)
			if replayResponse["stored"] != false || replayResponse["rejected_reason"] != "read_status_newer" || replayResponse["blocked_by_read_status"] != true || replayResponse["discard_pending"] != true {
				t.Fatalf("untimed replay = %#v, want discardable rejection", replayResponse)
			}
			progress := getJSON(t, s, "/api/progress?id=work-clock")["progress"].(map[string]any)
			if intValue(progress["index"]) != 1 {
				t.Fatalf("untimed replay overwrote first insert: %#v", progress)
			}
		})
	}
}

func TestSQLiteWriterWaitsForConcurrentTransactionInsteadOfReturningLocked(t *testing.T) {
	s := newProgressClockTestServer(t)
	lockTx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockTx.Exec(`
		UPDATE reader_profiles SET updated_at = updated_at WHERE key = 'default'
	`); err != nil {
		_ = lockTx.Rollback()
		t.Fatal(err)
	}
	body, err := json.Marshal(clockProgressPayload(2, "2026-07-11T12:30:00Z"))
	if err != nil {
		_ = lockTx.Rollback()
		t.Fatal(err)
	}
	type responseResult struct {
		code int
		body string
	}
	done := make(chan responseResult, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/api/progress", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(writeIntentHeader, writeIntentValue)
		rec := httptest.NewRecorder()
		s.Routes().ServeHTTP(rec, req)
		done <- responseResult{code: rec.Code, body: rec.Body.String()}
	}()

	select {
	case response := <-done:
		_ = lockTx.Rollback()
		t.Fatalf("concurrent writer returned before lock release: status=%d body=%s", response.code, response.body)
	case <-time.After(75 * time.Millisecond):
	}
	if err := lockTx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-done:
		if response.code != http.StatusOK {
			t.Fatalf("concurrent writer status after lock release = %d; body=%s", response.code, response.body)
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(response.body), &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["stored"] != true {
			t.Fatalf("concurrent writer response = %#v, want stored", decoded)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent writer did not resume after lock release")
	}
}

func TestFutureClientTimesAreRejectedWithoutReplayDrift(t *testing.T) {
	s := newProgressClockTestServer(t)
	futureProgress := clockProgressPayload(8, "2099-01-01T00:00:00Z")
	for attempt := 0; attempt < 2; attempt++ {
		response := postJSON(t, s, "/api/progress", futureProgress)
		if response["stored"] != false || response["timestamp_rejected"] != true || response["rejected_reason"] != "future_timestamp" || response["progress"] != nil {
			t.Fatalf("future progress replay %d = %#v", attempt, response)
		}
	}

	postJSON(t, s, "/api/progress", clockProgressPayload(2, "2026-07-11T12:00:00Z"))
	futureUnreadPayload := map[string]any{
		"target_type": "work", "target_id": "work-clock", "read_status": "unread",
		"client_updated_at": "2099-01-01T00:00:00Z",
	}
	for attempt := 0; attempt < 2; attempt++ {
		response := postJSON(t, s, "/api/user-mark", futureUnreadPayload)
		if response["read_status_stored"] != false || response["reset_stored"] != false {
			t.Fatalf("future unread replay %d = %#v", attempt, response)
		}
	}
	current := getJSON(t, s, "/api/progress?id=work-clock")["progress"].(map[string]any)
	if intValue(current["index"]) != 2 {
		t.Fatalf("future unread replay changed progress: %#v", current)
	}
	resetRows, err := s.query(`SELECT reset_at FROM reading_progress_resets WHERE work_identity_id = 'identity-clock'`)
	if err != nil {
		t.Fatal(err)
	}
	if len(resetRows) != 0 {
		t.Fatalf("future unread created drifting tombstone: %#v", resetRows)
	}
}

func TestCompletedProgressAndTombstoneRollbackTogether(t *testing.T) {
	s := newProgressClockTestServer(t)
	postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock", "read_status": "unread",
		"updated_at": "2026-07-11T13:00:00Z",
	})
	if _, err := s.db.Exec(`
		CREATE TRIGGER reject_completed_status_update
		BEFORE UPDATE OF read_status ON work_user_marks
		WHEN NEW.read_status = 'completed'
		BEGIN
			SELECT RAISE(ABORT, 'blocked completed status');
		END
	`); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(clockProgressPayload(11, "2026-07-11T13:01:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/progress", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(writeIntentHeader, writeIntentValue)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("completed trigger status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	if current := getJSON(t, s, "/api/progress?id=work-clock"); current["progress"] != nil {
		t.Fatalf("failed completed write partially committed progress: %#v", current["progress"])
	}
	resetRows, err := s.query(`SELECT reset_at FROM reading_progress_resets WHERE work_identity_id = 'identity-clock'`)
	if err != nil {
		t.Fatal(err)
	}
	if len(resetRows) != 1 {
		t.Fatalf("failed completed write partially cleared tombstone: %#v", resetRows)
	}
	mark := getJSON(t, s, "/api/user-mark?target_type=work&target_id=work-clock")["mark"].(map[string]any)
	if mark["read_status"] != "unread" {
		t.Fatalf("failed completed write partially changed mark: %#v", mark)
	}
	if _, err := s.db.Exec(`DROP TRIGGER reject_completed_status_update`); err != nil {
		t.Fatal(err)
	}
	retried := postJSON(t, s, "/api/progress", clockProgressPayload(11, "2026-07-11T13:01:00Z"))
	if retried["stored"] != true {
		t.Fatalf("same timestamp retry after rollback was not stored: %#v", retried)
	}
	mark = getJSON(t, s, "/api/user-mark?target_type=work&target_id=work-clock")["mark"].(map[string]any)
	if mark["read_status"] != "completed" {
		t.Fatalf("successful retry did not complete mark: %#v", mark)
	}
}

func TestDelayedUnreadCannotRegressCompletedProgress(t *testing.T) {
	s := newProgressClockTestServer(t)
	completed := postJSON(t, s, "/api/progress", clockProgressPayload(11, "2026-07-11T14:02:00Z"))
	if completed["stored"] != true {
		t.Fatalf("completed progress not stored: %#v", completed)
	}
	delayed := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock", "read_status": "unread",
		"updated_at": "2026-07-11T14:01:00Z",
	})
	if delayed["read_status_stored"] != false || delayed["reset_stored"] != false {
		t.Fatalf("delayed unread accepted after completed progress: %#v", delayed)
	}
	mark := delayed["mark"].(map[string]any)
	if mark["read_status"] != "completed" {
		t.Fatalf("delayed unread regressed completed mark: %#v", mark)
	}
	progress := getJSON(t, s, "/api/progress?id=work-clock")["progress"].(map[string]any)
	if progress["completed"] != true {
		t.Fatalf("delayed unread deleted completed progress: %#v", progress)
	}
}

func TestWorkReadStatusClientClockRejectsOlderEvents(t *testing.T) {
	s := newProgressClockTestServer(t)
	newer := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock", "read_status": "completed",
		"client_updated_at": "2026-07-11T14:10:00Z",
	})
	if newer["read_status_stored"] != true {
		t.Fatalf("initial timed status rejected: %#v", newer)
	}
	for name, testCase := range map[string][2]string{
		"older reading":   {"reading", "2026-07-11T14:09:00Z"},
		"equal abandoned": {"abandoned", "2026-07-11T14:10:00Z"},
	} {
		t.Run(name, func(t *testing.T) {
			response := postJSON(t, s, "/api/user-mark", map[string]any{
				"target_type": "work", "target_id": "work-clock", "read_status": testCase[0],
				"client_updated_at": testCase[1],
			})
			if response["read_status_stored"] != false || response["mark"].(map[string]any)["read_status"] != "completed" {
				t.Fatalf("stale status response = %#v", response)
			}
		})
	}
	latest := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock", "read_status": "reading",
		"client_updated_at": "2026-07-11T14:11:00Z",
	})
	latestMark := latest["mark"].(map[string]any)
	if latest["read_status_stored"] != true || latestMark["read_status"] != "reading" || latestMark["read_status_client_updated_at"] != "2026-07-11T14:11:00.000000000Z" {
		t.Fatalf("newer status did not advance clock: %#v", latest)
	}
	legacy := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock", "read_status": "completed",
	})
	if legacy["read_status_stored"] != false || legacy["mark"].(map[string]any)["read_status"] != "reading" {
		t.Fatalf("untimed legacy status overwrote modern clock: %#v", legacy)
	}
}

func TestSeriesReadStatusClientClockRejectsOlderOfflineEvents(t *testing.T) {
	s := newUserMarkFieldClockTestServer(t)
	newer := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "series", "target_id": "series-clock", "read_status": "completed",
		"client_updated_at": "2026-07-11T14:10:00Z",
	})
	newerMark := newer["mark"].(map[string]any)
	if newer["read_status_stored"] != true || newerMark["read_status"] != "completed" || newerMark["read_status_client_updated_at"] != "2026-07-11T14:10:00.000000000Z" {
		t.Fatalf("initial timed series status rejected: %#v", newer)
	}

	for name, testCase := range map[string][2]string{
		"older offline reading": {"reading", "2026-07-11T14:09:00Z"},
		"equal replay":          {"abandoned", "2026-07-11T14:10:00Z"},
	} {
		t.Run(name, func(t *testing.T) {
			response := postJSON(t, s, "/api/user-mark", map[string]any{
				"target_type": "series", "target_id": "series-clock", "read_status": testCase[0],
				"client_updated_at": testCase[1],
			})
			if response["read_status_stored"] != false || response["mark"].(map[string]any)["read_status"] != "completed" {
				t.Fatalf("stale series status response = %#v", response)
			}
		})
	}
	mixedOlder := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "series", "target_id": "series-clock", "read_status": "reading",
		"personal_rating": 0, "client_updated_at": "2026-07-11T14:09:30Z",
	})
	mixedMark := mixedOlder["mark"].(map[string]any)
	if mixedOlder["read_status_stored"] != false || mixedMark["read_status"] != "completed" || mixedMark["personal_rating"] == nil || intValue(mixedMark["personal_rating"]) != 0 {
		t.Fatalf("stale series status was not isolated from independent 0 rating: %#v", mixedOlder)
	}
	if got := jsonStringList(t, mixedOlder, "stored_fields"); !reflect.DeepEqual(got, []string{"personal_rating"}) {
		t.Fatalf("mixed series stored_fields = %#v, want only personal_rating", got)
	}

	latest := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "series", "target_id": "series-clock", "read_status": "reading",
		"client_updated_at": "2026-07-11T14:11:00Z",
	})
	latestMark := latest["mark"].(map[string]any)
	if latest["read_status_stored"] != true || latestMark["read_status"] != "reading" || latestMark["read_status_client_updated_at"] != "2026-07-11T14:11:00.000000000Z" {
		t.Fatalf("newer series status did not advance clock: %#v", latest)
	}
	legacy := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "series", "target_id": "series-clock", "read_status": "completed",
	})
	if legacy["read_status_stored"] != false || legacy["mark"].(map[string]any)["read_status"] != "reading" {
		t.Fatalf("untimed legacy series status overwrote modern clock: %#v", legacy)
	}
}

func TestSeriesReadStatusClockUsesLegacyRowAsUpgradeLowerBound(t *testing.T) {
	s := newUserMarkFieldClockTestServer(t)
	if _, err := s.db.Exec(`
		INSERT INTO series_user_marks (
			reader_profile_key, series_identity_id, group_id, read_status,
			favorite, reread_priority, hidden, hidden_reason, notes,
			marked_at, created_at, updated_at
		) VALUES (
			'default', 'series-identity-clock', 'series-clock', 'completed',
			0, 0, 0, '', '',
			'2026-07-11T14:20:00Z', '2026-07-11T14:20:00Z', '2026-07-11T14:20:00Z'
		)
	`); err != nil {
		t.Fatal(err)
	}

	older := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "series", "target_id": "series-clock", "read_status": "reading",
		"client_updated_at": "2026-07-11T14:19:00Z",
	})
	olderMark := older["mark"].(map[string]any)
	if older["read_status_stored"] != false || olderMark["read_status"] != "completed" || olderMark["read_status_client_updated_at"] != "2026-07-11T14:20:00.000000000Z" {
		t.Fatalf("older event replaced legacy series status: %#v", older)
	}

	newer := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "series", "target_id": "series-clock", "read_status": "reading",
		"client_updated_at": "2026-07-11T14:21:00Z",
	})
	newerMark := newer["mark"].(map[string]any)
	if newer["read_status_stored"] != true || newerMark["read_status"] != "reading" || newerMark["read_status_client_updated_at"] != "2026-07-11T14:21:00.000000000Z" {
		t.Fatalf("newer event could not replace legacy series status: %#v", newer)
	}
}

func TestMalformedOrFutureReadStatusTimestampsAreRejected(t *testing.T) {
	for name, testCase := range map[string][3]string{
		"work invalid":  {"work", "work-clock", "not-a-time"},
		"series future": {"series", "series-clock", "2099-01-01T00:00:00Z"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newUserMarkFieldClockTestServer(t)
			response := postJSON(t, s, "/api/user-mark", map[string]any{
				"target_type": testCase[0], "target_id": testCase[1], "read_status": "completed",
				"client_updated_at": testCase[2],
			})
			if response["read_status_stored"] != false || response["mark"].(map[string]any)["read_status"] != "unread" {
				t.Fatalf("untrusted read status timestamp was accepted: %#v", response)
			}
		})
	}
}

func TestProgressMustAdvanceTimedReadStatusClock(t *testing.T) {
	for name, status := range map[string]string{
		"abandoned": "abandoned",
		"completed": "completed",
	} {
		t.Run(name, func(t *testing.T) {
			s := newProgressClockTestServer(t)
			markResponse := postJSON(t, s, "/api/user-mark", map[string]any{
				"target_type": "work", "target_id": "work-clock", "read_status": status,
				"client_updated_at": "2026-07-11T14:20:00Z",
			})
			if markResponse["read_status_stored"] != true {
				t.Fatalf("timed %s mark not stored: %#v", status, markResponse)
			}
			oldProgress := postJSON(t, s, "/api/progress", clockProgressPayload(7, "2026-07-11T14:19:00Z"))
			if oldProgress["stored"] != false || oldProgress["blocked_by_read_status"] != true || oldProgress["rejected_reason"] != "read_status_newer" || oldProgress["progress"] != nil {
				t.Fatalf("old progress crossed %s clock: %#v", status, oldProgress)
			}
			newProgress := postJSON(t, s, "/api/progress", clockProgressPayload(4, "2026-07-11T14:21:00Z"))
			if newProgress["stored"] != true {
				t.Fatalf("new progress did not cross %s clock: %#v", status, newProgress)
			}
			mark := getJSON(t, s, "/api/user-mark?target_type=work&target_id=work-clock")["mark"].(map[string]any)
			if mark["read_status"] != "reading" || mark["read_status_client_updated_at"] != "2026-07-11T14:21:00.000000000Z" {
				t.Fatalf("new progress did not advance mark to reading: %#v", mark)
			}
		})
	}
}

func TestCompletedMarkThenEqualCompletedProgressIsIdempotent(t *testing.T) {
	s := newProgressClockTestServer(t)
	postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock", "read_status": "completed",
		"client_updated_at": "2026-07-11T14:25:00Z",
	})
	equalCompleted := postJSON(t, s, "/api/progress", clockProgressPayload(11, "2026-07-11T14:25:00Z"))
	if equalCompleted["stored"] != true || equalCompleted["blocked_by_read_status"] != false {
		t.Fatalf("equal completed event did not store missing progress: %#v", equalCompleted)
	}
	progress := equalCompleted["progress"].(map[string]any)
	if progress["completed"] != true {
		t.Fatalf("equal completed event stored wrong progress: %#v", progress)
	}
	equalIncomplete := postJSON(t, s, "/api/progress", clockProgressPayload(4, "2026-07-11T14:25:00Z"))
	if equalIncomplete["stored"] != false || equalIncomplete["blocked_by_read_status"] != true {
		t.Fatalf("equal incomplete event crossed completed clock: %#v", equalIncomplete)
	}
}

func TestPersistedFutureClocksCanBeRecoveredByCurrentEvents(t *testing.T) {
	t.Run("progress save and unread", func(t *testing.T) {
		s := newProgressClockTestServer(t)
		if _, err := s.db.Exec(`
			INSERT INTO reading_progress (
				reader_profile_key, work_identity_id, candidate_id, page_manifest_id,
				manifest_hash_snapshot, progress_status, last_page_index, progress_percent,
				completed, page_count_snapshot, last_read_at, created_at, updated_at
			) VALUES (
				'default', 'identity-clock', 'work-clock', 'manifest-clock',
				'hash-clock', 'normal', 9, 83.33, 0, 12,
				'2099-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2099-01-01T00:00:00Z'
			)
		`); err != nil {
			t.Fatal(err)
		}
		recovered := postJSON(t, s, "/api/progress", clockProgressPayload(2, "2026-07-11T14:30:00Z"))
		if recovered["stored"] != true || intValue(recovered["progress"].(map[string]any)["index"]) != 2 {
			t.Fatalf("current progress did not replace persisted future clock: %#v", recovered)
		}
		if _, err := s.db.Exec(`
			UPDATE reading_progress
			SET last_read_at = '2099-01-01T00:00:00Z', updated_at = '2099-01-01T00:00:00Z'
			WHERE work_identity_id = 'identity-clock'
		`); err != nil {
			t.Fatal(err)
		}
		unread := postJSON(t, s, "/api/user-mark", map[string]any{
			"target_type": "work", "target_id": "work-clock", "read_status": "unread",
			"client_updated_at": "2026-07-11T14:31:00Z",
		})
		if unread["read_status_stored"] != true || unread["reset_stored"] != true {
			t.Fatalf("current unread could not recover future progress clock: %#v", unread)
		}
		if current := getJSON(t, s, "/api/progress?id=work-clock"); current["progress"] != nil {
			t.Fatalf("recovered unread did not delete poisoned progress: %#v", current["progress"])
		}
	})

	t.Run("reset and read status", func(t *testing.T) {
		s := newProgressClockTestServer(t)
		if _, err := s.db.Exec(`
			INSERT INTO reading_progress_resets (
				reader_profile_key, work_identity_id, reset_at, created_at, updated_at
			) VALUES (
				'default', 'identity-clock', '2099-01-01T00:00:00Z',
				'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'
			);
			INSERT INTO work_user_marks (
				reader_profile_key, work_identity_id, candidate_id, read_status,
				read_status_client_updated_at, favorite, reread_priority, hidden,
				hidden_reason, notes, marked_at, created_at, updated_at
			) VALUES (
				'default', 'identity-clock', 'work-clock', 'abandoned',
				'2099-01-01T00:00:00Z', 0, 0, 0, '', '',
				'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'
			);
		`); err != nil {
			t.Fatal(err)
		}
		recovered := postJSON(t, s, "/api/progress", clockProgressPayload(3, "2026-07-11T14:33:00Z"))
		if recovered["stored"] != true {
			t.Fatalf("current progress could not recover future reset/status clocks: %#v", recovered)
		}
		resetRows, err := s.query(`SELECT reset_at FROM reading_progress_resets WHERE work_identity_id = 'identity-clock'`)
		if err != nil {
			t.Fatal(err)
		}
		if len(resetRows) != 0 {
			t.Fatalf("recovered progress did not clear future reset: %#v", resetRows)
		}
		mark := getJSON(t, s, "/api/user-mark?target_type=work&target_id=work-clock")["mark"].(map[string]any)
		if mark["read_status"] != "reading" || mark["read_status_client_updated_at"] != "2026-07-11T14:33:00.000000000Z" {
			t.Fatalf("recovered progress did not replace future status clock: %#v", mark)
		}
	})

	t.Run("migration", func(t *testing.T) {
		s := newProgressClockTestServer(t)
		if _, err := s.db.Exec(`
			INSERT INTO reading_progress (
				reader_profile_key, work_identity_id, candidate_id, page_manifest_id,
				manifest_hash_snapshot, progress_status, last_page_index, progress_percent,
				completed, page_count_snapshot, last_read_at, created_at, updated_at
			) VALUES (
				'default', 'identity-clock', 'work-clock', 'manifest-clock',
				'hash-clock', 'normal', 9, 83.33, 0, 12,
				'2099-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2099-01-01T00:00:00Z'
			)
		`); err != nil {
			t.Fatal(err)
		}
		migration := postJSON(t, s, "/api/progress-migration", map[string]any{
			"items": []any{map[string]any{
				"candidate_id": "work-clock", "index": 3, "count": 12,
				"updated_at": "2026-07-11T14:32:00Z",
			}},
		})
		if intValue(migration["imported"]) != 1 {
			t.Fatalf("migration could not replace persisted future clock: %#v", migration)
		}
		progress := getJSON(t, s, "/api/progress?id=work-clock")["progress"].(map[string]any)
		if intValue(progress["index"]) != 3 {
			t.Fatalf("migration future-clock recovery stored wrong progress: %#v", progress)
		}
	})
}

func TestLegacyMarkTimestampProtectsUpgradeOrderingWithoutModernClocks(t *testing.T) {
	s := newProgressClockTestServer(t)
	if _, err := s.db.Exec(`
		INSERT INTO work_user_marks (
			reader_profile_key, work_identity_id, candidate_id, read_status,
			read_status_client_updated_at, favorite, reread_priority, hidden,
			hidden_reason, notes, marked_at, created_at, updated_at
		) VALUES (
			'default', 'identity-clock', 'work-clock', 'completed', '', 0, 0, 0,
			'', '', '2026-07-11T14:35:00Z', '2026-07-11T14:35:00Z', '2026-07-11T14:35:00Z'
		)
	`); err != nil {
		t.Fatal(err)
	}
	older := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock", "read_status": "unread",
		"client_updated_at": "2026-07-11T14:34:00Z",
	})
	if older["read_status_stored"] != false || older["reset_stored"] != false || older["mark"].(map[string]any)["read_status"] != "completed" {
		t.Fatalf("older timed unread crossed legacy mark timestamp: %#v", older)
	}
	newer := postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock", "read_status": "unread",
		"client_updated_at": "2026-07-11T14:36:00Z",
	})
	if newer["read_status_stored"] != true || newer["reset_stored"] != true {
		t.Fatalf("newer timed unread did not advance legacy mark: %#v", newer)
	}
}

func TestProgressMigrationHonorsResetAndProgressClocks(t *testing.T) {
	s := newProgressClockTestServer(t)
	postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock", "read_status": "unread",
		"updated_at": "2026-07-11T09:01:00Z",
	})
	response := postJSON(t, s, "/api/progress-migration", map[string]any{
		"items": []any{
			map[string]any{"candidate_id": "work-clock", "index": 8, "count": 12, "updated_at": "2026-07-11T09:00:00Z"},
			map[string]any{"candidate_id": "work-clock", "index": 9, "count": 12},
			map[string]any{"candidate_id": "work-clock", "index": 4, "count": 12, "updated_at": "2026-07-11T09:02:00Z"},
		},
	})
	if intValue(response["imported"]) != 1 || intValue(response["skipped"]) != 2 {
		t.Fatalf("migration reset ordering response = %#v", response)
	}
	progress := getJSON(t, s, "/api/progress?id=work-clock")["progress"].(map[string]any)
	if intValue(progress["index"]) != 4 || progress["updated_at"] != "2026-07-11T09:02:00Z" {
		t.Fatalf("newer migrated progress = %#v", progress)
	}
	older := postJSON(t, s, "/api/progress-migration", map[string]any{
		"items": []any{
			map[string]any{"candidate_id": "work-clock", "index": 10, "count": 12, "updated_at": "2026-07-11T09:01:30Z"},
		},
	})
	if intValue(older["imported"]) != 0 || intValue(older["skipped"]) != 1 {
		t.Fatalf("older migration replaced sqlite progress: %#v", older)
	}
	progress = getJSON(t, s, "/api/progress?id=work-clock")["progress"].(map[string]any)
	if intValue(progress["index"]) != 4 {
		t.Fatalf("older migration regressed progress: %#v", progress)
	}
}

func TestProgressMigrationMustAdvanceTimedReadStatusClock(t *testing.T) {
	s := newProgressClockTestServer(t)
	postJSON(t, s, "/api/user-mark", map[string]any{
		"target_type": "work", "target_id": "work-clock", "read_status": "abandoned",
		"client_updated_at": "2026-07-11T14:40:00Z",
	})
	oldMigration := postJSON(t, s, "/api/progress-migration", map[string]any{
		"items": []any{map[string]any{
			"candidate_id": "work-clock", "index": 8, "count": 12,
			"updated_at": "2026-07-11T14:39:00Z",
		}},
	})
	if intValue(oldMigration["imported"]) != 0 || intValue(oldMigration["skipped"]) != 1 || getJSON(t, s, "/api/progress?id=work-clock")["progress"] != nil {
		t.Fatalf("old migration crossed timed read status: %#v", oldMigration)
	}
	newMigration := postJSON(t, s, "/api/progress-migration", map[string]any{
		"items": []any{map[string]any{
			"candidate_id": "work-clock", "index": 3, "count": 12,
			"updated_at": "2026-07-11T14:41:00Z",
		}},
	})
	if intValue(newMigration["imported"]) != 1 {
		t.Fatalf("new migration did not advance timed read status: %#v", newMigration)
	}
	mark := getJSON(t, s, "/api/user-mark?target_type=work&target_id=work-clock")["mark"].(map[string]any)
	if mark["read_status"] != "reading" || mark["read_status_client_updated_at"] != "2026-07-11T14:41:00.000000000Z" {
		t.Fatalf("new migration did not advance mark to reading: %#v", mark)
	}
}

func TestUserMarkFusionFilters(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE marks (
			id INTEGER PRIMARY KEY,
			label TEXT NOT NULL,
			favorite INTEGER,
			personal_rating INTEGER,
			read_status TEXT,
			reread_priority INTEGER,
			notes TEXT
		)
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO marks (id, label, favorite, personal_rating) VALUES
			(1, 'fav-unrated', 1, NULL),
			(2, 'fav-low', 1, 5),
			(3, 'rating-6', 0, 6),
			(4, 'rating-7', 0, 7),
			(5, 'rating-8', 0, 8),
			(6, 'fav-rating-8', 1, 8),
			(7, 'none', 0, NULL)
	`); err != nil {
		t.Fatal(err)
	}

	matches := func(mark string) string {
		t.Helper()
		filters := []string{}
		addUserMarkFilter(&filters, mark, "wum")
		if len(filters) != 1 {
			t.Fatalf("addUserMarkFilter(%q) produced %d filters, want 1", mark, len(filters))
		}
		rows, err := db.Query("SELECT label FROM marks wum WHERE " + filters[0] + " ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		labels := []string{}
		for rows.Next() {
			var label string
			if err := rows.Scan(&label); err != nil {
				t.Fatal(err)
			}
			labels = append(labels, label)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return strings.Join(labels, ",")
	}

	cases := map[string]string{
		"favorite":         "fav-unrated,fav-low,fav-rating-8",
		"liked":            "fav-unrated,fav-low,rating-7,rating-8,fav-rating-8",
		"strong-liked":     "rating-8,fav-rating-8",
		"favorite-unrated": "fav-unrated",
		"rating-liked":     "rating-7,rating-8",
		"favorite-low":     "fav-low",
		"rated":            "fav-low,rating-6,rating-7,rating-8,fav-rating-8",
	}
	for mark, want := range cases {
		if got := matches(mark); got != want {
			t.Fatalf("matches(%q) = %q, want %q", mark, got, want)
		}
	}
}

func TestBrowseStateSyncKeepsNewestState(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	initial := getJSON(t, s, "/api/browse-state")
	if initial["state"] != nil {
		t.Fatalf("initial state = %#v, want nil", initial["state"])
	}

	newer := postJSON(t, s, "/api/browse-state", map[string]any{
		"state": map[string]any{
			"bmangaView":             "shelf",
			"bmangaOffset":           48,
			"bmangaLimit":            24,
			"bmangaPage":             3,
			"bmangaBrowseScrollY":    1280,
			"bmangaBrowseAnchorType": "series",
			"bmangaBrowseAnchorId":   "series-1",
			"bmangaBrowseAnchorTop":  96,
			"bmangaType":             "ignored-outside-works",
			"updated_at":             "2026-06-15T20:37:00+08:00",
		},
	})
	if newer["stored"] != true {
		t.Fatalf("newer stored = %v, want true", newer["stored"])
	}

	older := postJSON(t, s, "/api/browse-state", map[string]any{
		"state": map[string]any{
			"bmangaView":   "shelf",
			"bmangaOffset": 0,
			"bmangaLimit":  24,
			"bmangaPage":   1,
			"updated_at":   "2026-06-15T12:36:00.000Z",
		},
	})
	if older["stored"] != false {
		t.Fatalf("older stored = %v, want false", older["stored"])
	}

	payload := getJSON(t, s, "/api/browse-state")
	state := payload["state"].(map[string]any)
	if int(state["bmangaPage"].(float64)) != 3 {
		t.Fatalf("bmangaPage = %v, want 3", state["bmangaPage"])
	}
	if int(state["bmangaOffset"].(float64)) != 48 {
		t.Fatalf("bmangaOffset = %v, want 48", state["bmangaOffset"])
	}
	if int(state["bmangaBrowseScrollY"].(float64)) != 1280 {
		t.Fatalf("bmangaBrowseScrollY = %v, want 1280", state["bmangaBrowseScrollY"])
	}
	if state["bmangaBrowseAnchorType"] != "series" || state["bmangaBrowseAnchorId"] != "series-1" {
		t.Fatalf("browse anchor = %v/%v, want series/series-1", state["bmangaBrowseAnchorType"], state["bmangaBrowseAnchorId"])
	}
	if state["bmangaType"] != "" {
		t.Fatalf("bmangaType = %v, want empty outside works view", state["bmangaType"])
	}
}

func TestLibraryPageStateSyncMergesModesAndResetsOnNewSort(t *testing.T) {
	s, err := newServerWithoutCatalogForTest(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	legacyTime := formatOrderedClientTime(time.Now().UTC().Add(-2 * time.Hour))
	postJSON(t, s, "/api/browse-state", map[string]any{
		"state": map[string]any{
			"bmangaView": "shelf", "bmangaOffset": 60, "bmangaLimit": 60,
			"bmangaPage": 2, "updated_at": legacyTime,
		},
	})
	var legacyStateBefore string
	if err := s.db.QueryRow(`
		SELECT state_json FROM browse_states
		WHERE reader_profile_key = 'default' AND state_key = 'browse'
	`).Scan(&legacyStateBefore); err != nil {
		t.Fatal(err)
	}

	initial := getJSON(t, s, "/api/library-page-state")
	if initial["state"] != nil || initial["updated_at"] != "" {
		t.Fatalf("initial library page state = %#v, want null/empty", initial)
	}

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	firstTime := formatOrderedClientTime(base)
	firstInputTime := base.In(time.FixedZone("test-utc-plus-8", 8*60*60)).Format(time.RFC3339Nano)
	first := postJSON(t, s, "/api/library-page-state", map[string]any{
		"state": map[string]any{
			"sort":       "added_desc",
			"mode":       "doujin",
			"offset":     37,
			"updated_at": firstInputTime,
			"event_id":   "origin-b",
			"initial_offsets": map[string]any{
				"all": 19, "doujin": 90, "series": 1_000_001,
			},
		},
	})
	if first["stored"] != true || first["updated_at"] != firstTime {
		t.Fatalf("initial save = %#v, want stored canonical clock %s", first, firstTime)
	}
	firstState := libraryPageStateResponseForTest(t, first)
	if firstState["version"] != float64(1) || firstState["sort"] != "added_desc" || firstState["sort_updated_at"] != firstTime || firstState["sort_event_id"] != "origin-b" {
		t.Fatalf("initial canonical state = %#v", firstState)
	}
	assertLibraryPagePositionForTest(t, firstState, "all", 18, firstTime, "origin-b")
	assertLibraryPagePositionForTest(t, firstState, "doujin", 36, firstTime, "origin-b")
	assertLibraryPagePositionForTest(t, firstState, "series", 999990, firstTime, "origin-b")

	smallerTie := postJSON(t, s, "/api/library-page-state", map[string]any{
		"state": map[string]any{
			"sort": "added_desc", "mode": "doujin", "offset": 72,
			"updated_at": firstTime, "event_id": "origin-a",
		},
	})
	if smallerTie["stored"] != false {
		t.Fatalf("smaller same-time event stored = %#v, want false", smallerTie)
	}
	assertLibraryPagePositionForTest(t, libraryPageStateResponseForTest(t, smallerTie), "doujin", 36, firstTime, "origin-b")

	largerTie := postJSON(t, s, "/api/library-page-state", map[string]any{
		"state": map[string]any{
			"sort": "added_desc", "mode": "doujin", "offset": 77,
			"updated_at": firstTime, "event_id": "origin-c",
		},
	})
	if largerTie["stored"] != true {
		t.Fatalf("larger same-time event stored = %#v, want true", largerTie)
	}
	assertLibraryPagePositionForTest(t, libraryPageStateResponseForTest(t, largerTie), "doujin", 72, firstTime, "origin-c")

	exactReplay := postJSON(t, s, "/api/library-page-state", map[string]any{
		"state": map[string]any{
			"sort": "added_desc", "mode": "doujin", "offset": 180,
			"updated_at": firstTime, "event_id": "origin-c",
		},
	})
	if exactReplay["stored"] != false {
		t.Fatalf("exact replay stored = %#v, want false", exactReplay)
	}
	assertLibraryPagePositionForTest(t, libraryPageStateResponseForTest(t, exactReplay), "doujin", 72, firstTime, "origin-c")

	seriesTime := formatOrderedClientTime(base.Add(time.Minute))
	seriesUpdate := postJSON(t, s, "/api/library-page-state", map[string]any{
		"state": map[string]any{
			"sort": "added_desc", "mode": "series", "offset": 55,
			"updated_at": seriesTime, "event_id": "series-1",
			"initial_offsets": map[string]any{"all": 900, "doujin": 900},
		},
	})
	seriesState := libraryPageStateResponseForTest(t, seriesUpdate)
	assertLibraryPagePositionForTest(t, seriesState, "all", 18, firstTime, "origin-b")
	assertLibraryPagePositionForTest(t, seriesState, "doujin", 72, firstTime, "origin-c")
	assertLibraryPagePositionForTest(t, seriesState, "series", 54, seriesTime, "series-1")
	if seriesState["updated_at"] != seriesTime || seriesUpdate["updated_at"] != seriesTime {
		t.Fatalf("merged latest clock = state:%v response:%v, want %s", seriesState["updated_at"], seriesUpdate["updated_at"], seriesTime)
	}

	staleSortTime := formatOrderedClientTime(base.Add(30 * time.Second))
	staleSort := postJSON(t, s, "/api/library-page-state", map[string]any{
		"state": map[string]any{
			"sort": "title_asc", "mode": "all", "offset": 180,
			"updated_at": staleSortTime, "event_id": "stale-sort",
		},
	})
	if staleSort["stored"] != false || libraryPageStateResponseForTest(t, staleSort)["sort"] != "added_desc" {
		t.Fatalf("sort event older than a current-sort position = %#v, want rejected canonical added_desc", staleSort)
	}

	sortTime := formatOrderedClientTime(base.Add(2 * time.Minute))
	sortChange := postJSON(t, s, "/api/library-page-state", map[string]any{
		"state": map[string]any{
			"sort": "title_asc", "mode": "doujin", "offset": 91,
			"updated_at": sortTime, "event_id": "sort-b",
		},
	})
	if sortChange["stored"] != true {
		t.Fatalf("sort change = %#v, want stored", sortChange)
	}
	sortState := libraryPageStateResponseForTest(t, sortChange)
	if sortState["sort"] != "title_asc" || sortState["sort_updated_at"] != sortTime || sortState["sort_event_id"] != "sort-b" {
		t.Fatalf("sort canonical state = %#v", sortState)
	}
	assertLibraryPagePositionForTest(t, sortState, "all", 0, sortTime, "sort-b")
	assertLibraryPagePositionForTest(t, sortState, "doujin", 90, sortTime, "sort-b")
	assertLibraryPagePositionForTest(t, sortState, "series", 0, sortTime, "sort-b")

	olderSortTie := postJSON(t, s, "/api/library-page-state", map[string]any{
		"state": map[string]any{
			"sort": "pages_desc", "mode": "all", "offset": 180,
			"updated_at": sortTime, "event_id": "sort-a",
		},
	})
	if olderSortTie["stored"] != false || libraryPageStateResponseForTest(t, olderSortTie)["sort"] != "title_asc" {
		t.Fatalf("smaller sort tie = %#v, want rejected canonical title sort", olderSortTie)
	}

	newerSortTie := postJSON(t, s, "/api/library-page-state", map[string]any{
		"state": map[string]any{
			"sort": "pages_desc", "mode": "all", "offset": 181,
			"updated_at": sortTime, "event_id": "sort-c",
		},
	})
	if newerSortTie["stored"] != true {
		t.Fatalf("larger sort tie = %#v, want stored", newerSortTie)
	}
	newSortState := libraryPageStateResponseForTest(t, newerSortTie)
	if newSortState["sort"] != "pages_desc" || newSortState["sort_event_id"] != "sort-c" {
		t.Fatalf("new sort tie canonical state = %#v", newSortState)
	}
	assertLibraryPagePositionForTest(t, newSortState, "all", 180, sortTime, "sort-c")
	assertLibraryPagePositionForTest(t, newSortState, "doujin", 0, sortTime, "sort-c")
	assertLibraryPagePositionForTest(t, newSortState, "series", 0, sortTime, "sort-c")

	var legacyStateAfter string
	if err := s.db.QueryRow(`
		SELECT state_json FROM browse_states
		WHERE reader_profile_key = 'default' AND state_key = 'browse'
	`).Scan(&legacyStateAfter); err != nil {
		t.Fatal(err)
	}
	if legacyStateAfter != legacyStateBefore {
		t.Fatalf("legacy browse state changed:\nbefore %s\nafter  %s", legacyStateBefore, legacyStateAfter)
	}
}

func TestLibraryPageStateRejectsInvalidMutation(t *testing.T) {
	s, err := newServerWithoutCatalogForTest(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	validTime := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	testCases := []struct {
		name  string
		state map[string]any
	}{
		{name: "sort", state: map[string]any{"sort": "recent", "mode": "all", "offset": 0, "updated_at": validTime, "event_id": "event"}},
		{name: "mode", state: map[string]any{"sort": "added_desc", "mode": "books", "offset": 0, "updated_at": validTime, "event_id": "event"}},
		{name: "timestamp", state: map[string]any{"sort": "added_desc", "mode": "all", "offset": 0, "updated_at": "not-a-time", "event_id": "event"}},
		{name: "future", state: map[string]any{"sort": "added_desc", "mode": "all", "offset": 0, "updated_at": time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339Nano), "event_id": "event"}},
		{name: "missing event", state: map[string]any{"sort": "added_desc", "mode": "all", "offset": 0, "updated_at": validTime}},
		{name: "long event", state: map[string]any{"sort": "added_desc", "mode": "all", "offset": 0, "updated_at": validTime, "event_id": strings.Repeat("a", 101)}},
		{name: "unicode event", state: map[string]any{"sort": "added_desc", "mode": "all", "offset": 0, "updated_at": validTime, "event_id": "事件"}},
		{name: "numeric event", state: map[string]any{"sort": "added_desc", "mode": "all", "offset": 0, "updated_at": validTime, "event_id": 123}},
		{name: "boolean event", state: map[string]any{"sort": "added_desc", "mode": "all", "offset": 0, "updated_at": validTime, "event_id": true}},
		{name: "object event", state: map[string]any{"sort": "added_desc", "mode": "all", "offset": 0, "updated_at": validTime, "event_id": map[string]any{}}},
		{name: "missing offset", state: map[string]any{"sort": "added_desc", "mode": "all", "updated_at": validTime, "event_id": "event"}},
		{name: "null offset", state: map[string]any{"sort": "added_desc", "mode": "all", "offset": nil, "updated_at": validTime, "event_id": "event"}},
		{name: "boolean offset", state: map[string]any{"sort": "added_desc", "mode": "all", "offset": true, "updated_at": validTime, "event_id": "event"}},
		{name: "string offset", state: map[string]any{"sort": "added_desc", "mode": "all", "offset": "18", "updated_at": validTime, "event_id": "event"}},
		{name: "object offset", state: map[string]any{"sort": "added_desc", "mode": "all", "offset": map[string]any{}, "updated_at": validTime, "event_id": "event"}},
		{name: "negative offset", state: map[string]any{"sort": "added_desc", "mode": "all", "offset": -1, "updated_at": validTime, "event_id": "event"}},
		{name: "fractional offset", state: map[string]any{"sort": "added_desc", "mode": "all", "offset": 18.5, "updated_at": validTime, "event_id": "event"}},
		{name: "initial offsets", state: map[string]any{"sort": "added_desc", "mode": "all", "offset": 0, "updated_at": validTime, "event_id": "event", "initial_offsets": "bad"}},
		{name: "invalid initial offset", state: map[string]any{"sort": "added_desc", "mode": "all", "offset": 0, "updated_at": validTime, "event_id": "event", "initial_offsets": map[string]any{"doujin": "18"}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"state": testCase.state})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/library-page-state", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(writeIntentHeader, writeIntentValue)
			rec := httptest.NewRecorder()
			s.Routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("invalid %s returned %d: %s", testCase.name, rec.Code, rec.Body.String())
			}
			if testCase.name == "future" {
				var response map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response["code"] != "future_timestamp" || browseStateTime(stringValue(response["server_time"])).IsZero() {
					t.Fatalf("future timestamp response = %#v, want structured server clock", response)
				}
			}
		})
	}
	if payload := getJSON(t, s, "/api/library-page-state"); payload["state"] != nil {
		t.Fatalf("invalid mutations created state: %#v", payload)
	}
}

func TestLibraryPageStateBatchPreservesSortEpochs(t *testing.T) {
	s, err := newServerWithoutCatalogForTest(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seedTime := formatOrderedClientTime(base)
	postJSON(t, s, "/api/library-page-state", map[string]any{
		"state": map[string]any{
			"sort": "added_desc", "mode": "all", "offset": 36,
			"updated_at": seedTime, "event_id": "seed-a",
			"initial_offsets": map[string]any{"all": 36, "doujin": 72, "series": 90},
		},
	})

	sortBTime := formatOrderedClientTime(base.Add(time.Minute))
	returnATime := formatOrderedClientTime(base.Add(2 * time.Minute))
	batch := postJSON(t, s, "/api/library-page-state", map[string]any{
		// Deliberately reversed: the handler must order the event epochs before merging.
		"states": []map[string]any{
			{
				"sort": "added_desc", "mode": "doujin", "offset": 54,
				"updated_at": returnATime, "event_id": "return-a",
			},
			{
				"sort": "title_asc", "mode": "all", "offset": 18,
				"updated_at": sortBTime, "event_id": "sort-b",
			},
		},
	})
	if batch["stored"] != true {
		t.Fatalf("sort epoch batch = %#v, want stored", batch)
	}
	acknowledged, ok := batch["acknowledged_event_ids"].([]any)
	if !ok || len(acknowledged) != 2 || acknowledged[0] != "sort-b" || acknowledged[1] != "return-a" {
		t.Fatalf("batch acknowledgements = %#v, want chronological event ids", batch["acknowledged_event_ids"])
	}
	state := libraryPageStateResponseForTest(t, batch)
	if state["sort"] != "added_desc" || state["sort_event_id"] != "return-a" {
		t.Fatalf("batch final sort = %#v, want returned added_desc epoch", state)
	}
	assertLibraryPagePositionForTest(t, state, "all", 0, returnATime, "return-a")
	assertLibraryPagePositionForTest(t, state, "doujin", 54, returnATime, "return-a")
	assertLibraryPagePositionForTest(t, state, "series", 0, returnATime, "return-a")
}

func TestLibraryPageStateBatchRejectsInvalidPayloadWithoutPartialWrite(t *testing.T) {
	s, err := newServerWithoutCatalogForTest(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	base := time.Now().UTC().Add(-time.Minute)
	validState := func(eventID string, offset int) map[string]any {
		return map[string]any{
			"sort": "added_desc", "mode": "all", "offset": offset,
			"updated_at": base.Format(time.RFC3339Nano), "event_id": eventID,
		}
	}
	tooMany := make([]map[string]any, libraryPageStateMaxBatch+1)
	for index := range tooMany {
		tooMany[index] = validState(fmt.Sprintf("batch-%02d", index), index*libraryPageStatePageSize)
	}
	testCases := []struct {
		name    string
		payload map[string]any
	}{
		{
			name: "valid followed by invalid",
			payload: map[string]any{"states": []map[string]any{
				validState("valid-first", 18),
				{
					"sort": "added_desc", "mode": "doujin", "offset": 36,
					"updated_at": time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339Nano), "event_id": "future-second",
				},
			}},
		},
		{name: "empty", payload: map[string]any{"states": []map[string]any{}}},
		{name: "too many", payload: map[string]any{"states": tooMany}},
		{name: "duplicate event", payload: map[string]any{"states": []map[string]any{validState("duplicate", 18), validState("duplicate", 36)}}},
		{name: "ambiguous", payload: map[string]any{"state": validState("single", 18), "states": []map[string]any{validState("batch", 36)}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			body, err := json.Marshal(testCase.payload)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/library-page-state", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(writeIntentHeader, writeIntentValue)
			rec := httptest.NewRecorder()
			s.Routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("invalid batch returned %d: %s", rec.Code, rec.Body.String())
			}
			if payload := getJSON(t, s, "/api/library-page-state"); payload["state"] != nil {
				t.Fatalf("invalid batch partially wrote state: %#v", payload)
			}
		})
	}
}

func TestLibraryPageStateConcurrentCASKeepsNewestEventPerMode(t *testing.T) {
	s, err := newServerWithoutCatalogForTest(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seedTime := formatOrderedClientTime(base)
	postJSON(t, s, "/api/library-page-state", map[string]any{
		"state": map[string]any{
			"sort": "added_desc", "mode": "all", "offset": 0,
			"updated_at": seedTime, "event_id": "seed",
		},
	})

	const requestCount = 30
	eventTime := formatOrderedClientTime(base.Add(time.Minute))
	type concurrentResult struct {
		status int
		body   string
	}
	start := make(chan struct{})
	results := make(chan concurrentResult, requestCount)
	handler := s.Routes()
	for index := 0; index < requestCount; index++ {
		index := index
		go func() {
			mode := libraryPageStateModes[index%len(libraryPageStateModes)]
			body, marshalErr := json.Marshal(map[string]any{
				"state": map[string]any{
					"sort": "added_desc", "mode": mode, "offset": (index + 1) * libraryPageStatePageSize,
					"updated_at": eventTime, "event_id": fmt.Sprintf("event-%02d", index),
				},
			})
			if marshalErr != nil {
				results <- concurrentResult{body: marshalErr.Error()}
				return
			}
			<-start
			req := httptest.NewRequest(http.MethodPost, "/api/library-page-state", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(writeIntentHeader, writeIntentValue)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			results <- concurrentResult{status: rec.Code, body: rec.Body.String()}
		}()
	}
	close(start)
	for index := 0; index < requestCount; index++ {
		result := <-results
		if result.status != http.StatusOK {
			t.Fatalf("concurrent request returned %d: %s", result.status, result.body)
		}
	}

	finalPayload := getJSON(t, s, "/api/library-page-state")
	finalState := libraryPageStateResponseForTest(t, finalPayload)
	assertLibraryPagePositionForTest(t, finalState, "all", 28*libraryPageStatePageSize, eventTime, "event-27")
	assertLibraryPagePositionForTest(t, finalState, "doujin", 29*libraryPageStatePageSize, eventTime, "event-28")
	assertLibraryPagePositionForTest(t, finalState, "series", 30*libraryPageStatePageSize, eventTime, "event-29")
	if finalState["updated_at"] != eventTime || finalPayload["updated_at"] != eventTime {
		t.Fatalf("concurrent canonical clock = state:%v response:%v, want %s", finalState["updated_at"], finalPayload["updated_at"], eventTime)
	}

	tiedOlderSort := postJSON(t, s, "/api/library-page-state", map[string]any{
		"state": map[string]any{
			"sort": "title_asc", "mode": "all", "offset": 180,
			"updated_at": eventTime, "event_id": "event-28z",
		},
	})
	if tiedOlderSort["stored"] != false || libraryPageStateResponseForTest(t, tiedOlderSort)["sort"] != "added_desc" {
		t.Fatalf("sort tie older than the largest position event = %#v, want rejected", tiedOlderSort)
	}
	tiedNewerSort := postJSON(t, s, "/api/library-page-state", map[string]any{
		"state": map[string]any{
			"sort": "title_asc", "mode": "all", "offset": 181,
			"updated_at": eventTime, "event_id": "event-30",
		},
	})
	if tiedNewerSort["stored"] != true {
		t.Fatalf("sort tie newer than every position event = %#v, want stored", tiedNewerSort)
	}
	tiedNewerState := libraryPageStateResponseForTest(t, tiedNewerSort)
	if tiedNewerState["sort"] != "title_asc" || tiedNewerState["sort_event_id"] != "event-30" {
		t.Fatalf("newest tied sort canonical state = %#v", tiedNewerState)
	}
}

func libraryPageStateResponseForTest(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	state, ok := payload["state"].(map[string]any)
	if !ok {
		t.Fatalf("library page state response = %#v", payload)
	}
	return state
}

func assertLibraryPagePositionForTest(t *testing.T, state map[string]any, mode string, offset int, updatedAt, eventID string) {
	t.Helper()
	positions, ok := state["positions"].(map[string]any)
	if !ok {
		t.Fatalf("library page positions = %#v", state["positions"])
	}
	position, ok := positions[mode].(map[string]any)
	if !ok {
		t.Fatalf("library page position %s = %#v", mode, positions[mode])
	}
	if intValue(position["offset"]) != offset || position["updated_at"] != updatedAt || position["event_id"] != eventID {
		t.Fatalf("library page position %s = %#v, want offset=%d updated_at=%s event_id=%s", mode, position, offset, updatedAt, eventID)
	}
}

func TestReadingHistoryExpressionIndexUpgradesExistingDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmanga-prototype.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE reading_progress (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			reader_profile_key TEXT NOT NULL,
			work_identity_id TEXT NOT NULL,
			candidate_id TEXT,
			page_manifest_id TEXT,
			manifest_hash_snapshot TEXT NOT NULL DEFAULT '',
			progress_status TEXT NOT NULL DEFAULT 'normal',
			last_page_index INTEGER NOT NULL DEFAULT 0,
			progress_percent REAL NOT NULL DEFAULT 0,
			completed INTEGER NOT NULL DEFAULT 0,
			page_count_snapshot INTEGER,
			last_read_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (reader_profile_key, work_identity_id)
		);
		CREATE INDEX idx_reading_progress_recent
			ON reading_progress (reader_profile_key, last_read_at DESC, updated_at DESC, work_identity_id)
			WHERE COALESCE(last_read_at, '') <> '';
		INSERT INTO reading_progress (
			reader_profile_key, work_identity_id, last_read_at, created_at, updated_at
		) VALUES (
			'default', 'legacy-history', '2026-07-10T08:00:00+08:00',
			'2026-07-10T08:00:00+08:00', '2026-07-10T08:00:00+08:00'
		);
	`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := newServerWithoutCatalogForTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	rows, err := s.query(`
		SELECT sql
		FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_reading_progress_recent_julianday'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expression progress index missing after startup upgrade: %#v", rows)
	}
	indexSQL := strings.Join(strings.Fields(stringValue(rows[0]["sql"])), " ")
	for _, expected := range []string{
		"julianday(last_read_at) DESC",
		"julianday(updated_at) DESC",
		"work_identity_id",
		"WHERE last_read_at <> ''",
	} {
		if !strings.Contains(indexSQL, expected) {
			t.Fatalf("expression progress index SQL = %q, missing %q", indexSQL, expected)
		}
	}
	legacyRows, err := s.query(`
		SELECT last_read_at
		FROM reading_progress
		WHERE reader_profile_key = 'default' AND work_identity_id = 'legacy-history'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacyRows) != 1 || stringValue(legacyRows[0]["last_read_at"]) != "2026-07-10T08:00:00+08:00" {
		t.Fatalf("startup index upgrade changed legacy history: %#v", legacyRows)
	}
}

func TestReadingHistoryUsesRequiredProgressJoinAndChronologicalIndex(t *testing.T) {
	s := newCatalogTestServer(t)
	defer s.Close()

	for _, seed := range []struct {
		id         string
		title      string
		lastReadAt string
	}{
		{id: "history-older", title: "Older", lastReadAt: "2026-07-09T23:59:59Z"},
		{id: "history-offset", title: "Alpha Offset", lastReadAt: "2026-07-10T08:00:00+08:00"},
		{id: "history-fraction", title: "Zulu Fraction", lastReadAt: "2026-07-10T00:00:00.500Z"},
	} {
		seedCatalogWork(t, s, seed.id, seed.title, seed.id+".zip", "")
		seedReadableProgress(t, s, seed.id, seed.title, seed.lastReadAt)
	}

	rows, err := s.query(`
		SELECT name
		FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_reading_progress_recent_julianday'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("recent progress index missing: %#v", rows)
	}

	baseSQL := discoverHistoryWorkBaseSQL()
	if !strings.Contains(baseSQL, "JOIN reading_progress rp") || strings.Contains(baseSQL, "LEFT JOIN reading_progress rp") {
		t.Fatalf("history progress join is not required: %s", baseSQL)
	}
	if !strings.Contains(baseSQL, "INDEXED BY idx_reading_progress_recent_julianday") {
		t.Fatalf("history progress join does not pin the selective recent index: %s", baseSQL)
	}
	if !strings.Contains(baseSQL, "rp.last_read_at <> ''") || strings.Contains(baseSQL, "COALESCE(rp.last_read_at, '') <> ''") {
		t.Fatalf("history progress join does not expose the partial-index predicate: %s", baseSQL)
	}

	plan, err := s.query(`
		EXPLAIN QUERY PLAN
		SELECT wb.candidate_id
	`+baseSQL+whereClause([]string{
		"wb.page_count_status = 'counted'",
		"CAST(COALESCE(wb.readable_page_count, 0) AS INTEGER) > 0",
	})+`
		ORDER BY julianday(rp.last_read_at) DESC, julianday(rp.updated_at) DESC, wb.title
		LIMIT ?
	`, 10)
	if err != nil {
		t.Fatal(err)
	}
	progressIndexSeen := false
	for _, row := range plan {
		detail := stringValue(row["detail"])
		if strings.Contains(detail, "idx_reading_progress_recent_julianday") {
			progressIndexSeen = true
		}
		if strings.Contains(detail, "USE TEMP B-TREE FOR ORDER BY") {
			t.Fatalf("history plan regressed to a full temporary ORDER BY sort: %#v", plan)
		}
		if strings.Contains(detail, "SCAN wc") {
			t.Fatalf("history plan regressed to a work-candidate scan: %#v", plan)
		}
	}
	if !progressIndexSeen {
		t.Fatalf("history plan does not use recent progress index: %#v", plan)
	}

	history, err := s.queryReadingHistoryWorks("", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("history length = %d, want 3: %#v", len(history), history)
	}
	wantOrder := []string{"history-fraction", "history-offset", "history-older"}
	for index, want := range wantOrder {
		if got := stringValue(history[index]["candidate_id"]); got != want {
			t.Fatalf("history[%d] candidate = %q, want %q; rows=%#v", index, got, want, history)
		}
		progress, ok := history[index]["progress"].(map[string]any)
		if !ok || stringValue(progress["last_read_at"]) == "" {
			t.Fatalf("history[%d] progress contract missing: %#v", index, history[index])
		}
	}

	searched, err := s.queryReadingHistoryWorks("", "Fraction", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(searched) != 1 || stringValue(searched[0]["candidate_id"]) != "history-fraction" {
		t.Fatalf("history search = %#v, want history-fraction", searched)
	}
}

func newCatalogTestServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := newServerWithoutCatalogForTest(filepath.Join(dataDir, "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureCatalogTables(s.db); err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`
		CREATE TABLE doujin_creator_items (
			candidate_id TEXT NOT NULL,
			creator_display TEXT NOT NULL DEFAULT ''
		)
	`); err != nil {
		t.Fatal(err)
	}
	return s
}

func seedCatalogWork(t *testing.T, s *Server, candidateID string, title string, relativePath string, creator string) {
	t.Helper()
	now := "2026-01-01T00:00:00Z"
	if _, err := s.db.Exec(`
		INSERT INTO work_candidates (
			candidate_id, library_key, library_name, candidate_type, source_kind, title, root, path, relative_path,
			parent_relative_path, source_record_id, source_status, source_reason, size_bytes, modified_utc, extension,
			page_file_count, confidence, notes
		) VALUES (?, 'doujin-lanraragi', '同人本', 'doujin', 'archive', ?, '', '', ?, '', '', 'indexed', '', '1', ?, '.zip', '1', '1.0', '')
	`, candidateID, title, relativePath, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`
		INSERT INTO work_identities (
			work_identity_id, library_key, current_candidate_id, identity_type, display_title,
			canonical_relative_path, match_status, identity_version, first_seen_at, last_seen_at, updated_at
		) VALUES (?, 'doujin-lanraragi', ?, 'work', ?, ?, 'matched', 'test', ?, ?, ?)
	`, "identity-"+candidateID, candidateID, title, relativePath, now, now, now); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(creator) != "" {
		if _, err := s.db.Exec(`
			INSERT INTO doujin_creator_items (candidate_id, creator_display)
			VALUES (?, ?)
		`, candidateID, creator); err != nil {
			t.Fatal(err)
		}
	}
}

func seedReadableProgress(t *testing.T, s *Server, candidateID string, title string, lastReadAt string) {
	t.Helper()
	if _, err := s.db.Exec(`
		INSERT INTO page_counts (
			candidate_id, library_key, candidate_type, source_kind, title, path, extension,
			page_count_status, readable_page_count, total_entry_count, reason, elapsed_ms
		) VALUES (?, 'doujin-lanraragi', 'doujin', 'archive', ?, ?, '.zip', 'counted', 12, 12, 'test', 1)
	`, candidateID, title, candidateID+".zip"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`
		INSERT INTO reading_progress (
			reader_profile_key, work_identity_id, candidate_id, page_manifest_id,
			manifest_hash_snapshot, progress_status, last_page_index, progress_percent,
			completed, page_count_snapshot, last_read_at, created_at, updated_at
		) VALUES ('default', ?, ?, '', '', 'normal', 1, 16.67, 0, 12, ?, ?, ?)
	`, "identity-"+candidateID, candidateID, lastReadAt, lastReadAt, lastReadAt); err != nil {
		t.Fatal(err)
	}
}

func seedSeriesWork(t *testing.T, s *Server, candidateID string, title string, creator string, lastReadAt string) {
	t.Helper()
	seedCatalogWork(t, s, candidateID, title, candidateID+".zip", creator)
	seedReadableProgress(t, s, candidateID, title, lastReadAt)
	if _, err := s.db.Exec(`
		UPDATE work_candidates
		SET library_key = 'commercial-manga', library_name = '漫画', candidate_type = 'manga', extension = '.bin'
		WHERE candidate_id = ?;
		UPDATE work_identities
		SET library_key = 'commercial-manga'
		WHERE current_candidate_id = ?;
		UPDATE page_counts
		SET library_key = 'commercial-manga', candidate_type = 'manga', extension = '.bin'
		WHERE candidate_id = ?;
	`, candidateID, candidateID, candidateID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`
		INSERT INTO work_cover_candidates (
			candidate_id, library_key, candidate_type, source_kind, title, cover_status, cover_kind,
			cover_source_path, cover_source_relative_path, cover_source_record_id,
			requires_extraction, confidence, reason, cover_sort_key
		) VALUES (?, 'commercial-manga', 'manga', 'archive', ?, 'ready', 'archive', ?, ?, '', 1, '1.0', 'test', '0001')
	`, candidateID, title, candidateID+".zip", candidateID+".zip"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`
		INSERT INTO page_manifests (
			page_manifest_id, work_identity_id, candidate_id, manifest_hash,
			page_count, source_kind, manifest_status, builder_version, built_at
		) VALUES (?, ?, ?, ?, 12, 'archive', 'ready', 'go-reader-manifest-v3', ?);
	`, "manifest-"+candidateID, "identity-"+candidateID, candidateID, "hash-"+candidateID, lastReadAt); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`
		UPDATE reading_progress
		SET page_manifest_id = ?, manifest_hash_snapshot = ?
		WHERE reader_profile_key = 'default' AND work_identity_id = ?;
	`, "manifest-"+candidateID, "hash-"+candidateID, "identity-"+candidateID); err != nil {
		t.Fatal(err)
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dbPath := strings.TrimSpace(os.Getenv("BMANGA_TEST_DB_PATH"))
	if dbPath == "" {
		t.Skip("BMANGA_TEST_DB_PATH is required; tests never open the live local database implicitly")
	}
	productionPath, err := filepath.Abs(filepath.Join("..", "..", "data", "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	testPath, err := filepath.Abs(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.EqualFold(filepath.Clean(testPath), filepath.Clean(productionPath)) {
		t.Fatal("BMANGA_TEST_DB_PATH must point to a disposable snapshot, not the live local database")
	}
	if !strings.EqualFold(filepath.Dir(testPath), filepath.Dir(productionPath)) || !strings.HasPrefix(strings.ToLower(filepath.Base(testPath)), "bmanga-test-snapshot-") {
		t.Fatal("BMANGA_TEST_DB_PATH must be a bmanga-test-snapshot-* file in the production data directory")
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Skipf("test database snapshot unavailable: %v", err)
	}
	if productionInfo, productionErr := os.Stat(productionPath); productionErr == nil {
		testInfo, testErr := os.Stat(testPath)
		if testErr != nil {
			t.Fatal(testErr)
		}
		if os.SameFile(productionInfo, testInfo) {
			t.Fatal("BMANGA_TEST_DB_PATH resolves to the live local database")
		}
	}
	s, err := newServerWithoutCatalogForTest(testPath)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func expectedShelfTotal(t *testing.T, s *Server) int {
	t.Helper()
	seriesRows, err := s.queryShelfSeries("", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	seriesTotal := len(seriesRows)
	_, workTotal, err := s.queryShelfWorkLiteItems("", "", "", "", browseSort(""), 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	return seriesTotal + workTotal
}

func legacyShelfPageIDsForTest(t *testing.T, s *Server, query url.Values, limit int, offset int) (int, int, []string) {
	t.Helper()
	total, effectiveOffset, pageItems := legacyShelfPageLiteItemsForTest(t, s, query, limit, offset)
	ids := make([]string, 0, len(pageItems))
	for _, item := range pageItems {
		if item.shelfType == "series" {
			ids = append(ids, "series:"+stringValue(item.data["group_id"]))
		} else {
			ids = append(ids, "work:"+item.candidateID)
		}
	}
	return total, effectiveOffset, ids
}

func legacyShelfPageLiteItemsForTest(t *testing.T, s *Server, query url.Values, limit int, offset int) (int, int, []shelfLiteItem) {
	t.Helper()
	library := strings.TrimSpace(query.Get("library"))
	source := strings.TrimSpace(query.Get("source"))
	pageStatus := strings.TrimSpace(query.Get("pageStatus"))
	q := strings.TrimSpace(query.Get("q"))
	sortKey := browseSort(query.Get("sort"))

	items := []shelfLiteItem{}
	if library != "doujin-lanraragi" && (source == "" || source == "image_folder") {
		seriesRows, err := s.queryShelfSeries(library, q, pageStatus, "", "")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range seriesRows {
			enrichSeries(row)
			row["shelf_type"] = "series"
			items = append(items, shelfLiteFromMap(row))
		}
	}

	workFetchLimit := offset + limit + len(items)
	if workFetchLimit < limit {
		workFetchLimit = limit
	}
	workItems, workTotal, err := s.queryShelfWorkLiteItemsWithFastPath(library, source, pageStatus, q, sortKey, workFetchLimit, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	items = append(items, workItems...)
	sortShelfLiteItems(items, sortKey)

	seriesTotal := 0
	for _, item := range items {
		if item.shelfType == "series" {
			seriesTotal++
		}
	}
	total := workTotal + seriesTotal
	effectiveOffset := offset
	if effectiveOffset > total {
		effectiveOffset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	pageItems := items[effectiveOffset:end]
	return total, effectiveOffset, pageItems
}

func legacyShelfPayloadForTest(t *testing.T, s *Server, query url.Values, limit int, offset int) map[string]any {
	t.Helper()
	total, effectiveOffset, pageItems := legacyShelfPageLiteItemsForTest(t, s, query, limit, offset)
	workIDs := []string{}
	for _, item := range pageItems {
		if item.shelfType == "work" && item.candidateID != "" {
			workIDs = append(workIDs, item.candidateID)
		}
	}
	workByID := map[string]map[string]any{}
	if len(workIDs) > 0 {
		details, err := s.loadWorkListDetails(workIDs)
		if err != nil {
			t.Fatal(err)
		}
		for _, detail := range details {
			detail["shelf_type"] = "work"
			workByID[stringValue(detail["candidate_id"])] = detail
		}
	}
	responseItems := make([]map[string]any, 0, len(pageItems))
	for _, item := range pageItems {
		if item.shelfType == "series" {
			responseItems = append(responseItems, item.data)
			continue
		}
		if detail := workByID[item.candidateID]; detail != nil {
			responseItems = append(responseItems, detail)
			continue
		}
		responseItems = append(responseItems, map[string]any{
			"shelf_type":          "work",
			"candidate_id":        item.candidateID,
			"library_key":         item.libraryKey,
			"title":               item.title,
			"display_title":       item.title,
			"modified_utc":        item.modified,
			"added_utc":           item.added,
			"readable_page_count": item.pages,
		})
	}
	payload := map[string]any{"total": total, "limit": limit, "offset": effectiveOffset, "items": responseItems}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(body, &normalized); err != nil {
		t.Fatal(err)
	}
	return normalized
}

func shelfPayloadIDsForTest(payload map[string]any) []string {
	items, _ := payload["items"].([]any)
	ids := make([]string, 0, len(items))
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if stringValue(item["shelf_type"]) == "series" {
			ids = append(ids, "series:"+stringValue(item["group_id"]))
		} else {
			ids = append(ids, "work:"+stringValue(item["candidate_id"]))
		}
	}
	return ids
}

func cloneURLValuesForTest(source url.Values) url.Values {
	cloned := url.Values{}
	for key, values := range source {
		cloned[key] = append([]string{}, values...)
	}
	return cloned
}

func getJSON(t *testing.T, s *Server, path string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s returned %d: %s", path, rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func postJSON(t *testing.T, s *Server, path string, payload map[string]any) map[string]any {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(writeIntentHeader, writeIntentValue)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s returned %d: %s", path, rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}
