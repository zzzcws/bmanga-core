package prototype

import (
	"bytes"
	"encoding/json"
	rand "math/rand/v2"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

func seedDiscoverStateWork(t *testing.T, s *Server, candidateID string, withProgress bool, completed bool) {
	t.Helper()
	seedCatalogWork(t, s, candidateID, candidateID, candidateID+".zip", "")
	seedReadableProgress(t, s, candidateID, candidateID, "2026-07-11T08:00:00Z")
	if !withProgress {
		if _, err := s.db.Exec(`DELETE FROM reading_progress WHERE work_identity_id = ?`, "identity-"+candidateID); err != nil {
			t.Fatal(err)
		}
		return
	}
	if completed {
		if _, err := s.db.Exec(`
			UPDATE reading_progress
			SET last_page_index = 11, progress_percent = 100, completed = 1
			WHERE work_identity_id = ?
		`, "identity-"+candidateID); err != nil {
			t.Fatal(err)
		}
	}
}

func seedDiscoverStateMark(t *testing.T, s *Server, candidateID string, readStatus string, favorite bool) {
	t.Helper()
	favoriteValue := 0
	if favorite {
		favoriteValue = 1
	}
	if _, err := s.db.Exec(`
		INSERT INTO work_user_marks (
			reader_profile_key, work_identity_id, candidate_id, read_status, favorite,
			marked_at, created_at, updated_at
		) VALUES ('default', ?, ?, ?, ?, '2026-07-11T08:00:00Z', '2026-07-11T08:00:00Z', '2026-07-11T08:00:00Z')
	`, "identity-"+candidateID, candidateID, readStatus, favoriteValue); err != nil {
		t.Fatal(err)
	}
}

func discoverStateIDs(t *testing.T, s *Server, mode string) []string {
	t.Helper()
	items, err := s.queryDiscoverRandomWorks(mode, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, stringValue(item["candidate_id"]))
	}
	sort.Strings(ids)
	return ids
}

func legacyDiscoverStateIDs(t *testing.T, s *Server, mode string, library string) []string {
	t.Helper()
	filters := []string{}
	args := []any{}
	addDiscoverWorkFilters(&filters, &args, library, "")
	addDiscoverRandomModeFilter(&filters, mode)
	rows, err := s.query(`
		SELECT wb.candidate_id
	`+discoverWorkBaseSQL()+whereClause(filters), args...)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, stringValue(row["candidate_id"]))
	}
	sort.Strings(ids)
	return ids
}

func discoverStateIDsForLibrary(t *testing.T, s *Server, mode string, library string) []string {
	t.Helper()
	items, err := s.queryDiscoverRandomWorks(mode, library, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, stringValue(item["candidate_id"]))
	}
	sort.Strings(ids)
	return ids
}

