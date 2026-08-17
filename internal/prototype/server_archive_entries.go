package prototype

import (
	"archive/zip"
	"bytes"
	textencoding "golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

func (s *Server) nestedZipPageRows(work map[string]any, archivePath string, files []*zip.File) ([]map[string]any, error) {
	nested := archiveNestedZipFiles(files)
	rows := []map[string]any{}
	for _, outer := range nested {
		outerName := displayArchiveEntryName(outer.Name)
		if s.archiveLimits.maxNestedDepth < 1 {
			return nil, archiveLimitError("nested archives are disabled")
		}
		innerBytes, err := readAllArchiveEntry(outer, s.archiveLimits.maxNestedBytes)
		if err != nil {
			return nil, err
		}
		innerSource := bytes.NewReader(innerBytes)
		if err := s.preflightZipDirectory(innerSource, int64(len(innerBytes))); err != nil {
			return nil, err
		}
		innerReader, err := zip.NewReader(innerSource, int64(len(innerBytes)))
		if err != nil {
			return nil, err
		}
		if err := s.validateZipEntries(innerReader.File, 1); err != nil {
			return nil, err
		}
		images := archiveImageFiles(innerReader.File)
		if err := s.validateReadablePageCount(len(rows) + len(images)); err != nil {
			return nil, err
		}
		for _, imageFile := range images {
			imageName := displayArchiveEntryName(imageFile.Name)
			innerPath := outerName + "!" + imageName
			rows = append(rows, map[string]any{
				"page_index":            len(rows),
				"library_key":           work["library_key"],
				"source_type":           "nested_archive_inner",
				"path":                  archivePath,
				"relative_path":         innerPath,
				"source_inner_path":     innerPath,
				"size_bytes":            int64(imageFile.UncompressedSize64),
				"extension":             archiveEntryExtension(imageName),
				"mime_type":             archiveEntryMIME(imageName),
				"archive_relative_path": work["relative_path"],
			})
		}
	}
	return rows, nil
}

func (s *Server) ebookPageRows(work map[string]any) ([]map[string]any, error) {
	if !isEPUBExtension(stringValue(work["extension"])) {
		return nil, errUnsupportedReaderSource
	}
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
	rows := make([]map[string]any, 0, len(files))
	for index, file := range files {
		entryName := displayArchiveEntryName(file.Name)
		rows = append(rows, map[string]any{
			"page_index":            index,
			"library_key":           work["library_key"],
			"source_type":           "ebook_inner",
			"path":                  archivePath,
			"relative_path":         entryName,
			"source_relative_path":  work["relative_path"],
			"source_inner_path":     entryName,
			"size_bytes":            int64(file.UncompressedSize64),
			"extension":             archiveEntryExtension(entryName),
			"mime_type":             archiveEntryMIME(entryName),
			"archive_relative_path": work["relative_path"],
		})
	}
	return rows, nil
}

func archiveImageFiles(files []*zip.File) []*zip.File {
	filtered := make([]*zip.File, 0, len(files))
	for _, file := range files {
		if isIgnoredArchiveEntry(file) {
			continue
		}
		if !isArchiveImageExtension(archiveEntryExtension(file.Name)) {
			continue
		}
		filtered = append(filtered, file)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return archiveEntryLess(filtered[i], filtered[j])
	})
	return filtered
}

func archiveNestedZipFiles(files []*zip.File) []*zip.File {
	filtered := make([]*zip.File, 0, len(files))
	for _, file := range files {
		if isIgnoredArchiveEntry(file) {
			continue
		}
		if !isZipCBZExtension(archiveEntryExtension(file.Name)) {
			continue
		}
		filtered = append(filtered, file)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return archiveEntryLess(filtered[i], filtered[j])
	})
	return filtered
}

