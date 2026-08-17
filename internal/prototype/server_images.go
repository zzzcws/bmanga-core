package prototype

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"golang.org/x/image/draw"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/jpeg"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func imageETag(size int64, modTime time.Time) string {
	return fmt.Sprintf(`W/"%x-%x"`, size, modTime.UnixNano())
}

func etagMatches(r *http.Request, etag string) bool {
	if etag == "" {
		return false
	}
	for _, candidate := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == etag || candidate == "*" {
			return true
		}
	}
	return false
}

func (s *Server) isLocalGeneratedImagePath(source string) bool {
	for _, root := range []string{
		s.localCacheRoot,
		s.thumbnailCacheRoot,
	} {
		if isUnderPathRoot(source, root) {
			return true
		}
	}
	return false
}

func writeImageCacheHeaders(w http.ResponseWriter, r *http.Request, cacheControl string, etag string) bool {
	w.Header().Set("Cache-Control", cacheControl)
	if etag != "" {
		w.Header().Set("ETag", etag)
		if etagMatches(r, etag) {
			w.Header().Del("Content-Length")
			w.WriteHeader(http.StatusNotModified)
			return true
		}
	}
	return false
}

func (s *Server) serveImageFile(w http.ResponseWriter, r *http.Request, source string, preferredType string, maxDimension int) {
	contentType := preferredType
	if contentType == "" {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(source)))
	}
	if contentType == "" && strings.EqualFold(filepath.Ext(source), ".webp") {
		contentType = "image/webp"
	}
	if !allowedImageMIME(contentType) {
		http.Error(w, http.StatusText(http.StatusUnsupportedMediaType), http.StatusUnsupportedMediaType)
		return
	}
	if maxDimension > 0 && s.sendThumbnail(w, r, source, maxDimension) {
		return
	}
	file, err := os.Open(source)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	cacheControl := "public, max-age=3600"
	if s.isLocalGeneratedImagePath(source) {
		cacheControl = "public, max-age=86400, immutable"
	}
	if writeImageCacheHeaders(w, r, cacheControl, imageETag(stat.Size(), stat.ModTime())) {
		return
	}
	http.ServeContent(w, r, filepath.Base(source), stat.ModTime(), file)
}

func (s *Server) serveImageData(w http.ResponseWriter, r *http.Request, data []byte, contentType string, cacheKey string, modTime time.Time, maxDimension int) {
	if !allowedImageMIME(contentType) {
		http.Error(w, http.StatusText(http.StatusUnsupportedMediaType), http.StatusUnsupportedMediaType)
		return
	}
	if maxDimension > 0 && s.sendThumbnailBytes(w, r, data, contentType, cacheKey, modTime, maxDimension) {
		return
	}
	w.Header().Set("Content-Type", contentType)
	if writeImageCacheHeaders(w, r, "public, max-age=3600", imageETag(int64(len(data)), modTime)) {
		return
	}
	http.ServeContent(w, r, filepath.Base(cacheKey), modTime, bytes.NewReader(data))
}

func (s *Server) thumbnailCachePath(source string, maxDimension int) (string, error) {
	stat, err := os.Stat(source)
	if err != nil {
		return "", err
	}
	sum := sha1.Sum([]byte(fmt.Sprintf(
		"%s|%s|%d|%d|%d",
		thumbnailCacheVersion,
		source,
		stat.Size(),
		stat.ModTime().UnixNano(),
		maxDimension,
	)))
	return filepath.Join(s.thumbnailCacheRoot, hex.EncodeToString(sum[:])+".jpg"), nil
}

func (s *Server) thumbnailCachePathForKey(key string) string {
	sum := sha1.Sum([]byte(thumbnailCacheVersion + "|" + key))
	return filepath.Join(s.thumbnailCacheRoot, hex.EncodeToString(sum[:])+".jpg")
}

func (s *Server) sendCachedThumbnail(w http.ResponseWriter, r *http.Request, cachePath string) bool {
	file, stat, ok := openCachedThumbnail(cachePath)
	if !ok {
		return false
	}
	defer file.Close()
	serveCachedThumbnailFile(w, r, cachePath, file, stat)
	return true
}

func openCachedThumbnail(cachePath string) (*os.File, os.FileInfo, bool) {
	if cachePath == "" {
		return nil, nil, false
	}
	file, err := os.Open(cachePath)
	if err != nil {
		return nil, nil, false
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, false
	}
	return file, stat, true
}

func serveCachedThumbnailFile(w http.ResponseWriter, r *http.Request, cachePath string, file *os.File, stat os.FileInfo) {
	w.Header().Set("Content-Type", "image/jpeg")
	if w.Header().Get("X-Bmanga-Cache") == "" {
		w.Header().Set("X-Bmanga-Cache", "hit")
	}
	if writeImageCacheHeaders(w, r, "public, max-age=86400, immutable", imageETag(stat.Size(), stat.ModTime())) {
		return
	}
	http.ServeContent(w, r, filepath.Base(cachePath), stat.ModTime(), file)
}

func (s *Server) sendThumbnail(w http.ResponseWriter, r *http.Request, source string, maxDimension int) bool {
	if readerSourceQualityRequested(r) && imageFileWithinMaxDimension(source, maxDimension) {
		w.Header().Set("X-Bmanga-Image-Mode", "source")
		return false
	}
	started := time.Now()
	cachePath, err := s.thumbnailCachePath(source, maxDimension)
	if err != nil {
		return false
	}
	appendServerTiming(w.Header(), "thumbnail", time.Since(started))
	built, err := s.ensureCacheFile(r.Context(), cachePath, "thumb:"+cachePath, s.thumbnailSem, func() error {
		return s.writeThumbnail(source, cachePath, maxDimension)
	})
	if err != nil {
		return false
	}
	if built {
		w.Header().Set("X-Bmanga-Cache", "miss")
	}
	return s.sendCachedThumbnail(w, r, cachePath)
}

