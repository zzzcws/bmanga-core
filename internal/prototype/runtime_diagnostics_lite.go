package prototype

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	runtimeDiagnosticsDatabaseTimeout = 250 * time.Millisecond
	runtimeDiagnosticsCacheTimeout    = 750 * time.Millisecond
	runtimeDiagnosticsCacheTTL        = 2 * time.Second
	runtimeDiagnosticsReadDirBatch    = 128
	runtimeDiagnosticsMaxEntries      = 100_000
)

var (
	runtimeDiagnosticsProcessStartedAt = time.Now()
	runtimeDiagnosticsCaches           runtimeDiagnosticsCacheCoordinator
)

type runtimeDiagnosticsDatabase struct {
	Status string `json:"status"`
}

type runtimeDiagnosticsCache struct {
	FileCount            int64 `json:"file_count"`
	Bytes                int64 `json:"bytes"`
	ScanErrors           int64 `json:"scan_errors"`
	Complete             bool  `json:"complete"`
	cancellationRecorded bool
}

type runtimeDiagnosticsResponse struct {
	OK            bool                       `json:"ok"`
	Version       string                     `json:"version"`
	UptimeSeconds int64                      `json:"uptime_seconds"`
	Database      runtimeDiagnosticsDatabase `json:"database"`
	Cache         runtimeDiagnosticsCache    `json:"cache"`
}

type runtimeDiagnosticsCacheCall struct {
	done   chan struct{}
	result runtimeDiagnosticsCache
}

type runtimeDiagnosticsCacheCoordinator struct {
	mu           sync.Mutex
	cachedKey    string
	cachedResult runtimeDiagnosticsCache
	cachedUntil  time.Time
	inFlight     map[string]*runtimeDiagnosticsCacheCall
}

type runtimeDiagnosticsPendingDirectory struct {
	relative string
	expected os.FileInfo
}

func (s *Server) handleRuntimeDiagnosticsLite(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}

	database := runtimeDiagnosticsDatabase{Status: "unavailable"}
	databaseContext, cancelDatabase := context.WithTimeout(r.Context(), runtimeDiagnosticsDatabaseTimeout)
	if s.db != nil && s.db.PingContext(databaseContext) == nil {
		database.Status = "healthy"
	}
	cancelDatabase()

	cacheContext, cancelCache := context.WithTimeout(r.Context(), runtimeDiagnosticsCacheTimeout)
	cache := runtimeDiagnosticsCaches.snapshot(cacheContext, []string{s.localCacheRoot, s.thumbnailCacheRoot})
	cancelCache()

	uptime := int64(time.Since(runtimeDiagnosticsProcessStartedAt) / time.Second)
	if uptime < 0 {
		uptime = 0
	}
	writeJSON(w, runtimeDiagnosticsResponse{
		OK:            database.Status == "healthy" && cache.Complete,
		Version:       publicRuntimeVersion(),
		UptimeSeconds: uptime,
		Database:      database,
		Cache:         cache,
	})
}

func publicRuntimeVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "development"
	}
	version := strings.TrimSpace(info.Main.Version)
	if version == "" || version == "(devel)" || len(version) > 64 {
		return "development"
	}
	for _, character := range version {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || strings.ContainsRune("._+-", character) {
			continue
		}
		return "development"
	}
	return version
}

func (coordinator *runtimeDiagnosticsCacheCoordinator) snapshot(ctx context.Context, roots []string) runtimeDiagnosticsCache {
	return coordinator.snapshotWithScanner(ctx, roots, scanRuntimeDiagnosticCaches)
}

func (coordinator *runtimeDiagnosticsCacheCoordinator) snapshotWithScanner(
	ctx context.Context,
	roots []string,
	scanner func(context.Context, []string) runtimeDiagnosticsCache,
) runtimeDiagnosticsCache {
	normalizedRoots, key := normalizeRuntimeDiagnosticRoots(roots)
	if ctx.Err() != nil {
		return failedRuntimeDiagnosticsCache()
	}

	now := time.Now()
	coordinator.mu.Lock()
	if coordinator.cachedKey == key && now.Before(coordinator.cachedUntil) {
		result := coordinator.cachedResult
		coordinator.mu.Unlock()
		return result
	}
	if coordinator.inFlight == nil {
		coordinator.inFlight = make(map[string]*runtimeDiagnosticsCacheCall)
	}
	call, ok := coordinator.inFlight[key]
	if !ok {
		call = &runtimeDiagnosticsCacheCall{done: make(chan struct{})}
		coordinator.inFlight[key] = call
		scanContext, cancelScan := context.WithTimeout(context.Background(), runtimeDiagnosticsCacheTimeout)
		go coordinator.runScan(key, normalizedRoots, call, scanContext, cancelScan, scanner)
	}
	coordinator.mu.Unlock()

	select {
	case <-call.done:
		return call.result
	case <-ctx.Done():
		return failedRuntimeDiagnosticsCache()
	}
}

