package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/payload"
)

// Both range predicates open with the same guard — `if line <= 0 { return false }`
// — and NEITHER is reachable from its production caller. isGrounded short-circuits
// the exact variant (`return f.Line > 0 && lineInExactRanges(...)`, grounding.go:85)
// and returns for a file-level finding before it reaches the tolerant one
// (`if f.Line <= 0 { return true }`, grounding.go:91). Measured: both guard bodies
// sit in coverage blocks with count 0.
//
// They are pinned here anyway, for two reasons.
//
// First, the contract is DOCUMENTED on both functions ("A non-positive line is
// never in range"), so it is a promise the predicate makes on its own, to any
// caller, not a detail of today's single call site. A future caller that drops
// the short-circuit — reasonably, since the predicate already handles the case —
// must not silently change the answer.
//
// Second, for lineInRanges the guard is genuinely LOAD-BEARING, not decorative.
// Its comparison is tolerance-expanded, so against a range near the start of a
// file (1..2, tolerance 3) the guard-less arithmetic for line 0 reads
// `0 >= 1-3` and `0 <= 2+3` — both true — and a non-positive line would be
// reported as in range. The fixture below uses exactly that range so the
// assertion fails if the guard is removed, rather than passing because the
// comparison happened to reject the value anyway.
//
// For lineInExactRanges the same fixture is belt-and-braces: ranges are 1-based,
// so a non-positive line fails `line >= r.Start` on its own. That asymmetry is
// stated rather than papered over — the test still pins the documented contract,
// it just is not the thing standing between the code and a wrong answer there.
func TestRangePredicates_RejectNonPositiveLines(t *testing.T) {
	// Deliberately near the start of the file: see the load-bearing note above.
	ranges := []payload.LineRange{{Start: 1, End: 2}}

	predicates := []struct {
		name string
		call func(int, []payload.LineRange) bool
	}{
		{"lineInRanges", lineInRanges},
		{"lineInExactRanges", lineInExactRanges},
	}

	for _, p := range predicates {
		t.Run(p.name, func(t *testing.T) {
			for _, line := range []int{0, -1, -1000} {
				if p.call(line, ranges) {
					t.Fatalf("%s(%d, [1..2]) = true, want false: a non-positive line is never in range", p.name, line)
				}
			}

			// Not one-sided. Without this, a predicate that answered false
			// unconditionally would satisfy every assertion above.
			if !p.call(2, ranges) {
				t.Fatalf("%s(2, [1..2]) = false, want true: line 2 is the range end", p.name)
			}
		})
	}
}
