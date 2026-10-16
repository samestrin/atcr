package fanout

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// overWindowConfig is one persona, greta, declaring a 20,000-token window, so
// one file of a few hundred KB can never fit it while ordinary files do.
func overWindowConfig() *ReviewConfig {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	win := 20000
	g := cfg.Registry.Agents["greta"]
	g.ContextWindowTokens = &win
	cfg.Registry.Agents["greta"] = g
	return cfg
}

// overWindowEntries is one file far larger than greta's window and two small
// ones, which PartitionByBudget plans as two bins: the big file alone, and the
// two small files together.
func overWindowEntries() []payload.FileEntry {
	return []payload.FileEntry{
		baselineEntry("data/rates.csv", 400_000),
		baselineEntry("a.go", 100),
		baselineEntry("b.go", 100),
	}
}

func overWindowSlots(t *testing.T) []Slot {
	t.Helper()
	slots, _, err := buildSlots(overWindowConfig(), baselinePayloads(overWindowEntries()), ReviewRange{}, string(payload.ModeFiles), "", false, true)
	require.NoError(t, err)
	require.Len(t, slots, 2)
	return slots
}

func slotWithFile(t *testing.T, slots []Slot, path string) int {
	t.Helper()
	for i, s := range slots {
		for _, f := range s.Primary.chunkFiles {
			if f == path {
				return i
			}
		}
	}
	t.Fatalf("no slot carries %s", path)
	return -1
}

// Epic 35.16.14 fix AC1b: a bin holding one file larger than its reviewer's
// window is marked as such when the plan is built; a bin that fits is not.
func TestBaselineSlots_MarksOverWindowSingleton(t *testing.T) {
	slots := overWindowSlots(t)
	big := slotWithFile(t, slots, "data/rates.csv")
	small := slotWithFile(t, slots, "a.go")
	assert.True(t, slots[big].overWindow, "the lone over-window file's bin is marked")
	assert.False(t, slots[small].overWindow, "a bin that fits is not")
}

// The mark follows the reviewer's WINDOW, not the payload_byte_budget cap: a
// file larger than the cap but inside the window is still shipped whole, as
// its own bin, and a model can take it.
func TestBaselineSlots_OverPayloadCapInsideWindowIsNotOverWindow(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = 100
	entries := []payload.FileEntry{baselineEntry("big.go", 500), baselineEntry("a.go", 100)}
	slots, _, err := buildSlots(cfg, baselinePayloads(entries), ReviewRange{}, string(payload.ModeFiles), "", false, true)
	require.NoError(t, err)
	require.Len(t, slots, 2)
	for i, s := range slots {
		assert.False(t, s.overWindow, "slot %d fits greta's window", i)
	}
}

// The plan says so on stderr, naming the persona, the file, its size and the
// budget it is over, so an unreviewed bin never needs explaining after the fact.
func TestBaselineSlots_OverWindowWarnsAtPlanTime(t *testing.T) {
	out := captureStderr(t, func() {
		_, _, err := buildSlots(overWindowConfig(), baselinePayloads(overWindowEntries()), ReviewRange{}, string(payload.ModeFiles), "", true, true)
		require.NoError(t, err)
	})
	assert.Contains(t, out, `agent "greta"`)
	assert.Contains(t, out, "data/rates.csv")
	assert.Contains(t, out, "larger than its context window")
	assert.Contains(t, out, "not retried")
}

// The ledger stamps over_window on that bin in chunks.json, and the outer retry
// does not cover it; every other bin keeps both as before.
func TestChunkLedger_OverWindowBinIsStampedAndNotRetryable(t *testing.T) {
	slots := overWindowSlots(t)
	big := slotWithFile(t, slots, "data/rates.csv")
	small := slotWithFile(t, slots, "a.go")
	poolDir := t.TempDir()
	l := newChunkLedger(slog.New(slog.DiscardHandler), poolDir, slots)
	require.NotNil(t, l)
	l.writePlan()

	assert.False(t, l.retryable(big), "an over-window bin is never requeued")
	assert.True(t, l.retryable(small), "a bin that fits keeps the outer retry")
	assert.True(t, l.recorded(big), "it is still a recorded bin")

	raw, err := os.ReadFile(filepath.Join(poolDir, poolRawAgentDir, "greta", chunksFile))
	require.NoError(t, err)
	var rec chunkRecord
	require.NoError(t, json.Unmarshal(raw, &rec))
	for _, b := range rec.Bins {
		if b.Files[0] == "data/rates.csv" {
			assert.True(t, b.OverWindow)
		} else {
			assert.False(t, b.OverWindow)
		}
	}
	assert.Equal(t, 1, strings.Count(string(raw), `"over_window"`), "omitempty on every other bin")
}

// timesOut answers every call with a timeout and counts calls per file marker.
type timesOut struct {
	mu    sync.Mutex
	calls map[string]int
}

func (c *timesOut) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, f := range []string{"data/rates.csv", "a.go"} {
		if strings.Contains(inv.Prompt, fmt.Sprintf("// FILE:%s\n", f)) {
			c.calls[f]++
		}
	}
	return "", fmt.Errorf("call: %w", context.DeadlineExceeded)
}

// Wired the way runEngine wires it, an over-window bin that times out is tried
// once and resolves failed, while a bin that fits is requeued up to the budget.
func TestUnitRetry_OverWindowBinIsNotRequeued(t *testing.T) {
	slots := overWindowSlots(t)
	big := slotWithFile(t, slots, "data/rates.csv")
	small := slotWithFile(t, slots, "a.go")
	fake := &timesOut{calls: map[string]int{}}
	results, _ := ledgerEngine(t, fake, slots, true, withUnitClock(newFakeUnitClock()))

	assert.Equal(t, 1, fake.calls["data/rates.csv"], "never requeued")
	assert.Zero(t, results[big].Requeues)
	assert.Equal(t, unitMaxAttempts, fake.calls["a.go"], "the bin that fits keeps its budget")
	assert.Equal(t, StatusFailed, results[small].Status)
}

// Coverage counts an unreviewed over-window bin apart, so the coverage line can
// say why it was never reviewed. A reviewed one (a fallback with a bigger window
// served it) is not counted.
func TestReadCoverage_CountsUnreviewedOverWindowBins(t *testing.T) {
	dir := t.TempDir()
	writeCoverageRecord(t, dir, "greta", []chunkRecordBin{
		{Index: 0, Files: []string{"big.csv"}, Outcome: StatusFailed, OverWindow: true},
		{Index: 1, Files: []string{"huge.json"}, Outcome: StatusOK, OverWindow: true},
		{Index: 2, Files: []string{"a.go"}, Outcome: StatusFailed},
	})

	cov, ok, err := ReadCoverage(dir)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 1, cov.BinsOverWindow)
	assert.Equal(t, 1, cov.BinsReviewed)
	assert.Equal(t, 3, cov.BinsPlanned)
}
