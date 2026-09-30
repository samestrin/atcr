package debate

import (
	reclib "github.com/samestrin/atcr/reconcile"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/samestrin/atcr/internal/llmclient"
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

// Sprint 35.16.11.2.1 AC 03-03: under response_format json_object the judge's
// reply is a bare object with no fence and no prose — which is why the judge seat
// needs no Output Format swap. What JSON mode actually changes is the envelope a
// model may choose: a bare, valid-JSON object that nests the ruling inside a
// wrapper (no top-level outcome) must degrade to unresolved — never read the
// nested ruling or crash. (The plain bare ruling is already covered, with
// stricter assertions, by the "uphold" case of TestParseRuling above.)
func TestParseRuling_BareJSONModeObject(t *testing.T) {
	r := parseRuling(`{"ruling":{"outcome":"uphold","reasoning":"nested"},"confidence":0.9}`)
	assert.Equal(t, OutcomeUnresolved, r.Outcome)
	// The diagnostic must say why — an empty unresolved ruling would hide the
	// wrapper-envelope cause from whoever reads the debate log.
	assert.NotEmpty(t, r.Reasoning)
}

// TestParseRuling_ThinkWrappedDraftLosesToTheRealRuling is the decoy shape
// TestParseRuling_SkipsDecoyBrace does not cover. That test skips an object
// LACKING an outcome key; a judge that drafts a ruling inside a leading <think>
// block emits a decoy that HAS one, so it is accepted as the first match and
// the discarded draft becomes the debate's outcome.
//
// The fix lives at the driveSeat choke point, not in parseRuling — internal/llmclient
// owns every tag rule. This is a COMPOSED parse-level guard: it calls
// llmclient.SplitThink in the test body, so it pins the SplitThink-into-parseRuling
// contract, not the lane wiring.
//
// The change-sensitive proof that JudgeRaw reaches parseRuling stripped lives in
// protocol_test.go → TestRunDebate_StripsThinkBlocksFromSeatContent, which drives
// RunDebate. The exhaustive tag table is owned by internal/llmclient/think_test.go;
// rows here are kept only where the parse consequence at THIS level is what is
// pinned. Each row states which kind of guard it is, so the table's size is not
// read as that many RED cases.
func TestParseRuling_ThinkWrappedDraftLosesToTheRealRuling(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantOutcome string
		wantReason  string
	}{
		{
			// NO-REGRESSION GUARD (composed): change-insensitive by construction — it
			// pins the parse consequence, not the strip.
			name:        "a draft ruling inside a closed think block loses to the real one after it",
			raw:         `<think>{"outcome":"overturn","reasoning":"draft, wrong"}</think>{"outcome":"uphold","reasoning":"real answer"}`,
			wantOutcome: OutcomeUphold,
			wantReason:  "real answer",
		},
		{
			// CHANGE-DETECTING: under the reversed bare-closer rule this ruling
			// was stripped to a fragment and degraded to unresolved.
			name:        "a ruling naming only the bare closer keeps its whole prefix",
			raw:         `{"outcome":"uphold","reasoning":"the code never looks for </think> at all"}`,
			wantOutcome: OutcomeUphold,
			wantReason:  "the code never looks for </think> at all",
		},
		{
			// CHANGE-DETECTING: an eager mid-string strip would eat the ruling.
			name: "a ruling quoting both tags after real answer text survives intact",
			// The leading-only rule reaching this lane: the debated finding is
			// itself about think-tag handling, so the judge cites both tags. An
			// eager mid-string strip would eat the ruling it is recording.
			raw:         `{"outcome":"uphold","reasoning":"the handler drops text between <think> and </think>"}`,
			wantOutcome: OutcomeUphold,
			wantReason:  "the handler drops text between <think> and </think>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			answer, _ := llmclient.SplitThink(tc.raw)
			r := parseRuling(answer)
			assert.Equal(t, tc.wantOutcome, r.Outcome)
			assert.Equal(t, tc.wantReason, r.Reasoning)
		})
	}
}
