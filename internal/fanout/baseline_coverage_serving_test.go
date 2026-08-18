package fanout

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.5.4 T3: baseline coverage must attribute to the agent that ACTUALLY
// SERVED the slot, not unconditionally to its Primary.
//
// This is the prerequisite that makes T2 safe rather than a regression. An
// oversized prompt fails loudly at the provider; a falsified coverage tag is
// silent — CommitBaselineIndex records the files as reviewed and the next scan
// skips them, so a file nothing ever read is never read again. A re-packed
// fallback that inherited its primary's tag would produce exactly that.
//
// BOTH decision points must close. The `allOK` short-circuit (T3a) is the one that
// fires in practice: a slot served by a successful fallback returns StatusOK, so
// the scenario this epic creates — primary fails, re-packed fallback succeeds — is
// an ALL-OK run that returns "fully covered" before the per-slot attribution loop
// (T3b) ever runs. Fixing only T3b is no fix at all.

// servingSlot builds a slot whose primary is tagged with primaryFiles and whose
// single fallback served a re-packed subset servedFiles.
func servingSlot(name string, primaryFiles, servedFiles []string) Slot {
	return Slot{
		Primary:   Agent{Name: name, chunkFiles: primaryFiles},
		Fallbacks: []Agent{{Name: name + "-backup", chunkFiles: servedFiles, rePacked: true}},
	}
}

// servedResult is the Result a re-packed fallback produces: StatusOK, attributed
// to the primary's name (persona attribution follows the slot), carrying the
// files IT actually reviewed.
func servedResult(name string, servedFiles []string, rePacked bool) Result {
	return Result{
		Agent:            name,
		Status:           StatusOK,
		FallbackUsed:     true,
		FallbackFrom:     name,
		servedChunkFiles: servedFiles,
		servedRePacked:   rePacked,
	}
}

// T3a — the arm that actually fires. Every slot succeeds, so the pre-epic
// short-circuit would return nil ("nothing excluded") and the shed files would be
// recorded as reviewed.
func TestUncoveredBaselineFiles_AllOKWithRePackedFallbackStillAttributes(t *testing.T) {
	t.Parallel()
	slots := []Slot{servingSlot("greta", []string{"a.go", "b.go", "c.go"}, []string{"a.go"})}
	results := []Result{servedResult("greta", []string{"a.go"}, true)}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2", "c.go": "h3"}

	got := uncoveredBaselineFiles(context.Background(), slots, results, reviewed)

	// Asserted by the COMPLEMENT, per AC2: the shed files must come back as
	// uncovered so the next scan re-reads them. A positive-only assertion would
	// pass even if the short-circuit silently vouched for everything.
	assert.Equal(t, map[string]struct{}{"b.go": {}, "c.go": {}}, got,
		"a re-packed fallback must vouch ONLY for the files it reviewed, even when every slot succeeded")
	assert.NotContains(t, got, "a.go", "the file the fallback really did review stays covered")
}

// T3b — the per-slot loop, reached when some other slot failed.
func TestUncoveredBaselineFiles_MixedRunAttributesToServingAgent(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		servingSlot("greta", []string{"a.go", "b.go", "c.go"}, []string{"a.go"}),
		{Primary: Agent{Name: "kai", chunkFiles: []string{"b.go"}}},
	}
	results := []Result{
		servedResult("greta", []string{"a.go"}, true),
		{Agent: "kai", Status: StatusFailed},
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2", "c.go": "h3"}

	got := uncoveredBaselineFiles(context.Background(), slots, results, reviewed)
	assert.Equal(t, map[string]struct{}{"b.go": {}, "c.go": {}}, got,
		"the mixed run must give the same answer as the all-OK run — same evidence, same attribution")
}

// AC4: a run with no re-pack anywhere must be byte-identical to the pre-epic
// build, INCLUDING still taking the allOK short-circuit rather than falling
// through to per-slot attribution.
func TestUncoveredBaselineFiles_NoRePackKeepsAllOKShortCircuit(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		{Primary: Agent{Name: "greta"}}, // untagged, as a chunked-strategy slot is
		{Primary: Agent{Name: "kai", chunkFiles: []string{"a.go"}}},
	}
	results := []Result{
		{Agent: "greta", Status: StatusOK},
		{Agent: "kai", Status: StatusOK},
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2"}

	got := uncoveredBaselineFiles(context.Background(), slots, results, reviewed)
	assert.Empty(t, got,
		"with no re-pack the all-succeeded inference still holds: an untagged slot must not start reporting files uncovered")
}

