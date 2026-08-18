package fanout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initTruncatingRepo builds a two-file range whose diff exceeds the byte budget the
// test sets, so ApplyByteBudgetPreferEscalated sheds the larger file and keeps the
// smaller — the some-but-not-all case, which is the only one that can be silent
// (AllDropped already returns an error).
func initTruncatingRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	run("config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "small.go"), []byte("package main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.go"), []byte("package big\n"), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "base")
	base = run("rev-parse", "HEAD")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "small.go"), []byte("package main\n\nfunc a() {}\n"), 0o644))
	var big strings.Builder
	big.WriteString("package big\n")
	for i := 0; i < 4000; i++ {
		big.WriteString("// a padding line that makes this file dominate the byte budget\n")
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.go"), []byte(big.String()), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "head")
	head = run("rev-parse", "HEAD")
	return dir, base, head
}

// buildPayloads sheds WHOLE FILES largest-first when payload_byte_budget binds, and
// did so with no log line and no machine-readable record — while its two sibling
// builders (PrepareReviewFromRepo, PrepareReviewFromDiff) both warn on exactly this
// condition. On the 2026-08-15 external-repo run that silence cost 40 of 73 files:
// reviewers saw a quarter of the diff and the operator was told nothing.
func TestBuildPayloads_WarnsWhenTheByteBudgetShedsFiles(t *testing.T) {
	dir, base, head := initTruncatingRepo(t)

	cfg := &ReviewConfig{
		Registry: &registry.Registry{
			Agents: map[string]registry.AgentConfig{"greta": {Model: "m", Payload: string(payload.ModeDiff)}},
		},
		Project:  &registry.ProjectConfig{Agents: []string{"greta"}},
		Settings: registry.Settings{PayloadByteBudget: 2048},
	}

	var buf bytes.Buffer
	logger, err := log.New("debug", "json", &buf)
	require.NoError(t, err)
	ctx := log.NewContext(context.Background(), logger)

	payloads, _, err := buildPayloads(ctx, cfg, dir, base, head, false)
	require.NoError(t, err)
	require.Len(t, payloads, 1)
	var mp modePayload
	for _, v := range payloads {
		mp = v
	}
	require.True(t, mp.Truncation.Truncated,
		"precondition: the budget must have shed at least one file for this test to mean anything")
	require.NotEmpty(t, mp.Truncation.FilesDropped)

	got := buf.String()
	assert.Contains(t, got, "byte budget truncated",
		"the git-range builder must warn on the same condition its two siblings warn on")
	assert.Contains(t, got, "files_dropped",
		"the warning must name WHICH files were shed, not just that shedding happened")
}

// The chunked strategy renders every chunk-slot with a neutral payload.Truncation{}
// on purpose: truncation is a diff-wide event, so a per-chunk record would make each
// chunk claim the same dropped files, most of which never appear in it. The cost was
// that AgentStatus.Truncated/FilesDropped then stayed zero for every chunked agent,
// contradicting status.go's stated "never silent (AC 06-03)" contract.
//
// The honest place to record it is the merge: mergeResultGroup folds N chunk results
// into ONE persona record, so the diff-wide fact lands exactly once per agent.
func TestMergeResultGroup_PromotesDiffWideTruncationOntoTheMergedResult(t *testing.T) {
	trunc := payload.Truncation{Truncated: true, FilesDropped: []string{"dropped_a.go", "dropped_b.go"}}
	chunk := func(content string) Result {
		return Result{
			Agent: "greta", Status: StatusOK, Content: content,
			// Neutral per chunk — unchanged, and still what buildFallbackAgent reads.
			Truncation:     payload.Truncation{},
			DiffTruncation: trunc,
		}
	}

	merged := mergeResultGroup([]Result{chunk("f1"), chunk("f2")}, nil)

	assert.True(t, merged.Truncation.Truncated,
		"the merged persona record must carry the diff-wide truncation so statusFor reports it")
	assert.Equal(t, []string{"dropped_a.go", "dropped_b.go"}, merged.Truncation.FilesDropped)

	st := statusFor(merged, findingsResult{})
	assert.True(t, st.Truncated, "status.json is the artifact the contract promises this on")
	assert.Equal(t, []string{"dropped_a.go", "dropped_b.go"}, st.FilesDropped)
}

