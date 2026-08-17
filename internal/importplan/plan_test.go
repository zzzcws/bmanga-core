package importplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestBuildClassifiesAgainstLibraryAndPreservesBothTrees(t *testing.T) {
	root := t.TempDir()
	intake := filepath.Join(root, "intake")
	library := filepath.Join(root, "library")
	mustMkdirAll(t, filepath.Join(intake, "nested"))
	mustMkdirAll(t, filepath.Join(library, "archive"))

	mustWriteFile(t, filepath.Join(intake, "Fresh.cbz"), "fresh")
	mustWriteFile(t, filepath.Join(intake, "SameElsewhere.cbz"), "same-content")
	mustWriteFile(t, filepath.Join(intake, "Conflict.cbz"), "incoming")
	mustWriteFile(t, filepath.Join(intake, "nested", "notes.txt"), "not a supported input")
	mustWriteFile(t, filepath.Join(library, "archive", "Existing.cbz"), "same-content")
	mustWriteFile(t, filepath.Join(library, "Conflict.cbz"), "already-present")

	intakeBefore := fixtureTreeDigest(t, intake)
	libraryBefore := fixtureTreeDigest(t, library)
	plan, err := Build(context.Background(), Options{
		Root:    root,
		Intake:  "intake",
		Library: "library",
	})
	if err != nil {
		t.Fatal(err)
	}
	if intakeAfter := fixtureTreeDigest(t, intake); intakeAfter != intakeBefore {
		t.Fatalf("intake tree changed: before %s, after %s", intakeBefore, intakeAfter)
	}
	if libraryAfter := fixtureTreeDigest(t, library); libraryAfter != libraryBefore {
		t.Fatalf("library tree changed: before %s, after %s", libraryBefore, libraryAfter)
	}
	if plan.Mode != "dry-run-read-only" || plan.Intake != "intake" || plan.Library != "library" {
		t.Fatalf("unexpected plan header: %#v", plan)
	}
	if plan.IntakeFiles != 4 || plan.LibraryFiles != 2 || plan.New != 1 || plan.ExactMatch != 1 || plan.NameConflict != 1 || plan.Unsupported != 1 {
		t.Fatalf("unexpected plan counts: %#v", plan)
	}

	fresh := itemByPath(t, plan.Items, "Fresh.cbz")
	if fresh.Classification != "new" || len(fresh.Matches) != 0 {
		t.Fatalf("fresh item = %#v", fresh)
	}
	exact := itemByPath(t, plan.Items, "SameElsewhere.cbz")
	if exact.Classification != "exact-match" || exact.MatchCount != 1 || exact.MatchesTruncated || len(exact.Matches) != 1 || exact.Matches[0] != (Match{Scope: "library", Path: "archive/Existing.cbz"}) {
		t.Fatalf("exact item = %#v", exact)
	}
	conflict := itemByPath(t, plan.Items, "Conflict.cbz")
	if conflict.Classification != "name-conflict" || conflict.MatchCount != 1 || conflict.MatchesTruncated || len(conflict.Matches) != 1 || conflict.Matches[0] != (Match{Scope: "library", Path: "Conflict.cbz"}) {
		t.Fatalf("conflict item = %#v", conflict)
	}
	unsupported := itemByPath(t, plan.Items, "nested/notes.txt")
	if unsupported.Classification != "unsupported" || unsupported.CollisionKey != "" {
		t.Fatalf("unsupported item = %#v", unsupported)
	}
}

func TestClassifyRequiresSizeAndSHA256ForExactMatch(t *testing.T) {
	intake := []Item{{Path: "Book.cbz", Kind: "archive", Size: 5, SHA256: "digest", CollisionKey: collisionKey("Book.cbz"), Classification: "new"}}
	library := []Item{{Path: "Elsewhere.cbz", Kind: "archive", Size: 6, SHA256: "digest", CollisionKey: collisionKey("Elsewhere.cbz"), Classification: "new"}}
	classify(intake, library)
	if intake[0].Classification != "new" {
		t.Fatalf("classification = %q, want new", intake[0].Classification)
	}
}

