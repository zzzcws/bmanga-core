package prototype

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func seedWorksAddedWindowWork(t *testing.T, s *Server, candidateID string, title string, firstSeen string, sourceStatus string, sourceCreated string) {
	t.Helper()
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: candidateID,
		title:       title,
		readable:    true,
	})
	if _, err := s.db.Exec(`
		UPDATE work_identities SET first_seen_at = ? WHERE current_candidate_id = ?
	`, firstSeen, candidateID); err != nil {
		t.Fatal(err)
	}
	if sourceStatus == "" {
		return
	}
	if _, err := s.db.Exec(`
		INSERT INTO source_filesystem_times (
			target_type, target_id, source_created_utc, status, observed_at
		) VALUES ('work', ?, ?, ?, '2026-07-18T00:00:00Z')
	`, candidateID, sourceCreated, sourceStatus); err != nil {
		t.Fatal(err)
	}
}

func legacyWorksAddedDescIDsForTest(t *testing.T, s *Server, filters []string, args []any, limit int, offset int) []string {
	t.Helper()
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit, offset)
	rows, err := s.query(`
		SELECT wc.candidate_id
		FROM work_candidates wc
		LEFT JOIN page_counts pc ON pc.candidate_id = wc.candidate_id
		LEFT JOIN work_identities wi ON wi.current_candidate_id = wc.candidate_id
		LEFT JOIN metadata_field_overrides mfo_title
			ON mfo_title.work_identity_id = wi.work_identity_id
		   AND mfo_title.field_name = 'title'
		   AND mfo_title.override_status = 'active'
		LEFT JOIN (
			SELECT candidate_id, MAX(series_title) AS series_title
			FROM series_items
			GROUP BY candidate_id
		) si ON si.candidate_id = wc.candidate_id
		`+whereClause(filters)+`
		`+fastWorkOrderSQL("added_desc")+`
		LIMIT ? OFFSET ?
	`, queryArgs...)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, stringValue(row["candidate_id"]))
	}
	return ids
}

func TestWorksAddedDescWindowMatchesLegacyAcrossTiesOverridesSeriesAndExceptions(t *testing.T) {
	s := newContinueTargetTestServer(t)
	defer s.Close()

	seedWorksAddedWindowWork(t, s, "tie-series", "Zulu", "2026-06-01T00:00:00Z", "ok", "2026-07-10T00:00:00Z")
	seedWorksAddedWindowWork(t, s, "tie-override", "Zulu", "2026-06-01T00:00:00Z", "ok", "2026-07-10T00:00:00Z")
	seedWorksAddedWindowWork(t, s, "tie-plain", "Middle", "2026-06-01T00:00:00Z", "ok", "2026-07-10T00:00:00Z")
	seedWorksAddedWindowWork(t, s, "missing-between", "Between", "2026-07-09T00:00:00Z", "", "")
	seedWorksAddedWindowWork(t, s, "valid-old", "Old", "2026-06-01T00:00:00Z", "ok", "2026-07-01T00:00:00Z")
	seedWorksAddedWindowWork(t, s, "invalid-new-first-seen", "Invalid", "2026-08-01T00:00:00Z", "missing", "2026-08-01T00:00:00Z")
	seedWorksAddedWindowWork(t, s, "missing-undated", "Undated", "", "", "")

	if _, err := s.db.Exec(`
		INSERT INTO series_items (
			group_id, library_key, series_title, candidate_id, candidate_type, source_kind, title,
			item_role, sequence_number, sort_key, relative_path, parent_relative_path, page_file_count, confidence
		) VALUES (
			'window-series', 'commercial-manga', 'A Series', 'tie-series', 'manga', 'archive', 'Zulu',
			'chapter', '1', '1', 'tie-series.zip', 'A Series', 12, '1.0'
		);
		INSERT INTO metadata_field_overrides (
			work_identity_id, candidate_id, field_name, field_value, override_status,
			applied_at, created_at, updated_at
		) VALUES (
			'identity-tie-override', 'tie-override', 'title', 'B Override', 'active',
			'2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z'
		)
	`); err != nil {
		t.Fatal(err)
	}

	total := 7
	limits := fastWorksAddedDescLimits{initialWindow: 2, maxWindow: 16}
	for limit := 1; limit <= 3; limit++ {
		for offset := 0; offset <= total; offset++ {
			expected := legacyWorksAddedDescIDsForTest(t, s, nil, nil, limit, offset)
			actual, used, err := s.fetchAddedDescWindowedWorkIDsWithLimits(nil, nil, total, limit, offset, limits)
			if err != nil {
				t.Fatalf("limit %d offset %d: %v", limit, offset, err)
			}
			if !used {
				t.Fatalf("limit %d offset %d unexpectedly fell back", limit, offset)
			}
			if strings.Join(actual, ",") != strings.Join(expected, ",") {
				t.Fatalf("limit %d offset %d IDs = %v, want %v", limit, offset, actual, expected)
			}
		}
	}
}

