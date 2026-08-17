package prototype

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Server) readyCoverCandidate(candidateID string) (map[string]any, error) {
	rows, err := s.query(readyCoverCandidateQuery, candidateID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if stringValue(row["cover_status"]) == "ready" {
			return row, nil
		}
	}
	return nil, nil
}

func (s *Server) coverAssetForCandidate(candidateID string, libraryKey string) (map[string]any, error) {
	rows, err := s.query(`
		SELECT cache_path, mime_type, source_path, source_relative_path, source_inner_path
		FROM cover_assets
		WHERE candidate_id = ?
		  AND asset_kind = 'extracted_cover'
		ORDER BY updated_at DESC, stable_key DESC
		LIMIT 1
	`, candidateID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	rows[0]["library_key"] = libraryKey
	return rows[0], nil
}

func (s *Server) sendCoverAssetSourceImage(w http.ResponseWriter, r *http.Request, asset map[string]any, maxDimension int) error {
	libraryKey := stringValue(asset["library_key"])
	sourcePath := stringValue(asset["source_path"])
	relativePath := stringValue(asset["source_relative_path"])
	innerPath := stringValue(asset["source_inner_path"])
	if libraryKey == "" || sourcePath == "" || innerPath == "" {
		return os.ErrNotExist
	}
	source, err := s.openPinnedLibrarySource(libraryKey, sourcePath, relativePath)
	if err != nil {
		return err
	}
	defer source.Close()
	archivePath := source.path
	if strings.EqualFold(filepath.Ext(archivePath), ".pdf") || strings.Contains(strings.ToLower(innerPath), "#page=") {
		return errUnsupportedReaderSource
	}
	normalizedInnerPath := normalizeArchiveEntryName(innerPath)
	loadSource := func() ([]byte, string, error) {
		reader, err := s.openPinnedZipArchiveSource(source)
		if err != nil {
			return nil, "", err
		}
		defer reader.Close()

		var selected *zip.File
		for _, file := range reader.File {
			if archiveEntryNameMatches(file.Name, normalizedInnerPath) {
				selected = file
				break
			}
		}
		if selected == nil || isIgnoredArchiveEntry(selected) || !isArchiveImageExtension(archiveEntryExtension(selected.Name)) {
			return nil, "", os.ErrNotExist
		}
		if selected.UncompressedSize64 > uint64(s.archiveLimits.maxPageBytes) {
			return nil, "", fmt.Errorf("cover source page too large")
		}
		file, err := selected.Open()
		if err != nil {
			return nil, "", err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, s.archiveLimits.maxPageBytes+1))
		if err != nil {
			return nil, "", err
		}
		if int64(len(data)) > s.archiveLimits.maxPageBytes {
			return nil, "", fmt.Errorf("cover source page too large")
		}
		if err := s.validateArchiveImageData(data); err != nil {
			return nil, "", err
		}
		return data, archiveEntryMIME(innerPath), nil
	}

	thumbnailPath := ""
	if maxDimension > 0 {
		if archiveStat := source.stat; archiveStat != nil {
			thumbnailPath = s.thumbnailCachePathForKey(fmt.Sprintf(
				"archive-entry|%s|%s|%d|%d|%d",
				archivePath,
				normalizedInnerPath,
				archiveStat.Size(),
				archiveStat.ModTime().UnixNano(),
				maxDimension,
			))
			if s.sendCachedThumbnail(w, r, thumbnailPath) {
				return nil
			}
		}
	}
	if thumbnailPath != "" {
		releaseLock, ok := s.acquireRenderLock(r.Context(), "archive-cover-source:"+thumbnailPath)
		if !ok {
			return r.Context().Err()
		}
		if thumbnailFile, thumbnailStat, ready := openCachedThumbnail(thumbnailPath); ready {
			releaseLock()
			defer thumbnailFile.Close()
			serveCachedThumbnailFile(w, r, thumbnailPath, thumbnailFile, thumbnailStat)
			return nil
		}
		data, contentType, err := loadSource()
		if err != nil {
			releaseLock()
			return err
		}
		started := time.Now()
		built, thumbnailErr := s.ensureThumbnailBytesToPathCached(r.Context(), data, thumbnailPath, maxDimension)
		appendServerTiming(w.Header(), "thumbnail", time.Since(started))
		thumbnailFile, thumbnailStat, ready := openCachedThumbnail(thumbnailPath)
		releaseLock()
		if built {
			w.Header().Set("X-Bmanga-Cache", "miss")
		}
		if thumbnailErr == nil && ready {
			defer thumbnailFile.Close()
			serveCachedThumbnailFile(w, r, thumbnailPath, thumbnailFile, thumbnailStat)
			return nil
		}
		if ready {
			_ = thumbnailFile.Close()
		}
		s.serveImageData(w, r, data, contentType, archivePath+"!"+innerPath, time.Time{}, maxDimension)
		return nil
	}

	data, contentType, err := loadSource()
	if err != nil {
		return err
	}
	s.serveImageData(w, r, data, contentType, archivePath+"!"+innerPath, time.Time{}, maxDimension)
	return nil
}
