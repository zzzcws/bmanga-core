package prototype

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var errArchiveResourceLimit = errors.New("archive resource limit exceeded")

const (
	defaultArchiveImagePixels int64 = 100_000_000
	hardArchiveImagePixels    int64 = 500_000_000
)

type archiveResourceLimits struct {
	maxSourceBytes       int64
	maxEntries           int
	maxDirectoryBytes    int64
	maxReadablePages     int
	maxPageBytes         int64
	maxImagePixels       int64
	maxArchiveEntryBytes int64
	maxDeclaredBytes     int64
	maxCompressionRatio  int64
	maxNestedBytes       int64
	maxNestedDepth       int
}

func loadArchiveResourceLimits() archiveResourceLimits {
	return archiveResourceLimits{
		maxSourceBytes:       envInt64InRange("BMANGA_ARCHIVE_MAX_SOURCE_BYTES", 32*1024*1024*1024, 100*1024*1024, 1024*1024*1024*1024),
		maxEntries:           envIntInRange("BMANGA_ARCHIVE_MAX_ENTRIES", 20000, 100, 100000),
		maxDirectoryBytes:    envInt64InRange("BMANGA_ARCHIVE_MAX_DIRECTORY_BYTES", 128*1024*1024, 1024*1024, 4*1024*1024*1024),
		maxReadablePages:     envIntInRange("BMANGA_ARCHIVE_MAX_READABLE_PAGES", 5000, 100, 50000),
		maxPageBytes:         envInt64InRange("BMANGA_ARCHIVE_MAX_PAGE_BYTES", maxArchivePageBytes, 1024*1024, 1024*1024*1024),
		maxImagePixels:       envInt64InRange("BMANGA_ARCHIVE_MAX_IMAGE_PIXELS", defaultArchiveImagePixels, 1_000_000, hardArchiveImagePixels),
		maxArchiveEntryBytes: envInt64InRange("BMANGA_ARCHIVE_MAX_ENTRY_BYTES", 768*1024*1024, maxArchivePageBytes, 4*1024*1024*1024),
		maxDeclaredBytes:     envInt64InRange("BMANGA_ARCHIVE_MAX_DECLARED_BYTES", 32*1024*1024*1024, maxNestedArchiveBytes, 1024*1024*1024*1024),
		maxCompressionRatio:  int64(envIntInRange("BMANGA_ARCHIVE_MAX_COMPRESSION_RATIO", 500, 10, 10000)),
		maxNestedBytes:       envInt64InRange("BMANGA_ARCHIVE_MAX_NESTED_BYTES", maxNestedArchiveBytes, maxArchivePageBytes, 4*1024*1024*1024),
		maxNestedDepth:       envIntInRange("BMANGA_ARCHIVE_MAX_NESTED_DEPTH", 1, 0, 4),
	}
}

func archiveLimitError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errArchiveResourceLimit, fmt.Sprintf(format, args...))
}

func (s *Server) archiveImagePixelLimit() int64 {
	if s != nil && s.archiveLimits.maxImagePixels > 0 {
		return s.archiveLimits.maxImagePixels
	}
	return defaultArchiveImagePixels
}

func validateArchiveImageDimensions(width, height int, maxPixels int64) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid image dimensions %dx%d", width, height)
	}
	if maxPixels <= 0 {
		return archiveLimitError("image pixel limit is unavailable")
	}
	width64 := int64(width)
	height64 := int64(height)
	// Divide before multiplying so crafted dimensions cannot overflow the
	// product and wrap back underneath the configured ceiling.
	if width64 > maxPixels/height64 {
		return archiveLimitError("image dimensions %dx%d exceed %d pixels", width, height, maxPixels)
	}
	return nil
}

func validateArchiveImageConfig(reader io.Reader, maxPixels int64) error {
	if reader == nil {
		return os.ErrInvalid
	}
	config, _, err := image.DecodeConfig(reader)
	if err != nil {
		return fmt.Errorf("decode image dimensions: %w", err)
	}
	return validateArchiveImageDimensions(config.Width, config.Height, maxPixels)
}

func (s *Server) validateArchiveImageData(data []byte) error {
	return validateArchiveImageConfig(bytes.NewReader(data), s.archiveImagePixelLimit())
}

func (s *Server) validateArchiveImageFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return validateArchiveImageConfig(file, s.archiveImagePixelLimit())
}

