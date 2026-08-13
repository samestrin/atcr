package fanout

import (
	"fmt"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.5.4 T2: under on_overflow=truncate, a fallback whose OWN budget
// cannot hold the payload its primary was sized for re-packs that payload against
// its own budget instead of shipping a prompt it provably cannot hold.
//
// The pre-epic behavior — warn and ship — stays exactly as it was on every other
// arm: fail/fallback still hard-fail pre-dispatch, chunk and unset still
// warn-and-ship, and so does truncate itself whenever no re-pack source exists
// (the chunked-diff path). Those are pinned by fallback_overflow_policy_test.go,
// which passes unmodified.

// refitRoster wires greta (declaring `window` tokens, model absent from the static
// table) to fall back to kai on an unlisted model, so kai resolves the 32768
// default and its budget is genuinely too small for the payload greta was sized
// for. Only greta is rostered, so buildSlots yields exactly one slot.
func refitRoster(t *testing.T, window int, onOverflow string) *ReviewConfig {
	t.Helper()
	cfg := declaredWindowRoster(t, window)
	g := cfg.Registry.Agents["greta"]
	g.Fallback = "kai"
	cfg.Registry.Agents["greta"] = g
	kai := cfg.Registry.Agents["kai"]
	kai.Model = "unlisted-backup-model" // undeclared → 32768 default
	cfg.Registry.Agents["kai"] = kai
	cfg.Settings.OnOverflow = onOverflow
	cfg.Project.Agents = []string{"greta"}
	return cfg
}

// markedOversizedPayload is oversizedBlocksPayload with column-0 `=== FILE: `
// markers, so EntriesFromRenderedPayload recovers the per-file breakdown and both
// agents carry a populated CodeContext. That matters: CodeContext is the only
// measurement of what an agent was actually SENT, so without markers these tests
// could assert the re-fit's record but never that the bytes really shrank.
func markedOversizedPayload() map[string]modePayload {
	const fileBytes = 50000
	const nFiles = 10
	var entries []payload.FileEntry
	var full strings.Builder
	for i := 0; i < nFiles; i++ {
		body := fmt.Sprintf("=== FILE: f%d.go ===\n", i) + strings.Repeat("x", fileBytes) + "\n"
		entries = append(entries, payload.FileEntry{Path: fmt.Sprintf("f%d.go", i), Size: int64(len(body)), Body: body})
		full.WriteString(body)
	}
	return map[string]modePayload{
		"blocks": {Entries: entries, Text: full.String(), FileCount: nFiles},
	}
}

// wideDiffOfNFiles builds a diff whose lines are wide enough that a line-capped
// chunk still exceeds the backup's byte budget — diffOfNFiles' 6-byte lines would
// need ~12000 lines per chunk to get there.
func wideDiffOfNFiles(fileCount, bodyLines int) string {
	var b strings.Builder
	wide := "+" + strings.Repeat("w", 98) + "\n"
	for i := 0; i < fileCount; i++ {
		path := fmt.Sprintf("f%d.go", i)
		b.WriteString("diff --git a/" + path + " b/" + path + "\n")
		b.WriteString("--- a/" + path + "\n+++ b/" + path + "\n")
		fmt.Fprintf(&b, "@@ -1,%d +1,%d @@\n", bodyLines, bodyLines)
		for j := 0; j < bodyLines; j++ {
			b.WriteString(wide)
		}
	}
	return b.String()
}

// buildRefitSlot builds the single greta slot (with its kai fallback) over the
// oversized payload, through buildSlots — the only production path that supplies
// the re-pack context.
func buildRefitSlot(t *testing.T, cfg *ReviewConfig) Slot {
	t.Helper()
	var slots []Slot
	var err error
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, markedOversizedPayload(), ReviewRange{Base: "a", Head: "b"}, "", "", true)
	})
	require.NoError(t, err)
	require.Len(t, slots, 1)
	require.Len(t, slots[0].Fallbacks, 1, "precondition: greta must resolve exactly one fallback (kai)")
	return slots[0]
}

