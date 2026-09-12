package prototype

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Everything is synthetic and database-only: no source archives or HTTP sources
// are created. The selected series remains 20 chapters as the library grows.
func newSeriesDetailPerformanceServer(tb testing.TB, groups, itemsPerGroup int) *Server {
	tb.Helper()
	dataDir := filepath.Join(tb.TempDir(), "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		tb.Fatal(err)
	}
	s, err := newServerWithoutCatalogForTest(filepath.Join(dataDir, "synthetic.sqlite"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = s.Close() })
	if err := EnsureCatalogTables(s.db); err != nil {
		tb.Fatal(err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		tb.Fatal(err)
	}
	defer tx.Rollback()
	exec := func(stmt string, args ...any) {
		tb.Helper()
		if _, err := tx.Exec(stmt, args...); err != nil {
			tb.Fatal(err)
		}
	}
	exec(`WITH RECURSIVE nums(n) AS (VALUES(0) UNION ALL SELECT n+1 FROM nums WHERE n+1 < ?)
		INSERT INTO series_groups (group_id,library_key,series_title,group_path,group_type,candidate_count,confidence,notes)
		SELECT printf('series-%04d',n),'commercial-manga',printf('Synthetic %04d',n),printf('Series%04d',n),'series_candidate',?,'1.0','' FROM nums`, groups, itemsPerGroup)
	exec(`WITH RECURSIVE nums(n) AS (VALUES(0) UNION ALL SELECT n+1 FROM nums WHERE n+1 < ?)
		INSERT INTO series_items (group_id,library_key,series_title,candidate_id,candidate_type,source_kind,title,item_role,
		 sequence_number,sort_key,relative_path,parent_relative_path,page_file_count,confidence)
		SELECT printf('series-%04d',n / ?),'commercial-manga',printf('Synthetic %04d',n / ?),printf('item-%06d',n),
		 'manga','archive',printf('第%d话',n % ?),'chapter',n % ?,printf('%06d',n % ?),
		 printf('Series%04d',n / ?) || char(92) || '本篇' || char(92) || printf('%03d.cbz',n % ?),
		 printf('Series%04d',n / ?) || char(92) || '本篇','12','1.0' FROM nums`,
		groups*itemsPerGroup, itemsPerGroup, itemsPerGroup, itemsPerGroup, itemsPerGroup, itemsPerGroup, itemsPerGroup, itemsPerGroup, itemsPerGroup)
	exec(`INSERT INTO work_candidates (candidate_id,library_key,library_name,candidate_type,source_kind,title,root,path,relative_path,
		 parent_relative_path,source_record_id,source_status,source_reason,size_bytes,modified_utc,extension,page_file_count,confidence,notes)
		SELECT candidate_id,library_key,'漫画',candidate_type,source_kind,title,'',relative_path,relative_path,
		 parent_relative_path,'','indexed','','1024','2026-09-01T00:00:00Z','.cbz','12','1.0','' FROM series_items`)
	exec(`INSERT INTO work_identities (work_identity_id,library_key,current_candidate_id,identity_type,display_title,
		 canonical_relative_path,match_status,identity_version,first_seen_at,last_seen_at,updated_at)
		SELECT 'identity-' || candidate_id,library_key,candidate_id,'work',title,relative_path,'matched','synthetic',
		 '2026-09-01T00:00:00Z','2026-09-01T00:00:00Z','2026-09-01T00:00:00Z' FROM work_candidates`)
	exec(`INSERT INTO series_identities (series_identity_id,library_key,current_group_id,identity_type,display_title,
		 canonical_group_path,match_status,identity_version,first_seen_at,last_seen_at,updated_at)
		SELECT 'identity-' || group_id,library_key,group_id,'series',series_title,group_path,'matched','synthetic',
		 '2026-09-01T00:00:00Z','2026-09-01T00:00:00Z','2026-09-01T00:00:00Z' FROM series_groups`)
	exec(`INSERT INTO page_counts (candidate_id,page_count_status,readable_page_count,total_entry_count)
		SELECT candidate_id,'counted','12','12' FROM work_candidates`)
	exec(`INSERT INTO work_cover_candidates (candidate_id,cover_status,cover_kind,cover_source_path,cover_source_relative_path,requires_extraction)
		SELECT candidate_id,'ready','archive',relative_path,relative_path,'1' FROM work_candidates`)
	exec(`INSERT INTO series_cover_candidates (group_id,selected_candidate_id,cover_status,cover_kind,cover_source_path,requires_extraction)
		SELECT group_id,MIN(candidate_id),'ready','archive',MIN(relative_path),'1' FROM series_items GROUP BY group_id`)
	if err := tx.Commit(); err != nil {
		tb.Fatal(err)
	}
	return s
}

// Preserve the public baseline query for a byte-for-byte row parity check and
// a reproducible comparison; no production catalog or archives are involved.
func legacySeriesDetailMainSQLForPerformanceTest() string {
	return fmt.Sprintf(`
		SELECT
			sg.group_id,
			sg.library_key,
			sg.series_title,
			sg.group_path,
			sg.group_type,
			sg.candidate_count,
			COALESCE(stats.unique_sequence_count, sg.candidate_count) AS unique_sequence_count,
			COALESCE(stats.item_count, sg.candidate_count) AS item_count,
			COALESCE(section_stats.section_count, 1) AS section_count,
			COALESCE(section_stats.multi_section_count, 0) AS multi_section_count,
			COALESCE(section_stats.special_section_count, 0) AS special_section_count,
			sg.confidence,
			COALESCE(cover_override.candidate_id, safe_cover.selected_candidate_id, scc.selected_candidate_id) AS selected_candidate_id,
			cover_choice.correction_value AS manual_cover_candidate_id,
			kind_choice.correction_value AS series_kind,
			unit_choice.correction_value AS series_unit,
			COALESCE(cover_override.cover_status, safe_cover.cover_status, scc.cover_status) AS cover_status,
			COALESCE(cover_override.cover_kind, safe_cover.cover_kind, scc.cover_kind) AS cover_kind,
			COALESCE(cover_override.cover_source_path, safe_cover.cover_source_path, scc.cover_source_path) AS cover_source_path,
			COALESCE(cover_override.requires_extraction, safe_cover.requires_extraction, scc.requires_extraction) AS requires_extraction
		FROM series_groups sg
		LEFT JOIN series_cover_candidates scc
			ON scc.group_id = sg.group_id
		   AND %s
		%s
		%s
		%s
		LEFT JOIN (
			SELECT
				si.group_id,
				COUNT(*) AS item_count,
				COUNT(DISTINCT CASE
					WHEN si.sequence_number IS NOT NULL AND si.sequence_number <> '' THEN si.sequence_number
					ELSE si.candidate_id
				END) AS unique_sequence_count
			FROM series_items si
			JOIN work_browse stats_wb ON stats_wb.candidate_id = si.candidate_id
			GROUP BY si.group_id
		) stats ON stats.group_id = sg.group_id
		%s
		WHERE sg.group_id = ?
		  AND %s
	`, visibleWorkCandidateExistsSQL("scc.selected_candidate_id"), safeSeriesCoverJoinSQL(), seriesCoverOverrideJoinSQL(), seriesKindJoinSQL(), seriesSectionStatsJoinSQL(), seriesHasVisibleMemberSQL("sg"))
}

func TestSeriesDetailSelectedQueryParityAndOldIndexes(t *testing.T) {
	s := newSeriesDetailPerformanceServer(t, 24, 20)
	if _, err := s.db.Exec(`
 UPDATE series_items SET sequence_number='1.5' WHERE candidate_id='item-000002';
 UPDATE series_items SET sequence_number='0' WHERE candidate_id='item-000001';
 UPDATE series_items SET relative_path='Series0000/番外/synthetic.cbz' WHERE candidate_id IN ('item-000018','item-000019');
 UPDATE work_cover_candidates SET cover_source_path='credits.txt',cover_source_relative_path='credits.txt' WHERE candidate_id='item-000000';
 `); err != nil {
		t.Fatal(err)
	}
	for _, legacyIndexes := range []bool{false, true} {
		if legacyIndexes {
			if _, err := s.db.Exec("DROP INDEX IF EXISTS idx_series_items_group_sequence_covering; DROP INDEX IF EXISTS idx_series_items_candidate_covering"); err != nil {
				t.Fatal(err)
			}
		}
		for _, id := range []string{"series-0000", "series-0019", "missing"} {
			before, err := s.query(legacySeriesDetailMainSQLForPerformanceTest(), id)
			if err != nil {
				t.Fatal(err)
			}
			after, err := s.query(seriesDetailMainSQL(), id)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("index compatibility=%v id=%s baseline=%v selected=%v err=%v", legacyIndexes, id, before, after, err)
			}
		}
	}
}

