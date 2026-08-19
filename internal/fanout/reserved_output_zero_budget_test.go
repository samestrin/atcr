package fanout

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reserved_output_tokens was recorded only when the sizing left a positive input budget,
// which suppressed it EXACTLY on the zero-budget arm — the one case where the cap is the
// cause of the degradation rather than incidental to it.
//
// On that record status.json carries resolved_window and degradation_action: overflow
// with nothing naming the cap, and effective_budget is 0 (omitted) too. Nothing else on
// disk carries it either: payload.Manifest records max_parallel and timeout_secs "for
// post-hoc diagnosis" but not the resolved cap or the --max-tokens override, and that
// override is re-resolved from live config on a resume — so config at diagnosis time need
// not be the config the run used. An operator reading the artifact afterwards cannot tell
// whether the WINDOW or the CAP closed the budget, which is the exact question the
// zero-budget remedy asks them to answer.
//
// The old rationale held that reserving 8192 out of a 12288-token window "is a record
// that contradicts itself". It is not a contradiction — it is the CAUSE, and it is the
// only number on the record that explains the zero.
func TestBuildSlots_ZeroBudgetRecordStillNamesTheCap(t *testing.T) {
	// A window this small is fully consumed by the cap plus the fixed prompt overhead,
	// so sizing yields no input budget at all.
	cfg := declaredWindowRoster(t, 12288)

	got, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)

	require.Zero(t, got.EffectiveBudget,
		"premise: this window funds no input budget, so this is the arm under test")
	require.NotZero(t, got.ResolvedWindow,
		"premise: the agent WAS sized — resolved_window is the signal for that")
	require.Zero(t, got.ReservedOutputTokens,
		"premise: nothing was funded, so the RESERVATION is correctly 0 — that field keeps "+
			"its meaning, and this test is not asking it to lie")

	assert.Equal(t, defaultMaxTokens, got.ResolvedMaxTokens,
		"the cap is what closed this budget, so the record has to name it — otherwise nothing "+
			"on disk says whether the window or the cap was at fault")
}

// The same on the fallback lane, which has its own re-derived budget and window and so
// reaches this arm independently.
func TestBuildFallbackAgent_ZeroBudgetRecordStillNamesTheCap(t *testing.T) {
	cfg := declaredWindowRoster(t, 512000)
	kai := cfg.Registry.Agents["kai"]
	kai.Model = "unlisted-backup-model"
	tiny := 1
	kai.ContextWindowTokens = &tiny
	cfg.Registry.Agents["kai"] = kai

	primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)

	var fb Agent
	_ = captureStderr(t, func() { fb, _, err = buildFallbackAgent(cfg, primary, "kai", true, fallbackRefit{}) })
	require.NoError(t, err)
	require.Zero(t, fb.EffectiveBudget, "premise: a 1-token window funds no input budget")
	require.Zero(t, fb.ReservedOutputTokens, "premise: nothing was funded")

	assert.Equal(t, defaultMaxTokens, fb.ResolvedMaxTokens,
		"a fallback that could not fund its cap must still record what that cap was")
}

// A funded agent records both, and they agree — equal is the readable signal for
// "the reservation was funded".
func TestBuildOneAgent_FundedRecordAgreesOnCapAndReservation(t *testing.T) {
	cfg := declaredWindowRoster(t, 128000)

	got, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)
	require.NotZero(t, got.EffectiveBudget, "premise: this window funds a budget")

	assert.Equal(t, got.ReservedOutputTokens, got.ResolvedMaxTokens,
		"when the budget funds the cap the two are the same number; a reader compares them "+
			"to tell a funded reservation from an unfunded one")
}

// A bare, unsized agent (doctor, direct construction) records nothing, so its status.json
// stays byte-identical under omitempty — the same guarantee its sibling sizing fields make.
func TestAgentStatus_UnsizedAgentRecordsNoCap(t *testing.T) {
	st := statusFor(Result{Agent: "greta", Status: StatusOK}, findingsResult{})

	assert.Zero(t, st.ResolvedWindow, "premise: a bare Result is unsized")
	assert.Zero(t, st.ReservedOutputTokens)
	assert.Zero(t, st.ResolvedMaxTokens,
		"an unsized agent resolved no cap; omitempty must keep the key absent as before")
}