type openedZipArchive struct {
	File   []*zip.File
	source *openedArchiveSource
	close  func() error
}

func (r *openedZipArchive) Close() error {
	if r == nil || r.close == nil {
		return nil
	}
	closeFn := r.close
	r.close = nil
	return closeFn()
}

type openedArchiveSource struct {
	file     *os.File
	stat     os.FileInfo
	path     string
	rootPath string
}

func (source *openedArchiveSource) Close() error {
	if source == nil || source.file == nil {
		return nil
	}
	file := source.file
	source.file = nil
	return file.Close()
}

func rootRelativePath(targetPath, rootPath string) (string, string, string, error) {
	targetAbs, err := filepath.Abs(targetPath)
	if err != nil {
		return "", "", "", err
	}
	rootAbs, err := filepath.Abs(rootPath)
	if err != nil {
		return "", "", "", err
	}
	relative, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", "", "", err
	}
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", "", os.ErrPermission
	}
	return targetAbs, rootAbs, relative, nil
}

func validateRootRelativeComponents(root *os.Root, relative string) (os.FileInfo, error) {
	parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	current := ""
	var final os.FileInfo
	for index, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, os.ErrPermission
		}
		if current == "" {
			current = part
		} else {
			current = filepath.Join(current, part)
		}
		info, err := root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if fileInfoIsReparsePoint(info) {
			return nil, os.ErrPermission
		}
		if index < len(parts)-1 && !info.IsDir() {
			return nil, os.ErrPermission
		}
		final = info
	}
	return final, nil
}

func openPinnedFileUnderRoot(path string, rootPath string, maxBytes int64) (*openedArchiveSource, error) {
	return openPinnedFileUnderRootWithHooks(path, rootPath, maxBytes, nil, nil)
}

func openPinnedFileUnderRootWithHooks(
	path string,
	rootPath string,
	maxBytes int64,
	beforeTargetOpen func(),
	afterTargetOpen func(),
) (*openedArchiveSource, error) {
	targetAbs, rootAbs, relative, err := rootRelativePath(path, rootPath)
	if err != nil {
		return nil, err
	}
	rootBefore, err := os.Lstat(rootAbs)
	if err != nil {
		return nil, err
	}
	if !rootBefore.IsDir() || fileInfoIsReparsePoint(rootBefore) {
		return nil, os.ErrPermission
	}
	root, err := os.OpenRoot(rootAbs)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	rootFile, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	rootOpened, err := rootFile.Stat()
	_ = rootFile.Close()
	if err != nil || !rootOpened.IsDir() {
		if err == nil {
			err = os.ErrPermission
		}
		return nil, err
	}
	rootAfter, err := os.Lstat(rootAbs)
	if err != nil || fileInfoIsReparsePoint(rootAfter) || !os.SameFile(rootOpened, rootAfter) {
		if err == nil {
			err = os.ErrPermission
		}
		return nil, err
	}
	before, err := validateRootRelativeComponents(root, relative)
	if err != nil || before.IsDir() {
		if err == nil {
			err = os.ErrPermission
		}
		return nil, err
	}
	if beforeTargetOpen != nil {
		beforeTargetOpen()
	}
	file, err := root.Open(relative)
	if err != nil {
		return nil, err
	}
	closeOnError := func(err error) (*openedArchiveSource, error) {
		_ = file.Close()
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		return closeOnError(err)
	}
	if !opened.Mode().IsRegular() {
		return closeOnError(os.ErrPermission)
	}
	if afterTargetOpen != nil {
		afterTargetOpen()
	}
	after, err := validateRootRelativeComponents(root, relative)
	if err != nil || fileInfoIsReparsePoint(after) || !os.SameFile(opened, after) {
		if err == nil {
			err = os.ErrPermission
		}
		return closeOnError(err)
	}
	if maxBytes > 0 && opened.Size() > maxBytes {
		return closeOnError(archiveLimitError("source is %d bytes; maximum is %d", opened.Size(), maxBytes))
	}
	return &openedArchiveSource{file: file, stat: opened, path: targetAbs, rootPath: rootAbs}, nil
}

