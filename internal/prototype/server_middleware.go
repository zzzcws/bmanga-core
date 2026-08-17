package prototype

import (
	"compress/gzip"
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func readerTimingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !readerTimingTrackedPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		tw := &readerTimingResponseWriter{ResponseWriter: w, started: started}
		next.ServeHTTP(tw, r)
		tw.finish(r)
	})
}

func readerTimingTrackedPath(path string) bool {
	switch path {
	case "/api/work", "/api/works", "/api/shelf", "/api/discover", "/api/random-work", "/api/reading-history", "/api/continue-target", "/api/series-detail", "/api/pages", "/api/progress", "/page", "/cover":
		return true
	default:
		return false
	}
}

type readerTimingResponseWriter struct {
	http.ResponseWriter
	started     time.Time
	status      int
	bytes       int64
	wroteHeader bool
}

func (w *readerTimingResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = statusCode
	appendServerTiming(w.Header(), "app", time.Since(w.started))
	w.ResponseWriter.WriteHeader(statusCode)
}

func appendServerTiming(header http.Header, name string, duration time.Duration) {
	part := fmt.Sprintf("%s;dur=%.1f", name, float64(duration.Microseconds())/1000.0)
	if existing := strings.TrimSpace(header.Get("Server-Timing")); existing != "" {
		header.Set("Server-Timing", existing+", "+part)
		return
	}
	header.Set("Server-Timing", part)
}

func (w *readerTimingResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += int64(n)
	return n, err
}

func (w *readerTimingResponseWriter) finish(r *http.Request) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	logSlowReaderRequest(r, w.status, w.bytes, time.Since(w.started))
}

func slowRequestLogThreshold() time.Duration {
	ms := envIntInRange("BMANGA_SLOW_REQUEST_LOG_MS", 1500, 0, 600000)
	return time.Duration(ms) * time.Millisecond
}

func logSlowReaderRequest(r *http.Request, status int, bytesWritten int64, duration time.Duration) {
	threshold := slowRequestLogThreshold()
	if !shouldLogReaderRequest(status, duration, threshold) {
		return
	}
	query := r.URL.Query()
	fmt.Fprintf(os.Stderr,
		"bmanga slow request method=%s path=%s status=%d dur_ms=%d bytes=%d id=%s index=%s max=%s manifest=%s\n",
		r.Method,
		r.URL.Path,
		status,
		duration.Milliseconds(),
		bytesWritten,
		shortLogValue(query.Get("id")),
		shortLogValue(query.Get("index")),
		shortLogValue(query.Get("max")),
		shortLogValue(query.Get("manifest")),
	)
}

func shouldLogReaderRequest(status int, duration time.Duration, threshold time.Duration) bool {
	return status >= http.StatusInternalServerError || (threshold > 0 && duration >= threshold)
}

func shortLogValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	value = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			return r
		}
		return '_'
	}, value)
	if len(value) <= 32 {
		return value
	}
	return value[:20] + "..." + value[len(value)-8:]
}

func gzipTextResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requestAcceptsGzip(r) || !gzipEligiblePath(r.URL.Path) || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{ResponseWriter: w}
		next.ServeHTTP(gw, r)
		if gw.writer != nil {
			_ = gw.writer.Close()
		}
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer      *gzip.Writer
	wroteHeader bool
}

func (w *gzipResponseWriter) WriteHeader(statusCode int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		if statusAllowsBody(statusCode) {
			header := w.Header()
			header.Add("Vary", "Accept-Encoding")
			if header.Get("Content-Encoding") == "" {
				header.Del("Content-Length")
				header.Set("Content-Encoding", "gzip")
				if w.writer == nil {
					w.writer = gzip.NewWriter(w.ResponseWriter)
				}
			}
		}
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *gzipResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.Header().Get("Content-Encoding") != "gzip" {
		return w.ResponseWriter.Write(data)
	}
	if w.writer == nil {
		return w.ResponseWriter.Write(data)
	}
	return w.writer.Write(data)
}

func requestAcceptsGzip(r *http.Request) bool {
	return requestAcceptsEncoding(r, "gzip")
}