func TestClassifyMergesLibraryAndIntakeExactMatchesDeterministically(t *testing.T) {
	intake := []Item{
		{Path: "A.cbz", Kind: "archive", Size: 5, SHA256: "same", CollisionKey: collisionKey("A.cbz"), Classification: "new"},
		{Path: "B.cbz", Kind: "archive", Size: 5, SHA256: "same", CollisionKey: collisionKey("B.cbz"), Classification: "new"},
	}
	library := []Item{
		{Path: "z/Existing.cbz", Kind: "archive", Size: 5, SHA256: "same", CollisionKey: collisionKey("z/Existing.cbz"), Classification: "new"},
	}
	classify(intake, library)
	wantFirst := []Match{
		{Scope: "intake", Path: "B.cbz"},
		{Scope: "library", Path: "z/Existing.cbz"},
	}
	wantSecond := []Match{
		{Scope: "intake", Path: "A.cbz"},
		{Scope: "library", Path: "z/Existing.cbz"},
	}
	if intake[0].Classification != "exact-match" || intake[0].MatchCount != 2 || intake[0].MatchesTruncated || !matchesEqual(intake[0].Matches, wantFirst) {
		t.Fatalf("first item = %#v, want matches %#v", intake[0], wantFirst)
	}
	if intake[1].Classification != "exact-match" || intake[1].MatchCount != 2 || intake[1].MatchesTruncated || !matchesEqual(intake[1].Matches, wantSecond) {
		t.Fatalf("second item = %#v, want matches %#v", intake[1], wantSecond)
	}
}

func TestClassifyMergesNameConflictScopesDeterministically(t *testing.T) {
	intake := []Item{
		{Path: "A.cbz", Kind: "archive", Size: 1, SHA256: "a", CollisionKey: "same-target", Classification: "new"},
		{Path: "B.cbz", Kind: "archive", Size: 2, SHA256: "b", CollisionKey: "same-target", Classification: "new"},
	}
	library := []Item{
		{Path: "C.cbz", Kind: "archive", Size: 3, SHA256: "c", CollisionKey: "same-target", Classification: "new"},
	}
	classify(intake, library)
	want := []Match{
		{Scope: "intake", Path: "B.cbz"},
		{Scope: "library", Path: "C.cbz"},
	}
	if intake[0].Classification != "name-conflict" || intake[0].MatchCount != 2 || intake[0].MatchesTruncated || !matchesEqual(intake[0].Matches, want) {
		t.Fatalf("item = %#v, want matches %#v", intake[0], want)
	}
}

func TestClassifyNameConflictSamplesPastDominantPrefix(t *testing.T) {
	items := make([]Item, 12)
	for index := range items {
		items[index] = Item{
			Path:           fmt.Sprintf("book-%02d.cbz", index),
			Kind:           "archive",
			Size:           1,
			SHA256:         "dominant-content",
			CollisionKey:   "same-target",
			Classification: "new",
		}
	}
	items[10].Size, items[10].SHA256 = 2, "tail-content-b"
	items[11].Size, items[11].SHA256 = 3, "tail-content-c"

	classify(items, nil)
	wantDominant := []Match{
		{Scope: "intake", Path: "book-10.cbz"},
		{Scope: "intake", Path: "book-11.cbz"},
	}
	if items[0].MatchCount != 2 || items[0].MatchesTruncated || !matchesEqual(items[0].Matches, wantDominant) {
		t.Fatalf("dominant-content item = %#v, want matches %#v", items[0], wantDominant)
	}
	if items[10].MatchCount != 11 || len(items[10].Matches) != maxMatchSamples || !items[10].MatchesTruncated {
		t.Fatalf("tail-content item = %#v", items[10])
	}
}

func TestClassifyLargeGroupsUsesBoundedDeterministicSamples(t *testing.T) {
	const (
		smallSize = 256
		largeSize = 2_048
	)
	tests := []struct {
		name           string
		classification string
		items          func(int) []Item
	}{
		{name: "exact matches", classification: "exact-match", items: exactDuplicateItems},
		{name: "name conflicts", classification: "name-conflict", items: nameConflictItems},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			smallBody := classifiedItemsJSON(t, test.items(smallSize))
			largeItems := test.items(largeSize)
			largeBody := classifiedItemsJSON(t, largeItems)
			repeatedBody := classifiedItemsJSON(t, largeItems)
			if string(largeBody) != string(repeatedBody) {
				t.Fatal("classification output changed when the same group was classified again")
			}
			if len(largeBody) > len(smallBody)*10 {
				t.Fatalf("encoded output grew faster than linearly: %d bytes for %d items, %d bytes for %d items", len(smallBody), smallSize, len(largeBody), largeSize)
			}
			var retainedSamples int
			for _, item := range largeItems {
				if item.Classification != test.classification {
					t.Fatalf("classification for %q = %q, want %q", item.Path, item.Classification, test.classification)
				}
				if item.MatchCount != largeSize-1 {
					t.Fatalf("matchCount for %q = %d, want %d", item.Path, item.MatchCount, largeSize-1)
				}
				if len(item.Matches) != maxMatchSamples || !item.MatchesTruncated {
					t.Fatalf("bounded matches for %q = %d, truncated = %v", item.Path, len(item.Matches), item.MatchesTruncated)
				}
				retainedSamples += len(item.Matches)
			}
			if retainedSamples != largeSize*maxMatchSamples {
				t.Fatalf("retained samples = %d, want %d", retainedSamples, largeSize*maxMatchSamples)
			}
		})
	}
}

