package fanout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
func TestBaselineChunkedRun_ShedFilesReachStatusOnASuccessfulRun(t *testing.T) {
	shed := payload.Truncation{Truncated: true, FilesDropped: []string{"shed_a.go", "shed_b.go"}}

	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = 100

	entries := []payload.FileEntry{
		baselineEntry("kept_a.go", 100),
		baselineEntry("kept_b.go", 100),
		baselineEntry("kept_c.go", 100),
	}
	payloads := baselinePayloads(entries)
	// The diff-wide shed buildPayloads recorded upstream: these two files never made
	// it into any chunk, which is exactly why no per-chunk record can carry the fact.
	mp := payloads[string(payload.ModeFiles)]
	mp.Truncation = shed
	payloads[string(payload.ModeFiles)] = mp

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: the payload must fan out into multiple chunk slots")

	raw := NewEngine(baselineChunkFindingCompleter{}).Run(context.Background(), slots)
	require.Len(t, raw, len(slots))
	for i, r := range raw {
		require.Equal(t, StatusOK, r.Status,
			"precondition: chunk %d must SUCCEED — the failure path stamps DiffTruncation on its own", i)
	}

	merged := mergeChunkResults(raw)
	require.Len(t, merged, 1, "the persona's chunks collapse into one record")

	st := statusFor(merged[0], findingsResult{})
	assert.True(t, st.Truncated,
		"status.json must report the diff-wide shed on a successful chunked run — the never-silent contract")
	assert.Equal(t, shed.FilesDropped, st.FilesDropped,
		"and must name WHICH files were shed, which is the actionable half")
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
func TestBaselineChunkedRun_RePackedChunkZeroStillNamesTheDiffWideShed(t *testing.T) {
	diffWide := payload.Truncation{Truncated: true, FilesDropped: []string{"never_seen_a.go", "never_seen_b.go"}}

	cfg := refitRoster(t, 128000, OverflowTruncate)
	var entries []payload.FileEntry
	for i := 0; i < 20; i++ {
		entries = append(entries, baselineEntry(fmt.Sprintf("kept_%02d.go", i), 50000))
	}
	payloads := baselinePayloads(entries)
	mp := payloads[string(payload.ModeFiles)]
	mp.Truncation = diffWide
	payloads[string(payload.ModeFiles)] = mp

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
	require.True(t, st.Truncated)
	assert.Subset(t, st.FilesDropped, diffWide.FilesDropped,
		"files_dropped must still name the files the global byte budget shed — a re-pack on chunk 0 "+
			"must not overwrite the diff-wide half of the union")
	assert.Greater(t, len(st.FilesDropped), len(diffWide.FilesDropped),
		"and it must remain a UNION: the re-pack's own shed files belong there too")
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
