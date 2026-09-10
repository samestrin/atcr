package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/stream"
)

// Context-aware pre-fetching (Epic 35.16.8) adds files the patch NEVER TOUCHED
// to the grounding map so a finding about a retrieved consumer can survive the
// Epic 14.1 gate. That widening has an exact stated boundary: only the spans
// actually shown are groundable.
//
// These tests pin that boundary, because the permissive arms of isGrounded make
// it very easy to exceed by accident — a file present in ChangedLines normally
// keeps ANY finding that cites no line, and a file with no range data fails
// open. Either arm applied to a merely-referenced file would let a fabricated
// finding against untouched code clear the anti-hallucination gate, which is a
// strictly wider hole than the one pre-fetching set out to open.

func prefetchGroundingFixture() payload.ChangedLines {
	return payload.ChangedLines{
		// A genuinely changed file: the ordinary Epic 14.1 rules apply.
		"store.go": {
			Ranges:      []payload.LineRange{{Start: 3, End: 5}},
			ChangedText: []string{"return \"\", nil"},
		},
		// Retrieved by reference only. The patch did not touch it.
		"consumer.go": {
			Ranges:       []payload.LineRange{{Start: 3, End: 10}},
			PrefetchOnly: true,
		},
	}
}

func TestGroundFindings_PrefetchedFileKeepsAFindingInsideTheShownSpan(t *testing.T) {
	// The epic's whole purpose: a defect in a file the diff never touched must be
	// reportable, so long as it cites code the reviewer was actually shown.
	in := []stream.Finding{{File: "consumer.go", Line: 5, Category: "correctness"}}
	out, dropped := groundFindings(in, prefetchGroundingFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("in-span prefetched finding: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_PrefetchedFileDropsFileLevelFinding(t *testing.T) {
	// A finding citing NO line is kept on a changed file ("the file itself is in
	// scope"). That justification does not exist for a file the patch never
	// touched, so this must be dropped — it is the fabricated-file class the gate
	// exists to catch, reached through a retrieved path.
	in := []stream.Finding{{File: "consumer.go", Line: 0, Category: "correctness"}}
	out, dropped := groundFindings(in, prefetchGroundingFixture())
	if len(out) != 0 || dropped != 1 {
		t.Fatalf("file-level prefetched finding: kept=%d dropped=%d, want kept=0 dropped=1", len(out), dropped)
	}
}

func TestGroundFindings_PrefetchedFileDropsFindingOutsideTheShownSpan(t *testing.T) {
	// Only what was SHOWN is groundable. The rest of a retrieved file is exactly
	// as unseen as any other untouched code.
	in := []stream.Finding{{File: "consumer.go", Line: 500, Category: "correctness"}}
	out, dropped := groundFindings(in, prefetchGroundingFixture())
	if len(out) != 0 || dropped != 1 {
		t.Fatalf("out-of-span prefetched finding: kept=%d dropped=%d, want kept=0 dropped=1", len(out), dropped)
	}
}

func TestGroundFindings_PrefetchedFileDoesNotFailOpenWithoutRanges(t *testing.T) {
	// A changed file with no line data fails OPEN (binary/mode-only: nothing to
	// check against). A prefetched entry with no spans means nothing was shown at
	// all, so failing open would ground every finding against an untouched file.
	changed := payload.ChangedLines{
		"consumer.go": {PrefetchOnly: true},
	}
	in := []stream.Finding{
		{File: "consumer.go", Line: 0, Category: "correctness"},
		{File: "consumer.go", Line: 7, Category: "correctness"},
	}
	out, dropped := groundFindings(in, changed)
	if len(out) != 0 || dropped != 2 {
		t.Fatalf("span-less prefetched entry: kept=%d dropped=%d, want kept=0 dropped=2", len(out), dropped)
	}
}

func TestGroundFindings_PrefetchedFileIsNotRescuedByEvidence(t *testing.T) {
	// The evidence arm matches a finding's quoted text against the CHANGED lines
	// of a patched file. A prefetched entry carries no changed text, and must not
	// acquire a second route to grounding.
	changed := payload.ChangedLines{
		"consumer.go": {
			Ranges:       []payload.LineRange{{Start: 3, End: 10}},
			ChangedText:  []string{"data, err := ReadStore(path)"},
			PrefetchOnly: true,
		},
	}
	in := []stream.Finding{{File: "consumer.go", Line: 900, Category: "correctness", Evidence: "data, err := ReadStore(path)"}}
	out, dropped := groundFindings(in, changed)
	if len(out) != 0 || dropped != 1 {
		t.Fatalf("evidence-rescued prefetched finding: kept=%d dropped=%d, want kept=0 dropped=1", len(out), dropped)
	}
}

func TestGroundFindings_GenuinelyChangedFileKeepsItsPermissiveArms(t *testing.T) {
	// The narrowing must apply ONLY to prefetched entries. A real changed file
	// keeps the Epic 14.1 behavior, including the file-level arm — otherwise this
	// fix would silently tighten the gate for every ordinary review.
	in := []stream.Finding{{File: "store.go", Line: 0, Category: "correctness"}}
	out, dropped := groundFindings(in, prefetchGroundingFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("file-level finding on a changed file: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}
