package prototype

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWidthResizeWorkingBudgetBeforeDecodeAndAllocation(t *testing.T) {
	s := &Server{archiveLimits: loadArchiveResourceLimits()}
	cachePath := filepath.Join(t.TempDir(), "must-not-exist.jpg")
	// This is only a tiny PNG header, not an 80-million-pixel allocation.
	// It passes the existing input-pixel ceiling but width scaling at 1800
	// would require more than 2 GB of CatmullRom scratch storage alone.
	header := testPNGWithDimensions(2000, 40000)
	if err := s.validateArchiveImageData(header); err != nil {
		t.Fatalf("fixture must pass the source pixel limit: %v", err)
	}
	if widthResizeWithinWorkingBudget(image.Config{Width: 2000, Height: 40000}, 1800) {
		t.Fatal("oversized working set was accepted")
	}
	if !widthResizeWithinWorkingBudget(image.Config{Width: 1200, Height: 2400}, 600) {
		t.Fatal("ordinary width derivative should remain supported")
	}
	built, err := s.ensureThumbnailBytesToPathCachedWithResize(context.Background(), header, cachePath, widthResize(1800))
	if built || !errors.Is(err, errWidthResizeWorkingBudget) {
		t.Fatalf("expected budget rejection before PNG pixel decode, got %v, %v", built, err)
	}
	source := filepath.Join(t.TempDir(), "synthetic-header.png")
	if err := os.WriteFile(source, header, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.writeThumbnailWithResize(source, cachePath, widthResize(1800)); !errors.Is(err, errWidthResizeWorkingBudget) {
		t.Fatalf("file decoder did not stop at the header: %v", err)
	}
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("budget rejection left a cache artifact: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.ensureThumbnailBytesToPathCachedWithResize(ctx, header, cachePath, widthResize(1800)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request proceeded to render preflight: %v", err)
	}
}

func TestWidthBudgetCannotBypassInputLimits(t *testing.T) {
	s := &Server{archiveLimits: loadArchiveResourceLimits()}
	for _, width := range []int{1200, 3200} {
		// The input is over the existing pixel cap even when its width fits.
		header := testPNGWithDimensions(2000, 60000)
		rec := httptest.NewRecorder()
		if !s.sendThumbnailBytesWithResize(rec, httptest.NewRequest(http.MethodGet, "/page", nil), header, "image/png", "synthetic", time.Time{}, widthResize(width)) || rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("pixel limit fell back to source: handled status=%d", rec.Code)
		}
		if rec.Header().Get("X-Bmanga-Image-Mode") == "source" {
			t.Fatal("rejected pixel bomb was marked source-safe")
		}
		if _, err := s.ensureThumbnailBytesToPathCachedWithResize(context.Background(), header, filepath.Join(t.TempDir(), "invalid.jpg"), widthResize(width)); !errors.Is(err, errArchiveResourceLimit) {
			t.Fatalf("direct cache path bypassed input-pixel guard: %v", err)
		}
	}
	s.archiveLimits.maxPageBytes = 8
	rec := httptest.NewRecorder()
	if !s.sendThumbnailBytesWithResize(rec, httptest.NewRequest(http.MethodGet, "/page", nil), testPNGWithDimensions(2, 2), "image/png", "synthetic", time.Time{}, widthResize(10)) || rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("input-byte limit fell back to source: %d", rec.Code)
	}
}

