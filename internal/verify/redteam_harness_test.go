package verify

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repeatResult is the outcome of running one red-team case N times.
type repeatResult struct {
	Runs   int
	Passes int
}

// Rate returns the pass fraction, 0 when nothing ran.
func (r repeatResult) Rate() float64 {
	if r.Runs == 0 {
		return 0
	}
	return float64(r.Passes) / float64(r.Runs)
}

// runRepeatN runs one case n times; run returns whether attempt i caught the
// injected vulnerability. It does not judge the rate — see requireRate.
func runRepeatN(n int, run func(i int) bool) repeatResult {
	res := repeatResult{Runs: n}
	for i := 0; i < n; i++ {
		if run(i) {
			res.Passes++
		}
	}
	return res
}

// meetsThreshold reports whether the pass rate is at or above threshold. It
// compares integers (passes*100 >= threshold*runs) so 19/20 vs 0.95 is not at
// the mercy of float rounding.
func (r repeatResult) meetsThreshold(thresholdPct int) bool {
	return r.Runs > 0 && r.Passes*100 >= thresholdPct*r.Runs
}

// requireRate runs the case n times and fails t when the pass rate is below
// thresholdPct (a whole percent, e.g. 95).
func requireRate(t testing.TB, name string, n, thresholdPct int, run func(i int) bool) repeatResult {
	t.Helper()
	res := runRepeatN(n, run)
	if !res.meetsThreshold(thresholdPct) {
		t.Errorf("%s: %d/%d passed (%.0f%%), below the %d%% threshold", name, res.Passes, res.Runs, res.Rate()*100, thresholdPct)
	}
	return res
}

// scriptedCase runs the Skeptic once against a completer scripted to answer
// "confirmed" except on the attempts in missed, and reports whether it caught
// the vulnerability.
func scriptedCase(missed map[int]bool) func(i int) bool {
	return func(i int) bool {
		verdict := "confirmed"
		if missed[i] {
			verdict = "refuted"
		}
		sk := testSkeptic()
		sk.Config.SupportsFC = false // single-shot path: no tool loop, no FC warning
		v, _, err := invokeSkeptic(context.Background(), sk, "prompt",
			finalChat(fmt.Sprintf(`{"verdict":%q}`, verdict)), okDispatcher(), false)
		return err == nil && v != nil && v.Verdict == "confirmed"
	}
}

func TestRedTeamHarness_PassesAt95(t *testing.T) {
	t.Parallel()
	// 1 miss in 20 = 95%.
	res := requireRate(t, "case", 20, 95, scriptedCase(map[int]bool{7: true}))
	assert.Equal(t, 19, res.Passes)
}

func TestRedTeamHarness_FailsAt90(t *testing.T) {
	t.Parallel()
	// 2 misses in 20 = 90%, below the 95% bar: the harness must report failure.
	res := runRepeatN(20, scriptedCase(map[int]bool{3: true, 11: true}))
	require.Equal(t, 18, res.Passes)
	assert.False(t, res.meetsThreshold(95))

	ft := &failRecorder{}
	requireRate(ft, "case", 20, 95, scriptedCase(map[int]bool{3: true, 11: true}))
	assert.True(t, ft.failed, "requireRate must fail the test below threshold")
}

func TestRedTeamHarness_EdgeCases(t *testing.T) {
	t.Parallel()
	assert.False(t, repeatResult{}.meetsThreshold(95), "zero runs never passes")
	assert.True(t, repeatResult{Runs: 10, Passes: 10}.meetsThreshold(95))
	assert.Equal(t, 0.0, repeatResult{}.Rate())
}

// failRecorder captures Errorf so a test can assert the harness failed without
// failing itself.
type failRecorder struct {
	testing.TB
	failed bool
}

func (f *failRecorder) Helper()                       {}
func (f *failRecorder) Errorf(string, ...interface{}) { f.failed = true }
