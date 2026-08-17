package prototype

import (
	"archive/zip"
	"bytes"
	"errors"
	"image"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (s *Server) sendSourceImage(w http.ResponseWriter, r *http.Request, libraryKey, sourcePath, relativePath string, maxDimension int) {
	source, err := s.resolveSourcePath(libraryKey, sourcePath, relativePath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		http.NotFound(w, r)
		return
	}
	s.serveImageFile(w, r, source, "", maxDimension)
}

func (s *Server) sendSourcePageImage(w http.ResponseWriter, r *http.Request, work map[string]any, row map[string]any, manifest map[string]any, maxDimension int) {
	libraryKey := stringValue(row["library_key"])
	sourcePath := stringValue(row["path"])
	relativePath := stringValue(row["relative_path"])
	source, err := s.resolveSourcePath(libraryKey, sourcePath, relativePath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		http.NotFound(w, r)
		return
	}

	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(source)))
	if contentType == "" && strings.EqualFold(filepath.Ext(source), ".webp") {
		contentType = "image/webp"
	}
	if !allowedImageMIME(contentType) {
		http.Error(w, http.StatusText(http.StatusUnsupportedMediaType), http.StatusUnsupportedMediaType)
		return
	}

	file, err := os.Open(source)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || stat.IsDir() {
		http.NotFound(w, r)
		return
	}
	if stat.Size() > s.archiveLimits.maxPageBytes {
		s.serveImageFile(w, r, source, contentType, maxDimension)
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, s.archiveLimits.maxPageBytes+1))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if int64(len(data)) > s.archiveLimits.maxPageBytes {
		s.serveImageFile(w, r, source, contentType, maxDimension)
		return
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		http.Error(w, "invalid image data", http.StatusUnsupportedMediaType)
		return
	}

	if maxDimension > 0 && s.sendThumbnailBytes(w, r, data, contentType, source, stat.ModTime(), maxDimension) {
		return
	}
	w.Header().Set("Content-Type", contentType)
	if writeImageCacheHeaders(w, r, "public, max-age=3600", imageETag(stat.Size(), stat.ModTime())) {
		return
	}
	http.ServeContent(w, r, filepath.Base(source), stat.ModTime(), bytes.NewReader(data))
}

func (s *Server) sendLocalCacheImage(w http.ResponseWriter, r *http.Request, cachePath, mimeType string, maxDimension int) {
	source, err := s.resolveLocalCachePath(cachePath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		http.NotFound(w, r)
		return
	}
	s.serveImageFile(w, r, source, mimeType, maxDimension)
}

func archivePageSourceLockKey(archivePath, normalizedInnerPath string, maxDimension int) string {
	return "archive-page-source:" + archivePath + "|" + normalizedInnerPath + "|" + strconv.Itoa(maxDimension)
}

func (s *Server) openPinnedLibrarySource(libraryKey, sourcePath, relativePath string) (*openedArchiveSource, error) {
	resolved, rootPath, err := s.resolveSourcePathWithRoot(libraryKey, sourcePath, relativePath)
	if err != nil {
		return nil, err
	}
	return openPinnedFileUnderRoot(resolved, rootPath, s.archiveLimits.maxSourceBytes)
}

func (s *Server) openPinnedLibraryZipArchive(libraryKey, sourcePath, relativePath string) (*openedZipArchive, error) {
	resolved, rootPath, err := s.resolveSourcePathWithRoot(libraryKey, sourcePath, relativePath)
	if err != nil {
		return nil, err
	}
	return s.openPinnedZipArchiveUnderRoot(resolved, rootPath)
}

func (s *Server) trustedArchiveRootForPath(archivePath string) (string, error) {
	mappedPath := s.remapRuntimePath(archivePath)
	bestRoot := ""
	consider := func(rootPath string) {
		rootPath = s.remapRuntimePath(strings.TrimSpace(rootPath))
		if rootPath == "" || !isUnderPathRoot(mappedPath, rootPath) {
			return
		}
		if len(normalizePathForBoundary(rootPath)) > len(normalizePathForBoundary(bestRoot)) {
			bestRoot = rootPath
		}
	}
	for _, rootPath := range []string{
		s.localCacheRoot,
	} {
		consider(rootPath)
	}
	if s.db != nil {
		rows, err := s.query("SELECT root_path FROM libraries")
		if err != nil {
			return "", err
		}
		for _, row := range rows {
			consider(stringValue(row["root_path"]))
		}
	}
	if bestRoot == "" {
		return "", os.ErrPermission
	}
	return bestRoot, nil
}

