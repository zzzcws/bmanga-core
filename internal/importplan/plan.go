// Package importplan builds a deterministic, read-only comparison between an
// explicitly selected intake directory and library directory. It never
// creates, moves, overwrites, or removes source files.
package importplan

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var (
	ErrPathBoundary  = errors.New("selected path is outside the explicit root")
	ErrReparsePoint  = errors.New("symbolic link or reparse point is not allowed")
	ErrResourceLimit = errors.New("import planning resource limit exceeded")
	ErrSourceChanged = errors.New("source changed while it was being planned")
)

const (
	defaultMaxFiles      = 10_000
	defaultMaxEntries    = 20_000
	defaultMaxFileBytes  = int64(2 * 1024 * 1024 * 1024)
	defaultMaxTotalBytes = int64(32 * 1024 * 1024 * 1024)
	defaultMaxDepth      = 32
	defaultMaxPathBytes  = 4_096

	hardMaxFiles      = 100_000
	hardMaxEntries    = 200_000
	hardMaxFileBytes  = int64(32 * 1024 * 1024 * 1024)
	hardMaxTotalBytes = int64(256 * 1024 * 1024 * 1024)
	hardMaxDepth      = 64
	hardMaxPathBytes  = 32_768

	maxMatchSamples = 8
)

// Limits bound the amount of untrusted intake data inspected by Build.
// Zero values select conservative defaults.
type Limits struct {
	MaxFiles      int
	MaxEntries    int
	MaxFileBytes  int64
	MaxTotalBytes int64
	MaxDepth      int
	MaxPathBytes  int
}

// DefaultLimits returns the limits used by the command-line planner.
func DefaultLimits() Limits {
	return Limits{
		MaxFiles:      defaultMaxFiles,
		MaxEntries:    defaultMaxEntries,
		MaxFileBytes:  defaultMaxFileBytes,
		MaxTotalBytes: defaultMaxTotalBytes,
		MaxDepth:      defaultMaxDepth,
		MaxPathBytes:  defaultMaxPathBytes,
	}
}

// Options identifies the only filesystem trees that Build may inspect.
// Intake and Library may be absolute or relative to Root, but both must resolve
// within Root and must not overlap.
type Options struct {
	Root    string
	Intake  string
	Library string
	Limits  Limits
}

// Plan is deterministic for unchanged trees. It intentionally records only
// paths relative to intake or library, never host-absolute paths.
type Plan struct {
	SchemaVersion     int      `json:"schemaVersion"`
	Mode              string   `json:"mode"`
	Intake            string   `json:"intake"`
	Library           string   `json:"library"`
	IntakeTreeSHA256  string   `json:"intakeTreeSha256"`
	LibraryTreeSHA256 string   `json:"libraryTreeSha256"`
	IntakeFiles       int      `json:"intakeFiles"`
	LibraryFiles      int      `json:"libraryFiles"`
	IntakeBytes       int64    `json:"intakeBytes"`
	LibraryBytes      int64    `json:"libraryBytes"`
	EligibleFiles     int      `json:"eligibleFiles"`
	Unsupported       int      `json:"unsupportedFiles"`
	New               int      `json:"new"`
	ExactMatch        int      `json:"exactMatches"`
	NameConflict      int      `json:"nameConflicts"`
	Items             []Item   `json:"items"`
	Notes             []string `json:"notes"`
}

// Item describes one regular file. Classification is one of new,
// exact-match, name-conflict, or unsupported.
type Item struct {
	Path             string  `json:"path"`
	Kind             string  `json:"kind"`
	Size             int64   `json:"size"`
	SHA256           string  `json:"sha256"`
	CollisionKey     string  `json:"collisionKey,omitempty"`
	Classification   string  `json:"classification"`
	MatchCount       int     `json:"matchCount"`
	MatchesTruncated bool    `json:"matchesTruncated"`
	Matches          []Match `json:"matches,omitempty"`
}

