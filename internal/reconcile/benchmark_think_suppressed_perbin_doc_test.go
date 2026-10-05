package reconcile

import (
	"strings"
	"testing"
)

// docs/benchmark.md's `slot_failures[]` paragraph spells the per-bin rule for the
// SALVAGED half of `unmeasured_salvaged_ok` in full ("an unchunked salvage, or a
// `salvaged_chunks` set covering every bin") but gives the think-suppressed half only
// "(the strip removed the entire reply)". WholePersonaThinkSuppressed made that half
// per-bin too — UnparseableChunks over ChunkCount — so the published doc under-states
// a rule that now governs both halves, and a reader cannot tell from it that a chunked
// persona with one suppressed bin of eight is scored.
//
// BIDIRECTIONAL, like its siblings: the doc needle pins the sentence stating the rule,
// and the code needle pins the predicate arm that makes it true. It lives in
// internal/reconcile/ per the repo's convention for doc-vs-code drift tests (no Go
// package lives under docs/, and the published reconcile/ module must not assume this
// repo's file layout — see justification_record_boundary_test.go).
func TestBenchmarkDoc_ThinkSuppressedPerBinRuleIsStated(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")
	code := readRepoFile(t, "../../internal/fanout/revieweroutcome.go")

	// The doc must state the think-suppressed per-bin rule, not just the salvaged one.
	// Matched on the operative phrases rather than a bare "partially": the sentence
	// must name BOTH halves and the every-bin denominator for the think half.
	for _, needle := range []string{
		"every bin",
		"unparseable_chunks covering chunk_count",
	} {
		if !strings.Contains(doc, needle) {
			t.Errorf("docs/benchmark.md slot_failures paragraph must state the think-suppressed per-bin rule (%q): "+
				"a chunked persona with one suppressed bin of eight is scored, and only a strip that ate EVERY bin is unmeasured", needle)
		}
	}
	if !strings.Contains(doc, "partially-suppressed") {
		t.Errorf("docs/benchmark.md must name the partially-suppressed chunked slot explicitly, " +
			"the way it names the partially salvaged one")
	}

	// Behaviour-bearing code arm: WholePersonaThinkSuppressed compares the numerator
	// against the denominator. Deleting that comparison would make one suppressed bin
	// of eight count as a whole-persona loss, silently contradicting the doc again.
	if !strings.Contains(code, "a.UnparseableChunks >= a.ChunkCount") {
		t.Errorf("WholePersonaThinkSuppressed must still compare UnparseableChunks against ChunkCount — " +
			"the per-bin rule the doc states is implemented here, and the doc must not promise a rule the code dropped")
	}
}