// codeContextBytes totals the per-file bodies an agent was actually sent — the
// quantity the effective byte budget governs.
func codeContextBytes(a Agent) int64 {
	var total int64
	for _, ref := range a.CodeContext {
		total += int64(len(ref.Body))
	}
	return total
}

func TestBuildFallbackAgent_TruncateRefitsPayloadToItsOwnBudget(t *testing.T) {
	cfg := refitRoster(t, 128000, OverflowTruncate)
	slot := buildRefitSlot(t, cfg)
	primary, fb := slot.Primary, slot.Fallbacks[0]

	fbBudget := payload.EffectiveByteBudget("unlisted-backup-model", nil, defaultMaxTokens)
	require.Greater(t, primary.EffectiveBudget, fbBudget,
		"precondition: the backup's budget must be smaller than the primary's, or nothing overflows")
	require.Greater(t, codeContextBytes(primary), fbBudget,
		"precondition: the payload the primary shipped must not fit the backup's budget")

	// AC1: the payload the fallback receives fits ITS budget, not its primary's.
	assert.LessOrEqual(t, codeContextBytes(fb), fbBudget,
		"the re-packed fallback's payload must fit its OWN effective byte budget")
	assert.Less(t, len(fb.Prompt), len(primary.Prompt),
		"a re-fit may only ever send FEWER bytes than the inherited prompt (Conservatism NFR)")

	// AC6: the record describes what was actually sent.
	assert.Equal(t, degradationTruncate, fb.DegradationAction,
		"a fallback that re-fit its payload records truncate, not the overflow it would have shipped")
	assert.True(t, fb.Truncation.Truncated, "the shed must be recorded, never silent")
	assert.NotEmpty(t, fb.Truncation.FilesDropped, "the fallback's own Truncation must name the files IT shed")
	assert.Equal(t, fbBudget, fb.EffectiveBudget,
		"effective_budget must be the budget the re-fit payload was actually sized to")

	// The shed set is the fallback's own, not the primary's inherited record.
	assert.NotEqual(t, primary.Truncation.FilesDropped, fb.Truncation.FilesDropped,
		"the fallback must not report its primary's shed list as its own")
	for _, dropped := range fb.Truncation.FilesDropped {
		for _, ref := range fb.CodeContext {
			assert.NotEqual(t, dropped, ref.Path, "a file recorded as dropped must not be in the payload sent")
		}
	}
}

func TestBuildFallbackAgent_RefitNeverShipsAnEmptyPayload(t *testing.T) {
	// AC5: when not even one file fits the fallback's budget, keep the single
	// smallest entry — an empty payload returns a false-clean "no findings" review.
	// A 1-token declaration on the backup drives its effective budget to 0, the
	// strongest possible overflow signal.
	cfg := refitRoster(t, 512000, OverflowTruncate)
	kai := cfg.Registry.Agents["kai"]
	one := 1
	kai.ContextWindowTokens = &one
	cfg.Registry.Agents["kai"] = kai

	slot := buildRefitSlot(t, cfg)
	fb := slot.Fallbacks[0]

	require.Equal(t, int64(0), payload.EffectiveByteBudget("unlisted-backup-model", &one, defaultMaxTokens),
		"precondition: a 1-token window must fund no input budget at all")
	assert.Len(t, fb.CodeContext, 1, "no file fits, so exactly the single smallest entry is kept — never zero")
	assert.NotEmpty(t, fb.CodeContext[0].Body, "the kept entry must carry real content")
	assert.True(t, fb.Truncation.Truncated)
	assert.Len(t, fb.Truncation.FilesDropped, len(markedOversizedPayload()["blocks"].Entries)-1,
		"every entry except the kept one must be recorded as dropped")
}

