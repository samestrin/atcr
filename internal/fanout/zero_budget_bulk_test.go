package fanout

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.5.1 TD (19.10 TD-002 territory): a declaration at or below
// defaultMaxTokens + promptOverheadTokens (12288) drives EffectiveByteBudget to
// 0. The bulk path then SKIPPED shedding entirely and shipped the FULL
// global-budget payload — so honestly declaring a small window sent MORE bytes
// than leaving the agent undeclared, the exact inverse of the Conservatism NFR.
//
// The zero-budget arm is a genuine overflow, so it must be handled like one: the
// operator's on_overflow policy decides fail/fallback pre-dispatch, and the
// default path ships the smallest viable payload rather than everything.

// zeroBudgetCfg gives greta a declaration inside the sub-overhead band, on a
// model absent from the static table so the window can only come from the
// declaration.
func zeroBudgetCfg(t *testing.T, onOverflow string) *ReviewConfig {
	t.Helper()
	cfg := declaredWindowRoster(t, 12288) // exactly defaultMaxTokens + promptOverheadTokens
	cfg.Settings.OnOverflow = onOverflow
	return cfg
}

// unevenBlocksPayload returns an oversized payload whose entries differ in size,
// with the smallest one carrying a marker so "which single file was kept" is
// observable in the rendered prompt.
func unevenBlocksPayload() map[string]modePayload {
	const smallestMarker = "SMALLEST-FILE-MARKER"
	var entries []payload.FileEntry
	var full strings.Builder
	for i := 0; i < 6; i++ {
		body := fmt.Sprintf("// file %d\n", i) + strings.Repeat("x", 40000*(i+1))
		entries = append(entries, payload.FileEntry{Path: fmt.Sprintf("f%d.go", i), Size: int64(len(body)), Body: body})
		full.WriteString(body)
	}
	// The smallest entry, added last so ordering cannot stand in for size.
	small := "// " + smallestMarker + "\n" + strings.Repeat("y", 100)
	entries = append(entries, payload.FileEntry{Path: "tiny.go", Size: int64(len(small)), Body: small})
	full.WriteString(small)

	return map[string]modePayload{
		"blocks": {Entries: entries, Text: full.String(), FileCount: len(entries)},
	}
}

func TestBuildSlots_ZeroBudgetShipsSmallestEntryNotWholePayload(t *testing.T) {
	cfg := zeroBudgetCfg(t, OverflowChunk)
	payloads := oversizedBlocksPayload()
	rng := ReviewRange{Base: "a", Head: "b"}

	agent, _, err := buildOneAgent(cfg, "greta", payloads, rng, "", "")
	require.NoError(t, err)
	require.Zero(t, agent.EffectiveBudget, "precondition: a 12288-token window funds no input budget")

	total := len(payloads["blocks"].Entries)
	assert.Len(t, agent.Truncation.FilesDropped, total-1,
		"a zero-budget agent must keep exactly one file — shipping all %d sends MORE than leaving it undeclared", total)
	assert.NotEmpty(t, agent.Prompt,
		"it must still ship something: an empty payload returns a false-clean review")
	assert.True(t, agent.Truncation.Truncated,
		"cutting the payload down must be recorded as truncation, not passed off as a clean fit")
}

func TestBuildSlots_ZeroBudgetShipsTheSmallestFile(t *testing.T) {
	// "Minimum viable" means the SMALLEST entry: picking an arbitrary one would
	// leave the degraded payload larger than it has to be.
	cfg := zeroBudgetCfg(t, OverflowChunk)
	agent, _, err := buildOneAgent(cfg, "greta", unevenBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)
	require.Zero(t, agent.EffectiveBudget)

	assert.Contains(t, agent.Prompt, "SMALLEST-FILE-MARKER",
		"the single shipped file must be the smallest entry")
	assert.NotContains(t, agent.Prompt, "// file 5\n",
		"the largest entry must not ride along")
}

func TestBuildSlots_ZeroBudgetRecordsOverflowDegradation(t *testing.T) {
	cfg := zeroBudgetCfg(t, OverflowChunk)
	agent, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)
	assert.Equal(t, degradationOverflow, agent.DegradationAction,
		"shipping a degraded payload must stay marked as overflow, not read as a clean fit")
}

func TestBuildSlots_ZeroBudgetHonorsOnOverflowFail(t *testing.T) {
	// The FIX's "or fails pre-dispatch" half: a zero budget is an overflow, so an
	// operator who asked to fail on overflow must not get a degraded dispatch.
	cfg := zeroBudgetCfg(t, OverflowFail)
	_, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrOverflowPolicyFail),
		"on_overflow=fail must hard-fail pre-dispatch on a zero budget, got: %v", err)
}

func TestBuildSlots_ZeroBudgetHonorsOnOverflowFallback(t *testing.T) {
	cfg := zeroBudgetCfg(t, OverflowFallback)
	_, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrFallbackUnavailable),
		"on_overflow=fallback must surface its typed error pre-dispatch on a zero budget, got: %v", err)
}

func TestBuildSlots_PositiveBudgetUnaffected(t *testing.T) {
	// The new arm must fire ONLY on a zero budget: an agent with a real budget
	// keeps shedding to fit rather than being cut down to one file.
	cfg := declaredWindowRoster(t, 128000)
	agent, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)
	require.Positive(t, agent.EffectiveBudget)
	// The 10 equal-sized files make WHICH ones survive an implementation detail of
	// the shed order; the invariant is that a funded agent is not cut to one file.
	assert.Less(t, len(agent.Truncation.FilesDropped), 9,
		"a funded agent must still receive a multi-file payload")
}