func (coordinator *runtimeDiagnosticsCacheCoordinator) runScan(
	key string,
	roots []string,
	call *runtimeDiagnosticsCacheCall,
	ctx context.Context,
	cancel context.CancelFunc,
	scanner func(context.Context, []string) runtimeDiagnosticsCache,
) {
	defer cancel()
	result := failedRuntimeDiagnosticsCache()
	func() {
		defer func() {
			if recover() != nil {
				result = failedRuntimeDiagnosticsCache()
			}
		}()
		result = scanner(ctx, roots)
	}()

	coordinator.mu.Lock()
	call.result = result
	coordinator.cachedKey = key
	coordinator.cachedResult = result
	coordinator.cachedUntil = time.Now().Add(runtimeDiagnosticsCacheTTL)
	delete(coordinator.inFlight, key)
	close(call.done)
	coordinator.mu.Unlock()
}

func failedRuntimeDiagnosticsCache() runtimeDiagnosticsCache {
	return runtimeDiagnosticsCache{ScanErrors: 1, Complete: false, cancellationRecorded: true}
}

func normalizeRuntimeDiagnosticRoots(roots []string) ([]string, string) {
	normalized := make([]string, 0, len(roots))
	keys := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "" || root == "." {
			continue
		}
		key := root
		if filepath.Separator == '\\' {
			key = strings.ToLower(key)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, root)
		keys = append(keys, key)
	}
	return normalized, strings.Join(keys, "\x00")
}

func scanRuntimeDiagnosticCaches(ctx context.Context, roots []string) runtimeDiagnosticsCache {
	return scanRuntimeDiagnosticCachesWithLimit(ctx, roots, runtimeDiagnosticsMaxEntries)
}

func scanRuntimeDiagnosticCachesWithLimit(ctx context.Context, roots []string, maxEntries int64) runtimeDiagnosticsCache {
	result := runtimeDiagnosticsCache{Complete: true}
	normalizedRoots, _ := normalizeRuntimeDiagnosticRoots(roots)
	var entries int64
	for _, root := range normalizedRoots {
		if markRuntimeDiagnosticsCacheCancellation(ctx, &result) {
			break
		}
		if scanRuntimeDiagnosticCacheRoot(ctx, root, &entries, maxEntries, &result) {
			break
		}
	}
	return result
}

// scanRuntimeDiagnosticCacheRoot returns true when the global scan must stop.
func scanRuntimeDiagnosticCacheRoot(
	ctx context.Context,
	rootPath string,
	entries *int64,
	maxEntries int64,
	result *runtimeDiagnosticsCache,
) bool {
	rootBefore, err := os.Lstat(rootPath)
	if markRuntimeDiagnosticsCacheCancellation(ctx, result) {
		return true
	}
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil || !rootBefore.IsDir() || fileInfoIsReparsePoint(rootBefore) {
		markRuntimeDiagnosticsCacheError(result)
		return false
	}

	root, err := os.OpenRoot(rootPath)
	if markRuntimeDiagnosticsCacheCancellation(ctx, result) {
		if root != nil {
			_ = root.Close()
		}
		return true
	}
	if err != nil {
		markRuntimeDiagnosticsCacheError(result)
		return false
	}
	defer root.Close()

	rootDirectory, err := openRuntimeDiagnosticDirectory(ctx, root, ".", rootBefore)
	if markRuntimeDiagnosticsCacheCancellation(ctx, result) {
		if rootDirectory != nil {
			_ = rootDirectory.Close()
		}
		return true
	}
	if err != nil {
		markRuntimeDiagnosticsCacheError(result)
		return false
	}
	rootAfter, err := os.Lstat(rootPath)
	if markRuntimeDiagnosticsCacheCancellation(ctx, result) {
		_ = rootDirectory.Close()
		return true
	}
	if err != nil || fileInfoIsReparsePoint(rootAfter) || !os.SameFile(rootBefore, rootAfter) {
		_ = rootDirectory.Close()
		markRuntimeDiagnosticsCacheError(result)
		return false
	}

	pending := []runtimeDiagnosticsPendingDirectory{{relative: ".", expected: rootBefore}}
	for len(pending) > 0 {
		if markRuntimeDiagnosticsCacheCancellation(ctx, result) {
			if rootDirectory != nil {
				_ = rootDirectory.Close()
			}
			return true
		}

		last := len(pending) - 1
		directory := pending[last]
		pending = pending[:last]
		opened := rootDirectory
		rootDirectory = nil
		if opened == nil {
			opened, err = openRuntimeDiagnosticDirectory(ctx, root, directory.relative, directory.expected)
			if markRuntimeDiagnosticsCacheCancellation(ctx, result) {
				if opened != nil {
					_ = opened.Close()
				}
				return true
			}
			if err != nil {
				markRuntimeDiagnosticsCacheError(result)
				continue
			}
		}

		stop := scanRuntimeDiagnosticDirectory(
			ctx,
			opened,
			directory.relative,
			entries,
			maxEntries,
			result,
			&pending,
		)
		_ = opened.Close()
		if stop {
			return true
		}
	}
	return false
}

