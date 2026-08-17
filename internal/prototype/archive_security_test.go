package prototype

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func testArchiveLimits() archiveResourceLimits {
	limits := loadArchiveResourceLimits()
	limits.maxSourceBytes = 64 * 1024 * 1024
	limits.maxEntries = 100
	limits.maxDirectoryBytes = 8 * 1024 * 1024
	limits.maxReadablePages = 100
	limits.maxPageBytes = 8 * 1024 * 1024
	limits.maxImagePixels = 10_000_000
	limits.maxArchiveEntryBytes = 16 * 1024 * 1024
	limits.maxDeclaredBytes = 32 * 1024 * 1024
	limits.maxCompressionRatio = 500
	limits.maxNestedBytes = 16 * 1024 * 1024
	limits.maxNestedDepth = 1
	return limits
}

func testPNGWithDimensions(width, height uint32) []byte {
	var output bytes.Buffer
	output.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	writeChunk := func(kind string, payload []byte) {
		_ = binary.Write(&output, binary.BigEndian, uint32(len(payload)))
		output.WriteString(kind)
		output.Write(payload)
		checksum := crc32.NewIEEE()
		_, _ = checksum.Write([]byte(kind))
		_, _ = checksum.Write(payload)
		_ = binary.Write(&output, binary.BigEndian, checksum.Sum32())
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = 8
	ihdr[9] = 2
	writeChunk("IHDR", ihdr)
	writeChunk("IEND", nil)
	return output.Bytes()
}

func TestArchiveImagePixelBudgetRejectsMetadataBombAndOverflow(t *testing.T) {
	limits := testArchiveLimits()
	limits.maxImagePixels = 100_000_000
	s := &Server{archiveLimits: limits}
	bomb := testPNGWithDimensions(50_000, 50_000)
	if err := s.validateArchiveImageData(bomb); !errors.Is(err, errArchiveResourceLimit) {
		t.Fatalf("pixel bomb error = %v, want resource limit", err)
	}
	maxInt := int(^uint(0) >> 1)
	if err := validateArchiveImageDimensions(maxInt, maxInt, hardArchiveImagePixels); !errors.Is(err, errArchiveResourceLimit) {
		t.Fatalf("overflow dimensions error = %v, want resource limit", err)
	}
}

func TestArchiveThumbnailRejectsPixelBombBeforeFullDecode(t *testing.T) {
	dir := t.TempDir()
	limits := testArchiveLimits()
	limits.maxImagePixels = 100_000_000
	s := &Server{
		root:               dir,
		thumbnailCacheRoot: filepath.Join(dir, "thumbs"),
		thumbnailSem:       make(chan struct{}, 1),
		renderLocks:        map[string]*keyedRenderLock{},
		archiveLimits:      limits,
	}
	cachePath := filepath.Join(s.thumbnailCacheRoot, "bomb.jpg")
	built, err := s.ensureThumbnailBytesToPathCached(context.Background(), testPNGWithDimensions(50_000, 50_000), cachePath, 480)
	if built || !errors.Is(err, errArchiveResourceLimit) {
		t.Fatalf("thumbnail build = %v, %v; want resource limit before decode", built, err)
	}
	if _, statErr := os.Stat(cachePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("pixel bomb left thumbnail cache behind: %v", statErr)
	}
}

func writeArchiveEntries(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, data := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPinnedZipArchiveReadsOpenedFileAfterPathReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows may intentionally deny replacing an open archive")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "book.zip")
	writeArchiveEntries(t, path, map[string][]byte{"page.jpg": []byte("original")})
	s := &Server{archiveLimits: testArchiveLimits()}
	reader, err := s.openZipArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	replacement := filepath.Join(dir, "replacement.zip")
	writeArchiveEntries(t, replacement, map[string][]byte{"page.jpg": []byte("replacement")})
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	data, err := readAllArchiveEntry(reader.File[0], 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original" {
		t.Fatalf("opened archive changed to %q", data)
	}
}

func TestOpenPinnedFileUnderRootRejectsAncestorSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not reliably available on Windows test hosts")
	}
	root := t.TempDir()
	outside := t.TempDir()
	directory := filepath.Join(root, "shelf")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "book.zip")
	if err := os.WriteFile(target, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "book.zip"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	saved := filepath.Join(root, "shelf-saved")
	if err := os.Rename(directory, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, directory); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Remove(directory)
		_ = os.Rename(saved, directory)
	}()

	source, err := openPinnedFileUnderRoot(target, root, 1024)
	if source != nil {
		_ = source.Close()
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("error = %v, want permission failure", err)
	}
}