func TestSeriesDetailSelectedQueryPlan(t *testing.T) {
	s := newSeriesDetailPerformanceServer(t, 100, 20)
	rows, err := s.query("EXPLAIN QUERY PLAN "+seriesDetailMainSQL(), "series-0000")
	if err != nil {
		t.Fatal(err)
	}
	bounded := 0
	for _, row := range rows {
		detail := stringValue(row["detail"])
		if strings.Contains(detail, "SEARCH si USING") && strings.Contains(detail, "(group_id=?)") {
			bounded++
		}
		if strings.Contains(detail, "SCAN si ") {
			t.Errorf("unbounded aggregate input: %s", detail)
		}
	}
	if bounded < 3 {
		t.Fatalf("need bounded stats, covers and sections; got %d: %v", bounded, rows)
	}
}

func TestSeriesDetailSelectedHelpersPreserveMultiGroupResults(t *testing.T) {
	s := newSeriesDetailPerformanceServer(t, 10, 20)
	stmt := `WITH selected_groups(group_id) AS (VALUES (?),(?),(?))
 SELECT sg.group_id,safe_cover.selected_candidate_id,section_stats.section_count,
 section_stats.multi_section_count,section_stats.special_section_count
 FROM series_groups sg ` + safeSeriesCoverJoinSQLForSelected(true) + seriesSectionStatsJoinSQLForSelected(true) + `
 WHERE sg.group_id IN (SELECT group_id FROM selected_groups) ORDER BY sg.group_id`
	before := strings.ReplaceAll(stmt, "AND si.group_id IN (SELECT group_id FROM selected_groups)", "")
	before = strings.ReplaceAll(before, "WHERE si.group_id IN (SELECT group_id FROM selected_groups)", "")
	for _, ids := range [][]any{{"series-0001", "series-0007", "missing"}, {"series-0001", "series-0001", "series-0007"}} {
		oldRows, err := s.query(before, ids...)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := s.query(stmt, ids...)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 || !reflect.DeepEqual(oldRows, rows) {
			t.Fatalf("multi-group results changed: %v / %v", oldRows, rows)
		}
	}
}