// A fallback that served WITHOUT re-packing is still attributed by its SERVED
// tag, not the primary's. The two are identical on every production path
// (buildFallbackAgent copies the primary's tag onto every fallback), so this
// hand-constructed divergence is the only shape that distinguishes the two arms
// of servedCoverage — reverting it to return s.Primary.chunkFiles must fail
// here, not pass.
func TestUncoveredBaselineFiles_NonRePackedFallbacksServedTagGoverns(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		{
			Primary:   Agent{Name: "greta", chunkFiles: []string{"a.go", "b.go"}},
			Fallbacks: []Agent{{Name: "kai", chunkFiles: []string{"a.go"}}},
		},
		{Primary: Agent{Name: "vera", chunkFiles: []string{"c.go"}}},
	}
	results := []Result{
		servedResult("greta", []string{"a.go"}, false),
		{Agent: "vera", Status: StatusFailed},
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2", "c.go": "h3"}

	got := uncoveredBaselineFiles(context.Background(), slots, results, reviewed)
	assert.Equal(t, map[string]struct{}{"b.go": {}, "c.go": {}}, got,
		"attribution follows the SERVED tag even without a re-pack — a served set naming only a.go cannot vouch for b.go")
}

// Nil polarity survives the change: a served result carrying NO tag vouches for
// nothing, exactly as an untagged primary does. The primary is deliberately
// TAGGED here so "vouches for nothing" is distinguishable from "vouches for the
// primary's nothing" — dropping the !r.servedRePacked half of servedCoverage's
// guard would fall back to the primary's tag and fail this test.
func TestUncoveredBaselineFiles_UntaggedServingAgentVouchesForNothing(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		{Primary: Agent{Name: "greta", chunkFiles: []string{"b.go"}}, Fallbacks: []Agent{{Name: "kai", rePacked: true}}},
		{Primary: Agent{Name: "vera", chunkFiles: []string{"a.go"}}},
	}
	results := []Result{
		servedResult("greta", nil, true),
		{Agent: "vera", Status: StatusFailed},
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2"}

	got := uncoveredBaselineFiles(context.Background(), slots, results, reviewed)
	assert.Equal(t, map[string]struct{}{"a.go": {}, "b.go": {}}, got,
		"an untagged serving agent must vouch for NOTHING, never for everything")
}

// The fall-back to the Primary's tag (for a Result built outside invokeSlot) must
// be unavailable to a re-packed server. This is the single assertion standing
// between the epic and the silent-skip bug it exists to prevent: if a re-packed
// result with no tag of its own could reach the primary's full tag, every shed
// file would be recorded as reviewed.
func TestUncoveredBaselineFiles_RePackedResultNeverFallsBackToPrimaryTag(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go", "b.go", "c.go"}}},
		{Primary: Agent{Name: "vera", chunkFiles: []string{"a.go"}}},
	}
	results := []Result{
		// Re-packed, but carrying no tag of its own — the shape that must NOT
		// inherit the primary's three files.
		{Agent: "greta", Status: StatusOK, FallbackUsed: true, servedRePacked: true},
		{Agent: "vera", Status: StatusFailed},
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2", "c.go": "h3"}

	got := uncoveredBaselineFiles(context.Background(), slots, results, reviewed)
	assert.Equal(t, map[string]struct{}{"a.go": {}, "b.go": {}, "c.go": {}}, got,
		"a re-packed server with no tag must vouch for NOTHING — never for its primary's full chunk")
}

// AC6 on the multi-chunk baseline path: the merge inherits its record from g[0],
// so a re-fit that happens on any chunk but the first would be invisible in
// status.json — the persona would present as a clean "chunk" review while a
// substitute reviewed strictly less than it was sent.
func TestMergeChunkResults_PromotesARePackedChunksDegradationRecord(t *testing.T) {
	t.Parallel()
	g := []Result{
		{Agent: "greta", Status: StatusOK, DegradationAction: degradationChunk, EffectiveBudget: 400000, Content: "c0"},
		{
			Agent: "greta", Status: StatusOK, Content: "c1",
			DegradationAction: degradationTruncate,
			EffectiveBudget:   71680,
			Truncation:        payload.Truncation{Truncated: true, FilesDropped: []string{"b.go", "a.go"}},
			servedRePacked:    true,
		},
		{
			Agent: "greta", Status: StatusOK, Content: "c2",
			DegradationAction: degradationTruncate,
			EffectiveBudget:   71680,
			Truncation:        payload.Truncation{Truncated: true, FilesDropped: []string{"c.go", "a.go"}},
			servedRePacked:    true,
		},
	}

	merged := mergeChunkResults(g, nil)
	require.Len(t, merged, 1)
	out := merged[0]

	assert.Equal(t, degradationTruncate, out.DegradationAction,
		"a re-fit on a later chunk must not be reported as an ordinary chunk split")
	assert.True(t, out.Truncation.Truncated)
	assert.Equal(t, []string{"a.go", "b.go", "c.go"}, out.Truncation.FilesDropped,
		"the persona's shed record is the deduplicated, sorted union across its re-packed chunks")
	assert.Equal(t, int64(71680), out.EffectiveBudget,
		"effective_budget must be the tightest budget any of this persona's payloads was sized to")
}

