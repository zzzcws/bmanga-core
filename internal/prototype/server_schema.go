package prototype

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func runSQLiteMigration(db *sql.DB, migrationName string, migrate func(sqliteSchemaRunner) error) (returnErr error) {
	migrationName = strings.TrimSpace(migrationName)
	if migrationName == "" {
		return errors.New("sqlite migration name is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve sqlite migration connection: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin immediate sqlite migration %q: %w", migrationName, err)
	}
	runner := sqliteConnRunner{ctx: ctx, conn: conn}
	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer rollbackCancel()
		_, rollbackErr := conn.ExecContext(rollbackCtx, "ROLLBACK")
		if rollbackErr == nil {
			return
		}
		rollbackErr = fmt.Errorf("rollback sqlite migration %q: %w", migrationName, rollbackErr)
		if returnErr == nil {
			returnErr = rollbackErr
		} else {
			returnErr = errors.Join(returnErr, rollbackErr)
		}
	}()

	if _, err := runner.Exec(`
		CREATE TABLE IF NOT EXISTS bmanga_schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create sqlite migration registry: %w", err)
	}
	if err := migrate(runner); err != nil {
		return fmt.Errorf("apply sqlite migration %q: %w", migrationName, err)
	}
	if _, err := runner.Exec(`
		INSERT INTO bmanga_schema_migrations (name, applied_at)
		VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET applied_at = excluded.applied_at
	`, migrationName, nowISO()); err != nil {
		return fmt.Errorf("record sqlite migration %q: %w", migrationName, err)
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit sqlite migration %q: %w", migrationName, err)
	}
	committed = true
	return nil
}

func ensureLocalSQLiteTables(db *sql.DB) error {
	return runSQLiteMigration(db, "local-schema-v1", ensureLocalSQLiteTablesInTransaction)
}

func ensureLocalSQLiteTablesInTransaction(tx sqliteSchemaRunner) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS reader_profiles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS local_corrections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type TEXT NOT NULL,
			target_id TEXT NOT NULL,
			correction_type TEXT NOT NULL,
			correction_value TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (target_type, target_id, correction_type)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_local_corrections_target
			ON local_corrections (target_type, target_id)`,
		`CREATE INDEX IF NOT EXISTS idx_local_corrections_type
			ON local_corrections (correction_type, correction_value)`,
		`CREATE TABLE IF NOT EXISTS source_filesystem_times (
			target_type TEXT NOT NULL,
			target_id TEXT NOT NULL,
			library_key TEXT NOT NULL DEFAULT '',
			source_path TEXT NOT NULL DEFAULT '',
			source_relative_path TEXT NOT NULL DEFAULT '',
			source_path_kind TEXT NOT NULL DEFAULT '',
			source_created_utc TEXT NOT NULL DEFAULT '',
			source_modified_utc TEXT NOT NULL DEFAULT '',
			source_size_bytes INTEGER,
			status TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '',
			observed_at TEXT NOT NULL,
			PRIMARY KEY (target_type, target_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_source_filesystem_times_added
			ON source_filesystem_times (target_type, source_created_utc)`,
		`CREATE INDEX IF NOT EXISTS idx_source_filesystem_times_library
			ON source_filesystem_times (library_key, target_type, status)`,
		`CREATE INDEX IF NOT EXISTS idx_source_filesystem_times_work_added
			ON source_filesystem_times (target_type, status, source_created_utc, target_id)`,
		`CREATE TABLE IF NOT EXISTS metadata_field_overrides (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			work_identity_id TEXT NOT NULL,
			candidate_id TEXT,
			field_name TEXT NOT NULL,
			field_value TEXT NOT NULL DEFAULT '',
			source_proposal_id INTEGER,
			source_field_id INTEGER,
			override_status TEXT NOT NULL DEFAULT 'active',
			reviewer_note TEXT NOT NULL DEFAULT '',
			applied_at TEXT NOT NULL,
			reverted_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (work_identity_id, field_name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_metadata_field_overrides_work
			ON metadata_field_overrides (work_identity_id, override_status)`,
		`CREATE INDEX IF NOT EXISTS idx_metadata_field_overrides_field
			ON metadata_field_overrides (field_name, override_status)`,
		`CREATE INDEX IF NOT EXISTS idx_metadata_field_overrides_field_work
			ON metadata_field_overrides (field_name, override_status, work_identity_id, field_value)`,
		`CREATE TABLE IF NOT EXISTS duplicate_candidates (
			candidate_id TEXT PRIMARY KEY,
			left_book_id TEXT NOT NULL,
			right_book_id TEXT NOT NULL,
			score REAL NOT NULL,
			reason TEXT NOT NULL,
			evidence_json TEXT NOT NULL DEFAULT '{}',
			source TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (source, left_book_id, right_book_id, reason)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_duplicate_candidates_status
			ON duplicate_candidates (status, source, reason)`,
		`CREATE INDEX IF NOT EXISTS idx_duplicate_candidates_books
			ON duplicate_candidates (left_book_id, right_book_id, status)`,
		`CREATE TABLE IF NOT EXISTS work_user_marks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			reader_profile_key TEXT NOT NULL,
			work_identity_id TEXT NOT NULL,
			candidate_id TEXT,
			read_status TEXT NOT NULL DEFAULT 'unread',
			read_status_client_updated_at TEXT NOT NULL DEFAULT '',
			personal_rating INTEGER CHECK (personal_rating BETWEEN 0 AND 10),
			favorite INTEGER NOT NULL DEFAULT 0,
			reread_priority INTEGER NOT NULL DEFAULT 0 CHECK (reread_priority BETWEEN 0 AND 3),
			translation_quality INTEGER CHECK (translation_quality BETWEEN 1 AND 5),
			image_quality INTEGER CHECK (image_quality BETWEEN 1 AND 5),
			hidden INTEGER NOT NULL DEFAULT 0,
			hidden_reason TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			marked_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (reader_profile_key, work_identity_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_work_user_marks_identity
			ON work_user_marks (work_identity_id)`,
		`CREATE INDEX IF NOT EXISTS idx_work_user_marks_candidate
			ON work_user_marks (candidate_id)`,
		`CREATE INDEX IF NOT EXISTS idx_work_user_marks_rating
			ON work_user_marks (reader_profile_key, personal_rating)`,
		`CREATE INDEX IF NOT EXISTS idx_work_user_marks_favorite
			ON work_user_marks (reader_profile_key, favorite)`,
		`CREATE INDEX IF NOT EXISTS idx_work_user_marks_read_status
			ON work_user_marks (reader_profile_key, read_status)`,
		`CREATE TABLE IF NOT EXISTS series_user_marks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			reader_profile_key TEXT NOT NULL,
			series_identity_id TEXT NOT NULL,
			group_id TEXT,
			read_status TEXT NOT NULL DEFAULT 'unread',
			read_status_client_updated_at TEXT NOT NULL DEFAULT '',
			personal_rating INTEGER CHECK (personal_rating BETWEEN 0 AND 10),
			favorite INTEGER NOT NULL DEFAULT 0,
			reread_priority INTEGER NOT NULL DEFAULT 0 CHECK (reread_priority BETWEEN 0 AND 3),
			hidden INTEGER NOT NULL DEFAULT 0,
			hidden_reason TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			marked_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (reader_profile_key, series_identity_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_series_user_marks_group
			ON series_user_marks (group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_series_user_marks_identity
			ON series_user_marks (series_identity_id)`,
		`CREATE TABLE IF NOT EXISTS user_mark_field_clocks (
			reader_profile_key TEXT NOT NULL,
			target_type TEXT NOT NULL CHECK (target_type IN ('work', 'series')),
			identity_id TEXT NOT NULL,
			field_name TEXT NOT NULL CHECK (field_name IN (
				'personal_rating', 'favorite', 'reread_priority',
				'translation_quality', 'image_quality',
				'hidden', 'hidden_reason', 'notes'
			)),
			client_updated_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (reader_profile_key, target_type, identity_id, field_name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_mark_field_clocks_target
			ON user_mark_field_clocks (reader_profile_key, target_type, identity_id, client_updated_at)`,
		`CREATE TABLE IF NOT EXISTS local_tags (
			tag_key TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			color TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS work_tag_links (
			reader_profile_key TEXT NOT NULL,
			work_identity_id TEXT NOT NULL,
			candidate_id TEXT,
			tag_key TEXT NOT NULL REFERENCES local_tags(tag_key) ON DELETE CASCADE,
			created_at TEXT NOT NULL,
			UNIQUE (reader_profile_key, work_identity_id, tag_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_work_tag_links_tag
			ON work_tag_links (reader_profile_key, tag_key)`,
		`CREATE INDEX IF NOT EXISTS idx_work_tag_links_tag_identity
			ON work_tag_links (reader_profile_key, tag_key, work_identity_id)`,
		`CREATE INDEX IF NOT EXISTS idx_work_tag_links_identity
			ON work_tag_links (work_identity_id)`,
		`CREATE TABLE IF NOT EXISTS series_tag_links (
			reader_profile_key TEXT NOT NULL,
			series_identity_id TEXT NOT NULL,
			group_id TEXT,
			tag_key TEXT NOT NULL REFERENCES local_tags(tag_key) ON DELETE CASCADE,
			created_at TEXT NOT NULL,
			UNIQUE (reader_profile_key, series_identity_id, tag_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_series_tag_links_tag
			ON series_tag_links (reader_profile_key, tag_key)`,
		`CREATE INDEX IF NOT EXISTS idx_series_tag_links_identity
			ON series_tag_links (series_identity_id)`,
		`CREATE TABLE IF NOT EXISTS reading_progress (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			reader_profile_key TEXT NOT NULL,
			work_identity_id TEXT NOT NULL,
			candidate_id TEXT,
			page_manifest_id TEXT,
			manifest_hash_snapshot TEXT NOT NULL DEFAULT '',
			progress_status TEXT NOT NULL DEFAULT 'normal',
			last_page_index INTEGER NOT NULL DEFAULT 0,
			progress_percent REAL NOT NULL DEFAULT 0,
			completed INTEGER NOT NULL DEFAULT 0,
			page_count_snapshot INTEGER,
			reader_fit_mode TEXT NOT NULL DEFAULT '',
			reader_split_panel INTEGER NOT NULL DEFAULT 0,
			stage_scroll_top INTEGER NOT NULL DEFAULT 0,
			stage_scroll_left INTEGER NOT NULL DEFAULT 0,
			last_read_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (reader_profile_key, work_identity_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_reading_progress_identity
			ON reading_progress (work_identity_id)`,
		`CREATE INDEX IF NOT EXISTS idx_reading_progress_candidate
			ON reading_progress (candidate_id)`,
		`CREATE INDEX IF NOT EXISTS idx_reading_progress_status
			ON reading_progress (reader_profile_key, progress_status)`,
		`CREATE INDEX IF NOT EXISTS idx_reading_progress_recent
			ON reading_progress (reader_profile_key, last_read_at DESC, updated_at DESC, work_identity_id)
			WHERE COALESCE(last_read_at, '') <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_reading_progress_recent_julianday
			ON reading_progress (
				reader_profile_key,
				julianday(last_read_at) DESC,
				julianday(updated_at) DESC,
				work_identity_id
			)
			WHERE last_read_at <> ''`,
		`CREATE TABLE IF NOT EXISTS reading_progress_resets (
			reader_profile_key TEXT NOT NULL,
			work_identity_id TEXT NOT NULL,
			reset_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (reader_profile_key, work_identity_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_reading_progress_resets_reset
			ON reading_progress_resets (reader_profile_key, reset_at)`,
		`CREATE TABLE IF NOT EXISTS browse_states (
			reader_profile_key TEXT NOT NULL,
			state_key TEXT NOT NULL,
			state_json TEXT NOT NULL DEFAULT '{}',
			client_updated_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (reader_profile_key, state_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_browse_states_profile
			ON browse_states (reader_profile_key, state_key, client_updated_at)`,
		`CREATE TABLE IF NOT EXISTS page_manifests (
			page_manifest_id TEXT PRIMARY KEY,
			work_identity_id TEXT NOT NULL,
			candidate_id TEXT,
			manifest_hash TEXT NOT NULL,
			page_count INTEGER NOT NULL DEFAULT 0,
			source_kind TEXT NOT NULL,
			manifest_status TEXT NOT NULL DEFAULT 'ready',
			builder_version TEXT NOT NULL,
			built_at TEXT NOT NULL,
			UNIQUE (work_identity_id, manifest_hash)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_page_manifests_work_status
			ON page_manifests (work_identity_id, manifest_status)`,
		`CREATE TABLE IF NOT EXISTS page_manifest_items (
			page_manifest_id TEXT NOT NULL,
			page_index INTEGER NOT NULL,
			library_key TEXT NOT NULL,
			source_path TEXT NOT NULL,
			source_relative_path TEXT NOT NULL,
			source_inner_path TEXT NOT NULL DEFAULT '',
			extension TEXT NOT NULL DEFAULT '',
			mime_type TEXT NOT NULL DEFAULT '',
			size_bytes INTEGER,
			modified_utc TEXT NOT NULL DEFAULT '',
			quick_hash TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (page_manifest_id, page_index)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_page_manifest_items_manifest
			ON page_manifest_items (page_manifest_id, page_index)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	if err := ensureOptionalPerformanceIndexes(tx); err != nil {
		return err
	}
	progressColumns := map[string]string{
		"reader_fit_mode":    "reader_fit_mode TEXT NOT NULL DEFAULT ''",
		"reader_split_panel": "reader_split_panel INTEGER NOT NULL DEFAULT 0",
		"stage_scroll_top":   "stage_scroll_top INTEGER NOT NULL DEFAULT 0",
		"stage_scroll_left":  "stage_scroll_left INTEGER NOT NULL DEFAULT 0",
	}
	if err := ensureSQLiteColumns(tx, "reading_progress", progressColumns); err != nil {
		return err
	}
	workUserMarkColumns := map[string]string{
		"read_status_client_updated_at": "read_status_client_updated_at TEXT NOT NULL DEFAULT ''",
	}
	if err := ensureSQLiteColumns(tx, "work_user_marks", workUserMarkColumns); err != nil {
		return err
	}
	seriesUserMarkColumns := map[string]string{
		"read_status_client_updated_at": "read_status_client_updated_at TEXT NOT NULL DEFAULT ''",
	}
	if err := ensureSQLiteColumns(tx, "series_user_marks", seriesUserMarkColumns); err != nil {
		return err
	}
	if err := ensureOptimizedWorkBrowseViewInRunner(tx, optimizedWorkBrowseViewDDL); err != nil {
		return err
	}
	now := nowISO()
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO reader_profiles (key, display_name, created_at, updated_at)
		VALUES (?, ?, ?, ?)`,
		"default", "Default", now, now,
	); err != nil {
		return err
	}
	return nil
}

