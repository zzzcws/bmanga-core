package prototype

import (
	"encoding/json"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

func (s *Server) resolveSourcePath(libraryKey, sourcePath, relativePath string) (string, error) {
	resolved, _, err := s.resolveSourcePathWithRoot(libraryKey, sourcePath, relativePath)
	return resolved, err
}

func (s *Server) resolveSourcePathWithRoot(libraryKey, sourcePath, relativePath string) (string, string, error) {
	if libraryKey == "" || sourcePath == "" {
		return "", "", os.ErrNotExist
	}
	if hasIgnoredDir(relativePath) || hasIgnoredDir(sourcePath) {
		return "", "", os.ErrPermission
	}
	rows, err := s.query("SELECT root_path FROM libraries WHERE key = ?", libraryKey)
	if err != nil {
		return "", "", err
	}
	if len(rows) == 0 || stringValue(rows[0]["root_path"]) == "" {
		return "", "", os.ErrNotExist
	}
	rootPath := s.remapRuntimePath(stringValue(rows[0]["root_path"]))
	sourcePath = s.remapRuntimePath(sourcePath)
	root := normalizePathForBoundary(rootPath)
	source := normalizePathForBoundary(sourcePath)
	if !pathEqualOrUnderNorm(source, root) {
		return "", "", os.ErrPermission
	}
	resolved, err := s.resolvePathUnderRoot(sourcePath, rootPath)
	if err != nil {
		return "", "", err
	}
	return resolved, rootPath, nil
}

func (s *Server) resolveLocalCachePath(cachePath string) (string, error) {
	cachePath = s.remapRuntimePath(cachePath)
	if cachePath == "" || !filepath.IsAbs(cachePath) {
		return "", os.ErrPermission
	}
	return s.resolvePathUnderRoot(cachePath, s.localCacheRoot)
}

func (s *Server) resolvePathUnderRoot(targetPath string, rootPath string) (string, error) {
	targetPath = s.remapRuntimePath(targetPath)
	rootPath = s.remapRuntimePath(rootPath)
	if targetPath == "" || !filepath.IsAbs(targetPath) {
		return "", os.ErrPermission
	}
	sourceAbs, err := filepath.Abs(targetPath)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(rootPath)
	if err != nil {
		return "", err
	}
	sourceNorm := normalizePathForBoundary(sourceAbs)
	rootNorm := normalizePathForBoundary(rootAbs)
	if !pathEqualOrUnderNorm(sourceNorm, rootNorm) {
		return "", os.ErrPermission
	}
	if problem := pathReparseAncestorProblem(filepath.Dir(sourceAbs), rootAbs); problem != "" {
		return "", os.ErrPermission
	}
	info, err := os.Lstat(sourceAbs)
	if err != nil {
		return "", err
	}
	if fileInfoIsReparsePoint(info) {
		return "", os.ErrPermission
	}
	if info.IsDir() {
		return "", os.ErrNotExist
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", err
	}
	resolvedSource, err := filepath.EvalSymlinks(sourceAbs)
	if err != nil {
		return "", err
	}
	resolvedSourceNorm := normalizePathForBoundary(resolvedSource)
	resolvedRootNorm := normalizePathForBoundary(resolvedRoot)
	if !pathEqualOrUnderNorm(resolvedSourceNorm, resolvedRootNorm) {
		return "", os.ErrPermission
	}
	return sourceAbs, nil
}

func pathReparseAncestorProblem(path, root string) string {
	rootClean := normalizePathForBoundary(root)
	current := filepath.Clean(path)
	if currentClean := normalizePathForBoundary(current); !pathEqualOrUnderNorm(currentClean, rootClean) {
		return "path_outside_root"
	}
	ancestors := []string{}
	for {
		ancestors = append(ancestors, current)
		if normalizePathForBoundary(current) == rootClean {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
		if currentClean := normalizePathForBoundary(current); !pathEqualOrUnderNorm(currentClean, rootClean) {
			return "path_outside_root"
		}
	}
	for i := len(ancestors) - 1; i >= 0; i-- {
		info, err := os.Lstat(ancestors[i])
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "reparse_check_failed: " + err.Error()
		}
		if fileInfoIsReparsePoint(info) {
			return "reparse_point: " + ancestors[i]
		}
	}
	return ""
}

func isUnderPathRoot(targetPath string, rootPath string) bool {
	if targetPath == "" || !filepath.IsAbs(targetPath) {
		return false
	}
	sourceAbs, err := filepath.Abs(targetPath)
	if err != nil {
		return false
	}
	rootAbs, err := filepath.Abs(rootPath)
	if err != nil {
		return false
	}
	sourceNorm := normalizePathForBoundary(sourceAbs)
	rootNorm := normalizePathForBoundary(rootAbs)
	return pathEqualOrUnderNorm(sourceNorm, rootNorm)
}

func allowedImageMIME(contentType string) bool {
	switch strings.ToLower(strings.Split(contentType, ";")[0]) {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}

func hasIgnoredDir(value string) bool {
	for _, part := range regexp.MustCompile(`[\\/]+`).Split(value, -1) {
		switch strings.ToLower(part) {
		case "#recycle", "@eadir", "@synoresource", "thumb":
			return true
		}
	}
	return false
}

func normalizePathForBoundary(value string) string {
	return normalizePathForBoundaryForOS(value, runtime.GOOS)
}

func normalizePathForBoundaryForOS(value string, goos string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, `\`, `/`)
	cleaned := pathpkg.Clean(value)
	if cleaned == "." {
		return ""
	}
	if cleaned != "/" {
		cleaned = strings.TrimRight(cleaned, "/")
	}
	if strings.EqualFold(strings.TrimSpace(goos), "windows") {
		return strings.ToLower(cleaned)
	}
	return cleaned
}

func pathEqualOrUnderNorm(candidateNorm string, rootNorm string) bool {
	if candidateNorm == "" || rootNorm == "" {
		return false
	}
	if candidateNorm == rootNorm {
		return true
	}
	if rootNorm == "/" {
		return strings.HasPrefix(candidateNorm, "/")
	}
	return strings.HasPrefix(candidateNorm, rootNorm+"/")
}

func loadPathMappingsFromEnv() ([]pathMapping, error) {
	raw := strings.TrimSpace(os.Getenv("BMANGA_PATH_MAP"))
	if raw == "" {
		return nil, nil
	}
	var entries []pathMapping
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("invalid BMANGA_PATH_MAP: %w", err)
	}
	mappings := []pathMapping{}
	for _, entry := range entries {
		entry.From = strings.TrimSpace(entry.From)
		entry.To = strings.TrimSpace(entry.To)
		if entry.From == "" || entry.To == "" {
			continue
		}
		mappings = append(mappings, entry)
	}
	sort.SliceStable(mappings, func(i, j int) bool {
		return len(normalizePathForBoundary(mappings[i].From)) > len(normalizePathForBoundary(mappings[j].From))
	})
	return mappings, nil
}

func (s *Server) remapRuntimePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(s.pathMappings) == 0 {
		return value
	}
	candidateNorm := normalizePathForBoundary(value)
	candidateSlash := slashClean(value)
	for _, mapping := range s.pathMappings {
		fromNorm := normalizePathForBoundary(mapping.From)
		if !pathEqualOrUnderNorm(candidateNorm, fromNorm) {
			continue
		}
		fromSlash := slashClean(mapping.From)
		suffix := ""
		if candidateNorm != fromNorm && len(candidateSlash) >= len(fromSlash) {
			suffix = strings.TrimLeft(candidateSlash[len(fromSlash):], "/")
		}
		return joinMappedPath(mapping.To, suffix)
	}
	return value
}

func slashClean(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, `\`, `/`)
	cleaned := pathpkg.Clean(value)
	if cleaned == "." {
		return ""
	}
	if cleaned != "/" {
		cleaned = strings.TrimRight(cleaned, "/")
	}
	return cleaned
}

func joinMappedPath(root string, slashSuffix string) string {
	root = strings.TrimSpace(root)
	if slashSuffix == "" {
		return root
	}
	result := root
	for _, part := range strings.Split(slashSuffix, "/") {
		if part == "" || part == "." {
			continue
		}
		result = filepath.Join(result, part)
	}
	return result
}
