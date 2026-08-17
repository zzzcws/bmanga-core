package prototype

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

func (s *Server) getCurrentManifest(candidateID string) (map[string]any, error) {
	rows, err := s.query(`
		SELECT
			pm.page_manifest_id,
			pm.work_identity_id,
			pm.candidate_id,
			pm.manifest_hash,
			pm.page_count,
			pm.manifest_status,
			pm.builder_version,
			pm.built_at
		FROM page_manifests pm
		JOIN work_identities wi ON wi.work_identity_id = pm.work_identity_id
		WHERE wi.current_candidate_id = ?
		  AND pm.manifest_status = 'ready'
		ORDER BY pm.built_at DESC
		LIMIT 1
	`, candidateID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (s *Server) currentManifestForCandidate(ctx context.Context, candidateID string) (map[string]any, error) {
	workRows, err := s.query("SELECT * FROM work_browse WHERE candidate_id = ?", candidateID)
	if err != nil || len(workRows) == 0 {
		return nil, err
	}
	return s.currentManifestForWork(ctx, workRows[0])
}

func (s *Server) currentManifestForWork(ctx context.Context, work map[string]any) (map[string]any, error) {
	if !publicReaderSourceSupported(work) {
		return s.getCurrentManifest(stringValue(work["candidate_id"]))
	}
	_, manifest, err := s.pageRowsAndManifestForWork(ctx, work, false)
	return manifest, err
}

func (s *Server) pageRowsAndManifestForWork(ctx context.Context, work map[string]any, includeRows bool) ([]map[string]any, map[string]any, error) {
	if !publicReaderSourceSupported(work) {
		return nil, nil, errUnsupportedReaderSource
	}
	rows, manifest, ok, err := s.storedPageRowsAndManifestForWork(work)
	if err != nil {
		return nil, nil, err
	}
	if ok {
		if includeRows {
			return rows, manifest, nil
		}
		return nil, manifest, nil
	}

	if pageManifestDiscoveryNeedsLock(work) {
		candidateID := stringValue(work["candidate_id"])
		releaseLock, acquired := s.acquireRenderLock(ctx, "page-manifest-discovery:"+candidateID)
		if !acquired {
			return nil, nil, ctx.Err()
		}
		defer releaseLock()

		rows, manifest, ok, err = s.storedPageRowsAndManifestForWork(work)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			if includeRows {
				return rows, manifest, nil
			}
			return nil, manifest, nil
		}
	}

	switch stringValue(work["source_kind"]) {
	case "archive":
		rows, err := s.dynamicArchiveRows(work)
		if err != nil {
			return nil, nil, err
		}
		manifest, err := s.ensurePageManifest(ctx, work, rows)
		if err != nil {
			return nil, nil, err
		}
		if includeRows {
			return rows, manifest, nil
		}
		return nil, manifest, nil
	case "ebook":
		rows, err := s.ebookPageRows(work)
		if err != nil {
			return nil, nil, err
		}
		manifest, err := s.ensurePageManifest(ctx, work, rows)
		if err != nil {
			return nil, nil, err
		}
		if includeRows {
			return rows, manifest, nil
		}
		return nil, manifest, nil
	default:
		if includeRows {
			return s.imageFolderPageRows(work, manifest)
		}
		return nil, manifest, nil
	}
}

func (s *Server) storedPageRowsAndManifestForWork(work map[string]any) ([]map[string]any, map[string]any, bool, error) {
	candidateID := stringValue(work["candidate_id"])
	manifest, err := s.getCurrentManifest(candidateID)
	if err != nil || manifest == nil {
		return nil, nil, false, err
	}
	if shouldRebuildStoredManifest(work, manifest) {
		_ = s.markManifestStale(manifest)
		return nil, nil, false, nil
	}
	rows, err := s.storedManifestRows(work, manifest)
	if err != nil {
		return nil, nil, false, err
	}
	if !publicStoredManifestRowsSupported(rows) {
		_ = s.markManifestStale(manifest)
		return nil, nil, false, nil
	}
	if !shouldValidateStoredManifest(work) || storedManifestMatches(work, rows, manifest) {
		return rows, manifest, true, nil
	}
	_ = s.markManifestStale(manifest)
	return nil, nil, false, nil
}

func pageManifestDiscoveryNeedsLock(work map[string]any) bool {
	switch stringValue(work["source_kind"]) {
	case "archive", "ebook":
		return true
	default:
		return false
	}
}

func virtualPageManifest(work map[string]any, rows []map[string]any) map[string]any {
	if len(rows) == 0 {
		return nil
	}
	hasher := sha256.New()
	sourcePath := stringValue(work["path"])
	_, _ = fmt.Fprintf(
		hasher,
		"%s\x00%s\x00%s\x00%d\n",
		stringValue(work["source_kind"]),
		stringValue(work["candidate_id"]),
		sourcePath,
		len(rows),
	)
	if stat, err := os.Stat(sourcePath); err == nil {
		_, _ = fmt.Fprintf(hasher, "%d\x00%d\n", stat.Size(), stat.ModTime().UnixNano())
	}
	for _, row := range rows {
		_, _ = fmt.Fprintf(
			hasher,
			"%d\x00%s\x00%s\x00%s\x00%d\n",
			intValue(row["page_index"]),
			stringValue(row["relative_path"]),
			stringValue(row["source_inner_path"]),
			stringValue(row["extension"]),
			intValue(row["size_bytes"]),
		)
	}
	manifestHash := hex.EncodeToString(hasher.Sum(nil))
	manifestID := "virtual:" + manifestHash[:24]
	return map[string]any{
		"page_manifest_id": manifestID,
		"work_identity_id": stringValue(work["work_identity_id"]),
		"candidate_id":     stringValue(work["candidate_id"]),
		"manifest_hash":    manifestHash,
		"page_count":       len(rows),
		"manifest_status":  "ready",
		"built_at":         "",
	}
}

func (s *Server) getPageRows(ctx context.Context, candidateID string) (map[string]any, []map[string]any, map[string]any, error) {
	workRows, err := s.query("SELECT * FROM work_browse WHERE candidate_id = ?", candidateID)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(workRows) == 0 {
		return nil, nil, nil, nil
	}
	work := workRows[0]
	rows, manifest, err := s.pageRowsAndManifestForWork(ctx, work, true)
	if err != nil {
		return nil, nil, nil, err
	}
	return work, rows, manifest, nil
}

func (s *Server) getPageRow(ctx context.Context, candidateID string, index int, requestedManifestID string) (map[string]any, map[string]any, map[string]any, int, error) {
	workRows, err := s.query("SELECT * FROM work_browse WHERE candidate_id = ?", candidateID)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	if len(workRows) == 0 {
		return nil, nil, nil, 0, nil
	}
	work := workRows[0]
	manifest, err := s.getCurrentManifest(candidateID)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	if manifest != nil && shouldRebuildStoredManifest(work, manifest) {
		_ = s.markManifestStale(manifest)
		manifest = nil
	}
	if requestedManifestID != "" && manifest != nil && requestedManifestID != stringValue(manifest["page_manifest_id"]) {
		return nil, nil, nil, 0, errPageManifestStale
	}
	if manifest != nil {
		pageCount := intValue(manifest["page_count"])
		if index >= pageCount && pageCount > 0 {
			return work, nil, manifest, pageCount, nil
		}
		rows, err := s.query(`
			SELECT
				page_index,
				library_key,
				source_path AS path,
				source_relative_path,
				source_inner_path,
				extension,
				mime_type,
				size_bytes,
				modified_utc,
				quick_hash
			FROM page_manifest_items
			WHERE page_manifest_id = ?
			  AND page_index = ?
			LIMIT 1
		`, manifest["page_manifest_id"], index)
		if err != nil {
			return nil, nil, nil, 0, err
		}
		if len(rows) > 0 {
			decorateStoredManifestRow(work, rows[0])
			return work, rows[0], manifest, pageCount, nil
		}
	}

	rows, manifest, err := s.pageRowsAndManifestForWork(ctx, work, true)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	if requestedManifestID != "" && manifest != nil && requestedManifestID != stringValue(manifest["page_manifest_id"]) {
		return nil, nil, nil, 0, errPageManifestStale
	}
	if index >= len(rows) {
		return work, nil, manifest, len(rows), nil
	}
	return work, rows[index], manifest, len(rows), nil
}

func (s *Server) imageFolderPageRows(work map[string]any, manifest map[string]any) ([]map[string]any, map[string]any, error) {
	candidateID := stringValue(work["candidate_id"])
	if manifest == nil {
		var err error
		manifest, err = s.getCurrentManifest(candidateID)
		if err != nil {
			return nil, nil, err
		}
	}
	if manifest != nil {
		rows, err := s.storedManifestRows(work, manifest)
		return rows, manifest, err
	}
	normalizedRelativePath := strings.Trim(strings.ReplaceAll(stringValue(work["relative_path"]), `\`, "/"), "/")
	prefix := normalizedRelativePath + "/"
	rows, err := s.query(`
		SELECT library_key, 'file' AS source_type, path, relative_path, '' AS source_inner_path, size_bytes, extension
		FROM scan_entries
		WHERE library_key = ?
		  AND item_kind = 'image_file'
		  AND status = 'indexed_as_page'
		  AND substr(REPLACE(relative_path, char(92), '/'), 1, ?) = ?
		ORDER BY REPLACE(relative_path, char(92), '/')
	`, work["library_key"], len([]rune(prefix)), prefix)
	return rows, nil, err
}

func shouldValidateStoredManifest(work map[string]any) bool {
	switch stringValue(work["source_kind"]) {
	case "archive":
		return isZipCBZExtension(stringValue(work["extension"]))
	case "ebook":
		return isEPUBExtension(stringValue(work["extension"]))
	default:
		return false
	}
}

func shouldRebuildStoredManifest(work map[string]any, manifest map[string]any) bool {
	if !shouldValidateStoredManifest(work) {
		return false
	}
	builderVersion := stringValue(manifest["builder_version"])
	return strings.HasPrefix(builderVersion, "go-reader-manifest-") && builderVersion != readerManifestVersion
}

func storedManifestMatches(work map[string]any, rows []map[string]any, manifest map[string]any) bool {
	expected := virtualPageManifest(work, rows)
	if expected == nil {
		return false
	}
	return stringValue(expected["manifest_hash"]) == stringValue(manifest["manifest_hash"]) &&
		intValue(expected["page_count"]) == intValue(manifest["page_count"])
}

func (s *Server) markManifestStale(manifest map[string]any) error {
	pageManifestID := stringValue(manifest["page_manifest_id"])
	if pageManifestID == "" {
		return nil
	}
	_, err := s.db.Exec(`
		UPDATE page_manifests
		SET manifest_status = 'stale'
		WHERE page_manifest_id = ?
		  AND manifest_status = 'ready'
	`, pageManifestID)
	return err
}

func (s *Server) dynamicArchiveRows(work map[string]any) ([]map[string]any, error) {
	if isZipCBZExtension(stringValue(work["extension"])) {
		return s.archivePageRows(work)
	}
	return []map[string]any{}, nil
}

func (s *Server) ensurePageManifest(ctx context.Context, work map[string]any, rows []map[string]any) (map[string]any, error) {
	manifest := virtualPageManifest(work, rows)
	if manifest == nil {
		return nil, nil
	}
	manifestHash := stringValue(manifest["manifest_hash"])
	workIdentityID := stringValue(work["work_identity_id"])
	if manifestHash == "" || workIdentityID == "" {
		return manifest, nil
	}
	releaseLock, ok := s.acquireRenderLock(
		ctx,
		"page-manifest:"+workIdentityID+":"+manifestHash,
	)
	if !ok {
		return nil, ctx.Err()
	}
	defer releaseLock()

	existing, err := s.query(`
		SELECT page_manifest_id, work_identity_id, candidate_id, manifest_hash, page_count, manifest_status, builder_version, built_at
		FROM page_manifests
		WHERE work_identity_id = ?
		  AND manifest_hash = ?
		LIMIT 1
	`, workIdentityID, manifestHash)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		pageManifestID := stringValue(existing[0]["page_manifest_id"])
		if stringValue(existing[0]["manifest_status"]) != "ready" || stringValue(existing[0]["builder_version"]) != readerManifestVersion {
			if _, err := s.db.Exec(`
				UPDATE page_manifests
				SET manifest_status = 'ready', candidate_id = ?, builder_version = ?, built_at = ?
				WHERE page_manifest_id = ?
			`, stringValue(work["candidate_id"]), readerManifestVersion, nowISO(), pageManifestID); err != nil {
				return nil, err
			}
			existing[0]["manifest_status"] = "ready"
			existing[0]["candidate_id"] = stringValue(work["candidate_id"])
			existing[0]["builder_version"] = readerManifestVersion
		}
		if err := s.ensurePageManifestItems(pageManifestID, work, rows); err != nil {
			return nil, err
		}
		return existing[0], nil
	}

	pageManifestID := "pm:" + manifestHash[:32]
	now := nowISO()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.Exec(`
		INSERT INTO page_manifests (
			page_manifest_id, work_identity_id, candidate_id, manifest_hash,
			page_count, source_kind, manifest_status, builder_version, built_at
		)
		VALUES (?, ?, ?, ?, ?, ?, 'ready', ?, ?)
	`, pageManifestID, workIdentityID, stringValue(work["candidate_id"]), manifestHash, len(rows), stringValue(work["source_kind"]), readerManifestVersion, now); err != nil {
		return nil, err
	}
	if err := insertPageManifestItems(tx, pageManifestID, work, rows); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true

	manifest["page_manifest_id"] = pageManifestID
	manifest["built_at"] = now
	return manifest, nil
}

func (s *Server) ensurePageManifestItems(pageManifestID string, work map[string]any, rows []map[string]any) error {
	if pageManifestID == "" {
		return nil
	}
	countRows, err := s.query("SELECT COUNT(*) AS count FROM page_manifest_items WHERE page_manifest_id = ?", pageManifestID)
	if err != nil {
		return err
	}
	if len(countRows) > 0 && intValue(countRows[0]["count"]) == len(rows) {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.Exec("DELETE FROM page_manifest_items WHERE page_manifest_id = ?", pageManifestID); err != nil {
		return err
	}
	if err := insertPageManifestItems(tx, pageManifestID, work, rows); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func insertPageManifestItems(tx *sql.Tx, pageManifestID string, work map[string]any, rows []map[string]any) error {
	stmt, err := tx.Prepare(`
		INSERT INTO page_manifest_items (
			page_manifest_id, page_index, library_key, source_path, source_relative_path,
			source_inner_path, extension, mime_type, size_bytes, modified_utc, quick_hash
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for index, row := range rows {
		pageIndex := intValue(row["page_index"])
		if pageIndex == 0 && index != 0 {
			pageIndex = index
		}
		if _, err := stmt.Exec(
			pageManifestID,
			pageIndex,
			stringValue(row["library_key"]),
			stringValue(row["path"]),
			manifestItemSourceRelativePath(work, row),
			stringValue(row["source_inner_path"]),
			stringValue(row["extension"]),
			stringValue(row["mime_type"]),
			intValue(row["size_bytes"]),
			stringValue(row["modified_utc"]),
			stringValue(row["quick_hash"]),
		); err != nil {
			return err
		}
	}
	return nil
}

func manifestItemSourceRelativePath(work map[string]any, row map[string]any) string {
	if stringValue(work["source_kind"]) == "image_folder" {
		return coalesceString(row["source_relative_path"], row["relative_path"])
	}
	return stringValue(work["relative_path"])
}

func (s *Server) storedManifestRows(work map[string]any, manifest map[string]any) ([]map[string]any, error) {
	rows, err := s.query(`
		SELECT
			page_index,
			library_key,
			source_path AS path,
			source_relative_path,
			source_inner_path,
			extension,
			mime_type,
			size_bytes,
			modified_utc,
			quick_hash
		FROM page_manifest_items
		WHERE page_manifest_id = ?
		ORDER BY page_index
	`, manifest["page_manifest_id"])
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		decorateStoredManifestRow(work, row)
	}
	return rows, nil
}

func decorateStoredManifestRow(work map[string]any, row map[string]any) {
	switch stringValue(work["source_kind"]) {
	case "archive":
		if strings.Contains(stringValue(row["source_inner_path"]), "!") {
			row["source_type"] = "nested_archive_inner"
		} else {
			row["source_type"] = "archive_inner"
		}
		row["relative_path"] = stringValue(row["source_inner_path"])
		row["archive_relative_path"] = stringValue(row["source_relative_path"])
	case "ebook":
		row["source_type"] = "ebook_inner"
		row["relative_path"] = stringValue(row["source_inner_path"])
		row["archive_relative_path"] = stringValue(row["source_relative_path"])
		if stringValue(row["extension"]) == "" {
			row["extension"] = archiveEntryExtension(stringValue(row["source_inner_path"]))
		}
		if stringValue(row["mime_type"]) == "" {
			row["mime_type"] = archiveEntryMIME(stringValue(row["source_inner_path"]))
		}
	default:
		row["source_type"] = "file"
		row["relative_path"] = stringValue(row["source_relative_path"])
	}
}

func (s *Server) archivePageRows(work map[string]any) ([]map[string]any, error) {
	reader, err := s.openPinnedLibraryZipArchive(
		stringValue(work["library_key"]),
		stringValue(work["path"]),
		stringValue(work["relative_path"]),
	)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	archivePath := reader.source.path

	files := archiveImageFiles(reader.File)
	if err := s.validateReadablePageCount(len(files)); err != nil {
		return nil, err
	}
	if len(files) == 0 && len(archiveNestedZipFiles(reader.File)) > 0 {
		return s.nestedZipPageRows(work, archivePath, reader.File)
	}
	rows := make([]map[string]any, 0, len(files))
	for index, file := range files {
		entryName := displayArchiveEntryName(file.Name)
		rows = append(rows, map[string]any{
			"page_index":            index,
			"library_key":           work["library_key"],
			"source_type":           "archive_inner",
			"path":                  archivePath,
			"relative_path":         entryName,
			"source_inner_path":     entryName,
			"size_bytes":            int64(file.UncompressedSize64),
			"extension":             archiveEntryExtension(entryName),
			"mime_type":             archiveEntryMIME(entryName),
			"archive_relative_path": work["relative_path"],
		})
	}
	return rows, nil
}

func publicStoredManifestRowsSupported(rows []map[string]any) bool {
	for _, row := range rows {
		innerPath := strings.ToLower(strings.TrimSpace(stringValue(row["source_inner_path"])))
		if strings.Contains(innerPath, "#page=") || strings.HasPrefix(innerPath, "pdf_page_") {
			return false
		}
	}
	return true
}
