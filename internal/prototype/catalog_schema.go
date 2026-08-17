package prototype

import "database/sql"

// EnsureCatalogTables creates the source-independent catalog schema used by
// both the local scanner and the reader service.
func EnsureCatalogTables(db *sql.DB) error {
	return runSQLiteMigration(db, "catalog-schema-v1", ensureCatalogTablesInTransaction)
}

func ensureCatalogTablesInTransaction(tx sqliteSchemaRunner) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS libraries (
			key TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			root_path TEXT NOT NULL DEFAULT '',
			mode TEXT NOT NULL DEFAULT '',
			scanned_record_count TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_libraries_key ON libraries (key)`,
		`CREATE TABLE IF NOT EXISTS scan_entries (
			stable_key TEXT NOT NULL PRIMARY KEY,
			library_key TEXT NOT NULL,
			library_name TEXT NOT NULL DEFAULT '',
			root TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL,
			relative_path TEXT NOT NULL,
			entry_type TEXT NOT NULL,
			item_kind TEXT NOT NULL,
			status TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			size_bytes INTEGER NOT NULL DEFAULT 0,
			modified_utc TEXT NOT NULL DEFAULT '',
			extension TEXT NOT NULL DEFAULT '',
			page_file_count INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_scan_entries_library_pages
			ON scan_entries (library_key, item_kind, status, relative_path)`,
		`CREATE TABLE IF NOT EXISTS work_candidates (
			candidate_id TEXT, library_key TEXT, library_name TEXT, candidate_type TEXT, source_kind TEXT,
			title TEXT, root TEXT, path TEXT, relative_path TEXT, parent_relative_path TEXT, source_record_id TEXT,
			source_status TEXT, source_reason TEXT, size_bytes TEXT, modified_utc TEXT, extension TEXT,
			page_file_count TEXT, confidence TEXT, notes TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS translation_items (
			candidate_id TEXT, translation_group TEXT
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_work_candidates_candidate_id_unique
			ON work_candidates (candidate_id)`,
		`CREATE INDEX IF NOT EXISTS idx_work_candidates_candidate_id ON work_candidates (candidate_id)`,
		`CREATE TABLE IF NOT EXISTS page_counts (
			candidate_id TEXT, library_key TEXT, candidate_type TEXT, source_kind TEXT, title TEXT, path TEXT, extension TEXT,
			page_count_status TEXT, readable_page_count TEXT, total_entry_count TEXT, reason TEXT, elapsed_ms TEXT
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_page_counts_candidate_id_unique
			ON page_counts (candidate_id)`,
		`CREATE INDEX IF NOT EXISTS idx_page_counts_candidate_status_reason ON page_counts (candidate_id, page_count_status, reason, readable_page_count)`,
		`CREATE TABLE IF NOT EXISTS work_cover_candidates (
			candidate_id TEXT, library_key TEXT, candidate_type TEXT, source_kind TEXT, title TEXT, cover_status TEXT,
			cover_kind TEXT, cover_source_path TEXT, cover_source_relative_path TEXT, cover_source_record_id TEXT,
			requires_extraction TEXT, confidence TEXT, reason TEXT, cover_sort_key TEXT
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_work_cover_candidates_candidate_id_unique
			ON work_cover_candidates (candidate_id)`,
		`CREATE INDEX IF NOT EXISTS idx_work_cover_candidates_candidate_id ON work_cover_candidates (candidate_id)`,
		`CREATE TABLE IF NOT EXISTS series_groups (
			group_id TEXT, library_key TEXT, series_title TEXT, group_path TEXT, group_type TEXT,
			candidate_count TEXT, confidence TEXT, notes TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_series_groups_group_id ON series_groups (group_id)`,
		`CREATE TABLE IF NOT EXISTS series_items (
			group_id TEXT, library_key TEXT, series_title TEXT, candidate_id TEXT, candidate_type TEXT,
			source_kind TEXT, title TEXT, item_role TEXT, sequence_number TEXT, sort_key TEXT,
			relative_path TEXT, parent_relative_path TEXT, page_file_count TEXT, confidence TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_series_items_candidate_covering ON series_items (candidate_id, series_title, item_role, sequence_number)`,
		`CREATE INDEX IF NOT EXISTS idx_series_items_group_sequence_covering ON series_items (group_id, sequence_number, item_role, candidate_id)`,
		`CREATE TABLE IF NOT EXISTS series_cover_candidates (
			group_id TEXT, library_key TEXT, series_title TEXT, selected_candidate_id TEXT, selected_title TEXT,
			cover_status TEXT, cover_kind TEXT, cover_source_path TEXT, cover_source_relative_path TEXT,
			requires_extraction TEXT, confidence TEXT, reason TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS work_identities (
			work_identity_id TEXT PRIMARY KEY, library_key TEXT NOT NULL, current_candidate_id TEXT,
			identity_type TEXT NOT NULL, display_title TEXT NOT NULL, canonical_relative_path TEXT NOT NULL DEFAULT '',
			match_status TEXT NOT NULL DEFAULT 'matched', identity_version TEXT NOT NULL,
			first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_work_identities_candidate ON work_identities (current_candidate_id)`,
		`CREATE TABLE IF NOT EXISTS work_identity_path_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT, work_identity_id TEXT NOT NULL, library_key TEXT NOT NULL,
			candidate_id TEXT, relative_path TEXT NOT NULL, path_fingerprint TEXT NOT NULL DEFAULT '',
			active INTEGER NOT NULL DEFAULT 1, first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL,
			UNIQUE (work_identity_id, library_key, relative_path)
		)`,
		`CREATE TABLE IF NOT EXISTS series_identities (
			series_identity_id TEXT PRIMARY KEY, library_key TEXT NOT NULL, current_group_id TEXT,
			identity_type TEXT NOT NULL, display_title TEXT NOT NULL, canonical_group_path TEXT NOT NULL DEFAULT '',
			match_status TEXT NOT NULL DEFAULT 'matched', identity_version TEXT NOT NULL,
			first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_series_identities_group ON series_identities (current_group_id)`,
		`CREATE TABLE IF NOT EXISTS series_identity_path_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT, series_identity_id TEXT NOT NULL, library_key TEXT NOT NULL,
			group_id TEXT, group_path TEXT NOT NULL, path_fingerprint TEXT NOT NULL DEFAULT '',
			active INTEGER NOT NULL DEFAULT 1, first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL,
			UNIQUE (series_identity_id, library_key, group_path)
		)`,
		`CREATE TABLE IF NOT EXISTS local_search_index (
			target_type TEXT NOT NULL, target_id TEXT NOT NULL, library_key TEXT NOT NULL DEFAULT '',
			search_text TEXT NOT NULL, index_version TEXT NOT NULL, source_hash TEXT NOT NULL, indexed_at TEXT NOT NULL,
			PRIMARY KEY (target_type, target_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_local_search_index_target ON local_search_index (target_type, library_key)`,
		`DROP VIEW IF EXISTS library_dashboard`,
		`CREATE VIEW library_dashboard AS
			SELECT
				l.key AS library_key,
				l.name AS name,
				l.name AS library_name,
				COUNT(w.candidate_id) AS work_count,
				SUM(CASE WHEN w.candidate_type = 'doujin' THEN 1 ELSE 0 END) AS doujin_count,
				SUM(CASE WHEN w.candidate_id IS NOT NULL AND w.candidate_type <> 'doujin' THEN 1 ELSE 0 END) AS manga_count,
				SUM(CASE WHEN w.source_kind = 'image_folder' THEN 1 ELSE 0 END) AS image_folder_count,
				SUM(CASE WHEN w.source_kind = 'archive' THEN 1 ELSE 0 END) AS archive_count,
				SUM(CASE WHEN w.source_kind = 'pdf' THEN 1 ELSE 0 END) AS pdf_count
			FROM libraries l
			LEFT JOIN work_candidates w ON w.library_key = l.key
			GROUP BY l.key, l.name
			UNION ALL
			SELECT
				w.library_key AS library_key,
				COALESCE(MAX(NULLIF(w.library_name, '')), w.library_key) AS name,
				COALESCE(MAX(NULLIF(w.library_name, '')), w.library_key) AS library_name,
				COUNT(*) AS work_count,
				SUM(CASE WHEN w.candidate_type = 'doujin' THEN 1 ELSE 0 END) AS doujin_count,
				SUM(CASE WHEN w.candidate_type <> 'doujin' THEN 1 ELSE 0 END) AS manga_count,
				SUM(CASE WHEN w.source_kind = 'image_folder' THEN 1 ELSE 0 END) AS image_folder_count,
				SUM(CASE WHEN w.source_kind = 'archive' THEN 1 ELSE 0 END) AS archive_count,
				SUM(CASE WHEN w.source_kind = 'pdf' THEN 1 ELSE 0 END) AS pdf_count
			FROM work_candidates w
			WHERE NOT EXISTS (
				SELECT 1
				FROM libraries l
				WHERE l.key = w.library_key
			)
			GROUP BY w.library_key`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	if err := ensureOptionalPerformanceIndexes(tx); err != nil {
		return err
	}
	return ensureOptimizedWorkBrowseViewInRunner(tx, optimizedWorkBrowseViewDDL)
}
