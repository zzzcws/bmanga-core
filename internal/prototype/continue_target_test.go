package prototype

import (
	"path/filepath"
	"testing"
)

type continueTargetWorkSeed struct {
	candidateID  string
	groupID      string
	title        string
	seriesTitle  string
	sequence     string
	sortKey      string
	relativePath string
	itemRole     string
	lastReadAt   string
	completed    bool
	readable     bool
}

func TestContinueTargetUsesMostRecentMeaningfulSeriesProgressAndNextItem(t *testing.T) {
	s := newContinueTargetTestServer(t)
	defer s.Close()

	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "chapter-1", groupID: "series-forward", title: "Forward Chapter 1",
		seriesTitle: "Forward Series", sequence: "1", lastReadAt: "2026-07-11T12:00:00Z", readable: true,
	})
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "chapter-4", groupID: "series-forward", title: "Forward Chapter 4",
		seriesTitle: "Forward Series", sequence: "4", lastReadAt: "2026-07-10T12:00:00Z", readable: true,
	})
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "chapter-5", groupID: "series-forward", title: "Forward Chapter 5",
		seriesTitle: "Forward Series", sequence: "5", readable: true,
	})

	target := continueTargetPayload(t, s)
	item := continueTargetMap(t, target, "item")
	if item["candidate_id"] != "chapter-1" {
		t.Fatalf("continue item = %#v, want recently read chapter-1", item)
	}
	progress := continueTargetMap(t, target, "progress")
	if progress["candidate_id"] != "chapter-1" || boolValue(progress["completed"]) {
		t.Fatalf("continue progress = %#v, want incomplete chapter-1 progress", progress)
	}
	series := continueTargetMap(t, target, "series")
	if series["group_id"] != "series-forward" || series["series_title"] != "Forward Series" {
		t.Fatalf("continue series = %#v", series)
	}
	nextItem := continueTargetMap(t, target, "next_item")
	if nextItem["candidate_id"] != "chapter-4" {
		t.Fatalf("next item = %#v, want chapter-4", nextItem)
	}
}

func TestContinueTargetDoesNotLetNewerPageOneOpenOverrideMeaningfulProgress(t *testing.T) {
	s := newContinueTargetTestServer(t)
	defer s.Close()

	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "chapter-1", groupID: "series-page-one", title: "Page One Chapter 1",
		seriesTitle: "Page One Series", sequence: "1", lastReadAt: "2026-07-11T12:00:00Z", readable: true,
	})
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "chapter-4", groupID: "series-page-one", title: "Page One Chapter 4",
		seriesTitle: "Page One Series", sequence: "4", lastReadAt: "2026-07-12T12:00:00Z", readable: true,
	})
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "chapter-5", groupID: "series-page-one", title: "Page One Chapter 5",
		seriesTitle: "Page One Series", sequence: "5", readable: true,
	})
	if _, err := s.db.Exec(`
		UPDATE reading_progress
		SET last_page_index = 0, progress_percent = 8.33
		WHERE reader_profile_key = 'default'
		  AND candidate_id = 'chapter-4'
	`); err != nil {
		t.Fatal(err)
	}

	target := continueTargetPayload(t, s)
	if item := continueTargetMap(t, target, "item"); item["candidate_id"] != "chapter-1" {
		t.Fatalf("continue item = %#v, want meaningful chapter-1", item)
	}
	if next := continueTargetMap(t, target, "next_item"); next["candidate_id"] != "chapter-4" {
		t.Fatalf("next item = %#v, want chapter-4", next)
	}
}

