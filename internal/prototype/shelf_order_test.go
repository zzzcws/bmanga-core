package prototype

import (
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestShelfTitleWindowUsesSameStableOrderAsFinalMerge(t *testing.T) {
	s := newContinueTargetTestServer(t)
	defer s.Close()

	seeds := []struct {
		candidateID string
		title       string
		libraryKey  string
		relative    string
	}{
		{candidateID: "same-z", title: "Same Title", libraryKey: "library-a", relative: "z.zip"},
		{candidateID: "same-a", title: "Same Title", libraryKey: "library-z", relative: "a.zip"},
		{candidateID: "ascii-upper", title: "Alpha", libraryKey: "library-b", relative: "b.zip"},
		{candidateID: "ascii-lower", title: "alpha", libraryKey: "library-a", relative: "a.zip"},
		{candidateID: "unicode-upper", title: "Ä", libraryKey: "library-a", relative: "u1.zip"},
		{candidateID: "unicode-lower", title: "ä", libraryKey: "library-a", relative: "u2.zip"},
	}
	for _, seed := range seeds {
		seedContinueTargetWork(t, s, continueTargetWorkSeed{
			candidateID:  seed.candidateID,
			title:        seed.title,
			relativePath: seed.relative,
			readable:     true,
		})
		if _, err := s.db.Exec(`
			UPDATE work_candidates SET library_key = ?, library_name = ? WHERE candidate_id = ?;
			UPDATE work_identities SET library_key = ? WHERE current_candidate_id = ?;
		`, seed.libraryKey, seed.libraryKey, seed.candidateID, seed.libraryKey, seed.candidateID); err != nil {
			t.Fatal(err)
		}
	}

	for _, sortKey := range []string{"title_asc", "title_desc"} {
		t.Run(sortKey, func(t *testing.T) {
			all, total, err := s.queryShelfWorkLiteItemsWithFastPath("", "archive", "", "", sortKey, 100, 0, false)
			if err != nil {
				t.Fatal(err)
			}
			if total != len(seeds) || len(all) != len(seeds) {
				t.Fatalf("full window total/items = %d/%d, want %d", total, len(all), len(seeds))
			}
			sortShelfLiteItems(all, sortKey)
			for offset, expected := range all {
				fast, fastTotal, err := s.queryShelfWorkLiteItemsWithFastPath("", "archive", "", "", sortKey, 1, offset, true)
				if err != nil {
					t.Fatal(err)
				}
				if fastTotal != len(seeds) || len(fast) != 1 {
					t.Fatalf("offset %d fast total/items = %d/%d", offset, fastTotal, len(fast))
				}
				if fast[0].candidateID != expected.candidateID {
					t.Fatalf("offset %d fast candidate = %q, canonical merge wants %q", offset, fast[0].candidateID, expected.candidateID)
				}
			}
		})
	}
}

func TestShelfAddedWindowMergesSmallMissingSourceTimeCacheWithoutChangingOrder(t *testing.T) {
	s := newContinueTargetTestServer(t)
	defer s.Close()

	seeds := []struct {
		candidateID string
		title       string
		firstSeen   string
		cacheStatus string
		cacheTime   string
	}{
		{candidateID: "cached-latest", title: "Latest", firstSeen: "2025-01-01T00:00:00Z", cacheStatus: "ok", cacheTime: "2026-07-10T00:00:00Z"},
		{candidateID: "uncached-between", title: "Between", firstSeen: "2026-07-08T00:00:00Z"},
		{candidateID: "cached-tie", title: "Bravo", firstSeen: "2025-01-01T00:00:00Z", cacheStatus: "ok", cacheTime: "2026-07-05T00:00:00Z"},
		{candidateID: "uncached-tie", title: "Alpha", firstSeen: "2026-07-05T00:00:00Z"},
		{candidateID: "cached-old", title: "Old Cached", firstSeen: "2025-01-01T00:00:00Z", cacheStatus: "ok", cacheTime: "2026-07-01T00:00:00Z"},
		{candidateID: "uncached-old", title: "Old Uncached", firstSeen: "2026-06-30T00:00:00Z"},
		// Present-but-invalid cache rows must remain undated even when first_seen_at is newer.
		{candidateID: "cache-error", title: "Undated Error", firstSeen: "2026-08-01T00:00:00Z", cacheStatus: "missing", cacheTime: "2026-08-01T00:00:00Z"},
		{candidateID: "cache-empty", title: "Undated Empty", firstSeen: "2026-08-02T00:00:00Z", cacheStatus: "ok", cacheTime: ""},
		{candidateID: "uncached-undated", title: "Undated Missing", firstSeen: ""},
	}
	for _, seed := range seeds {
		seedContinueTargetWork(t, s, continueTargetWorkSeed{
			candidateID:  seed.candidateID,
			title:        seed.title,
			relativePath: seed.candidateID + ".zip",
			readable:     true,
		})
		if _, err := s.db.Exec(`
			UPDATE work_identities SET first_seen_at = ? WHERE current_candidate_id = ?
		`, seed.firstSeen, seed.candidateID); err != nil {
			t.Fatal(err)
		}
		if seed.cacheStatus != "" {
			if _, err := s.db.Exec(`
				INSERT INTO source_filesystem_times (
					target_type, target_id, source_created_utc, status, observed_at
				) VALUES ('work', ?, ?, ?, '2026-07-12T00:00:00Z')
			`, seed.candidateID, seed.cacheTime, seed.cacheStatus); err != nil {
				t.Fatal(err)
			}
		}
	}
	seedContinueTargetWork(t, s, continueTargetWorkSeed{candidateID: "exceptional-other-source", title: "Other Source", readable: true})
	if _, err := s.db.Exec(`UPDATE work_candidates SET source_kind = 'online' WHERE candidate_id = 'exceptional-other-source'`); err != nil {
		t.Fatal(err)
	}
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "exceptional-series-item", groupID: "exceptional-series", title: "Series Item",
		seriesTitle: "Exceptional Series", sequence: "1", readable: true,
	})
	if s.workSourceTimeCacheComplete() {
		t.Fatal("test fixture unexpectedly has complete source-time coverage")
	}
	exceptionalIDs, bounded, err := s.fastShelfAddedExceptionalWorkIDs()
	if err != nil {
		t.Fatal(err)
	}
	if !bounded || len(exceptionalIDs) != 8 {
		t.Fatalf("global exceptional IDs = bounded:%v ids:%v, want eight bounded IDs", bounded, exceptionalIDs)
	}
	exceptionalSet := map[string]bool{}
	for _, candidateID := range exceptionalIDs {
		exceptionalSet[candidateID] = true
	}
	for _, candidateID := range []string{"exceptional-other-source", "exceptional-series-item"} {
		if !exceptionalSet[candidateID] {
			t.Fatalf("global exceptional scan omitted out-of-scope candidate %q", candidateID)
		}
	}
	baseFilters := []string{
		"(wc.candidate_type = 'doujin' OR NOT EXISTS (SELECT 1 FROM series_items si WHERE si.candidate_id = wc.candidate_id))",
		"wc.source_kind = ?",
	}
	if _, used, err := s.queryFastShelfAddedKeepItems(baseFilters, []any{"archive"}, "added_desc", 3, 0); err != nil {
		t.Fatal(err)
	} else if !used {
		t.Fatal("bounded missing-cache fixture did not use added-sort fast path")
	}

	itemKeys := func(items []shelfLiteItem) string {
		keys := make([]string, 0, len(items))
		for _, item := range items {
			keys = append(keys, item.candidateID+"|"+item.added)
		}
		return strings.Join(keys, ",")
	}
	for _, sortKey := range []string{"added_desc", "added_asc"} {
		t.Run(sortKey, func(t *testing.T) {
			legacy, total, err := s.queryShelfWorkLiteItemsWithFastPath("", "archive", "", "", sortKey, len(seeds), 0, false)
			if err != nil {
				t.Fatal(err)
			}
			if total != len(seeds) || len(legacy) != len(seeds) {
				t.Fatalf("legacy total/items = %d/%d, want %d", total, len(legacy), len(seeds))
			}
			sortShelfLiteItems(legacy, sortKey)
			for limit := 1; limit <= 4; limit++ {
				for offset := 0; offset <= len(seeds); offset++ {
					fast, fastTotal, err := s.queryShelfWorkLiteItemsWithFastPath("", "archive", "", "", sortKey, limit, offset, true)
					if err != nil {
						t.Fatal(err)
					}
					end := offset + limit
					if end > len(legacy) {
						end = len(legacy)
					}
					if got, want := itemKeys(fast), itemKeys(legacy[offset:end]); fastTotal != total || got != want {
						t.Fatalf("limit %d offset %d fast total/items = %d/%q, legacy = %d/%q", limit, offset, fastTotal, got, total, want)
					}
				}
			}
			for _, offset := range []int{0, 3, 8} {
				query := url.Values{
					"source": {"archive"},
					"sort":   {sortKey},
				}
				expected := legacyShelfPayloadForTest(t, s, query, 12, offset)
				requestQuery := cloneURLValuesForTest(query)
				requestQuery.Set("limit", "12")
				requestQuery.Set("offset", strconv.Itoa(offset))
				actual := getJSON(t, s, "/api/shelf?"+requestQuery.Encode())
				if !reflect.DeepEqual(actual, expected) {
					t.Fatalf("offset %d fast JSON differs from legacy\nactual: %#v\nlegacy: %#v", offset, actual, expected)
				}
			}
		})
	}
}

func TestShelfAddedExceptionalWorkIDsFallBackAboveBound(t *testing.T) {
	s := newContinueTargetTestServer(t)
	defer s.Close()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	statement, err := tx.Prepare(`
		INSERT INTO work_candidates (candidate_id, candidate_type, source_kind, title, relative_path)
		VALUES (?, 'doujin', 'archive', ?, ?)
	`)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= fastShelfAddedExceptionalLimit; index++ {
		candidateID := "uncached-" + strconv.Itoa(index)
		if _, err := statement.Exec(candidateID, candidateID, candidateID+".zip"); err != nil {
			_ = statement.Close()
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := statement.Close(); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	ids, bounded, err := s.fastShelfAddedExceptionalWorkIDs()
	if err != nil {
		t.Fatal(err)
	}
	if bounded || len(ids) != 0 {
		t.Fatalf("over-limit exceptional scan = bounded:%v ids:%d, want safe fallback", bounded, len(ids))
	}
}
