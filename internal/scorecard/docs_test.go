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
	if !strings.Contains(doc, "Superseded but retained") {
		t.Errorf("raised_includes_unresolved row must be described as superseded-but-retained now that raised_denominator carries the era")
	}
}

// TestDocs_PublicEnvelopeRaisedDenominator pins the PUBLIC-envelope reference
// against PublicRecord.RaisedDenominator (export.go): the field is deliberately
// NOT omitempty, so every exported reviewer row emits the key, and the docs must
// show it — in the reviewer field table, in the sample envelope, in the privacy
// allowlist, and in the benchmark submission sample (a benchmark row stamps
// RaisedDenominatorBenchmarkSuite, 100).
func TestDocs_PublicEnvelopeRaisedDenominator(t *testing.T) {
	sc, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "scorecard.md"))
	if err != nil {
		t.Fatalf("read docs/scorecard.md: %v", err)
	}
	doc := string(sc)
	for _, want := range []string{
		"| `raised_denominator` | int | always |", // reviewer field table row
		`"raised_denominator": 3`,                 // production sample envelope row
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/scorecard.md public-envelope reference is missing %s (PublicRecord.RaisedDenominator is not omitempty — every row emits it)", want)
		}
	}
	// The privacy allowlist bullet must name the key, or a reader concludes the
	// emitted key is off-allowlist.
	allowlistIdx := strings.Index(doc, "Preserved (allowlist):")
	if allowlistIdx < 0 {
		t.Fatalf("docs/scorecard.md privacy allowlist section not found")
	}
	strippedIdx := strings.Index(doc[allowlistIdx:], "Stripped / never exported:")
	if strippedIdx < 0 || !strings.Contains(doc[allowlistIdx:allowlistIdx+strippedIdx], "`raised_denominator`") {
		t.Errorf("privacy allowlist must list raised_denominator as preserved (a schema discriminator carrying no run content)")
	}

	bm, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "benchmark.md"))
	if err != nil {
		t.Fatalf("read docs/benchmark.md: %v", err)
	}
	if !strings.Contains(string(bm), `"raised_denominator": 100`) {
		t.Errorf("docs/benchmark.md sample submission is missing raised_denominator: 100 (benchmark rows stamp RaisedDenominatorBenchmarkSuite)")
	}
}
