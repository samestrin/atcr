package fanout

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/log"
)

// TD internal/fanout/artifacts.go:395 — Result.Salvaged reached NO artifact, so a
// reviewer that lost its whole contribution to a salvage was byte-identical in
// status.json to one that emitted garbled prose. These pin the three disclosures:
// the per-agent marker, the run-level tally, and the per-bin indices for a chunked
// persona.
func TestWritePool_RecordsSalvagedRepliesPerAgentAndInTheTally(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	results := []Result{
		// Salvaged: the client promoted reasoning into Content, so parseFindings
		// refuses it and the agent's zero findings are a refusal.
		{Agent: "drafter", Status: StatusOK, Salvaged: true, UnparseableResponse: true,
			Content: "HIGH|a.go:1|draft I never committed to|f|correctness|5|e"},
		{Agent: "clean", Status: StatusOK, Content: "HIGH|b.go:2|b|f|correctness|5|e|clean"},
	}
	_, err := WritePool(pool, results, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(pool, "summary.json"))
	require.NoError(t, err)
	var ps PoolSummary
	require.NoError(t, json.Unmarshal(data, &ps))

	assert.Equal(t, 1, ps.SalvagedCount, "the run-level tally must count the salvaged agent")
	byAgent := map[string]AgentStatus{}
	for _, a := range ps.Agents {
		byAgent[a.Agent] = a
	}
	assert.True(t, byAgent["drafter"].Salvaged, "the per-agent marker must say the reply was salvaged")
	assert.Equal(t, 0, byAgent["drafter"].FindingsCount, "precondition: the salvaged reply contributed nothing")
	assert.False(t, byAgent["clean"].Salvaged)

	// The key is always present, so a 0 is distinguishable from an older summary.
	assert.Contains(t, string(data), `"salvaged_count"`)
}

// A merged persona's per-bin indices: the persona-wide bit cannot say WHICH bin
// salvaged, and parseFindings refuses per bin.
func TestStatusFor_MergedPersonaNamesTheSalvagedBins(t *testing.T) {
	merged := mergeResultGroup([]Result{
		{Agent: "bruce", Status: StatusOK, Content: "MEDIUM|b.go:2|real finding|f|correctness|2|e"},
		{Agent: "bruce", Status: StatusOK, Content: "HIGH|a.go:1|draft|f|correctness|5|e",
			Salvaged: true, UnparseableResponse: true},
	}, nil)
	require.Equal(t, []bool{false, true}, merged.chunkSalvaged, "precondition: the flags are aligned")

	st := statusFor(merged, findingsFor(merged, nil))
	assert.True(t, st.Salvaged, "the persona record must disclose the salvage")
	assert.Equal(t, []int{1}, st.SalvagedChunks, "the index of the salvaged bin must be named")
}

// The operator-facing half: a salvage must reach the console, not only an artifact
// an operator has to go looking for.
func TestWritePool_WarnsAboutSalvagedReplies(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	var err error
	out := captureWarnLog(t, func(ctx context.Context) {
		_, err = writePool(ctx, pool, []Result{
			{Agent: "drafter", Status: StatusOK, Salvaged: true, Content: "HIGH|a.go:1|draft|f|correctness|5|e"},
			{Agent: "bruce", Status: StatusOK, Content: "HIGH|x.go:1|p|f|correctness|5|e"},
		}, nil, "")
	})
	require.NoError(t, err)
	assert.Contains(t, out, "salvaged", "a refused contribution must reach the console, not just summary.json")
	assert.Contains(t, out, "drafter", "the warning must name the agent whose contribution was refused")
	assert.NotContains(t, out, "bruce", "a clean agent is not a salvage")
}

// A chunked persona's Salvaged bit is an OR-fold over its bins, so one refused bin
// sets it beside a sibling bin that landed real findings. The disclosure must not
// then claim the reviewer contributed nothing — that is false of exactly the case the
// per-bin refusal exists to protect.
func TestWritePool_SalvagedBinBesideRealFindingsIsNotReportedAsTotalLoss(t *testing.T) {
	merged := mergeResultGroup([]Result{
		{Agent: "bruce", Status: StatusOK, Content: "MEDIUM|b.go:2|real finding|f|correctness|2|e"},
		{Agent: "bruce", Status: StatusOK, Content: "HIGH|a.go:1|draft|f|correctness|5|e", Salvaged: true},
	}, nil)
	pool := filepath.Join(t.TempDir(), "pool")
	var err error
	out := captureWarnLog(t, func(ctx context.Context) {
		_, err = writePool(ctx, pool, []Result{merged}, nil, "")
	})
	require.NoError(t, err)
	assert.Contains(t, out, "bruce")
	assert.Contains(t, out, "chunk 1 refused", "the warning must name the bin, not the whole persona")
	assert.NotContains(t, out, "contributed nothing",
		"bruce landed a real finding from its clean bin")
}

