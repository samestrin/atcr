package repro

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/sandbox"
	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBackend returns a queued sequence of results, one per Run call.
type fakeBackend struct {
	results []sandbox.RunResult
	calls   int
}

func (b *fakeBackend) Name() string                    { return "fake" }
func (b *fakeBackend) Preflight(context.Context) error { return nil }
func (b *fakeBackend) Run(context.Context, sandbox.RunSpec) (sandbox.RunResult, error) {
	r := b.results[b.calls%len(b.results)]
	b.calls++
	return r, nil
}

func TestVerdict_TwoRunRule(t *testing.T) {
	cases := []struct {
		name     string
		r1, r2   sandbox.RunResult
		expected string
	}{
		{"deterministic non-zero => confirmed", rr(1, false), rr(1, false), reclib.VerdictConfirmed},
		{"deterministic zero => unverifiable (not reproduced)", rr(0, false), rr(0, false), reclib.VerdictUnverifiable},
		{"exit codes disagree => unverifiable (flaky)", rr(1, false), rr(2, false), reclib.VerdictUnverifiable},
		{"first timed out => unverifiable", rr(124, true), rr(1, false), reclib.VerdictUnverifiable},
		{"both timed out => unverifiable", rr(124, true), rr(124, true), reclib.VerdictUnverifiable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Verdict(tc.r1, tc.r2))
		})
	}
}

func TestVerdict_DockerInfraCodesAreUnverifiable(t *testing.T) {
	// Docker-reserved exit codes and signal deaths reflect infrastructure, not
	// the asserted defect. They must not be promoted to confirmed.
	cases := []struct {
		name string
		code int
	}{
		{"docker daemon error", 125},
		{"container command cannot execute", 126},
		{"container command not found", 127},
		{"oom kill / sigkill", 137},
		{"segfault", 139},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, reclib.VerdictUnverifiable, Verdict(rr(tc.code, false), rr(tc.code, false)))
		})
	}
}

func rr(code int, timedOut bool) sandbox.RunResult {
	return sandbox.RunResult{Command: "repro", ExitCode: code, Output: "out", TimedOut: timedOut}
}

func TestReproduce_RunsTwiceAndBuildsEvidence(t *testing.T) {
	b := &fakeBackend{results: []sandbox.RunResult{
		{Command: "go test ./x", ExitCode: 1, Output: "FAIL"},
		{Command: "go test ./x", ExitCode: 1, Output: "FAIL"},
	}}
	verdict, ev, err := Reproduce(context.Background(), b, sandbox.RunSpec{Command: []string{"go", "test"}, SnapshotDir: "/snap"})
	require.NoError(t, err)
	assert.Equal(t, 2, b.calls, "the determinism rule requires exactly two runs")
	assert.Equal(t, reclib.VerdictConfirmed, verdict)
	require.NotNil(t, ev)
	assert.Equal(t, 1, ev.ExitCode)
	assert.Equal(t, "go test ./x", ev.Command)
	assert.Contains(t, ev.OutputExcerpt, "FAIL")
}

// errBackend fails on the Nth run (1-based), succeeding before that.
type errBackend struct {
	failOn int
	calls  int
}

func (b *errBackend) Name() string                    { return "err" }
func (b *errBackend) Preflight(context.Context) error { return nil }
func (b *errBackend) Run(context.Context, sandbox.RunSpec) (sandbox.RunResult, error) {
	b.calls++
	if b.calls == b.failOn {
		return sandbox.RunResult{}, assertErr
	}
	return sandbox.RunResult{ExitCode: 1, Output: "out"}, nil
}

var assertErr = errBackendErr("docker spawn failed")

type errBackendErr string

func (e errBackendErr) Error() string { return string(e) }

func TestReproduce_BackendFaultIsReturned(t *testing.T) {
	for _, failOn := range []int{1, 2} {
		b := &errBackend{failOn: failOn}
		_, _, err := Reproduce(context.Background(), b, sandbox.RunSpec{Command: []string{"x"}, SnapshotDir: "/s"})
		require.Error(t, err, "a backend fault on run %d must surface", failOn)
	}
}

