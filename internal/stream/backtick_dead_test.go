package stream

import "testing"

// TestInfoStringBacktickDead documents that the backtick check in isInfoString
// is redundant: any token holding a backtick already fails infoStringTokenRe,
// so the token loop alone must reject a backtick-carrying rest. Behavior-
// preserving dead-code deletion is not RED-testable; this pins the equivalence.
func TestInfoStringBacktickDead(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"```json title=x", true},
		{"```js `x`", false}, // backtick in rest → content
		{"```js x`y", false}, // backtick in bare token → content
		{"```js a=1 b=2", true},
		{"``` but the lock is never released", false},
	}
	for _, tc := range cases {
		c, n := fenceRun(tc.line)
		if got := isInfoString(tc.line, c, n); got != tc.want {
			t.Errorf("isInfoString(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}
