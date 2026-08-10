package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/sandbox"
	"github.com/samestrin/atcr/internal/verify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureToolchainGate swaps the toolchain pre-check seam, recording the argv it
// was asked about. Shared by both gate call sites, so the cleanup is mandatory.
func captureToolchainGate(t *testing.T, ret error) *[][]string {
	t.Helper()
	var seen [][]string
	prev := checkOSLevelToolchainFn
	checkOSLevelToolchainFn = func(b sandbox.Backend, cmd []string) error {
		seen = append(seen, cmd)
		return ret
	}
	t.Cleanup(func() { checkOSLevelToolchainFn = prev })
	return &seen
}

// TestResolveExec_GatesToolchainReachability proves the --exec call site actually
// consults the toolchain gate, and hands it the RESOLVED test command.
//
// A gate nobody calls is the failure mode worth testing for here: the check
// itself is unit-tested in internal/sandbox, so the only thing that can silently
// go missing is the wiring.
func TestResolveExec_GatesToolchainReachability(t *testing.T) {
	captureSnapshotGate(t, nil)
	seen := captureToolchainGate(t, nil)

	cmd, proj := execResolveFixture(t)
	_, testCmd, _, err := resolveExec(cmd, proj)
	require.NoError(t, err)

	require.Len(t, *seen, 1, "the toolchain gate must be consulted exactly once")
	assert.Equal(t, testCmd, (*seen)[0],
		"the gate must inspect the command the run will execute, not a re-derived one")
}

// TestResolveExec_ToolchainRefusalIsAUsageError pins the failure CLASS, matching
// its snapshot-gate sibling: a fail-fast usage error (exit 2) before the review
// starts, never a pipeline degradation.
func TestResolveExec_ToolchainRefusalIsAUsageError(t *testing.T) {
	captureSnapshotGate(t, nil)
	captureToolchainGate(t, errors.New(`os-level sandbox cannot reach "go": staged refusal`))

	cmd, proj := execResolveFixture(t)
	_, _, _, err := resolveExec(cmd, proj)

	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, err.Error(), "cannot reach")
}

// TestCheckOSLevelToolchainReachable_DispatchesFailClosed pins the dispatch rules
// at the verify boundary, mirroring its snapshot sibling exactly.
//
// The unrecognized-backend case is the load-bearing one: a decorating wrapper
// reports its OWN Name(), so a name-based pass-through would turn this gate into
// a silent no-op for precisely the shape it exists to check.
func TestCheckOSLevelToolchainReachable_DispatchesFailClosed(t *testing.T) {
	// The REAL function, not the seam: dispatch is the behavior under test, and
	// routing through a stub would assert the stub.
	fn := verify.CheckOSLevelToolchainReachable

	// nil backend: sandboxing is off, nothing to check.
	assert.NoError(t, fn(nil, []string{"go", "test"}))

	// docker: supplies its own image PATH, so host sanitization is irrelevant.
	assert.NoError(t, fn(&nameOnlyBackend{name: "docker"}, []string{"go", "test"}))

	// Anything else is refused rather than skipped. This is the load-bearing
	// case: a decorating wrapper reports its OWN Name(), so a name-based
	// pass-through would turn the gate into a silent no-op for exactly the shape
	// it exists to check — and item 3's pending-warning decorator is precisely
	// such a wrapper.
	err := fn(&nameOnlyBackend{name: "decorating-wrapper"}, []string{"go", "test"})
	require.Error(t, err, "an unidentifiable backend must be refused, never silently skipped")
	assert.Contains(t, err.Error(), "fail-closed")
}

// TestResolveExec_ToolchainGateSkippedForDocker is the negative control, run
// against the REAL gate (the seam is left alone). The check is meaningless for
// the container backend, and one that fired there would refuse perfectly good
// docker runs on the basis of the HOST's PATH — with a PATH deliberately emptied
// here so a leaking gate cannot accidentally pass.
func TestResolveExec_ToolchainGateSkippedForDocker(t *testing.T) {
	captureSnapshotGate(t, nil)
	t.Setenv("PATH", t.TempDir())

	origResolve := resolveExecBackendFn
	resolveExecBackendFn = func(context.Context, bool, *registry.SandboxConfig) (sandbox.Backend, []string, time.Duration, error) {
		return &nameOnlyBackend{name: "docker"}, []string{"go", "test"}, time.Minute, nil
	}
	t.Cleanup(func() { resolveExecBackendFn = origResolve })

	cmd := newVerifyCmd()
	cmd.SetContext(context.Background())
	require.NoError(t, cmd.Flags().Set("exec", "true"))
	proj := &registry.ProjectConfig{Sandbox: &registry.SandboxConfig{
		Image: "alpine:3.20", TestCommand: []string{"go", "test"},
	}}

	_, _, _, err := resolveExec(cmd, proj)
	assert.NoError(t, err, "the docker backend must pass the toolchain gate unconditionally")
}

// TestResolveExec_FallbackWarningFiresOnlyAfterEveryGatePasses is the ordering
// guarantee at the boundary that motivated the deferral.
//
// The resolver used to warn the instant the os-level preflight passed, but
// resolveExec's own gates run afterwards and can still refuse — so an operator
// read "os-level sandbox fallback engaged ... runs_as invoking user" describing a
// run that never executed anything. These two subtests are the positive and
// negative halves of the fix; without the negative one, simply moving the warn
// call would look equally green.
func TestResolveExec_FallbackWarningFiresOnlyAfterEveryGatePasses(t *testing.T) {
	stageEmit := func(t *testing.T) *int {
		t.Helper()
		var fired int
		prev := emitPendingFallbackWarningFn
		emitPendingFallbackWarningFn = func(context.Context, sandbox.Backend) { fired++ }
		t.Cleanup(func() { emitPendingFallbackWarningFn = prev })
		return &fired
	}

	t.Run("all gates pass: warning fires once", func(t *testing.T) {
		captureSnapshotGate(t, nil)
		captureToolchainGate(t, nil)
		fired := stageEmit(t)

		cmd, proj := execResolveFixture(t)
		_, _, _, err := resolveExec(cmd, proj)

		require.NoError(t, err)
		assert.Equal(t, 1, *fired, "a committed run must tell the operator the isolation model changed")
	})

	t.Run("snapshot gate refuses: warning never fires", func(t *testing.T) {
		captureSnapshotGate(t, errors.New("staged snapshot refusal"))
		captureToolchainGate(t, nil)
		fired := stageEmit(t)

		cmd, proj := execResolveFixture(t)
		_, _, _, err := resolveExec(cmd, proj)

		require.Error(t, err)
		assert.Zero(t, *fired, "nothing was engaged, so the refusal is the whole signal")
	})

	t.Run("toolchain gate refuses: warning never fires", func(t *testing.T) {
		captureSnapshotGate(t, nil)
		captureToolchainGate(t, errors.New("staged toolchain refusal"))
		fired := stageEmit(t)

		cmd, proj := execResolveFixture(t)
		_, _, _, err := resolveExec(cmd, proj)

		require.Error(t, err)
		assert.Zero(t, *fired, "a run refused for an unreachable toolchain engaged nothing")
	})
}