func TestBuildRecognizesExactDuplicatesWithinIntake(t *testing.T) {
	root := fixtureRoots(t)
	mustWriteFile(t, filepath.Join(root, "intake", "A.cbz"), "same")
	mustWriteFile(t, filepath.Join(root, "intake", "B.cbz"), "same")
	plan, err := Build(context.Background(), Options{Root: root, Intake: "intake", Library: "library"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ExactMatch != 2 || plan.New != 0 {
		t.Fatalf("plan counts = %#v", plan)
	}
	first := itemByPath(t, plan.Items, "A.cbz")
	second := itemByPath(t, plan.Items, "B.cbz")
	if first.Classification != "exact-match" || first.MatchCount != 1 || first.MatchesTruncated || !matchesEqual(first.Matches, []Match{{Scope: "intake", Path: "B.cbz"}}) {
		t.Fatalf("first item = %#v", first)
	}
	if second.Classification != "exact-match" || second.MatchCount != 1 || second.MatchesTruncated || !matchesEqual(second.Matches, []Match{{Scope: "intake", Path: "A.cbz"}}) {
		t.Fatalf("second item = %#v", second)
	}
}

func TestClassifyNameConflictUsesNormalizedRelativePath(t *testing.T) {
	intake := []Item{{Path: "Series/Book.cbz", Kind: "archive", Size: 5, SHA256: "incoming", CollisionKey: collisionKey("Series/Book.cbz"), Classification: "new"}}
	library := []Item{{Path: "series/book.cbz", Kind: "archive", Size: 7, SHA256: "existing", CollisionKey: collisionKey("series/book.cbz"), Classification: "new"}}
	classify(intake, library)
	if intake[0].Classification != "name-conflict" || intake[0].MatchCount != 1 || intake[0].MatchesTruncated || len(intake[0].Matches) != 1 {
		t.Fatalf("classified item = %#v", intake[0])
	}
}

func TestBuildRejectsOutsideAndOverlappingSelections(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "intake"))
	mustMkdirAll(t, filepath.Join(root, "library"))
	outside := t.TempDir()

	_, err := Build(context.Background(), Options{Root: root, Intake: outside, Library: "library"})
	if !errors.Is(err, ErrPathBoundary) {
		t.Fatalf("outside error = %v, want ErrPathBoundary", err)
	}
	_, err = Build(context.Background(), Options{Root: root, Intake: "intake", Library: "."})
	if err == nil || !strings.Contains(err.Error(), "disjoint") {
		t.Fatalf("overlap error = %v, want disjoint selection error", err)
	}
}

func TestBuildRejectsSymbolicLinksOrReparsePoints(t *testing.T) {
	root := t.TempDir()
	intake := filepath.Join(root, "intake")
	library := filepath.Join(root, "library")
	mustMkdirAll(t, intake)
	mustMkdirAll(t, library)
	mustWriteFile(t, filepath.Join(library, "target.cbz"), "target")
	if err := os.Symlink(filepath.Join(library, "target.cbz"), filepath.Join(intake, "linked.cbz")); err != nil {
		t.Skipf("symbolic links are unavailable in this test environment: %v", err)
	}
	_, err := Build(context.Background(), Options{Root: root, Intake: "intake", Library: "library"})
	if !errors.Is(err, ErrReparsePoint) {
		t.Fatalf("error = %v, want ErrReparsePoint", err)
	}
}

