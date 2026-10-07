package reconcile

import (
	"strings"
	"testing"
)

// docs/cross-examination.md's Per-seat-budgets bullet defines `seat_suppressed` as
// "every asked seat RAN CLEAN and said something that was entirely inline `<think>`
// reasoning, which the strip removed" — but recordTurnCause (internal/debate/protocol.go)
// now records Suppressed WITHOUT consulting status, so a seat that did NOT run clean
// (a budget-tripped seat whose forced final answer was all think markup) also reports
// seat_suppressed, because debateOne checks Suppressed before Halted. The `seat_halted`
// definition ("every seat that was asked halted") is no longer sufficient either.
//
// BIDIRECTIONAL: the doc needle pins the corrected definitions, the code needle pins
// the precedence that makes them true (Suppressed outranks Halted in debateOne). It
// lives in internal/reconcile/ per the repo's convention for doc-vs-code drift tests.
func TestCrossExaminationDoc_SeatTokensMatchTheRecordedPrecedence(t *testing.T) {
	doc := readRepoFile(t, "../../docs/cross-examination.md")
	code := readRepoFile(t, "../../internal/debate/debate.go")

	// The doc must state that suppressed outranks halted, and that a seat can be both.
	for _, needle := range []string{
		"suppressed outranks halted",
		"seat can be both",
	} {
		if !strings.Contains(doc, needle) {
			t.Errorf("docs/cross-examination.md must state the seat-token precedence (%q): "+
				"a budget-tripped seat whose forced final answer was all think markup is BOTH, and suppressed wins", needle)
		}
	}

	// The stale "ran clean" definition of seat_suppressed must be gone.
	if strings.Contains(doc, "every asked seat ran clean and said something that was entirely inline") {
		t.Errorf("the `ran clean` definition of seat_suppressed is false after the precedence flip — " +
			"a seat that did not run clean still reports seat_suppressed")
	}

	// Behaviour-bearing code arm: the precedence that makes the doc true.
	if !strings.Contains(code, "case allSeatsIn(rec.Suppressed, blamed):") {
		t.Errorf("debateOne must check Suppressed before Halted — the doc's stated precedence is implemented here")
	}
}