func ensureSQLiteColumns(db sqliteSchemaRunner, table string, columns map[string]string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		existing[name] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for name, ddl := range columns {
		if !existing[name] {
			if _, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + ddl); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureOptimizedWorkBrowseView(db *sql.DB) error {
	return ensureOptimizedWorkBrowseViewWithDDL(db, optimizedWorkBrowseViewDDL)
}

func ensureOptimizedWorkBrowseViewWithDDL(db *sql.DB, createViewDDL string) error {
	return runSQLiteMigration(db, "optimized-work-browse-view-v1", func(tx sqliteSchemaRunner) error {
		return ensureOptimizedWorkBrowseViewInRunner(tx, createViewDDL)
	})
}

func ensureOptimizedWorkBrowseViewInRunner(db sqliteSchemaRunner, createViewDDL string) error {
	var baseTableCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'work_candidates'`).Scan(&baseTableCount); err != nil {
		return err
	}
	if baseTableCount > 0 {
		var existingType string
		err := db.QueryRow(`SELECT type FROM sqlite_master WHERE name = 'work_browse' ORDER BY CASE type WHEN 'view' THEN 0 ELSE 1 END LIMIT 1`).Scan(&existingType)
		if errors.Is(err, sql.ErrNoRows) {
			existingType = ""
		} else if err != nil {
			return err
		}
		if existingType != "table" {
			if _, err := db.Exec(`DROP VIEW IF EXISTS work_browse`); err != nil {
				return fmt.Errorf("drop work_browse view: %w", err)
			}
			if _, err := db.Exec(createViewDDL); err != nil {
				return fmt.Errorf("create optimized work_browse view: %w", err)
			}
		}
	}
	return nil
}

const optimizedWorkBrowseViewDDL = `
		CREATE VIEW work_browse AS
		SELECT
			wi.work_identity_id,
			wc.candidate_id,
			wc.library_key,
			wc.library_name,
			wc.candidate_type,
			wc.source_kind,
			wc.title,
			wc.path,
			wc.relative_path,
			wc.parent_relative_path,
			wc.size_bytes,
			wc.modified_utc,
			wc.extension,
			COALESCE(pc.page_count_status, '') AS page_count_status,
			COALESCE(pc.readable_page_count, '') AS readable_page_count,
			COALESCE(pc.reason, '') AS page_count_reason,
			COALESCE(wcc.cover_status, '') AS cover_status,
			COALESCE(wcc.cover_kind, '') AS cover_kind,
			COALESCE((
				SELECT GROUP_CONCAT(DISTINCT ti.translation_group)
				FROM translation_items ti
				WHERE ti.candidate_id = wc.candidate_id
			), '') AS translation_sources
		FROM work_candidates wc
		LEFT JOIN page_counts pc ON pc.candidate_id = wc.candidate_id
		LEFT JOIN work_cover_candidates wcc ON wcc.candidate_id = wc.candidate_id
		LEFT JOIN work_identities wi ON wi.current_candidate_id = wc.candidate_id
	`

func ensureOptionalPerformanceIndexes(db sqliteSchemaRunner) error {
	optional := []struct {
		table string
		sql   string
	}{
		{
			table: "series_items",
			sql: `CREATE INDEX IF NOT EXISTS idx_series_items_candidate_covering
				ON series_items (candidate_id, series_title, item_role, sequence_number)`,
		},
		{
			table: "series_items",
			sql: `CREATE INDEX IF NOT EXISTS idx_series_items_group_sequence_covering
				ON series_items (group_id, sequence_number, item_role, candidate_id)`,
		},
		{
			table: "page_counts",
			sql: `CREATE INDEX IF NOT EXISTS idx_page_counts_candidate_status_reason
				ON page_counts (candidate_id, page_count_status, reason, readable_page_count)`,
		},
		{
			table: "cover_image_hashes",
			sql: `CREATE INDEX IF NOT EXISTS idx_cover_image_hashes_difference_candidate
				ON cover_image_hashes (difference_hash, candidate_id)`,
		},
		{
			table: "work_candidates",
			sql: `CREATE INDEX IF NOT EXISTS idx_work_candidates_shelf_default_order
				ON work_candidates (library_key, title COLLATE NOCASE, relative_path COLLATE NOCASE, candidate_id, candidate_type, source_kind)`,
		},
		{
			table: "work_candidates",
			sql: `CREATE INDEX IF NOT EXISTS idx_work_candidates_title_lookup
				ON work_candidates (title COLLATE NOCASE, candidate_id)`,
		},
		{
			table: "work_candidates",
			sql: `CREATE INDEX IF NOT EXISTS idx_work_candidates_shelf_title_order
				ON work_candidates (title COLLATE NOCASE, relative_path COLLATE NOCASE, candidate_id, library_key, candidate_type, source_kind)`,
		},
		{
			table: "work_candidates",
			sql: `CREATE INDEX IF NOT EXISTS idx_work_candidates_relative_path_lookup
				ON work_candidates (relative_path COLLATE NOCASE, candidate_id)`,
		},
		{
			table: "page_counts",
			sql: `CREATE INDEX IF NOT EXISTS idx_page_counts_shelf_pages_desc_v2
				ON page_counts (CAST(COALESCE(readable_page_count, 0) AS INTEGER) DESC, candidate_id)`,
		},
		{
			table: "cover_assets",
			sql: `CREATE INDEX IF NOT EXISTS idx_cover_assets_cache_path_present
				ON cover_assets (cache_path)
				WHERE COALESCE(cache_path, '') <> ''`,
		},
	}
	for _, item := range optional {
		var tableName string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, item.table).Scan(&tableName)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if _, err := db.Exec(item.sql); err != nil {
			return err
		}
	}
	return nil
}
