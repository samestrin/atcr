package payload

import (
	"strings"
	"testing"
)

func TestScopeFocus(t *testing.T) {
	if got := ScopeFocus(nil); got != "" {
		t.Fatalf("ScopeFocus(nil) = %q, want empty", got)
	}
	if got := ScopeFocus([]string{}); got != "" {
		t.Fatalf("ScopeFocus([]) = %q, want empty", got)
	}

	// Lock the exact rendered string so the wording, leading blank lines, and
	// join punctuation cannot drift unnoticed.
	const wantSingle = "\n\n## Review Focus\nConcentrate this review on the following categories: " +
		"performance. Prioritize findings in these areas. This is a focus hint, not a hard " +
		"limit — still report any genuinely critical issue you find outside them. " +
		"These are focus areas, not CATEGORY values: CATEGORY still comes from the closed vocabulary above."
	single := ScopeFocus([]string{"performance"})
	if single != wantSingle {
		t.Fatalf("ScopeFocus single = %q, want %q", single, wantSingle)
	}
	// The soft-not-hard contract must be present in the output — it steers the
	// model without hard-dropping cross-cutting findings, and deleting it would
	// silently change the constraint semantics.
	if !strings.Contains(single, "not a hard limit") {
		t.Fatalf("ScopeFocus output missing soft-not-hard clause: %q", single)
	}

	multi := ScopeFocus([]string{"performance", "efficiency"})
	if want := "performance, efficiency"; !strings.Contains(multi, want) {
		t.Fatalf("ScopeFocus multi = %q, want join %q", multi, want)
	}

	// Blank entries are skipped, never rendered as an empty category. A trailing
	// blank must produce exactly the single-category render — asserting equality
	// (not just the absence of ", ,") so a stray empty join cannot pass undetected.
	clean := ScopeFocus([]string{"performance", "", "  "})
	if clean != wantSingle {
		t.Fatalf("ScopeFocus with blank entries = %q, want identical to single render %q", clean, wantSingle)
	}
}

// TestScopeFocus_RestatesCategoryBoundary guards the seam between operator scope
// config and the closed CATEGORY vocabulary (epic 35.16.4). The focus block is
// appended AFTER RenderPrompt returns (internal/fanout/review.go), so it is the
// last text the model reads — after both the vocabulary rule and the diff, at
// the position of maximum recency. An operator scope entry that is not a
// vocabulary member would otherwise end the prompt by naming a word the prompt
// earlier declared illegal. The block must therefore say what these words are:
// focus areas, not CATEGORY values.
func TestScopeFocus_RestatesCategoryBoundary(t *testing.T) {
	out := ScopeFocus([]string{"efficiency"})
	if !strings.Contains(out, "not CATEGORY values") {
		t.Errorf("ScopeFocus does not separate focus areas from CATEGORY values: %q", out)
	}
	if !strings.Contains(out, "closed vocabulary") {
		t.Errorf("ScopeFocus does not point CATEGORY back at the closed vocabulary: %q", out)
	}
}