// A single-result group takes mergeChunkResults' fast path, which returns g[0]
// untouched. The promotion must not be shape-dependent: an agent whose chunk set
// collapsed to one result is exactly as truncated as one whose did not.
func TestMergeChunkResults_PromotesDiffWideTruncationOnTheSingleResultPath(t *testing.T) {
	merged := mergeChunkResults([]Result{{
		Agent: "greta", Status: StatusOK,
		Truncation:     payload.Truncation{},
		DiffTruncation: payload.Truncation{Truncated: true, FilesDropped: []string{"only.go"}},
	}})

	require.Len(t, merged, 1)
	assert.True(t, merged[0].Truncation.Truncated)
	assert.Equal(t, []string{"only.go"}, merged[0].Truncation.FilesDropped)
}

// The bulk path already carries the real diff-wide truncation in Truncation itself.
// Promotion must never overwrite a per-slot record that is already populated.
func TestMergeResultGroup_DoesNotOverwriteAnExistingTruncationRecord(t *testing.T) {
	own := payload.Truncation{Truncated: true, FilesDropped: []string{"this_slots_own.go"}}
	r := func() Result {
		return Result{
			Agent: "greta", Status: StatusOK, Content: "x",
			Truncation:     own,
			DiffTruncation: payload.Truncation{Truncated: true, FilesDropped: []string{"diff_wide.go"}},
		}
	}
	merged := mergeResultGroup([]Result{r(), r()}, nil)
	assert.Equal(t, []string{"this_slots_own.go"}, merged.Truncation.FilesDropped,
		"a slot that recorded its own shed keeps it; the diff-wide value is a fallback, not an override")
}

// Every test above hand-builds Result{DiffTruncation: ...} literals and calls the
// merge helpers directly, so all four wiring sites — the producer at review.go, the
// carry in invokeSlot, the panic path and the cancellation path — survived deletion
// with `go test ./internal/fanout/` green. Nothing exercised the Agent -> Result hop,
// which is where the signal was actually being lost.
//
// This drives the real chain on a SUCCEEDING chunked run:
//
//	buildSlots -> Engine.Run -> mergeChunkResults -> statusFor
//
// Success is the whole point. invokeSlot stamps DiffTruncation at its tail, which only
// runs after the chain FAILS — a successful chunk returns earlier, so on the case the
// operator actually hits (a shed payload reviewed successfully) status.json reported
// truncated=false with files_dropped empty while whole files had been dropped.
// On the BASELINE (--all / --dir) multi-chunk path the global byte budget sheds
// nothing from the review: modePayload.Entries is the PRE-budget set
// (PrepareReviewFromRepo, review.go:833) and the branch partitions THAT via
// payload.PartitionByBudget, whose contract is never-split-never-dropped. The shed
// recorded in mp.Truncation applies only to the concatenated AUDIT TEXT. review.go's
// own TD-012 comment (:812-818) states it: "every enumerated file is still reviewed
// across per-model chunks".
//
// So reporting truncated=true here is a FALSE data-loss claim — the inverse of the
// never-silent violation the diff-wide promotion was built to fix — and it is read by
// AgentStatus' contract (status.go:291) and by cli/benchmark_run.go:437, which maps
// Truncated to benchmark.OutcomeIncomplete.
//
// The fixture is what let this survive: it previously named shed paths ABSENT from
// mp.Entries, a state PrepareReviewFromRepo cannot produce on this path, so the
// assertion was guaranteed by the fixture rather than by the code. Here the shed path
// is a MEMBER of Entries, which is the only shape production emits — and the test then
// proves the file is genuinely delivered rather than merely assuming it.
func TestBaselineChunkedRun_DeliveredFilesAreNotReportedAsShed(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = 100

	entries := []payload.FileEntry{
		baselineEntry("kept_a.go", 100),
		baselineEntry("kept_b.go", 100),
		baselineEntry("kept_c.go", 100),
	}
	payloads := baselinePayloads(entries)
	// The audit-text shed buildPayloads recorded upstream. kept_c.go is dropped from
	// the concatenated Text, yet REMAINS in Entries — which is precisely why the
	// baseline fan-out still delivers it.
	mp := payloads[string(payload.ModeFiles)]
	mp.Truncation = payload.Truncation{Truncated: true, FilesDropped: []string{"kept_c.go"}}
	payloads[string(payload.ModeFiles)] = mp

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: the payload must fan out into multiple chunk slots")

	// The load-bearing proof: the "shed" file really is in a dispatched chunk.
	assert.True(t, slotsContainMarker(slots, "kept_c.go"),
		"precondition and the whole point: PartitionByBudget delivers every entry, so the "+
			"file the audit-text shed named is still reviewed")

	raw := NewEngine(baselineChunkFindingCompleter{}).Run(context.Background(), slots)
	require.Len(t, raw, len(slots))
	for i, r := range raw {
		require.Equal(t, StatusOK, r.Status, "precondition: chunk %d must SUCCEED", i)
	}

	merged := mergeChunkResults(raw)
	require.Len(t, merged, 1, "the persona's chunks collapse into one record")

	st := statusFor(merged[0], findingsResult{})
	assert.False(t, st.Truncated,
		"nothing reached no reviewer on this path, so status.json must NOT claim a shed")
	assert.Empty(t, st.FilesDropped,
		"and must name no dropped files — every one of them was delivered")
}