func openRuntimeDiagnosticDirectory(ctx context.Context, root *os.Root, relative string, expected os.FileInfo) (*os.File, error) {
	return openRuntimeDiagnosticDirectoryWithHooks(ctx, root, relative, expected, nil, nil)
}

func openRuntimeDiagnosticDirectoryWithHooks(
	ctx context.Context,
	root *os.Root,
	relative string,
	expected os.FileInfo,
	afterLstat func(),
	afterOpen func(),
) (*os.File, error) {
	before, err := validateRuntimeDiagnosticDirectoryComponents(ctx, root, relative)
	if err != nil || !os.SameFile(expected, before) {
		return nil, os.ErrPermission
	}
	if afterLstat != nil {
		afterLstat()
	}

	directory, err := root.Open(relative)
	if err != nil {
		return nil, err
	}
	closeOnError := func(err error) (*os.File, error) {
		_ = directory.Close()
		return nil, err
	}
	opened, err := directory.Stat()
	if err != nil || !opened.IsDir() || fileInfoIsReparsePoint(opened) || !os.SameFile(before, opened) {
		return closeOnError(os.ErrPermission)
	}
	if afterOpen != nil {
		afterOpen()
	}
	after, err := validateRuntimeDiagnosticDirectoryComponents(ctx, root, relative)
	if err != nil || !os.SameFile(opened, after) {
		return closeOnError(os.ErrPermission)
	}
	return directory, nil
}

func validateRuntimeDiagnosticDirectoryComponents(ctx context.Context, root *os.Root, relative string) (os.FileInfo, error) {
	relative = filepath.Clean(relative)
	if relative == "." {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := root.Lstat(".")
		if err != nil || !info.IsDir() || fileInfoIsReparsePoint(info) {
			return nil, os.ErrPermission
		}
		return info, nil
	}

	current := ""
	var final os.FileInfo
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if part == "" || part == "." || part == ".." {
			return nil, os.ErrPermission
		}
		if current == "" {
			current = part
		} else {
			current = filepath.Join(current, part)
		}
		info, err := root.Lstat(current)
		if err != nil || !info.IsDir() || fileInfoIsReparsePoint(info) {
			return nil, os.ErrPermission
		}
		final = info
	}
	if final == nil {
		return nil, os.ErrPermission
	}
	return final, nil
}

func scanRuntimeDiagnosticDirectory(
	ctx context.Context,
	directory *os.File,
	relative string,
	entries *int64,
	maxEntries int64,
	result *runtimeDiagnosticsCache,
	pending *[]runtimeDiagnosticsPendingDirectory,
) bool {
	for {
		if markRuntimeDiagnosticsCacheCancellation(ctx, result) {
			return true
		}

		remainingWithLimitProbe := maxEntries - *entries + 1
		if remainingWithLimitProbe < 1 {
			markRuntimeDiagnosticsCacheError(result)
			return true
		}
		batchSize := int64(runtimeDiagnosticsReadDirBatch)
		if remainingWithLimitProbe < batchSize {
			batchSize = remainingWithLimitProbe
		}
		batch, readErr := directory.ReadDir(int(batchSize))
		for _, entry := range batch {
			if markRuntimeDiagnosticsCacheCancellation(ctx, result) {
				return true
			}
			if *entries >= maxEntries {
				markRuntimeDiagnosticsCacheError(result)
				return true
			}
			*entries++

			info, err := entry.Info()
			if err != nil {
				if markRuntimeDiagnosticsCacheCancellation(ctx, result) {
					return true
				}
				markRuntimeDiagnosticsCacheError(result)
				continue
			}
			if fileInfoIsReparsePoint(info) {
				continue
			}
			if info.IsDir() {
				child := entry.Name()
				if relative != "." {
					child = filepath.Join(relative, child)
				}
				*pending = append(*pending, runtimeDiagnosticsPendingDirectory{
					relative: child,
					expected: info,
				})
				continue
			}
			if !info.Mode().IsRegular() {
				continue
			}
			result.FileCount++
			size := info.Size()
			if size < 0 || size > math.MaxInt64-result.Bytes {
				markRuntimeDiagnosticsCacheError(result)
				result.Bytes = math.MaxInt64
				continue
			}
			result.Bytes += size
		}

		if markRuntimeDiagnosticsCacheCancellation(ctx, result) {
			return true
		}
		if errors.Is(readErr, io.EOF) {
			return false
		}
		if readErr != nil {
			markRuntimeDiagnosticsCacheError(result)
			return false
		}
	}
}

func markRuntimeDiagnosticsCacheError(result *runtimeDiagnosticsCache) {
	result.ScanErrors++
	result.Complete = false
}

func markRuntimeDiagnosticsCacheCancellation(ctx context.Context, result *runtimeDiagnosticsCache) bool {
	if ctx.Err() == nil {
		return false
	}
	result.Complete = false
	if !result.cancellationRecorded {
		result.cancellationRecorded = true
		result.ScanErrors++
	}
	return true
}
