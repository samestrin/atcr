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
	salvaged  bool
}

func (m *metaTruncatingCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return m.content, nil
}

func (m *metaTruncatingCompleter) CompleteWithMeta(_ context.Context, _ llmclient.Invocation) (llmclient.Completion, error) {
	return llmclient.Completion{Content: m.content, Truncated: m.truncated, Salvaged: m.salvaged}, nil
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

// A response whose only content is a ```json block that decodes to nothing is
// the JSON-era form of "emitted prose no parser could use": recorded, not a
// clean review. (The clean-review sentinel is covered above.)
func TestInvokeSlot_UndecodableJSONBlock_IsRecordedUnparseable(t *testing.T) {
	c := &mapMetaCompleter{byModel: map[string]llmclient.Completion{
		"primary": {Content: "```json\n[{\"severity\": oops\n```\n"},
	}}
	e := NewEngine(c, WithTruncationFailover())
	slot := Slot{Primary: Agent{Name: "brad", Invocation: llmclient.Invocation{Model: "primary"}}}
	r := e.invokeSlot(context.Background(), slot)

	assert.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 0, r.ParsedFindingCount())
	assert.True(t, r.UnparseableResponse, "an undecodable JSON block is not a clean review")
}

// The truncation-failover gate counts only COMPLETE findings: a JSON block cut
// off mid-object contributes the objects before the cut, never the partial one.
func TestResult_ParsedFindingCount_CountsOnlyCompleteJSONFindings(t *testing.T) {
	r := &Result{Content: "```json\n[" +
		`{"severity":"HIGH","file_line":"a.go:1","problem":"p"},` + "\n" +
		`{"severity":"LOW","file_line":"b.go:2","problem":"q"},` + "\n" +
		`{"severity":"LOW","file_line":"c.go:3","prob`}
	assert.Equal(t, 2, r.ParsedFindingCount())
}

// TD internal/llmclient/client.go:394: a salvaged reply (empty content, the
// chain-of-thought promoted to Content, finish_reason=stop) comes back StatusOK
// and NOT truncated — caching it would replay one model's raw reasoning as a
// clean review on a later same-diff run. The cache gate must read the Salvaged
// marker, not just ResponseTruncated.
//
// Scoped to CACHING only. Since T6 (sprint 35.16.11.2.2.4) parseFindings refuses a
// salvaged reply outright, so this row now contributes zero findings as well — the
// salvage no longer "still contributes a partial review" anywhere. StatusOK below
// records that the CALL succeeded, not that its content is usable. See
// TestInvokeSlot_SalvagedReply_ContributesNoFindings.
func TestCache_DoesNotCacheSalvagedReply(t *testing.T) {
	cache := &memCache{m: map[string]string{}}
	c := &metaTruncatingCompleter{content: "CHAIN OF THOUGHT ONLY", salvaged: true}
	e := NewEngine(c, WithCache(cache, false))
	slot := Slot{Primary: Agent{Name: "bruce", CacheKey: "k1", Invocation: llmclient.Invocation{Model: "m"}}}

	r := e.invokeSlot(context.Background(), slot)
	assert.Equal(t, StatusOK, r.Status, "the call still succeeded; this gate only refuses to cache")
	assert.True(t, r.Salvaged, "the Salvaged marker must ride the Result")

	_, cached := cache.m["k1"]
	assert.False(t, cached, "a salvaged reply must not be written to the diff cache")
}

// --- T4 (sprint 35.16.11.2.2.4): strip <think> before findings are parsed -----