func TestWidthBudgetReturnsOriginalValidatedSources(t *testing.T) {
	// Four million source pixels is a bounded fixture. Scaling only slightly
	// narrower would still exceed the scratch budget; serving its encoded PNG
	// preserves every source pixel without a server-side full-image decode.
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewNRGBA(image.Rect(0, 0, 512, 8192))); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()
	root := t.TempDir()
	s, err := newServerWithoutCatalogForTest(filepath.Join(root, "synthetic.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec("CREATE TABLE libraries(root_path TEXT); INSERT INTO libraries(root_path) VALUES(?)", root); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "synthetic.png")
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "synthetic.cbz")
	writeArchiveEntries(t, archive, map[string][]byte{"synthetic.png": data})
	for _, kind := range []string{"file", "bytes", "archive"} {
		t.Run(kind, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/page?max_width=500", nil)
			switch kind {
			case "file":
				s.servePageImageFile(rec, req, source, "image/png", 0, 500)
			case "bytes":
				s.serveImageDataWithResize(rec, req, data, "image/png", "synthetic", time.Time{}, widthResize(500))
			case "archive":
				s.sendArchivePage(rec, req, archive, "synthetic.png", int64(len(data)), ".png", 0, 500)
			}
			if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || !bytes.Equal(rec.Body.Bytes(), data) {
				t.Fatalf("budget fallback altered source quality/MIME: %d %q", rec.Code, rec.Header().Get("Content-Type"))
			}
		})
	}
	files, err := filepath.Glob(filepath.Join(s.thumbnailCacheRoot, "*.jpg"))
	if err != nil || len(files) != 0 {
		t.Fatalf("over-budget derivatives were cached: %v %v", files, err)
	}
}

func TestPageImageResizeParametersAreBoundedAndPreferWidth(t *testing.T) {
	for _, tc := range []struct {
		query       string
		edge, width int
	}{
		{"", 0, 0}, {"max=900", 900, 0},
		{"max=900&max_width=1200", 0, 1200},
		{"max=900&max_width=0", 900, 0},
		{"max=900&max_width=-1", 900, 0},
		{"max=900&max_width=broken", 900, 0},
		{"max=900&max_width=3201", 0, readerPageMaxDimension},
		{"max=999999", readerPageMaxDimension, 0},
	} {
		t.Run(tc.query, func(t *testing.T) {
			edge, width := pageImageResizeParameters(httptest.NewRequest(http.MethodGet, "/page?"+tc.query, nil))
			if edge != tc.edge || width != tc.width {
				t.Fatalf("resize = (%d, %d), want (%d, %d)", edge, width, tc.edge, tc.width)
			}
		})
	}
	if edge, width := pageImageResizeParameters(nil); edge != 0 || width != 0 {
		t.Fatalf("nil request resize = (%d, %d)", edge, width)
	}
}

func TestWidthResizePreservesLegacyCacheKeys(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.png")
	if err := os.WriteFile(path, encodedTestPNG(t, 120, 1200), 0o600); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{thumbnailCacheRoot: root}
	digest := sha1.Sum([]byte(fmt.Sprintf("%s|%s|%d|%d|%d", thumbnailCacheVersion, path, stat.Size(), stat.ModTime().UnixNano(), 60)))
	want := filepath.Join(root, hex.EncodeToString(digest[:])+".jpg")
	got, err := s.thumbnailCachePath(path, 60)
	if err != nil || got != want {
		t.Fatalf("legacy file cache changed: %q, %v", got, err)
	}
	widthPath, err := s.thumbnailCachePathWithResize(path, widthResize(60))
	if err != nil || widthPath == want {
		t.Fatalf("width cache axis collided: %q, %v", widthPath, err)
	}
	digest = sha1.Sum([]byte(fmt.Sprintf("%s|bytes|%s|%s|%d|%d|%d", thumbnailCacheVersion, path, "image/png", stat.Size(), stat.ModTime().UnixNano(), 60)))
	want = filepath.Join(root, hex.EncodeToString(digest[:])+".jpg")
	if got := s.thumbnailBytesCachePathForSize(stat.Size(), "image/png", path, stat.ModTime(), 60); got != want {
		t.Fatalf("legacy bytes cache changed: %q", got)
	}
	if got := archivePageSourceLockKey(path, "page.png", 60); got != "archive-page-source:"+path+"|page.png|60" {
		t.Fatalf("legacy lock changed: %q", got)
	}
}

