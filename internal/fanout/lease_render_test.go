package fanout

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// planPromptBytes is the rendered prompt text the plan itself holds: every
// slot's primary and fallback prompts, as the slice handed to Engine.Run.
func planPromptBytes(slots []Slot) int {
	n := 0
	for _, s := range slots {
		for _, a := range append([]Agent{s.Primary}, s.Fallbacks...) {
			n += len(a.Prompt) + len(a.Invocation.Prompt)
			for _, ref := range a.CodeContext {
				n += len(ref.Body)
			}
		}
	}
	return n
}

// residentPromptCompleter measures, at every call, the prompt bytes the plan
// still holds, and records which baseline files reached a call.
type residentPromptCompleter struct {
	slots []Slot

	mu         sync.Mutex
	maxPlan    int
	maxPrompt  int
	seenMarker map[string]bool
}

func (c *residentPromptCompleter) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	held := planPromptBytes(c.slots)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxPlan = max(c.maxPlan, held)
	c.maxPrompt = max(c.maxPrompt, len(inv.Prompt))
	for _, ln := range strings.Split(inv.Prompt, "\n") {
		if p, ok := strings.CutPrefix(ln, "// FILE:"); ok {
			c.seenMarker[p] = true
		}
	}
	return "", nil
}

// AC2 (Epic 35.16.14): peak payload memory follows max_parallel, not the number
// of planned units. A 40-bin baseline plan run two workers wide must not hold
// every bin's rendered prompt (primary and fallback) while a worker leases unit
// N; the plan may hold at most what max_parallel units in flight would, and
// every bin's content must still reach a call.
func TestBaselineUnits_ResidentPromptBytesTrackMaxParallel(t *testing.T) {
	const units, maxParallel = 40, 2
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = 100 // one 100-byte file per bin
	g := cfg.Registry.Agents["greta"]
	g.Fallback = "kai"
	cfg.Registry.Agents["greta"] = g

	entries := make([]payload.FileEntry, units)
	for i := range entries {
		entries[i] = baselineEntry(fmt.Sprintf("f%02d.go", i), 100)
	}
	slots, _, err := buildSlots(cfg, baselinePayloads(entries), ReviewRange{}, string(payload.ModeFiles), "", false, true)
	require.NoError(t, err)
	require.Len(t, slots, units, "one persona x 40 bins")

	c := &residentPromptCompleter{slots: slots, seenMarker: map[string]bool{}}
	res := NewEngine(c, WithMaxParallel(maxParallel)).Run(context.Background(), slots)
	require.Len(t, res, units)

	for _, e := range entries {
		assert.True(t, c.seenMarker[e.Path], "bin for %s never reached a call", e.Path)
	}
	require.Positive(t, c.maxPrompt)
	// A unit is a primary and one fallback, each counted as Prompt and
	// Invocation.Prompt: four prompt-sized strings at most per unit in flight.
	bound := maxParallel * 4 * c.maxPrompt
	assert.LessOrEqual(t, c.maxPlan, bound,
		"the plan held %d prompt bytes during the run; max_parallel=%d units in flight need at most %d", c.maxPlan, maxParallel, bound)
}

// renderedSlots returns each slot as a worker runs it: a baseline bin, which the
// plan holds without its prompts, rendered as at lease time.
func renderedSlots(t *testing.T, slots []Slot) []Slot {
	t.Helper()
	out := make([]Slot, len(slots))
	for i, s := range slots {
		r, err := s.leased()
		require.NoError(t, err, "slot %d: lease-time render", i)
		out[i] = r
	}
	return out
}

