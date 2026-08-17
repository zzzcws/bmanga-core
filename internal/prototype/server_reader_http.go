package prototype

import (
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handlePages(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	candidateID := strings.TrimSpace(r.URL.Query().Get("id"))
	if candidateID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing id")
		return
	}
	manifestStarted := time.Now()
	work, rows, manifest, err := s.getPageRows(r.Context(), candidateID)
	appendServerTiming(w.Header(), "pagesManifest", time.Since(manifestStarted))
	if err != nil {
		if errors.Is(err, errUnsupportedReaderSource) {
			writeJSONError(w, http.StatusUnsupportedMediaType, err.Error())
			return
		}
		if errors.Is(err, errArchiveResourceLimit) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, err.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if work == nil {
		writeJSONError(w, http.StatusNotFound, "work not found")
		return
	}
	pages := make([]map[string]any, 0, len(rows))
	for index, row := range rows {
		pages = append(pages, map[string]any{
			"index":         index,
			"relative_path": row["relative_path"],
			"extension":     row["extension"],
			"size_bytes":    row["size_bytes"],
		})
	}
	payload := map[string]any{
		"candidate_id":     candidateID,
		"work_identity_id": stringValue(work["work_identity_id"]),
		"page_manifest_id": "",
		"manifest_hash":    "",
		"readable":         len(rows) > 0,
		"count":            len(rows),
		"pages":            pages,
	}
	if manifest != nil {
		payload["page_manifest_id"] = manifest["page_manifest_id"]
		payload["manifest_hash"] = manifest["manifest_hash"]
	}
	writeJSON(w, payload)
}

func (s *Server) handlePageImage(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	candidateID := strings.TrimSpace(r.URL.Query().Get("id"))
	rawIndex := strings.TrimSpace(r.URL.Query().Get("index"))
	requestedManifestID := strings.TrimSpace(r.URL.Query().Get("manifest"))
	index, err := strconv.Atoi(rawIndex)
	maxDimension := clampInt(r.URL.Query().Get("max"), 0, 0, readerPageMaxDimension)
	if candidateID == "" {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	if rawIndex == "" || err != nil || index < 0 || index > 1_000_000 {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	work, row, manifest, _, err := s.getPageRow(r.Context(), candidateID, index, requestedManifestID)
	if err != nil {
		if errors.Is(err, errUnsupportedReaderSource) {
			writeJSONError(w, http.StatusUnsupportedMediaType, err.Error())
			return
		}
		if errors.Is(err, errPageManifestStale) {
			writeJSONStatus(w, http.StatusConflict, map[string]any{
				"error":           "页面清单已变化，请重新打开或校准进度",
				"progress_status": "manifest_stale",
			})
			return
		}
		if errors.Is(err, errArchiveResourceLimit) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, err.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if work == nil || row == nil {
		http.NotFound(w, r)
		return
	}
	if stringValue(row["source_type"]) == "archive_inner" {
		s.sendLibraryArchivePage(
			w, r,
			stringValue(row["library_key"]),
			stringValue(row["path"]),
			stringValue(row["source_relative_path"]),
			stringValue(row["source_inner_path"]),
			int64(intValue(row["size_bytes"])),
			stringValue(row["extension"]),
			maxDimension,
		)
		return
	}
	if stringValue(row["source_type"]) == "nested_archive_inner" {
		s.sendNestedArchivePage(
			w, r,
			stringValue(row["library_key"]),
			stringValue(row["path"]),
			stringValue(row["source_relative_path"]),
			stringValue(row["source_inner_path"]),
			int64(intValue(row["size_bytes"])),
			stringValue(row["extension"]),
			maxDimension,
		)
		return
	}
	if unsupportedReaderRowSourceType(stringValue(row["source_type"])) {
		http.NotFound(w, r)
		return
	}
	if stringValue(row["source_type"]) == "ebook_inner" {
		s.sendEbookPage(w, r, stringValue(row["library_key"]), stringValue(row["path"]), coalesceString(row["source_relative_path"], row["archive_relative_path"]), stringValue(row["source_inner_path"]), int64(intValue(row["size_bytes"])), stringValue(row["extension"]), maxDimension)
		return
	}
	if _, ok := row["page_index"]; !ok {
		row["page_index"] = index
	}
	s.sendSourcePageImage(w, r, work, row, manifest, maxDimension)
}

func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	candidateID := strings.TrimSpace(r.URL.Query().Get("id"))
	maxDimension := clampInt(r.URL.Query().Get("size"), 0, 0, 1600)
	if candidateID == "" {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	metadataStarted := time.Now()
	metadataRecorded := false
	recordMetadataTiming := func() {
		if metadataRecorded {
			return
		}
		metadataRecorded = true
		appendServerTiming(w.Header(), "coverMeta", time.Since(metadataStarted))
	}
	coverCandidate, err := s.readyCoverCandidate(candidateID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if coverCandidate != nil && !publicCoverCandidateSupported(coverCandidate) {
		http.NotFound(w, r)
		return
	}
	if coverCandidate != nil {
		asset, err := s.coverAssetForCandidate(candidateID, stringValue(coverCandidate["library_key"]))
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if asset != nil {
			recordMetadataTiming()
			if source, err := s.resolveLocalCachePath(stringValue(asset["cache_path"])); err == nil {
				s.serveImageFile(w, r, source, stringValue(asset["mime_type"]), maxDimension)
				return
			}
			if err := s.sendCoverAssetSourceImage(w, r, asset, maxDimension); err == nil {
				return
			}
		}
	}
	recordMetadataTiming()

	row := coverCandidate
	kind := stringValue(row["cover_kind"])
	if row == nil || stringValue(row["cover_source_path"]) == "" || !supportedCoverSourceKind(kind) {
		http.NotFound(w, r)
		return
	}
	if kind == "archive" || kind == "pdf" {
		if source, err := s.resolveLocalCachePath(stringValue(row["cover_source_path"])); err == nil {
			s.serveImageFile(w, r, source, "", maxDimension)
			return
		}
	}
	s.sendSourceImage(w, r, stringValue(row["library_key"]), stringValue(row["cover_source_path"]), stringValue(row["cover_source_relative_path"]), maxDimension)
}

func supportedCoverSourceKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "page_image", "archive", "ebook":
		return true
	default:
		return false
	}
}

func publicCoverCandidateSupported(candidate map[string]any) bool {
	if !supportedCoverSourceKind(stringValue(candidate["cover_kind"])) {
		return false
	}
	sourcePath := coalesceString(candidate["cover_source_relative_path"], candidate["cover_source_path"])
	switch stringValue(candidate["source_kind"]) {
	case "image_folder":
		return true
	case "archive":
		return isZipCBZExtension(filepath.Ext(sourcePath))
	case "ebook":
		return isEPUBExtension(filepath.Ext(sourcePath))
	default:
		return false
	}
}

func unsupportedReaderRowSourceType(sourceType string) bool {
	switch strings.TrimSpace(sourceType) {
	case "zip_pdf_page", "sevenzip_inner", "pdf_page":
		return true
	default:
		return false
	}
}