// chunkedFallbackRoster wires greta (declaring a large window) to fall back to kai,
// under the chunked strategy — the only strategy where the diff-wide shed lives on
// DiffTruncation rather than on Truncation itself, and so the only one where losing
// the carrier is silent.
func chunkedFallbackRoster(t *testing.T) *ReviewConfig {
	t.Helper()
	cfg := declaredWindowRoster(t, 128000)
	g := cfg.Registry.Agents["greta"]
	g.Fallback = "kai"
	cfg.Registry.Agents["greta"] = g
	kai := cfg.Registry.Agents["kai"]
	kai.Model = "unlisted-backup-model"
	cfg.Registry.Agents["kai"] = kai
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	return cfg
}

// A fallback reviews the SAME payload its primary was sized for, so it inherits the
// primary's record of what that payload dropped. Every other provenance field is
// copied across (Truncation, CodeContext, chunkFiles); DiffTruncation was not, so a
// fallback-served slot carried the zero value — and invokeAgent stamps the result's
// DiffTruncation from the SERVING agent.
func TestBuildFallbackAgent_InheritsThePrimarysDiffWideShed(t *testing.T) {
	shed := payload.Truncation{Truncated: true, FilesDropped: []string{"shed_a.go", "shed_b.go"}}

	cfg := chunkedFallbackRoster(t)
	payloads := map[string]modePayload{"blocks": {
		Text:       diffOfNFiles(12, 900), // > the declared budget → splits into chunks
		FileCount:  12,
		Truncation: shed,
	}}

	var slots []Slot
	var err error
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
	})
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: this diff must split into chunk slots")

	for i, s := range slots {
		require.Equal(t, shed, s.Primary.DiffTruncation,
			"precondition: chunk %d's primary must carry the diff-wide shed", i)
		require.Len(t, s.Fallbacks, 1, "precondition: chunk %d must resolve exactly one fallback", i)
		assert.Equal(t, shed, s.Fallbacks[0].DiffTruncation,
			"chunk %d's fallback reviews the same payload, so it owes the same shed record", i)
	}
}

// The end-to-end consequence, on the arrangement an operator actually hits: every
// chunk's primary fails, the fallback chain serves, and the persona's status.json
// must still name the files the global byte budget shed. This is the AC 06-03
// never-silent contract on a run with a fallback chain — a shape the existing
// chunked-run tests never reach because their primaries always answer.
func TestChunkedRun_FallbackServedSlotStillReportsTheDiffWideShed(t *testing.T) {
	shed := payload.Truncation{Truncated: true, FilesDropped: []string{"shed_a.go", "shed_b.go"}}

	cfg := chunkedFallbackRoster(t)
	payloads := map[string]modePayload{"blocks": {
		Text:       diffOfNFiles(12, 900),
		FileCount:  12,
		Truncation: shed,
	}}

	var slots []Slot
	var err error
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
	})
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: this diff must split into chunk slots")

	f := newFake()
	f.failFor["unlisted-small-model"] = errors.New("boom") // every primary fails; kai serves
	raw := NewEngine(f).Run(context.Background(), slots)
	require.Len(t, raw, len(slots))
	for i, r := range raw {
		require.Equal(t, StatusOK, r.Status,
			"precondition: chunk %d must be served by the FALLBACK — the failure path stamps DiffTruncation on its own", i)
		require.True(t, r.FallbackUsed, "precondition: chunk %d must have fallen back", i)
	}

	merged := mergeChunkResults(raw)
	require.Len(t, merged, 1)

	st := statusFor(merged[0], findingsResult{})
	assert.True(t, st.Truncated,
		"a fallback-served persona is exactly as truncated as a primary-served one")
	assert.Equal(t, shed.FilesDropped, st.FilesDropped,
		"and must name WHICH files were shed — the files no reviewer ever saw")
}

