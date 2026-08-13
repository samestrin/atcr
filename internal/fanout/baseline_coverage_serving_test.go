package fanout

import (
	"context"
	"errors"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
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

// A fallback that served WITHOUT re-packing reviewed its primary's chunk, so the
// served tag equals the primary's and coverage is unchanged.
func TestUncoveredBaselineFiles_NonRePackedFallbackCoversPrimaryChunk(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		{
			Primary:   Agent{Name: "greta", chunkFiles: []string{"a.go", "b.go"}},
			Fallbacks: []Agent{{Name: "kai", chunkFiles: []string{"a.go", "b.go"}}},
		},
		{Primary: Agent{Name: "vera", chunkFiles: []string{"c.go"}}},
	}
	results := []Result{
		servedResult("greta", []string{"a.go", "b.go"}, false),
		{Agent: "vera", Status: StatusFailed},
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2", "c.go": "h3"}

	got := uncoveredBaselineFiles(context.Background(), slots, results, reviewed)
	assert.Equal(t, map[string]struct{}{"c.go": {}}, got,
		"a fallback that did not re-pack reviewed the primary's whole chunk and vouches for all of it")
}

// Nil polarity survives the change: a served result carrying NO tag vouches for
// nothing, exactly as an untagged primary does.
func TestUncoveredBaselineFiles_UntaggedServingAgentVouchesForNothing(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		{Primary: Agent{Name: "greta"}, Fallbacks: []Agent{{Name: "kai", rePacked: true}}},
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
