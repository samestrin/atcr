package fanout

import (
	"os"
	"path/filepath"
	"testing"
)

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