// The merge reads the diff-wide shed off g[0] alone, so a persona whose FIRST chunk
// lost the carrier reports nothing even when its siblings carry it. Every chunk of a
// persona is rendered from the same modePayload, so the shed is identical across the
// group — which makes first-non-zero the honest read and makes g[0]-only a
// position-dependent accident.
func TestMergeResultGroup_TakesTheDiffWideShedFromAnyChunkNotOnlyTheFirst(t *testing.T) {
	t.Parallel()
	shed := payload.Truncation{Truncated: true, FilesDropped: []string{"shed_a.go"}}
	g := []Result{
		// Chunk 0 served by an agent that carried no DiffTruncation.
		{Agent: "greta", Status: StatusOK, Content: "c0"},
		{Agent: "greta", Status: StatusOK, Content: "c1", DiffTruncation: shed},
	}

	merged := mergeResultGroup(g, nil)

	assert.True(t, merged.Truncation.Truncated,
		"the persona's shed is a diff-wide fact — which chunk happens to carry it is an accident of ordering")
	assert.Equal(t, shed.FilesDropped, merged.Truncation.FilesDropped)
}

// The re-packed-chunk-ZERO arrangement, driven through the real chain.
//
// promoteRePackedDegradation seeds its dropped-file union from out.DiffTruncation,
// and out is g[0] — so the union is only complete when chunk 0 carries the carrier.
// A re-packed chunk is by construction fallback-served (servedRePacked is stamped
// only from a buildFallbackAgent product), which is exactly the agent that used to
// arrive without it. In this arrangement the seed loop iterated an empty slice and
// out.Truncation was then overwritten with the re-pack's files alone: the persona
// named the files ONE reviewer did not see and silently omitted the ones NO reviewer
// ever saw — the more serious half.
//
// It is asserted end-to-end rather than on hand-built Results because the defect
// lives in the Agent -> Result hop: any merge-level fixture would have to hand chunk
// 0 the carrier the production path was failing to give it.
// A re-packed chunk on the BASELINE path still owns a REAL shed — its own. Re-fitting
// drops files from the chunk the fallback ships, and those genuinely reach no reviewer.
// What must NOT appear is a phantom diff-wide entry: on this path PartitionByBudget
// delivered every entry, so the only honest files_dropped is the re-pack's own set.
//
// Previously this test seeded an mp.Truncation naming files absent from mp.Entries —
// unreachable on the baseline path — and asserted the union contained them. The union
// itself is still pinned, at the level where it remains reachable, by
// TestMergeChunkResults_UnionsTheDiffWideShedWithARePackRecord.
func TestBaselineChunkedRun_RePackedChunkZeroNamesOnlyItsOwnShed(t *testing.T) {
	cfg := refitRoster(t, 128000, OverflowTruncate)
	var entries []payload.FileEntry
	for i := 0; i < 20; i++ {
		entries = append(entries, baselineEntry(fmt.Sprintf("kept_%02d.go", i), 50000))
	}
	payloads := baselinePayloads(entries)

	var slots []Slot
	var err error
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{}, string(payload.ModeFiles), "", true, true)
	})
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: the payload must fan out into multiple chunk slots")
	require.Len(t, slots[0].Fallbacks, 1)
	require.True(t, slots[0].Fallbacks[0].rePacked,
		"precondition: chunk 0's backup must re-fit, or this asserts nothing about the re-packed arrangement")

	f := newFake()
	f.failFor["unlisted-small-model"] = errors.New("boom") // every primary fails; the re-fitting backup serves
	var raw []Result
	captureStderr(t, func() { raw = NewEngine(f).Run(context.Background(), slots) })
	require.Len(t, raw, len(slots))
	require.Equal(t, StatusOK, raw[0].Status, "precondition: chunk 0 must be SERVED by its re-fitting fallback")
	require.True(t, raw[0].servedRePacked, "precondition: chunk 0 must be the re-packed one")

	var merged []Result
	captureStderr(t, func() { merged = mergeChunkResults(raw) })
	require.Len(t, merged, 1)

	st := statusFor(merged[0], findingsResult{})
	require.True(t, st.Truncated,
		"the re-pack really did drop files from the chunk it shipped, so the shed is owed")
	assert.NotEmpty(t, st.FilesDropped,
		"and files_dropped must name the re-pack's own shed — the actionable half")
	for _, p := range st.FilesDropped {
		assert.True(t, strings.HasPrefix(p, "kept_"),
			"every named file must be one this run actually handled (%q was not); a path from "+
				"the audit-text shed would be a phantom, since PartitionByBudget delivered them all", p)
	}
}