func TestContinueTargetGlobalSelectionPrefersMeaningfulReadingOverNewerPageOneOpen(t *testing.T) {
	s := newContinueTargetTestServer(t)
	defer s.Close()

	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "meaningful-chapter", groupID: "series-meaningful", title: "Meaningful Chapter",
		seriesTitle: "Meaningful Series", sequence: "1", lastReadAt: "2026-07-11T12:00:00Z", readable: true,
	})
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "opened-chapter", groupID: "series-opened", title: "Opened Chapter",
		seriesTitle: "Opened Series", sequence: "1", lastReadAt: "2026-07-12T12:00:00Z", readable: true,
	})
	if _, err := s.db.Exec(`
		UPDATE reading_progress
		SET last_page_index = 0, progress_percent = 8.33
		WHERE reader_profile_key = 'default'
		  AND candidate_id = 'opened-chapter'
	`); err != nil {
		t.Fatal(err)
	}

	target := continueTargetPayload(t, s)
	if item := continueTargetMap(t, target, "item"); item["candidate_id"] != "meaningful-chapter" {
		t.Fatalf("global continue item = %#v, want meaningful-chapter", item)
	}
}

func TestContinueTargetAdvancesCompletedAndSkipsHiddenOrUnreadableItems(t *testing.T) {
	s := newContinueTargetTestServer(t)
	defer s.Close()

	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "chapter-4", groupID: "series-skip", title: "Skip Chapter 4",
		seriesTitle: "Skip Series", sequence: "4", lastReadAt: "2026-07-11T12:00:00Z", completed: true, readable: true,
	})
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "chapter-5-hidden", groupID: "series-skip", title: "Skip Chapter 5",
		seriesTitle: "Skip Series", sequence: "5", readable: true,
	})
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "chapter-8-unreadable", groupID: "series-skip", title: "Skip Chapter 8",
		seriesTitle: "Skip Series", sequence: "8", readable: false,
	})
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "chapter-9", groupID: "series-skip", title: "Skip Chapter 9",
		seriesTitle: "Skip Series", sequence: "9", readable: true,
	})
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "chapter-10", groupID: "series-skip", title: "Skip Chapter 10",
		seriesTitle: "Skip Series", sequence: "10", readable: true,
	})
	setContinueTargetWorkHidden(t, s, "chapter-5-hidden")

	target := continueTargetPayload(t, s)
	item := continueTargetMap(t, target, "item")
	if item["candidate_id"] != "chapter-9" {
		t.Fatalf("continue item = %#v, want first eligible readable chapter-9", item)
	}
	if target["progress"] != nil {
		t.Fatalf("advanced unread target progress = %#v, want nil", target["progress"])
	}
	nextItem := continueTargetMap(t, target, "next_item")
	if nextItem["candidate_id"] != "chapter-10" {
		t.Fatalf("next item = %#v, want chapter-10", nextItem)
	}
}

func TestContinueTargetSkipsHiddenStandaloneHistory(t *testing.T) {
	s := newContinueTargetTestServer(t)
	defer s.Close()

	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "standalone-hidden", title: "Hidden Standalone",
		lastReadAt: "2026-07-11T12:00:00Z", readable: true,
	})
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "standalone-visible", title: "Visible Standalone",
		lastReadAt: "2026-07-10T12:00:00Z", readable: true,
	})
	setContinueTargetWorkHidden(t, s, "standalone-hidden")

	target := continueTargetPayload(t, s)
	item := continueTargetMap(t, target, "item")
	if item["candidate_id"] != "standalone-visible" {
		t.Fatalf("continue item = %#v, want older visible standalone", item)
	}
	if target["series"] != nil || target["next_item"] != nil {
		t.Fatalf("standalone context = series %#v next %#v, want null/null", target["series"], target["next_item"])
	}
}

func TestContinueTargetRespectsManualStandaloneReadState(t *testing.T) {
	s := newContinueTargetTestServer(t)
	defer s.Close()

	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "standalone-completed", title: "Completed Standalone",
		lastReadAt: "2026-07-11T12:00:00Z", readable: true,
	})
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "standalone-abandoned", title: "Abandoned Standalone",
		lastReadAt: "2026-07-10T12:00:00Z", readable: true,
	})
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "standalone-reading", title: "Reading Standalone",
		lastReadAt: "2026-07-09T12:00:00Z", readable: true,
	})
	setContinueTargetWorkReadStatus(t, s, "standalone-completed", "completed")
	setContinueTargetWorkReadStatus(t, s, "standalone-abandoned", "abandoned")

	target := continueTargetPayload(t, s)
	item := continueTargetMap(t, target, "item")
	if item["candidate_id"] != "standalone-reading" {
		t.Fatalf("continue item = %#v, want older effective-reading standalone", item)
	}
}

