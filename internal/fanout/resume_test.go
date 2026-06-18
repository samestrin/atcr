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
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/require"
)

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
