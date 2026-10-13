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
