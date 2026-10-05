package reconcile

import (
	"strings"
	"testing"
)

// docs/benchmark.md's `unshown` paragraph (:314) says "re-running will not help it —
// the case ran and the rest of the panel scored it, so what needs investigating is
// that one provider." Both halves are false for `unmeasured_salvaged_ok`: the
// reviewer WAS shown the case and replied ok, and the remedy is the agent's thinking
// declaration or a different model, not the provider. The :305 paragraph WAS updated
// to document the new reason, so the published doc contradicted itself two paragraphs
// apart.
//
// BIDIRECTIONAL: the doc needle pins the corrected sentence, the code needle pins the
// `unmeasured_ok` label the coverage diagnostic emits for that class (it used to fold
// into `unshown`). It lives in internal/reconcile/ per the repo's convention for
// doc-vs-code drift tests.
func TestBenchmarkDoc_UnshownParagraphSeparatesTheUnmeasuredOKReason(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")
	code := readRepoFile(t, "../../cli/benchmark_coverage.go")

	// The :314 paragraph must name the unmeasured-ok class and its distinct remedy.
	for _, needle := range []string{
		"unmeasured_ok",
		"thinking",
	} {
		if !strings.Contains(doc, needle) {
			t.Errorf("docs/benchmark.md's unshown paragraph must name the unmeasured-ok class and its remedy (%q): "+
				"re-running CAN differ for it, and the provider is not the thing to investigate", needle)
		}
	}

	// Behaviour-bearing code arm: the diagnostic must emit its own label for the
	// class, not fold it into `unshown`.
	if !strings.Contains(code, `"unmeasured_ok "`) {
		t.Errorf("describeMissing must label SlotFailureUnmeasuredOK `unmeasured_ok`, not `unshown` — " +
			"`unshown` asserts the reviewer was never shown a case it did run")
	}
	if !strings.Contains(code, "benchmark.SlotFailureUnmeasuredOK") {
		t.Errorf("the unmeasured-ok class must be keyed on the vocabulary constant, not a string literal")
	}
}