func TestMergeChunkResults_PromotesOverflowOverTruncate(t *testing.T) {
	t.Parallel()
	g := []Result{
		{Agent: "greta", Status: StatusOK, DegradationAction: degradationTruncate, EffectiveBudget: 71680, servedRePacked: true, Content: "c0"},
		{Agent: "greta", Status: StatusOK, DegradationAction: degradationOverflow, EffectiveBudget: 0, servedRePacked: true, Content: "c1"},
	}

	merged := mergeChunkResults(g, nil)
	require.Len(t, merged, 1)
	assert.Equal(t, degradationOverflow, merged[0].DegradationAction,
		"overflow is the only action saying the review may not have been read in full — it must survive the merge")
	assert.Equal(t, int64(0), merged[0].EffectiveBudget,
		"a re-packed member that genuinely ran at budget 0 is the tightest budget in the group — 0 is a recorded value, not an unknown")
}

// The min must not depend on iteration order: a zero budget wins whether it
// appears before or after a larger one.
func TestMergeChunkResults_ZeroBudgetWinsRegardlessOfOrder(t *testing.T) {
	t.Parallel()
	g := []Result{
		{Agent: "greta", Status: StatusOK, DegradationAction: degradationOverflow, EffectiveBudget: 0, servedRePacked: true, Content: "c0"},
		{Agent: "greta", Status: StatusOK, DegradationAction: degradationTruncate, EffectiveBudget: 71680, servedRePacked: true, Content: "c1"},
	}

	merged := mergeChunkResults(g, nil)
	require.Len(t, merged, 1)
	assert.Equal(t, degradationOverflow, merged[0].DegradationAction)
	assert.Equal(t, int64(0), merged[0].EffectiveBudget,
		"a genuine 0 seen FIRST must not be overwritten by a later larger budget")
}

// A promoted zero budget must not sit beside an inherited non-zero
// reserved_output_tokens: status.json omits both fields at 0, so leaving the
// reservation in place would render "no budget recorded" next to "output
// reserved" — two fields contradicting each other on the same record.
func TestMergeChunkResults_PromotedZeroBudgetKeepsFieldsConsistent(t *testing.T) {
	t.Parallel()
	g := []Result{
		{Agent: "greta", Status: StatusOK, DegradationAction: degradationChunk, EffectiveBudget: 400000, ReservedOutputTokens: 4096, Content: "c0"},
		{Agent: "greta", Status: StatusOK, DegradationAction: degradationOverflow, EffectiveBudget: 0, servedRePacked: true, Content: "c1"},
	}

	merged := mergeChunkResults(g, nil)
	require.Len(t, merged, 1)
	assert.Equal(t, int64(0), merged[0].EffectiveBudget)
	assert.Equal(t, 0, merged[0].ReservedOutputTokens,
		"reserved_output_tokens must not survive a promoted zero budget — the two fields must agree")
}

