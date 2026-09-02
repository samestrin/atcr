package scorecard

import (
	"os"
	"path/filepath"
	"strings"
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

// TestDocs_ScorecardMdRaisedDenominator pins the record-schema table against the
// era discriminator this package writes: Record.RaisedDenominator supersedes the
// RaisedIncludesUnresolved bool, so the doc must carry a `raised_denominator`
// row and describe `raised_includes_unresolved` as superseded-but-retained —
// a reader of the table must not conclude the bool still carries the era alone.
func TestDocs_ScorecardMdRaisedDenominator(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "scorecard.md"))
	if err != nil {
		t.Fatalf("read docs/scorecard.md: %v", err)
	}
	doc := string(data)
	if !strings.Contains(doc, "| `raised_denominator` | int | conditional |") {
		t.Errorf("record-field table is missing the raised_denominator row that Record.RaisedDenominator (scorecard.go) writes")
	}
	if !strings.Contains(doc, "superseded") {
		t.Errorf("raised_includes_unresolved row must be described as superseded-but-retained now that raised_denominator carries the era")
	}
}
