package prototype

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
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
	s.serveImageFileWithResize(w, r, source, preferredType, longestEdgeResize(maxDimension))
}

type imageResizeSpec struct {
	maxDimension int
	maxWidth     int
}

// CatmullRom keeps targetWidth*sourceHeight RGBA float64 pixels while scaling.
// A width-only target can therefore use far more memory than a longest-edge
// thumbnail. Bound the estimated source, target, scratch and filter storage
// before decoding: 128 MiB per render, or 512 MiB at the default four thumbnail
// permits. This is not a process RSS limit (encoded inputs/runtime add overhead;
// an operator increasing thumbnail concurrency also increases this envelope).
const widthResizeWorkingBytes int64 = 128 * 1024 * 1024

var errWidthResizeWorkingBudget = errors.New("width derivative exceeds working memory budget")

func widthResizeWithinWorkingBudget(config image.Config, maxWidth int) bool {
	if maxWidth <= 0 || config.Width <= maxWidth {
		return true // No derivative or full decode is required.
	}
	if config.Width <= 0 || config.Height <= 0 {
		return false
	}
	targetHeight := maxInt(1, int(math.Round(float64(config.Height)*float64(maxWidth)/float64(config.Width))))
	remaining := widthResizeWorkingBytes
	consume := func(factors ...int64) bool {
		cost := int64(1)
		for _, factor := range factors {
			if factor <= 0 || cost > remaining/factor {
				return false
			}
			cost *= factor
		}
		remaining -= cost
		return true
	}
	// Eight bytes covers decoded 16-bit RGBA inputs. The last term bounds
	// the separable filter's weights/indices and decoder row workspaces.
	return consume(8, int64(config.Width), int64(config.Height)) &&
		consume(4, int64(maxWidth), int64(targetHeight)) &&
		consume(32, int64(maxWidth), int64(config.Height)) &&
		consume(128, int64(config.Width)+int64(config.Height)+int64(maxWidth)+int64(targetHeight))
}

func (s *Server) thumbnailInputConfig(reader io.Reader, resize imageResizeSpec) (image.Config, error) {
	config, _, err := image.DecodeConfig(reader)
	if err != nil {
		return config, fmt.Errorf("decode image dimensions: %w", err)
	}
	// Validate the existing input limit first. A memory fallback must never
	// turn an invalid pixel bomb into an accepted original-source response.
	if err := validateArchiveImageDimensions(config.Width, config.Height, s.archiveImagePixelLimit()); err != nil {
		return config, err
	}
	if resize.maxWidth > 0 && !widthResizeWithinWorkingBudget(config, resize.maxWidth) {
		return config, errWidthResizeWorkingBudget
	}
	return config, nil
}

func widthThumbnailPreflightResponse(w http.ResponseWriter, config image.Config, maxWidth int, err error) (handled, source bool) {
	if errors.Is(err, errWidthResizeWorkingBudget) || err == nil && imageConfigWithinMaxWidth(config, maxWidth) {
		w.Header().Set("X-Bmanga-Image-Mode", "source")
		return false, true
	}
	if err != nil {
		status := http.StatusUnsupportedMediaType
		if errors.Is(err, errArchiveResourceLimit) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, http.StatusText(status), status)
		return true, false
	}
	return false, false
}

func longestEdgeResize(maxDimension int) imageResizeSpec {
	return imageResizeSpec{maxDimension: maxDimension}
}

func widthResize(maxWidth int) imageResizeSpec {
	return imageResizeSpec{maxWidth: maxWidth}
}

func pageImageResizeSpec(maxDimension int, maxWidth ...int) imageResizeSpec {
	if len(maxWidth) > 0 && maxWidth[0] > 0 {
		return widthResize(maxWidth[0])
	}
	return longestEdgeResize(maxDimension)
}

func (spec imageResizeSpec) active() bool {
	return spec.maxDimension > 0 || spec.maxWidth > 0
}

