package prototype

import (
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSONError(w, http.StatusNotFound, "unknown endpoint")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if r.URL.Path == "/" {
		location := (&url.URL{Path: "/v2/", RawQuery: r.URL.RawQuery}).String()
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, location, http.StatusTemporaryRedirect)
		return
	}
	s.serveStatic(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	writeJSON(w, map[string]any{
		"ok":      true,
		"service": "bmanga-go",
	})
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	libraries, err := s.query(`
		SELECT *
		FROM library_dashboard
		ORDER BY library_key
	`)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	pageStatus, err := s.query(`
		SELECT page_count_status AS status, COUNT(*) AS count
		FROM page_counts
		GROUP BY page_count_status
	`)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	totalsRows, err := s.query(`
		SELECT
			COUNT(*) AS works,
			SUM(CASE WHEN candidate_type = 'doujin' THEN 1 ELSE 0 END) AS doujin,
			SUM(CASE WHEN candidate_type != 'doujin' THEN 1 ELSE 0 END) AS manga,
			0 AS page_counted
		FROM work_candidates
	`)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	totals := firstRow(totalsRows)
	pageCountRows, err := s.query(`
		SELECT COUNT(*) AS count
		FROM page_counts
		WHERE page_count_status = 'counted'
	`)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	totals["page_counted"] = firstRow(pageCountRows)["count"]
	writeJSON(w, map[string]any{
		"totals":      totals,
		"libraries":   libraries,
		"page_status": pageStatus,
		"actions":     []map[string]any{},
	})
}