func isIgnoredArchiveEntry(file *zip.File) bool {
	if file == nil {
		return true
	}
	name := normalizeArchiveEntryName(file.Name)
	if name == "" || strings.HasSuffix(name, "/") || file.FileInfo().IsDir() || file.UncompressedSize64 == 0 {
		return true
	}
	lower := strings.ToLower(name)
	if strings.Contains("/"+lower+"/", "/__macosx/") {
		return true
	}
	leaf := strings.ToLower(pathLeaf(name))
	return leaf == ".ds_store" || leaf == "thumbs.db"
}

func archiveEntryLess(left *zip.File, right *zip.File) bool {
	leftRank, leftDepth, leftNumber, leftName := archiveEntrySortKey(left.Name)
	rightRank, rightDepth, rightNumber, rightName := archiveEntrySortKey(right.Name)
	if leftRank != rightRank {
		return leftRank < rightRank
	}
	if leftDepth != rightDepth {
		return leftDepth < rightDepth
	}
	if leftNumber != rightNumber {
		return leftNumber < rightNumber
	}
	return leftName < rightName
}

func archiveEntrySortKey(name string) (int, int, int, string) {
	fullName := displayArchiveEntryName(name)
	leaf := pathLeaf(fullName)
	stem := strings.TrimSuffix(leaf, filepath.Ext(leaf))
	stem = strings.ToLower(stem)
	depth := 0
	for _, part := range strings.Split(fullName, "/") {
		if part != "" {
			depth++
		}
	}
	nameRank := 9
	numberRank := 99_999_999
	if archiveCoverNameRe.MatchString(stem) {
		nameRank = 0
	} else if match := archiveNumberNameRe.FindStringSubmatch(stem); len(match) > 1 {
		if parsed, err := strconv.Atoi(match[1]); err == nil {
			nameRank = 1
			numberRank = parsed
		}
	}
	return nameRank, depth, numberRank, strings.ToLower(fullName)
}

func normalizeArchiveEntryName(name string) string {
	return strings.TrimLeft(strings.ReplaceAll(name, `\`, "/"), "/")
}

func displayArchiveEntryName(name string) string {
	return normalizeArchiveEntryName(decodeArchiveEntryName(name))
}

func archiveCompatibilityFold(name string) string {
	return strings.ReplaceAll(name, "老", "老")
}

func decodeArchiveEntryName(name string) string {
	if utf8.ValidString(name) {
		return name
	}
	encodings := []textencoding.Encoding{
		japanese.ShiftJIS,
		simplifiedchinese.GBK,
		traditionalchinese.Big5,
	}
	for _, enc := range encodings {
		decoded, err := enc.NewDecoder().String(name)
		if err == nil && utf8.ValidString(decoded) && !strings.Contains(decoded, "\ufffd") {
			return decoded
		}
	}
	return name
}

func archiveEntryExtension(name string) string {
	return strings.ToLower(filepath.Ext(pathLeaf(normalizeArchiveEntryName(name))))
}

func pathLeaf(name string) string {
	name = normalizeArchiveEntryName(name)
	if index := strings.LastIndex(name, "/"); index >= 0 {
		return name[index+1:]
	}
	return name
}

func isArchiveImageExtension(extension string) bool {
	switch strings.ToLower(extension) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return true
	default:
		return false
	}
}

func isZipCBZExtension(extension string) bool {
	switch strings.ToLower(strings.TrimSpace(extension)) {
	case ".zip", ".cbz", "zip", "cbz":
		return true
	default:
		return false
	}
}

func isEPUBExtension(extension string) bool {
	switch strings.ToLower(strings.TrimSpace(extension)) {
	case ".epub", "epub", ".kepub", "kepub":
		return true
	default:
		return false
	}
}

func archiveEntryMIME(name string) string {
	switch archiveEntryExtension(name) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "application/octet-stream"
	}
}

const readyCoverCandidateQuery = `
		SELECT *
		FROM work_cover_candidates
		WHERE candidate_id = ?
`
