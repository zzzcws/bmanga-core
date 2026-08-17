package catalog

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	_ "modernc.org/sqlite"
)

func TestScanBuildsStableCatalogWithoutChangingSources(t *testing.T) {
	root := t.TempDir()
	libraryRoot := filepath.Join(root, "library")
	imageFolder := filepath.Join(libraryRoot, "Folder Book")
	if err := os.MkdirAll(imageFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(imageFolder, "001.jpg")
	if err := os.WriteFile(imagePath, []byte("synthetic image fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imageFolder, "notes.txt"), []byte("unsupported sidecar"), 0o644); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(libraryRoot, "Archive Book.cbz")
	createZipFixture(t, archivePath, map[string]string{"001.png": "one", "002.webp": "two", "../unsafe.jpg": "ignored"})
	ignoredDirectory := filepath.Join(libraryRoot, ".cache")
	if err := os.MkdirAll(ignoredDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ignoredDirectory, "scanner-must-not-touch.bin"), []byte{0x00, 0xff, 0x42}, 0o644); err != nil {
		t.Fatal(err)
	}
	wantLibraryTree := snapshotLibraryTree(t, libraryRoot)
	databasePath := filepath.Join(root, "data", "catalog.sqlite")
	configPath := filepath.Join(root, "libraries.json")
	configJSON, err := json.Marshal(Config{
		Database:  databasePath,
		Libraries: []LibraryConfig{{Key: "demo", Name: "Demo", Root: libraryRoot, Mode: "mixed", CatalogKind: "standalone"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, configJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Scan(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if first.Works != 2 || first.Pages != 3 {
		t.Fatalf("first summary = %#v", first)
	}
	assertLibraryTreeUnchanged(t, wantLibraryTree, snapshotLibraryTree(t, libraryRoot))
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var firstIDs string
	if err := db.QueryRow(`SELECT GROUP_CONCAT(candidate_id, ',') FROM (SELECT candidate_id FROM work_candidates ORDER BY candidate_id)`).Scan(&firstIDs); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE private_reader_state (value TEXT NOT NULL); INSERT INTO private_reader_state VALUES ('preserve-me')`); err != nil {
		t.Fatal(err)
	}
	second, err := Scan(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if second.Works != first.Works || second.Pages != first.Pages {
		t.Fatalf("second summary = %#v; first = %#v", second, first)
	}
	var secondIDs string
	if err := db.QueryRow(`SELECT GROUP_CONCAT(candidate_id, ',') FROM (SELECT candidate_id FROM work_candidates ORDER BY candidate_id)`).Scan(&secondIDs); err != nil {
		t.Fatal(err)
	}
	if secondIDs != firstIDs {
		t.Fatalf("candidate IDs changed: %q -> %q", firstIDs, secondIDs)
	}
	var privateState string
	if err := db.QueryRow(`SELECT value FROM private_reader_state`).Scan(&privateState); err != nil || privateState != "preserve-me" {
		t.Fatalf("private state = %q, %v", privateState, err)
	}
	assertLibraryTreeUnchanged(t, wantLibraryTree, snapshotLibraryTree(t, libraryRoot))
}

type libraryTreeEntry struct {
	EntryType     string
	SizeBytes     int64
	ContentSHA256 string
}

func snapshotLibraryTree(t *testing.T, root string) map[string]libraryTreeEntry {
	t.Helper()
	tree := make(map[string]libraryTreeEntry)
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		record := libraryTreeEntry{EntryType: libraryEntryType(info), SizeBytes: info.Size()}
		switch {
		case info.Mode().IsRegular():
			handle, err := os.Open(current)
			if err != nil {
				return err
			}
			hasher := sha256.New()
			_, copyErr := io.Copy(hasher, handle)
			closeErr := handle.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			record.ContentSHA256 = fmt.Sprintf("%x", hasher.Sum(nil))
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(current)
			if err != nil {
				return err
			}
			sum := sha256.Sum256([]byte(target))
			record.ContentSHA256 = fmt.Sprintf("%x", sum)
		}
		tree[filepath.ToSlash(relative)] = record
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot library tree: %v", err)
	}
	return tree
}

func libraryEntryType(info os.FileInfo) string {
	switch {
	case info.IsDir():
		return "directory"
	case info.Mode().IsRegular():
		return "regular-file"
	case info.Mode()&os.ModeSymlink != 0:
		return "symlink"
	default:
		return "other:" + info.Mode().Type().String()
	}
}

func assertLibraryTreeUnchanged(t *testing.T, before map[string]libraryTreeEntry, after map[string]libraryTreeEntry) {
	t.Helper()
	for _, relative := range sortedLibraryPaths(before) {
		want := before[relative]
		got, exists := after[relative]
		if !exists {
			t.Errorf("source path %q was deleted, renamed, or moved", relative)
			continue
		}
		if got.EntryType != want.EntryType {
			t.Errorf("source path %q type changed from %q to %q", relative, want.EntryType, got.EntryType)
		}
		if got.SizeBytes != want.SizeBytes {
			t.Errorf("source path %q size changed from %d to %d bytes (source overwrite)", relative, want.SizeBytes, got.SizeBytes)
		}
		if got.ContentSHA256 != want.ContentSHA256 {
			t.Errorf("source path %q content SHA-256 changed from %q to %q (source overwrite)", relative, want.ContentSHA256, got.ContentSHA256)
		}
	}
	for _, relative := range sortedLibraryPaths(after) {
		if _, existed := before[relative]; !existed {
			t.Errorf("source path %q was added (source write or rename/move target)", relative)
		}
	}
}

func sortedLibraryPaths(tree map[string]libraryTreeEntry) []string {
	paths := make([]string, 0, len(tree))
	for relative := range tree {
		paths = append(paths, relative)
	}
	sort.Strings(paths)
	return paths
}

func TestLoadConfigRejectsUnsafeShape(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "bad.json")
	if err := os.WriteFile(configPath, []byte(`{"database":"catalog.sqlite","libraries":[{"key":"Bad Key","root":".","mode":"mixed"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(configPath); err == nil {
		t.Fatal("invalid library key was accepted")
	}
}

func TestLoadConfigRejectsDatabaseInsideLibraryRoot(t *testing.T) {
	root := t.TempDir()
	libraryRoot := filepath.Join(root, "library")
	if err := os.MkdirAll(libraryRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "libraries.json")
	configJSON, err := json.Marshal(Config{
		Database: filepath.Join(libraryRoot, "catalog.sqlite"),
		Libraries: []LibraryConfig{{
			Key: "demo", Root: libraryRoot, Mode: "mixed", CatalogKind: "standalone",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, configJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(configPath); err == nil {
		t.Fatal("database inside the library root was accepted")
	}
}

func createZipFixture(t *testing.T, filename string, files map[string]string) {
	t.Helper()
	handle, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(handle)
	for name, body := range files {
		entry, createErr := writer.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write([]byte(body)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
}