// Exercise the actual /page handler with only generated PNG/ZIP fixtures and
// an isolated in-memory-style catalog on disk. No source discovery is needed.
func TestPageMaxWidthHandlerPreservesTallLocalSources(t *testing.T) {
	for _, kind := range []string{"image_folder", "archive", "nested", "ebook"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "data"), 0o700); err != nil {
				t.Fatal(err)
			}
			s, err := newServerWithoutCatalogForTest(filepath.Join(root, "data", "test.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			library := filepath.Join(root, "library")
			if err := os.MkdirAll(library, 0o700); err != nil {
				t.Fatal(err)
			}
			page := encodedTestPNG(t, 120, 1200)
			zipData := func(name string, data []byte) []byte {
				var result bytes.Buffer
				writer := zip.NewWriter(&result)
				entry, err := writer.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write(data); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				return result.Bytes()
			}
			fileName, inner, extension, sourceKind, sourceData := "page.png", "", ".png", kind, page
			switch kind {
			case "archive":
				fileName, inner, extension, sourceData = "book.cbz", "page.png", ".cbz", zipData("page.png", page)
			case "nested":
				fileName, inner, extension, sourceKind, sourceData = "outer.zip", "inner.cbz!page.png", ".zip", "archive", zipData("inner.cbz", zipData("page.png", page))
			case "ebook":
				fileName, inner, extension, sourceData = "book.epub", "page.png", ".epub", zipData("page.png", page)
			}
			path := filepath.Join(library, fileName)
			if err := os.WriteFile(path, sourceData, 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.db.Exec(`
				CREATE TABLE libraries (key TEXT PRIMARY KEY, root_path TEXT NOT NULL);
				CREATE TABLE work_browse (candidate_id TEXT PRIMARY KEY, work_identity_id TEXT, title TEXT, source_kind TEXT, extension TEXT);
				CREATE TABLE work_identities (work_identity_id TEXT PRIMARY KEY, current_candidate_id TEXT);
				INSERT INTO libraries VALUES ('synthetic', ?1);
				INSERT INTO work_browse VALUES ('work', 'identity', 'Synthetic tall page', ?2, ?3);
				INSERT INTO work_identities VALUES ('identity', 'work');
				INSERT INTO page_manifests (page_manifest_id, work_identity_id, candidate_id, manifest_hash, page_count, source_kind, manifest_status, builder_version, built_at)
				VALUES ('manifest', 'identity', 'work', 'synthetic-hash', 1, ?4, 'ready', ?5, '2026-01-01T00:00:00Z');
				INSERT INTO page_manifest_items (page_manifest_id, page_index, library_key, source_path, source_relative_path, source_inner_path, extension, mime_type, size_bytes)
				VALUES ('manifest', 0, 'synthetic', ?6, ?7, ?8, '.png', 'image/png', ?9);
			`, library, sourceKind, extension, sourceKind, readerManifestVersion, path, fileName, inner, len(page))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.resolveSourcePath("synthetic", path, fileName); err != nil {
				t.Fatalf("synthetic source fixture resolution failed: %v", err)
			}
			rows, err := s.query("SELECT source_path, source_relative_path FROM page_manifest_items")
			if err != nil || len(rows) != 1 || stringValue(rows[0]["source_path"]) != path || stringValue(rows[0]["source_relative_path"]) != fileName {
				t.Fatalf("synthetic fixture row mismatch: %#v, %v", rows, err)
			}
			routes := s.Routes()
			for _, tc := range []struct {
				query         string
				width, height int
				format        string
				source        bool
			}{
				{"max=60", 6, 60, "jpeg", false},
				{"max=60&max_width=60", 60, 600, "jpeg", false},
				{"max_width=60", 60, 600, "jpeg", false},
				{"max_width=120", 120, 1200, "png", true},
				{"max_width=3200", 120, 1200, "png", true},
				{"", 120, 1200, "png", true},
			} {
				request := httptest.NewRequest(http.MethodGet, "/page?id=work&index=0&manifest=manifest&"+tc.query, nil)
				recorder := httptest.NewRecorder()
				routes.ServeHTTP(recorder, request)
				if recorder.Code != http.StatusOK {
					t.Fatalf("%s status = %d: %s", tc.query, recorder.Code, recorder.Body.String())
				}
				config, format, err := image.DecodeConfig(bytes.NewReader(recorder.Body.Bytes()))
				if err != nil || format != tc.format || config.Width != tc.width || config.Height != tc.height {
					t.Fatalf("%s = %s %dx%d, %v", tc.query, format, config.Width, config.Height, err)
				}
				if tc.source && !bytes.Equal(recorder.Body.Bytes(), page) {
					t.Fatalf("%s re-encoded fitting source", tc.query)
				}
				if tc.source && strings.Contains(tc.query, "max_width") && recorder.Header().Get("X-Bmanga-Image-Mode") != "source" {
					t.Fatalf("missing source mode for %s", tc.query)
				}
			}
			after, err := os.Stat(path)
			if err != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
				t.Fatal("source stat changed")
			}
			data, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(data, sourceData) {
				t.Fatal("source bytes changed")
			}
			var count int
			if err := s.db.QueryRow("SELECT COUNT(*) FROM reading_progress").Scan(&count); err != nil || count != 0 {
				t.Fatalf("reading progress mutated: %d, %v", count, err)
			}
		})
	}
}

func encodedTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	source := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, source); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return output.Bytes()
}

func TestReaderSourceQualityRequested(t *testing.T) {
	for _, testCase := range []struct {
		name string
		url  string
		want bool
	}{
		{name: "reader source", url: "/page?quality=source", want: true},
		{name: "case insensitive", url: "/page?quality=SOURCE", want: true},
		{name: "reader default", url: "/page?max=2400", want: false},
		{name: "cover cannot opt in", url: "/cover?quality=source", want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, testCase.url, nil)
			if got := readerSourceQualityRequested(request); got != testCase.want {
				t.Fatalf("readerSourceQualityRequested() = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestSourceQualityBypassesReencodeWhenImageAlreadyFits(t *testing.T) {
	data := encodedTestPNG(t, 160, 90)
	request := httptest.NewRequest(http.MethodGet, "/page?quality=source", nil)
	response := httptest.NewRecorder()

	if (&Server{}).sendThumbnailBytes(response, request, data, "image/png", "page.png", time.Now(), 3200) {
		t.Fatal("source-quality page unexpectedly generated a thumbnail")
	}
	if got := response.Header().Get("X-Bmanga-Image-Mode"); got != "source" {
		t.Fatalf("X-Bmanga-Image-Mode = %q, want source", got)
	}
}

func TestImageDimensionFitHelpers(t *testing.T) {
	data := encodedTestPNG(t, 160, 90)
	if !imageBytesWithinMaxDimension(data, 160) {
		t.Fatal("imageBytesWithinMaxDimension rejected an image at the limit")
	}
	if imageBytesWithinMaxDimension(data, 159) {
		t.Fatal("imageBytesWithinMaxDimension accepted an oversized image")
	}
	if imageBytesWithinMaxDimension([]byte("not an image"), 3200) {
		t.Fatal("imageBytesWithinMaxDimension accepted invalid image data")
	}

	path := filepath.Join(t.TempDir(), "page.png")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write image fixture: %v", err)
	}
	if !imageFileWithinMaxDimension(path, 3200) {
		t.Fatal("imageFileWithinMaxDimension rejected a valid image")
	}
	if imageFileWithinMaxDimension(path, 80) {
		t.Fatal("imageFileWithinMaxDimension accepted an oversized image")
	}
}

func TestMaxWidthReturnsSourceEncodingWhenWidthAlreadyFits(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := NewServer(filepath.Join(root, "data", "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	data := encodedTestPNG(t, 120, 1200)
	request := httptest.NewRequest(http.MethodGet, "/page?id=work&index=0&max_width=120", nil)
	response := httptest.NewRecorder()
	s.serveImageDataWithResize(response, request, data, "image/png", "tall-page.png", time.Unix(1, 0), widthResize(120))

	if response.Code != http.StatusOK {
		t.Fatalf("source-width response = %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want source image/png", got)
	}
	if got := response.Header().Get("X-Bmanga-Image-Mode"); got != "source" {
		t.Fatalf("X-Bmanga-Image-Mode = %q, want source", got)
	}
	if !bytes.Equal(response.Body.Bytes(), data) {
		t.Fatal("width-fitting page was re-encoded instead of returning source bytes")
	}
}

func TestTallArchivePageMaxWidthKeepsReadableWidthAndSeparatesCacheAxis(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "tall.zip")
	pageData := encodedTestPNG(t, 120, 1200)
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	archiveWriter := zip.NewWriter(archiveFile)
	entry, err := archiveWriter.Create("page.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(pageData); err != nil {
		t.Fatal(err)
	}
	if err := archiveWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archiveFile.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}

	s, err := NewServer(filepath.Join(root, "data", "bmanga-prototype.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.zipOpenReader = zip.OpenReader

	edgeRequest := httptest.NewRequest(http.MethodGet, "/page?id=work&index=0&max=60", nil)
	edgeResponse := httptest.NewRecorder()
	s.sendArchivePage(edgeResponse, edgeRequest, archivePath, "page.png", int64(len(pageData)), ".png", 60)
	if edgeResponse.Code != http.StatusOK {
		t.Fatalf("edge response = %d: %s", edgeResponse.Code, edgeResponse.Body.String())
	}
	edgeConfig, edgeFormat, err := image.DecodeConfig(bytes.NewReader(edgeResponse.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode edge response: %v", err)
	}
	if edgeFormat != "jpeg" || edgeConfig.Width != 6 || edgeConfig.Height != 60 {
		t.Fatalf("edge output = %s %dx%d, want jpeg 6x60", edgeFormat, edgeConfig.Width, edgeConfig.Height)
	}

	widthRequest := httptest.NewRequest(http.MethodGet, "/page?id=work&index=0&max_width=60", nil)
	widthResponse := httptest.NewRecorder()
	s.sendArchivePage(widthResponse, widthRequest, archivePath, "page.png", int64(len(pageData)), ".png", 0, 60)
	if widthResponse.Code != http.StatusOK {
		t.Fatalf("width response = %d: %s", widthResponse.Code, widthResponse.Body.String())
	}
	widthConfig, widthFormat, err := image.DecodeConfig(bytes.NewReader(widthResponse.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode width response: %v", err)
	}
	if widthFormat != "jpeg" || widthConfig.Width != 60 || widthConfig.Height != 600 {
		t.Fatalf("width output = %s %dx%d, want jpeg 60x600", widthFormat, widthConfig.Width, widthConfig.Height)
	}

	sourceRequest := httptest.NewRequest(http.MethodGet, "/page?id=work&index=0&max_width=120", nil)
	sourceResponse := httptest.NewRecorder()
	s.sendArchivePage(sourceResponse, sourceRequest, archivePath, "page.png", int64(len(pageData)), ".png", 0, 120)
	if sourceResponse.Code != http.StatusOK || sourceResponse.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("source-width archive response = %d type=%q", sourceResponse.Code, sourceResponse.Header().Get("Content-Type"))
	}
	if sourceResponse.Header().Get("X-Bmanga-Image-Mode") != "source" || !bytes.Equal(sourceResponse.Body.Bytes(), pageData) {
		t.Fatal("source-width archive response did not preserve original PNG bytes")
	}

	stat, err := os.Stat(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	cacheKey := archivePath + "|page.png"
	edgeCache := s.thumbnailBytesCachePathForSize(int64(len(pageData)), "image/png", cacheKey, stat.ModTime(), 60)
	widthCache := s.thumbnailBytesCachePathForSizeWithResize(int64(len(pageData)), "image/png", cacheKey, stat.ModTime(), widthResize(60))
	if edgeCache == widthCache {
		t.Fatalf("edge and width cache paths collided: %s", edgeCache)
	}
	for _, path := range []string{edgeCache, widthCache} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected derived cache %q: %v", path, err)
		}
	}
	sourceCache := s.thumbnailBytesCachePathForSizeWithResize(int64(len(pageData)), "image/png", cacheKey, stat.ModTime(), widthResize(120))
	if _, err := os.Stat(sourceCache); !os.IsNotExist(err) {
		t.Fatalf("width-fitting source unexpectedly created derived cache %q: %v", sourceCache, err)
	}
	edgeLock := archivePageSourceLockKey(archivePath, "page.png", 60)
	widthLock := archivePageSourceLockKey(archivePath, "page.png", 0, 60)
	if edgeLock == widthLock {
		t.Fatalf("edge and width render lock keys collided: %s", edgeLock)
	}
}
