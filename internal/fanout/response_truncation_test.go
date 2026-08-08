package fanout

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// metaTruncatingCompleter implements MetaCompleter so the single-shot path can
// observe a finish_reason=length truncation on the returned Result. It also
// satisfies Completer (Complete) for the degrade path.
type metaTruncatingCompleter struct {
	content   string
	truncated bool
}

func (m *metaTruncatingCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return m.content, nil
}

func (m *metaTruncatingCompleter) CompleteWithMeta(_ context.Context, _ llmclient.Invocation) (llmclient.Completion, error) {
	return llmclient.Completion{Content: m.content, Truncated: m.truncated}, nil
}

// --- Task 1: the truncation signal reaches the Result on both paths ----------

func TestSingleShot_SetsResponseTruncated(t *testing.T) {
	e := NewEngine(&metaTruncatingCompleter{content: "ramble, no findings", truncated: true})
	r := e.invokeAgent(context.Background(), Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "m"}})
	assert.Equal(t, StatusOK, r.Status)
	assert.True(t, r.ResponseTruncated, "single-shot must surface finish_reason=length onto the Result")
	assert.Equal(t, "ramble, no findings", r.Content)
}

func TestSingleShot_NoTruncationWhenClean(t *testing.T) {
	e := NewEngine(&metaTruncatingCompleter{content: "HIGH|a.go:1|b|f|correctness|5|e|bruce", truncated: false})
	r := e.invokeAgent(context.Background(), Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "m"}})
	assert.Equal(t, StatusOK, r.Status)
	assert.False(t, r.ResponseTruncated)
}

// A completer implementing only UsageCompleter (no MetaCompleter) still works —
// ResponseTruncated stays false (graceful degradation, no signal available).
func TestSingleShot_UsageOnlyCompleterLeavesTruncationFalse(t *testing.T) {
	e := NewEngine(&usageCompleter{})
	r := e.invokeAgent(context.Background(), Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "claude"}})
	assert.Equal(t, StatusOK, r.Status)
	assert.False(t, r.ResponseTruncated)
}

func TestToolLoop_SetsResponseTruncatedOnFinalTurn(t *testing.T) {
	// A tool-capable agent whose FINAL content-bearing turn hit finish_reason=length.
	sc := &scriptedChat{turns: []chatTurn{{content: "ramble", truncated: true}}}
	e := NewEngine(sc, WithDispatcher(&fakeDispatcher{}))
	r := e.invokeAgent(context.Background(), Agent{
		Name: "ronin", Tools: true, SupportsFC: true,
		Invocation: llmclient.Invocation{Model: "m"},
	})
	assert.Equal(t, StatusOK, r.Status)
	assert.True(t, r.ResponseTruncated, "tool loop must surface a truncated final answer onto the Result")
}

// mapMetaCompleter returns a per-model scripted Completion, so a slot's primary
// and fallback agents (distinguished by Invocation.Model) can be driven with
// different truncation/finding shapes.
type mapMetaCompleter struct {
	byModel map[string]llmclient.Completion
}

func (m *mapMetaCompleter) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	return m.byModel[inv.Model].Content, nil
}

func (m *mapMetaCompleter) CompleteWithMeta(_ context.Context, inv llmclient.Invocation) (llmclient.Completion, error) {
	return m.byModel[inv.Model], nil
}

// --- Task 2 / AC scenario (a): truncated + 0 findings -> fail + fallback ------

func TestInvokeSlot_TruncatedZeroFindings_FailsAndFallsBack(t *testing.T) {
	c := &mapMetaCompleter{byModel: map[string]llmclient.Completion{
		"primary":  {Content: "I was thinking hard but never emitted a finding", Truncated: true}, // 0 parseable findings
		"fallback": {Content: "HIGH|a.go:1|bug|fix|correctness|5|ev|bruce"},                       // 1 finding
	}}
	e := NewEngine(c, WithTruncationFailover())
	slot := Slot{
		Primary:   Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "primary"}},
		Fallbacks: []Agent{{Name: "bruce-fb", Invocation: llmclient.Invocation{Model: "fallback"}}},
	}
	r := e.invokeSlot(context.Background(), slot)
	assert.Equal(t, StatusOK, r.Status, "fallback rescued the slot")
	assert.True(t, r.FallbackUsed, "the truncated primary must trigger the fallback chain")
	assert.Equal(t, "bruce", r.Agent, "attribution follows the slot's primary")
	assert.Contains(t, r.Content, "HIGH|a.go:1", "the fallback's findings win")
}