func TestContinueTargetManualSeriesStateAdvancesToNextChapter(t *testing.T) {
	for _, status := range []string{"completed", "abandoned"} {
		t.Run(status, func(t *testing.T) {
			s := newContinueTargetTestServer(t)
			defer s.Close()

			seedContinueTargetWork(t, s, continueTargetWorkSeed{
				candidateID: "manual-1-" + status, groupID: "series-manual-" + status,
				title: "Manual Chapter 1", seriesTitle: "Manual Series", sequence: "1",
				lastReadAt: "2026-07-11T12:00:00Z", readable: true,
			})
			seedContinueTargetWork(t, s, continueTargetWorkSeed{
				candidateID: "manual-2-" + status, groupID: "series-manual-" + status,
				title: "Manual Chapter 2", seriesTitle: "Manual Series", sequence: "2", readable: true,
			})
			seedContinueTargetWork(t, s, continueTargetWorkSeed{
				candidateID: "manual-3-" + status, groupID: "series-manual-" + status,
				title: "Manual Chapter 3", seriesTitle: "Manual Series", sequence: "3", readable: true,
			})
			setContinueTargetWorkReadStatus(t, s, "manual-1-"+status, status)

			target := continueTargetPayload(t, s)
			if item := continueTargetMap(t, target, "item"); item["candidate_id"] != "manual-2-"+status {
				t.Fatalf("continue item = %#v, want chapter 2 after manual %s", item, status)
			}
			if next := continueTargetMap(t, target, "next_item"); next["candidate_id"] != "manual-3-"+status {
				t.Fatalf("next item = %#v, want chapter 3 after manual %s", next, status)
			}
		})
	}
}

func TestContinueTargetUsesSeriesDetailSectionOrderForExtras(t *testing.T) {
	s := newContinueTargetTestServer(t)
	defer s.Close()
	const groupID = "series-sections"
	seedContinueTargetSeriesGroup(t, s, groupID, "Section Series", "Section Series")
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "section-main-1", groupID: groupID, title: "Section Series 第1话",
		seriesTitle: "Section Series", sequence: "1", sortKey: "001", relativePath: `Section Series\本篇\01.zip`, readable: true,
	})
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "section-main-2", groupID: groupID, title: "Section Series 第2话",
		seriesTitle: "Section Series", sequence: "2", sortKey: "002", relativePath: `Section Series\本篇\02.zip`,
		lastReadAt: "2026-07-11T12:00:00Z", completed: true, readable: true,
	})
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "section-extra", groupID: groupID, title: "Section Series 番外",
		seriesTitle: "Section Series", sequence: "", sortKey: "900", relativePath: `Section Series\番外\extra.zip`, itemRole: "special", readable: true,
	})

	target := continueTargetPayload(t, s)
	if item := continueTargetMap(t, target, "item"); item["candidate_id"] != "section-extra" {
		t.Fatalf("continue item = %#v, want the extra after the completed main section", item)
	}
}

