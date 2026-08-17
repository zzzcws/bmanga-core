package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zzzcws/bmanga-core/internal/importplan"
)

func TestRunRequiresExplicitSelections(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(context.Background(), nil, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "explicit root") {
		t.Fatalf("error = %v, want explicit root error", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunEmitsPlanOnlyToStdout(t *testing.T) {
	root := t.TempDir()
	intake := filepath.Join(root, "incoming")
	library := filepath.Join(root, "catalog")
	if err := os.MkdirAll(intake, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(intake, "book.cbz"), []byte("book"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(context.Background(), []string{
		"--root", root,
		"--intake", "incoming",
		"--library", "catalog",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var plan importplan.Plan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("decode stdout: %v; body = %s", err, stdout.String())
	}
	if plan.Mode != "dry-run-read-only" || plan.New != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	if len(plan.Items) != 1 || plan.Items[0].MatchCount != 0 || plan.Items[0].MatchesTruncated {
		t.Fatalf("item match summary = %#v", plan.Items)
	}
	if !strings.Contains(stdout.String(), `"matchCount": 0`) || !strings.Contains(stdout.String(), `"matchesTruncated": false`) {
		t.Fatalf("stdout does not contain the explicit match summary fields: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), filepath.ToSlash(root)) || strings.Contains(stdout.String(), root) {
		t.Fatalf("stdout contains the absolute security root: %s", stdout.String())
	}
}
