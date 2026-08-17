package prototype

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRuntimeDiagnosticsLiteReportsOnlyAggregatedReadOnlyState(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := NewServer(filepath.Join(root, "data", "bmanga.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	cacheFile := filepath.Join(root, "cache", "nested", "private-cache-name.bin")
	thumbnailFile := filepath.Join(root, ".cache", "cover-thumbs", "cover.bin")
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(thumbnailFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheFile, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(thumbnailFile, []byte("1234567"), 0o600); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/runtime-diagnostics", nil)
	s.Routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control = %q, want no-store", got)
	}
	if strings.Contains(recorder.Body.String(), root) || strings.Contains(recorder.Body.String(), "private-cache-name") {
		t.Fatalf("diagnostics leaked a cache path or file name: %s", recorder.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	assertRuntimeDiagnosticKeys(t, raw, "ok", "version", "uptime_seconds", "database", "cache")
	assertRuntimeDiagnosticKeys(t, raw["database"].(map[string]any), "status")
	assertRuntimeDiagnosticKeys(t, raw["cache"].(map[string]any), "file_count", "bytes", "scan_errors", "complete")

	var response runtimeDiagnosticsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Database.Status != "healthy" {
		t.Fatalf("database state = %#v, ok = %v", response.Database, response.OK)
	}
	if response.Version == "" || response.UptimeSeconds < 0 {
		t.Fatalf("version/uptime = %q/%d", response.Version, response.UptimeSeconds)
	}
	if response.Cache.FileCount != 2 || response.Cache.Bytes != 12 || response.Cache.ScanErrors != 0 || !response.Cache.Complete {
		t.Fatalf("cache diagnostics = %#v", response.Cache)
	}
}

func TestRuntimeDiagnosticsLiteSkipsLinksAndDoesNotLeakErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := NewServer(filepath.Join(root, "data", "bmanga.sqlite"))
	if err != nil {
		t.Fatal(err)
	}

	outside := filepath.Join(t.TempDir(), "outside-secret.bin")
	if err := os.WriteFile(outside, []byte("must-not-count"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.localCacheRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.localCacheRoot, "local.bin"), []byte("123"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(s.localCacheRoot, "linked-secret.bin")); err != nil {
		t.Logf("symlink unavailable on this platform: %v", err)
	}
	outsideDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(outsideDirectory, "nested-secret.bin"), []byte("must-not-count"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDirectory, filepath.Join(s.localCacheRoot, "linked-secret-directory")); err != nil {
		t.Logf("directory symlink unavailable on this platform: %v", err)
	}
	badCacheRoot := filepath.Join(root, "private-invalid-cache-root")
	if err := os.WriteFile(badCacheRoot, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.thumbnailCacheRoot = badCacheRoot
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/runtime-diagnostics", nil)
	s.Routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, forbidden := range []string{root, outside, outsideDirectory, badCacheRoot, "database is closed", "outside-secret", "nested-secret", "private-invalid"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("diagnostics leaked %q: %s", forbidden, body)
		}
	}

	var response runtimeDiagnosticsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.OK || response.Database.Status != "unavailable" {
		t.Fatalf("database state = %#v, ok = %v", response.Database, response.OK)
	}
	if response.Cache.FileCount != 1 || response.Cache.Bytes != 3 || response.Cache.ScanErrors != 1 || response.Cache.Complete {
		t.Fatalf("cache diagnostics = %#v", response.Cache)
	}
}

func TestRuntimeDiagnosticsLiteRejectsWrites(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := NewServer(filepath.Join(root, "data", "bmanga.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime-diagnostics", strings.NewReader(`{}`))
	request.Header.Set(writeIntentHeader, writeIntentValue)
	s.Routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusMethodNotAllowed, recorder.Body.String())
	}
}

func TestRuntimeDiagnosticsCacheScanFailsClosedWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := scanRuntimeDiagnosticCaches(ctx, []string{filepath.Join(t.TempDir(), "cache")})
	if result.Complete || result.ScanErrors != 1 || result.FileCount != 0 || result.Bytes != 0 {
		t.Fatalf("canceled cache diagnostics = %#v", result)
	}
}

func TestRuntimeDiagnosticsCacheScanEnforcesHardEntryLimit(t *testing.T) {
	exactRoot := t.TempDir()
	for _, name := range []string{"one.bin", "two.bin", "three.bin"} {
		if err := os.WriteFile(filepath.Join(exactRoot, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	exact := scanRuntimeDiagnosticCachesWithLimit(context.Background(), []string{exactRoot}, 3)
	if !exact.Complete || exact.ScanErrors != 0 || exact.FileCount != 3 || exact.Bytes != 3 {
		t.Fatalf("exact-limit cache diagnostics = %#v", exact)
	}

	overLimitRoot := t.TempDir()
	for _, name := range []string{"one.bin", "two.bin", "three.bin", "four.bin"} {
		if err := os.WriteFile(filepath.Join(overLimitRoot, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	result := scanRuntimeDiagnosticCachesWithLimit(context.Background(), []string{overLimitRoot}, 3)
	if result.Complete || result.ScanErrors != 1 {
		t.Fatalf("limited cache diagnostics = %#v", result)
	}
	if result.FileCount != 3 || result.Bytes != 3 {
		t.Fatalf("hard limit processed %d files/%d bytes, want 3/3", result.FileCount, result.Bytes)
	}
}

func TestRuntimeDiagnosticsDirectoryOpenRejectsIdentitySwap(t *testing.T) {
	rootPath := t.TempDir()
	directoryPath := filepath.Join(rootPath, "cache")
	replacementPath := filepath.Join(rootPath, "replacement")
	movedPath := filepath.Join(rootPath, "cache-before-swap")
	if err := os.Mkdir(directoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(replacementPath, 0o755); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Lstat(directoryPath)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	opened, err := openRuntimeDiagnosticDirectoryWithHooks(context.Background(), root, "cache", expected, func() {
		if renameErr := os.Rename(directoryPath, movedPath); renameErr != nil {
			t.Fatalf("move original directory: %v", renameErr)
		}
		if renameErr := os.Rename(replacementPath, directoryPath); renameErr != nil {
			t.Fatalf("move replacement directory: %v", renameErr)
		}
	}, nil)
	if opened != nil {
		_ = opened.Close()
	}
	if err == nil {
		t.Fatal("directory identity swap was accepted")
	}
}

func TestRuntimeDiagnosticsCacheCoordinatorCoalescesAndCaches(t *testing.T) {
	coordinator := &runtimeDiagnosticsCacheCoordinator{}
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	scanner := func(context.Context, []string) runtimeDiagnosticsCache {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return runtimeDiagnosticsCache{FileCount: 7, Bytes: 11, Complete: true}
	}

	results := make([]runtimeDiagnosticsCache, 2)
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		results[0] = coordinator.snapshotWithScanner(context.Background(), []string{"cache"}, scanner)
	}()
	<-started
	wait.Add(1)
	go func() {
		defer wait.Done()
		results[1] = coordinator.snapshotWithScanner(context.Background(), []string{"cache"}, scanner)
	}()
	close(release)
	wait.Wait()

	for index, result := range results {
		if !result.Complete || result.FileCount != 7 || result.Bytes != 11 {
			t.Fatalf("result %d = %#v", index, result)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("scanner calls = %d, want 1", got)
	}

	cached := coordinator.snapshotWithScanner(context.Background(), []string{"cache"}, scanner)
	if !cached.Complete || cached.FileCount != 7 || calls.Load() != 1 {
		t.Fatalf("cached result = %#v, scanner calls = %d", cached, calls.Load())
	}
}

func TestRuntimeDiagnosticsCacheCoordinatorLeaderCancellationDoesNotCancelSharedScan(t *testing.T) {
	coordinator := &runtimeDiagnosticsCacheCoordinator{}
	scanContexts := make(chan context.Context, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	scanner := func(ctx context.Context, _ []string) runtimeDiagnosticsCache {
		calls.Add(1)
		scanContexts <- ctx
		select {
		case <-release:
			return runtimeDiagnosticsCache{FileCount: 5, Bytes: 13, Complete: true}
		case <-ctx.Done():
			return failedRuntimeDiagnosticsCache()
		}
	}

	leaderContext, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan runtimeDiagnosticsCache, 1)
	go func() {
		leaderDone <- coordinator.snapshotWithScanner(leaderContext, []string{"cache"}, scanner)
	}()
	sharedContext := <-scanContexts
	if _, ok := sharedContext.Deadline(); !ok {
		t.Fatal("shared scan context is not bounded")
	}

	cancelLeader()
	leader := <-leaderDone
	if leader.Complete || leader.ScanErrors != 1 {
		t.Fatalf("canceled leader result = %#v", leader)
	}
	if err := sharedContext.Err(); err != nil {
		t.Fatalf("leader cancellation polluted shared scan context: %v", err)
	}

	waiterDone := make(chan runtimeDiagnosticsCache, 1)
	go func() {
		waiterDone <- coordinator.snapshotWithScanner(context.Background(), []string{"cache"}, scanner)
	}()
	close(release)
	waiter := <-waiterDone
	if !waiter.Complete || waiter.FileCount != 5 || waiter.Bytes != 13 {
		t.Fatalf("waiter result = %#v", waiter)
	}

	cached := coordinator.snapshotWithScanner(context.Background(), []string{"cache"}, scanner)
	if !cached.Complete || cached.FileCount != 5 || cached.Bytes != 13 {
		t.Fatalf("cached result = %#v", cached)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("scanner calls = %d, want 1", got)
	}
}

func TestRuntimeDiagnosticsCacheCancellationIsCountedOnce(t *testing.T) {
	result := runtimeDiagnosticsCache{Complete: true}
	markRuntimeDiagnosticsCacheError(&result)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for range 3 {
		if !markRuntimeDiagnosticsCacheCancellation(ctx, &result) {
			t.Fatal("canceled context was not detected")
		}
	}
	if result.Complete || result.ScanErrors != 2 || !result.cancellationRecorded {
		t.Fatalf("cache diagnostics after repeated cancellation checks = %#v", result)
	}
}

func assertRuntimeDiagnosticKeys(t *testing.T, payload map[string]any, allowed ...string) {
	t.Helper()
	allowedKeys := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedKeys[key] = struct{}{}
	}
	if len(payload) != len(allowedKeys) {
		t.Fatalf("diagnostic keys = %v, want exactly %v", payload, allowed)
	}
	for key := range payload {
		if _, ok := allowedKeys[key]; !ok {
			t.Fatalf("unexpected diagnostic key %q", key)
		}
	}
}
