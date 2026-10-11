package verify

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/metrics"
)

// verdictCorpusDir is the shared jsonrepair fixture corpus (epic
// 35.16.11.2.2.10 T2). See its README for the layout.
var verdictCorpusDir = filepath.Join("..", "jsonrepair", "testdata")

type verdictFixture struct {
	Name   string `json:"name"`
	Lane   string `json:"lane"`
	Class  string `json:"class"`
	CutOff bool   `json:"cut_off"`
}

func loadVerdictCorpus(t *testing.T) []verdictFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(verdictCorpusDir, "corpus.json"))
	require.NoError(t, err)
	var m struct {
		Fixtures []verdictFixture `json:"fixtures"`
	}
	require.NoError(t, json.Unmarshal(raw, &m))
	var out []verdictFixture
	for _, f := range m.Fixtures {
		if f.Lane == "verdict" {
			out = append(out, f)
		}
	}
	require.NotEmpty(t, out, "the corpus must carry verdict-lane fixtures")
	return out
}

// readVerdictFixture returns the reply text and the expected verdict, or nil
// when the fixture expects no verdict.
func readVerdictFixture(t *testing.T, f verdictFixture) (string, *struct{ Verdict, Reasoning string }) {
	t.Helper()
	in, err := os.ReadFile(filepath.Join(verdictCorpusDir, "verdict", f.Name+".txt"))
	require.NoError(t, err)
	w, err := os.ReadFile(filepath.Join(verdictCorpusDir, "verdict", f.Name+".want.json"))
	require.NoError(t, err)
	var want *struct{ Verdict, Reasoning string }
	require.NoError(t, json.Unmarshal(w, &want))
	return string(in), want
}

// TestParseVerdictCore_RepairCorpus: every damaged verdict in the corpus returns
// its real verdict, not malformed_output, and is reported repaired; the valid
// controls parse strictly and are not; the cut-off reply yields no verdict.
func TestParseVerdictCore_RepairCorpus(t *testing.T) {
	t.Parallel()
	for _, f := range loadVerdictCorpus(t) {
		t.Run(f.Name, func(t *testing.T) {
			t.Parallel()
			in, want := readVerdictFixture(t, f)
			v, cause, repaired := parseVerdictCore(in)
			if want == nil {
				assert.NotEqual(t, parseCauseUsable, cause, "no verdict may be taken from %s", f.Name)
				assert.Equal(t, verdictUnverifiable, v.Verdict)
				assert.False(t, repaired)
				assert.False(t, carriesVerdict(in), "carriesVerdict must agree with parseVerdictCore")
				return
			}
			require.Equal(t, parseCauseUsable, cause, "notes: %s", v.Notes)
			assert.Equal(t, want.Verdict, v.Verdict)
			assert.Equal(t, want.Reasoning, v.Notes)
			assert.Equal(t, f.Class != "valid", repaired, "repaired must be set only for a damaged verdict")
			assert.True(t, carriesVerdict(in), "carriesVerdict must agree with parseVerdictCore")
		})
	}
}

func TestParseVerdictCore_RepairsSingleQuotesAndTrailingComma(t *testing.T) {
	t.Parallel()
	for name, in := range map[string]string{
		"single quotes":  `{'verdict': 'confirmed', 'reasoning': 'nil deref at line 9'}`,
		"trailing comma": `{"verdict": "confirmed", "reasoning": "nil deref at line 9",}`,
	} {
		v, cause, repaired := parseVerdictCore(in)
		assert.Equal(t, parseCauseUsable, cause, name)
		assert.Equal(t, verdictConfirmed, v.Verdict, name)
		assert.Equal(t, "nil deref at line 9", v.Notes, name)
		assert.True(t, repaired, name)
		assert.True(t, carriesVerdict(in), name)
	}
}