// Match identifies a read-only comparison result without exposing an
// absolute host path.
type Match struct {
	Scope string `json:"scope"`
	Path  string `json:"path"`
}

type treeRecord struct {
	kind   byte
	path   string
	size   int64
	digest string
}

type scanner struct {
	root       *os.Root
	limits     Limits
	items      []Item
	records    []treeRecord
	totalBytes int64
	entries    int
}

// Build reads and hashes the explicit intake tree without modifying it.
func Build(ctx context.Context, options Options) (Plan, error) {
	if ctx == nil {
		return Plan{}, errors.New("context is required")
	}
	if strings.TrimSpace(options.Root) == "" {
		return Plan{}, errors.New("explicit root is required")
	}
	if strings.TrimSpace(options.Intake) == "" {
		return Plan{}, errors.New("explicit intake is required")
	}
	if strings.TrimSpace(options.Library) == "" {
		return Plan{}, errors.New("explicit library is required")
	}
	limits, err := normalizeLimits(options.Limits)
	if err != nil {
		return Plan{}, err
	}

	rootAbs, err := filepath.Abs(options.Root)
	if err != nil {
		return Plan{}, fmt.Errorf("resolve root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)
	rootBefore, err := os.Lstat(rootAbs)
	if err != nil {
		return Plan{}, fmt.Errorf("inspect root: %w", err)
	}
	if !rootBefore.IsDir() {
		return Plan{}, errors.New("explicit root is not a directory")
	}
	if fileInfoIsReparsePoint(rootBefore) {
		return Plan{}, fmt.Errorf("%w: explicit root", ErrReparsePoint)
	}

	intakeRel, err := resolveIntakeRelative(rootAbs, options.Intake)
	if err != nil {
		return Plan{}, err
	}
	libraryRel, err := resolveIntakeRelative(rootAbs, options.Library)
	if err != nil {
		return Plan{}, fmt.Errorf("resolve library: %w", err)
	}
	if relativePathsOverlap(intakeRel, libraryRel) {
		return Plan{}, errors.New("intake and library must be disjoint directories")
	}
	root, err := os.OpenRoot(rootAbs)
	if err != nil {
		return Plan{}, fmt.Errorf("open explicit root: %w", err)
	}
	defer root.Close()
	if err := validateOpenedRoot(root, rootAbs, rootBefore); err != nil {
		return Plan{}, err
	}

	intakeScanner := &scanner{root: root, limits: limits}
	if err := intakeScanner.scanDirectory(ctx, intakeRel, ".", 0); err != nil {
		return Plan{}, err
	}
	libraryScanner := &scanner{root: root, limits: limits}
	if err := libraryScanner.scanDirectory(ctx, libraryRel, ".", 0); err != nil {
		return Plan{}, err
	}
	sort.Slice(intakeScanner.items, func(i, j int) bool { return intakeScanner.items[i].Path < intakeScanner.items[j].Path })
	sort.Slice(libraryScanner.items, func(i, j int) bool { return libraryScanner.items[i].Path < libraryScanner.items[j].Path })
	classify(intakeScanner.items, libraryScanner.items)
	sort.Slice(intakeScanner.records, func(i, j int) bool {
		if intakeScanner.records[i].path == intakeScanner.records[j].path {
			return intakeScanner.records[i].kind < intakeScanner.records[j].kind
		}
		return intakeScanner.records[i].path < intakeScanner.records[j].path
	})
	sort.Slice(libraryScanner.records, func(i, j int) bool {
		if libraryScanner.records[i].path == libraryScanner.records[j].path {
			return libraryScanner.records[i].kind < libraryScanner.records[j].kind
		}
		return libraryScanner.records[i].path < libraryScanner.records[j].path
	})

	plan := Plan{
		SchemaVersion:     1,
		Mode:              "dry-run-read-only",
		Intake:            filepath.ToSlash(intakeRel),
		Library:           filepath.ToSlash(libraryRel),
		IntakeTreeSHA256:  hashTreeRecords(intakeScanner.records),
		LibraryTreeSHA256: hashTreeRecords(libraryScanner.records),
		IntakeFiles:       len(intakeScanner.items),
		LibraryFiles:      len(libraryScanner.items),
		IntakeBytes:       intakeScanner.totalBytes,
		LibraryBytes:      libraryScanner.totalBytes,
		Items:             intakeScanner.items,
		Notes: []string{
			"No intake or library file was copied, moved, overwritten, or deleted.",
			"Paths are relative to their explicit intake or library directory.",
			"Exact matches require equal size and SHA-256; name conflicts use the same normalized relative path with different content.",
			"Each item retains at most 8 deterministic match examples; matchCount records the complete total.",
			"Extension-based eligibility does not validate that an archive is readable or structurally valid.",
			"The selected trees should remain unchanged while planning, and the plan must be rerun before any later operator action.",
		},
	}
	for _, item := range plan.Items {
		switch item.Classification {
		case "unsupported":
			plan.Unsupported++
		case "new":
			plan.EligibleFiles++
			plan.New++
		case "exact-match":
			plan.EligibleFiles++
			plan.ExactMatch++
		case "name-conflict":
			plan.EligibleFiles++
			plan.NameConflict++
		}
	}
	return plan, nil
}

func normalizeLimits(input Limits) (Limits, error) {
	defaults := DefaultLimits()
	if input.MaxFiles == 0 {
		input.MaxFiles = defaults.MaxFiles
	}
	if input.MaxEntries == 0 {
		input.MaxEntries = defaults.MaxEntries
	}
	if input.MaxFileBytes == 0 {
		input.MaxFileBytes = defaults.MaxFileBytes
	}
	if input.MaxTotalBytes == 0 {
		input.MaxTotalBytes = defaults.MaxTotalBytes
	}
	if input.MaxDepth == 0 {
		input.MaxDepth = defaults.MaxDepth
	}
	if input.MaxPathBytes == 0 {
		input.MaxPathBytes = defaults.MaxPathBytes
	}
	if input.MaxFiles < 1 || input.MaxFiles > hardMaxFiles {
		return Limits{}, fmt.Errorf("max files must be between 1 and %d", hardMaxFiles)
	}
	if input.MaxEntries < 1 || input.MaxEntries > hardMaxEntries {
		return Limits{}, fmt.Errorf("max entries must be between 1 and %d", hardMaxEntries)
	}
	if input.MaxFileBytes < 1 || input.MaxFileBytes > hardMaxFileBytes {
		return Limits{}, fmt.Errorf("max file bytes must be between 1 and %d", hardMaxFileBytes)
	}
	if input.MaxTotalBytes < 1 || input.MaxTotalBytes > hardMaxTotalBytes {
		return Limits{}, fmt.Errorf("max total bytes must be between 1 and %d", hardMaxTotalBytes)
	}
	if input.MaxDepth < 1 || input.MaxDepth > hardMaxDepth {
		return Limits{}, fmt.Errorf("max depth must be between 1 and %d", hardMaxDepth)
	}
	if input.MaxPathBytes < 1 || input.MaxPathBytes > hardMaxPathBytes {
		return Limits{}, fmt.Errorf("max path bytes must be between 1 and %d", hardMaxPathBytes)
	}
	return input, nil
}

func resolveIntakeRelative(rootAbs, intake string) (string, error) {
	intakeAbs := intake
	if !filepath.IsAbs(intakeAbs) {
		intakeAbs = filepath.Join(rootAbs, intakeAbs)
	}
	intakeAbs, err := filepath.Abs(intakeAbs)
	if err != nil {
		return "", fmt.Errorf("resolve intake: %w", err)
	}
	intakeAbs = filepath.Clean(intakeAbs)
	relative, err := filepath.Rel(rootAbs, intakeAbs)
	if err != nil {
		return "", fmt.Errorf("compare intake with root: %w", err)
	}
	if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrPathBoundary
	}
	if relative == "" {
		relative = "."
	}
	return filepath.Clean(relative), nil
}

func relativePathsOverlap(first, second string) bool {
	first = collisionKey(filepath.Clean(first))
	second = collisionKey(filepath.Clean(second))
	if first == "." || second == "." || first == second {
		return true
	}
	return strings.HasPrefix(first, second+"/") || strings.HasPrefix(second, first+"/")
}

func validateOpenedRoot(root *os.Root, rootAbs string, before os.FileInfo) error {
	handle, err := root.Open(".")
	if err != nil {
		return fmt.Errorf("pin explicit root: %w", err)
	}
	opened, statErr := handle.Stat()
	closeErr := handle.Close()
	if statErr != nil {
		return fmt.Errorf("stat pinned root: %w", statErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close pinned root: %w", closeErr)
	}
	after, err := os.Lstat(rootAbs)
	if err != nil {
		return fmt.Errorf("recheck explicit root: %w", err)
	}
	if !opened.IsDir() || fileInfoIsReparsePoint(after) || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		return fmt.Errorf("%w: explicit root changed", ErrSourceChanged)
	}
	return nil
}

func (s *scanner) scanDirectory(ctx context.Context, rootRelative, displayRelative string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > s.limits.MaxDepth {
		return fmt.Errorf("%w: directory depth exceeds %d", ErrResourceLimit, s.limits.MaxDepth)
	}
	if len([]byte(filepath.ToSlash(displayRelative))) > s.limits.MaxPathBytes {
		return fmt.Errorf("%w: path exceeds %d bytes", ErrResourceLimit, s.limits.MaxPathBytes)
	}
	directory, err := s.openPinnedDirectory(rootRelative)
	if err != nil {
		return err
	}
	entries, readErr := s.readDirectoryEntries(directory)
	closeErr := directory.Close()
	if readErr != nil {
		return fmt.Errorf("read selected directory %q: %w", filepath.ToSlash(displayRelative), readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close selected directory %q: %w", filepath.ToSlash(displayRelative), closeErr)
	}
	s.records = append(s.records, treeRecord{kind: 'd', path: filepath.ToSlash(displayRelative)})
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		childRoot := filepath.Join(rootRelative, entry.Name())
		childDisplay := entry.Name()
		if displayRelative != "." {
			childDisplay = filepath.Join(displayRelative, entry.Name())
		}
		childSlash := filepath.ToSlash(childDisplay)
		if len([]byte(childSlash)) > s.limits.MaxPathBytes {
			return fmt.Errorf("%w: path exceeds %d bytes", ErrResourceLimit, s.limits.MaxPathBytes)
		}
		info, err := s.root.Lstat(childRoot)
		if err != nil {
			return fmt.Errorf("inspect %q: %w", childSlash, err)
		}
		if fileInfoIsReparsePoint(info) {
			return fmt.Errorf("%w: %q", ErrReparsePoint, childSlash)
		}
		switch {
		case info.IsDir():
			if err := s.scanDirectory(ctx, childRoot, childDisplay, depth+1); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if len(s.items) >= s.limits.MaxFiles {
				return fmt.Errorf("%w: file count exceeds %d", ErrResourceLimit, s.limits.MaxFiles)
			}
			if info.Size() > s.limits.MaxFileBytes {
				return fmt.Errorf("%w: %q exceeds %d bytes", ErrResourceLimit, childSlash, s.limits.MaxFileBytes)
			}
			if info.Size() < 0 || s.totalBytes > s.limits.MaxTotalBytes-info.Size() {
				return fmt.Errorf("%w: total bytes exceed %d", ErrResourceLimit, s.limits.MaxTotalBytes)
			}
			item, err := s.hashPinnedFile(ctx, childRoot, childSlash)
			if err != nil {
				return err
			}
			if item.Size < 0 || s.totalBytes > s.limits.MaxTotalBytes-item.Size {
				return fmt.Errorf("%w: total bytes exceed %d", ErrResourceLimit, s.limits.MaxTotalBytes)
			}
			s.totalBytes += item.Size
			s.items = append(s.items, item)
			s.records = append(s.records, treeRecord{kind: 'f', path: item.Path, size: item.Size, digest: item.SHA256})
		default:
			return fmt.Errorf("%w: unsupported filesystem object %q", ErrPathBoundary, childSlash)
		}
	}
	return nil
}

func (s *scanner) readDirectoryEntries(directory *os.File) ([]os.DirEntry, error) {
	const batchSize = 256
	var entries []os.DirEntry
	for {
		batch, err := directory.ReadDir(batchSize)
		if len(batch) > 0 {
			if s.entries > s.limits.MaxEntries-len(batch) {
				return nil, fmt.Errorf("%w: directory entries exceed %d", ErrResourceLimit, s.limits.MaxEntries)
			}
			s.entries += len(batch)
			entries = append(entries, batch...)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

func (s *scanner) openPinnedDirectory(relative string) (*os.File, error) {
	before, err := s.validateRelativeComponents(relative)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() {
		return nil, fmt.Errorf("selected path is not a directory")
	}
	directory, err := s.root.Open(relative)
	if err != nil {
		return nil, fmt.Errorf("open selected directory: %w", err)
	}
	opened, err := directory.Stat()
	if err != nil {
		_ = directory.Close()
		return nil, fmt.Errorf("stat selected directory: %w", err)
	}
	after, err := s.validateRelativeComponents(relative)
	if err != nil || !opened.IsDir() || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		_ = directory.Close()
		if err == nil {
			err = ErrSourceChanged
		}
		return nil, fmt.Errorf("pin selected directory: %w", err)
	}
	return directory, nil
}

func (s *scanner) hashPinnedFile(ctx context.Context, rootRelative, display string) (Item, error) {
	before, err := s.validateRelativeComponents(rootRelative)
	if err != nil {
		return Item{}, err
	}
	if !before.Mode().IsRegular() {
		return Item{}, fmt.Errorf("%w: %q is not a regular file", ErrPathBoundary, display)
	}
	file, err := s.root.Open(rootRelative)
	if err != nil {
		return Item{}, fmt.Errorf("open %q: %w", display, err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return Item{}, fmt.Errorf("stat %q: %w", display, err)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return Item{}, fmt.Errorf("%w: %q changed before hashing", ErrSourceChanged, display)
	}
	if opened.Size() > s.limits.MaxFileBytes {
		return Item{}, fmt.Errorf("%w: %q exceeds %d bytes", ErrResourceLimit, display, s.limits.MaxFileBytes)
	}

	digest := sha256.New()
	written, err := copyWithContext(ctx, digest, io.LimitReader(file, s.limits.MaxFileBytes+1))
	if err != nil {
		return Item{}, fmt.Errorf("hash %q: %w", display, err)
	}
	if written > s.limits.MaxFileBytes {
		return Item{}, fmt.Errorf("%w: %q grew beyond %d bytes", ErrResourceLimit, display, s.limits.MaxFileBytes)
	}
	afterHandle, err := file.Stat()
	if err != nil {
		return Item{}, fmt.Errorf("restat %q: %w", display, err)
	}
	afterPath, err := s.validateRelativeComponents(rootRelative)
	if err != nil {
		return Item{}, err
	}
	if written != opened.Size() || afterHandle.Size() != opened.Size() || !afterHandle.ModTime().Equal(opened.ModTime()) ||
		!os.SameFile(opened, afterHandle) || !os.SameFile(afterHandle, afterPath) {
		return Item{}, fmt.Errorf("%w: %q changed while hashing", ErrSourceChanged, display)
	}
	kind := fileKind(display)
	item := Item{
		Path:           display,
		Kind:           kind,
		Size:           written,
		SHA256:         hex.EncodeToString(digest.Sum(nil)),
		Classification: "new",
	}
	if kind == "unsupported" {
		item.Classification = "unsupported"
	} else {
		item.CollisionKey = collisionKey(display)
	}
	return item, nil
}

func (s *scanner) validateRelativeComponents(relative string) (os.FileInfo, error) {
	clean := filepath.Clean(relative)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, ErrPathBoundary
	}
	if clean == "." {
		info, err := s.root.Lstat(".")
		if err != nil {
			return nil, err
		}
		if fileInfoIsReparsePoint(info) {
			return nil, ErrReparsePoint
		}
		return info, nil
	}
	current := ""
	var final os.FileInfo
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if part == "" || part == "." || part == ".." {
			return nil, ErrPathBoundary
		}
		if current == "" {
			current = part
		} else {
			current = filepath.Join(current, part)
		}
		info, err := s.root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if fileInfoIsReparsePoint(info) {
			return nil, fmt.Errorf("%w: %q", ErrReparsePoint, filepath.ToSlash(current))
		}
		final = info
	}
	return final, nil
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 64*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			written, writeErr := destination.Write(buffer[:count])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != count {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			return total, readErr
		}
	}
}

func fileKind(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".gif", ".jpeg", ".jpg", ".png", ".webp":
		return "image"
	case ".cbz", ".zip":
		return "archive"
	case ".epub":
		return "ebook"
	default:
		return "unsupported"
	}
}

