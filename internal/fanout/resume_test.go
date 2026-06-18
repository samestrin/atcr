package fanout

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/require"
)

// fourAgentConfig builds a roster of four agents pointed at srvURL, serialized
// (MaxParallel=1) so an interrupt lands deterministically after N completions.
// Each persona equals its agent name so resolution falls through to the embedded
// default (an explicit persona ref distinct from the agent name would require a
// file on disk).
func fourAgentConfig(srvURL string) *ReviewConfig {
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: srvURL}},
		Agents: map[string]registry.AgentConfig{
			"greta": {Provider: "p", Model: "m-greta", Persona: "greta", Temperature: ptrF(0.7)},
			"kai":   {Provider: "p", Model: "m-kai", Persona: "kai", Temperature: ptrF(0.7)},
			"mira":  {Provider: "p", Model: "m-mira", Persona: "mira", Temperature: ptrF(0.7)},
			"otto":  {Provider: "p", Model: "m-otto", Persona: "otto", Temperature: ptrF(0.7)},
		},
	}
	return &ReviewConfig{
		Registry:    reg,
		Project:     &registry.ProjectConfig{Agents: []string{"greta", "kai", "mira", "otto"}},
		Settings:    registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600, MaxParallel: 1},
		PersonaDirs: registry.PersonaDirs{}, // empty → embedded personas
	}
}

// okCompleter is a fake Completer that returns one finding for every call, so a
// resumed pending agent always succeeds deterministically.
type okCompleter struct{}

func (okCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "CRITICAL|auth.go:3|Unchecked call|Guard it|security|15|b() unchecked", nil
}

// writeAgentStatusFixture scaffolds sources/pool/raw/agent/<agent>/status.json with
// the given outcome so resume-state tests exercise the real on-disk layout.
func writeAgentStatusFixture(t *testing.T, reviewDir, agent, status string) {
	t.Helper()
	dir := filepath.Join(reviewDir, "sources", "pool", poolRawAgentDir, agent)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st := &AgentStatus{Agent: agent, Status: status}
	if err := WriteStatus(filepath.Join(dir, statusFile), st); err != nil {
		t.Fatal(err)
	}
}

func TestCompletedAgents_OnlyOKAgentsAreComplete(t *testing.T) {
	dir := t.TempDir()
	writeAgentStatusFixture(t, dir, "alpha", StatusOK)
	writeAgentStatusFixture(t, dir, "bravo", StatusOK)
	writeAgentStatusFixture(t, dir, "charlie", StatusFailed)
	writeAgentStatusFixture(t, dir, "delta", StatusTimeout)

	got, err := CompletedAgents(dir)
	if err != nil {
		t.Fatalf("CompletedAgents: %v", err)
	}
	if len(got) != 2 || !got["alpha"] || !got["bravo"] {
		t.Fatalf("expected completed = {alpha, bravo}, got %v", got)
	}
	if got["charlie"] || got["delta"] {
		t.Fatalf("failed/timeout agents must be pending, got %v", got)
	}
}

func TestCompletedAgents_MissingPoolDirIsEmpty(t *testing.T) {
	dir := t.TempDir() // nothing fanned out yet
	got, err := CompletedAgents(dir)
	if err != nil {
		t.Fatalf("missing pool dir must not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty set, got %v", got)
	}
}

