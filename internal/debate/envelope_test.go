package debate

import (
	reclib "github.com/samestrin/atcr/reconcile"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseRuling(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantOutcome string
		wantSev     string
		wantCluster string
		wantSurvive bool
		wantVerdict string
	}{
		{
			name:        "uphold",
			raw:         `{"outcome":"uphold","settled_severity":"HIGH","reasoning":"evidence holds"}`,
			wantOutcome: OutcomeUphold, wantSev: "HIGH", wantSurvive: true, wantVerdict: reclib.VerdictConfirmed,
		},
		{
			name:        "overturn",
			raw:         `{"outcome":"overturn","reasoning":"false positive"}`,
			wantOutcome: OutcomeOverturn, wantSurvive: false, wantVerdict: reclib.VerdictRefuted,
		},
		{
			name:        "split lowers severity",
			raw:         `{"outcome":"split","settled_severity":"low","reasoning":"real but minor"}`,
			wantOutcome: OutcomeSplit, wantSev: "LOW", wantSurvive: true, wantVerdict: reclib.VerdictConfirmed,
		},
		{
			name:        "gray-zone merge decision",
			raw:         `{"outcome":"uphold","settled_severity":"MEDIUM","cluster_decision":"merge"}`,
			wantOutcome: OutcomeUphold, wantSev: "MEDIUM", wantCluster: ClusterMerge, wantSurvive: true, wantVerdict: reclib.VerdictConfirmed,
		},
		{
			name:        "fenced json",
			raw:         "Here is my ruling:\n```json\n{\"outcome\": \"uphold\", \"settled_severity\": \"CRITICAL\"}\n```\n",
			wantOutcome: OutcomeUphold, wantSev: "CRITICAL", wantSurvive: true, wantVerdict: reclib.VerdictConfirmed,
		},
		{
			name:        "invalid outcome degrades to unresolved",
			raw:         `{"outcome":"maybe","reasoning":"hmm"}`,
			wantOutcome: OutcomeUnresolved, wantVerdict: "",
		},
		{
			name:        "empty degrades to unresolved",
			raw:         "   ",
			wantOutcome: OutcomeUnresolved, wantVerdict: "",
		},
		{
			name:        "garbage degrades to unresolved",
			raw:         "I cannot decide.",
			wantOutcome: OutcomeUnresolved, wantVerdict: "",
		},
		{
			name:        "invalid severity dropped, outcome kept",
			raw:         `{"outcome":"uphold","settled_severity":"BLOCKER"}`,
			wantOutcome: OutcomeUphold, wantSev: "", wantSurvive: true, wantVerdict: reclib.VerdictConfirmed,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r := parseRuling(tc.raw)
			assert.Equal(t, tc.wantOutcome, r.Outcome)
			assert.Equal(t, tc.wantSev, r.SettledSeverity)
			assert.Equal(t, tc.wantCluster, r.ClusterDecision)
			assert.Equal(t, tc.wantSurvive, r.ChallengeSurvived())
			assert.Equal(t, tc.wantVerdict, r.Verdict())
		})
	}
}

func TestParseRuling_SkipsDecoyBrace(t *testing.T) {
	// A decoy object without an "outcome" key before the real ruling must not
	// degrade the result.
	raw := `{"note":"thinking"} then {"outcome":"overturn","reasoning":"nope"}`
	r := parseRuling(raw)
	assert.Equal(t, OutcomeOverturn, r.Outcome)
}