func TestStamp_NoPriorVerdictUnverifiableStillRecords(t *testing.T) {
	f := &reconcile.JSONFinding{}
	Stamp(f, reclib.VerdictUnverifiable, &reconcile.EvidenceExec{Command: "x", ExitCode: 0})
	require.NotNil(t, f.Verification)
	assert.Equal(t, reclib.VerdictUnverifiable, f.Verification.Verdict)
	assert.Equal(t, "repro", f.Verification.Skeptic)
}

func TestStamp_ConfirmedSetsVerdictAndEvidence(t *testing.T) {
	f := &reconcile.JSONFinding{Severity: "HIGH", Confidence: "MEDIUM"}
	ev := &reconcile.EvidenceExec{Command: "go test ./x", ExitCode: 1, OutputExcerpt: "FAIL"}
	Stamp(f, reclib.VerdictConfirmed, ev)

	require.NotNil(t, f.Verification)
	assert.Equal(t, reclib.VerdictConfirmed, f.Verification.Verdict)
	assert.Equal(t, "repro", f.Verification.Skeptic)
	require.NotNil(t, f.EvidenceExec)
	// A reproduced finding is VERIFIED by definition (gate/confidence reuse).
	assert.Equal(t, reclib.ConfidenceVerified, reclib.ConfidenceForVerdict(f.Confidence, f.Verification.Verdict))
}

func TestStamp_DoesNotDowngradeExistingConfirmed(t *testing.T) {
	f := &reconcile.JSONFinding{Verification: &reconcile.Verification{Verdict: reclib.VerdictConfirmed, Skeptic: "otto"}}
	ev := &reconcile.EvidenceExec{Command: "x", ExitCode: 0, OutputExcerpt: ""}
	Stamp(f, reclib.VerdictUnverifiable, ev)
	// Evidence is attached, but an existing confirmed verdict is never downgraded.
	assert.Equal(t, reclib.VerdictConfirmed, f.Verification.Verdict)
	require.NotNil(t, f.EvidenceExec)
}

// TestReproduce_EndToEnd_PlantedBug is the SC-3 fixture: a planted failing repro
// runs in the (fake-docker) sandbox, is confirmed by the two-run rule, stamped,
// and serializes into findings.json with an evidence_exec block (Reproduced).
func TestReproduce_EndToEnd_PlantedBug(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-docker shell shim is POSIX-only")
	}
	// Fake docker that reproduces a deterministic failure (exit 1) every run.
	dir := t.TempDir()
	fake := filepath.Join(dir, "docker")
	require.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\necho 'assertion failed: want 4 got 5'\nexit 1\n"), 0o755))

	cfg := sandbox.DefaultDockerConfig()
	cfg.DockerPath = fake
	backend := sandbox.NewDockerBackend(cfg)

	verdict, ev, err := Reproduce(context.Background(), backend,
		sandbox.RunSpec{Script: "go test ./...", SnapshotDir: dir})
	require.NoError(t, err)
	require.Equal(t, reclib.VerdictConfirmed, verdict, "a deterministic failing repro must be confirmed")

	f := reconcile.JSONFinding{Severity: "HIGH", File: "calc.go", Line: 10, Problem: "off-by-one", Confidence: "MEDIUM"}
	Stamp(&f, verdict, ev)

	data, err := json.Marshal(f)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))

	require.Contains(t, decoded, "evidence_exec", "reproduced finding must carry an evidence_exec block")
	exec := decoded["evidence_exec"].(map[string]any)
	assert.Equal(t, float64(1), exec["exit_code"])
	assert.Contains(t, exec["output_excerpt"], "assertion failed")
	verif := decoded["verification"].(map[string]any)
	assert.Equal(t, "confirmed", verif["verdict"])
	assert.Equal(t, "repro", verif["skeptic"])
}
