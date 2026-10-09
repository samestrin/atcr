package fanout

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A salvaged reply is StatusOK with content — the client promoted the model's
// reasoning into it because the provider returned none — and since T6 every lane
// REFUSES it, so parseFindings returns nothing for it by construction. Nothing in
// uncoveredBaselineFiles saw that: the slot passed the all-OK short-circuit and,
// failing that, the per-slot pass accepted it as a successful server. Either way
// its files were recorded in the persistent baseline index as reviewed, and the
// next incremental scan skipped them — verbatim the "a file nothing ever read,
// never read again" failure the per-slot attribution exists to prevent.
func TestUncoveredBaselineFiles_SalvagedSlotCoversNothing(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go", "b.go"}}},
		{Primary: Agent{Name: "greta", chunkFiles: []string{"c.go"}}},
	}
	results := []Result{
		{Agent: "greta", Status: StatusOK, servedChunkFiles: []string{"a.go", "b.go"}},
		{Agent: "greta", Status: StatusOK, Salvaged: true, servedChunkFiles: []string{"c.go"}},
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2", "c.go": "h3"}

	got := uncoveredBaselineFiles(context.Background(), slots, results, reviewed)
	assert.Equal(t, map[string]struct{}{"c.go": {}}, got,
		"a salvaged slot contributed no findings, so its files were read by nobody")
}

// The same hole, reached through the OTHER door: when every slot is salvaged the
// run is still all-OK and not re-packed, so the short-circuit returned nil and the
// per-slot pass never ran at all.
func TestUncoveredBaselineFiles_AllSalvagedRunIsNotAFullyCoveredRun(t *testing.T) {
	t.Parallel()
	slots := []Slot{{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go", "b.go"}}}}
	results := []Result{{Agent: "greta", Status: StatusOK, Salvaged: true, servedChunkFiles: []string{"a.go", "b.go"}}}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2"}

	got := uncoveredBaselineFiles(context.Background(), slots, results, reviewed)
	assert.Equal(t, map[string]struct{}{"a.go": {}, "b.go": {}}, got,
		"an all-salvaged run reviewed nothing — the all-OK short-circuit must not call it covered")
}

// A reply the strip consumed entirely is the same fact by a different route: it
// parses to nothing, provably, and it is also StatusOK.
func TestUncoveredBaselineFiles_ThinkSuppressedSlotCoversNothing(t *testing.T) {
	t.Parallel()
	slots := []Slot{{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go"}}}}
	results := []Result{{Agent: "greta", Status: StatusOK, ThinkSuppressed: true, servedChunkFiles: []string{"a.go"}}}
	reviewed := map[string]string{"a.go": "h1"}

	got := uncoveredBaselineFiles(context.Background(), slots, results, reviewed)
	assert.Equal(t, map[string]struct{}{"a.go": {}}, got,
		"the whole reply was reasoning — nothing read this file")
}

// And the ordinary clean run must stay on the cheap path: the short-circuit is a
// real optimisation, so the fix must not disable it for every run.
func TestUncoveredBaselineFiles_CleanRunStillShortCircuits(t *testing.T) {
	t.Parallel()
	slots := []Slot{{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go"}}}}
	results := []Result{{Agent: "greta", Status: StatusOK, servedChunkFiles: []string{"a.go"}}}
	reviewed := map[string]string{"a.go": "h1"}

	assert.Nil(t, uncoveredBaselineFiles(context.Background(), slots, results, reviewed),
		"every slot succeeded and contributed — the whole payload was covered")
}

// A stop-reason salvage is a finished answer whose findings parseFindings keeps, so
// its files WERE read: contributedNothing must be false for it, while a truncated
// salvage of the same slot stays a refusal (AC5).
func TestContributedNothing_StopReasonSalvageContributes(t *testing.T) {
	t.Parallel()
	onStop := Result{Agent: "greta", Status: StatusOK, Salvaged: true, SalvagedOnStop: true}
	assert.False(t, contributedNothing(onStop),
		"the model finished its answer on the reasoning channel; its findings shipped")

	truncated := Result{Agent: "greta", Status: StatusOK, Salvaged: true}
	assert.True(t, contributedNothing(truncated),
		"an abandoned draft is still refused, so its files were read by nobody")

	slots := []Slot{{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go"}}}}
	onStop.servedChunkFiles = []string{"a.go"}
	assert.Nil(t, uncoveredBaselineFiles(context.Background(), slots, []Result{onStop}, map[string]string{"a.go": "h1"}),
		"a stop-reason salvage covered its payload")
}
