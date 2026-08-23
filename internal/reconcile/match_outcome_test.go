package reconcile

import "testing"

// stampJustifications warns "review.md narratives exist but matched zero findings;
// possible format drift" when nothing was stamped. That sentence is only true for ONE
// of the two ways matchNarrative reports no match, and it names the wrong one for the
// other: a review.md whose findings all sit inside a terminated fence anchors every
// finding perfectly and still stamps nothing, because extractSection suppresses a
// section that is pure quoted example. Sending an operator to hunt for parser drift
// there costs them the search and finds nothing.
//
// The discrimination has to come from matchNarrative — the caller cannot re-derive it.
// `len(index[file]) > 0` is not the same question: refs below minAnchorTier are pruned
// after the index lookup, so a bare file mention that never became a candidate would
// be reported as an elided quote.
func TestMatchNarrative_ReportsWhyItFoundNothing(t *testing.T) {
	t.Run("all candidate sections elided", func(t *testing.T) {
		narratives := []reviewNarrative{{
			relPath: "sources/pool/alice/review.md",
			leaf:    "alice",
			lines: []string{
				"## Review",
				"",
				"```",
				"internal/x.go:42 HIGH the token is never rotated",
				"```",
			},
		}}

		_, out := matchNarrative(narratives, buildAnchorIndex(narratives), "internal/x.go", 42, []string{"alice"})

		if out.ok() {
			t.Fatal("expected no match: the only anchor sits inside a terminated fence")
		}
		if out != matchAllElided {
			t.Errorf("outcome = %v, want matchAllElided — the anchor DID match, so reporting\n"+
				"this as a missing anchor makes the caller blame format drift for a quoted example", out)
		}
	})

	t.Run("no anchor at all", func(t *testing.T) {
		narratives := []reviewNarrative{{
			relPath: "sources/pool/alice/review.md",
			leaf:    "alice",
			lines: []string{
				"## Review",
				"",
				"internal/other.go:7 LOW unrelated",
			},
		}}

		_, out := matchNarrative(narratives, buildAnchorIndex(narratives), "internal/x.go", 42, []string{"alice"})

		if out.ok() {
			t.Fatal("expected no match: nothing references internal/x.go")
		}
		if out != matchNoAnchor {
			t.Errorf("outcome = %v, want matchNoAnchor", out)
		}
	})

	// An indexed reference that never RANKS is the no-anchor case, not the elided one,
	// and this is the distinction the caller provably cannot re-derive from the index:
	// len(index[file]) is > 0 here — the file is referenced, and with a line number, so
	// indexLineFiles records it — yet the line is far enough from the finding's that
	// anchorTier scores it below minAnchorTier and it never becomes a candidate. No
	// section was ever extracted, so no quote was involved.
	t.Run("reference indexed but ranks below the tier floor", func(t *testing.T) {
		narratives := []reviewNarrative{{
			relPath: "sources/pool/alice/review.md",
			leaf:    "alice",
			lines: []string{
				"## Review",
				"",
				"Unrelated: internal/x.go:900 has a different problem entirely.",
			},
		}}
		index := buildAnchorIndex(narratives)

		if len(index["internal/x.go"]) == 0 {
			t.Fatal("fixture no longer indexes the reference; it cannot exercise the pruning path")
		}

		_, out := matchNarrative(narratives, index, "internal/x.go", 42, []string{"alice"})

		if out != matchNoAnchor {
			t.Errorf("outcome = %v, want matchNoAnchor — the only reference ranked below\n"+
				"minAnchorTier and never became a candidate, so no section was ever elided", out)
		}
	})

	t.Run("prose anchor matches", func(t *testing.T) {
		narratives := []reviewNarrative{{
			relPath: "sources/pool/alice/review.md",
			leaf:    "alice",
			lines: []string{
				"## Review",
				"",
				"The handler at internal/x.go:42 skips verification.",
			},
		}}

		m, out := matchNarrative(narratives, buildAnchorIndex(narratives), "internal/x.go", 42, []string{"alice"})

		if !out.ok() || out != matchFound {
			t.Fatalf("outcome = %v, want matchFound", out)
		}
		if m.text == "" {
			t.Error("matched outcome carried an empty excerpt")
		}
	})
}
