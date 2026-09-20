package scorecard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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

// TestDocs_ScorecardMdSchemaVersionMatchesTheConstant is the pin the v1->v2 bump
// needed and did not have. docs/scorecard.md states the current version in four
// places, and a bump that updates the constant while leaving the doc claiming
// "Currently 1" publishes a reference that is simply false — the exact drift the
// sibling pins in this file exist to stop.
func TestDocs_ScorecardMdSchemaVersionMatchesTheConstant(t *testing.T) {
	doc := string(readDoc(t, "scorecard.md"))

	for _, want := range []string{
		fmt.Sprintf("## Record Schema (v%d)", SchemaVersion),
		fmt.Sprintf(`"schema_version": %d,`, SchemaVersion),
		fmt.Sprintf("| `schema_version` | int | always | Record schema version. Currently `%d`. |", SchemaVersion),
		fmt.Sprintf("- `schema_version` (`%d`) is stamped on every **stored** record", SchemaVersion),
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/scorecard.md has drifted from SchemaVersion = %d; missing:\n%s",
				SchemaVersion, want)
		}
	}
}

// TestDocs_ScorecardMdDocumentsOutcomeAndEligibility pins the two fields the v2
// bump carries and the trust rule that reads the first of them. The eligibility
// rule is the one that changes what an operator SEES — a pre-v2 store renders an
// all-n/a `personas list --scores` table — so an undocumented version of it is a
// support problem, not a cosmetic gap.
func TestDocs_ScorecardMdDocumentsOutcomeAndEligibility(t *testing.T) {
	doc := string(readDoc(t, "scorecard.md"))

	for _, want := range []string{
		"| `outcome` | string | conditional |",
		"| `categories_raised` | array of string | conditional |",
		"- **Outcome eligibility.**",
		"every reviewer in it is excluded and the",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/scorecard.md is missing required passage:\n%s", want)
		}
	}

	// Pin the doc's OWN eligibility sentence against the code allowlist, not the
	// whole file. A whole-file substring search cannot fail: all nine vocabulary
	// values already appear backticked somewhere in this document, so adding
	// `truncated` to eligibleOutcomeRuns would leave the check green while the
	// doc went on stating that truncated is excluded.
	//
	// The sentence lists the counted set and then the excluded set, so asserting
	// each value appears on the correct SIDE of "are excluded" makes the two
	// lists a real complement of each other.
	countedStart := strings.Index(doc, "- **Counted:**")
	excludedStart := strings.Index(doc, "- **Excluded:**")
	if countedStart < 0 || excludedStart <= countedStart {
		t.Fatalf("docs/scorecard.md must render the eligibility split as a Counted: bullet followed by an Excluded: bullet; the pin below cannot work otherwise")
	}
	// Bound the excluded bullet explicitly. strings.Index returns -1 when the
	// bullet is the final block, and slicing on that panics with "slice bounds
	// out of range" — a routine doc edit would crash this test instead of
	// failing it with the diagnostic it exists to print.
	excludedEnd := strings.Index(doc[excludedStart:], "\n\n")
	if excludedEnd < 0 {
		t.Fatalf("docs/scorecard.md: the Excluded: bullet must be followed by a blank line so its extent is unambiguous")
	}
	counted := doc[countedStart:excludedStart]
	excluded := doc[excludedStart : excludedStart+excludedEnd]

	// Each value must be named on the side the CODE actually puts it on, so
	// moving one across the allowlist without moving it in the doc fails here.
	// A whole-file substring search cannot do this: all nine values already
	// appear backticked somewhere in this document.
	for _, o := range allTestOutcomes {
		if o == "" {
			continue // absent/unknown is prose, not a backticked token
		}
		admitted := len(eligibleOutcomeRuns([]Record{{RecordType: RecordTypeReviewer, Outcome: o}})) > 0
		side, other := excluded, counted
		if admitted {
			side, other = counted, excluded
		}
		if !strings.Contains(side, "`"+o+"`") {
			t.Errorf("code treats %q as admitted=%v but docs/scorecard.md does not name it on that side of the eligibility split", o, admitted)
		}
		if strings.Contains(other, "`"+o+"`") {
			t.Errorf("docs/scorecard.md names %q on BOTH sides of the eligibility split", o)
		}
	}
}

// readDoc reads a file from docs/ relative to the repo root.
func readDoc(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", name))
	if err != nil {
		t.Fatalf("read docs/%s: %v", name, err)
	}
	return data
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

// TestDocs_ScorecardMdDocumentsDocShieldedColumn keeps the two rendered column
// lists in docs/scorecard.md honest about the conditional DOC-SHIELDED column.
//
// The column exists because Record.FindingsRaised stopped counting doc-shielded
// routings, so a reader of either table cannot otherwise tell a clean 100% from
// one with shielded findings behind it. A doc that omits the column teaches the
// old, now-ambiguous reading.
func TestDocs_ScorecardMdDocumentsDocShieldedColumn(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "scorecard.md"))
	if err != nil {
		t.Fatalf("read docs/scorecard.md: %v", err)
	}
	doc := string(data)

	assert.Contains(t, doc, "RAISED  CORROBORATED  SOLO  CORR%  COST  LATENCY",
		"the `atcr scorecard` base column list must stay documented verbatim")
	assert.Contains(t, doc, "RUNS  RAISED  CORROBORATED  CORR%  COST  COST/CORR  LATENCY",
		"the `atcr leaderboard` base column list must stay documented verbatim")
	assert.GreaterOrEqual(t, strings.Count(doc, "DOC-SHIELDED"), 2,
		"both column lists must document the conditional DOC-SHIELDED column, not just one")
}