func collisionKey(path string) string {
	return cases.Fold().String(norm.NFC.String(filepath.ToSlash(path)))
}

type matchSummary struct {
	count   int
	matches []Match
}

type conflictGroup struct {
	indexes        []int
	contentCounts  map[string]int
	firstContent   string
	defaultSamples []int
	samplesWithout map[string][]int
}

func classify(intake, library []Item) {
	libraryByDigestAndSize := buildIndexGroups(library, func(item Item) string { return contentKey(item) })
	libraryByTarget := buildConflictGroups(library)
	intakeByDigestAndSize := buildIndexGroups(intake, func(item Item) string { return contentKey(item) })
	intakeByTarget := buildConflictGroups(intake)
	for index := range intake {
		intake[index].MatchCount = 0
		intake[index].MatchesTruncated = false
		intake[index].Matches = nil
		if intake[index].Kind == "unsupported" {
			intake[index].Classification = "unsupported"
			continue
		}
		intake[index].Classification = "new"
		conflicts := combineMatchSummaries(
			differentContentSummary(intake[index], intake, intakeByTarget[intake[index].CollisionKey], "intake"),
			differentContentSummary(intake[index], library, libraryByTarget[intake[index].CollisionKey], "library"),
		)
		if conflicts.count > 0 {
			applyMatchSummary(&intake[index], "name-conflict", conflicts)
			continue
		}
		exact := combineMatchSummaries(
			exactMatchSummary(intake, intakeByDigestAndSize[contentKey(intake[index])], "intake", index),
			exactMatchSummary(library, libraryByDigestAndSize[contentKey(intake[index])], "library", -1),
		)
		if exact.count > 0 {
			applyMatchSummary(&intake[index], "exact-match", exact)
		}
	}
}