// model, resolved_window and reserved_output_tokens must stay ONE agent's
// record, and effective_budget must not drag them off it.
//
// invokeAgent stamps Model, ResolvedWindow and ReservedOutputTokens from the
// agent that served that chunk, in one block (engine.go) — so the three arrive
// as a matched set and `out := g[0]` inherits a matched set. resolved_window is
// a pure function of model (payload.ContextWindowTokens), which makes that pair
// checkable by anyone reading status.json.
//
// effective_budget is deliberately NOT part of that set on a merged record: the
// promotion documents it as the tightest budget any re-packed payload was sized
// to — an AGGREGATE across chunks, flagged by degradation_action and the
// truncation union beside it. Making the window follow the aggregate instead
// would trade a documented aggregate for an undocumented lie, pairing chunk 0's
// model with a different agent's window: a combination payload.ContextWindowTokens
// says is impossible, on the record a user actually reads.
func TestMergeChunkResults_PromotedBudgetDoesNotDragTheWindowOffItsModel(t *testing.T) {
	t.Parallel()
	g := []Result{
		{
			Agent: "greta", Status: StatusOK, Content: "c0",
			Model:                "primary-model",
			DegradationAction:    degradationChunk,
			EffectiveBudget:      400000,
			ResolvedWindow:       200000,
			ReservedOutputTokens: 8192,
		},
		{
			Agent: "greta", Status: StatusOK, Content: "c1",
			// A DIFFERENT serving agent: its own model, window and reservation.
			Model:                "backup-model",
			DegradationAction:    degradationTruncate,
			EffectiveBudget:      71680,
			ResolvedWindow:       32768,
			ReservedOutputTokens: 4096,
			Truncation:           payload.Truncation{Truncated: true, FilesDropped: []string{"a.go"}},
			servedRePacked:       true,
		},
	}

	merged := mergeChunkResults(g, nil)
	require.Len(t, merged, 1)
	out := merged[0]

	require.Equal(t, int64(71680), out.EffectiveBudget,
		"precondition: the aggregate budget still promotes — that is the epic's signal")
	require.Equal(t, "primary-model", out.Model,
		"precondition: the merged record keeps the persona identity it inherited")
	assert.Equal(t, 200000, out.ResolvedWindow,
		"resolved_window must still describe the model named on the SAME record — it is derivable from it")
	assert.Equal(t, 8192, out.ReservedOutputTokens,
		"the reservation belongs to that same record, not to whichever chunk won the budget min")
}

// Chunk-level serving identity must not survive the collapse into one persona
// record: inheriting chunk 0's tag would name only its files as if they were
// the persona's reviewed set, beside a degradation record possibly lifted from
// a DIFFERENT chunk. Coverage attribution reads RAW results pre-merge (the
// uncoveredBaselineFiles PRECONDITION), so the merged Result vouches for
// nothing.
func TestMergeChunkResults_ServingIdentityDoesNotSurviveTheMerge(t *testing.T) {
	t.Parallel()
	g := []Result{
		{Agent: "greta", Status: StatusOK, Content: "c0", servedChunkFiles: []string{"a.go"}, servedRePacked: true},
		{Agent: "greta", Status: StatusOK, Content: "c1", servedChunkFiles: []string{"b.go", "c.go"}, servedRePacked: true},
	}

	merged := mergeChunkResults(g, nil)
	require.Len(t, merged, 1)
	assert.Nil(t, merged[0].servedChunkFiles,
		"the merged persona record must not inherit chunk 0's tag as if it were the persona's reviewed set")
	assert.False(t, merged[0].servedRePacked,
		"the merged persona record must not inherit chunk 0's re-pack flag")
}

// AC4: with no re-pack anywhere the promotion is inert and the merged record is
// exactly the pre-epic one (g[0]'s).
func TestMergeChunkResults_NoRePackLeavesTheMergedRecordUntouched(t *testing.T) {
	t.Parallel()
	g := []Result{
		{Agent: "greta", Status: StatusOK, DegradationAction: degradationChunk, EffectiveBudget: 400000, Content: "c0"},
		{
			Agent: "greta", Status: StatusOK, Content: "c1",
			DegradationAction: degradationTruncate,
			EffectiveBudget:   1234,
			Truncation:        payload.Truncation{Truncated: true, FilesDropped: []string{"z.go"}},
		},
	}

	merged := mergeChunkResults(g, nil)
	require.Len(t, merged, 1)
	assert.Equal(t, degradationChunk, merged[0].DegradationAction,
		"without a re-pack the pre-epic g[0] inheritance must stand, byte-identical")
	assert.Equal(t, int64(400000), merged[0].EffectiveBudget)
	assert.False(t, merged[0].Truncation.Truncated)
}

// The stamping half, through the real engine: invokeSlot must record the SERVING
// chain member's tag on the Result. Without it every assertion above is testing a
// field production never populates.
func TestInvokeSlot_StampsServingAgentCoverageTag(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.failFor["primary-model"] = errors.New("boom")

	slot := Slot{
		Primary: Agent{
			Name:        "greta",
			Invocation:  llmclient.Invocation{Model: "primary-model"},
			PayloadMode: "blocks",
			chunkFiles:  []string{"a.go", "b.go", "c.go"},
		},
		Fallbacks: []Agent{{
			Name:        "kai",
			Invocation:  llmclient.Invocation{Model: "backup-model"},
			PayloadMode: "blocks",
			chunkFiles:  []string{"a.go"},
			rePacked:    true,
		}},
	}

	results := NewEngine(f).Run(context.Background(), []Slot{slot})
	require.Len(t, results, 1)
	r := results[0]

	require.Equal(t, StatusOK, r.Status, "precondition: the fallback must serve the slot successfully")
	assert.Equal(t, "greta", r.Agent, "persona attribution still follows the slot, not the substitute")
	assert.True(t, r.FallbackUsed)
	assert.Equal(t, []string{"a.go"}, r.servedChunkFiles,
		"the served tag must be the FALLBACK's re-packed set, not the primary's full chunk")
	assert.True(t, r.servedRePacked, "a re-packed serving agent must be flagged so allOK can decline to short-circuit")
}

