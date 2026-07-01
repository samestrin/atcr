package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/stream"
)

func changedFixture() payload.ChangedLines {
	return payload.ChangedLines{
		"auth.go": {
			Ranges:      []payload.LineRange{{Start: 40, End: 45}},
			ChangedText: []string{"token := parseToken(r)", "return validate(token)"},
		},
	}
}

func TestGroundFindings_KeepsInRange(t *testing.T) {
	in := []stream.Finding{{File: "auth.go", Line: 42, Category: "security", Evidence: "x"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("in-range: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_KeepsWithinTolerance(t *testing.T) {
	// line 47 is 2 past range end 45 -> within ±3
	in := []stream.Finding{{File: "auth.go", Line: 47, Category: "correctness"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("tolerance: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_DropsUngrounded(t *testing.T) {
	in := []stream.Finding{{File: "auth.go", Line: 200, Category: "correctness", Evidence: "totally unrelated prose"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 0 || dropped != 1 {
		t.Fatalf("ungrounded: kept=%d dropped=%d, want kept=0 dropped=1", len(out), dropped)
	}
}

func TestGroundFindings_KeepsViaEvidence(t *testing.T) {
	// wrong line, but EVIDENCE quotes a changed line -> fuzzy keep
	in := []stream.Finding{{File: "auth.go", Line: 999, Category: "security", Evidence: "return validate(token)"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("evidence: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_ExemptsOutOfScope(t *testing.T) {
	in := []stream.Finding{{File: "auth.go", Line: 999, Category: "out-of-scope", Evidence: "pre-existing"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("out-of-scope: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_DropsUnknownFile(t *testing.T) {
	// A file the patch never touched is the fabricated-file hallucination class:
	// it must be dropped, not kept (AC2, clarification Q1 hard-drop).
	in := []stream.Finding{{File: "ghost.go", Line: 5, Category: "correctness", Evidence: "invented"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 0 || dropped != 1 {
		t.Fatalf("unknown file: kept=%d dropped=%d, want kept=0 dropped=1", len(out), dropped)
	}
}

func TestGroundFindings_KeepsFileLevelOnChangedFile(t *testing.T) {
	// A file-level finding (line 0) on a file the patch changed is kept.
	in := []stream.Finding{{File: "auth.go", Line: 0, Category: "correctness"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("file-level: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_KeepsBinaryFile(t *testing.T) {
	// A present-but-empty FileChange (binary/mode-only) has no lines to check, so
	// the finding cannot be proven ungrounded — fail open.
	changed := payload.ChangedLines{"logo.png": {}}
	in := []stream.Finding{{File: "logo.png", Line: 3, Category: "correctness"}}
	out, dropped := groundFindings(in, changed)
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("binary: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_NormalizesPathPrefix(t *testing.T) {
	// A model citing the diff-side "b/auth.go" must match the bare head-path key,
	// so path-form drift does not cause a false drop under the unknown-file rule.
	in := []stream.Finding{{File: "b/auth.go", Line: 42, Category: "correctness"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("path normalize: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_OutOfScopeExemptOnUnknownFile(t *testing.T) {
	// out-of-scope stays exempt even for a file not in the patch (clarification Q2:
	// out-of-scope is annotated-not-promoted downstream, so it is never dropped here).
	in := []stream.Finding{{File: "ghost.go", Line: 5, Category: "out-of-scope", Evidence: "pre-existing"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("out-of-scope unknown file: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_NilDataKeepsAll(t *testing.T) {
	in := []stream.Finding{{File: "auth.go", Line: 999, Category: "correctness"}}
	out, dropped := groundFindings(in, nil)
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("nil data: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}
