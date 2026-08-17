package prototype

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	relative := staticRelativePath(r.URL.Path)
	webRoot := filepath.Join(s.root, "web")
	target := filepath.Join(webRoot, filepath.FromSlash(relative))
	absTarget, err := filepath.Abs(target)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rootNorm := normalizePathForBoundary(webRoot)
	targetNorm := normalizePathForBoundary(absTarget)
	if !pathEqualOrUnderNorm(targetNorm, rootNorm) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	info, err := os.Stat(absTarget)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(absTarget)))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	baseType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if strings.HasSuffix(baseType, "javascript") || strings.HasPrefix(baseType, "text/") {
		contentType = baseType + "; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", staticCacheControl(relative))
	if s.servePrecompressedStatic(w, r, absTarget) {
		return
	}
	http.ServeFile(w, r, absTarget)
}

func staticRelativePath(requestPath string) string {
	relative := strings.TrimPrefix(requestPath, "/")
	if relative == "" {
		return "index.html"
	}
	if relative == "v2" || relative == "v2/" {
		return "v2/index.html"
	}
	if strings.HasPrefix(relative, "v2/") {
		tail := strings.TrimPrefix(relative, "v2/")
		base := filepath.Base(filepath.FromSlash(tail))
		if tail == "" || (filepath.Ext(base) == "" && !strings.HasPrefix(tail, "assets/")) {
			return "v2/index.html"
		}
	}
	return relative
}

func (s *Server) servePrecompressedStatic(w http.ResponseWriter, r *http.Request, absTarget string) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if r.Header.Get("Range") != "" {
		return false
	}
	for _, candidate := range []struct {
		encoding string
		suffix   string
	}{
		{encoding: "br", suffix: ".br"},
		{encoding: "gzip", suffix: ".gz"},
	} {
		if !requestAcceptsEncoding(r, candidate.encoding) {
			continue
		}
		encodedPath := absTarget + candidate.suffix
		file, err := os.Open(encodedPath)
		if err != nil {
			continue
		}
		stat, err := file.Stat()
		if err != nil || stat.IsDir() {
			_ = file.Close()
			continue
		}
		w.Header().Set("Content-Encoding", candidate.encoding)
		w.Header().Add("Vary", "Accept-Encoding")
		http.ServeContent(w, r, filepath.Base(absTarget), stat.ModTime(), file)
		_ = file.Close()
		return true
	}
	return false
}

func staticCacheControl(relative string) string {
	name := strings.ToLower(filepath.Base(relative))
	if name == "index.html" || name == "" {
		return "no-cache"
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".js", ".css":
		return "public, max-age=604800, immutable"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".svg":
		return "public, max-age=86400"
	default:
		return "no-cache"
	}
}