// AC7 (Epic 35.16.14): rendering at lease time changes when a unit's prompts
// exist, not what the unit is. Every field the plan keeps (identity, coverage
// tag, cache key, sizing, the fallback chain) matches the lease-time render, and
// two leases of one unit render the same prompts.
func TestBaselineUnits_LeaseRenderMatchesThePlan(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Settings.PayloadByteBudget = 100
	g := cfg.Registry.Agents["greta"]
	g.Fallback = "kai"
	cfg.Registry.Agents["greta"] = g

	entries := make([]payload.FileEntry, 6)
	for i := range entries {
		entries[i] = baselineEntry(fmt.Sprintf("f%02d.go", i), 100)
	}
	slots, _, err := buildSlots(cfg, baselinePayloads(entries), ReviewRange{}, string(payload.ModeFiles), "", false, true)
	require.NoError(t, err)
	require.Len(t, slots, 12, "two personas x six bins")

	for i, s := range slots {
		require.NotNil(t, s.render, "slot %d: a baseline bin renders at lease time", i)
		assert.Zero(t, planPromptBytes([]Slot{s}), "slot %d: the plan holds no prompt text", i)

		first, err := s.leased()
		require.NoError(t, err)
		again, err := s.leased()
		require.NoError(t, err)
		assert.Equal(t, first, again, "slot %d: two leases render the same unit", i)

		for _, p := range s.Primary.chunkFiles {
			assert.Contains(t, first.Primary.Prompt, "// FILE:"+p+"\n", "slot %d: the rendered primary ships its bin", i)
			for _, fb := range first.Fallbacks {
				assert.Contains(t, fb.Prompt, "// FILE:"+p+"\n", "slot %d: the rendered fallback ships the same bin", i)
			}
		}
		planned := s
		planned.render = nil
		assert.Equal(t, planned, first.deferRender(nil), "slot %d: the plan keeps exactly the lease-time unit, prompts aside", i)
	}
}

// A unit whose lease-time render fails resolves as a failed result under its
// own name; it neither panics the worker nor stalls the queue.
func TestEngineRun_LeaseRenderErrorFailsTheUnit(t *testing.T) {
	bad := Slot{Primary: Agent{Name: "greta", PayloadMode: "files"}}
	bad.render = func() (Slot, error) { return Slot{}, fmt.Errorf("persona vanished") }
	good := Slot{Primary: Agent{Name: "kai", Invocation: llmclient.Invocation{Prompt: "p"}}}

	res := NewEngine(&residentPromptCompleter{seenMarker: map[string]bool{}}, WithMaxParallel(1)).Run(context.Background(), []Slot{bad, good})
	require.Len(t, res, 2)
	assert.Equal(t, "greta", res[0].Agent)
	assert.Equal(t, StatusFailed, res[0].Status)
	assert.ErrorContains(t, res[0].Err, "persona vanished")
	assert.Equal(t, "files", res[0].PayloadMode)
	assert.Equal(t, StatusOK, res[1].Status)
}

// A lease-time render is silent. The prepare-time build of a unit already
// printed its warnings, so a worker re-rendering it must print none, and must not
// touch buildSlots' lane-warning dedup map, which only the single-threaded
// prepare-time build may write. The fixture makes both of the fallback's
// warnings fire at prepare: the AC4 "may overflow" line (kai's window is too
// small for greta's bins) and the "sets its own min_severity" line (kai declares
// one). Run two workers wide over several bins, so a lease that printed or wrote
// the map would show here, and as a data race under -race.
func TestBaselineUnits_LeaseRenderPrintsNoWarnings(t *testing.T) {
	const overflow, ownSeverity = "may overflow", "sets its own min_severity"
	cfg := refitRoster(t, 128000, OverflowChunk)
	kai := cfg.Registry.Agents["kai"]
	kai.MinSeverity = "HIGH"
	cfg.Registry.Agents["kai"] = kai

	var slots []Slot
	var err error
	prepared := captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, markedOversizedPayload(), ReviewRange{Base: "a", Head: "b"}, "", "", true, true)
	})
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: the payload partitions into several bins")
	for i, s := range slots {
		require.NotNil(t, s.render, "precondition: slot %d renders at lease time", i)
	}
	require.Equal(t, 1, strings.Count(prepared, overflow), "precondition: the lane's overflow warning prints once, at prepare")
	require.Equal(t, len(slots), strings.Count(prepared, ownSeverity), "precondition: the min_severity warning prints at prepare, once per bin")

	var res []Result
	ran := captureStderr(t, func() {
		res = NewEngine(&residentPromptCompleter{seenMarker: map[string]bool{}}, WithMaxParallel(2)).Run(context.Background(), slots)
	})
	require.Len(t, res, len(slots))
	assert.NotContains(t, ran, overflow, "a lease re-render must not repeat the overflow warning")
	assert.NotContains(t, ran, ownSeverity, "a lease re-render must not repeat the min_severity warning")
}