// AC2 end-to-end, composing the real pieces rather than hand-built Results:
// buildSlots re-fits the fallback (T2), Engine.Run stamps the served tag (T3), and
// uncoveredBaselineFiles reads it. The primary fails and the re-packed fallback
// succeeds, so EVERY slot is StatusOK — the allOK path, which is the one that
// fires in production and the one a mixed pass/fail test would never exercise.
//
// The two halves are pinned separately above; this pins that they compose. A
// plumbing gap between them (a served tag stamped but never read, or read from an
// agent that never re-packed) would leave both unit tests green while production
// silently recorded the shed files as reviewed.
func TestBaselineRun_RePackedFallbackRecordsOnlyTheFilesItReviewed(t *testing.T) {
	cfg := refitRoster(t, 512000, OverflowTruncate)
	payloads := markedOversizedPayload()

	var slots []Slot
	var err error
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "blocks", "", true, true)
	})
	require.NoError(t, err)
	require.Len(t, slots, 1, "precondition: greta's large window holds the payload in one baseline slot")
	require.Len(t, slots[0].Fallbacks, 1)

	fb := slots[0].Fallbacks[0]
	require.True(t, fb.rePacked, "precondition: the backup's budget must force a re-fit, or this asserts nothing")
	require.Greater(t, len(slots[0].Primary.chunkFiles), len(fb.chunkFiles),
		"precondition: the re-fit must shed files, so the two tags genuinely differ")

	// The primary's model fails; the re-packed fallback answers.
	f := newFake()
	f.failFor["unlisted-small-model"] = errors.New("boom")
	results := NewEngine(f).Run(context.Background(), slots)
	require.Len(t, results, 1)
	require.Equal(t, StatusOK, results[0].Status,
		"precondition: this must be an ALL-OK run — that is the path under test")

	reviewed := map[string]string{}
	for _, e := range payloads["blocks"].Entries {
		reviewed[e.Path] = "hash-" + e.Path
	}

	got := uncoveredBaselineFiles(context.Background(), slots, results, reviewed)

	// The complement, per AC2: every file the fallback shed must come back
	// uncovered so the next scan re-reads it.
	kept := map[string]struct{}{}
	for _, p := range fb.chunkFiles {
		kept[p] = struct{}{}
	}
	require.NotEmpty(t, kept)
	assert.Len(t, got, len(reviewed)-len(kept),
		"exactly the shed files must be reported uncovered")
	for p := range kept {
		assert.NotContains(t, got, p, "the file the fallback really reviewed must stay covered")
	}
	for p := range reviewed {
		if _, ok := kept[p]; !ok {
			assert.Contains(t, got, p,
				"shed file %q must be re-scanned, not silently recorded as reviewed", p)
		}
	}
}

// A chain deeper than one fallback: each member is built independently by
// buildFallbackAgent, so member 3 may carry its own re-packed tag, and only the
// member that SERVED may be stamped. Fallback 1 re-packs and fails; fallback 2
// re-packs to a different set and succeeds — the stamp must be fallback 2's,
// never fallback 1's or the primary's.
func TestInvokeSlot_TwoFallbackChainStampsTheActualServer(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.failFor["primary-model"] = errors.New("boom")
	f.failFor["backup-model-1"] = errors.New("boom")

	slot := Slot{
		Primary: Agent{
			Name:        "greta",
			Invocation:  llmclient.Invocation{Model: "primary-model"},
			PayloadMode: "blocks",
			chunkFiles:  []string{"a.go", "b.go", "c.go"},
		},
		Fallbacks: []Agent{
			{
				Name:        "kai",
				Invocation:  llmclient.Invocation{Model: "backup-model-1"},
				PayloadMode: "blocks",
				chunkFiles:  []string{"a.go"},
				rePacked:    true,
			},
			{
				Name:        "brad",
				Invocation:  llmclient.Invocation{Model: "backup-model-2"},
				PayloadMode: "blocks",
				chunkFiles:  []string{"b.go", "c.go"},
				rePacked:    true,
			},
		},
	}

	results := NewEngine(f).Run(context.Background(), []Slot{slot})
	require.Len(t, results, 1)
	r := results[0]

	require.Equal(t, StatusOK, r.Status, "precondition: fallback 2 must serve the slot successfully")
	assert.Equal(t, []string{"b.go", "c.go"}, r.servedChunkFiles,
		"one slot, one result, one server: the stamped tag must be fallback 2's, not fallback 1's or the primary's")
	assert.True(t, r.servedRePacked)
}

