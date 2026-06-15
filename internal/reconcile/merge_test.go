package reconcile

import (
	"testing"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
)

// mf builds a per-source finding with the merge-relevant fields.
func TestMerge_OutOfScopeFailClosed(t *testing.T) {
	// Fail-closed: out-of-scope suppresses gating only when EVERY reviewer
	// tagged the finding out-of-scope. A 2-vs-1 out-of-scope majority must not
	// drop the real category — the real category wins and the finding still
	// gates (TD: merge.go modalCategory could silently drop the tag's inverse).
	mixed := []stream.Finding{
		mf("HIGH", "a.go", 1, "p", "f", CategoryOutOfScope, 10, "e", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", CategoryOutOfScope, 10, "e", "kai"),
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e", "mira"),
	}
	assert.Equal(t, "security", Merge(mixed).Category, "real category wins over an out-of-scope majority")

	unanimous := []stream.Finding{
		mf("HIGH", "a.go", 1, "p", "f", CategoryOutOfScope, 10, "e", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", CategoryOutOfScope, 10, "e", "kai"),
	}
	assert.Equal(t, CategoryOutOfScope, Merge(unanimous).Category, "unanimous out-of-scope stays annotated")
}

func mf(sev, file string, line int, problem, fix, category string, est int, evidence, reviewer string) stream.Finding {
	return stream.Finding{
		Severity: sev, File: file, Line: line, Problem: problem, Fix: fix,
		Category: category, EstMinutes: est, Evidence: evidence, Reviewer: reviewer,
	}
}

func TestMerge_ReviewersJoinedDedupedSorted(t *testing.T) {
	m := Merge([]stream.Finding{
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "e1", "kai"),
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "e2", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "e3", "kai"), // dup reviewer
	})
	assert.Equal(t, []string{"greta", "kai"}, m.Reviewers)
	assert.Equal(t, ConfHigh, m.Confidence, "2 distinct reviewers → HIGH")
}

func TestMerge_SingleReviewerIsMedium(t *testing.T) {
	m := Merge([]stream.Finding{mf("LOW", "a.go", 1, "p", "f", "style", 5, "e", "greta")})
	assert.Equal(t, ConfMedium, m.Confidence)
	assert.Empty(t, m.Disagreement)
}

func TestMerge_MaxSeverityWithDisagreement(t *testing.T) {
	// agent-a CRITICAL, agent-b LOW on the same merged finding.
	m := Merge([]stream.Finding{
		mf("CRITICAL", "a.go", 42, "long detailed problem text", "f", "sec", 30, "e", "greta"),
		mf("LOW", "a.go", 42, "short", "fix detail longer", "perf", 60, "e", "kai"),
	})
	assert.Equal(t, "CRITICAL", m.Severity, "max severity wins")
	assert.Equal(t, "LOW vs CRITICAL", m.Disagreement)
}

func TestMerge_LongestProblemAndFix_MaxEst(t *testing.T) {
	m := Merge([]stream.Finding{
		mf("HIGH", "a.go", 1, "short", "f1", "sec", 15, "e", "greta"),
		mf("HIGH", "a.go", 1, "a much longer and more detailed problem", "longer fix text", "sec", 45, "e", "kai"),
	})
	assert.Equal(t, "a much longer and more detailed problem", m.Problem)
	assert.Equal(t, "longer fix text", m.Fix)
	assert.Equal(t, 45, m.EstMinutes, "max estimate (most pessimistic)")
}

func TestMerge_ModalCategoryAlphabeticTiebreak(t *testing.T) {
	// security x2, performance x1 → security (modal).
	m := Merge([]stream.Finding{
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e", "a"),
		mf("HIGH", "a.go", 1, "p", "f", "performance", 10, "e", "b"),
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e", "c"),
	})
	assert.Equal(t, "security", m.Category)

	// tie: correctness vs security, 1 each → alphabetical "correctness".
	tie := Merge([]stream.Finding{
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e", "a"),
		mf("HIGH", "a.go", 1, "p", "f", "correctness", 10, "e", "b"),
	})
	assert.Equal(t, "correctness", tie.Category)
}

func TestMerge_EmptyGroupDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() { _ = Merge(nil) })
}

func TestMerge_EmptyCategoryDoesNotHijackTie(t *testing.T) {
	// One empty-category finding tied with a real category must not win.
	m := Merge([]stream.Finding{
		mf("HIGH", "a.go", 1, "p", "f", "", 10, "e", "a"),
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e", "b"),
	})
	assert.Equal(t, "security", m.Category)
}

func TestMerge_EvidenceReviewerPrefixedWhenMultiple(t *testing.T) {
	m := Merge([]stream.Finding{
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "saw X", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "saw Y", "kai"),
	})
	assert.Contains(t, m.Evidence, "[greta] saw X")
	assert.Contains(t, m.Evidence, "[kai] saw Y")
}

func TestMerge_ModalCategoryCanonicalizesCase(t *testing.T) {
	// Non-canonical "Out-Of-Scope" must be normalized to the canonical
	// "out-of-scope" so the gate, the summary out_of_scope count, and the
	// report out-of-scope section all agree on what counts as out-of-scope.
	nonCanonical := []stream.Finding{
		mf("HIGH", "a.go", 1, "p", "f", "Out-Of-Scope", 10, "e", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", "Out-Of-Scope", 10, "e", "kai"),
	}
	m := Merge(nonCanonical)
	assert.Equal(t, CategoryOutOfScope, m.Category,
		"non-canonical 'Out-Of-Scope' normalized to canonical 'out-of-scope'")

	// Mixed casing across reviewers: all non-canonical out-of-scope variants
	// must collapse to the canonical form so unanimous detection works.
	mixedCase := []stream.Finding{
		mf("HIGH", "a.go", 1, "p", "f", "OUT-OF-SCOPE", 10, "e", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", "out-of-scope", 10, "e", "kai"),
	}
	assert.Equal(t, CategoryOutOfScope, Merge(mixedCase).Category,
		"all-caps and canonical variants collapse to the same canonical form")
}

func TestMerge_ModalCategoryCanonicalizesAllCategories(t *testing.T) {
	// All category values must emerge canonicalized (lower+trim), not just
	// out-of-scope — otherwise the same casing mismatch breaks the summary
	// counts and report sections for any category.
	m := Merge([]stream.Finding{
		mf("HIGH", "a.go", 1, "p", "f", "Security", 10, "e", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", "SECURITY", 10, "e", "kai"),
	})
	assert.Equal(t, "security", m.Category,
		"non-canonical category casing normalized to canonical lowercase")
}