// warnSalvaged must distinguish a fresh run from a resume rebuild, the way its
// sibling warnTruncatedZeroFindings already does. RebuildPool tallies the UNION of
// all on-disk statuses, and a salvaged agent stays StatusOK so agentCompleted marks
// it done, filterPendingSlots never re-runs it and its status.json is never
// rewritten — so every subsequent resume re-prints the same salvage as though it
// were new, with no restatement marker (TD internal/fanout/resume.go:780).
func TestWarnSalvaged_DistinguishesTheResumeRestatement(t *testing.T) {
	fresh := captureWarn(t, func(ctx context.Context) {
		warnSalvaged(ctx, 1, []string{"drafter (contributed nothing)"}, false)
	})
	cumulative := captureWarn(t, func(ctx context.Context) {
		warnSalvaged(ctx, 1, []string{"drafter (contributed nothing)"}, true)
	})

	require.NotEmpty(t, fresh)
	require.NotEmpty(t, cumulative)
	assert.NotEqual(t, fresh, cumulative,
		"a resume rebuild reports a cumulative tally, so it must read differently from a fresh run")
	assert.Contains(t, cumulative, "cumulative",
		"the resumed wording must mark itself as a restatement, matching warnTruncatedZeroFindings")

	// Silent at 0 in both modes — the shared contract with its sibling.
	assert.Empty(t, captureWarn(t, func(ctx context.Context) { warnSalvaged(ctx, 0, nil, true) }))
}

// captureWarn runs fn with a context carrying an in-memory logger and returns the
// captured output.
func captureWarn(t *testing.T, fn func(context.Context)) string {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	fn(log.NewContext(context.Background(), logger))
	return buf.String()
}

// The SalvagedCount contract carries a resume-path limitation that had to be
// STATED rather than silently inherited: the field is not-omitempty precisely so a
// 0 reads as a measurement, but on the resume path the count is derived from
// per-agent status.json files and an agent completed by a pre-upgrade binary has no
// `salvaged` key — it unmarshals false and tallySalvaged cannot see it. The record
// then publishes a measured-looking 0 that is unmeasured for that subset.
//
// Pinned as a doc-vs-code guard on the field's own contract comment, because the
// limitation is a property of the DERIVATION, not of any value the field can hold.
func TestPoolSummary_SalvagedCountDocumentsTheResumeGap(t *testing.T) {
	src := readRepoFile(t, "artifacts.go")
	assert.Contains(t, src, "ON THE RESUME PATH ONLY, a 0 can undercount",
		"the SalvagedCount contract must state the resume-path gap; the not-omitempty tag reads as unconditional otherwise")
	assert.Contains(t, src, "pre-upgrade agent that salvaged is invisible to it",
		"and it must name the concrete failure mode, so the limitation is actionable")
}

// readRepoFile reads a repo file by path relative to this package, failing the test
// rather than the package if it has moved.
func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoErrorf(t, err, "reading %s: if it moved, update this guard to follow it", path)
	return string(b)
}

// A PARTIAL salvage whose sibling's findings were then dropped by the grounding
// gate or the min_severity floor reaches salvageCost with FindingsCount 0, so the
// "contributed nothing" arm fired, discarded the SalvagedChunks detail and blamed
// the salvage for a loss the gate caused — sending the operator to salvagedRemedy
// ("declare thinking: off / repoint the model"), which would not change that
// outcome. FindingsCount is POST-grounding (statusFor reads fr.Findings), so it
// cannot answer what the SALVAGE cost (TD internal/fanout/artifacts.go:248).
func TestSalvageCost_DoesNotBlameTheSalvageForAPostGroundingZero(t *testing.T) {
	// A chunked persona: bin 1 refused, bin 0 clean. The clean bin's finding was
	// then dropped downstream, so the published count is 0 while the refusal is
	// still only partial.
	st := AgentStatus{
		Agent:          "bruce",
		Salvaged:       true,
		SalvagedChunks: []int{1},
		FindingsCount:  0, // post-grounding
	}
	cost := salvageCost(st)
	assert.NotContains(t, cost, "contributed nothing",
		"the salvage cost one BIN, not the persona; a post-grounding zero must not be attributed to it")
	assert.Contains(t, cost, "chunk 1",
		"and the bin detail must survive, which is what the early return discarded")

	// A whole-persona salvage has no bin index and DID lose everything, so that
	// case keeps the total-loss wording.
	whole := salvageCost(AgentStatus{Agent: "dax", Salvaged: true, FindingsCount: 0})
	assert.Contains(t, whole, "contributed nothing")

	// A chunked persona whose every bin salvaged is also a total loss.
	every := salvageCost(AgentStatus{Agent: "kai", Salvaged: true, SalvagedChunks: []int{0, 1}, ChunkCount: 2, FindingsCount: 0})
	assert.Contains(t, every, "contributed nothing",
		"every bin refused is a whole-persona loss, whatever the index says")

	// Partial salvage with surviving findings keeps its existing wording.
	partial := salvageCost(AgentStatus{Agent: "otto", Salvaged: true, SalvagedChunks: []int{1}, FindingsCount: 5})
	assert.Equal(t, " (chunk 1 refused, its siblings kept)", partial)
}