// The re-pack flag is DECLARED, not derived: nothing couples rePacked to "this
// agent ships something other than what its primary shipped". A future fallback
// arm that ships its own payload but forgets the flag must not silently pass
// servedCoverage's guard — invokeSlot promotes the divergence to re-packed
// (fail-open: the differing tag governs, and allOK declines to short-circuit)
// and makes it loud.
func TestInvokeSlot_TagDivergenceWithoutRePackFlagIsPromotedAndLoud(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.failFor["primary-model"] = errors.New("boom")

	slot := Slot{
		Primary: Agent{
			Name:        "greta",
			Invocation:  llmclient.Invocation{Model: "primary-model"},
			PayloadMode: "blocks",
			chunkFiles:  []string{"a.go", "b.go", "c.go"},
		},
		Fallbacks: []Agent{{
			Name:        "kai",
			Invocation:  llmclient.Invocation{Model: "backup-model"},
			PayloadMode: "blocks",
			// A DIFFERENT tag but no re-pack flag — the divergence shape a
			// forgotten field assignment would produce.
			chunkFiles: []string{"a.go"},
			rePacked:   false,
		}},
	}

	results := NewEngine(f).Run(context.Background(), []Slot{slot})
	require.Len(t, results, 1)
	r := results[0]

	require.Equal(t, StatusOK, r.Status, "precondition: the fallback must serve the slot successfully")
	assert.Equal(t, []string{"a.go"}, r.servedChunkFiles,
		"the honest served tag still governs attribution")
	assert.True(t, r.servedRePacked,
		"a served tag differing from the primary's without the re-pack flag must be promoted to re-packed — the guard's assumption is broken, so fail open")
}

func TestInvokeSlot_PrimaryServedStampsItsOwnTag(t *testing.T) {
	t.Parallel()
	slot := Slot{
		Primary: Agent{
			Name:        "greta",
			Invocation:  llmclient.Invocation{Model: "primary-model"},
			PayloadMode: "blocks",
			chunkFiles:  []string{"a.go", "b.go"},
		},
		Fallbacks: []Agent{{Name: "kai", Invocation: llmclient.Invocation{Model: "backup-model"}, chunkFiles: []string{"a.go"}, rePacked: true}},
	}

	results := NewEngine(newFake()).Run(context.Background(), []Slot{slot})
	require.Len(t, results, 1)

	assert.Equal(t, []string{"a.go", "b.go"}, results[0].servedChunkFiles,
		"the primary served, so its own tag is the served one — the unused fallback's re-pack must not leak in")
	assert.False(t, results[0].servedRePacked,
		"no re-pack happened, so the allOK short-circuit must stay available")
}

// A re-packed chunk 0 carries ChunkCount 1 (its own re-fit record), so the
// merge inheriting g[0] wholesale would report a split of 1 for a persona that
// really shipped 3 chunks — and status.json is written from this merged record.
// chunk_count describes the persona's split, so it must be restored.
func TestMergeChunkResults_RePackedChunkZeroKeepsThePersonasSplit(t *testing.T) {
	t.Parallel()
	g := []Result{
		{Agent: "greta", Status: StatusOK, Content: "c0",
			DegradationAction: degradationTruncate, EffectiveBudget: 71680, ChunkCount: 1, servedRePacked: true},
		{Agent: "greta", Status: StatusOK, DegradationAction: degradationChunk, EffectiveBudget: 400000, ChunkCount: 3, Content: "c1"},
		{Agent: "greta", Status: StatusOK, DegradationAction: degradationChunk, EffectiveBudget: 400000, ChunkCount: 3, Content: "c2"},
	}

	merged := mergeChunkResults(g, nil)
	require.Len(t, merged, 1)
	assert.Equal(t, 3, merged[0].ChunkCount,
		"chunk_count describes the persona's split — a re-packing chunk 0 must not under-report it")
}

