package prototype

import "testing"

func TestSeriesUnitCorrectionUsesChapterAcrossSummaryAndDirectory(t *testing.T) {
	series := map[string]any{
		"series_title":          "Synthetic Chapter Series",
		"display_title":         "Synthetic Chapter Series",
		"series_kind":           "normal_manga",
		"series_unit":           "chapter",
		"item_count":            46,
		"unique_sequence_count": 45,
	}
	enrichSeries(series)
	if got := stringValue(series["item_summary"]); got != "45 话" {
		t.Fatalf("item_summary = %q, want 45 话", got)
	}
	item := map[string]any{
		"candidate_id":        "synthetic-series-chapter-9001",
		"title":               "第9001话",
		"display_title":       "第9001话",
		"item_role":           "unknown",
		"sequence_number":     "9001",
		"relative_path":       `Synthetic Series\第9001话.cbz`,
		"readable_page_count": 24,
		"can_read":            true,
	}
	if got := chapterLabelFor(item, series); got != "第9001话" {
		t.Fatalf("chapter label = %q, want 第9001话", got)
	}
}

func TestSeriesDetailIncludesPerItemProgress(t *testing.T) {
	s := newCatalogTestServer(t)
	defer s.Close()

	seedSeriesWork(t, s, "series-progress-work", "Series Progress Work", "Other Artist", "2026-07-10T03:00:00Z")
	if _, err := s.db.Exec(`
		INSERT INTO series_groups (
			group_id, library_key, series_title, group_path, group_type, candidate_count, confidence, notes
		) VALUES (
			'series-progress', 'commercial-manga', 'Series Progress', 'Series Progress',
			'series_candidate', '1', '1.0', ''
		);
		INSERT INTO series_items (
			group_id, library_key, series_title, candidate_id, candidate_type, source_kind, title,
			item_role, sequence_number, sort_key, relative_path, parent_relative_path, page_file_count, confidence
		) VALUES (
			'series-progress', 'commercial-manga', 'Series Progress', 'series-progress-work',
			'manga', 'archive', 'Series Progress Work', 'chapter', '0', '000',
			'Series Progress\\00.zip', 'Series Progress', '12', '1.0'
		);
	`); err != nil {
		t.Fatal(err)
	}
	seededProgress, err := s.query(`
		SELECT page_manifest_id, manifest_hash_snapshot
		FROM reading_progress
		WHERE reader_profile_key = 'default'
		  AND work_identity_id = 'identity-series-progress-work'
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(seededProgress) != 1 {
		t.Fatalf("seeded progress = %#v, want one row", seededProgress)
	}
	if seededProgress[0]["page_manifest_id"] != "manifest-series-progress-work" || seededProgress[0]["manifest_hash_snapshot"] != "hash-series-progress-work" {
		t.Fatalf("seeded manifest snapshot = %#v", seededProgress[0])
	}

	payload := getJSON(t, s, "/api/series-detail?id=series-progress")
	items, ok := payload["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want one item", payload["items"])
	}
	item := items[0].(map[string]any)
	progress, ok := item["progress"].(map[string]any)
	if !ok {
		t.Fatalf("series item progress missing: %#v", item)
	}
	if progress["candidate_id"] != "series-progress-work" ||
		progress["work_identity_id"] != "identity-series-progress-work" ||
		progress["page_manifest_id"] != "manifest-series-progress-work" ||
		progress["manifest_hash"] != "hash-series-progress-work" ||
		intValue(progress["index"]) != 1 || intValue(progress["count"]) != 12 {
		t.Fatalf("series item progress = %#v", progress)
	}

	sections, ok := payload["sections"].([]any)
	if !ok || len(sections) != 1 {
		t.Fatalf("sections = %#v, want one section", payload["sections"])
	}
	groups := sections[0].(map[string]any)["groups"].([]any)
	nestedItems := groups[0].(map[string]any)["items"].([]any)
	if _, ok := nestedItems[0].(map[string]any)["progress"].(map[string]any); !ok {
		t.Fatalf("nested series item lost progress: %#v", nestedItems[0])
	}
}

func TestBuildSeriesSectionsKeepsZeroBeforeOne(t *testing.T) {
	items := []map[string]any{
		{
			"candidate_id":        "chapter-1",
			"title":               "第1话",
			"display_title":       "第1话",
			"item_role":           "chapter",
			"sequence_number":     "1",
			"can_read":            true,
			"readable_page_count": 20,
		},
		{
			"candidate_id":        "chapter-0",
			"title":               "第0话",
			"display_title":       "第0话",
			"item_role":           "chapter",
			"sequence_number":     "0",
			"can_read":            true,
			"readable_page_count": 18,
		},
	}
	series := map[string]any{
		"series_title":  "Test Series",
		"display_title": "Test Series",
		"series_kind":   "normal_manga",
	}
	sections, _, _ := buildSeriesSections(items, series)
	if len(sections) != 1 {
		t.Fatalf("sections = %d, want 1", len(sections))
	}
	groups := sectionGroups(sections[0])
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if got := stringValue(groups[0]["label"]); got != "第0话" {
		t.Fatalf("first group = %q, want 第0话", got)
	}
	if got := stringValue(groups[1]["label"]); got != "第1话" {
		t.Fatalf("second group = %q, want 第1话", got)
	}
}

func TestSeriesDetailUsesCandidateIDToBreakEditionTies(t *testing.T) {
	s := newCatalogTestServer(t)
	defer s.Close()
	seedSeriesWork(t, s, "edition-z", "Shared Chapter", "Other Artist", "2026-07-10T03:00:00Z")
	seedSeriesWork(t, s, "edition-a", "Shared Chapter", "Other Artist", "2026-07-10T03:01:00Z")
	if _, err := s.db.Exec(`
		INSERT INTO series_groups (
			group_id, library_key, series_title, group_path, group_type, candidate_count, confidence, notes
		) VALUES ('series-editions', 'commercial-manga', 'Edition Tie', 'Edition Tie', 'series_candidate', '2', '1.0', '');
		INSERT INTO series_items (
			group_id, library_key, series_title, candidate_id, candidate_type, source_kind, title,
			item_role, sequence_number, sort_key, relative_path, parent_relative_path, page_file_count, confidence
		) VALUES
			('series-editions', 'commercial-manga', 'Edition Tie', 'edition-z', 'manga', 'archive', 'Shared Chapter', 'chapter', '1', '001', 'Edition Tie\\z.zip', 'Edition Tie', '12', '1.0'),
			('series-editions', 'commercial-manga', 'Edition Tie', 'edition-a', 'manga', 'archive', 'Shared Chapter', 'chapter', '1', '001', 'Edition Tie\\a.zip', 'Edition Tie', '12', '1.0');
	`); err != nil {
		t.Fatal(err)
	}
	payload := getJSON(t, s, "/api/series-detail?id=series-editions")
	items := payload["items"].([]any)
	if len(items) != 2 || stringValue(items[0].(map[string]any)["candidate_id"]) != "edition-a" || stringValue(items[1].(map[string]any)["candidate_id"]) != "edition-z" {
		t.Fatalf("series item tie order = %#v, want edition-a then edition-z", items)
	}
	sections := payload["sections"].([]any)
	groups := sections[0].(map[string]any)["groups"].([]any)
	groupItems := groups[0].(map[string]any)["items"].([]any)
	if stringValue(groupItems[0].(map[string]any)["candidate_id"]) != "edition-a" {
		t.Fatalf("series primary edition = %#v, want edition-a", groupItems[0])
	}
}