func contentKey(item Item) string {
	return fmt.Sprintf("%d:%s", item.Size, item.SHA256)
}

func buildIndexGroups(items []Item, key func(Item) string) map[string][]int {
	groups := make(map[string][]int)
	for index := range items {
		if items[index].Kind == "unsupported" {
			continue
		}
		groupKey := key(items[index])
		groups[groupKey] = append(groups[groupKey], index)
	}
	for _, indexes := range groups {
		sortIndexesByPath(indexes, items)
	}
	return groups
}

func buildConflictGroups(items []Item) map[string]conflictGroup {
	rawGroups := buildIndexGroups(items, func(item Item) string { return item.CollisionKey })
	groups := make(map[string]conflictGroup, len(rawGroups))
	for key, indexes := range rawGroups {
		group := conflictGroup{indexes: indexes}
		if len(indexes) > 0 {
			group.firstContent = contentKey(items[indexes[0]])
		}
		if len(indexes) > 1 {
			group.contentCounts = make(map[string]int)
			for _, index := range indexes {
				group.contentCounts[contentKey(items[index])]++
			}
		}
		sampleCount := min(maxMatchSamples, len(indexes))
		group.defaultSamples = append([]int(nil), indexes[:sampleCount]...)
		for _, index := range group.defaultSamples {
			excludedContent := contentKey(items[index])
			if group.contentCount(excludedContent) == len(indexes) {
				continue
			}
			if group.samplesWithout == nil {
				group.samplesWithout = make(map[string][]int)
			}
			if _, exists := group.samplesWithout[excludedContent]; exists {
				continue
			}
			samples := make([]int, 0, maxMatchSamples)
			for _, candidateIndex := range indexes {
				if contentKey(items[candidateIndex]) == excludedContent {
					continue
				}
				samples = append(samples, candidateIndex)
				if len(samples) == maxMatchSamples {
					break
				}
			}
			group.samplesWithout[excludedContent] = samples
		}
		groups[key] = group
	}
	return groups
}