func TestOpenPinnedFileUnderRootRejectsAncestorABASwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not reliably available on Windows test hosts")
	}
	root := t.TempDir()
	directory := filepath.Join(root, "shelf")
	alternate := filepath.Join(root, "alternate")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(alternate, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "book.zip")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(alternate, "book.zip"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	saved := filepath.Join(root, "shelf-saved")
	swapped := false
	beforeOpen := func() {
		if err := os.Rename(directory, saved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("alternate", directory); err != nil {
			t.Fatal(err)
		}
		swapped = true
	}
	afterOpen := func() {
		if err := os.Remove(directory); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(saved, directory); err != nil {
			t.Fatal(err)
		}
		swapped = false
	}
	defer func() {
		if swapped {
			_ = os.Remove(directory)
			_ = os.Rename(saved, directory)
		}
	}()

	source, err := openPinnedFileUnderRootWithHooks(target, root, 1024, beforeOpen, afterOpen)
	if source != nil {
		_ = source.Close()
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("error = %v, want permission failure", err)
	}
}

func TestOpenZipArchiveRejectsEntryCountLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "many.zip")
	writeArchiveEntries(t, path, map[string][]byte{"1.jpg": {1}, "2.jpg": {2}})
	limits := testArchiveLimits()
	limits.maxEntries = 1
	s := &Server{archiveLimits: limits}
	reader, err := s.openZipArchive(path)
	if reader != nil {
		_ = reader.Close()
	}
	if !errors.Is(err, errArchiveResourceLimit) {
		t.Fatalf("error = %v, want resource limit", err)
	}
}

func TestOpenZipArchiveRejectsCentralDirectoryBudgetBeforeParsing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "directory-budget.zip")
	writeArchiveEntries(t, path, map[string][]byte{"page.jpg": []byte("image")})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	signature := []byte{'P', 'K', 0x05, 0x06}
	index := bytes.LastIndex(data, signature)
	if index < 0 || index+16 > len(data) {
		t.Fatal("zip end record not found")
	}
	data[index+12] = 0x00
	data[index+13] = 0x10
	data[index+14] = 0x00
	data[index+15] = 0x00
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	limits := testArchiveLimits()
	limits.maxDirectoryBytes = 1024
	s := &Server{archiveLimits: limits}
	reader, err := s.openZipArchive(path)
	if reader != nil {
		_ = reader.Close()
	}
	if !errors.Is(err, errArchiveResourceLimit) {
		t.Fatalf("error = %v, want central-directory resource limit", err)
	}
}

func TestOpenZipArchiveRejectsCompressionRatioLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bomb.zip")
	writeArchiveEntries(t, path, map[string][]byte{"page.jpg": bytes.Repeat([]byte{0}, 2*1024*1024)})
	limits := testArchiveLimits()
	limits.maxCompressionRatio = 10
	s := &Server{archiveLimits: limits}
	reader, err := s.openZipArchive(path)
	if reader != nil {
		_ = reader.Close()
	}
	if !errors.Is(err, errArchiveResourceLimit) {
		t.Fatalf("error = %v, want resource limit", err)
	}
}

func TestValidateZipEntriesRejectsNestedDepth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inner.zip")
	writeArchiveEntries(t, path, map[string][]byte{"deeper.zip": {1, 2, 3}})
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	s := &Server{archiveLimits: testArchiveLimits()}
	if err := s.validateZipEntries(reader.File, 1); !errors.Is(err, errArchiveResourceLimit) {
		t.Fatalf("error = %v, want nested depth limit", err)
	}
}

func TestDefaultArchiveLimitsPreserveExistingPageAndNestedCaps(t *testing.T) {
	limits := loadArchiveResourceLimits()
	if limits.maxPageBytes != maxArchivePageBytes {
		t.Fatalf("page bytes = %d, want %d", limits.maxPageBytes, maxArchivePageBytes)
	}
	if limits.maxNestedBytes != maxNestedArchiveBytes {
		t.Fatalf("nested bytes = %d, want %d", limits.maxNestedBytes, maxNestedArchiveBytes)
	}
}
