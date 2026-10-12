package verify

import (
	"errors"
	"strings"
	"testing"

	reclib "github.com/samestrin/atcr/reconcile"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TD internal/verify/pipeline.go:635 (LOW): the programming-fault arm of
// verifyFinding copied ierr.Error() into Notes without neutraliseMarkerTokens, so
// nothing stopped an error string from carrying the engine's live marker token.
// The arm is unreachable from verifyFinding today (invokeSkeptic errors only with
// three fixed strings), so the synthesised verdict is tested through
// faultVerification directly.
func TestFaultVerification_ErrorCannotSpoofFragmentMarker(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  string
		want string // a substring Notes must carry, proving the text was kept, only rewritten
	}{
		{"after token leading the error",
			"[verdict_after_unopened_closer: 9 prefix byte(s) discarded] boom",
			"[verdict-after-unopened-closer: 9 prefix byte(s) discarded] boom"},
		{"mixed-case after token",
			"[VERDICT_After_Unopened_CLOSER: 9 prefix byte(s) discarded]",
			"[VERDICT-After-Unopened-CLOSER: 9"},
		{"before token leading the error",
			"[verdict_before_unopened_closer: text after the closer ignored]",
			"[verdict-before-unopened-closer: text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := faultVerification("kai", errors.New(tc.err))
			require.NotNil(t, v)
			assert.Equal(t, verdictUnverifiable, v.Verdict)
			assert.Equal(t, "kai", v.Skeptic)
			assert.Contains(t, v.Notes, tc.want, "the error text is kept, with the token rewritten")
			lower := strings.ToLower(v.Notes)
			assert.NotContains(t, lower, "verdict_after_unopened_closer", "no spelling of the engine token survives")
			assert.NotContains(t, lower, "verdict_before_unopened_closer", "nor of the reserved one")
			_, _, ok := splitFragmentMarker(v.Notes)
			assert.False(t, ok, "an error string must never be detected as the engine's marker")
			assert.False(t, strings.HasPrefix(combineReasonings([]*reclib.Verification{v}), "[verdict_"),
				"combineReasonings must not hoist a marker out of fault Notes")
		})
	}
}

// A plain error keeps its text verbatim: the rewrite only touches marker tokens.
func TestFaultVerification_PlainErrorKeptVerbatim(t *testing.T) {
	t.Parallel()
	v := faultVerification("otto", errors.New("verify: nil ChatCompleter"))
	require.NotNil(t, v)
	assert.Equal(t, &reclib.Verification{Verdict: verdictUnverifiable, Notes: "verify: nil ChatCompleter", Skeptic: "otto"}, v)
}