func TestDiscoverRandomCandidateSelectionUsesFastPathOnlyWithoutSearch(t *testing.T) {
	wideFilters := []string{"wb.library_key = ?"}
	fastFilters := []string{"pc.page_count_status = 'counted'", "wc.library_key = ?"}

	fastSQL := discoverFastRandomCandidateWindowSQL("unread", fastFilters, false)
	for _, fragment := range []string{
		"FROM work_candidates wc INDEXED BY idx_work_candidates_candidate_id",
		"CROSS JOIN page_counts pc INDEXED BY idx_page_counts_candidate_status_reason",
		"LEFT JOIN work_identities wi ON wi.current_candidate_id = wc.candidate_id",
		"LEFT JOIN work_user_marks wum",
		"LEFT JOIN reading_progress rp",
		"wc.candidate_id >= ?",
		"ORDER BY wc.candidate_id",
	} {
		if !strings.Contains(fastSQL, fragment) {
			t.Fatalf("fast random SQL missing %q:\n%s", fragment, fastSQL)
		}
	}
	if strings.Contains(fastSQL, "FROM work_browse wb") || strings.Contains(fastSQL, "ORDER BY RANDOM()") {
		t.Fatalf("fast random SQL regressed to a wide random sort:\n%s", fastSQL)
	}
	wrapSQL := discoverFastRandomCandidateWindowSQL("unread", fastFilters, true)
	if !strings.Contains(wrapSQL, "wc.candidate_id < ?") || strings.Contains(wrapSQL, "wc.candidate_id >= ?") {
		t.Fatalf("wrapped random SQL lost its lower key range:\n%s", wrapSQL)
	}

	searchSQL := discoverWideRandomCandidateSelectionSQL(wideFilters)
	if !strings.Contains(searchSQL, "FROM work_browse wb") {
		t.Fatalf("search random SQL did not use the wide fallback:\n%s", searchSQL)
	}
	if !strings.Contains(searchSQL, "ORDER BY RANDOM()") {
		t.Fatalf("search fallback unexpectedly lost random selection:\n%s", searchSQL)
	}
	if strings.Contains(searchSQL, "INDEXED BY idx_page_counts_candidate_status_reason") {
		t.Fatalf("search random SQL unexpectedly used the fast candidate path:\n%s", searchSQL)
	}

	sparseSQL := discoverFastRandomCandidateWindowSQL("liked", fastFilters, false)
	if !strings.Contains(sparseSQL, "FROM work_user_marks wum") || strings.Contains(sparseSQL, "LEFT JOIN work_user_marks wum") {
		t.Fatalf("sparse mark mode is not driven by work_user_marks:\n%s", sparseSQL)
	}
	if !strings.Contains(sparseSQL, "CROSS JOIN work_candidates wc INDEXED BY idx_work_candidates_candidate_id") {
		t.Fatalf("sparse mark mode lost the candidate lookup index:\n%s", sparseSQL)
	}
}

