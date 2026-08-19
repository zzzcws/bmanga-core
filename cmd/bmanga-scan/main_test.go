package main

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/zzzcws/bmanga-core/internal/catalog"
	"github.com/zzzcws/bmanga-core/internal/prototype"

	_ "modernc.org/sqlite"
)

func TestRunCreatesReadableCatalogFromSyntheticLibrary(t *testing.T) {
	root := t.TempDir()
	libraryRoot := filepath.Join(root, "library")
	folderRoot := filepath.Join(libraryRoot, "Folder Book")
	if err := os.MkdirAll(folderRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	firstImage := pngFixture(t, color.RGBA{R: 0x33, G: 0x66, B: 0x99, A: 0xff})
	secondImage := pngFixture(t, color.RGBA{R: 0x99, G: 0x66, B: 0x33, A: 0xff})
	if err := os.WriteFile(filepath.Join(folderRoot, "001.png"), firstImage, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folderRoot, "002.png"), secondImage, 0o644); err != nil {
		t.Fatal(err)
	}
	createArchiveFixture(t, filepath.Join(libraryRoot, "Archive Book.cbz"), map[string][]byte{
		"001.png": firstImage,
		"002.png": secondImage,
	})

	databasePath := filepath.Join(root, "data", "bmanga.sqlite")
	configPath := filepath.Join(root, "libraries.json")
	configBody, err := json.Marshal(catalog.Config{
		Database: databasePath,
		Libraries: []catalog.LibraryConfig{{
			Key:         "demo",
			Name:        "Demo library",
			Root:        libraryRoot,
			Mode:        "mixed",
			CatalogKind: "standalone",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, configBody, 0o600); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := run(context.Background(), configPath, &output); err != nil {
		t.Fatal(err)
	}
	var summary catalog.Summary
	if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Works != 2 || summary.Pages != 4 {
		t.Fatalf("summary = %#v, want 2 works / 4 pages", summary)
	}

	server, err := prototype.NewServer(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	works := httptest.NewRecorder()
	server.Routes().ServeHTTP(
		works,
		httptest.NewRequest(http.MethodGet, "/api/works?limit=18", nil),
	)
	if works.Code != http.StatusOK {
		t.Fatalf("works status = %d; body = %s", works.Code, works.Body.String())
	}
	var workPayload map[string]any
	if err := json.Unmarshal(works.Body.Bytes(), &workPayload); err != nil {
		t.Fatal(err)
	}
	items, ok := workPayload["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("works payload = %#v; body = %s", workPayload, works.Body.String())
	}

	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var archiveID string
	if err := db.QueryRow(
		"SELECT candidate_id FROM work_candidates WHERE source_kind = 'archive' LIMIT 1",
	).Scan(&archiveID); err != nil {
		t.Fatal(err)
	}
	var folderID string
	if err := db.QueryRow(
		"SELECT candidate_id FROM work_candidates WHERE source_kind = 'image_folder' LIMIT 1",
	).Scan(&folderID); err != nil {
		t.Fatal(err)
	}
	var unexpectedCoverAssetColumn int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('cover_assets')
		WHERE name = 'source_relative_path'
	`).Scan(&unexpectedCoverAssetColumn); err != nil {
		t.Fatal(err)
	}
	if unexpectedCoverAssetColumn != 0 {
		t.Fatalf("cover_assets source_relative_path columns = %d, want 0", unexpectedCoverAssetColumn)
	}
	if _, err := db.Exec(
		"INSERT INTO translation_items (candidate_id, translation_group) VALUES (?, ?)",
		folderID,
		"Synthetic translation group",
	); err != nil {
		t.Fatal(err)
	}

	detail := httptest.NewRecorder()
	server.Routes().ServeHTTP(
		detail,
		httptest.NewRequest(http.MethodGet, "/api/work?id="+folderID, nil),
	)
	if detail.Code != http.StatusOK {
		t.Fatalf("work detail status = %d; body = %s", detail.Code, detail.Body.String())
	}
	var detailPayload map[string]any
	if err := json.Unmarshal(detail.Body.Bytes(), &detailPayload); err != nil {
		t.Fatal(err)
	}
	translations, ok := detailPayload["translations"].([]any)
	if !ok || len(translations) != 1 {
		t.Fatalf("work detail translations = %#v; body = %s", detailPayload["translations"], detail.Body.String())
	}
	translation, ok := translations[0].(map[string]any)
	if !ok {
		t.Fatalf("work detail translation = %#v, want object", translations[0])
	}
	if len(translation) != 1 || translation["translation_group"] != "Synthetic translation group" {
		t.Fatalf("work detail translation = %#v, want only translation_group", translation)
	}

	cover := httptest.NewRecorder()
	server.Routes().ServeHTTP(
		cover,
		httptest.NewRequest(http.MethodGet, "/cover?id="+folderID+"&size=640", nil),
	)
	if cover.Code != http.StatusOK {
		t.Fatalf("cover status = %d; body = %s", cover.Code, cover.Body.String())
	}
	if contentType := cover.Header().Get("Content-Type"); contentType != "image/jpeg" {
		t.Fatalf("cover content type = %q, want image/jpeg", contentType)
	}
	if cover.Body.Len() == 0 {
		t.Fatal("cover body is empty")
	}

	pages := httptest.NewRecorder()
	server.Routes().ServeHTTP(
		pages,
		httptest.NewRequest(http.MethodGet, "/api/pages?id="+archiveID, nil),
	)
	if pages.Code != http.StatusOK {
		t.Fatalf("pages status = %d; body = %s", pages.Code, pages.Body.String())
	}
	var pagePayload map[string]any
	if err := json.Unmarshal(pages.Body.Bytes(), &pagePayload); err != nil {
		t.Fatal(err)
	}
	if pagePayload["readable"] != true || pagePayload["count"] != float64(2) {
		t.Fatalf("pages = %#v; body = %s", pagePayload, pages.Body.String())
	}

	page := httptest.NewRecorder()
	server.Routes().ServeHTTP(
		page,
		httptest.NewRequest(
			http.MethodGet,
			"/page?id="+archiveID+"&index=0&max=640",
			nil,
		),
	)
	if page.Code != http.StatusOK {
		t.Fatalf("page status = %d; body = %s", page.Code, page.Body.String())
	}
	if contentType := page.Header().Get("Content-Type"); contentType != "image/jpeg" {
		t.Fatalf("page content type = %q, want image/jpeg", contentType)
	}
	if page.Body.Len() == 0 {
		t.Fatal("page body is empty")
	}
}

func pngFixture(t *testing.T, fill color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetRGBA(x, y, fill)
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func createArchiveFixture(t *testing.T, filename string, files map[string][]byte) {
	t.Helper()
	handle, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(handle)
	for name, body := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
}