// cacheToken deliberately preserves the historical longest-edge token so
// existing /page?max= caches remain reusable. Width-constrained derivatives
// use an explicit axis prefix and can never collide with those entries.
func (spec imageResizeSpec) cacheToken() string {
	if spec.maxWidth > 0 {
		return "width=" + fmt.Sprint(spec.maxWidth)
	}
	return fmt.Sprint(spec.maxDimension)
}

func (s *Server) servePageImageFile(w http.ResponseWriter, r *http.Request, source string, preferredType string, maxDimension int, maxWidth ...int) {
	s.serveImageFileWithResize(w, r, source, preferredType, pageImageResizeSpec(maxDimension, maxWidth...))
}

func (s *Server) serveImageFileWithResize(w http.ResponseWriter, r *http.Request, source string, preferredType string, resize imageResizeSpec) {
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
	if resize.active() && s.sendThumbnailWithResize(w, r, source, resize) {
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
	s.serveImageDataWithResize(w, r, data, contentType, cacheKey, modTime, longestEdgeResize(maxDimension))
}

func (s *Server) serveImageDataWithResize(w http.ResponseWriter, r *http.Request, data []byte, contentType string, cacheKey string, modTime time.Time, resize imageResizeSpec) {
	if !allowedImageMIME(contentType) {
		http.Error(w, http.StatusText(http.StatusUnsupportedMediaType), http.StatusUnsupportedMediaType)
		return
	}
	if resize.active() && s.sendThumbnailBytesWithResize(w, r, data, contentType, cacheKey, modTime, resize) {
		return
	}
	w.Header().Set("Content-Type", contentType)
	if writeImageCacheHeaders(w, r, "public, max-age=3600", imageETag(int64(len(data)), modTime)) {
		return
	}
	http.ServeContent(w, r, filepath.Base(cacheKey), modTime, bytes.NewReader(data))
}

func (s *Server) thumbnailCachePath(source string, maxDimension int) (string, error) {
	return s.thumbnailCachePathWithResize(source, longestEdgeResize(maxDimension))
}

func (s *Server) thumbnailCachePathWithResize(source string, resize imageResizeSpec) (string, error) {
	stat, err := os.Stat(source)
	if err != nil {
		return "", err
	}
	sum := sha1.Sum([]byte(fmt.Sprintf(
		"%s|%s|%d|%d|%s",
		thumbnailCacheVersion,
		source,
		stat.Size(),
		stat.ModTime().UnixNano(),
		resize.cacheToken(),
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
	return s.sendThumbnailWithResize(w, r, source, longestEdgeResize(maxDimension))
}

func (s *Server) sendThumbnailWithResize(w http.ResponseWriter, r *http.Request, source string, resize imageResizeSpec) bool {
	if resize.maxWidth > 0 {
		file, err := os.Open(source)
		if err != nil {
			return false
		}
		config, err := s.thumbnailInputConfig(file, resize)
		_ = file.Close()
		if handled, useSource := widthThumbnailPreflightResponse(w, config, resize.maxWidth, err); handled || useSource {
			return handled
		}
	}
	if resize.maxDimension > 0 && readerSourceQualityRequested(r) && imageFileWithinMaxDimension(source, resize.maxDimension) {
		w.Header().Set("X-Bmanga-Image-Mode", "source")
		return false
	}
	started := time.Now()
	cachePath, err := s.thumbnailCachePathWithResize(source, resize)
	if err != nil {
		return false
	}
	appendServerTiming(w.Header(), "thumbnail", time.Since(started))
	built, err := s.ensureCacheFile(r.Context(), cachePath, "thumb:"+cachePath, s.thumbnailSem, func() error {
		return s.writeThumbnailWithResize(source, cachePath, resize)
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
	return s.writeThumbnailWithResize(source, cachePath, longestEdgeResize(maxDimension))
}

func (s *Server) writeThumbnailWithResize(source string, cachePath string, resize imageResizeSpec) error {
	if !resize.active() {
		return fmt.Errorf("invalid thumbnail size")
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if _, err := s.thumbnailInputConfig(in, resize); err != nil {
		return err
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return err
	}
	imageSource, _, err := image.Decode(in)
	if err != nil {
		return err
	}
	return s.writeThumbnailImageWithResize(imageSource, cachePath, resize)
}

func (s *Server) sendThumbnailBytes(w http.ResponseWriter, r *http.Request, data []byte, contentType string, cacheKey string, modTime time.Time, maxDimension int) bool {
	return s.sendThumbnailBytesWithResize(w, r, data, contentType, cacheKey, modTime, longestEdgeResize(maxDimension))
}

func (s *Server) sendThumbnailBytesWithResize(w http.ResponseWriter, r *http.Request, data []byte, contentType string, cacheKey string, modTime time.Time, resize imageResizeSpec) bool {
	if resize.maxWidth > 0 {
		if s.archiveLimits.maxPageBytes > 0 && int64(len(data)) > s.archiveLimits.maxPageBytes {
			http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
			return true
		}
		config, err := s.thumbnailInputConfig(bytes.NewReader(data), resize)
		if handled, useSource := widthThumbnailPreflightResponse(w, config, resize.maxWidth, err); handled || useSource {
			return handled
		}
	}
	if resize.maxDimension > 0 && readerSourceQualityRequested(r) && imageBytesWithinMaxDimension(data, resize.maxDimension) {
		w.Header().Set("X-Bmanga-Image-Mode", "source")
		return false
	}
	cachePath := s.thumbnailBytesCachePathWithResize(data, contentType, cacheKey, modTime, resize)
	return s.sendThumbnailBytesToPathWithResize(w, r, data, cachePath, resize)
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

func imageConfigWithinMaxWidth(config image.Config, maxWidth int) bool {
	return maxWidth > 0 && config.Width > 0 && config.Height > 0 && config.Width <= maxWidth
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

func imageFileWithinMaxWidth(source string, maxWidth int) bool {
	file, err := os.Open(source)
	if err != nil {
		return false
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	return err == nil && imageConfigWithinMaxWidth(config, maxWidth)
}

func imageBytesWithinMaxWidth(data []byte, maxWidth int) bool {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	return err == nil && imageConfigWithinMaxWidth(config, maxWidth)
}

func (s *Server) thumbnailBytesCachePath(data []byte, contentType string, cacheKey string, modTime time.Time, maxDimension int) string {
	return s.thumbnailBytesCachePathWithResize(data, contentType, cacheKey, modTime, longestEdgeResize(maxDimension))
}

func (s *Server) thumbnailBytesCachePathWithResize(data []byte, contentType string, cacheKey string, modTime time.Time, resize imageResizeSpec) string {
	return s.thumbnailBytesCachePathForSizeWithResize(int64(len(data)), contentType, cacheKey, modTime, resize)
}

func (s *Server) thumbnailBytesCachePathForSize(size int64, contentType string, cacheKey string, modTime time.Time, maxDimension int) string {
	return s.thumbnailBytesCachePathForSizeWithResize(size, contentType, cacheKey, modTime, longestEdgeResize(maxDimension))
}

func (s *Server) thumbnailBytesCachePathForSizeWithResize(size int64, contentType string, cacheKey string, modTime time.Time, resize imageResizeSpec) string {
	sum := sha1.Sum([]byte(fmt.Sprintf(
		"%s|bytes|%s|%s|%d|%d|%s",
		thumbnailCacheVersion,
		cacheKey,
		contentType,
		size,
		modTime.UnixNano(),
		resize.cacheToken(),
	)))
	return filepath.Join(s.thumbnailCacheRoot, hex.EncodeToString(sum[:])+".jpg")
}

func (s *Server) ensureThumbnailBytesCached(ctx context.Context, data []byte, contentType string, cacheKey string, modTime time.Time, maxDimension int) (bool, string, error) {
	resize := longestEdgeResize(maxDimension)
	cachePath := s.thumbnailBytesCachePathWithResize(data, contentType, cacheKey, modTime, resize)
	built, err := s.ensureThumbnailBytesToPathCachedWithResize(ctx, data, cachePath, resize)
	return built, cachePath, err
}

func (s *Server) ensureThumbnailBytesToPathCached(ctx context.Context, data []byte, cachePath string, maxDimension int) (bool, error) {
	return s.ensureThumbnailBytesToPathCachedWithResize(ctx, data, cachePath, longestEdgeResize(maxDimension))
}

func (s *Server) ensureThumbnailBytesToPathCachedWithResize(ctx context.Context, data []byte, cachePath string, resize imageResizeSpec) (bool, error) {
	// Width mode is a display constraint, not a request to transcode every
	// image. Preserve the source bytes and MIME type when no downscale is
	// needed, including archive entries that use the early cache path.
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if resize.maxWidth > 0 {
		if s.archiveLimits.maxPageBytes > 0 && int64(len(data)) > s.archiveLimits.maxPageBytes {
			return false, archiveLimitError("page exceeds input byte limit")
		}
		config, err := s.thumbnailInputConfig(bytes.NewReader(data), resize)
		if err != nil {
			return false, err
		}
		if imageConfigWithinMaxWidth(config, resize.maxWidth) {
			return false, nil
		}
	}
	built, err := s.ensureCacheFile(ctx, cachePath, "thumb-bytes:"+cachePath, s.thumbnailSem, func() error {
		if err := s.validateArchiveImageData(data); err != nil {
			return err
		}
		imageSource, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return err
		}
		return s.writeThumbnailImageWithResize(imageSource, cachePath, resize)
	})
	return built, err
}

func (s *Server) sendThumbnailBytesToPath(w http.ResponseWriter, r *http.Request, data []byte, cachePath string, maxDimension int) bool {
	return s.sendThumbnailBytesToPathWithResize(w, r, data, cachePath, longestEdgeResize(maxDimension))
}

func (s *Server) sendThumbnailBytesToPathWithResize(w http.ResponseWriter, r *http.Request, data []byte, cachePath string, resize imageResizeSpec) bool {
	started := time.Now()
	built, err := s.ensureThumbnailBytesToPathCachedWithResize(r.Context(), data, cachePath, resize)
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
	return s.writeThumbnailImageWithResize(imageSource, cachePath, longestEdgeResize(maxDimension))
}

func (s *Server) writeThumbnailImageWithResize(imageSource image.Image, cachePath string, resize imageResizeSpec) error {
	thumb, err := resizeForImageSpec(imageSource, resize)
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
	return resizeForImageSpec(source, longestEdgeResize(maxDimension))
}

func resizeForImageSpec(source image.Image, resize imageResizeSpec) (*image.RGBA, error) {
	bounds := source.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid image bounds")
	}
	if resize.maxWidth > 0 && !widthResizeWithinWorkingBudget(image.Config{Width: width, Height: height}, resize.maxWidth) {
		return nil, errWidthResizeWorkingBudget
	}
	targetWidth := width
	targetHeight := height
	if resize.maxWidth > 0 && width > resize.maxWidth {
		scale := float64(resize.maxWidth) / float64(width)
		targetWidth = resize.maxWidth
		targetHeight = maxInt(1, int(math.Round(float64(height)*scale)))
	} else if resize.maxDimension > 0 && (width > resize.maxDimension || height > resize.maxDimension) {
		scale := math.Min(float64(resize.maxDimension)/float64(width), float64(resize.maxDimension)/float64(height))
		targetWidth = maxInt(1, int(math.Round(float64(width)*scale)))
		targetHeight = maxInt(1, int(math.Round(float64(height)*scale)))
	}
	target := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	stddraw.Draw(target, target.Bounds(), &image.Uniform{C: color.RGBA{R: 246, G: 244, B: 239, A: 255}}, image.Point{}, stddraw.Src)
	draw.CatmullRom.Scale(target, target.Bounds(), source, bounds, draw.Over, nil)
	return target, nil
}
