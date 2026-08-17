package catalog

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const scannerVersion = "core-scan-v1"

var libraryKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

var imageExtensions = map[string]bool{
	".gif": true, ".jpeg": true, ".jpg": true, ".png": true, ".webp": true,
}

var archiveExtensions = map[string]string{
	".cbz":  "archive",
	".epub": "ebook",
	".zip":  "archive",
}

type Config struct {
	Database  string          `json:"database"`
	Libraries []LibraryConfig `json:"libraries"`
}

type LibraryConfig struct {
	Key               string   `json:"key"`
	Name              string   `json:"name"`
	Root              string   `json:"root"`
	Mode              string   `json:"mode"`
	CatalogKind       string   `json:"catalog_kind"`
	IgnoreDirectories []string `json:"ignore_directories"`
}

type Summary struct {
	Database    string   `json:"database"`
	Libraries   int      `json:"libraries"`
	Works       int      `json:"works"`
	Pages       int      `json:"pages"`
	Skipped     int      `json:"skipped"`
	Warnings    []string `json:"warnings,omitempty"`
	Scanner     string   `json:"scanner"`
	CompletedAt string   `json:"completed_at"`
}

type candidate struct {
	ID                  string
	IdentityID          string
	LibraryKey          string
	LibraryName         string
	CandidateType       string
	IdentityType        string
	SourceKind          string
	Title               string
	AbsolutePath        string
	RelativePath        string
	ParentRelativePath  string
	SizeBytes           int64
	ModifiedUTC         string
	Extension           string
	PageCount           int
	CoverKind           string
	CoverSourcePath     string
	CoverSourceRelative string
	RequiresExtraction  bool
	ImageEntries        []imageEntry
}

type imageEntry struct {
	AbsolutePath string
	RelativePath string
	SizeBytes    int64
	ModifiedUTC  string
	Extension    string
}

func LoadConfig(configPath string) (Config, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return Config{}, errors.New("config path is required")
	}
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("resolve config path: %w", err)
	}
	data, err := os.ReadFile(absConfig)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	base := filepath.Dir(absConfig)
	if strings.TrimSpace(config.Database) == "" {
		config.Database = filepath.Join(base, "data", "bmanga.sqlite")
	} else if !filepath.IsAbs(config.Database) {
		config.Database = filepath.Join(base, config.Database)
	}
	config.Database, err = filepath.Abs(config.Database)
	if err != nil {
		return Config{}, fmt.Errorf("resolve database path: %w", err)
	}
	config.Database, err = resolvePathWithExistingAncestor(config.Database)
	if err != nil {
		return Config{}, fmt.Errorf("resolve database path links: %w", err)
	}
	if len(config.Libraries) == 0 {
		return Config{}, errors.New("at least one library is required")
	}
	seen := map[string]bool{}
	for index := range config.Libraries {
		library := &config.Libraries[index]
		library.Key = strings.ToLower(strings.TrimSpace(library.Key))
		if !libraryKeyPattern.MatchString(library.Key) {
			return Config{}, fmt.Errorf("library %d has invalid key %q", index+1, library.Key)
		}
		if seen[library.Key] {
			return Config{}, fmt.Errorf("duplicate library key %q", library.Key)
		}
		seen[library.Key] = true
		library.Name = strings.TrimSpace(library.Name)
		if library.Name == "" {
			library.Name = library.Key
		}
		library.Mode = strings.ToLower(strings.TrimSpace(library.Mode))
		switch library.Mode {
		case "archive", "image-folder", "mixed":
		default:
			return Config{}, fmt.Errorf("library %q has invalid mode %q", library.Key, library.Mode)
		}
		library.CatalogKind = strings.ToLower(strings.TrimSpace(library.CatalogKind))
		if library.CatalogKind == "" {
			library.CatalogKind = "standalone"
		}
		if library.CatalogKind != "standalone" && library.CatalogKind != "collection" {
			return Config{}, fmt.Errorf("library %q has invalid catalog_kind %q", library.Key, library.CatalogKind)
		}
		if !filepath.IsAbs(library.Root) {
			library.Root = filepath.Join(base, library.Root)
		}
		library.Root, err = filepath.Abs(library.Root)
		if err != nil {
			return Config{}, fmt.Errorf("resolve library %q root: %w", library.Key, err)
		}
		library.Root, err = filepath.EvalSymlinks(library.Root)
		if err != nil {
			return Config{}, fmt.Errorf("resolve library %q root links: %w", library.Key, err)
		}
		info, statErr := os.Stat(library.Root)
		if statErr != nil {
			return Config{}, fmt.Errorf("library %q root: %w", library.Key, statErr)
		}
		if !info.IsDir() {
			return Config{}, fmt.Errorf("library %q root is not a directory", library.Key)
		}
		if pathWithin(library.Root, config.Database) {
			return Config{}, fmt.Errorf("database path must be outside library %q root", library.Key)
		}
	}
	return config, nil
}