// When every agent in the chain truncates empty, the slot fails (no false clean).
func TestInvokeSlot_AllTruncatedEmpty_SlotFails(t *testing.T) {
	c := &mapMetaCompleter{byModel: map[string]llmclient.Completion{
		"primary":  {Content: "ramble one", Truncated: true},
		"fallback": {Content: "ramble two", Truncated: true},
	}}
	e := NewEngine(c, WithTruncationFailover())
	slot := Slot{
		Primary:   Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "primary"}},
		Fallbacks: []Agent{{Name: "bruce-fb", Invocation: llmclient.Invocation{Model: "fallback"}}},
	}
	r := e.invokeSlot(context.Background(), slot)
	assert.Equal(t, StatusFailed, r.Status)
	assert.True(t, r.ResponseTruncated, "the surviving failed result stays marked truncated")
}

// --- Unflagged zero-findings responses ---------------------------------------

// A reviewer that returns no content at all fails over even without a
// truncation flag: there is nothing that could have parsed, so it is
// indistinguishable from a dead call and must not be recorded as a clean
// review. This is the shape agent brad hit on four cases of the 35.16.2
// dry-run.
func TestInvokeSlot_EmptyResponse_FailsAndFallsBackWithoutATruncationFlag(t *testing.T) {
	c := &mapMetaCompleter{byModel: map[string]llmclient.Completion{
		"primary":  {Content: "", Truncated: false},
		"fallback": {Content: "HIGH|a.go:1|bug|fix|correctness|5|ev|bruce"},
	}}
	e := NewEngine(c, WithTruncationFailover())
	slot := Slot{
		Primary:   Agent{Name: "brad", Invocation: llmclient.Invocation{Model: "primary"}},
		Fallbacks: []Agent{{Name: "brad-backup", Invocation: llmclient.Invocation{Model: "fallback"}}},
	}
	r := e.invokeSlot(context.Background(), slot)

	assert.Equal(t, StatusOK, r.Status, "the backup rescued the slot")
	assert.True(t, r.FallbackUsed,
		"an empty response must fail over: scoring it as a clean zero-finding review hides a dead call")
	assert.Equal(t, "brad", r.Agent, "attribution follows the slot's primary")
}

// The explicit clean-review sentinel is the positive signal that lets the
// empty-response gate above be safe: a reviewer with nothing to report says so
// rather than going silent, so silence is left meaning "the call produced
// nothing". It must be neither a failure nor an anomaly.
func TestInvokeSlot_NoFindingsSentinel_IsACleanReview(t *testing.T) {
	c := &mapMetaCompleter{byModel: map[string]llmclient.Completion{
		"primary":  {Content: stream.NoFindingsSentinel},
		"fallback": {Content: "HIGH|a.go:1|bug|fix|correctness|5|ev|bruce"},
	}}
	e := NewEngine(c, WithTruncationFailover())
	slot := Slot{
		Primary:   Agent{Name: "brad", Invocation: llmclient.Invocation{Model: "primary"}},
		Fallbacks: []Agent{{Name: "brad-backup", Invocation: llmclient.Invocation{Model: "fallback"}}},
	}
	r := e.invokeSlot(context.Background(), slot)

	assert.Equal(t, StatusOK, r.Status, "a reviewer with nothing to report is a success")
	assert.False(t, r.FallbackUsed, "the backup model is not spent on a declared clean review")
	assert.False(t, r.UnparseableResponse,
		"the sentinel is the specified clean-review output, so it is not an anomaly")
	assert.Equal(t, 0, r.ParsedFindingCount(), "and it yields no findings")
}

