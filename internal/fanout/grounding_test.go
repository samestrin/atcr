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

func TestGroundFindings_FailsOpenUnknownFile(t *testing.T) {
	in := []stream.Finding{{File: "other.go", Line: 5, Category: "correctness"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("fail-open: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_NilDataKeepsAll(t *testing.T) {
	in := []stream.Finding{{File: "auth.go", Line: 999, Category: "correctness"}}
	out, dropped := groundFindings(in, nil)
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("nil data: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}