func TestCompletedAgents_CorruptOrMissingStatusIsPending(t *testing.T) {
	dir := t.TempDir()
	writeAgentStatusFixture(t, dir, "alpha", StatusOK)

	// Corrupt status.json: cannot be trusted as complete, so the agent re-runs.
	corrupt := filepath.Join(dir, "sources", "pool", poolRawAgentDir, "bravo")
	if err := os.MkdirAll(corrupt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corrupt, statusFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Agent dir with no status.json at all (interrupted before it started).
	if err := os.MkdirAll(filepath.Join(dir, "sources", "pool", poolRawAgentDir, "charlie"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := CompletedAgents(dir)
	if err != nil {
		t.Fatalf("CompletedAgents: %v", err)
	}
	if !got["alpha"] {
		t.Fatalf("alpha (ok) must be complete, got %v", got)
	}
	if got["bravo"] || got["charlie"] {
		t.Fatalf("corrupt/missing status must be pending, got %v", got)
	}
}

func TestValidateResumeRange(t *testing.T) {
	m := &payload.Manifest{Base: "aaa111", Head: "bbb222"}

	if err := ValidateResumeRange(m, ReviewRange{Base: "aaa111", Head: "bbb222"}); err != nil {
		t.Fatalf("matching range must validate, got %v", err)
	}
	if err := ValidateResumeRange(m, ReviewRange{Base: "ZZZ", Head: "bbb222"}); !errors.Is(err, ErrRangeChanged) {
		t.Fatalf("base mismatch must be ErrRangeChanged, got %v", err)
	}
	if err := ValidateResumeRange(m, ReviewRange{Base: "aaa111", Head: "ZZZ"}); !errors.Is(err, ErrRangeChanged) {
		t.Fatalf("head mismatch must be ErrRangeChanged, got %v", err)
	}
}

func TestValidateResumeRoster_SetEquality(t *testing.T) {
	m := &payload.Manifest{Roster: []string{"alpha", "bravo", "charlie"}}

	// Same set, different order — must pass (order-independent).
	if err := ValidateResumeRoster(m, []string{"charlie", "alpha", "bravo"}); err != nil {
		t.Fatalf("same set in different order must validate, got %v", err)
	}
	// Extra agent in config.
	if err := ValidateResumeRoster(m, []string{"alpha", "bravo", "charlie", "delta"}); !errors.Is(err, ErrRosterChanged) {
		t.Fatalf("added agent must be ErrRosterChanged, got %v", err)
	}
	// Missing agent from config.
	if err := ValidateResumeRoster(m, []string{"alpha", "bravo"}); !errors.Is(err, ErrRosterChanged) {
		t.Fatalf("removed agent must be ErrRosterChanged, got %v", err)
	}
	// Swapped agent (same count, different membership).
	if err := ValidateResumeRoster(m, []string{"alpha", "bravo", "delta"}); !errors.Is(err, ErrRosterChanged) {
		t.Fatalf("swapped agent must be ErrRosterChanged, got %v", err)
	}
}

func TestFilterPendingSlots(t *testing.T) {
	slots := []Slot{
		{Primary: Agent{Name: "alpha"}},
		{Primary: Agent{Name: "bravo"}},
		{Primary: Agent{Name: "charlie"}},
	}
	done := map[string]bool{"alpha": true, "charlie": true}
	got := filterPendingSlots(slots, done)
	require.Len(t, got, 1)
	require.Equal(t, "bravo", got[0].Primary.Name)
}

func TestRebuildPool_UnionFromDisk(t *testing.T) {
	dir := t.TempDir()
	poolDir := filepath.Join(dir, "sources", "pool")
	// alpha: ok with a finding; bravo: ok with zero findings; charlie: failed.
	require.NoError(t, writeResumedAgents(poolDir, []Result{
		{Agent: "alpha", Status: StatusOK, Content: "CRITICAL|a.go:1|x|y|security|15|ev"},
		{Agent: "bravo", Status: StatusOK, Content: ""},
		{Agent: "charlie", Status: StatusFailed, Err: errors.New("boom")},
	}))

	sum, statuses, err := RebuildPool(poolDir)
	require.NoError(t, err)
	require.Equal(t, 3, sum.Total)
	require.Equal(t, 2, sum.Succeeded)
	require.Equal(t, 1, sum.Failed)
	require.True(t, sum.Partial)
	require.Len(t, statuses, 3)

	fdata, err := os.ReadFile(filepath.Join(poolDir, findingsFile))
	require.NoError(t, err)
	pr, err := stream.ParseSource(fdata)
	require.NoError(t, err)
	require.Len(t, pr.Findings, 1, "only alpha contributed a finding")

	ps, err := ReadPoolSummary(dir)
	require.NoError(t, err)
	require.Equal(t, 3, ps.Total)
	require.Equal(t, 1, ps.TotalFindings)
}

func TestExecuteResume_MergesCompletedAndPending(t *testing.T) {
	dir := t.TempDir()
	names := []string{"greta", "kai", "mira", "otto"}
	m := &payload.Manifest{
		Base: "a", Head: "b", Roster: names,
		StartedAt: time.Now().UTC(), TimeoutSecs: 600, PayloadMode: "blocks",
		PerAgentPayload: map[string]string{}, Stages: []string{"review"},
	}
	require.NoError(t, WriteManifest(dir, m))

	// Pre-populate greta + kai as already completed on disk (kai found nothing).
	poolDir := filepath.Join(dir, "sources", "pool")
	require.NoError(t, writeResumedAgents(poolDir, []Result{
		{Agent: "greta", Status: StatusOK, Content: "CRITICAL|auth.go:3|x|y|security|15|ev"},
		{Agent: "kai", Status: StatusOK, Content: ""},
	}))

	// Resume runs only the pending slots (mira, otto).
	var slots []Slot
	for _, n := range []string{"mira", "otto"} {
		slots = append(slots, Slot{Primary: Agent{
			Name: n, Invocation: llmclient.Invocation{Model: n}, PayloadMode: "blocks",
		}})
	}
	prep := &PreparedReview{ID: "2026-06-18_x", Dir: dir, Slots: slots, MaxParallel: 1, manifest: m}

	res, err := ExecuteResume(context.Background(), okCompleter{}, prep)
	require.NoError(t, err)
	require.Equal(t, 4, res.Summary.Total, "union covers the whole roster")
	require.Equal(t, 4, res.Summary.Succeeded)
	require.False(t, res.Summary.Partial)

	st, err := ReadReviewStatus(dir, "2026-06-18_x")
	require.NoError(t, err)
	require.Equal(t, RunCompleted, st.Status, "AC6: all agents complete -> completed")

	// All four per-agent status.json present and ok.
	done, err := CompletedAgents(dir)
	require.NoError(t, err)
	require.Len(t, done, 4)
}

// TestExecuteResume_ReviewStageReflectsResumedRun verifies the manifest's Review
// stage is recomputed from the resumed engine results, not preserved verbatim
// from the original run. A tool agent that degraded in the original run but
// succeeds with full tools on resume must not stay recorded as degraded.
func TestExecuteResume_ReviewStageReflectsResumedRun(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)

	// Phase 1: scaffold a 4-agent review. Pre-populate greta+kai as already
	// completed on disk — this time with ToolsDegraded=FALSE (they succeeded
	// with tools). The original manifest's Review stage, however, records them
	// as degraded (simulating a scenario where the manifest was written before
	// the agent artifacts were finalized, or where the manifest was preserved
	// from an earlier interrupted snapshot).
	cfg := fourAgentConfig("http://unused")
	prep, err := PrepareReview(context.Background(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)

	poolDir := filepath.Join(prep.Dir, "sources", "pool")
	require.NoError(t, writeResumedAgents(poolDir, []Result{
		{Agent: "greta", Status: StatusOK, Tools: true, ToolsRequested: true, ToolsDegraded: false,
			Content: "CRITICAL|auth.go:3|x|y|security|15|ev"},
		{Agent: "kai", Status: StatusOK, Tools: true, ToolsRequested: true, ToolsDegraded: false,
			Content: ""},
	}))

	// Stamp the manifest with a Review stage showing greta+kai as degraded
	// (simulating the bug: the manifest says degraded, but the on-disk statuses
	// say they completed successfully with tools).
	m, err := ReadManifest(prep.Dir)
	require.NoError(t, err)
	m.Review = &payload.ReviewStage{
		Agents:        []string{"greta", "kai", "mira", "otto"},
		ToolsEnabled:  []string{"greta", "kai", "mira", "otto"},
		ToolsDegraded: []string{"greta", "kai"},
		SnapshotMode:  "live",
		HeadSHA:       head,
	}
	require.NoError(t, WriteManifest(prep.Dir, m))

	// Phase 2: resume against a healthy provider. Only the 2 pending agents run;
	// they succeed with tools enabled (not degraded).
	srv := mockProvider(t)
	cfg2 := fourAgentConfig(srv.URL)
	rprep, info, err := PrepareResume(context.Background(), cfg2, prep.Dir, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	require.Len(t, info.Completed, 2)
	require.Len(t, info.Pending, 2)

	res, err := ExecuteResume(context.Background(), llmclient.New(), rprep)
	require.NoError(t, err)
	require.Equal(t, 4, res.Summary.Total)
	require.Equal(t, 4, res.Summary.Succeeded)

	// THE FIX ASSERTION: manifest Review stage must be recomputed from the
	// union of on-disk statuses, NOT preserved verbatim from the pre-resume
	// manifest. greta+kai's status.json says ToolsDegraded=false; mira+otto
	// just ran with tools and did NOT degrade. The union's ToolsDegraded must
	// be empty — but the bug preserves the pre-resume manifest's
	// ToolsDegraded=["greta", "kai"] verbatim.
	mAfter, err := ReadManifest(prep.Dir)
	require.NoError(t, err)
	require.NotNil(t, mAfter.Review, "Review stage must be present after resume")
	require.ElementsMatch(t, []string{"greta", "kai", "mira", "otto"}, mAfter.Review.ToolsEnabled)
	require.Empty(t, mAfter.Review.ToolsDegraded,
		"ToolsDegraded must be derived from the union of on-disk statuses (all false), not preserved from pre-resume manifest")
	// Snapshot provenance must also reflect the resumed run, not the pre-resume
	// manifest (which happened to match in this test, but the principle holds).
	require.Equal(t, "live", mAfter.Review.SnapshotMode)
	require.Equal(t, head, mAfter.Review.HeadSHA)
}

// TestResume_InterruptThenResumeCompletesAllAgents is the epic 4.1.1 AC9
// integration test: a 4-agent review is interrupted after 2 agents complete, then
// resumed against a healthy provider — exercising the real PrepareReview ->
// ExecuteReview(interrupt) -> PrepareResume -> ExecuteResume stack. Only the 2
// pending agents are re-run, all 4 end with results on disk, and the final derived
// status is "completed" (not interrupted/partial).
func TestResume_InterruptThenResumeCompletesAllAgents(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)

	// Phase 1: scaffold + fan out, interrupted after greta + kai complete. The
	// provider URL is irrelevant here — the fake completer drives this phase.
	cfg := fourAgentConfig("http://unused")
	prep, err := PrepareReview(context.Background(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := &cancelAfterCompleter{cancelAt: 2, cancel: cancel}
	_, _ = ExecuteReview(ctx, fake, prep)

	done, err := CompletedAgents(prep.Dir)
	require.NoError(t, err)
	require.Len(t, done, 2, "exactly 2 agents completed before the interrupt (the engine picks which 2)")
	st, err := ReadReviewStatus(prep.Dir, prep.ID)
	require.NoError(t, err)
	require.Equal(t, RunInterrupted, st.Status)

	// Phase 2: resume against a healthy provider — runs only the pending agents.
	srv := mockProvider(t)
	cfg2 := fourAgentConfig(srv.URL)
	rprep, info, err := PrepareResume(context.Background(), cfg2, prep.Dir, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	require.Len(t, info.Completed, 2)
	require.Len(t, info.Pending, 2)
	require.ElementsMatch(t, []string{"greta", "kai", "mira", "otto"},
		append(append([]string{}, info.Completed...), info.Pending...), "completed + pending = full roster")
	require.Len(t, rprep.Slots, 2, "AC4: only the pending agents fan out")

	res, err := ExecuteResume(context.Background(), llmclient.New(), rprep)
	require.NoError(t, err)
	require.Equal(t, 4, res.Summary.Total)
	require.Equal(t, 4, res.Summary.Succeeded, "AC9: all four agents have results")
	require.False(t, res.Summary.Partial)

	doneAfter, err := CompletedAgents(prep.Dir)
	require.NoError(t, err)
	require.Len(t, doneAfter, 4)
	stAfter, err := ReadReviewStatus(prep.Dir, prep.ID)
	require.NoError(t, err)
	require.Equal(t, RunCompleted, stAfter.Status, "AC6/AC9: final status is completed, not interrupted")
	require.False(t, stAfter.Partial)
}