func TestWorksAddedDescWindowExpandsFilteringGapsAndFallsBackAtCap(t *testing.T) {
	s := newContinueTargetTestServer(t)
	defer s.Close()

	for index := 0; index < 8; index++ {
		candidateID := fmt.Sprintf("new-online-%02d", index)
		seedWorksAddedWindowWork(t, s, candidateID, candidateID, "2026-06-01T00:00:00Z", "ok", fmt.Sprintf("2026-07-%02dT00:00:00Z", 18-index))
		if _, err := s.db.Exec(`UPDATE work_candidates SET source_kind = 'online' WHERE candidate_id = ?`, candidateID); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < 2; index++ {
		candidateID := fmt.Sprintf("old-archive-%02d", index)
		seedWorksAddedWindowWork(t, s, candidateID, candidateID, "2026-06-01T00:00:00Z", "ok", fmt.Sprintf("2026-06-%02dT00:00:00Z", 20-index))
	}
	filters := []string{"wc.source_kind = ?"}
	args := []any{"archive"}
	expected := legacyWorksAddedDescIDsForTest(t, s, filters, args, 2, 0)

	actual, used, err := s.fetchAddedDescWindowedWorkIDsWithLimits(filters, args, 2, 2, 0, fastWorksAddedDescLimits{initialWindow: 2, maxWindow: 16})
	if err != nil {
		t.Fatal(err)
	}
	if !used || strings.Join(actual, ",") != strings.Join(expected, ",") {
		t.Fatalf("expanded window = used:%v ids:%v, want %v", used, actual, expected)
	}
	if ids, used, err := s.fetchAddedDescWindowedWorkIDsWithLimits(filters, args, 2, 2, 0, fastWorksAddedDescLimits{initialWindow: 2, maxWindow: 4}); err != nil {
		t.Fatal(err)
	} else if used || len(ids) != 0 {
		t.Fatalf("capped sparse window = used:%v ids:%v, want safe fallback", used, ids)
	}
}

func TestWorksAddedDescWindowPreservesWorksFilterAliases(t *testing.T) {
	s := newContinueTargetTestServer(t)
	defer s.Close()

	for index, candidateID := range []string{"filter-alpha", "filter-beta", "filter-plain", "filter-missing"} {
		status := "ok"
		created := fmt.Sprintf("2026-07-%02dT00:00:00Z", 18-index)
		firstSeen := "2026-06-01T00:00:00Z"
		if candidateID == "filter-missing" {
			status = ""
			created = ""
			firstSeen = "2026-07-12T00:00:00Z"
		}
		seedWorksAddedWindowWork(t, s, candidateID, candidateID, firstSeen, status, created)
	}
	if _, err := s.db.Exec(`
		UPDATE work_candidates SET library_key = 'library-alpha', candidate_type = 'doujin' WHERE candidate_id = 'filter-alpha';
		UPDATE work_candidates SET library_key = 'library-beta', source_kind = 'image_folder' WHERE candidate_id = 'filter-beta';
		UPDATE page_counts SET page_count_status = 'pending' WHERE candidate_id = 'filter-beta';
	`); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		filters []string
		args    []any
	}{
		{name: "library", filters: []string{"wc.library_key = ?"}, args: []any{"library-alpha"}},
		{name: "type", filters: []string{"wc.candidate_type = ?"}, args: []any{"doujin"}},
		{name: "source", filters: []string{"wc.source_kind = ?"}, args: []any{"image_folder"}},
		{name: "page status", filters: []string{"COALESCE(pc.page_count_status, '') = ?"}, args: []any{"pending"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			expectedAll := legacyWorksAddedDescIDsForTest(t, s, test.filters, test.args, 100, 0)
			limit := 2
			if len(expectedAll) < limit {
				limit = len(expectedAll)
			}
			if limit == 0 {
				t.Fatal("fixture unexpectedly produced an empty filter scope")
			}
			expected := expectedAll[:limit]
			actual, used, err := s.fetchAddedDescWindowedWorkIDsWithLimits(test.filters, test.args, len(expectedAll), limit, 0, fastWorksAddedDescLimits{initialWindow: 2, maxWindow: 16})
			if err != nil {
				t.Fatal(err)
			}
			if !used || strings.Join(actual, ",") != strings.Join(expected, ",") {
				t.Fatalf("filtered window = used:%v ids:%v, want %v", used, actual, expected)
			}
		})
	}
}