func TestDiscoverRandomCandidatePivotIsFixedWidthHex(t *testing.T) {
	pivot, err := discoverRandomCandidatePivot(bytes.NewReader([]byte{
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05,
		0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if pivot != "000102030405060708090a0b" {
		t.Fatalf("pivot = %q, want fixed-width 96-bit hex", pivot)
	}
	if _, err := discoverRandomCandidatePivot(bytes.NewReader([]byte{0x01})); err == nil {
		t.Fatal("short random source unexpectedly produced a pivot")
	}
}

func TestDiscoverCandidatePoolLimitStaysBounded(t *testing.T) {
	for _, testCase := range []struct {
		limit int
		want  int
	}{
		{limit: 0, want: 0},
		{limit: 1, want: discoverRandomCandidatePoolMin},
		{limit: 8, want: 64},
		{limit: 24, want: 192},
		{limit: 1000, want: discoverRandomCandidatePoolMax},
	} {
		if got := discoverRandomCandidatePoolLimit(testCase.limit); got != testCase.want {
			t.Fatalf("pool limit(%d) = %d, want %d", testCase.limit, got, testCase.want)
		}
	}
}

func TestDiscoverSampleCandidatePoolHasBasicUniformDistribution(t *testing.T) {
	pool := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	const (
		trials = 20_000
		limit  = 3
	)
	rng := rand.New(rand.NewPCG(0x12345678, 0x9abcdef0))
	counts := map[string]int{}
	for trial := 0; trial < trials; trial++ {
		selected := discoverSampleCandidatePool(pool, limit, rng.IntN)
		if len(selected) != limit {
			t.Fatalf("sample length = %d, want %d", len(selected), limit)
		}
		seen := map[string]bool{}
		for _, candidateID := range selected {
			if seen[candidateID] {
				t.Fatalf("sample contains duplicate %q: %#v", candidateID, selected)
			}
			seen[candidateID] = true
			counts[candidateID]++
		}
	}
	want := trials * limit / len(pool)
	tolerance := want / 20
	for _, candidateID := range pool {
		got := counts[candidateID]
		if got < want-tolerance || got > want+tolerance {
			t.Fatalf("candidate %q selected %d times, want %d +/- %d; counts=%#v", candidateID, got, want, tolerance, counts)
		}
	}
}

func TestDiscoverFastRandomCandidatePoolWrapsAndHonorsFilters(t *testing.T) {
	s := newCatalogTestServer(t)
	defer s.Close()

	ids := []string{
		"100000000000000000000000",
		"200000000000000000000000",
		"400000000000000000000000",
		"800000000000000000000000",
		"f00000000000000000000000",
	}
	for _, candidateID := range ids {
		seedDiscoverStateWork(t, s, candidateID, false, false)
	}
	if _, err := s.db.Exec(`
		UPDATE work_candidates
		SET library_key = 'commercial-manga', library_name = '漫画'
		WHERE candidate_id = '400000000000000000000000';
		UPDATE work_identities
		SET library_key = 'commercial-manga'
		WHERE current_candidate_id = '400000000000000000000000';
		UPDATE page_counts
		SET library_key = 'commercial-manga', candidate_type = 'manga'
		WHERE candidate_id = '400000000000000000000000';
	`); err != nil {
		t.Fatal(err)
	}

	filters, args := discoverFastRandomFilterPlan("any", "doujin-lanraragi", "")
	got, err := s.queryDiscoverFastRandomCandidatePool(
		"any", filters, args, "700000000000000000000000", 3,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"800000000000000000000000",
		"f00000000000000000000000",
		"100000000000000000000000",
	}
	if !equalStrings(got, want) {
		t.Fatalf("wrapped filtered pool = %#v, want %#v", got, want)
	}
}

func TestDiscoverFastRandomCandidatePoolReturnsEmptyWithoutEligibleWorks(t *testing.T) {
	s := newCatalogTestServer(t)
	defer s.Close()
	filters, args := discoverFastRandomFilterPlan("unread", "", "")
	got, err := s.queryDiscoverFastRandomCandidatePool(
		"unread", filters, args, "800000000000000000000000", 8,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("empty candidate pool = %#v, want none", got)
	}
}

func TestDiscoverFastRandomFilterPlanCoversEveryUnsearchedMode(t *testing.T) {
	for _, mode := range []string{"any", "unread", "reading", "completed", "abandoned", "liked", "strong-liked", "rated", "reread"} {
		filters, args := discoverFastRandomFilterPlan(mode, "doujin-lanraragi", "")
		joined := strings.Join(filters, "\n")
		for _, required := range []string{"pc.page_count_status = 'counted'", "wc.library_key = ?"} {
			if !strings.Contains(joined, required) {
				t.Fatalf("mode %q fast filters missing %q: %#v", mode, required, filters)
			}
		}
		if len(args) != 1 || stringValue(args[0]) != "doujin-lanraragi" {
			t.Fatalf("mode %q library args = %#v", mode, args)
		}
	}
	if filters, args := discoverFastRandomFilterPlan("unread", "doujin-lanraragi", "needle"); len(filters) != 0 || len(args) != 0 {
		t.Fatalf("searched random filter plan must stay on wide SQL: filters=%#v args=%#v", filters, args)
	}
}

func TestDiscoverFastRandomCandidateMatchesWideQueryAcrossModes(t *testing.T) {
	s := newCatalogTestServer(t)
	defer s.Close()

	seedDiscoverStateWork(t, s, "plain-unread", false, false)
	seedDiscoverStateWork(t, s, "reading-progress", true, false)
	seedDiscoverStateWork(t, s, "completed-progress", true, true)
	seedDiscoverStateWork(t, s, "abandoned-progress", true, false)
	seedDiscoverStateMark(t, s, "abandoned-progress", "abandoned", false)
	seedDiscoverStateWork(t, s, "favorite-unread", false, false)
	seedDiscoverStateMark(t, s, "favorite-unread", "unread", true)
	seedDiscoverStateWork(t, s, "strong-needle", false, false)
	seedDiscoverStateMark(t, s, "strong-needle", "unread", false)
	seedDiscoverStateWork(t, s, "rated-unread", false, false)
	seedDiscoverStateMark(t, s, "rated-unread", "unread", false)
	seedDiscoverStateWork(t, s, "reread-unread", false, false)
	seedDiscoverStateMark(t, s, "reread-unread", "unread", false)
	if _, err := s.db.Exec(`
		UPDATE work_candidates SET title = 'Strong Needle' WHERE candidate_id = 'strong-needle';
		UPDATE work_user_marks SET personal_rating = 9 WHERE work_identity_id = 'identity-strong-needle';
		UPDATE work_user_marks SET personal_rating = 4 WHERE work_identity_id = 'identity-rated-unread';
		UPDATE work_user_marks SET reread_priority = 2 WHERE work_identity_id = 'identity-reread-unread';
	`); err != nil {
		t.Fatal(err)
	}

	seedDiscoverStateWork(t, s, "other-library", false, false)
	if _, err := s.db.Exec(`
		UPDATE work_candidates
		SET library_key = 'commercial-manga', library_name = '漫画'
		WHERE candidate_id = 'other-library';
		UPDATE work_identities
		SET library_key = 'commercial-manga'
		WHERE current_candidate_id = 'other-library';
		UPDATE page_counts
		SET library_key = 'commercial-manga', candidate_type = 'manga'
		WHERE candidate_id = 'other-library';
	`); err != nil {
		t.Fatal(err)
	}

	modes := []string{"any", "unread", "reading", "completed", "abandoned", "liked", "strong-liked", "rated", "reread"}
	libraries := []string{"", "doujin-lanraragi", "commercial-manga"}
	for _, library := range libraries {
		for _, mode := range modes {
			got := discoverStateIDsForLibrary(t, s, mode, library)
			want := legacyDiscoverStateIDs(t, s, mode, library)
			if !equalStrings(got, want) {
				t.Fatalf("fast discover mode=%q library=%q IDs = %#v, legacy = %#v", mode, library, got, want)
			}
		}
	}

	searched, err := s.queryDiscoverRandomWorks("any", "", "Needle", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(searched) != 1 || stringValue(searched[0]["candidate_id"]) != "strong-needle" {
		t.Fatalf("search fallback = %#v, want strong-needle", searched)
	}
}

func TestDiscoverModesDeriveReadStateFromProgress(t *testing.T) {
	s := newCatalogTestServer(t)
	defer s.Close()

	seedDiscoverStateWork(t, s, "reading-only", true, false)
	seedDiscoverStateWork(t, s, "favorite-unread", false, false)
	seedDiscoverStateMark(t, s, "favorite-unread", "unread", true)
	seedDiscoverStateWork(t, s, "favorite-reading", true, false)
	seedDiscoverStateMark(t, s, "favorite-reading", "unread", true)
	seedDiscoverStateWork(t, s, "completed-progress", true, true)
	seedDiscoverStateWork(t, s, "abandoned-progress", true, false)
	seedDiscoverStateMark(t, s, "abandoned-progress", "abandoned", false)
	seedDiscoverStateWork(t, s, "manual-reading", false, false)
	seedDiscoverStateMark(t, s, "manual-reading", "reading", false)
	seedDiscoverStateWork(t, s, "manual-completed", false, false)
	seedDiscoverStateMark(t, s, "manual-completed", "completed", false)

	if got, want := discoverStateIDs(t, s, "unread"), []string{"favorite-unread"}; !equalStrings(got, want) {
		t.Fatalf("unread IDs = %#v, want %#v", got, want)
	}
	if got, want := discoverStateIDs(t, s, "reading"), []string{"favorite-reading", "manual-reading", "reading-only"}; !equalStrings(got, want) {
		t.Fatalf("reading IDs = %#v, want %#v", got, want)
	}
	if got, want := discoverStateIDs(t, s, "completed"), []string{"completed-progress", "manual-completed"}; !equalStrings(got, want) {
		t.Fatalf("completed IDs = %#v, want %#v", got, want)
	}
	if got, want := discoverStateIDs(t, s, "abandoned"), []string{"abandoned-progress"}; !equalStrings(got, want) {
		t.Fatalf("abandoned IDs = %#v, want %#v", got, want)
	}
	if got, want := discoverStateIDs(t, s, "liked"), []string{"favorite-reading", "favorite-unread"}; !equalStrings(got, want) {
		t.Fatalf("liked IDs = %#v, want %#v", got, want)
	}
}

func TestDiscoverModesFollowProgressIdentityToCurrentCandidate(t *testing.T) {
	s := newCatalogTestServer(t)
	defer s.Close()

	seedDiscoverStateWork(t, s, "identity-old", true, false)
	seedDiscoverStateWork(t, s, "identity-current", false, false)
	if _, err := s.db.Exec(`
		DELETE FROM work_identities WHERE work_identity_id = 'identity-identity-current';
		UPDATE work_identities
		SET current_candidate_id = 'identity-current', display_title = 'identity-current'
		WHERE work_identity_id = 'identity-identity-old';
		DELETE FROM work_candidates WHERE candidate_id = 'identity-old';
	`); err != nil {
		t.Fatal(err)
	}

	if got, want := discoverStateIDs(t, s, "reading"), []string{"identity-current"}; !equalStrings(got, want) {
		t.Fatalf("identity-migrated reading IDs = %#v, want %#v", got, want)
	}
	if got := discoverStateIDs(t, s, "unread"); len(got) != 0 {
		t.Fatalf("identity-migrated unread IDs = %#v, want none", got)
	}
}

func TestDiscoverResponseOmitsMaintenancePayload(t *testing.T) {
	s := newCatalogTestServer(t)
	defer s.Close()
	seedDiscoverStateWork(t, s, "lean-visible", true, false)

	lean := getJSON(t, s, "/api/discover?randomMode=any&randomLimit=4&historyLimit=4&lean=1")
	if _, exists := lean["pending"]; exists {
		t.Fatalf("lean discover unexpectedly includes pending: %#v", lean["pending"])
	}
	if _, exists := lean["online_pending"]; exists {
		t.Fatalf("lean discover unexpectedly includes online_pending: %#v", lean["online_pending"])
	}
	if intValue(lean["total"]) != 2 {
		t.Fatalf("lean discover total = %#v, want random + history = 2", lean["total"])
	}
	if len(lean["random_items"].([]any)) != 1 || len(lean["history"].([]any)) != 1 {
		t.Fatalf("lean discover content = %#v", lean)
	}

	full := getJSON(t, s, "/api/discover?randomMode=any&randomLimit=4&historyLimit=4")
	if _, exists := full["pending"]; exists {
		t.Fatalf("discover unexpectedly includes pending maintenance payload: %#v", full["pending"])
	}
	if _, exists := full["online_pending"]; exists {
		t.Fatalf("classic discover unexpectedly includes removed online_pending payload: %#v", full["online_pending"])
	}
}

func TestDiscoverOptionalBranchesAndServerTiming(t *testing.T) {
	s := newCatalogTestServer(t)
	defer s.Close()
	seedDiscoverStateWork(t, s, "partial-visible", true, false)

	req := httptest.NewRequest(http.MethodGet, "/api/discover?randomMode=any&randomLimit=4&historyLimit=4&lean=1&includeHistory=0&includeStats=false", nil)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("partial discover returned %d: %s", rec.Code, rec.Body.String())
	}
	payload := map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["history"]; exists {
		t.Fatalf("partial discover unexpectedly returned history: %#v", payload)
	}
	if _, exists := payload["stats"]; exists {
		t.Fatalf("partial discover unexpectedly returned stats: %#v", payload)
	}
	if got := intValue(payload["total"]); got != 1 {
		t.Fatalf("partial discover total = %d, want returned random count 1", got)
	}
	timing := rec.Header().Get("Server-Timing")
	for _, phase := range []string{"app;dur=", "discoverRandom;dur=", "discoverSelect;dur=", "discoverDetail;dur=", "discoverEnrich;dur="} {
		if !strings.Contains(timing, phase) {
			t.Fatalf("partial Server-Timing = %q, missing %q", timing, phase)
		}
	}
	for _, omitted := range []string{"discoverHistory;dur=", "discoverStats;dur="} {
		if strings.Contains(timing, omitted) {
			t.Fatalf("partial Server-Timing = %q, unexpectedly contains %q", timing, omitted)
		}
	}

	fullReq := httptest.NewRequest(http.MethodGet, "/api/discover?randomMode=any&randomLimit=4&historyLimit=4&lean=1", nil)
	fullRec := httptest.NewRecorder()
	s.Routes().ServeHTTP(fullRec, fullReq)
	if fullRec.Code != http.StatusOK {
		t.Fatalf("full discover returned %d: %s", fullRec.Code, fullRec.Body.String())
	}
	fullTiming := fullRec.Header().Get("Server-Timing")
	for _, phase := range []string{"discoverHistory;dur=", "discoverStats;dur="} {
		if !strings.Contains(fullTiming, phase) {
			t.Fatalf("full Server-Timing = %q, missing %q", fullTiming, phase)
		}
	}
}

func TestDiscoverQueryBranchEnabledDefaultsToCompatibility(t *testing.T) {
	for _, value := range []string{"0", "false", "FALSE", "no", "off", " Off "} {
		if discoverQueryBranchEnabled(value) {
			t.Fatalf("branch value %q should disable the query", value)
		}
	}
	for _, value := range []string{"", "1", "true", "yes", "unexpected"} {
		if !discoverQueryBranchEnabled(value) {
			t.Fatalf("branch value %q should preserve the default query", value)
		}
	}
}

func TestDiscoverFastRandomDetailVisibilityRechecksMutableState(t *testing.T) {
	base := map[string]any{
		"library_key":           "doujin-lanraragi",
		"page_count_status":     "counted",
		"readable_page_count":   20,
		"user_read_status":      "unread",
		"user_personal_rating":  nil,
		"user_favorite":         0,
		"user_reread_priority":  0,
		"progress_updated_at":   "",
		"progress_last_read_at": "",
		"progress_completed":    0,
	}
	clone := func() map[string]any {
		row := make(map[string]any, len(base))
		for key, value := range base {
			row[key] = value
		}
		return row
	}
	if !discoverFastRandomDetailVisible(clone(), "unread", "doujin-lanraragi") {
		t.Fatal("fresh unread work should remain visible")
	}
	for name, mutate := range map[string]func(map[string]any){
		"library":     func(row map[string]any) { row["library_key"] = "commercial-manga" },
		"page status": func(row map[string]any) { row["page_count_status"] = "failed" },
		"progress":    func(row map[string]any) { row["progress_updated_at"] = "2026-07-13T00:00:00Z" },
	} {
		t.Run(name, func(t *testing.T) {
			row := clone()
			mutate(row)
			if discoverFastRandomDetailVisible(row, "unread", "doujin-lanraragi") {
				t.Fatalf("mutated row leaked through unread visibility: %#v", row)
			}
		})
	}

	liked := clone()
	liked["user_personal_rating"] = 8
	if !discoverFastRandomDetailVisible(liked, "liked", "") || !discoverFastRandomDetailVisible(liked, "strong-liked", "") || !discoverFastRandomDetailVisible(liked, "rated", "") {
		t.Fatalf("rated work lost its liked modes: %#v", liked)
	}
	reread := clone()
	reread["user_reread_priority"] = 1
	if !discoverFastRandomDetailVisible(reread, "reread", "") {
		t.Fatalf("reread work lost its mode: %#v", reread)
	}
}

func TestDiscoverTargetedDetailsStayOnProvenSparseModes(t *testing.T) {
	for _, mode := range []string{"liked", "strong-liked", "rated", "reread"} {
		if !discoverUsesTargetedListDetails(mode, "") {
			t.Fatalf("mode %q should use targeted list details", mode)
		}
		if discoverUsesTargetedListDetails(mode, "needle") {
			t.Fatalf("searched mode %q must preserve the wide search path", mode)
		}
	}
	for _, mode := range []string{"unread", "any", "reading", "completed", "abandoned"} {
		if discoverUsesTargetedListDetails(mode, "") {
			t.Fatalf("mode %q has not proven a targeted-detail win", mode)
		}
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