// AC5, the case a "fewest bytes" rule gets wrong: empty tracked files are
// ordinary (py.typed, __init__.py, .gitkeep). Picking one as "smallest" ships a
// payload of exactly 0 bytes — a false-clean "no findings" review that also tags
// the empty file as covered.
func TestBuildFallbackAgent_RefitNeverKeepsAZeroByteFile(t *testing.T) {
	cfg := refitRoster(t, 512000, OverflowTruncate)
	kai := cfg.Registry.Agents["kai"]
	one := 1
	kai.ContextWindowTokens = &one // effective budget 0 → the keep-smallest arm
	cfg.Registry.Agents["kai"] = kai

	payloads := markedOversizedPayload()
	mp := payloads["blocks"]
	mp.Entries = append([]payload.FileEntry{{Path: "py.typed", Size: 0, Body: ""}}, mp.Entries...)
	payloads["blocks"] = mp

	var slots []Slot
	var err error
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "blocks", "", true)
	})
	require.NoError(t, err)
	require.Len(t, slots[0].Fallbacks, 1)
	fb := slots[0].Fallbacks[0]

	require.Equal(t, int64(0), payload.EffectiveByteBudget("unlisted-backup-model", &one, defaultMaxTokens),
		"precondition: the backup must fund no input budget, so the keep-smallest arm runs")
	assert.NotEmpty(t, codeContextBytes(fb),
		"the re-fit must never ship a 0-byte payload — that is a false-clean review, not a small one")
	for _, ref := range fb.CodeContext {
		assert.NotEqual(t, "py.typed", ref.Path, "an empty file must not be chosen as the kept entry")
	}
	for _, p := range fb.chunkFiles {
		assert.NotEqual(t, "py.typed", p, "an empty file must not be tagged as covered by the re-fit")
	}
}

// emptyBodiedPayload builds entries whose Size claims content their Body does
// not carry — every body empty. keepSmallestEntry declines (ok=false) on this
// shape on EITHER re-fit arm, so the fallback must keep the inherited prompt
// and the pre-epic warn-and-ship rather than re-pack its way to a 0-byte
// dispatch. The bodies carry no `=== FILE:` markers, so the primary's
// CodeContext is unmeasurable and the overflow gate opens by design.
func emptyBodiedPayload() map[string]modePayload {
	entries := []payload.FileEntry{
		{Path: "a.py", Size: 400000, Body: ""},
		{Path: "py.typed", Size: 0, Body: ""},
	}
	return map[string]modePayload{
		"blocks": {Entries: entries, Text: "", FileCount: 2},
	}
}

// keepSmallestEntry's ok=false arm, reached through the zero-budget arm: every
// entry is empty, so there is no non-empty payload to send and the re-fit must
// be DECLINED — the fallback ships the inherited prompt unchanged.
func TestBuildFallbackAgent_RefitDeclinedWhenEveryEntryIsEmptyZeroBudget(t *testing.T) {
	cfg := refitRoster(t, 512000, OverflowTruncate)
	kai := cfg.Registry.Agents["kai"]
	one := 1
	kai.ContextWindowTokens = &one // effective budget 0 → the budget<=0 arm
	cfg.Registry.Agents["kai"] = kai

	var slots []Slot
	var err error
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, emptyBodiedPayload(), ReviewRange{Base: "a", Head: "b"}, "blocks", "", true)
	})
	require.NoError(t, err)
	require.Len(t, slots[0].Fallbacks, 1)
	fb := slots[0].Fallbacks[0]

	assert.False(t, fb.rePacked, "no non-empty payload exists to send, so no re-fit may be recorded")
	assert.Equal(t, slots[0].Primary.Prompt, fb.Prompt,
		"a declined re-fit ships the inherited prompt byte-identically")
	assert.Equal(t, degradationOverflow, fb.DegradationAction,
		"the honest overflow record stands — the backup cannot hold this lane")
}

// keepSmallestEntry's ok=false arm on the POSITIVE-budget arm: ApplyByteBudget's
// largest-first shed drops the big (empty-bodied) file and keeps the 0-byte
// py.typed, so the kept BODIES total zero — the same "nothing to send" state
// AllDropped names, and the same decline must follow.
func TestBuildFallbackAgent_RefitDeclinedWhenKeptBodiesAreEmpty(t *testing.T) {
	cfg := refitRoster(t, 512000, OverflowTruncate) // kai undeclared → 32768 default, a positive budget

	var slots []Slot
	var err error
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, emptyBodiedPayload(), ReviewRange{Base: "a", Head: "b"}, "blocks", "", true)
	})
	require.NoError(t, err)
	require.Len(t, slots[0].Fallbacks, 1)
	fb := slots[0].Fallbacks[0]

	assert.False(t, fb.rePacked, "a kept set whose bodies total 0 bytes is not a payload — the re-fit must be declined")
	assert.Equal(t, slots[0].Primary.Prompt, fb.Prompt,
		"a declined re-fit ships the inherited prompt byte-identically")
	assert.Equal(t, degradationOverflow, fb.DegradationAction)
}

