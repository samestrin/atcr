package reconcile

import "testing"

// TestNCD_IdenticalNearZero: a string against itself compresses to nearly the
// size of one copy, so NCD ≈ 0.
func TestNCD_IdenticalNearZero(t *testing.T) {
	x := []byte("the session token never expires and is never rotated on the server")
	d := ncd(x, x)
	if d < 0 || d > 0.2 {
		t.Errorf("ncd(x,x) = %v, want near 0 (<=0.2)", d)
	}
}

// TestNCD_UnrelatedHigh: two unrelated texts share little structure, so NCD is
// high (closer to 1).
func TestNCD_UnrelatedHigh(t *testing.T) {
	a := []byte("buffer overflow when parsing the oversized image header field")
	b := []byte("the quarterly revenue projection spreadsheet omits the tax line")
	d := ncd(a, b)
	if d < 0.5 {
		t.Errorf("ncd(unrelated) = %v, want high (>=0.5)", d)
	}
}

// TestNCD_DuplicateBeatsUnrelated: the core premise — a same-issue/different-words
// pair must score a LOWER distance than an unrelated pair. This is what lets NCD
// catch lexically-diverse duplicates that token overlap misses. NCD discriminates
// in the realistic finding-length regime (Problem+Fix+Evidence, ~50-150 words);
// on one-line fragments the compressor's per-string overhead erases the gap, so
// the inputs here are full-length findings.
func TestNCD_DuplicateBeatsUnrelated(t *testing.T) {
	dupA := []byte("the jwt access token issued at login is never invalidated server-side when the user logs out; the logout handler only clears the client cookie so a captured token still authorizes requests after sign-out and can be replayed freely")
	dupB := []byte("after a person signs off from their account the previously granted session credential remains valid and keeps granting access to protected endpoints because the sign-out routine wipes only the browser state and never revokes the bearer")
	unrelated := []byte("the responsive grid on the dashboard overflows its container at viewport widths below three hundred sixty pixels pushing the sidebar off screen and creating a horizontal scrollbar that breaks the entire mobile layout")
	dDup := ncd(dupA, dupB)
	dUnrel := ncd(dupA, unrelated)
	if dDup >= dUnrel {
		t.Errorf("expected duplicate distance %v < unrelated distance %v", dDup, dUnrel)
	}
}

// TestNCD_Bounded: NCD stays within a sane band [0, 1.2] for representative input.
func TestNCD_Bounded(t *testing.T) {
	d := ncd([]byte("alpha beta gamma delta"), []byte("epsilon zeta eta theta iota"))
	if d < 0 || d > 1.2 {
		t.Errorf("ncd out of band: %v", d)
	}
}

// TestNCD_Deterministic: the same inputs always yield the same distance bits.
func TestNCD_Deterministic(t *testing.T) {
	a := []byte("missing nil check before dereferencing the response body")
	b := []byte("response body dereferenced without a guard against nil")
	if ncd(a, b) != ncd(a, b) {
		t.Errorf("ncd not deterministic across calls")
	}
}