func Scan(ctx context.Context, config Config) (Summary, error) {
	if err := validateResolvedConfig(config); err != nil {
		return Summary{}, err
	}
	if err := os.MkdirAll(filepath.Dir(config.Database), 0o755); err != nil {
		return Summary{}, fmt.Errorf("create database directory: %w", err)
	}
	dsn := "file:" + filepath.ToSlash(config.Database) + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return Summary{}, fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return Summary{}, fmt.Errorf("ping database: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Summary{}, fmt.Errorf("begin scan transaction: %w", err)
	}
	defer tx.Rollback()
	if err := ensureSchema(ctx, tx); err != nil {
		return Summary{}, err
	}

	summary := Summary{Database: config.Database, Scanner: scannerVersion, Libraries: len(config.Libraries)}
	for _, library := range config.Libraries {
		candidates, skipped, warnings, scanErr := scanLibrary(ctx, library)
		if scanErr != nil {
			return Summary{}, scanErr
		}
		summary.Skipped += skipped
		summary.Warnings = append(summary.Warnings, warnings...)
		if err := replaceLibraryCatalog(ctx, tx, library, candidates); err != nil {
			return Summary{}, err
		}
		summary.Works += len(candidates)
		for _, item := range candidates {
			summary.Pages += item.PageCount
		}
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE VIEW IF NOT EXISTS library_dashboard AS
		SELECT library_key,
		       COUNT(*) AS work_count,
		       SUM(CASE WHEN candidate_type = 'doujin' THEN 1 ELSE 0 END) AS doujin_count,
		       SUM(CASE WHEN candidate_type <> 'doujin' THEN 1 ELSE 0 END) AS manga_count,
		       SUM(CASE WHEN source_kind = 'archive' THEN 1 ELSE 0 END) AS archive_count,
		       SUM(CASE WHEN source_kind = 'pdf' THEN 1 ELSE 0 END) AS pdf_count,
		       SUM(CASE WHEN source_kind = 'image_folder' THEN 1 ELSE 0 END) AS image_folder_count
		FROM work_candidates GROUP BY library_key
	`); err != nil {
		return Summary{}, fmt.Errorf("create dashboard view: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Summary{}, fmt.Errorf("commit scan: %w", err)
	}
	sort.Strings(summary.Warnings)
	summary.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return summary, nil
}

func validateResolvedConfig(config Config) error {
	if !filepath.IsAbs(config.Database) {
		return errors.New("database path must be absolute; use LoadConfig")
	}
	if len(config.Libraries) == 0 {
		return errors.New("at least one library is required")
	}
	for _, library := range config.Libraries {
		if !libraryKeyPattern.MatchString(library.Key) || !filepath.IsAbs(library.Root) {
			return fmt.Errorf("library %q is not normalized; use LoadConfig", library.Key)
		}
	}
	return nil
}

func scanLibrary(ctx context.Context, library LibraryConfig) ([]candidate, int, []string, error) {
	ignored := map[string]bool{"#recycle": true, ".cache": true, ".git": true, "@eadir": true}
	for _, value := range library.IgnoreDirectories {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			ignored[value] = true
		}
	}
	items := []candidate{}
	skipped := 0
	warnings := []string{}
	err := filepath.WalkDir(library.Root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			relative := safeRelative(library.Root, current)
			warnings = append(warnings, fmt.Sprintf("%s: %v", relative, walkErr))
			skipped++
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if current != library.Root && ignored[strings.ToLower(entry.Name())] {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			skipped++
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if library.Mode == "archive" {
				return nil
			}
			// A library root is a container, not a work. Keeping that boundary
			// explicit also prevents root-level images from absorbing every nested
			// folder into one reader manifest.
			if current == library.Root {
				return nil
			}
			item, ok, itemErr := imageFolderCandidate(library, current)
			if itemErr != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", safeRelative(library.Root, current), itemErr))
				skipped++
				return nil
			}
			if ok {
				items = append(items, item)
			}
			return nil
		}
		if library.Mode == "image-folder" {
			return nil
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		sourceKind, ok := archiveExtensions[extension]
		if !ok {
			return nil
		}
		item, itemErr := archiveCandidate(library, current, sourceKind)
		if itemErr != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", safeRelative(library.Root, current), itemErr))
			skipped++
			return nil
		}
		if item.PageCount == 0 {
			skipped++
			return nil
		}
		items = append(items, item)
		return nil
	})
	if err != nil {
		return nil, skipped, warnings, fmt.Errorf("scan library %q: %w", library.Key, err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].RelativePath < items[j].RelativePath })
	return items, skipped, warnings, nil
}

func archiveCandidate(library LibraryConfig, filename string, sourceKind string) (candidate, error) {
	reader, err := zip.OpenReader(filename)
	if err != nil {
		return candidate{}, err
	}
	defer reader.Close()
	if len(reader.File) > 100000 {
		return candidate{}, errors.New("archive has too many entries")
	}
	pageCount := 0
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || !safeArchiveName(file.Name) {
			continue
		}
		if imageExtensions[strings.ToLower(filepath.Ext(file.Name))] {
			pageCount++
		}
	}
	info, err := os.Stat(filename)
	if err != nil {
		return candidate{}, err
	}
	relative := safeRelative(library.Root, filename)
	candidateType, identityType := catalogTypes(library, sourceKind)
	coverKind := "archive"
	if sourceKind == "ebook" {
		coverKind = "ebook"
	}
	return candidate{
		ID: stableID("candidate", library.Key, relative), IdentityID: stableID("identity", library.Key, relative),
		LibraryKey: library.Key, LibraryName: library.Name, CandidateType: candidateType, IdentityType: identityType,
		SourceKind: sourceKind, Title: titleFromPath(filename), AbsolutePath: filename, RelativePath: relative,
		ParentRelativePath: slashPath(filepath.Dir(filepath.FromSlash(relative))), SizeBytes: info.Size(),
		ModifiedUTC: info.ModTime().UTC().Format(time.RFC3339Nano), Extension: strings.ToLower(filepath.Ext(filename)),
		PageCount: pageCount, CoverKind: coverKind, CoverSourcePath: filename, CoverSourceRelative: relative,
		RequiresExtraction: true,
	}, nil
}

func imageFolderCandidate(library LibraryConfig, directory string) (candidate, bool, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return candidate{}, false, err
	}
	images := []imageEntry{}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if !imageExtensions[extension] {
			continue
		}
		filename := filepath.Join(directory, entry.Name())
		info, statErr := entry.Info()
		if statErr != nil {
			return candidate{}, false, statErr
		}
		images = append(images, imageEntry{
			AbsolutePath: filename, RelativePath: safeRelative(library.Root, filename), SizeBytes: info.Size(),
			ModifiedUTC: info.ModTime().UTC().Format(time.RFC3339Nano), Extension: extension,
		})
	}
	if len(images) == 0 {
		return candidate{}, false, nil
	}
	sort.Slice(images, func(i, j int) bool { return naturalLess(images[i].RelativePath, images[j].RelativePath) })
	info, err := os.Stat(directory)
	if err != nil {
		return candidate{}, false, err
	}
	relative := safeRelative(library.Root, directory)
	if relative == "." || relative == "" {
		relative = filepath.Base(directory)
	}
	candidateType, identityType := catalogTypes(library, "image_folder")
	return candidate{
		ID: stableID("candidate", library.Key, relative), IdentityID: stableID("identity", library.Key, relative),
		LibraryKey: library.Key, LibraryName: library.Name, CandidateType: candidateType, IdentityType: identityType,
		SourceKind: "image_folder", Title: filepath.Base(directory), AbsolutePath: directory, RelativePath: relative,
		ParentRelativePath: slashPath(filepath.Dir(filepath.FromSlash(relative))), ModifiedUTC: info.ModTime().UTC().Format(time.RFC3339Nano),
		PageCount: len(images), CoverKind: "page_image", CoverSourcePath: images[0].AbsolutePath,
		CoverSourceRelative: images[0].RelativePath, ImageEntries: images,
	}, true, nil
}

func catalogTypes(library LibraryConfig, sourceKind string) (string, string) {
	if library.CatalogKind == "standalone" {
		return "doujin", "doujin"
	}
	if sourceKind == "image_folder" {
		return "manga_image_folder", "manga_image_folder"
	}
	return "manga_file", "manga_file"
}

func replaceLibraryCatalog(ctx context.Context, tx *sql.Tx, library LibraryConfig, items []candidate) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO libraries (key, name, root_path, mode, scanned_record_count)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT(key) DO UPDATE SET name=excluded.name, root_path=excluded.root_path,
		mode=excluded.mode, scanned_record_count=excluded.scanned_record_count`, library.Key, library.Name, library.Root, library.Mode, len(items)); err != nil {
		return fmt.Errorf("upsert library %q: %w", library.Key, err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE work_identities SET current_candidate_id=NULL, match_status='orphaned', updated_at=? WHERE library_key=?`, now, library.Key); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE work_identity_path_history SET active=0 WHERE library_key=?`, library.Key); err != nil {
		return err
	}
	for _, statement := range []string{
		`DELETE FROM scan_entries WHERE library_key=?`, `DELETE FROM page_counts WHERE library_key=?`,
		`DELETE FROM work_cover_candidates WHERE library_key=?`, `DELETE FROM series_items WHERE library_key=?`,
		`DELETE FROM series_groups WHERE library_key=?`, `DELETE FROM work_candidates WHERE library_key=?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, library.Key); err != nil {
			return fmt.Errorf("reset library %q catalog: %w", library.Key, err)
		}
	}
	for _, item := range items {
		if err := insertCandidate(ctx, tx, item, now); err != nil {
			return err
		}
	}
	return nil
}

func insertCandidate(ctx context.Context, tx *sql.Tx, item candidate, now string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO work_candidates (
		candidate_id, library_key, library_name, candidate_type, source_kind, title, root, path,
		relative_path, parent_relative_path, size_bytes, modified_utc, extension, page_file_count, confidence, notes
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'high', '')`,
		item.ID, item.LibraryKey, item.LibraryName, item.CandidateType, item.SourceKind, item.Title,
		filepath.Dir(item.AbsolutePath), item.AbsolutePath, item.RelativePath, item.ParentRelativePath,
		item.SizeBytes, item.ModifiedUTC, item.Extension, item.PageCount); err != nil {
		return fmt.Errorf("insert %q: %w", item.RelativePath, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO page_counts (
		candidate_id, library_key, candidate_type, source_kind, title, path, extension,
		page_count_status, readable_page_count, total_entry_count, reason, elapsed_ms
	) VALUES (?, ?, ?, ?, ?, ?, ?, 'counted', ?, ?, 'core_scan', '0')`,
		item.ID, item.LibraryKey, item.CandidateType, item.SourceKind, item.Title, item.AbsolutePath,
		item.Extension, item.PageCount, item.PageCount); err != nil {
		return err
	}
	requiresExtraction := 0
	coverStatus := "ready"
	if item.RequiresExtraction {
		requiresExtraction = 1
		coverStatus = "needs_extraction"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO work_cover_candidates (
		candidate_id, library_key, candidate_type, source_kind, title, cover_status, cover_kind,
		cover_source_path, cover_source_relative_path, requires_extraction, confidence, reason, cover_sort_key
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'high', 'core_scan', '')`,
		item.ID, item.LibraryKey, item.CandidateType, item.SourceKind, item.Title, coverStatus, item.CoverKind,
		item.CoverSourcePath, item.CoverSourceRelative, requiresExtraction); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO work_identities (
		work_identity_id, library_key, current_candidate_id, identity_type, display_title,
		canonical_relative_path, match_status, identity_version, first_seen_at, last_seen_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, 'matched', ?, ?, ?, ?)
	ON CONFLICT(work_identity_id) DO UPDATE SET current_candidate_id=excluded.current_candidate_id,
		display_title=excluded.display_title, canonical_relative_path=excluded.canonical_relative_path,
		match_status='matched', last_seen_at=excluded.last_seen_at, updated_at=excluded.updated_at`,
		item.IdentityID, item.LibraryKey, item.ID, item.IdentityType, item.Title, item.RelativePath,
		scannerVersion, now, now, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO work_identity_path_history (
		work_identity_id, library_key, candidate_id, relative_path, path_fingerprint, active, first_seen_at, last_seen_at
	) VALUES (?, ?, ?, ?, ?, 1, ?, ?)
	ON CONFLICT(work_identity_id, library_key, relative_path) DO UPDATE SET candidate_id=excluded.candidate_id,
		active=1, last_seen_at=excluded.last_seen_at`, item.IdentityID, item.LibraryKey, item.ID,
		item.RelativePath, stableID("path", item.LibraryKey, item.RelativePath), now, now); err != nil {
		return err
	}
	for _, image := range item.ImageEntries {
		if _, err := tx.ExecContext(ctx, `INSERT INTO scan_entries (
			stable_key, library_key, library_name, root, path, relative_path, entry_type, item_kind,
			status, reason, size_bytes, modified_utc, extension, page_file_count
		) VALUES (?, ?, ?, ?, ?, ?, 'file', 'image_file', 'indexed_as_page', 'core_scan', ?, ?, ?, 1)`,
			stableID("page", item.LibraryKey, image.RelativePath), item.LibraryKey, item.LibraryName,
			filepath.Dir(item.AbsolutePath), image.AbsolutePath, image.RelativePath, image.SizeBytes,
			image.ModifiedUTC, image.Extension); err != nil {
			return err
		}
	}
	return nil
}

func ensureSchema(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS libraries (key TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', root_path TEXT NOT NULL DEFAULT '', mode TEXT NOT NULL DEFAULT '', scanned_record_count INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS scan_entries (stable_key TEXT PRIMARY KEY, library_key TEXT NOT NULL, library_name TEXT NOT NULL DEFAULT '', root TEXT NOT NULL DEFAULT '', path TEXT NOT NULL, relative_path TEXT NOT NULL, entry_type TEXT NOT NULL, item_kind TEXT NOT NULL, status TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', size_bytes INTEGER, modified_utc TEXT NOT NULL DEFAULT '', extension TEXT NOT NULL DEFAULT '', page_file_count INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX IF NOT EXISTS idx_scan_entries_library_path ON scan_entries (library_key, relative_path)`,
		`CREATE TABLE IF NOT EXISTS work_candidates (candidate_id TEXT PRIMARY KEY, library_key TEXT NOT NULL, library_name TEXT NOT NULL DEFAULT '', candidate_type TEXT NOT NULL, source_kind TEXT NOT NULL, title TEXT NOT NULL, root TEXT NOT NULL DEFAULT '', path TEXT NOT NULL, relative_path TEXT NOT NULL, parent_relative_path TEXT NOT NULL DEFAULT '', source_record_id TEXT NOT NULL DEFAULT '', source_status TEXT NOT NULL DEFAULT '', source_reason TEXT NOT NULL DEFAULT '', size_bytes INTEGER NOT NULL DEFAULT 0, modified_utc TEXT NOT NULL DEFAULT '', extension TEXT NOT NULL DEFAULT '', page_file_count INTEGER NOT NULL DEFAULT 0, confidence TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '')`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_work_candidates_library_relative ON work_candidates (library_key, relative_path)`,
		`CREATE TABLE IF NOT EXISTS translation_items (candidate_id TEXT NOT NULL, translation_group TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS page_counts (candidate_id TEXT PRIMARY KEY, library_key TEXT NOT NULL, candidate_type TEXT NOT NULL DEFAULT '', source_kind TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '', path TEXT NOT NULL DEFAULT '', extension TEXT NOT NULL DEFAULT '', page_count_status TEXT NOT NULL DEFAULT '', readable_page_count INTEGER NOT NULL DEFAULT 0, total_entry_count INTEGER NOT NULL DEFAULT 0, reason TEXT NOT NULL DEFAULT '', elapsed_ms INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS work_cover_candidates (candidate_id TEXT PRIMARY KEY, library_key TEXT NOT NULL, candidate_type TEXT NOT NULL DEFAULT '', source_kind TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '', cover_status TEXT NOT NULL DEFAULT '', cover_kind TEXT NOT NULL DEFAULT '', cover_source_path TEXT NOT NULL DEFAULT '', cover_source_relative_path TEXT NOT NULL DEFAULT '', cover_source_record_id TEXT NOT NULL DEFAULT '', requires_extraction INTEGER NOT NULL DEFAULT 0, confidence TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '', cover_sort_key TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS series_groups (group_id TEXT PRIMARY KEY, library_key TEXT NOT NULL, series_title TEXT NOT NULL DEFAULT '', group_path TEXT NOT NULL DEFAULT '', group_type TEXT NOT NULL DEFAULT '', candidate_count INTEGER NOT NULL DEFAULT 0, confidence TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS series_items (group_id TEXT NOT NULL, library_key TEXT NOT NULL, series_title TEXT NOT NULL DEFAULT '', candidate_id TEXT NOT NULL, candidate_type TEXT NOT NULL DEFAULT '', source_kind TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '', item_role TEXT NOT NULL DEFAULT '', sequence_number INTEGER, sort_key TEXT NOT NULL DEFAULT '', relative_path TEXT NOT NULL DEFAULT '', parent_relative_path TEXT NOT NULL DEFAULT '', page_file_count INTEGER NOT NULL DEFAULT 0, confidence TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS idx_series_items_candidate ON series_items (candidate_id)`,
		`CREATE TABLE IF NOT EXISTS series_cover_candidates (group_id TEXT PRIMARY KEY, library_key TEXT NOT NULL DEFAULT '', series_title TEXT NOT NULL DEFAULT '', selected_candidate_id TEXT NOT NULL DEFAULT '', selected_title TEXT NOT NULL DEFAULT '', cover_status TEXT NOT NULL DEFAULT '', cover_kind TEXT NOT NULL DEFAULT '', cover_source_path TEXT NOT NULL DEFAULT '', cover_source_relative_path TEXT NOT NULL DEFAULT '', requires_extraction INTEGER NOT NULL DEFAULT 0, confidence TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS work_identities (work_identity_id TEXT PRIMARY KEY, library_key TEXT NOT NULL, current_candidate_id TEXT, identity_type TEXT NOT NULL, display_title TEXT NOT NULL, canonical_relative_path TEXT NOT NULL DEFAULT '', match_status TEXT NOT NULL DEFAULT 'matched', identity_version TEXT NOT NULL, first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_work_identities_candidate ON work_identities (current_candidate_id)`,
		`CREATE TABLE IF NOT EXISTS work_identity_path_history (id INTEGER PRIMARY KEY AUTOINCREMENT, work_identity_id TEXT NOT NULL, library_key TEXT NOT NULL, candidate_id TEXT, relative_path TEXT NOT NULL, path_fingerprint TEXT NOT NULL DEFAULT '', active INTEGER NOT NULL DEFAULT 1, first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, UNIQUE (work_identity_id, library_key, relative_path))`,
		`CREATE TABLE IF NOT EXISTS series_identities (series_identity_id TEXT PRIMARY KEY, library_key TEXT NOT NULL, current_group_id TEXT, identity_type TEXT NOT NULL, display_title TEXT NOT NULL, canonical_group_path TEXT NOT NULL DEFAULT '', match_status TEXT NOT NULL DEFAULT 'matched', identity_version TEXT NOT NULL, first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS local_search_index (target_type TEXT NOT NULL, target_id TEXT NOT NULL, library_key TEXT NOT NULL DEFAULT '', search_text TEXT NOT NULL, index_version TEXT NOT NULL, source_hash TEXT NOT NULL, indexed_at TEXT NOT NULL, PRIMARY KEY (target_type, target_id))`,
		`CREATE TABLE IF NOT EXISTS cover_assets (stable_key TEXT PRIMARY KEY, candidate_id TEXT NOT NULL DEFAULT '', work_candidate_id TEXT NOT NULL DEFAULT '', cache_path TEXT NOT NULL DEFAULT '', source_path TEXT NOT NULL DEFAULT '', source_inner_path TEXT NOT NULL DEFAULT '', asset_kind TEXT NOT NULL DEFAULT '', mime_type TEXT NOT NULL DEFAULT '', width INTEGER NOT NULL DEFAULT 0, height INTEGER NOT NULL DEFAULT 0, size_bytes INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS cover_image_hashes (stable_key TEXT PRIMARY KEY, candidate_id TEXT NOT NULL, work_identity_id TEXT NOT NULL DEFAULT '', cache_path TEXT NOT NULL DEFAULT '', hash_version TEXT NOT NULL DEFAULT '', average_hash TEXT NOT NULL DEFAULT '', difference_hash TEXT NOT NULL DEFAULT '', perceptual_hash TEXT NOT NULL DEFAULT '', width INTEGER NOT NULL DEFAULT 0, height INTEGER NOT NULL DEFAULT 0, cache_size_bytes INTEGER NOT NULL DEFAULT 0, cache_mtime_ns INTEGER NOT NULL DEFAULT 0, computed_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '')`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize catalog schema: %w", err)
		}
	}
	return nil
}

func stableID(kind string, libraryKey string, relative string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + libraryKey + "\x00" + slashPath(relative)))
	return kind + "-" + hex.EncodeToString(sum[:])
}

func safeRelative(root string, value string) string {
	relative, err := filepath.Rel(root, value)
	if err != nil {
		return filepath.Base(value)
	}
	return slashPath(relative)
}

func slashPath(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	value = path.Clean(value)
	if value == "." {
		return ""
	}
	return strings.TrimPrefix(value, "./")
}

func pathWithin(root string, value string) bool {
	relative, err := filepath.Rel(root, value)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func resolvePathWithExistingAncestor(value string) (string, error) {
	current := filepath.Clean(value)
	suffix := []string{}
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func safeArchiveName(value string) bool {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	if value == "" || strings.HasPrefix(value, "/") {
		return false
	}
	cleaned := path.Clean(value)
	return cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../") && !strings.Contains(cleaned, ":")
}

func titleFromPath(value string) string {
	name := filepath.Base(value)
	return strings.TrimSpace(strings.TrimSuffix(name, filepath.Ext(name)))
}

func naturalLess(left string, right string) bool {
	return strings.ToLower(left) < strings.ToLower(right)
}