func (s *Server) openZipArchive(archivePath string) (*openedZipArchive, error) {
	if s.zipOpenReader != nil {
		return s.openPinnedZipArchive(archivePath)
	}
	rootPath, err := s.trustedArchiveRootForPath(archivePath)
	if err == nil {
		return s.openPinnedZipArchiveUnderRoot(s.remapRuntimePath(archivePath), rootPath)
	}
	// Unit tests construct lightweight servers without configured roots. Keep the
	// compatibility path out of production servers, where a DB is always present.
	if s.db == nil {
		return s.openPinnedZipArchive(archivePath)
	}
	return nil, err
}

func (s *Server) sendArchivePage(w http.ResponseWriter, r *http.Request, archivePath string, innerPath string, sizeBytes int64, extension string, maxDimension int) {
	s.sendArchivePageWithSource(w, r, archivePath, innerPath, sizeBytes, extension, maxDimension, nil)
}

func (s *Server) sendLibraryArchivePage(
	w http.ResponseWriter,
	r *http.Request,
	libraryKey string,
	archivePath string,
	sourceRelativePath string,
	innerPath string,
	sizeBytes int64,
	extension string,
	maxDimension int,
) {
	s.sendArchivePageWithSource(w, r, archivePath, innerPath, sizeBytes, extension, maxDimension, func() (*openedArchiveSource, error) {
		return s.openPinnedLibrarySource(libraryKey, archivePath, sourceRelativePath)
	})
}

