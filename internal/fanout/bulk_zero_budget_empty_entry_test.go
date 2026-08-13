package fanout

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// zeroBudgetPayloadWithEmptyFile builds a bulk payload holding one real file and
// one EMPTY tracked file. Empty tracked files are ordinary in a baseline scan
// (py.typed, __init__.py, .gitkeep), and "fewest bytes" picks one every time.
func zeroBudgetPayloadWithEmptyFile() map[string]modePayload {
	body := "// real content\n" + strings.Repeat("x", 50000)
	entries := []payload.FileEntry{
		{Path: "big.go", Size: int64(len(body)), Body: body},
		{Path: "py.typed", Size: 0, Body: ""},
	}
	return map[string]modePayload{
		"blocks": {Entries: entries, Text: body, FileCount: len(entries)},
	}
}

// The zero-budget BULK arm must never ship a 0-byte payload.
//
// Epic 35.16.5.4 fixed this on the fallback re-fit path (keepSmallestEntry skips
// empty bodies), but the unchanged bulk arm at review.go:1933 still called
// smallestEntry, which picks purely by len(Body) and therefore selects a 0-byte
// file whenever the payload holds one. That dispatches an empty prompt, which
// comes back as a false-clean "no findings" review — the identical failure one
// function away from the one the epic closed.
//
// Asserted on the RENDERED payload, not just on the picked entry: what matters is
// the bytes that reach the model.
func TestBuildSlots_ZeroBudgetBulkArmNeverShipsAnEmptyPayload(t *testing.T) {
	require.Equal(t, int64(0), payload.EffectiveByteBudget("unlisted-small-model", ptrInt(1), defaultMaxTokens),
		"precondition: a 1-token declaration leaves no room for input after the output reservation")

	cfg := declaredWindowRoster(t, 1)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}

	var slots []Slot
	var err error
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, zeroBudgetPayloadWithEmptyFile(), ReviewRange{Base: "a", Head: "b"}, "blocks", "", true, true)
	})
	require.NoError(t, err)
	require.Len(t, slots, 1)

	p := slots[0].Primary
	require.Equal(t, []string{"big.go"}, p.chunkFiles,
		"the empty file must be skipped: keeping py.typed ships a 0-byte payload and a false-clean review")
	assert.Contains(t, p.Truncation.FilesDropped, "py.typed",
		"the skipped empty file is shed, and the record must say so")
	assert.Contains(t, p.Prompt, "// real content",
		"the dispatched prompt must carry the real file's bytes, not an empty payload")
}

// The all-empty payload has no non-empty entry to prefer, so the arm still ships
// one entry rather than nothing — there is no better option, and dropping to zero
// entries would trade a false-clean review for a broken one. Pins that the
// empty-skip degrades gracefully instead of erroring or emptying the slot.
func TestBuildSlots_ZeroBudgetBulkArmAllEntriesEmptyStillShipsOne(t *testing.T) {
	cfg := declaredWindowRoster(t, 1)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}

	payloads := map[string]modePayload{
		"blocks": {
			Entries: []payload.FileEntry{
				{Path: "a/py.typed", Size: 0, Body: ""},
				{Path: "b/__init__.py", Size: 0, Body: ""},
			},
			Text:      "",
			FileCount: 2,
		},
	}

	var slots []Slot
	var err error
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "blocks", "", true, true)
	})
	require.NoError(t, err)
	require.Len(t, slots, 1)
	assert.Len(t, slots[0].Primary.chunkFiles, 1,
		"every entry is empty, so one is still kept — the arm must not empty the slot")
}