func TestBuildEnforcesBoundedDirectoryReadsAndFileLimits(t *testing.T) {
	t.Run("entries", func(t *testing.T) {
		root := fixtureRoots(t)
		for index := 0; index < 3; index++ {
			mustMkdirAll(t, filepath.Join(root, "intake", string(rune('a'+index))))
		}
		_, err := Build(context.Background(), Options{
			Root: root, Intake: "intake", Library: "library",
			Limits: Limits{MaxEntries: 2},
		})
		if !errors.Is(err, ErrResourceLimit) {
			t.Fatalf("error = %v, want ErrResourceLimit", err)
		}
	})

	t.Run("files", func(t *testing.T) {
		root := fixtureRoots(t)
		mustWriteFile(t, filepath.Join(root, "intake", "one.cbz"), "1")
		mustWriteFile(t, filepath.Join(root, "intake", "two.cbz"), "2")
		_, err := Build(context.Background(), Options{
			Root: root, Intake: "intake", Library: "library",
			Limits: Limits{MaxFiles: 1},
		})
		if !errors.Is(err, ErrResourceLimit) {
			t.Fatalf("error = %v, want ErrResourceLimit", err)
		}
	})

	t.Run("single file bytes", func(t *testing.T) {
		root := fixtureRoots(t)
		mustWriteFile(t, filepath.Join(root, "intake", "large.cbz"), "1234")
		_, err := Build(context.Background(), Options{
			Root: root, Intake: "intake", Library: "library",
			Limits: Limits{MaxFileBytes: 3},
		})
		if !errors.Is(err, ErrResourceLimit) {
			t.Fatalf("error = %v, want ErrResourceLimit", err)
		}
	})
}

func TestBuildEnforcesAdditionalResourceLimits(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, root string)
		limits Limits
	}{
		{
			name: "total bytes",
			setup: func(t *testing.T, root string) {
				mustWriteFile(t, filepath.Join(root, "intake", "one.cbz"), "123")
				mustWriteFile(t, filepath.Join(root, "intake", "two.cbz"), "456")
			},
			limits: Limits{MaxTotalBytes: 5},
		},
		{
			name: "depth",
			setup: func(t *testing.T, root string) {
				mustMkdirAll(t, filepath.Join(root, "intake", "a", "b"))
				mustWriteFile(t, filepath.Join(root, "intake", "a", "b", "book.cbz"), "x")
			},
			limits: Limits{MaxDepth: 1},
		},
		{
			name: "relative path bytes",
			setup: func(t *testing.T, root string) {
				mustWriteFile(t, filepath.Join(root, "intake", "long-name.cbz"), "x")
			},
			limits: Limits{MaxPathBytes: 5},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := fixtureRoots(t)
			test.setup(t, root)
			_, err := Build(context.Background(), Options{
				Root: root, Intake: "intake", Library: "library", Limits: test.limits,
			})
			if !errors.Is(err, ErrResourceLimit) {
				t.Fatalf("error = %v, want ErrResourceLimit", err)
			}
		})
	}
}

func fixtureRoots(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "intake"))
	mustMkdirAll(t, filepath.Join(root, "library"))
	return root
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func itemByPath(t *testing.T, items []Item, path string) Item {
	t.Helper()
	for _, item := range items {
		if item.Path == path {
			return item
		}
	}
	t.Fatalf("item %q not found in %#v", path, items)
	return Item{}
}

func matchesEqual(actual, expected []Match) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}

func exactDuplicateItems(count int) []Item {
	items := make([]Item, count)
	for index := range items {
		path := fmt.Sprintf("book-%06d.cbz", index)
		items[index] = Item{
			Path:           path,
			Kind:           "archive",
			Size:           10,
			SHA256:         "same-content",
			CollisionKey:   collisionKey(path),
			Classification: "new",
		}
	}
	return items
}

func nameConflictItems(count int) []Item {
	items := make([]Item, count)
	for index := range items {
		items[index] = Item{
			Path:           fmt.Sprintf("book-%06d.cbz", index),
			Kind:           "archive",
			Size:           int64(index + 1),
			SHA256:         fmt.Sprintf("content-%06d", index),
			CollisionKey:   "same-target",
			Classification: "new",
		}
	}
	return items
}

func classifiedItemsJSON(t *testing.T, items []Item) []byte {
	t.Helper()
	classify(items, nil)
	body, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func fixtureTreeDigest(t *testing.T, root string) string {
	t.Helper()
	var records []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			records = append(records, "d\x00"+relative)
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(body)
		records = append(records, "f\x00"+relative+"\x00"+hex.EncodeToString(digest[:]))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(records)
	digest := sha256.Sum256([]byte(strings.Join(records, "\x00")))
	return hex.EncodeToString(digest[:])
}