func TestContinueTargetDoesNotTreatAlternateEditionAsNextChapter(t *testing.T) {
	s := newContinueTargetTestServer(t)
	defer s.Close()
	const groupID = "series-alternates"
	seedContinueTargetSeriesGroup(t, s, groupID, "Alternate Series", "Alternate Series")
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "alternate-primary", groupID: groupID, title: "Alternate Series 第1话 [A]",
		seriesTitle: "Alternate Series", sequence: "1", sortKey: "001", relativePath: `Alternate Series\第1话 A.zip`, readable: true,
	})
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "alternate-progress", groupID: groupID, title: "Alternate Series 第1话 [B]",
		seriesTitle: "Alternate Series", sequence: "1", sortKey: "002", relativePath: `Alternate Series\第1话 B.zip`,
		lastReadAt: "2026-07-11T12:00:00Z", readable: true,
	})
	seedContinueTargetWork(t, s, continueTargetWorkSeed{
		candidateID: "alternate-chapter-2", groupID: groupID, title: "Alternate Series 第2话",
		seriesTitle: "Alternate Series", sequence: "2", sortKey: "003", relativePath: `Alternate Series\第2话.zip`, readable: true,
	})

	target := continueTargetPayload(t, s)
	if item := continueTargetMap(t, target, "item"); item["candidate_id"] != "alternate-progress" {
		t.Fatalf("continue item = %#v, want the progressed edition", item)
	}
	if next := continueTargetMap(t, target, "next_item"); next["candidate_id"] != "alternate-chapter-2" {
		t.Fatalf("next item = %#v, want chapter 2 rather than another edition", next)
	}
}

func TestContinueTargetNoHistoryReturnsNull(t *testing.T) {
	s := newContinueTargetTestServer(t)
	defer s.Close()

	payload := getJSON(t, s, "/api/continue-target")
	if payload["target"] != nil {
		t.Fatalf("target = %#v, want nil", payload["target"])
	}
}

func TestContinueTargetLatestProgressEntryComparesAbsoluteTimes(t *testing.T) {
	entries := []continueTargetSeriesEntry{
		{item: map[string]any{"progress": map[string]any{"last_read_at": "2026-07-11T12:30:00+08:00"}}},
		{item: map[string]any{"progress": map[string]any{"last_read_at": "2026-07-11T05:00:00Z"}}},
	}
	if got := continueTargetLatestProgressEntry(entries); got != 1 {
		t.Fatalf("latest progress entry = %d, want UTC-later alternate at 1", got)
	}
}

func newContinueTargetTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := newServerWithoutCatalogForTest(filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureCatalogTables(s.db); err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	return s
}

func seedContinueTargetWork(t *testing.T, s *Server, seed continueTargetWorkSeed) {
	t.Helper()
	if seed.title == "" {
		seed.title = seed.candidateID
	}
	if seed.sortKey == "" {
		seed.sortKey = seed.sequence
	}
	if seed.relativePath == "" {
		seed.relativePath = seed.candidateID + ".zip"
	}
	if seed.itemRole == "" {
		seed.itemRole = "chapter"
	}
	now := "2026-07-01T00:00:00Z"
	if _, err := s.db.Exec(`
		INSERT INTO work_candidates (
			candidate_id, library_key, library_name, candidate_type, source_kind, title, root, path, relative_path,
			parent_relative_path, source_record_id, source_status, source_reason, size_bytes, modified_utc, extension,
			page_file_count, confidence, notes
		) VALUES (?, 'commercial-manga', '漫画', 'manga', 'archive', ?, '', '', ?, '', '', 'indexed', '', '1', ?, '.zip', '1', '1.0', '')
	`, seed.candidateID, seed.title, seed.relativePath, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`
		INSERT INTO work_identities (
			work_identity_id, library_key, current_candidate_id, identity_type, display_title,
			canonical_relative_path, match_status, identity_version, first_seen_at, last_seen_at, updated_at
		) VALUES (?, 'commercial-manga', ?, 'work', ?, ?, 'matched', 'test', ?, ?, ?)
	`, "identity-"+seed.candidateID, seed.candidateID, seed.title, seed.relativePath, now, now, now); err != nil {
		t.Fatal(err)
	}
	pageCount := 0
	if seed.readable {
		pageCount = 12
	}
	if _, err := s.db.Exec(`
		INSERT INTO page_counts (
			candidate_id, library_key, candidate_type, source_kind, title, path, extension,
			page_count_status, readable_page_count, total_entry_count, reason, elapsed_ms
		) VALUES (?, 'commercial-manga', 'manga', 'archive', ?, ?, '.zip', 'counted', ?, ?, 'test', 1)
	`, seed.candidateID, seed.title, seed.relativePath, pageCount, pageCount); err != nil {
		t.Fatal(err)
	}
	if seed.groupID != "" {
		if _, err := s.db.Exec(`
			INSERT INTO series_items (
				group_id, library_key, series_title, candidate_id, candidate_type, source_kind, title,
				item_role, sequence_number, sort_key, relative_path, parent_relative_path, page_file_count, confidence
			) VALUES (?, 'commercial-manga', ?, ?, 'manga', 'archive', ?, ?, ?, ?, ?, ?, ?, '1.0')
		`, seed.groupID, seed.seriesTitle, seed.candidateID, seed.title, seed.itemRole, seed.sequence, seed.sortKey, seed.relativePath, seed.seriesTitle, pageCount); err != nil {
			t.Fatal(err)
		}
	}
	if seed.lastReadAt == "" {
		return
	}
	completed := 0
	lastPageIndex := 1
	progressPercent := 16.67
	if seed.completed {
		completed = 1
		lastPageIndex = 11
		progressPercent = 100
	}
	if _, err := s.db.Exec(`
		INSERT INTO reading_progress (
			reader_profile_key, work_identity_id, candidate_id, page_manifest_id,
			manifest_hash_snapshot, progress_status, last_page_index, progress_percent,
			completed, page_count_snapshot, last_read_at, created_at, updated_at
		) VALUES ('default', ?, ?, '', '', 'normal', ?, ?, ?, 12, ?, ?, ?)
	`, "identity-"+seed.candidateID, seed.candidateID, lastPageIndex, progressPercent, completed, seed.lastReadAt, seed.lastReadAt, seed.lastReadAt); err != nil {
		t.Fatal(err)
	}
}