func (group conflictGroup) contentCount(key string) int {
	if len(group.indexes) == 0 {
		return 0
	}
	if group.contentCounts == nil {
		if key == group.firstContent {
			return 1
		}
		return 0
	}
	return group.contentCounts[key]
}

func differentContentSummary(item Item, candidates []Item, group conflictGroup, scope string) matchSummary {
	itemContent := contentKey(item)
	result := matchSummary{count: len(group.indexes) - group.contentCount(itemContent)}
	if result.count == 0 {
		return result
	}
	sampleIndexes, customized := group.samplesWithout[itemContent]
	if !customized {
		sampleIndexes = group.defaultSamples
	}
	result.matches = make([]Match, 0, min(maxMatchSamples, result.count))
	for _, index := range sampleIndexes {
		if contentKey(candidates[index]) == itemContent {
			continue
		}
		result.matches = append(result.matches, Match{Scope: scope, Path: candidates[index].Path})
		if len(result.matches) == maxMatchSamples {
			break
		}
	}
	return result
}

func exactMatchSummary(items []Item, indexes []int, scope string, excludeIndex int) matchSummary {
	result := matchSummary{count: len(indexes)}
	if excludeIndex >= 0 {
		result.count--
	}
	if result.count <= 0 {
		result.count = 0
		return result
	}
	result.matches = make([]Match, 0, min(maxMatchSamples, result.count))
	for _, index := range indexes {
		if index == excludeIndex {
			continue
		}
		result.matches = append(result.matches, Match{Scope: scope, Path: items[index].Path})
		if len(result.matches) == maxMatchSamples {
			break
		}
	}
	return result
}