func TestWorksAddedDescWindowEligibilityIsStrict(t *testing.T) {
	for _, test := range []struct {
		name    string
		query   map[string]string
		sortKey string
		offset  int
		want    bool
	}{
		{name: "shallow added desc", sortKey: "added_desc", offset: 512, want: true},
		{name: "main doujin type", query: map[string]string{"type": "doujin"}, sortKey: "added_desc", want: true},
		{name: "unsupported action", query: map[string]string{"action": "legacy"}, sortKey: "added_desc", want: false},
		{name: "search", query: map[string]string{"q": "needle"}, sortKey: "added_desc", offset: 0, want: false},
		{name: "ascending", sortKey: "added_asc", offset: 0, want: false},
		{name: "deep offset", sortKey: "added_desc", offset: 513, want: false},
		{name: "negative offset", sortKey: "added_desc", offset: -1, want: false},
		{name: "library", query: map[string]string{"library": "small"}, sortKey: "added_desc", want: false},
		{name: "source", query: map[string]string{"source": "online"}, sortKey: "added_desc", want: false},
		{name: "page status", query: map[string]string{"pageStatus": "pending"}, sortKey: "added_desc", want: false},
		{name: "commercial manga type", query: map[string]string{"type": "manga_file"}, sortKey: "added_desc", want: false},
		{name: "sparse type", query: map[string]string{"type": "other"}, sortKey: "added_desc", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := url.Values{}
			for key, value := range test.query {
				query.Set(key, value)
			}
			if got := canUseFastWorksAddedDescWindow(query, test.sortKey, test.offset); got != test.want {
				t.Fatalf("eligibility = %v, want %v", got, test.want)
			}
		})
	}
}

func TestWorksAddedDescWindowPlanStartsFromSourceTimeIndex(t *testing.T) {
	s := newContinueTargetTestServer(t)
	defer s.Close()

	rows, err := s.db.Query("EXPLAIN QUERY PLAN "+fastWorksAddedDescWindowSQL(nil, 0, true), "2026-07-01T00:00:00Z", 24)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	plan := strings.Builder{}
	for rows.Next() {
		var id int
		var parent int
		var notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "idx_source_filesystem_times_work_added") {
		t.Fatalf("query plan did not use source-time index:\n%s", plan.String())
	}
}