// The anomalous shape — the model said something other than the sentinel, none
// of it parsed as a finding — is recorded rather than failed over. It is not a
// failure, but findings_count 0 alone cannot distinguish it from a clean review.
func TestInvokeSlot_UnparseableNonEmptyResponse_StaysOKAndIsRecorded(t *testing.T) {
	c := &mapMetaCompleter{byModel: map[string]llmclient.Completion{
		"primary": {Content: "I reviewed the diff and found nothing worth reporting.", Truncated: false},
	}}
	e := NewEngine(c, WithTruncationFailover())
	slot := Slot{Primary: Agent{Name: "brad", Invocation: llmclient.Invocation{Model: "primary"}}}
	r := e.invokeSlot(context.Background(), slot)

	assert.Equal(t, StatusOK, r.Status, "a clean review is a legitimate outcome, not a failure")
	assert.False(t, r.FallbackUsed, "the backup model is not spent on a plausible clean review")
	assert.True(t, r.UnparseableResponse,
		"but it is recorded distinctly, so 'produced nothing parseable' is not silently equal to 'found nothing'")

	// And it reaches the run-result, which is where a leaderboard reader looks.
	st := statusFor(r, findingsResult{})
	assert.True(t, st.UnparseableResponse, "the marker is surfaced per-agent in status.json")
	assert.Equal(t, 0, st.FindingsCount, "the count alone stays 0 — which is exactly why the marker is needed")
}

// A TRUNCATED empty response is still a failover: there the provider told us
// the response was cut off, so "emitted nothing" is a runaway, not a clean
// review. This is the line between the two behaviours.
func TestInvokeSlot_TruncatedEmptyResponse_StillFailsOver(t *testing.T) {
	c := &mapMetaCompleter{byModel: map[string]llmclient.Completion{
		"primary":  {Content: "", Truncated: true},
		"fallback": {Content: "HIGH|a.go:1|bug|fix|correctness|5|ev|bruce"},
	}}
	e := NewEngine(c, WithTruncationFailover())
	slot := Slot{
		Primary:   Agent{Name: "brad", Invocation: llmclient.Invocation{Model: "primary"}},
		Fallbacks: []Agent{{Name: "brad-backup", Invocation: llmclient.Invocation{Model: "fallback"}}},
	}
	r := e.invokeSlot(context.Background(), slot)

	assert.True(t, r.FallbackUsed,
		"the truncation flag is what turns an empty response from a clean review into a runaway")
}

// --- Task 2 / AC scenario (b): truncated + >=1 finding -> StatusOK + marker ---

func TestInvokeSlot_TruncatedWithFindings_StaysOKWithMarker(t *testing.T) {
	c := &mapMetaCompleter{byModel: map[string]llmclient.Completion{
		"primary": {Content: "HIGH|a.go:1|bug|fix|correctness|5|ev|bruce\nMORE ramble cut off mid", Truncated: true},
	}}
	e := NewEngine(c, WithTruncationFailover())
	slot := Slot{Primary: Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "primary"}}}
	r := e.invokeSlot(context.Background(), slot)
	assert.Equal(t, StatusOK, r.Status, "partial findings are kept, not discarded")
	assert.False(t, r.FallbackUsed)
	assert.True(t, r.ResponseTruncated, "the truncated marker is preserved on the kept result")
	assert.Contains(t, r.Content, "HIGH|a.go:1")
}

// The policy is opt-in: an engine WITHOUT WithTruncationFailover (e.g. the
// executor's) keeps prior behavior — a truncated-empty response stays StatusOK.
func TestInvokeSlot_NoFailoverOption_TruncatedEmptyStaysOK(t *testing.T) {
	c := &mapMetaCompleter{byModel: map[string]llmclient.Completion{
		"primary": {Content: "ramble no findings", Truncated: true},
	}}
	e := NewEngine(c) // no WithTruncationFailover
	slot := Slot{Primary: Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "primary"}}}
	r := e.invokeSlot(context.Background(), slot)
	assert.Equal(t, StatusOK, r.Status, "without the option the demotion does not fire")
}

// memCache is an in-memory reviewCache for the cache-poisoning regression test.
type memCache struct{ m map[string]string }

func (c *memCache) Get(k string) (string, bool, error) { v, ok := c.m[k]; return v, ok, nil }
func (c *memCache) Put(k, v string) error              { c.m[k] = v; return nil }

