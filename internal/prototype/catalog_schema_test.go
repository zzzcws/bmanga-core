package prototype

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestNewServerCreatesEmptyCatalogSchema(t *testing.T) {
	s, err := NewServer(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	s.Routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var scanEntriesTableCount int
	if err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table' AND name = 'scan_entries'
	`).Scan(&scanEntriesTableCount); err != nil {
		t.Fatal(err)
	}
	if scanEntriesTableCount != 1 {
		t.Fatalf("scan_entries table count = %d, want 1", scanEntriesTableCount)
	}
}

func TestCatalogTablesSupportCandidateUpserts(t *testing.T) {
	s, err := NewServer(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	statements := []string{
		`INSERT INTO work_candidates (candidate_id, title) VALUES ('work-1', 'old')
			ON CONFLICT(candidate_id) DO UPDATE SET title = excluded.title`,
		`INSERT INTO work_candidates (candidate_id, title) VALUES ('work-1', 'new')
			ON CONFLICT(candidate_id) DO UPDATE SET title = excluded.title`,
		`INSERT INTO page_counts (candidate_id, reason) VALUES ('work-1', 'old')
			ON CONFLICT(candidate_id) DO UPDATE SET reason = excluded.reason`,
		`INSERT INTO page_counts (candidate_id, reason) VALUES ('work-1', 'new')
			ON CONFLICT(candidate_id) DO UPDATE SET reason = excluded.reason`,
		`INSERT INTO work_cover_candidates (candidate_id, reason) VALUES ('work-1', 'old')
			ON CONFLICT(candidate_id) DO UPDATE SET reason = excluded.reason`,
		`INSERT INTO work_cover_candidates (candidate_id, reason) VALUES ('work-1', 'new')
			ON CONFLICT(candidate_id) DO UPDATE SET reason = excluded.reason`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}

	for _, check := range []struct {
		table  string
		column string
		want   string
	}{
		{table: "work_candidates", column: "title", want: "new"},
		{table: "page_counts", column: "reason", want: "new"},
		{table: "work_cover_candidates", column: "reason", want: "new"},
	} {
		var got string
		if err := s.db.QueryRow(
			"SELECT " + check.column + " FROM " + check.table + " WHERE candidate_id = 'work-1'",
		).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != check.want {
			t.Fatalf("%s.%s = %q, want %q", check.table, check.column, got, check.want)
		}
	}
}

func TestImageFolderPageRowsNormalizesPathSeparators(t *testing.T) {
	s, err := NewServer(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for _, entry := range []struct {
		stableKey    string
		path         string
		relativePath string
	}{
		{stableKey: "page-1", path: "first.jpg", relativePath: "Series/Folder/001.jpg"},
		{stableKey: "page-2", path: "second.jpg", relativePath: `Series\Folder\002.jpg`},
		{stableKey: "sibling", path: "third.jpg", relativePath: "Series/Folderish/003.jpg"},
	} {
		if _, err := s.db.Exec(`
			INSERT INTO scan_entries (
				stable_key, library_key, path, relative_path, entry_type,
				item_kind, status, extension
			) VALUES (?, 'library', ?, ?, 'file', 'image_file', 'indexed_as_page', '.jpg')
		`, entry.stableKey, entry.path, entry.relativePath); err != nil {
			t.Fatal(err)
		}
	}

	rows, manifest, err := s.imageFolderPageRows(map[string]any{
		"candidate_id":  "image-folder",
		"library_key":   "library",
		"relative_path": `Series\Folder`,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if manifest != nil {
		t.Fatalf("manifest = %#v, want nil", manifest)
	}
	if len(rows) != 2 {
		t.Fatalf("page rows = %d, want 2: %#v", len(rows), rows)
	}
	if got := stringValue(rows[0]["relative_path"]); got != "Series/Folder/001.jpg" {
		t.Fatalf("first relative path = %q", got)
	}
	if got := stringValue(rows[1]["relative_path"]); got != `Series\Folder\002.jpg` {
		t.Fatalf("second relative path = %q", got)
	}
}