func openPinnedFile(path string, maxBytes int64) (*os.File, os.FileInfo, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if before.IsDir() || fileInfoIsReparsePoint(before) {
		return nil, nil, os.ErrPermission
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	closeOnError := func(err error) (*os.File, os.FileInfo, error) {
		_ = file.Close()
		return nil, nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		return closeOnError(err)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return closeOnError(os.ErrPermission)
	}
	after, err := os.Lstat(path)
	if err != nil || fileInfoIsReparsePoint(after) || !os.SameFile(opened, after) {
		if err == nil {
			err = os.ErrPermission
		}
		return closeOnError(err)
	}
	if maxBytes > 0 && opened.Size() > maxBytes {
		return closeOnError(archiveLimitError("source is %d bytes; maximum is %d", opened.Size(), maxBytes))
	}
	return file, opened, nil
}

func (s *Server) openPinnedZipArchive(path string) (*openedZipArchive, error) {
	if s.zipOpenReader != nil {
		reader, err := s.zipOpenReader(path)
		if err != nil {
			return nil, err
		}
		archive := &openedZipArchive{File: reader.File, close: reader.Close}
		if err := s.validateZipEntries(archive.File, 0); err != nil {
			_ = archive.Close()
			return nil, err
		}
		return archive, nil
	}
	file, stat, err := openPinnedFile(path, s.archiveLimits.maxSourceBytes)
	if err != nil {
		return nil, err
	}
	source := &openedArchiveSource{file: file, stat: stat, path: path}
	return s.openPinnedZipArchiveSource(source)
}

func (s *Server) openPinnedZipArchiveUnderRoot(path string, rootPath string) (*openedZipArchive, error) {
	source, err := openPinnedFileUnderRoot(path, rootPath, s.archiveLimits.maxSourceBytes)
	if err != nil {
		return nil, err
	}
	return s.openPinnedZipArchiveSource(source)
}

func (s *Server) openPinnedZipArchiveSource(source *openedArchiveSource) (*openedZipArchive, error) {
	if source == nil || source.file == nil || source.stat == nil {
		return nil, os.ErrInvalid
	}
	if err := s.preflightZipDirectory(source.file, source.stat.Size()); err != nil {
		_ = source.Close()
		return nil, err
	}
	reader, err := zip.NewReader(source.file, source.stat.Size())
	if err != nil {
		_ = source.Close()
		return nil, err
	}
	archive := &openedZipArchive{File: reader.File, source: source, close: source.Close}
	if err := s.validateZipEntries(archive.File, 0); err != nil {
		_ = archive.Close()
		return nil, err
	}
	return archive, nil
}

const (
	zipDirectoryEndSignature   = 0x06054b50
	zip64DirectoryEndSignature = 0x06064b50
	zip64DirectoryLocatorSig   = 0x07064b50
	zipDirectoryEndLen         = 22
	zipMaxCommentLen           = 1<<16 - 1
)

func (s *Server) preflightZipDirectory(reader io.ReaderAt, size int64) error {
	if size < zipDirectoryEndLen {
		return zip.ErrFormat
	}
	tailSize := int64(zipDirectoryEndLen + zipMaxCommentLen + 20 + 56)
	if tailSize > size {
		tailSize = size
	}
	tail := make([]byte, int(tailSize))
	if _, err := reader.ReadAt(tail, size-tailSize); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	eocd := -1
	for index := len(tail) - zipDirectoryEndLen; index >= 0; index-- {
		if binary.LittleEndian.Uint32(tail[index:index+4]) != zipDirectoryEndSignature {
			continue
		}
		commentLength := int(binary.LittleEndian.Uint16(tail[index+20 : index+22]))
		if index+zipDirectoryEndLen+commentLength == len(tail) {
			eocd = index
			break
		}
	}
	if eocd < 0 {
		return zip.ErrFormat
	}
	entryCount := uint64(binary.LittleEndian.Uint16(tail[eocd+10 : eocd+12]))
	directoryBytes := uint64(binary.LittleEndian.Uint32(tail[eocd+12 : eocd+16]))
	if entryCount == 0xffff || directoryBytes == 0xffffffff {
		locator := eocd - 20
		if locator < 0 || binary.LittleEndian.Uint32(tail[locator:locator+4]) != zip64DirectoryLocatorSig {
			return zip.ErrFormat
		}
		zip64End := -1
		for index := locator - 56; index >= 0; index-- {
			if binary.LittleEndian.Uint32(tail[index:index+4]) != zip64DirectoryEndSignature {
				continue
			}
			recordSize := binary.LittleEndian.Uint64(tail[index+4 : index+12])
			if recordSize >= 44 && recordSize <= uint64(locator-index-12) && index+12+int(recordSize) == locator {
				zip64End = index
				break
			}
		}
		if zip64End < 0 || zip64End+56 > len(tail) {
			return zip.ErrFormat
		}
		entryCount = binary.LittleEndian.Uint64(tail[zip64End+32 : zip64End+40])
		directoryBytes = binary.LittleEndian.Uint64(tail[zip64End+40 : zip64End+48])
	}
	if entryCount > uint64(s.archiveLimits.maxEntries) {
		return archiveLimitError("archive declares %d entries; maximum is %d", entryCount, s.archiveLimits.maxEntries)
	}
	if directoryBytes > uint64(s.archiveLimits.maxDirectoryBytes) {
		return archiveLimitError("archive central directory is %d bytes; maximum is %d", directoryBytes, s.archiveLimits.maxDirectoryBytes)
	}
	return nil
}

func (s *Server) validateZipEntries(files []*zip.File, depth int) error {
	limits := s.archiveLimits
	if len(files) > limits.maxEntries {
		return archiveLimitError("archive has %d entries; maximum is %d", len(files), limits.maxEntries)
	}
	var total uint64
	var totalCompressed uint64
	for _, file := range files {
		if file == nil || file.FileInfo().IsDir() {
			continue
		}
		uncompressed := file.UncompressedSize64
		if uncompressed > uint64(limits.maxArchiveEntryBytes) {
			return archiveLimitError("entry %q declares %d bytes; maximum is %d", displayArchiveEntryName(file.Name), uncompressed, limits.maxArchiveEntryBytes)
		}
		if total > ^uint64(0)-uncompressed {
			return archiveLimitError("declared uncompressed size overflow")
		}
		total += uncompressed
		if total > uint64(limits.maxDeclaredBytes) {
			return archiveLimitError("archive declares more than %d uncompressed bytes", limits.maxDeclaredBytes)
		}
		compressed := file.CompressedSize64
		if uncompressed > 0 && compressed == 0 {
			return archiveLimitError("entry %q has an invalid zero compressed size", displayArchiveEntryName(file.Name))
		}
		if compressed > 0 && exceedsCompressionRatio(uncompressed, compressed, uint64(limits.maxCompressionRatio)) {
			return archiveLimitError("entry %q exceeds compression ratio %d:1", displayArchiveEntryName(file.Name), limits.maxCompressionRatio)
		}
		if totalCompressed > ^uint64(0)-compressed {
			return archiveLimitError("declared compressed size overflow")
		}
		totalCompressed += compressed
		if depth >= limits.maxNestedDepth && isZipCBZExtension(archiveEntryExtension(file.Name)) {
			return archiveLimitError("nested archive depth exceeds %d", limits.maxNestedDepth)
		}
	}
	if total > 0 && exceedsCompressionRatio(total, totalCompressed, uint64(limits.maxCompressionRatio)) {
		return archiveLimitError("archive exceeds total compression ratio %d:1", limits.maxCompressionRatio)
	}
	return nil
}

func exceedsCompressionRatio(uncompressed, compressed, maximum uint64) bool {
	if uncompressed == 0 {
		return false
	}
	if compressed == 0 || maximum == 0 {
		return true
	}
	quotient := uncompressed / compressed
	return quotient > maximum || (quotient == maximum && uncompressed%compressed != 0)
}

func (s *Server) validateReadablePageCount(count int) error {
	if count > s.archiveLimits.maxReadablePages {
		return archiveLimitError("archive exposes %d readable pages; maximum is %d", count, s.archiveLimits.maxReadablePages)
	}
	return nil
}

func readAllArchiveEntry(file *zip.File, maxBytes int64) ([]byte, error) {
	if file == nil {
		return nil, os.ErrNotExist
	}
	if int64(file.UncompressedSize64) > maxBytes {
		return nil, archiveLimitError("entry %q exceeds %d bytes", displayArchiveEntryName(file.Name), maxBytes)
	}
	entry, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer entry.Close()
	data, err := io.ReadAll(io.LimitReader(entry, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, archiveLimitError("entry %q exceeds %d bytes", displayArchiveEntryName(file.Name), maxBytes)
	}
	return data, nil
}