// The no-shed early return: the entries measurably fit the backup's budget, but
// the payload shape is one EntriesFromRenderedPayload does not recognize, so
// the primary's CodeContext is empty and the overflow gate opened on "may not
// fit". The re-pack must then shed nothing and report no re-fit — warn-and-ship
// with a byte-identical prompt, which is what makes the Conservatism NFR hold
// by construction.
func TestBuildFallbackAgent_NoShedEarlyReturnKeepsWarnAndShip(t *testing.T) {
	cfg := refitRoster(t, 512000, OverflowTruncate) // kai undeclared → 32768 default

	var entries []payload.FileEntry
	var full strings.Builder
	for i := 0; i < 3; i++ {
		body := fmt.Sprintf("small file %d contents, no markers anywhere\n", i)
		entries = append(entries, payload.FileEntry{Path: fmt.Sprintf("s%d.txt", i), Size: int64(len(body)), Body: body})
		full.WriteString(body)
	}
	payloads := map[string]modePayload{
		"blocks": {Entries: entries, Text: full.String(), FileCount: 3},
	}

	var slots []Slot
	var err error
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "blocks", "", true)
	})
	require.NoError(t, err)
	require.Len(t, slots[0].Fallbacks, 1)
	fb := slots[0].Fallbacks[0]

	assert.False(t, fb.rePacked, "nothing was shed, so no re-fit happened")
	assert.Equal(t, slots[0].Primary.Prompt, fb.Prompt,
		"the no-shed arm ships the inherited prompt byte-identically")
	assert.Equal(t, degradationOverflow, fb.DegradationAction,
		"the unmeasurable payload keeps the honest overflow record, not a fabricated truncate")
}

// AC5 on the POSITIVE-budget arm (the arm the zero-budget tests never reach):
// one oversized entry plus a 0-byte py.typed. ApplyByteBudget drops largest-
// first and zero-size entries sort LAST — never dropped — so the budget pass
// keeps py.typed with AllDropped=false, and the re-fit would concatenate the
// kept bodies into a 0-byte payload: a false-clean "no findings" review that
// also tags py.typed as reviewed. The kept-body measurement must route this
// into keepSmallestEntry exactly as the AllDropped arm does.
func TestBuildFallbackAgent_ZeroByteKeptEntryRoutesToKeepSmallest(t *testing.T) {
	cfg := refitRoster(t, 512000, OverflowTruncate) // kai undeclared → 32768 default, a positive budget

	big := "=== FILE: big.go ===\n" + strings.Repeat("y", 400000) + "\n"
	entries := []payload.FileEntry{
		{Path: "big.go", Size: int64(len(big)), Body: big},
		{Path: "py.typed", Size: 0, Body: ""},
	}
	payloads := map[string]modePayload{
		"blocks": {Entries: entries, Text: big, FileCount: 2},
	}

	var slots []Slot
	var err error
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "blocks", "", true)
	})
	require.NoError(t, err)
	require.Len(t, slots[0].Fallbacks, 1)
	fb := slots[0].Fallbacks[0]

	assert.NotZero(t, codeContextBytes(fb),
		"the re-fit must never dispatch a 0-byte payload — that is a false-clean review, not a small one")
	assert.NotEqual(t, []string{"py.typed"}, fb.chunkFiles,
		"an empty file must not be the re-fit's whole coverage tag")
	assert.Equal(t, degradationOverflow, fb.DegradationAction,
		"the smallest available framing (big.go alone) still exceeds this budget — that is overflow, not truncate")
}