// The three carriers that move ResolvedMaxTokens from the serving Agent to the written
// artifact — invokeAgent, invokeSlot's tail, and statusFor — each survived deletion with
// the whole fanout suite green, because every other test for this field stops at the
// Agent that buildOneAgent returned. The field's entire purpose is to appear in
// status.json on a zero-budget record, so a chain that silently drops it there is the
// failure it was added to prevent, one layer down.
//
// Pinned through the REAL write path (writeAgentArtifacts -> WriteStatus -> the file on
// disk), not through statusFor alone: a test that stops at the struct cannot see an
// omitempty tag drop the key, and the key is what an operator actually reads.
func TestWriteStatus_ZeroBudgetRecordCarriesTheCapToDisk(t *testing.T) {
	dir := t.TempDir()
	// The zero-budget shape: sized (resolved_window set), nothing funded (budget and
	// reservation both 0), and a cap that explains why.
	r := Result{
		Agent: "greta", Status: StatusOK, PayloadMode: "diff",
		EffectiveBudget: 0, ResolvedWindow: 12288, ReservedOutputTokens: 0,
		ResolvedMaxTokens: 8192, DegradationAction: degradationOverflow,
	}
	require.NoError(t, writeAgentArtifacts(dir, "greta", r, findingsResult{}))

	data, err := os.ReadFile(filepath.Join(dir, poolRawAgentDir, "greta", statusFile))
	require.NoError(t, err)

	var st AgentStatus
	require.NoError(t, json.Unmarshal(data, &st))
	assert.Equal(t, 8192, st.ResolvedMaxTokens,
		"the cap must survive the whole chain to the artifact — this is the only number on "+
			"a zero-budget record that says what closed the budget")

	assert.Contains(t, string(data), `"resolved_max_tokens": 8192`,
		"and it must be a PRESENT key, not merely a non-zero struct field")
	assert.NotContains(t, string(data), `"reserved_output_tokens"`,
		"precondition: the reservation is absent here, which is the state that makes the "+
			"cap load-bearing rather than redundant")
}

// The engine-level twin, covering the two carriers writeAgentArtifacts cannot reach:
// invokeAgent stamps the sizing record onto the Result, and invokeSlot's tail re-stamps it
// from the Primary on the failure path. Driven through Engine.Run so the value crosses the
// same seams a real run does.
func TestEngineRun_CarriesTheResolvedCapFromAgentToResult(t *testing.T) {
	e := NewEngine(newFake())
	slots := []Slot{{Primary: Agent{
		Name: "greta", Prompt: "p",
		Invocation:     llmclient.Invocation{Model: "m"},
		ResolvedWindow: 12288, ReservedOutputTokens: 0, ResolvedMaxTokens: 8192,
	}}}

	results := e.Run(context.Background(), slots)

	require.Len(t, results, 1)
	assert.Equal(t, 8192, results[0].ResolvedMaxTokens,
		"the engine stamps the sizing record from the serving Agent; without that carrier "+
			"the cap never reaches statusFor and never reaches disk")
}

// The failure-path twin, and the one that actually isolates invokeSlot's tail.
//
// That tail re-stamps the sizing record from the PRIMARY after the whole chain fails,
// because the slot is reported under the primary's name even when a FALLBACK made the last
// attempt. A slot with no fallback cannot show this: invokeAgent already stamped the
// primary's own cap, so the tail is redundant there and the carrier survives deletion. The
// chain below ends on a fallback carrying a DIFFERENT cap, so only the tail can produce the
// primary's number.
func TestEngineRun_FailedSlotReportsThePrimarysCapNotTheLastFallbacks(t *testing.T) {
	f := newFake()
	f.failFor["primary-model"] = errors.New("primary exploded")
	f.failFor["backup-model"] = errors.New("backup exploded too")
	e := NewEngine(f)
	slots := []Slot{{
		Primary: Agent{
			Name: "greta", Prompt: "p",
			Invocation:     llmclient.Invocation{Model: "primary-model"},
			ResolvedWindow: 12288, ReservedOutputTokens: 0, ResolvedMaxTokens: 8192,
		},
		Fallbacks: []Agent{{
			Name: "kai", Prompt: "p",
			Invocation:     llmclient.Invocation{Model: "backup-model"},
			ResolvedWindow: 32768, ReservedOutputTokens: 4096, ResolvedMaxTokens: 4096,
		}},
	}}

	results := e.Run(context.Background(), slots)

	require.Len(t, results, 1)
	require.NotEqual(t, StatusOK, results[0].Status, "precondition: the whole chain failed")
	assert.Equal(t, 8192, results[0].ResolvedMaxTokens,
		"the slot is reported under the primary's name, so its sizing record must describe "+
			"the primary's regime — not the 4096 the last fallback happened to run at")
	assert.Equal(t, 12288, results[0].ResolvedWindow,
		"precondition: its sibling sizing fields follow the same rule, so the cap is not "+
			"being held to a stricter one than the block it sits in")
}
