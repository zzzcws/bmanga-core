package prototype

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	_ "golang.org/x/image/webp"
	_ "image/gif"
	_ "image/png"
	_ "modernc.org/sqlite"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultLimit = 60
	minLimit     = 12
	maxLimit     = 120

	thumbnailCacheVersion       = "cover-thumb-v2-q90"
	readerPageMaxDimension      = 3200
	maxArchivePageBytes         = 100 * 1024 * 1024
	maxNestedArchiveBytes       = 512 * 1024 * 1024
	readerManifestVersion       = "go-reader-manifest-v3"
	maxClientMutationFutureSkew = 5 * time.Minute
)

var coverExcludeMarkers = []string{
	"免责",
	"免責",
	"免责声明",
	"免責聲明",
	"黑日猎漫",
	"黑日獵漫",
	"readme",
	"credit",
	"credits",
}

var (
	chapterMarkerRe      = regexp.MustCompile(`(?i)[话話回]|ch(?:apter)?`)
	volumeMarkerRe       = regexp.MustCompile(`(?i)第\s*[0-9０-９]+(?:\.[0-9０-９]+)?\s*(卷|巻|集|册|冊)|(?:vol(?:ume)?|卷|巻|集|册|冊)\s*[_ .-]*[0-9０-９]+(?:\.[0-9０-９]+)?|[0-9０-９]+\s*[-~～至到]\s*[0-9０-９]+\s*(卷|巻|集|册|冊)`)
	rangeCountRe         = regexp.MustCompile(`(?i)([0-9０-９]+)\s*[-~～至到]\s*([0-9０-９]+)\s*(卷|巻|集|册|冊|话|話|回)`)
	leadingNumberRe      = regexp.MustCompile(`^[0-9]{1,4}[\s._-]+`)
	numberOnlyRe         = regexp.MustCompile(`^[0-9]+$`)
	collectionTitleRe    = regexp.MustCompile(`(?i)合集|作品集|短篇集|短編集|collection|anthology`)
	sectionSpecialLikeRe = regexp.MustCompile(`外传|外傳|番外|特典|资料|資料|设定|設定|公式|导读|導讀|周年|纪念|紀念|周边|周邊|附录|附錄`)
	archiveCoverNameRe   = regexp.MustCompile(`(?i)(^|[\s._-])(cover|folder|front|title)([\s._-]|$)`)
	archiveNumberNameRe  = regexp.MustCompile(`^\D*0*(\d{1,6})(?:\D|$)`)
	tagColorRe           = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

var (
	errPageManifestStale       = errors.New("page manifest stale")
	errUnsupportedReaderSource = errors.New("reader source is unsupported in this build")
)

type Server struct {
	db                  *sql.DB
	root                string
	authEnabled         bool
	localCacheRoot      string
	thumbnailCacheRoot  string
	pathMappings        []pathMapping
	reviewCacheMu       sync.Mutex
	reviewItemsCache    map[string][]map[string]any
	coverDuplicateMu    sync.Mutex
	coverDuplicateCache map[string]coverDuplicateResponseCache
	closeOnce           sync.Once
	closeErr            error
	thumbnailSem        chan struct{}
	renderLockMu        sync.Mutex
	renderLocks         map[string]*keyedRenderLock
	zipOpenReader       func(string) (*zip.ReadCloser, error)
	archiveLimits       archiveResourceLimits
}

type keyedRenderLock struct {
	ch   chan struct{}
	refs int
}

type pathMapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func NewServer(dbPath string) (*Server, error) {
	return newServer(dbPath, true)
}

func newServer(dbPath string, ensureCatalog bool) (*Server, error) {
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, err
	}
	dsn := sqliteDSN(abs)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := configureSQLiteConnection(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := rejectUnsupportedLegacyMaintenanceState(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureLocalSQLiteTables(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if ensureCatalog {
		if err := EnsureCatalogTables(db); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	pathMappings, err := loadPathMappingsFromEnv()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	root := filepath.Dir(filepath.Dir(abs))
	server := &Server{
		db:                  db,
		root:                root,
		authEnabled:         strings.TrimSpace(os.Getenv("BMANGA_AUTH_PASSWORD")) != "",
		localCacheRoot:      filepath.Join(root, "cache"),
		thumbnailCacheRoot:  filepath.Join(root, ".cache", "cover-thumbs"),
		pathMappings:        pathMappings,
		reviewItemsCache:    map[string][]map[string]any{},
		coverDuplicateCache: map[string]coverDuplicateResponseCache{},
		thumbnailSem:        make(chan struct{}, envIntInRange("BMANGA_THUMBNAIL_RENDER_CONCURRENCY", 4, 1, 16)),
		renderLocks:         map[string]*keyedRenderLock{},
		archiveLimits:       loadArchiveResourceLimits(),
	}
	return server, nil
}

func sqliteDSN(abs string) string {
	query := url.Values{}
	cacheKiB := envIntInRange("BMANGA_SQLITE_CACHE_KIB", 65536, 4096, 262144)
	mmapBytes := envInt64InRange("BMANGA_SQLITE_MMAP_BYTES", 0, 0, 1073741824)
	query.Add("_pragma", "busy_timeout(10000)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "synchronous(NORMAL)")
	query.Add("_pragma", fmt.Sprintf("cache_size(-%d)", cacheKiB))
	query.Add("_pragma", "temp_store(MEMORY)")
	query.Add("_pragma", fmt.Sprintf("mmap_size(%d)", mmapBytes))
	return "file:" + filepath.ToSlash(abs) + "?" + query.Encode()
}

func configureSQLiteConnection(db *sql.DB) error {
	maxOpenConns := envIntInRange("BMANGA_SQLITE_MAX_OPEN_CONNS", 8, 1, 64)
	defaultIdleConns := 4
	if maxOpenConns < defaultIdleConns {
		defaultIdleConns = maxOpenConns
	}
	maxIdleConns := envIntInRange("BMANGA_SQLITE_MAX_IDLE_CONNS", defaultIdleConns, 0, maxOpenConns)
	cacheKiB := envIntInRange("BMANGA_SQLITE_CACHE_KIB", 65536, 4096, 262144)
	mmapBytes := envInt64InRange("BMANGA_SQLITE_MMAP_BYTES", 0, 0, 1073741824)

	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxIdleTime(5 * time.Minute)
	for _, statement := range []string{
		"PRAGMA busy_timeout = 10000",
		fmt.Sprintf("PRAGMA cache_size = -%d", cacheKiB),
		"PRAGMA temp_store = MEMORY",
		fmt.Sprintf("PRAGMA mmap_size = %d", mmapBytes),
	} {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func envIntInRange(name string, fallback int, minValue int, maxValue int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	if parsed < minValue {
		return minValue
	}
	if parsed > maxValue {
		return maxValue
	}
	return parsed
}

func envInt64InRange(name string, fallback int64, minValue int64, maxValue int64) int64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	if parsed < minValue {
		return minValue
	}
	if parsed > maxValue {
		return maxValue
	}
	return parsed
}

func (s *Server) acquirePermit(ctx context.Context, sem chan struct{}) (func(), bool) {
	select {
	case sem <- struct{}{}:
		return func() {
			<-sem
		}, true
	case <-ctx.Done():
		return nil, false
	}
}

func (s *Server) acquireRenderLock(ctx context.Context, key string) (func(), bool) {
	if key == "" {
		key = "default"
	}
	s.renderLockMu.Lock()
	lock := s.renderLocks[key]
	if lock == nil {
		lock = &keyedRenderLock{ch: make(chan struct{}, 1)}
		s.renderLocks[key] = lock
	}
	lock.refs++
	s.renderLockMu.Unlock()

	select {
	case lock.ch <- struct{}{}:
		return func() {
			<-lock.ch
			s.renderLockMu.Lock()
			lock.refs--
			if lock.refs <= 0 {
				delete(s.renderLocks, key)
			}
			s.renderLockMu.Unlock()
		}, true
	case <-ctx.Done():
		s.renderLockMu.Lock()
		lock.refs--
		if lock.refs <= 0 {
			delete(s.renderLocks, key)
		}
		s.renderLockMu.Unlock()
		return nil, false
	}
}

func (s *Server) ensureCacheFile(ctx context.Context, cachePath string, lockKey string, sem chan struct{}, build func() error) (bool, error) {
	if _, err := os.Stat(cachePath); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	releaseLock, ok := s.acquireRenderLock(ctx, lockKey)
	if !ok {
		return false, ctx.Err()
	}
	defer releaseLock()

	if _, err := os.Stat(cachePath); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	releasePermit, ok := s.acquirePermit(ctx, sem)
	if !ok {
		return false, ctx.Err()
	}
	defer releasePermit()

	if _, err := os.Stat(cachePath); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := build(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Server) SetAuthEnabled(enabled bool) {
	s.authEnabled = enabled
}

type sqliteSchemaRunner interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

type sqliteConnRunner struct {
	ctx  context.Context
	conn *sql.Conn
}

func (runner sqliteConnRunner) Exec(query string, args ...any) (sql.Result, error) {
	return runner.conn.ExecContext(runner.ctx, query, args...)
}

func (runner sqliteConnRunner) Query(query string, args ...any) (*sql.Rows, error) {
	return runner.conn.QueryContext(runner.ctx, query, args...)
}

func (runner sqliteConnRunner) QueryRow(query string, args ...any) *sql.Row {
	return runner.conn.QueryRowContext(runner.ctx, query, args...)
}

func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		if s.db != nil {
			s.closeErr = s.db.Close()
		}
	})
	return s.closeErr
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/dashboard", s.handleDashboard)
	mux.HandleFunc("/api/discover", s.handleDiscover)
	mux.HandleFunc("/api/random-work", s.handleRandomWork)
	mux.HandleFunc("/api/reading-history", s.handleReadingHistory)
	mux.HandleFunc("/api/continue-target", s.handleContinueTarget)
	mux.HandleFunc("/api/works", s.handleWorks)
	mux.HandleFunc("/api/work", s.handleWork)
	mux.HandleFunc("/api/series", s.handleSeries)
	mux.HandleFunc("/api/series-detail", s.handleSeriesDetail)
	mux.HandleFunc("/api/shelf", s.handleShelf)
	mux.HandleFunc("/api/pages", s.handlePages)
	mux.HandleFunc("/api/progress", s.handleProgress)
	mux.HandleFunc("/api/progress-migration", s.handleProgressMigration)
	mux.HandleFunc("/api/series-progress", s.handleSeriesProgressGet)
	mux.HandleFunc("/api/browse-state", s.handleBrowseState)
	mux.HandleFunc("/api/library-page-state", s.handleLibraryPageState)
	mux.HandleFunc("/api/user-mark", s.handleUserMark)
	mux.HandleFunc("/api/tags", s.handleTags)
	mux.HandleFunc("/api/corrections/batch", s.handleCorrectionsBatch)
	mux.HandleFunc("/api/corrections", s.handleCorrections)
	mux.HandleFunc("/api/review-summary", s.handleReviewSummary)
	mux.HandleFunc("/api/review", s.handleReview)
	mux.HandleFunc("/api/cover-duplicates", s.handleCoverDuplicates)
	mux.HandleFunc("/api/duplicate-pair/status", s.handleDuplicatePairStatus)
	mux.HandleFunc("/api/duplicate-pair/evidence", s.handleDuplicatePairEvidence)
	mux.HandleFunc("/page", s.handlePageImage)
	mux.HandleFunc("/cover", s.handleCover)
	return readerTimingMiddleware(gzipTextResponses(s.sameOriginWriteGuard(mux)))
}
