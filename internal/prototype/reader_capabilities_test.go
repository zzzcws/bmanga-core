package prototype

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicReaderCapabilitiesFailClosed(t *testing.T) {
	tests := []struct {
		name       string
		sourceKind string
		extension  string
		reason     string
		supported  bool
	}{
		{name: "image folder", sourceKind: "image_folder", supported: true},
		{name: "zip", sourceKind: "archive", extension: ".zip", supported: true},
		{name: "cbz", sourceKind: "archive", extension: ".cbz", supported: true},
		{name: "image epub", sourceKind: "ebook", extension: ".epub", supported: true},
		{name: "pdf", sourceKind: "pdf", extension: ".pdf"},
		{name: "zip containing pdf", sourceKind: "archive", extension: ".zip", reason: "zip_contains_pdf"},
		{name: "seven zip", sourceKind: "archive", extension: ".7z"},
		{name: "mobi", sourceKind: "ebook", extension: ".mobi"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			work := map[string]any{
				"source_kind":       test.sourceKind,
				"extension":         test.extension,
				"page_count_reason": test.reason,
			}
			if got := publicReaderSourceSupported(work); got != test.supported {
				t.Fatalf("supported = %v, want %v", got, test.supported)
			}
		})
	}
}

func TestUnsupportedReaderSourcesReturnExplicitStatus(t *testing.T) {
	s := newCatalogTestServer(t)
	defer s.Close()

	tests := []struct {
		id         string
		sourceKind string
		extension  string
		reason     string
	}{
		{id: "unsupported-pdf", sourceKind: "pdf", extension: ".pdf"},
		{id: "unsupported-zip-pdf", sourceKind: "archive", extension: ".zip", reason: "zip_contains_pdf"},
		{id: "unsupported-seven-zip", sourceKind: "archive", extension: ".7z"},
		{id: "unsupported-mobi", sourceKind: "ebook", extension: ".mobi"},
	}
	for _, test := range tests {
		seedCatalogWork(t, s, test.id, test.id, test.id+test.extension, "")
		if _, err := s.db.Exec(`
			UPDATE work_candidates
			SET source_kind = ?, extension = ?
			WHERE candidate_id = ?
		`, test.sourceKind, test.extension, test.id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`
			INSERT INTO page_counts (
				candidate_id, library_key, candidate_type, source_kind, title, path, extension,
				page_count_status, readable_page_count, total_entry_count, reason, elapsed_ms
			) VALUES (?, 'doujin-lanraragi', 'doujin', ?, ?, '', ?, 'counted', 10, 10, ?, 1)
		`, test.id, test.sourceKind, test.id, test.extension, test.reason); err != nil {
			t.Fatal(err)
		}
		coverKind := "archive"
		if test.sourceKind == "pdf" || test.reason == "zip_contains_pdf" {
			coverKind = "pdf"
		} else if test.sourceKind == "ebook" {
			coverKind = "ebook"
		}
		if _, err := s.db.Exec(`
			INSERT INTO work_cover_candidates (
				candidate_id, library_key, candidate_type, source_kind, title,
				cover_status, cover_kind, cover_source_path, cover_source_relative_path
			) VALUES (?, 'doujin-lanraragi', 'doujin', ?, ?, 'ready', ?, ?, ?)
		`, test.id, test.sourceKind, test.id, coverKind, test.id+test.extension, test.id+test.extension); err != nil {
			t.Fatal(err)
		}

		for _, path := range []string{
			"/api/pages?id=" + test.id,
			"/page?id=" + test.id + "&index=0",
		} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			s.Routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("%s returned %d, want %d: %s", path, rec.Code, http.StatusUnsupportedMediaType, rec.Body.String())
			}
		}

		coverRequest := httptest.NewRequest(http.MethodGet, "/cover?id="+test.id, nil)
		coverRecorder := httptest.NewRecorder()
		s.Routes().ServeHTTP(coverRecorder, coverRequest)
		if coverRecorder.Code != http.StatusNotFound {
			t.Fatalf("unsupported cover returned %d, want %d", coverRecorder.Code, http.StatusNotFound)
		}
	}
}

func TestUnsupportedStoredPageKindsAreRejected(t *testing.T) {
	for _, sourceType := range []string{"zip_pdf_page", "sevenzip_inner", "pdf_page"} {
		if !unsupportedReaderRowSourceType(sourceType) {
			t.Fatalf("source type %q was not rejected", sourceType)
		}
	}
	if publicStoredManifestRowsSupported([]map[string]any{{"source_inner_path": "chapter.pdf#page=1"}}) {
		t.Fatal("stored ZIP-PDF manifest was accepted")
	}
}