func TestSeriesDetailSelectedReadOnlyAndCancellation(t *testing.T) {
	s := newSeriesDetailPerformanceServer(t, 4, 20)
	var before int64
	if err := s.db.QueryRow("SELECT total_changes()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/series-detail?id=series-0000", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response["items"].([]any)) != 20 {
		t.Fatal("selected directory lost chapters")
	}
	for _, phase := range []string{"seriesMain", "seriesItems", "seriesCovers", "seriesMark", "seriesMetadata", "seriesAssemble", "app"} {
		if !strings.Contains(rec.Header().Get("Server-Timing"), phase+";dur=") {
			t.Errorf("missing %s timing", phase)
		}
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("mutable detail must not be cached")
	}
	var after int64
	if err := s.db.QueryRow("SELECT total_changes()").Scan(&after); err != nil || before != after {
		t.Fatalf("detail wrote data: %d -> %d (%v)", before, after, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec = httptest.NewRecorder()
	s.handleSeriesDetail(rec, httptest.NewRequest(http.MethodGet, "/api/series-detail?id=series-0000", nil).WithContext(ctx))
	if rec.Code == http.StatusOK {
		t.Fatal("cancelled detail query succeeded")
	}
}

func BenchmarkSeriesDetailSelectedGroup(b *testing.B) {
	for _, groups := range []int{1, 1000} {
		b.Run(fmt.Sprintf("groups=%d/items=20", groups), func(b *testing.B) {
			s := newSeriesDetailPerformanceServer(b, groups, 20)
			for _, query := range []struct{ name, sql string }{{"baseline", legacySeriesDetailMainSQLForPerformanceTest()}, {"selected", seriesDetailMainSQL()}} {
				b.Run(query.name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						rows, err := s.query(query.sql, "series-0000")
						if err != nil || len(rows) != 1 {
							b.Fatalf("rows=%d err=%v", len(rows), err)
						}
					}
				})
			}
		})
	}
}
