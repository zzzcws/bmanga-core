//go:build linux

package importplan

import (
	"context"
	"errors"
	"path/filepath"
	"syscall"
	"testing"
)

func TestBuildRejectsSpecialFiles(t *testing.T) {
	root := fixtureRoots(t)
	pipePath := filepath.Join(root, "intake", "special.pipe")
	if err := syscall.Mkfifo(pipePath, 0o600); err != nil {
		t.Skipf("FIFO creation is unavailable in this test environment: %v", err)
	}
	_, err := Build(context.Background(), Options{Root: root, Intake: "intake", Library: "library"})
	if !errors.Is(err, ErrPathBoundary) {
		t.Fatalf("error = %v, want ErrPathBoundary", err)
	}
}