// TEST MAP: this is the parseFindings strip/content half. The merge-level cases
// live in chunker_test.go (TestMergeResultGroup_*Salvaged*), and the
// invokeSlot/salvage cases in engine_test.go (the T6 block); all three cover the
// one parseFindings choke point.
//
// A model that reasons inline can draft a finding inside a <think> block and then
// drop it before its real answer. parseFindings is the single choke point both
// ParsedFindingCount and findingsFor share, so the strip lives there: the draft
// must not be counted and must not reach the pool.
func TestResult_ParseFindings_ThinkWrappedDraftLosesToTheRealFinding(t *testing.T) {
	// The draft sits on its own line so the parser DOES read it today: pre-fix
	// this content yields two findings, which is what makes the draft's
	// disappearance the thing under test rather than a parsing accident.
	const content = "<think>\nHIGH|a.go:1|draft finding|f|correctness|1|e\n" +
		"on reflection a.go is fine\n</think>\nMEDIUM|b.go:2|real finding|f|correctness|2|e"

	// Fresh Result per assertion: ParsedFindingCount memoizes on first use.
	assert.Equal(t, 1, (&Result{Content: content}).ParsedFindingCount(),
		"the draft finding inside <think> must not be counted")

	fr := findingsFor(Result{Agent: "bruce", Status: StatusOK, Content: content}, nil)
	require.Len(t, fr.Findings, 1, "findingsFor must see the stripped content too, not just the count gate")
	assert.Equal(t, "b.go", fr.Findings[0].File)
	assert.Equal(t, "real finding", fr.Findings[0].Problem)
}

// The leading-only rule, at this lane's call site: a reviewer reviewing THIS
// sprint's diff writes a finding whose text quotes both tags. The finding starts
// the content, so nothing is a leading run and nothing is removed.
func TestResult_ParseFindings_QuotedTagAfterAnswerSurvives(t *testing.T) {
	const problem = "parseFindings never strips <think> or </think>"
	content := "HIGH|a.go:1|" + problem + "|add the strip|correctness|5|e"

	assert.Equal(t, 1, (&Result{Content: content}).ParsedFindingCount())
	fr := findingsFor(Result{Agent: "bruce", Status: StatusOK, Content: content}, nil)
	require.Len(t, fr.Findings, 1)
	assert.Equal(t, problem, fr.Findings[0].Problem,
		"a finding that merely names the tags must be byte-identical after the strip")
}

// A genuinely clean review from a thinking-inline model. IsNoFindings returns
// false on ANY text besides the sentinel, so without a strip before the sentinel
// check the reply reads as "prose no parser could use" and gets stamped
// UnparseableResponse — which ReviewerOutcome ranks ABOVE clean, so the false flag
// reaches the scorecard and the reviewer's trust prior.
func TestInvokeSlot_ThinkWrappedCleanReview_IsNotUnparseable(t *testing.T) {
	c := &mapMetaCompleter{byModel: map[string]llmclient.Completion{
		"primary": {Content: "<think>checked every file in the diff</think>\nNO FINDINGS"},
	}}
	e := NewEngine(c, WithTruncationFailover())
	r := e.invokeSlot(context.Background(), Slot{Primary: Agent{Name: "brad", Invocation: llmclient.Invocation{Model: "primary"}}})

	assert.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 0, r.ParsedFindingCount())
	assert.False(t, r.UnparseableResponse, "a stripped clean review is clean, not unparseable")
}

// PINNED, not special-cased (task-04 Risk Mitigation): a reply that is ONLY a
// think block has no answer at all. Zero findings is correct, and it is NOT the
// clean-review sentinel, so UnparseableResponse is the right flag — the reviewer
// really did emit nothing a parser could use.
func TestInvokeSlot_ThinkOnlyReply_IsUnparseable(t *testing.T) {
	c := &mapMetaCompleter{byModel: map[string]llmclient.Completion{
		"primary": {Content: "<think>HIGH|a.go:1|draft|f|correctness|1|e</think>"},
	}}
	e := NewEngine(c, WithTruncationFailover())
	r := e.invokeSlot(context.Background(), Slot{Primary: Agent{Name: "brad", Invocation: llmclient.Invocation{Model: "primary"}}})

	assert.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 0, r.ParsedFindingCount(), "a think-only reply carries no answer to parse")
	assert.True(t, r.UnparseableResponse, "think-only is not the clean-review sentinel")
}

// The strip must never reassign r.Content: review.md writes the raw reply
// (artifacts.go), and plan.md's Rollback Plan keeps it that way. The count
// assertion is deliberately absent — it is 1 with or without the strip, so it
// would discriminate nothing here; the sibling draft test covers the count.
func TestResult_ParseFindings_LeavesContentUnstripped(t *testing.T) {
	const content = "<think>draft</think>\nMEDIUM|b.go:2|real|f|correctness|2|e"
	r := &Result{Content: content}
	_ = r.ParsedFindingCount() // force the parse, which is what could mutate Content
	assert.Equal(t, content, r.Content, "the raw reply must survive for review.md")
}

