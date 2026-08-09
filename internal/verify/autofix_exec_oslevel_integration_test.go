//go:build integration

package verify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file proves the END of the --auto-fix fallback path against the REAL
// platform sandbox binary, not the resolver's selection of a backend.
//
// Why the distinction is the whole point (AC 03-03, added at the Phase 2 gate):
// RunSandboxedValidation sets RunSpec.Writable: true unconditionally for every
// --auto-fix validation (sandboxvalidate.go), while Preflight probes with both
// Writable values. A backend that could be SELECTED but not RUN under Writable
// would pass every unit test in autofix_exec_test.go — they all substitute a
// fake at the constructor seam — and then hard-fail on every real validation,
// after --auto-fix has already applied its patch. A test that stops at "the
// resolver returned a non-nil os-level backend" is precisely the assertion that
// would hide it, so this one carries a real command through to an exit code.
//
// Platform scope is deliberate (Phase 4 clarifications, decision 2): AC 03-03
// states no platform or execution-host requirement, and the Linux
// mount/network-namespace primitives were already exercised on a real host in
// Phase 3. This leg runs wherever the platform binary exists and skips
// elsewhere, rather than repeating Phase 3's cross-compile-and-sudo ritual to
// re-land a root-run-only record (TD-018).

// requireRealSandboxTool skips unless the running platform's sandbox binary is
// actually present and the platform is supported at all.
func requireRealSandboxTool(t *testing.T) {
	t.Helper()
	var tool string
	switch runtime.GOOS {
	case "darwin":
		tool = "sandbox-exec"
	case "linux":
		tool = "bwrap"
	default:
		t.Skipf("no os-level sandbox on %s", runtime.GOOS)
	}
	if _, err := exec.LookPath(tool); err != nil {
		t.Skipf("%s not installed on this host", tool)
	}
}

// TestIntegration_AutoFixFallback_WritableValidationCompletes is the end-to-end
// bar: config the fallback, fail Docker's preflight for real, and let the
// resolver hand back a backend the auto-fix validation adapter then actually
// runs — with the Writable: true spec that adapter always builds.
func TestIntegration_AutoFixFallback_WritableValidationCompletes(t *testing.T) {
	requireRealSandboxTool(t)

	snapshot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(snapshot, "source.txt"), []byte("original\n"), 0o644))

	sc := &registry.SandboxConfig{
		DockerPath:  fakeDocker(t, "exit 1"), // daemon unreachable, for real
		Image:       "alpine:3.20",
		TestCommand: []string{"true"},
		Fallback:    registry.SandboxFallbackOSLevel,
	}

	// The seam is NOT stubbed here: this must construct and preflight the real
	// backend, or it proves nothing the unit tests do not already prove.
	backend, err := ResolveAutoFixSandbox(context.Background(), true, sc)
	require.NoError(t, err, "the real os-level backend must preflight on a host that has the tool")
	require.NotNil(t, backend)
	require.Equal(t, registry.SandboxFallbackOSLevel, backend.Name())

	// A validate_command that WRITES inside its working directory — the shape
	// that distinguishes Writable: true from the read-only --exec path, and the
	// shape a non-Go builder (npm -> dist/, cargo -> target/) always has.
	res, runErr := RunSandboxedValidation(
		context.Background(),
		backend,
		[]string{"/bin/sh", "-c", "printf built > artifact.txt && cat source.txt"},
		snapshot,
		60*time.Second,
	)

	require.NoError(t, runErr, "a writable validation must COMPLETE under the fallback, not fault")
	assert.NoError(t, res.StartError)
	assert.False(t, res.TimedOut)
	assert.Equal(t, 0, res.ExitCode, "the validation command must exit cleanly: %s", res.Stdout+res.Stderr)
	assert.True(t, res.Passed())
	assert.Contains(t, res.Stdout, "original",
		"the snapshot's contents must be readable inside the sandbox")

	// The host snapshot stays pristine: the write landed in the ephemeral copy,
	// which is the entire justification for allowing writes at all.
	_, statErr := os.Stat(filepath.Join(snapshot, "artifact.txt"))
	assert.True(t, os.IsNotExist(statErr),
		"the writable overlay must not mutate the caller's snapshot directory")
}

// TestIntegration_AutoFixFallback_ContainmentHoldsOnTheResolvedBackend guards
// against the fallback silently resolving to something less contained than what
// Phase 3 proved. It re-runs the two denials through the resolver-selected
// backend rather than a directly constructed one, so a resolver that built the
// backend with a widened config would be caught here rather than in review.
func TestIntegration_AutoFixFallback_ContainmentHoldsOnTheResolvedBackend(t *testing.T) {
	requireRealSandboxTool(t)

	snapshot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(snapshot, "keep.txt"), []byte("x\n"), 0o644))

	forbidden := filepath.Join(t.TempDir(), "forbidden")
	require.NoError(t, os.MkdirAll(forbidden, 0o755))
	secret := filepath.Join(forbidden, "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("TOPSECRET\n"), 0o600))

	sc := &registry.SandboxConfig{
		DockerPath:  fakeDocker(t, "exit 1"),
		Image:       "alpine:3.20",
		TestCommand: []string{"true"},
		Fallback:    registry.SandboxFallbackOSLevel,
	}
	backend, err := ResolveAutoFixSandbox(context.Background(), true, sc)
	require.NoError(t, err)
	require.NotNil(t, backend)

	// Positive control, so a denial cannot pass vacuously. Every assertion below
	// is of the form "this failed" — which a broken shell, a missing fixture, or a
	// backend that never spawned would satisfy just as well as real containment.
	// Prove the read genuinely succeeds OUTSIDE the sandbox first.
	hostRead, hostErr := os.ReadFile(secret)
	require.NoError(t, hostErr, "the fixture must be readable on the host, or the denial below proves nothing")
	require.Contains(t, string(hostRead), "TOPSECRET")

	res, runErr := RunSandboxedValidation(
		context.Background(),
		backend,
		[]string{"/bin/sh", "-c", "cat " + secret},
		snapshot,
		60*time.Second,
	)
	require.NoError(t, runErr, "a denied read is a workload result, never a backend fault")
	assert.NotEqual(t, 0, res.ExitCode, "reading outside the allowed roots must fail")
	assert.NotContains(t, res.Stdout, "TOPSECRET",
		"the secret's contents must never reach the caller")

	netRes, netErr := RunSandboxedValidation(
		context.Background(),
		backend,
		[]string{"/bin/sh", "-c", "exec 3<>/dev/tcp/93.184.216.34/80"},
		snapshot,
		60*time.Second,
	)
	require.NoError(t, netErr)
	assert.NotEqual(t, 0, netRes.ExitCode, "outbound network egress must be blocked")
}
