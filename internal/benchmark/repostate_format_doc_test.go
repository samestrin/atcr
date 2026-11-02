package benchmark

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/samestrin/atcr/reconcile"
)

// formatMD reads the repo-state-v1 format specification the loader, matcher and
// scorer claim to implement. FORMAT.md:3 promises the document is self-contained
// ("no part of it requires reading Go source"), so the checks below read the
// document and nothing else.
func formatMD(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "benchmarks", "repo-state-v1", "FORMAT.md"))
	if err != nil {
		t.Fatalf("reading FORMAT.md: %v", err)
	}
	return string(data)
}

// scoringSection returns the body of FORMAT.md's "## Scoring" section — from its
// heading to the next same-or-higher-level heading — so the scoring contract can
// be checked against the section that is supposed to carry it, not against a
// stray sentence elsewhere in the document.
func scoringSection(t *testing.T, doc string) string {
	t.Helper()
	start := strings.Index(doc, "\n## Scoring")
	if start < 0 {
		t.Fatal("FORMAT.md has no `## Scoring` section: the scorer at score_repostate.go implements metrics the format document never defines")
	}
	rest := doc[start+1:]
	end := strings.Index(rest[len("## Scoring"):], "\n## ")
	if end < 0 {
		return rest
	}
	return rest[:len("## Scoring")+end]
}

// TestRepoStateFormat_DefinesScoring pins the row benchmarks/repo-state-v1/FORMAT.md:3:
// the document promises the scorer "implements this document rather than
// redefining it", so the Scoring section must define — by name — every figure
// ScorePositional emits, the per-case numerator and denominator behind them, what
// a reported finding that matches nothing does to the score, and how the
// standard-v1 submission-envelope metrics relate to a repo-state run.
func TestRepoStateFormat_DefinesScoring(t *testing.T) {
	sec := scoringSection(t, formatMD(t))

	// The three rates the scorer emits, named so a reader can map document to
	// run-result key without reading Go.
	for _, metric := range []string{"recall", "within_diff_recall", "outside_diff_recall"} {
		if !strings.Contains(sec, metric) {
			t.Errorf("Scoring section never names %q, a metric ScorePositional emits", metric)
		}
	}

	// The per-case recall numerator and denominator.
	if !strings.Contains(sec, "numerator") || !strings.Contains(sec, "denominator") {
		t.Error("Scoring section does not state the per-case recall numerator and denominator")
	}

	// What happens to a reported finding that matches no expected finding.
	if !strings.Contains(sec, "matches no expected finding") && !strings.Contains(sec, "matches nothing") {
		t.Error("Scoring section does not say what a reported finding that matches no expected finding does to the score")
	}

	// Whether the standard-v1 submission-envelope metrics apply, named by the
	// headline one.
	if !strings.Contains(sec, "corroboration_rate") {
		t.Error("Scoring section does not state whether standard-v1's submission-envelope metrics (corroboration_rate et al.) apply to a repo-state run")
	}

	// The aggregate across cases: micro-averaged over findings, deliberately not
	// macro-averaged over cases (score_repostate.go's choice).
	if !strings.Contains(sec, "micro-averag") {
		t.Error("Scoring section does not state how per-case outcomes aggregate across cases")
	}
}

// TestRepoStateFormat_ComputableForClaimAbsentCursorFix pins the row's own
// verification: a reader must be able to compute a score for
// claim-absent-cursor-fix from the document alone. That case plants two expected
// findings — one outside_diff, one in-diff — so the Scoring section must carry a
// worked example for it arriving at numbers.
func TestRepoStateFormat_ComputableForClaimAbsentCursorFix(t *testing.T) {
	sec := scoringSection(t, formatMD(t))
	if !strings.Contains(sec, "claim-absent-cursor-fix") {
		t.Fatal("Scoring section carries no worked example for claim-absent-cursor-fix, so its computability from the document alone is unverifiable")
	}
	// A worked example that arrives at no number is prose, not a demonstration.
	if !regexp.MustCompile(`\d+(\.\d+)?\s*/\s*\d+`).MatchString(sec) {
		t.Error("worked example contains no fraction (matched/expected); it does not demonstrate computing a score")
	}
}

// TestRepoStateFormat_ClosedVocabularyInline pins the row
// benchmarks/repo-state-v1/FORMAT.md:103: `category` is a required field whose
// value comes "from atcr's closed category vocabulary", so the document must
// enumerate that vocabulary — a third party greps FORMAT.md for a word, and every
// legal value must be there. This is the check the row's own verification names.
func TestRepoStateFormat_ClosedVocabularyInline(t *testing.T) {
	doc := formatMD(t)
	for _, word := range reconcile.Categories() {
		// Word-boundary match: `state` must not be satisfied by "head state".
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`).MatchString(doc) {
			t.Errorf("FORMAT.md never enumerates vocabulary member %q; an author must read Go source (reconcile/category.go) to learn it is legal", word)
		}
	}
}

// TestRepoStateFormat_CategoryRoleInScoring pins the rest of the :103 row: the
// document must state whether `category` participates in the matching rule (it
// does not — MatchFindings is positional) and whether the standard-v1 equivalence
// families apply (they do — on the category-recall side, corroboration_rate).
func TestRepoStateFormat_CategoryRoleInScoring(t *testing.T) {
	doc := formatMD(t)
	if !strings.Contains(doc, "category does not participate in matching") {
		t.Error("FORMAT.md never states that category does not participate in matching; a loader cannot tell whether a category mismatch breaks, degrades, or is informational to a match")
	}
	if !strings.Contains(doc, "equivalence famil") {
		t.Error("FORMAT.md never states whether the standard-v1 equivalence families apply")
	}
	// The families themselves, so the category-recall side is computable without
	// reading internal/benchmark/equivalence.go. Members within one family are
	// written on one line as "coarse: member, member, ..." so each family's
	// membership is checkable as a unit.
	for _, fam := range []struct{ key, members string }{
		{"correctness", "logic"},
		{"security", "secret, validation, input-validation"},
		{"maintainability", "complexity, duplication, naming, bloat, style"},
		{"performance", ""},
	} {
		idx := strings.Index(doc, fam.key+":")
		if idx < 0 {
			t.Errorf("FORMAT.md does not enumerate the %q equivalence family", fam.key)
			continue
		}
		line := doc[idx:]
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line = line[:nl]
		}
		if fam.members != "" && !strings.Contains(line, fam.members) {
			t.Errorf("FORMAT.md's %q family line does not list members %q", fam.key, fam.members)
		}
	}
}