func (s *Server) writeThumbnail(source string, cachePath string, maxDimension int) error {
	if maxDimension <= 0 {
		return fmt.Errorf("invalid thumbnail size")
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := validateArchiveImageConfig(in, s.archiveImagePixelLimit()); err != nil {
		return err
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return err
	}
	imageSource, _, err := image.Decode(in)
	if err != nil {
		return err
	}
	return s.writeThumbnailImage(imageSource, cachePath, maxDimension)
}

func (s *Server) sendThumbnailBytes(w http.ResponseWriter, r *http.Request, data []byte, contentType string, cacheKey string, modTime time.Time, maxDimension int) bool {
	if readerSourceQualityRequested(r) && imageBytesWithinMaxDimension(data, maxDimension) {
		w.Header().Set("X-Bmanga-Image-Mode", "source")
		return false
	}
	cachePath := s.thumbnailBytesCachePath(data, contentType, cacheKey, modTime, maxDimension)
	return s.sendThumbnailBytesToPath(w, r, data, cachePath, maxDimension)
}

func readerSourceQualityRequested(r *http.Request) bool {
	return r != nil &&
		r.URL != nil &&
		r.URL.Path == "/page" &&
		strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("quality")), "source")
}

func imageConfigWithinMaxDimension(config image.Config, maxDimension int) bool {
	return maxDimension > 0 &&
		config.Width > 0 &&
		config.Height > 0 &&
		config.Width <= maxDimension &&
		config.Height <= maxDimension
}

func imageFileWithinMaxDimension(source string, maxDimension int) bool {
	file, err := os.Open(source)
	if err != nil {
		return false
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	return err == nil && imageConfigWithinMaxDimension(config, maxDimension)
}

func imageBytesWithinMaxDimension(data []byte, maxDimension int) bool {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	return err == nil && imageConfigWithinMaxDimension(config, maxDimension)
}

func (s *Server) thumbnailBytesCachePath(data []byte, contentType string, cacheKey string, modTime time.Time, maxDimension int) string {
	return s.thumbnailBytesCachePathForSize(int64(len(data)), contentType, cacheKey, modTime, maxDimension)
}

func (s *Server) thumbnailBytesCachePathForSize(size int64, contentType string, cacheKey string, modTime time.Time, maxDimension int) string {
	sum := sha1.Sum([]byte(fmt.Sprintf(
		"%s|bytes|%s|%s|%d|%d|%d",
		thumbnailCacheVersion,
		cacheKey,
		contentType,
		size,
		modTime.UnixNano(),
		maxDimension,
	)))
	return filepath.Join(s.thumbnailCacheRoot, hex.EncodeToString(sum[:])+".jpg")
}

func (s *Server) ensureThumbnailBytesCached(ctx context.Context, data []byte, contentType string, cacheKey string, modTime time.Time, maxDimension int) (bool, string, error) {
	cachePath := s.thumbnailBytesCachePath(data, contentType, cacheKey, modTime, maxDimension)
	built, err := s.ensureThumbnailBytesToPathCached(ctx, data, cachePath, maxDimension)
	return built, cachePath, err
}

func (s *Server) ensureThumbnailBytesToPathCached(ctx context.Context, data []byte, cachePath string, maxDimension int) (bool, error) {
	built, err := s.ensureCacheFile(ctx, cachePath, "thumb-bytes:"+cachePath, s.thumbnailSem, func() error {
		if err := s.validateArchiveImageData(data); err != nil {
			return err
		}
		imageSource, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return err
		}
		return s.writeThumbnailImage(imageSource, cachePath, maxDimension)
	})
	return built, err
}

func (s *Server) sendThumbnailBytesToPath(w http.ResponseWriter, r *http.Request, data []byte, cachePath string, maxDimension int) bool {
	started := time.Now()
	built, err := s.ensureThumbnailBytesToPathCached(r.Context(), data, cachePath, maxDimension)
	if err != nil {
		return false
	}
	appendServerTiming(w.Header(), "thumbnail", time.Since(started))
	if built {
		w.Header().Set("X-Bmanga-Cache", "miss")
	}
	return s.sendCachedThumbnail(w, r, cachePath)
}

func (s *Server) writeThumbnailImage(imageSource image.Image, cachePath string, maxDimension int) error {
	thumb, err := resizeForThumbnail(imageSource, maxDimension)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(cachePath), ".thumb-*.jpg")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := jpeg.Encode(tmp, thumb, &jpeg.Options{Quality: 90}); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		if _, statErr := os.Stat(cachePath); statErr == nil {
			cleanup = true
			return nil
		}
		return err
	}
	cleanup = false
	return nil
}

func resizeForThumbnail(source image.Image, maxDimension int) (*image.RGBA, error) {
	bounds := source.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid image bounds")
	}
	targetWidth := width
	targetHeight := height
	if width > maxDimension || height > maxDimension {
		scale := math.Min(float64(maxDimension)/float64(width), float64(maxDimension)/float64(height))
		targetWidth = maxInt(1, int(math.Round(float64(width)*scale)))
		targetHeight = maxInt(1, int(math.Round(float64(height)*scale)))
	}
	target := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	stddraw.Draw(target, target.Bounds(), &image.Uniform{C: color.RGBA{R: 246, G: 244, B: 239, A: 255}}, image.Point{}, stddraw.Src)
	draw.CatmullRom.Scale(target, target.Bounds(), source, bounds, draw.Over, nil)
	return target, nil
}
