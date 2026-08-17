package prototype

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestEnsureCatalogTablesRollsBackOnSchemaFailure(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE work_candidates (candidate_id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	if err := EnsureCatalogTables(db); err == nil {
		t.Fatal("incompatible catalog schema unexpectedly migrated")
	}

	var librariesCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'libraries'`).Scan(&librariesCount); err != nil {
		t.Fatal(err)
	}
	if librariesCount != 0 {
		t.Fatalf("partial catalog schema survived failed migration: libraries count = %d, want 0", librariesCount)
	}
	var workCandidatesCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'work_candidates'`).Scan(&workCandidatesCount); err != nil {
		t.Fatal(err)
	}
	if workCandidatesCount != 1 {
		t.Fatalf("pre-existing catalog schema changed after rollback: work_candidates count = %d, want 1", workCandidatesCount)
	}
}