func requestAcceptsEncoding(r *http.Request, encoding string) bool {
	encoding = strings.ToLower(strings.TrimSpace(encoding))
	if encoding == "" {
		return false
	}
	for _, value := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		parts := strings.Split(value, ";")
		name := strings.ToLower(strings.TrimSpace(parts[0]))
		if name != encoding && name != "*" {
			continue
		}
		allowed := true
		for _, param := range parts[1:] {
			param = strings.TrimSpace(param)
			if strings.HasPrefix(strings.ToLower(param), "q=") {
				if strings.TrimSpace(param[2:]) == "0" || strings.TrimSpace(param[2:]) == "0.0" {
					allowed = false
				}
			}
		}
		if allowed {
			return true
		}
	}
	return false
}

func gzipEligiblePath(path string) bool {
	if strings.HasPrefix(path, "/api/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".css", ".js", ".json", ".txt", ".svg":
		return true
	default:
		return path == "/" || path == ""
	}
}

func statusAllowsBody(statusCode int) bool {
	return statusCode >= 200 && statusCode != http.StatusNoContent && statusCode != http.StatusNotModified
}

const (
	csrfIntentHeaderName  = "X-Bmanga-Write"
	csrfIntentHeaderValue = "same-origin"
	csrfHeaderName        = "X-Bmanga-Write-Token"
	csrfBrowserStateName  = "bmanga_write_token"
	csrfQueryName         = "write_token"

	writeIntentHeader = csrfIntentHeaderName
	writeIntentValue  = csrfIntentHeaderValue
	writeTokenHeader  = csrfHeaderName
	writeTokenCookie  = csrfBrowserStateName
	writeTokenQuery   = csrfQueryName
)

func (s *Server) sameOriginWriteGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWriteMethod(r.Method) && !s.sameOriginWriteAllowed(r) {
			writeJSONError(w, http.StatusForbidden, "cross-origin write blocked")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func (s *Server) sameOriginWriteAllowed(r *http.Request) bool {
	source := strings.TrimSpace(r.Header.Get("Origin"))
	if strings.EqualFold(source, "null") {
		return false
	}
	if source == "" {
		source = strings.TrimSpace(r.Header.Get("Referer"))
	}
	if source == "" {
		return trustedBrowserWrite(r)
	}
	sourceHost, sourcePort, ok := normalizedOriginHost(source, "http")
	if !ok {
		return false
	}
	if s.allowedOriginHost(sourceHost, sourcePort) {
		return trustedBrowserWrite(r)
	}
	requestHostHeader := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if requestHostHeader == "" {
		requestHostHeader = r.Host
	}
	requestScheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if requestScheme == "" {
		requestScheme = "http"
	}
	requestHost, requestPort, ok := normalizedOriginHost(requestHostHeader, requestScheme)
	if !ok {
		return false
	}
	return sourceHost == requestHost && sourcePort == requestPort && trustedBrowserWrite(r)
}

func (s *Server) allowedOriginHost(sourceHost, sourcePort string) bool {
	items := strings.FieldsFunc(os.Getenv("BMANGA_ALLOWED_ORIGINS"), func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	for _, item := range items {
		allowedHost, allowedPort, ok := normalizedOriginHost(strings.TrimSpace(item), "https")
		if ok && allowedHost == sourceHost && allowedPort == sourcePort {
			return true
		}
	}
	return false
}

func trustedWriteHeader(r *http.Request) bool {
	value := strings.TrimSpace(r.Header.Get(writeIntentHeader))
	return strings.EqualFold(value, writeIntentValue) || value == "1"
}

func trustedBrowserWrite(r *http.Request) bool {
	cookie, err := r.Cookie(writeTokenCookie)
	if err == nil && strings.TrimSpace(cookie.Value) != "" {
		token := strings.TrimSpace(r.Header.Get(writeTokenHeader))
		if token == "" && r.URL != nil && r.URL.Path == "/api/progress" {
			token = strings.TrimSpace(r.URL.Query().Get(writeTokenQuery))
		}
		return subtle.ConstantTimeCompare([]byte(token), []byte(strings.TrimSpace(cookie.Value))) == 1
	}
	return trustedWriteHeader(r)
}

func normalizedOriginHost(value string, fallbackScheme string) (string, string, bool) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return "", "", false
	}
	if !strings.Contains(raw, "://") {
		raw = fallbackScheme + "://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", "", false
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return host, port, true
}