func (s *Server) sendArchivePageWithSource(
	w http.ResponseWriter,
	r *http.Request,
	archivePath string,
	innerPath string,
	sizeBytes int64,
	extension string,
	maxDimension int,
	openSource func() (*openedArchiveSource, error),
) {
	if archivePath == "" || innerPath == "" {
		http.NotFound(w, r)
		return
	}
	normalizedInnerPath := normalizeArchiveEntryName(innerPath)
	cacheKey := archivePath + "|" + normalizedInnerPath
	modTime := time.Now()
	thumbnailPath := ""
	if openSource == nil {
		stat, statErr := os.Stat(archivePath)
		if statErr == nil {
			modTime = stat.ModTime()
			if maxDimension > 0 && sizeBytes > 0 {
				contentType := archiveEntryMIME(coalesceString(extension, innerPath))
				thumbnailPath = s.thumbnailBytesCachePathForSize(sizeBytes, contentType, cacheKey, modTime, maxDimension)
				if s.sendCachedThumbnail(w, r, thumbnailPath) {
					return
				}
			}
		}
	}
	releaseLock, ok := s.acquireRenderLock(
		r.Context(),
		archivePageSourceLockKey(archivePath, normalizedInnerPath, maxDimension),
	)
	if !ok {
		return
	}
	lockHeld := true
	unlockSource := func() {
		if lockHeld {
			releaseLock()
			lockHeld = false
		}
	}
	defer unlockSource()
	if thumbnailPath != "" {
		if thumbnailFile, thumbnailStat, ready := openCachedThumbnail(thumbnailPath); ready {
			unlockSource()
			defer thumbnailFile.Close()
			serveCachedThumbnailFile(w, r, thumbnailPath, thumbnailFile, thumbnailStat)
			return
		}
	}

	var pinnedSource *openedArchiveSource
	var err error
	if openSource != nil {
		pinnedSource, err = openSource()
		if err != nil {
			unlockSource()
			if errors.Is(err, errArchiveResourceLimit) {
				http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			} else if errors.Is(err, os.ErrPermission) {
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			} else {
				http.NotFound(w, r)
			}
			return
		}
		archivePath = pinnedSource.path
		cacheKey = archivePath + "|" + normalizedInnerPath
		modTime = pinnedSource.stat.ModTime()
		if maxDimension > 0 && sizeBytes > 0 {
			contentType := archiveEntryMIME(coalesceString(extension, innerPath))
			thumbnailPath = s.thumbnailBytesCachePathForSize(sizeBytes, contentType, cacheKey, modTime, maxDimension)
			if thumbnailFile, thumbnailStat, ready := openCachedThumbnail(thumbnailPath); ready {
				_ = pinnedSource.Close()
				unlockSource()
				defer thumbnailFile.Close()
				serveCachedThumbnailFile(w, r, thumbnailPath, thumbnailFile, thumbnailStat)
				return
			}
		}
	}
	var reader *openedZipArchive
	if pinnedSource != nil {
		reader, err = s.openPinnedZipArchiveSource(pinnedSource)
		pinnedSource = nil
	} else {
		reader, err = s.openZipArchive(archivePath)
	}
	if err != nil {
		unlockSource()
		if errors.Is(err, errArchiveResourceLimit) {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	readerOpen := true
	closeReader := func() {
		if readerOpen {
			_ = reader.Close()
			readerOpen = false
		}
	}
	defer closeReader()

	var selected *zip.File
	for _, file := range reader.File {
		if archiveEntryNameMatches(file.Name, normalizedInnerPath) {
			selected = file
			break
		}
	}
	if selected == nil || isIgnoredArchiveEntry(selected) || !isArchiveImageExtension(archiveEntryExtension(selected.Name)) {
		closeReader()
		unlockSource()
		http.NotFound(w, r)
		return
	}
	if selected.UncompressedSize64 > uint64(s.archiveLimits.maxPageBytes) {
		closeReader()
		unlockSource()
		http.Error(w, "archive page too large", http.StatusRequestEntityTooLarge)
		return
	}
	contentType := archiveEntryMIME(selected.Name)
	if !allowedImageMIME(contentType) {
		closeReader()
		unlockSource()
		http.Error(w, http.StatusText(http.StatusUnsupportedMediaType), http.StatusUnsupportedMediaType)
		return
	}
	if maxDimension > 0 && (thumbnailPath == "" || sizeBytes != int64(selected.UncompressedSize64)) {
		thumbnailPath = s.thumbnailBytesCachePathForSize(int64(selected.UncompressedSize64), contentType, cacheKey, modTime, maxDimension)
	}
	if thumbnailPath != "" {
		if thumbnailFile, thumbnailStat, ready := openCachedThumbnail(thumbnailPath); ready {
			closeReader()
			unlockSource()
			defer thumbnailFile.Close()
			serveCachedThumbnailFile(w, r, thumbnailPath, thumbnailFile, thumbnailStat)
			return
		}
	}
	entry, err := selected.Open()
	if err != nil {
		closeReader()
		unlockSource()
		http.NotFound(w, r)
		return
	}
	data, err := io.ReadAll(io.LimitReader(entry, s.archiveLimits.maxPageBytes+1))
	_ = entry.Close()
	if err != nil {
		closeReader()
		unlockSource()
		http.NotFound(w, r)
		return
	}
	if int64(len(data)) > s.archiveLimits.maxPageBytes {
		closeReader()
		unlockSource()
		http.Error(w, "archive page too large", http.StatusRequestEntityTooLarge)
		return
	}
	if err := s.validateArchiveImageData(data); err != nil {
		closeReader()
		unlockSource()
		if errors.Is(err, errArchiveResourceLimit) {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid archive image", http.StatusUnsupportedMediaType)
		}
		return
	}
	var thumbnailFile *os.File
	var thumbnailStat os.FileInfo
	thumbnailBuilt := false
	if maxDimension > 0 && thumbnailPath != "" {
		started := time.Now()
		built, thumbnailErr := s.ensureThumbnailBytesToPathCached(r.Context(), data, thumbnailPath, maxDimension)
		appendServerTiming(w.Header(), "thumbnail", time.Since(started))
		thumbnailBuilt = built
		if thumbnailErr == nil {
			thumbnailFile, thumbnailStat, _ = openCachedThumbnail(thumbnailPath)
		}
	}
	closeReader()
	unlockSource()
	if thumbnailBuilt {
		w.Header().Set("X-Bmanga-Cache", "miss")
	}
	if thumbnailFile != nil {
		defer thumbnailFile.Close()
		serveCachedThumbnailFile(w, r, thumbnailPath, thumbnailFile, thumbnailStat)
		return
	}
	if maxDimension > 0 && thumbnailPath == "" && s.sendThumbnailBytes(w, r, data, contentType, cacheKey, modTime, maxDimension) {
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if writeImageCacheHeaders(w, r, "public, max-age=3600", imageETag(int64(len(data)), modTime)) {
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) sendEbookPage(w http.ResponseWriter, r *http.Request, libraryKey, archivePath, sourceRelativePath, innerPath string, sizeBytes int64, extension string, maxDimension int) {
	if archivePath == "" || innerPath == "" {
		http.NotFound(w, r)
		return
	}
	if !isEPUBExtension(filepath.Ext(archivePath)) {
		http.NotFound(w, r)
		return
	}
	s.sendLibraryArchivePage(w, r, libraryKey, archivePath, sourceRelativePath, innerPath, sizeBytes, extension, maxDimension)
}

func (s *Server) sendNestedArchivePage(
	w http.ResponseWriter,
	r *http.Request,
	libraryKey string,
	archivePath string,
	sourceRelativePath string,
	innerPath string,
	sizeBytes int64,
	extension string,
	maxDimension int,
) {
	outerPath, imagePath, ok := splitNestedArchivePath(innerPath)
	if archivePath == "" || !ok {
		http.NotFound(w, r)
		return
	}
	cacheKey := archivePath + "|" + outerPath + "!" + imagePath
	modTime := time.Now()
	thumbnailPath := ""
	var releaseSourceLock func()
	unlockSource := func() {
		if releaseSourceLock != nil {
			releaseSourceLock()
			releaseSourceLock = nil
		}
	}
	defer unlockSource()
	releaseLock, acquired := s.acquireRenderLock(r.Context(), "nested-archive-page-source:"+cacheKey+"|"+strconv.Itoa(maxDimension))
	if !acquired {
		return
	}
	releaseSourceLock = releaseLock
	reader, err := s.openPinnedLibraryZipArchive(libraryKey, archivePath, sourceRelativePath)
	if err != nil {
		unlockSource()
		if errors.Is(err, errArchiveResourceLimit) {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		} else if errors.Is(err, os.ErrPermission) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	archivePath = reader.source.path
	cacheKey = archivePath + "|" + outerPath + "!" + imagePath
	modTime = reader.source.stat.ModTime()
	if maxDimension > 0 && sizeBytes > 0 {
		contentType := archiveEntryMIME(coalesceString(extension, imagePath))
		thumbnailPath = s.thumbnailBytesCachePathForSize(sizeBytes, contentType, cacheKey, modTime, maxDimension)
		if thumbnailFile, thumbnailStat, ready := openCachedThumbnail(thumbnailPath); ready {
			_ = reader.Close()
			unlockSource()
			defer thumbnailFile.Close()
			serveCachedThumbnailFile(w, r, thumbnailPath, thumbnailFile, thumbnailStat)
			return
		}
	}
	readerOpen := true
	closeReader := func() {
		if readerOpen {
			_ = reader.Close()
			readerOpen = false
		}
	}
	defer closeReader()

	outer := findZipFileByName(reader.File, outerPath)
	if outer == nil || isIgnoredArchiveEntry(outer) || !isZipCBZExtension(archiveEntryExtension(outer.Name)) {
		closeReader()
		unlockSource()
		http.NotFound(w, r)
		return
	}
	if s.archiveLimits.maxNestedDepth < 1 {
		closeReader()
		unlockSource()
		http.Error(w, "nested archives are disabled", http.StatusRequestEntityTooLarge)
		return
	}
	innerBytes, err := readAllArchiveEntry(outer, s.archiveLimits.maxNestedBytes)
	if err != nil {
		closeReader()
		unlockSource()
		if errors.Is(err, errArchiveResourceLimit) {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	innerSource := bytes.NewReader(innerBytes)
	if err := s.preflightZipDirectory(innerSource, int64(len(innerBytes))); err != nil {
		closeReader()
		unlockSource()
		if errors.Is(err, errArchiveResourceLimit) {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	innerReader, err := zip.NewReader(innerSource, int64(len(innerBytes)))
	if err != nil {
		closeReader()
		unlockSource()
		http.NotFound(w, r)
		return
	}
	if err := s.validateZipEntries(innerReader.File, 1); err != nil {
		closeReader()
		unlockSource()
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	selected := findZipFileByName(innerReader.File, imagePath)
	if selected == nil || isIgnoredArchiveEntry(selected) || !isArchiveImageExtension(archiveEntryExtension(selected.Name)) {
		closeReader()
		unlockSource()
		http.NotFound(w, r)
		return
	}
	if selected.UncompressedSize64 > uint64(s.archiveLimits.maxPageBytes) {
		closeReader()
		unlockSource()
		http.Error(w, "archive page too large", http.StatusRequestEntityTooLarge)
		return
	}
	contentType := archiveEntryMIME(selected.Name)
	if !allowedImageMIME(contentType) {
		closeReader()
		unlockSource()
		http.Error(w, http.StatusText(http.StatusUnsupportedMediaType), http.StatusUnsupportedMediaType)
		return
	}
	if maxDimension > 0 && (thumbnailPath == "" || sizeBytes != int64(selected.UncompressedSize64)) {
		thumbnailPath = s.thumbnailBytesCachePathForSize(int64(selected.UncompressedSize64), contentType, cacheKey, modTime, maxDimension)
		if thumbnailFile, thumbnailStat, ready := openCachedThumbnail(thumbnailPath); ready {
			closeReader()
			unlockSource()
			defer thumbnailFile.Close()
			serveCachedThumbnailFile(w, r, thumbnailPath, thumbnailFile, thumbnailStat)
			return
		}
	}
	data, err := readAllArchiveEntry(selected, s.archiveLimits.maxPageBytes)
	if err != nil {
		closeReader()
		unlockSource()
		http.NotFound(w, r)
		return
	}
	if err := s.validateArchiveImageData(data); err != nil {
		closeReader()
		unlockSource()
		if errors.Is(err, errArchiveResourceLimit) {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid archive image", http.StatusUnsupportedMediaType)
		}
		return
	}
	var thumbnailFile *os.File
	var thumbnailStat os.FileInfo
	thumbnailBuilt := false
	if maxDimension > 0 && thumbnailPath != "" {
		started := time.Now()
		built, thumbnailErr := s.ensureThumbnailBytesToPathCached(r.Context(), data, thumbnailPath, maxDimension)
		appendServerTiming(w.Header(), "thumbnail", time.Since(started))
		thumbnailBuilt = built
		if thumbnailErr == nil {
			thumbnailFile, thumbnailStat, _ = openCachedThumbnail(thumbnailPath)
		}
	}
	closeReader()
	unlockSource()
	if thumbnailBuilt {
		w.Header().Set("X-Bmanga-Cache", "miss")
	}
	if thumbnailFile != nil {
		defer thumbnailFile.Close()
		serveCachedThumbnailFile(w, r, thumbnailPath, thumbnailFile, thumbnailStat)
		return
	}
	if maxDimension > 0 && thumbnailPath == "" && s.sendThumbnailBytes(w, r, data, contentType, cacheKey, modTime, maxDimension) {
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if writeImageCacheHeaders(w, r, "public, max-age=3600", imageETag(int64(len(data)), modTime)) {
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func splitNestedArchivePath(innerPath string) (string, string, bool) {
	parts := strings.SplitN(normalizeArchiveEntryName(innerPath), "!", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func findZipFileByName(files []*zip.File, name string) *zip.File {
	normalized := normalizeArchiveEntryName(name)
	for _, file := range files {
		if archiveEntryNameMatches(file.Name, normalized) {
			return file
		}
	}
	return nil
}

func archiveEntryNameMatches(entryName string, normalizedTarget string) bool {
	normalizedEntry := normalizeArchiveEntryName(entryName)
	displayEntry := displayArchiveEntryName(entryName)
	target := normalizeArchiveEntryName(normalizedTarget)
	return normalizedEntry == target ||
		displayEntry == target ||
		archiveCompatibilityFold(normalizedEntry) == archiveCompatibilityFold(target) ||
		archiveCompatibilityFold(displayEntry) == archiveCompatibilityFold(target)
}