// status.json and summary.json are two published views of the SAME AgentStatus, and
// they must not disagree about whether a field is measured. WriteStatus normalizes a
// nil FilesDropped to [] on its own copy, but writePool builds PoolSummary.Agents
// from a separate, un-normalized statusFor call — so any path that hands statusFor a
// nil slice publishes "files_dropped": null in one artifact and [] in the other.
//
// The unconditional promotion is exactly such a path: a clean agent's Truncation
// carries the non-nil empty slice ApplyByteBudget guarantees, and overwriting it with
// a zero-valued DiffTruncation (nil FilesDropped) demotes an explicit "nothing was
// dropped" to "unmeasured" — the opposite direction from the never-silent contract
// AC 06-03 states.
func TestStatusFor_CleanAgentPublishesEmptyFilesDroppedNotNull(t *testing.T) {
	t.Parallel()
	// The shape ApplyByteBudget returns when nothing was shed (budget.go:61).
	clean := Result{
		Agent: "greta", Status: StatusOK, Content: "c0",
		Truncation: payload.Truncation{Truncated: false, FilesDropped: []string{}},
	}

	merged := mergeChunkResults([]Result{clean})
	require.Len(t, merged, 1)

	st := statusFor(merged[0], findingsResult{})
	require.False(t, st.Truncated, "precondition: this agent dropped nothing")
	assert.NotNil(t, st.FilesDropped,
		"a clean agent's shed list is MEASURED and empty, never unmeasured — statusFor is the seam both artifacts read")

	// The artifact-level consequence: summary.json is marshalled straight from these
	// records, with no second normalization pass.
	raw, err := json.Marshal(PoolSummary{Agents: []AgentStatus{st}})
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"files_dropped":[]`)
	assert.NotContains(t, string(raw), `"files_dropped":null`,
		"summary.json must not report a clean agent's shed list as unmeasured while status.json reports it as empty")
}

// The three FAILURE-path carriers, each driven through the real engine.
//
// engine.go asserts the field is "carried on every Result-construction path, not just
// the happy one" — a claim no test enforced: all three carriers (resultFromPanic, the
// serial lane's ctx.Err() Result, and invokeSlot's tail stamp) survived deletion with
// the suite green, because every existing DiffTruncation test either hand-builds
// Results or exercises a SUCCEEDING run.
//
// The reasoning behind the claim is what makes these worth pinning: a persona whose
// chunks panicked, were cancelled, or failed outright is exactly as truncated as one
// whose chunks returned — the files the global byte budget shed were never sent to
// anyone either way, so the merged record still owes the operator that fact.
//
// Every chunk in each group takes the SAME path, so no sibling can supply the value
// the carrier under test dropped.
func TestMergedPersona_ReportsTheDiffWideShedOnEveryFailurePath(t *testing.T) {
	shed := payload.Truncation{Truncated: true, FilesDropped: []string{"shed_a.go", "shed_b.go"}}
	chunkSlot := func(serial bool) Slot {
		return Slot{
			Primary: Agent{
				Name:           "greta",
				Invocation:     llmclient.Invocation{Model: "primary-model"},
				PayloadMode:    "blocks",
				DiffTruncation: shed,
			},
			Serial: serial,
		}
	}

	t.Run("panicked", func(t *testing.T) {
		pc := &panicCompleter{f: newFake(), panicFor: map[string]bool{"primary-model": true}}
		raw := NewEngine(pc).Run(context.Background(), []Slot{chunkSlot(false), chunkSlot(false)})
		require.Len(t, raw, 2)
		for i, r := range raw {
			require.Equal(t, StatusFailed, r.Status, "precondition: chunk %d must have panicked", i)
			require.ErrorContains(t, r.Err, "panic", "precondition: chunk %d must take the panic path", i)
		}

		st := statusFor(mergeChunkResults(raw)[0], findingsResult{})
		assert.True(t, st.Truncated, "a panicked persona is exactly as truncated as one that returned")
		assert.Equal(t, shed.FilesDropped, st.FilesDropped)
	})

	t.Run("context cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		// The SERIAL lane: its pre-invocation ctx.Err() check builds its own Result
		// literal rather than routing through invokeSlot's tail stamp.
		raw := NewEngine(newFake()).Run(ctx, []Slot{chunkSlot(true), chunkSlot(true)})
		require.Len(t, raw, 2)
		for i, r := range raw {
			require.NotEqual(t, StatusOK, r.Status, "precondition: chunk %d must be short-circuited by cancellation", i)
		}

		st := statusFor(mergeChunkResults(raw)[0], findingsResult{})
		assert.True(t, st.Truncated, "a cancelled persona still owes the shed the operator never saw")
		assert.Equal(t, shed.FilesDropped, st.FilesDropped)
	})

	t.Run("chain fully failed", func(t *testing.T) {
		// invokeAgent stamps the result from the SERVING agent, so a chain whose last
		// member carries the shed needs no tail stamp — which is why a primary-only
		// fixture cannot tell the tail carrier apart from that one. The discriminating
		// shape is a LAST attempt that lacks the field, and the tail's job is to restore
		// the slot's own record over it.
		//
		// buildFallbackAgent now copies DiffTruncation onto every fallback, so this
		// divergence is hand-constructed rather than reachable — the same reason the
		// served-tag tests hand-build theirs. The tail stamp is the backstop for exactly
		// this shape, and the slot is reported under the PRIMARY's name, so the primary's
		// record is the honest one whatever the substitute carried.
		f := newFake()
		f.failFor["primary-model"] = errors.New("boom")
		f.failFor["backup-model"] = errors.New("boom")
		withBackup := func() Slot {
			s := chunkSlot(false)
			s.Fallbacks = []Agent{{
				Name:        "kai",
				Invocation:  llmclient.Invocation{Model: "backup-model"},
				PayloadMode: "blocks",
				// Deliberately absent.
			}}
			return s
		}
		raw := NewEngine(f).Run(context.Background(), []Slot{withBackup(), withBackup()})
		require.Len(t, raw, 2)
		for i, r := range raw {
			require.Equal(t, StatusFailed, r.Status, "precondition: chunk %d's chain must be exhausted", i)
		}

		st := statusFor(mergeChunkResults(raw)[0], findingsResult{})
		assert.True(t, st.Truncated, "a wholly-failed persona still owes the shed")
		assert.Equal(t, shed.FilesDropped, st.FilesDropped)
	})
}

// The same production chain over the GIT-RANGE chunked branch, whose producer line
// (primary.DiffTruncation = mp.Truncation) is a separate site from the baseline one
// and survived deletion for the same reason: the merge-helper tests never reach it.
func TestChunkedRangeRun_ShedFilesReachStatusOnASuccessfulRun(t *testing.T) {
	shed := payload.Truncation{Truncated: true, FilesDropped: []string{"shed_range.go"}}

	cfg := declaredWindowRoster(t, 128000)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"

	payloads := map[string]modePayload{"blocks": {
		Text:       diffOfNFiles(12, 900), // > the declared budget → splits into chunks
		FileCount:  12,
		Truncation: shed,
	}}

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: this diff must split into chunk slots")

	raw := NewEngine(&plainStubCompleter{}).Run(context.Background(), slots)
	for i, r := range raw {
		require.Equal(t, StatusOK, r.Status, "precondition: chunk %d must succeed", i)
	}

	merged := mergeChunkResults(raw)
	require.Len(t, merged, 1)

	st := statusFor(merged[0], findingsResult{})
	assert.True(t, st.Truncated, "the git-range chunked path owes the same never-silent signal")
	assert.Equal(t, shed.FilesDropped, st.FilesDropped)
}