// A re-fit that shrank the payload but still cannot reach its budget has NOT
// truncated its way to safety. Recording "truncate" there would drop the only
// signal saying the review may not have been read in full — precisely when the
// agent is worst off — and the bulk zero-budget arm records overflow in the
// identical situation.
func TestBuildFallbackAgent_RefitThatStillOverflowsRecordsOverflow(t *testing.T) {
	cfg := refitRoster(t, 512000, OverflowTruncate)
	kai := cfg.Registry.Agents["kai"]
	one := 1
	kai.ContextWindowTokens = &one // effective budget 0; no file can ever fit
	cfg.Registry.Agents["kai"] = kai

	var out string
	var slots []Slot
	var err error
	out = captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, markedOversizedPayload(), ReviewRange{Base: "a", Head: "b"}, "blocks", "", true)
	})
	require.NoError(t, err)
	fb := slots[0].Fallbacks[0]

	assert.Equal(t, degradationOverflow, fb.DegradationAction,
		"the kept entry still exceeds a 0-byte budget, so this is overflow — not a successful truncate")
	assert.Contains(t, out, "may overflow",
		"the operator must still be told the backup cannot hold this lane's review")
	// The payload was still re-packed, so coverage must follow the smaller set.
	assert.True(t, fb.rePacked, "coverage attribution must follow the re-packed set whether or not it fits")
	assert.Len(t, fb.chunkFiles, 1, "the tag names only the single entry it kept")
	assert.Less(t, len(fb.Prompt), len(slots[0].Primary.Prompt), "the payload still shrank")
}

// The reservation must follow the budget actually recorded. Recording
// effective_budget 0 (absent under omitempty) beside reserved_output_tokens 8192
// is the self-contradictory status record 35.16.5.1 removed.
func TestBuildFallbackAgent_RefitReservationFollowsRecordedBudget(t *testing.T) {
	// The reservation is gated on the RAW per-model budget while a re-fit records
	// the APPLIED one, so the two can disagree. Exercised across a zero-window
	// backup and a --sprint-plan scope reservation (the path that narrows an
	// otherwise-positive budget), asserting the biconditional rather than one
	// scenario's constant: whatever budget is recorded, the reservation must agree
	// with it.
	for _, tc := range []struct {
		name   string
		window *int
		scope  string
	}{
		{name: "zero-window backup", window: func() *int { n := 1; return &n }()},
		{name: "scope constraint narrows the budget", scope: strings.Repeat("plan line\n", 8000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := refitRoster(t, 512000, OverflowTruncate)
			if tc.window != nil {
				kai := cfg.Registry.Agents["kai"]
				kai.ContextWindowTokens = tc.window
				cfg.Registry.Agents["kai"] = kai
			}

			var slots []Slot
			var err error
			captureStderr(t, func() {
				slots, _, err = buildSlots(cfg, markedOversizedPayload(), ReviewRange{Base: "a", Head: "b"}, "blocks", tc.scope, true)
			})
			require.NoError(t, err)
			require.Len(t, slots[0].Fallbacks, 1)
			fb := slots[0].Fallbacks[0]
			require.True(t, fb.rePacked, "precondition: this case must actually re-fit")

			if fb.EffectiveBudget > 0 {
				assert.Equal(t, defaultMaxTokens, fb.ReservedOutputTokens,
					"a recorded budget that funds the output cap must report reserving it")
			} else {
				assert.Equal(t, 0, fb.ReservedOutputTokens,
					"an agent whose recorded budget funds nothing must not also claim to reserve an output cap")
			}
		})
	}
}

func TestBuildFallbackAgent_RefitKeepsCacheKeyDistinct(t *testing.T) {
	// T4: a re-fit fallback reviews a payload that is neither its primary's nor its
	// own un-refit form, so it must not replay either one's cached review.
	cfg := refitRoster(t, 128000, OverflowTruncate)
	refit := buildRefitSlot(t, cfg)

	noRefit := refitRoster(t, 128000, OverflowChunk) // chunk still warns and ships
	plain := buildRefitSlot(t, noRefit)

	assert.NotEqual(t, refit.Primary.CacheKey, refit.Fallbacks[0].CacheKey,
		"a re-fit fallback must not share its primary's cache entry")
	assert.NotEqual(t, plain.Fallbacks[0].CacheKey, refit.Fallbacks[0].CacheKey,
		"a re-fit fallback must not share the cache entry of its own un-refit form")
}