func setContinueTargetWorkHidden(t *testing.T, s *Server, candidateID string) {
	t.Helper()
	now := "2026-07-11T12:00:00Z"
	if _, err := s.db.Exec(`
		INSERT INTO work_user_marks (
			reader_profile_key, work_identity_id, candidate_id, read_status,
			favorite, reread_priority, hidden, hidden_reason, notes,
			marked_at, created_at, updated_at
		) VALUES ('default', ?, ?, 'unread', 0, 0, 1, 'test hidden', '', ?, ?, ?)
	`, "identity-"+candidateID, candidateID, now, now, now); err != nil {
		t.Fatal(err)
	}
}

func setContinueTargetWorkReadStatus(t *testing.T, s *Server, candidateID, readStatus string) {
	t.Helper()
	now := "2026-07-11T12:00:00Z"
	if _, err := s.db.Exec(`
		INSERT INTO work_user_marks (
			reader_profile_key, work_identity_id, candidate_id, read_status,
			favorite, reread_priority, hidden, hidden_reason, notes,
			marked_at, created_at, updated_at
		) VALUES ('default', ?, ?, ?, 0, 0, 0, '', '', ?, ?, ?)
		ON CONFLICT(reader_profile_key, work_identity_id) DO UPDATE SET
			candidate_id = excluded.candidate_id,
			read_status = excluded.read_status,
			updated_at = excluded.updated_at
	`, "identity-"+candidateID, candidateID, readStatus, now, now, now); err != nil {
		t.Fatal(err)
	}
}

func seedContinueTargetSeriesGroup(t *testing.T, s *Server, groupID, seriesTitle, groupPath string) {
	t.Helper()
	if _, err := s.db.Exec(`
		INSERT INTO series_groups (
			group_id, library_key, series_title, group_path, group_type, candidate_count, confidence, notes
		) VALUES (?, 'commercial-manga', ?, ?, 'series_candidate', 3, '1.0', '')
	`, groupID, seriesTitle, groupPath); err != nil {
		t.Fatal(err)
	}
}

func continueTargetPayload(t *testing.T, s *Server) map[string]any {
	t.Helper()
	payload := getJSON(t, s, "/api/continue-target")
	target, ok := payload["target"].(map[string]any)
	if !ok {
		t.Fatalf("target = %#v, want object", payload["target"])
	}
	return target
}

func continueTargetMap(t *testing.T, source map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := source[key].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", key, source[key])
	}
	return value
}