// The primary-tag fall-back arm is dead in production (invokeSlot stamps every
// StatusOK result, and results synthesized elsewhere are never StatusOK), so it
// is the canary for a plumbing regression in the stamping seam: if it ever fires
// for real, the stamp was lost. It must log when taken, and stay silent on the
// normal served-tag path.
func TestServedCoverage_PrimaryFallbackArmLogs(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	slot := Slot{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go"}}}

	got := servedCoverage(ctx, slot, Result{Status: StatusOK})
	assert.Equal(t, []string{"a.go"}, got, "the fall-back returns the primary's tag")
	assert.Contains(t, buf.String(), "primary",
		"the primary-tag fall-back must log — any production firing means the stamping seam regressed")

	buf.Reset()
	got = servedCoverage(ctx, slot, Result{Status: StatusOK, servedChunkFiles: []string{"b.go"}})
	assert.Equal(t, []string{"b.go"}, got)
	assert.Empty(t, buf.String(), "the served-tag arm is the normal path and must not log")
}

// The re-pack FLAG must be stamped by invokeSlot in its own right, not merely
// arrived at by the divergence backstop.
//
// TestInvokeSlot_StampsServingAgentCoverageTag above cannot tell the two apart:
// its re-packed fallback carries a SMALLER tag than its primary, so deleting
// `r.servedRePacked = a.rePacked` still yields servedRePacked true — the
// divergence promotion (engine.go) sets it, loudly. The suite stayed green with
// the stamp removed, which means the stamp itself was unprotected and only the
// fail-open backstop was under test.
//
// The discriminating fixture is a re-packed fallback whose tag EQUALS its
// primary's. Nothing diverges, so the backstop cannot fire and only the stamp can
// make the flag true. Asserting the warning stays silent is the other half: it
// proves the flag arrived by the honest route rather than by fail-open, and keeps
// the two mechanisms separable if either is changed later.
//
// The fixture is legal, not contrived: a re-fit that sheds nothing returns
// ok=false and never sets rePacked, but nothing in the Agent contract couples the
// flag to a smaller tag, and the divergence promotion exists precisely because
// the flag is DECLARED rather than derived.
func TestInvokeSlot_StampsRePackFlagWithoutRelyingOnTagDivergence(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	f := newFake()
	f.failFor["primary-model"] = errors.New("boom")

	tag := []string{"a.go", "b.go", "c.go"}
	slot := Slot{
		Primary: Agent{
			Name:        "greta",
			Invocation:  llmclient.Invocation{Model: "primary-model"},
			PayloadMode: "blocks",
			chunkFiles:  tag,
		},
		Fallbacks: []Agent{{
			Name:        "kai",
			Invocation:  llmclient.Invocation{Model: "backup-model"},
			PayloadMode: "blocks",
			// Same tag as the primary — so the divergence backstop cannot fire.
			chunkFiles: []string{"a.go", "b.go", "c.go"},
			rePacked:   true,
		}},
	}

	results := NewEngine(f).Run(ctx, []Slot{slot})
	require.Len(t, results, 1)
	r := results[0]

	require.Equal(t, StatusOK, r.Status, "precondition: the fallback must serve the slot successfully")
	require.Equal(t, tag, r.servedChunkFiles,
		"precondition: the served tag must MATCH the primary's, or the backstop fires and this asserts nothing")
	assert.True(t, r.servedRePacked,
		"the re-pack flag must be stamped from the serving agent, not inferred from a differing tag")
	assert.NotContains(t, buf.String(), "promoting to re-packed",
		"the divergence backstop must stay silent here — a warning means the flag arrived fail-open rather than by the stamp")
}

// A persona can hit BOTH degradations at once: the diff-wide byte-budget shed
// (decided upstream in buildPayloads, carried on DiffTruncation) and a fallback that
// re-fit one of its chunks. promoteRePackedDegradation writes out.Truncation, and it
// runs BEFORE promoteDiffTruncation — which is deliberately a fallback and so declines
// to fill an already-populated record. The diff-wide file list was therefore masked:
// truncated=true stayed honest, but files_dropped named only the re-pack's files and
// silently omitted the files no reviewer ever saw.
//
// The persona's record has to name ALL of them, which is the same argument the
// existing re-pack union already makes across chunks — extended one level out.
func TestMergeChunkResults_UnionsTheDiffWideShedWithARePackRecord(t *testing.T) {
	t.Parallel()
	diffWide := payload.Truncation{Truncated: true, FilesDropped: []string{"shed_by_budget.go", "a.go"}}
	g := []Result{
		{
			Agent: "greta", Status: StatusOK, Content: "c0",
			DegradationAction: degradationChunk, EffectiveBudget: 400000,
			DiffTruncation: diffWide,
		},
		{
			Agent: "greta", Status: StatusOK, Content: "c1",
			DegradationAction: degradationTruncate, EffectiveBudget: 71680,
			Truncation:     payload.Truncation{Truncated: true, FilesDropped: []string{"b.go", "a.go"}},
			DiffTruncation: diffWide,
			servedRePacked: true,
		},
	}

	merged := mergeChunkResults(g, nil)
	require.Len(t, merged, 1)
	out := merged[0]

	assert.True(t, out.Truncation.Truncated)
	assert.Equal(t, []string{"a.go", "b.go", "shed_by_budget.go"}, out.Truncation.FilesDropped,
		"files_dropped must be the deduplicated, sorted union of the re-pack's shed AND the diff-wide "+
			"shed — a file dropped by the global byte budget is exactly the one no reviewer saw")
}

// The merged record's promoted budget is an AGGREGATE across chunks, and when it promotes
// a ZERO the reservation is force-zeroed alongside it so the two cannot contradict each
// other (promoteRePackedDegradation). ResolvedMaxTokens has to follow the same rule.
//
// Left inherited, it publishes CHUNK 0's cap — the funded primary's — beside a zero
// reservation caused by a DIFFERENT agent's window, and the field's documented reading
// rule ("a cap here with reserved_output_tokens absent means the window could not fund it,
// and this is the number to lower") then names a number that closed nothing. That is the
// exact inconsistency the reservation-zeroing was written to prevent.
func TestMergeChunkResults_PromotedZeroBudgetDoesNotLeaveAnInheritedCapBehind(t *testing.T) {
	t.Parallel()
	g := []Result{
		{
			Agent: "greta", Status: StatusOK, Content: "c0",
			Model:                "primary-model",
			DegradationAction:    degradationChunk,
			EffectiveBudget:      400000,
			ResolvedWindow:       200000,
			ReservedOutputTokens: 8192,
			ResolvedMaxTokens:    8192,
		},
		{
			Agent: "greta", Status: StatusOK, Content: "c1",
			// A different serving agent whose window funds nothing at all.
			Model:                "backup-model",
			DegradationAction:    degradationTruncate,
			EffectiveBudget:      0,
			ResolvedWindow:       1,
			ReservedOutputTokens: 0,
			ResolvedMaxTokens:    4096,
			Truncation:           payload.Truncation{Truncated: true, FilesDropped: []string{"a.go"}},
			servedRePacked:       true,
		},
	}

	merged := mergeChunkResults(g, nil)
	require.Len(t, merged, 1)
	out := merged[0]

	require.Zero(t, out.EffectiveBudget, "precondition: the zero budget is what promotes")
	require.Zero(t, out.ReservedOutputTokens,
		"precondition: the reservation is force-zeroed beside a promoted zero budget")

	assert.Zero(t, out.ResolvedMaxTokens,
		"the cap must be zeroed with its reservation: chunk 0's 8192 did not close this "+
			"budget, and publishing it beside an absent reservation names the wrong number "+
			"as the one to lower")
}

// The ordinary case must keep its cap: a non-zero promoted budget leaves the reservation
// alone, so the cap stays with it and the two still read as a matched pair.
func TestMergeChunkResults_PromotedNonZeroBudgetKeepsTheInheritedCap(t *testing.T) {
	t.Parallel()
	g := []Result{
		{
			Agent: "greta", Status: StatusOK, Content: "c0",
			Model: "primary-model", DegradationAction: degradationChunk,
			EffectiveBudget: 400000, ResolvedWindow: 200000,
			ReservedOutputTokens: 8192, ResolvedMaxTokens: 8192,
		},
		{
			Agent: "greta", Status: StatusOK, Content: "c1",
			Model: "backup-model", DegradationAction: degradationTruncate,
			EffectiveBudget: 71680, ResolvedWindow: 32768,
			ReservedOutputTokens: 4096, ResolvedMaxTokens: 4096,
			Truncation:     payload.Truncation{Truncated: true, FilesDropped: []string{"a.go"}},
			servedRePacked: true,
		},
	}

	merged := mergeChunkResults(g, nil)
	require.Len(t, merged, 1)
	out := merged[0]

	require.Equal(t, int64(71680), out.EffectiveBudget, "precondition: a non-zero budget promotes")
	assert.Equal(t, 8192, out.ResolvedMaxTokens,
		"the cap belongs to the model named on this record, like resolved_window beside it")
}