func TestBuildFallbackAgent_RefitRecordsOneChunkAndBulkSizing(t *testing.T) {
	// T4: the sizing record must describe the payload actually sent. A re-fit ships
	// ONE payload, so inheriting the primary's ChunkTotal would scale the per-call
	// deadline (timeout.go) and the diff-cache sizing token by a split it does not
	// follow.
	cfg := refitRoster(t, 128000, OverflowTruncate)
	cfg.Settings.ReviewStrategy = reviewStrategyChunked
	slot := buildRefitSlot(t, cfg)
	fb := slot.Fallbacks[0]

	assert.Equal(t, 1, fb.ChunkTotal, "a re-fit fallback ships exactly one payload")
	assert.Equal(t, 0, fb.chunkMaxLines, "a re-fit fallback is bulk-sized; the bulk sentinel is 0")
	assert.Equal(t, scaledTimeoutSecs(fb.TimeoutSecs, fb.ChunkTotal), fb.TimeoutSecs,
		"ChunkTotal 1 must leave the per-call deadline unscaled")
}

func TestBuildFallbackAgent_NonTruncatePoliciesDoNotRefit(t *testing.T) {
	// AC3, at the buildSlots level (fallback_overflow_policy_test.go pins the direct
	// call): chunk and unset still warn and ship the inherited prompt untouched.
	for _, policy := range []string{OverflowChunk, ""} {
		t.Run("policy="+policy, func(t *testing.T) {
			cfg := refitRoster(t, 128000, policy)
			slot := buildRefitSlot(t, cfg)
			fb := slot.Fallbacks[0]

			assert.Equal(t, slot.Primary.Prompt, fb.Prompt, "a non-truncate policy must ship the inherited prompt")
			assert.Equal(t, degradationOverflow, fb.DegradationAction)
			assert.Equal(t, slot.Primary.Truncation, fb.Truncation, "no re-pack, so the inherited shed record stands")
		})
	}
}

func TestBuildFallbackAgent_TruncateWithoutEntriesStillWarnsAndShips(t *testing.T) {
	// The chunked-diff path (review.go chunkDiff) carries no FileEntry list, so
	// there is nothing to re-pack. It must keep the pre-epic warn-and-ship rather
	// than re-pack against an empty list — which would shed every file and produce
	// the empty, false-clean payload AC5 forbids.
	cfg := refitRoster(t, 128000, OverflowTruncate)
	cfg.Settings.ReviewStrategy = reviewStrategyChunked
	// An explicit max_context_lines wins over the model-derived budget, so the
	// split is deterministic: each file's 1000 wide lines exceed the 600-line cap,
	// giving one chunk per file, each ~100 KB — comfortably past the backup's
	// 71680-byte budget, so the overflow branch really is entered with no entries
	// to re-pack.
	g := cfg.Registry.Agents["greta"]
	lines := 600
	g.MaxContextLines = &lines
	cfg.Registry.Agents["greta"] = g

	const nFiles = 6
	diff := wideDiffOfNFiles(nFiles, 1000)
	payloads := map[string]modePayload{
		"blocks": {Text: diff, FileCount: nFiles, Entries: []payload.FileEntry{{Path: "f0.go", Size: int64(len(diff)), Body: diff}}},
	}

	var slots []Slot
	var err error
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "blocks", "", true)
	})
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: the diff must bin-pack into multiple chunk slots")

	for i, s := range slots {
		require.Len(t, s.Fallbacks, 1, "slot %d: precondition, the fallback chain is attached", i)
		assert.Empty(t, s.entries, "slot %d: precondition, a chunked-diff slot carries no entries", i)
		assert.Equal(t, s.Primary.Prompt, s.Fallbacks[0].Prompt,
			"slot %d: with no re-pack source the fallback must ship the inherited prompt unchanged", i)
	}
}
