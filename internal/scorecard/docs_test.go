package scorecard

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDocs_ScorecardMdExists asserts the user-facing reference doc
// (docs/scorecard.md) is present at the repository root (AC 06-01). The repo
// root is located by walking up from the test's working directory to the
// directory containing go.mod, so the test is independent of where it runs.
func TestDocs_ScorecardMdExists(t *testing.T) {
	root := repoRoot(t)
	docPath := filepath.Join(root, "docs", "scorecard.md")
	info, err := os.Stat(docPath)
	if err != nil {
		t.Fatalf("docs/scorecard.md not found at %s: %v", docPath, err)
	}
	if info.IsDir() {
		t.Fatalf("docs/scorecard.md is a directory, expected a file: %s", docPath)
	}
	if info.Size() == 0 {
		t.Fatalf("docs/scorecard.md is empty: %s", docPath)
	}
}

// repoRoot walks up from the current working directory until it finds the
// directory containing go.mod (the module root).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found walking up from working directory")
		}
		dir = parent
	}
}
