package prototype

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestRemovedCapabilitiesAreNotRegistered(t *testing.T) {
	s, err := NewServer(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	tests := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/classic"},
		{method: http.MethodGet, path: "/classic/review"},
		{method: http.MethodGet, path: "/app.min.js"},
		{method: http.MethodGet, path: "/api/online-search"},
		{method: http.MethodGet, path: "/api/online-search/providers"},
		{method: http.MethodPost, path: "/api/online-search/download"},
		{method: http.MethodGet, path: "/api/online-search/downloads"},
		{method: http.MethodPost, path: "/api/online-search/downloads/clear"},
		{method: http.MethodGet, path: "/api/online-search/updates"},
		{method: http.MethodPost, path: "/api/online-search/updates/scan"},
		{method: http.MethodPost, path: "/api/online-search/updates/download"},
		{method: http.MethodGet, path: "/api/online-search/shelf"},
		{method: http.MethodPost, path: "/api/online-search/shelf/reset"},
		{method: http.MethodGet, path: "/api/online-comments"},
		{method: http.MethodGet, path: "/api/service-status"},
		{method: http.MethodGet, path: "/api/cache-status"},
		{method: http.MethodPost, path: "/api/cache-cleanup"},
		{method: http.MethodGet, path: "/api/task-center"},
		{method: http.MethodGet, path: "/api/import-pending"},
		{method: http.MethodGet, path: "/api/import-pending/blacklist"},
		{method: http.MethodPost, path: "/api/import-pending/decision"},
		{method: http.MethodPost, path: "/api/import-pending/apply"},
		{method: http.MethodGet, path: "/api/author-blacklist"},
		{method: http.MethodPost, path: "/api/author-blacklist"},
		{method: http.MethodGet, path: "/api/external-sync/status"},
		{method: http.MethodGet, path: "/api/external-sync/local-series"},
		{method: http.MethodGet, path: "/api/external-sync/bangumi/check"},
		{method: http.MethodPost, path: "/api/external-sync/links"},
		{method: http.MethodPost, path: "/api/tasks/gpu-duplicate-suggestions"},
		{method: http.MethodPost, path: "/api/tasks/gpu-cover-visual-index"},
		{method: http.MethodPost, path: "/api/tasks/cancel"},
		{method: http.MethodGet, path: "/api/metadata-scrape/sources"},
		{method: http.MethodPost, path: "/api/metadata-scrape/start"},
		{method: http.MethodGet, path: "/api/cleanup-preview"},
		{method: http.MethodPost, path: "/api/cleanup-apply"},
		{method: http.MethodGet, path: "/api/metadata-import-review"},
		{method: http.MethodGet, path: "/api/import-review"},
		{method: http.MethodPost, path: "/api/import-review/decision"},
		{method: http.MethodGet, path: "/api/import-review/similar-covers"},
		{method: http.MethodGet, path: "/api/metadata-overlays"},
		{method: http.MethodPost, path: "/api/metadata-overlay-apply"},
		{method: http.MethodGet, path: "/api/duplicate-cleanup-suggestions"},
		{method: http.MethodPost, path: "/api/duplicate-cleanup-suggestions/apply"},
		{method: http.MethodPost, path: "/api/duplicate-candidates/status"},
		{method: http.MethodPost, path: "/api/duplicate-pair/evidence-report"},
		{method: http.MethodGet, path: "/api/visual-review"},
		{method: http.MethodGet, path: "/api/cover-similar"},
		{method: http.MethodGet, path: "/api/page-similar"},
		{method: http.MethodGet, path: "/api/cover-search-image"},
		{method: http.MethodGet, path: "/api/page-search-image"},
		{method: http.MethodGet, path: "/api/page-image-search-coverage"},
		{method: http.MethodGet, path: "/page-cache"},
		{method: http.MethodGet, path: "/import-review-thumb/example"},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, nil)
			if isWriteMethod(test.method) {
				request.Header.Set(writeIntentHeader, writeIntentValue)
			}
			s.Routes().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusNotFound, recorder.Body.String())
			}
		})
	}
}