func combineMatchSummaries(parts ...matchSummary) matchSummary {
	result := matchSummary{}
	for _, part := range parts {
		result.count += part.count
		remaining := maxMatchSamples - len(result.matches)
		if remaining <= 0 {
			continue
		}
		if remaining > len(part.matches) {
			remaining = len(part.matches)
		}
		result.matches = append(result.matches, part.matches[:remaining]...)
	}
	return result
}

func applyMatchSummary(item *Item, classification string, summary matchSummary) {
	item.Classification = classification
	item.MatchCount = summary.count
	item.Matches = summary.matches
	item.MatchesTruncated = summary.count > len(summary.matches)
}

func sortIndexesByPath(indexes []int, items []Item) {
	sort.Slice(indexes, func(i, j int) bool {
		left := items[indexes[i]].Path
		right := items[indexes[j]].Path
		if left == right {
			return indexes[i] < indexes[j]
		}
		return left < right
	})
}

func hashTreeRecords(records []treeRecord) string {
	digest := sha256.New()
	for _, record := range records {
		_, _ = digest.Write([]byte{record.kind})
		writeDigestField(digest, []byte(record.path))
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(record.size))
		_, _ = digest.Write(size[:])
		writeDigestField(digest, []byte(record.digest))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func writeDigestField(destination hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}
