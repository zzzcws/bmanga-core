package prototype

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var errUnsupportedLegacyMaintenanceState = errors.New("unsupported legacy cleanup/author-exclusion state")

// rejectUnsupportedLegacyMaintenanceState keeps the public core fail-closed.
// Legacy maintenance decisions changed catalogue visibility, but the public
// core intentionally does not implement those workflows. Refusing to start is
// safer than silently exposing works that an older build hid. Candidate IDs
// are stable, so a decision remains meaningful even while its work is absent
// and may be recreated by a later scan. This check only reads the database and
// runs before any schema or view migration.
func rejectUnsupportedLegacyMaintenanceState(db *sql.DB) error {
	legacyState := make([]string, 0, 6)
	workBrowseType, err := sqliteSchemaObjectType(db, "work_browse")
	if err != nil {
		return fmt.Errorf("inspect legacy work_browse object: %w", err)
	}
	if workBrowseType == "table" {
		legacyState = append(legacyState, "materialized work_browse table")
	}

	matched, err := legacyRowsMatch(db, "deletion_candidates", []string{"candidate_id"}, `
		SELECT EXISTS (
			SELECT 1
			FROM deletion_candidates
			WHERE TRIM(COALESCE(candidate_id, '')) <> ''
			LIMIT 1
		)
	`)
	if err != nil {
		return err
	}
	if matched {
		legacyState = append(legacyState, "deletion candidates")
	}

	matched, err = legacyRowsMatch(db, "cleanup_quarantine_records", []string{"candidate_id", "status"}, `
		SELECT EXISTS (
			SELECT 1
			FROM cleanup_quarantine_records
			WHERE TRIM(COALESCE(candidate_id, '')) <> ''
			  AND LOWER(TRIM(COALESCE(status, ''))) IN ('quarantined', 'final_deleted')
			LIMIT 1
		)
	`)
	if err != nil {
		return err
	}
	if matched {
		legacyState = append(legacyState, "quarantined or deleted works")
	}

	matched, err = legacyRowsMatch(db, "local_corrections", []string{"target_type", "target_id", "correction_type", "correction_value"}, `
		SELECT EXISTS (
			SELECT 1
			FROM local_corrections
			WHERE target_type = 'work'
			  AND correction_type = 'review_status'
			  AND TRIM(COALESCE(target_id, '')) <> ''
			  AND LOWER(TRIM(COALESCE(correction_value, ''))) IN ('cleanup_confirmed', 'delete_candidate', 'keep')
			LIMIT 1
		)
	`)
	if err != nil {
		return err
	}
	if matched {
		legacyState = append(legacyState, "legacy visibility corrections")
	}

	matched, err = legacyTranslationMaintenanceMatches(db)
	if err != nil {
		return err
	}
	if matched {
		legacyState = append(legacyState, "translation maintenance actions")
	}

	matched, err = legacyRowsMatch(db, "author_blacklist_rules", []string{"normalized_author", "enabled"}, `
		SELECT EXISTS (
			SELECT 1
			FROM author_blacklist_rules
			WHERE COALESCE(enabled, 1) <> 0
			  AND TRIM(COALESCE(normalized_author, '')) <> ''
			LIMIT 1
		)
	`)
	if err != nil {
		return err
	}
	if matched {
		legacyState = append(legacyState, "enabled author-exclusion rules")
	}

	if len(legacyState) == 0 {
		return nil
	}
	return fmt.Errorf(
		"%w: %s; this build refused to start because it cannot preserve those visibility decisions; no database rows were changed",
		errUnsupportedLegacyMaintenanceState,
		strings.Join(legacyState, ", "),
	)
}

func legacyTranslationMaintenanceMatches(db *sql.DB) (bool, error) {
	objectType, err := sqliteSchemaObjectType(db, "translation_items")
	if err != nil {
		return false, fmt.Errorf("inspect legacy object translation_items: %w", err)
	}
	if objectType == "" {
		return false, nil
	}
	nonEmpty, err := sqliteObjectHasRows(db, "translation_items")
	if err != nil {
		return false, fmt.Errorf("inspect legacy object translation_items: %w", err)
	}
	if !nonEmpty {
		return false, nil
	}
	hasCandidateID, err := sqliteObjectHasColumns(db, "translation_items", "candidate_id")
	if err != nil {
		return false, fmt.Errorf("inspect legacy object translation_items columns: %w", err)
	}
	if !hasCandidateID {
		return false, fmt.Errorf(
			"%w: non-empty legacy object translation_items has an unrecognized schema; no database rows were changed",
			errUnsupportedLegacyMaintenanceState,
		)
	}
	hasAction, err := sqliteObjectHasColumns(db, "translation_items", "action")
	if err != nil {
		return false, fmt.Errorf("inspect legacy object translation_items columns: %w", err)
	}
	if !hasAction {
		return false, nil
	}

	var matched int
	if err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1
			FROM translation_items
			WHERE TRIM(COALESCE(candidate_id, '')) <> ''
			  AND LOWER(TRIM(COALESCE(action, ''))) NOT IN ('', 'keep')
			LIMIT 1
		)
	`).Scan(&matched); err != nil {
		return false, fmt.Errorf("inspect legacy object translation_items rows: %w", err)
	}
	return matched != 0, nil
}

func legacyRowsMatch(db *sql.DB, objectName string, requiredColumns []string, query string) (bool, error) {
	objectType, err := sqliteSchemaObjectType(db, objectName)
	if err != nil {
		return false, fmt.Errorf("inspect legacy object %s: %w", objectName, err)
	}
	if objectType == "" {
		return false, nil
	}

	nonEmpty, err := sqliteObjectHasRows(db, objectName)
	if err != nil {
		return false, fmt.Errorf("inspect legacy object %s: %w", objectName, err)
	}
	if !nonEmpty {
		return false, nil
	}

	hasColumns, err := sqliteObjectHasColumns(db, objectName, requiredColumns...)
	if err != nil {
		return false, fmt.Errorf("inspect legacy object %s columns: %w", objectName, err)
	}
	if !hasColumns {
		return false, fmt.Errorf(
			"%w: non-empty legacy object %s has an unrecognized schema; no database rows were changed",
			errUnsupportedLegacyMaintenanceState,
			objectName,
		)
	}

	var matched int
	if err := db.QueryRow(query).Scan(&matched); err != nil {
		return false, fmt.Errorf("inspect legacy object %s rows: %w", objectName, err)
	}
	return matched != 0, nil
}

func sqliteSchemaObjectType(db *sql.DB, name string) (string, error) {
	var objectType string
	err := db.QueryRow(`
		SELECT type
		FROM sqlite_master
		WHERE type IN ('table', 'view') AND name = ?
		LIMIT 1
	`, name).Scan(&objectType)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return objectType, err
}

func sqliteObjectHasColumns(db *sql.DB, name string, requiredColumns ...string) (bool, error) {
	if len(requiredColumns) == 0 {
		return true, nil
	}
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, name)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	found := make(map[string]bool, len(requiredColumns))
	for rows.Next() {
		var columnName string
		if err := rows.Scan(&columnName); err != nil {
			return false, err
		}
		found[columnName] = true
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, columnName := range requiredColumns {
		if !found[columnName] {
			return false, nil
		}
	}
	return true, nil
}

func sqliteObjectHasRows(db *sql.DB, name string) (bool, error) {
	var nonEmpty int
	// Callers only pass fixed, internal schema object names.
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM "` + name + `" LIMIT 1)`).Scan(&nonEmpty); err != nil {
		return false, err
	}
	return nonEmpty != 0, nil
}