// ACCEPTED LOSS, pinned so it is a decision on the record rather than a surprise.
// A LEADING  thinking with no canonical closer makes the whole reply reasoning. Where
// that lands depends on WHY the closer is missing: a reply cut off mid-thought has
// no following finding and is demoted by the truncation-failover gate instead — see
// TestInvokeSlot_TruncatedThinkOnlyReply_DemotesToFailover for that half.
//
// The variant-spelling half is NO LONGER an accepted loss. llmclient.SplitThink was
// changed (2026-09-30, superseding TD-002) so an unclosed opener followed by a
// closer-LIKE token strips NOTHING: the opener was real and its closer was merely
// spelled differently, so returning the reply whole beats classifying the entire
// answer as reasoning. A real finding after the variant closer therefore SURVIVES,
// which is what the second case below now asserts. Only a genuinely cut-off reply
// (no closer-like token at all) still loses its tail; that case keeps its own
// subtest.
//
// The raw-fallback rejection below is unaffected: it concerns a reply cut off
// mid-thought, whose raw parse sees only the draft.
func TestResult_ParseFindings_UnclosedLeadingOpenerLosesTheReply(t *testing.T) {
	t.Run("unclosed opener swallows a real finding after it", func(t *testing.T) {
		const c = "<think>\nreasoning about the diff\nHIGH|a.go:1|real finding|f|correctness|5|e"
		assert.Equal(t, 1, len(parseRawFindings(c)), "the raw reply really does hold one finding")
		assert.Equal(t, 0, (&Result{Content: c}).ParsedFindingCount(), "and the strip loses it")
	})
	t.Run("variant closer is not a closer: nothing is stripped, the finding survives", func(t *testing.T) {
		// Supersedes the TD-002 accepted-loss. SplitThink now returns the reply whole
		// when a variant closer follows an unclosed opener, so the real finding after it
		// is preserved rather than silently lost with the reasoning.
		const c = "<think>reasoning</thinking>\nHIGH|a.go:1|real|f|correctness|5|e"
		assert.Equal(t, 1, (&Result{Content: c}).ParsedFindingCount(),
			"a variant closer no longer makes the whole reply reasoning")
		assert.Equal(t, c, (&Result{Content: c}).Content, "the raw reply must survive for review.md")
	})
	t.Run("why the raw fallback is rejected: raw returns the draft", func(t *testing.T) {
		// A reply cut off mid-thought has no real answer, only the draft. Falling
		// back to the raw parse here would promote that draft to a real finding.
		const c = "<think>\nHIGH|a.go:1|draft|f|correctness|5|e"
		require.Len(t, parseRawFindings(c), 1, "the raw parse sees the DRAFT")
		assert.Equal(t, "draft", parseRawFindings(c)[0].Problem)
		assert.Equal(t, 0, (&Result{Content: c}).ParsedFindingCount(), "the strip correctly drops it")
	})
}

// parseRawFindings is the unstripped parse, used only to show what the strip gives
// up in TestResult_ParseFindings_UnclosedLeadingOpenerLosesTheReply.
func parseRawFindings(content string) []stream.Finding {
	return stream.ParseModelOutput([]byte(content))
}

// A truncated reply whose content is only a leading think block now demotes to
// StatusFailed and burns a backup-model call, where before the strip its draft row
// parsed and the slot stayed StatusOK. That is the intended reading of the task's
// Risk Mitigation ("pin that outcome, do not special-case it away"), but the
// failover cost is real, so it is recorded here rather than discovered in a run.
func TestInvokeSlot_TruncatedThinkOnlyReply_DemotesToFailover(t *testing.T) {
	e := NewEngine(&metaTruncatingCompleter{
		content:   "<think>\nHIGH|a.go:1|draft|f|correctness|5|e\n",
		truncated: true,
	}, WithTruncationFailover())
	r := e.invokeSlot(context.Background(), Slot{Primary: Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "m"}}})

	assert.Equal(t, 0, r.ParsedFindingCount())
	assert.Equal(t, StatusFailed, r.Status, "a truncated think-only reply is a runaway, not a clean review")
	assert.ErrorIs(t, r.Err, errTruncatedZeroFindings)
}
