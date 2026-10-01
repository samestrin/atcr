package payload

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestReasoningReserveClause pins both arms of the toolLoop gate. The gate
// survived mutation: forcing the clause on for every agent left internal/payload,
// internal/fanout and internal/doctor all green, so nothing stopped a single-shot
// agent's zero-budget warning from naming a reserve it never paid. Both lanes
// print this string to operators, and this repo treats a doctor-vs-review sizing
// mismatch as release-blocking, so the empty arm is the load-bearing one.
func TestReasoningReserveClause(t *testing.T) {
	for _, tc := range []struct {
		name        string
		toolLoop    bool
		outputToken int
		want        string
	}{
		{
			name: "single-shot reserves nothing, so it says nothing",
			want: "",
		},
		{
			name: "single-shot with a large cap still says nothing", outputToken: 32768,
			want: "",
		},
		{
			name:     "tool loop names the reserve and the multiplier",
			toolLoop: true, outputToken: 8192,
			want: fmt.Sprintf(", the %d-token replayed-reasoning reserve its tool loop holds back (%d× that cap)",
				8192*ReasoningReplayReserveCaps, ReasoningReplayReserveCaps),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ReasoningReserveClause(tc.toolLoop, tc.outputToken))
		})
	}
}

// TestReasoningReserveClause_MatchesSizingOutputTokens keeps the printed number
// honest against the number the payload is actually sized by. The clause and
// SizingOutputTokens are separate functions over the same rule, so a change to
// one that misses the other is a warning that describes a reservation the run
// did not make.
func TestReasoningReserveClause_MatchesSizingOutputTokens(t *testing.T) {
	const cap = 8192
	reserved := SizingOutputTokens(true, cap) - cap
	assert.Contains(t, ReasoningReserveClause(true, cap), fmt.Sprintf("%d-token", reserved),
		"the clause must name the extra tokens SizingOutputTokens actually holds back")
	assert.Equal(t, cap, SizingOutputTokens(false, cap),
		"a single-shot agent holds back nothing extra, which is why its clause is empty")
	assert.False(t, strings.Contains(ReasoningReserveClause(false, cap), "reserve"),
		"no reserve was held, so the word must not appear")
}