// TestParseVerdictCore_StrictVerdictBeatsEarlierDamagedDecoy: repair is a last
// try across the whole reply, so a damaged object ahead of a strict verdict is
// never repaired into the answer.
func TestParseVerdictCore_StrictVerdictBeatsEarlierDamagedDecoy(t *testing.T) {
	t.Parallel()
	in := "An example of the format: {'verdict': 'refuted', 'reasoning': 'example',}\n\n" +
		`{"verdict": "confirmed", "reasoning": "the real one"}`
	v, cause, repaired := parseVerdictCore(in)
	require.Equal(t, parseCauseUsable, cause)
	assert.Equal(t, verdictConfirmed, v.Verdict, "the strict verdict must win over the damaged decoy")
	assert.Equal(t, "the real one", v.Notes)
	assert.False(t, repaired)
	assert.True(t, carriesVerdict(in))
}

// TestParseVerdictCore_RepairedOutOfEnumIsInvalidVerdict: a repaired candidate
// with an out-of-enum value is reported as such rather than as malformed.
func TestParseVerdictCore_RepairedOutOfEnumIsInvalidVerdict(t *testing.T) {
	t.Parallel()
	v, cause, repaired := parseVerdictCore(`{'verdict': 'maybe'}`)
	assert.Equal(t, parseCauseInvalidEnum, cause)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.False(t, repaired)
	assert.False(t, carriesVerdict(`{'verdict': 'maybe'}`))
}

// TestRepairStaysOffTheSharedWalkAndExecutorLane: forEachJSONObject still hands
// callers the raw candidate, and parseExecutorResponse (whose fix --auto-fix
// writes to disk) still refuses a damaged envelope.
func TestRepairStaysOffTheSharedWalkAndExecutorLane(t *testing.T) {
	t.Parallel()
	damaged := `{'fix': 'add a bounds check', 'explanation': 'x',}`
	var seen []string
	forEachJSONObject(damaged, func(obj string) bool { seen = append(seen, obj); return false })
	assert.Equal(t, []string{damaged}, seen, "the shared walk must yield the candidate unrepaired")

	_, err := parseExecutorResponse(damaged)
	assert.Error(t, err, "the executor fix reply must stay strict")
}

// TestInvokeSkeptic_RepairableCutOffReplyStillRefused: the truncation guard
// runs before the parser, so a cut-off reply is refused even when its verdict
// object is complete and repairable.
func TestInvokeSkeptic_RepairableCutOffReplyStillRefused(t *testing.T) {
	t.Parallel()
	cc := &fakeChatCompleter{turns: []chatTurn{{content: `{'verdict': 'refuted', 'reasoning': 'draft',}`, truncated: true}}}
	v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher(), false)
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Equal(t, "response_truncated", v.Notes)
}

// TestInvokeSkeptic_CountsEachAcceptedRepair: the marker increments once for a
// repaired verdict and not for an untouched one, and carries no reply text.
// Not parallel: it reads a process-wide counter by delta.
func TestInvokeSkeptic_CountsEachAcceptedRepair(t *testing.T) {
	counter := func() int64 { return metrics.Counter(jsonRepairedVerdictMetric).Value() }
	run := func(reply string) string {
		cc := &fakeChatCompleter{turns: []chatTurn{{content: reply}}}
		v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher(), false)
		require.NoError(t, err)
		return v.Verdict
	}

	before := counter()
	assert.Equal(t, verdictConfirmed, run(`{"verdict": "confirmed", "reasoning": "untouched"}`))
	assert.Equal(t, before, counter(), "a strict verdict must not count as repaired")

	assert.Equal(t, verdictRefuted, run("```json\n{'verdict': 'refuted', 'reasoning': 'secret-reply-text',}\n```"))
	assert.Equal(t, before+1, counter(), "a repaired verdict must count exactly once")

	assert.Equal(t, verdictUnverifiable, run(`no verdict here`))
	assert.Equal(t, before+1, counter(), "a reply with no verdict must not count")

	exposition := metrics.DefaultRegistry.WritePrometheus()
	assert.Contains(t, exposition, `atcr_json_repaired_total{lane="verdict"}`)
	assert.NotContains(t, exposition, "secret-reply-text", "the marker must carry no reply text")
}