// A truncated, zero-finding runaway must NEVER be written to the diff cache:
// otherwise a later same-diff run replays it as a clean StatusOK review with the
// failover gate bypassed — the exact silent all-clean the epic prevents, served
// from cache (independent-review HIGH).
func TestCache_DoesNotCacheTruncatedRunaway(t *testing.T) {
	cache := &memCache{m: map[string]string{}}
	c := &metaTruncatingCompleter{content: "I rambled but emitted no finding", truncated: true}
	e := NewEngine(c, WithCache(cache, false), WithTruncationFailover())
	slot := Slot{Primary: Agent{Name: "bruce", CacheKey: "k1", Invocation: llmclient.Invocation{Model: "m"}}}

	r := e.invokeSlot(context.Background(), slot)
	assert.Equal(t, StatusFailed, r.Status, "truncated+zero demotes to failed")

	_, cached := cache.m["k1"]
	assert.False(t, cached, "a truncated runaway must not be written to the diff cache")
}

// --- Task 4: telemetry markers ------------------------------------------------

func TestStatusFor_SetsResponseTruncated(t *testing.T) {
	st := statusFor(Result{Agent: "bruce", Status: StatusFailed, ResponseTruncated: true}, findingsResult{})
	assert.True(t, st.ResponseTruncated)
}

func TestStatusFor_ResponseTruncatedFalseWhenClean(t *testing.T) {
	st := statusFor(Result{Agent: "bruce", Status: StatusOK}, findingsResult{})
	assert.False(t, st.ResponseTruncated)
}

func TestWritePool_CountsTruncatedZeroFindings(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	results := []Result{
		// truncated, zero parseable findings -> counted + per-agent marker
		{Agent: "runaway", Status: StatusFailed, ResponseTruncated: true, Content: "I rambled but emitted no finding", Err: errTruncatedZeroFindings},
		// truncated, but kept a finding -> per-agent marker, NOT counted in the run tally
		{Agent: "partial", Status: StatusOK, ResponseTruncated: true, Content: "HIGH|a.go:1|b|f|correctness|5|e|partial"},
		// clean -> neither
		{Agent: "clean", Status: StatusOK, Content: "HIGH|b.go:2|b|f|correctness|5|e|clean"},
	}
	_, err := WritePool(pool, results, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(pool, "summary.json"))
	require.NoError(t, err)
	var ps PoolSummary
	require.NoError(t, json.Unmarshal(data, &ps))

	assert.Equal(t, 1, ps.TruncatedZeroFindings, "only the truncated zero-finding agent is counted")

	byAgent := map[string]AgentStatus{}
	for _, a := range ps.Agents {
		byAgent[a.Agent] = a
	}
	assert.True(t, byAgent["runaway"].ResponseTruncated)
	assert.Equal(t, 0, byAgent["runaway"].FindingsCount)
	assert.True(t, byAgent["partial"].ResponseTruncated)
	assert.Equal(t, 1, byAgent["partial"].FindingsCount)
	assert.False(t, byAgent["clean"].ResponseTruncated)
}

// TestResult_ParsedFindingCount verifies that the engine computes the number of
// parseable findings once when a result is built. The cached count is shared by
// the truncation-failover gate and findingsFor instead of each path parsing
// Content independently (TD-019).
func TestResult_ParsedFindingCount(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{"zero findings", "ramble, no findings", 0},
		{"one finding", "HIGH|a.go:1|bug|fix|correctness|5|ev|bruce", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEngine(&metaTruncatingCompleter{content: tt.content, truncated: true})
			r := e.invokeAgent(context.Background(), Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "m"}})
			assert.Equal(t, tt.want, r.ParsedFindingCount(), "parsed finding count should be computed on result construction")
		})
	}
}

func TestToolLoop_ParsedFindingCount(t *testing.T) {
	sc := &scriptedChat{turns: []chatTurn{{content: "HIGH|a.go:1|bug|fix|correctness|5|ev|bruce", truncated: true}}}
	e := NewEngine(sc, WithDispatcher(&fakeDispatcher{}))
	r := e.invokeAgent(context.Background(), Agent{
		Name: "ronin", Tools: true, SupportsFC: true,
		Invocation: llmclient.Invocation{Model: "m"},
	})
	assert.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 1, r.ParsedFindingCount(), "tool-loop result should carry the parsed finding count")
}
