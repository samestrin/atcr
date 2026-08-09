//go:build integration

package verify

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file proves the END of the --auto-fix fallback path against the REAL
// platform sandbox binary — not the resolver's selection of a backend.
//
// Why the distinction is the whole point (AC 03-03, added at the Phase 2 gate):
// RunSandboxedValidation sets RunSpec.Writable: true unconditionally for every
// --auto-fix validation, while Preflight probes with both Writable values. A
// backend that could be SELECTED but not RUN under Writable would pass every
// unit test in autofix_exec_test.go — they all substitute a fake at the
// constructor seam — and then hard-fail on every real validation, after
// --auto-fix had already applied its patch. A test that stops at "the resolver
// returned a non-nil os-level backend" is precisely the assertion that would
// hide it, so this one carries a real command through to an exit code.
//
// Platform scope is deliberate (Phase 4 clarifications, decision 2): AC 03-03
// states no platform or execution-host requirement, and the Linux
// mount/network-namespace primitives were already exercised against real
// bubblewrap in Phase 3. This leg runs wherever the platform binary is usable
// and skips elsewhere, rather than repeating Phase 3's cross-compile-and-sudo
// ritual to re-land a root-run-only record (TD-018).
//
// Every helper below mirrors one established by the Phase 3 proofs
// (internal/sandbox/oslevel_profile_darwin_integration_test.go) — the same
// falsifiability rules apply here, because the failure mode they guard against
// is identical: a denial test that passes while proving nothing.

// skipOrFail skips a genuinely unrunnable case unless
// ATCR_REQUIRE_OSLEVEL_SANDBOX_PROOF is set, in which case it fails. A skip and
// a proof are indistinguishable in `go test` output — both render as a green
// package — so when this run is being taken as AC 03-03 sign-off, "the proof did
// not actually run" must not be mistakable for "the proof passed".
func skipOrFailFallbackProof(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("ATCR_REQUIRE_OSLEVEL_SANDBOX_PROOF") != "" {
		t.Fatalf("ATCR_REQUIRE_OSLEVEL_SANDBOX_PROOF is set, so this containment proof may not be skipped: "+format, args...)
	}
	t.Skipf(format, args...)
}

// trustedToolDirs mirrors internal/sandbox's own resolution rule. Gating on
// exec.LookPath instead would consult $PATH, which production deliberately does
// NOT: a bwrap in /usr/local/bin (source install, Homebrew-on-Linux, Nix) makes
// LookPath succeed, so the suite would not skip, and the resolver's subsequent
// refusal would be reported as a product regression when it is a host condition.
var trustedToolDirs = []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"}

// requireResolvableSandboxTool skips unless the platform's sandbox binary is
// present where production will actually look for it.
func requireResolvableSandboxTool(t *testing.T) {
	t.Helper()
	var tool string
	switch runtime.GOOS {
	case "darwin":
		tool = "sandbox-exec"
	case "linux":
		tool = "bwrap"
	default:
		skipOrFailFallbackProof(t, "no os-level sandbox on %s", runtime.GOOS)
	}
	for _, dir := range trustedToolDirs {
		if info, err := os.Stat(filepath.Join(dir, tool)); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return
		}
	}
	skipOrFailFallbackProof(t, "%s is not present in any trusted tool directory %v, which is the only place the backend resolves it from", tool, trustedToolDirs)
}

// resolveFallbackBackend runs the REAL resolver — no stubbed seam — with Docker
// failing preflight for real, and returns the os-level backend it selects.
//
// A resolution failure here is classified rather than blanket-skipped: the tool
// was already proven present in a trusted directory, so the only legitimate
// skips left are host capabilities the product cannot create (Linux unprivileged
// user namespaces, the case Phase 3 measured on orchestrator.lan). Anything else
// is a regression in this repo and must fail.
func resolveFallbackBackend(t *testing.T, sc *registry.SandboxConfig) sandbox.Backend {
	t.Helper()
	backend, err := ResolveAutoFixSandbox(context.Background(), true, sc)
	if err != nil {
		msg := err.Error()
		for _, hostLimit := range []string{"uid map", "user namespace", "userns", "Permission denied", "operation not permitted"} {
			if strings.Contains(strings.ToLower(msg), strings.ToLower(hostLimit)) {
				skipOrFailFallbackProof(t, "the host cannot create the namespaces the backend requires: %v", err)
				return nil
			}
		}
		require.NoError(t, err, "the sandbox tool resolved from a trusted directory, so a resolver failure is a regression in this repo, not a host limitation")
	}
	require.NotNil(t, backend)
	require.Equal(t, registry.SandboxFallbackOSLevel, backend.Name())
	return backend
}

func fallbackConfig(t *testing.T) *registry.SandboxConfig {
	t.Helper()
	return &registry.SandboxConfig{
		DockerPath:  fakeDocker(t, "exit 1"), // daemon unreachable, for real
		Image:       "alpine:3.20",
		TestCommand: []string{"true"},
		Fallback:    registry.SandboxFallbackOSLevel,
	}
}

// forbiddenFixture creates a secret-bearing directory OUTSIDE every root the
// profile allows, and asserts that placement before returning.
//
// The placement is load-bearing and is NOT t.TempDir(). t.TempDir() resolves
// through $TMPDIR else /tmp — and /tmp is an unconditionally allowed read/write
// root of this very profile. Measured during the 4.8.A review: with `env -u
// TMPDIR` (the launchd/cron/CI shape) the fixture landed in /tmp and the denial
// assertions reported a "containment breach" for a read the sandbox was correct
// to permit. Phase 3 hit the identical trap and solved it the same way.
//
// /var/tmp is world-writable on macOS and appears in no allow rule, so absence
// there is the profile denying it rather than an ambient accident.
func forbiddenFixture(t *testing.T) (secretPath, secretBody string) {
	t.Helper()
	root := "/var/tmp"
	if _, err := os.Stat(root); err != nil {
		skipOrFailFallbackProof(t, "%s is unavailable, so no directory outside the allowed roots can be created: %v", root, err)
	}
	dir, err := os.MkdirTemp(root, "atcr-fallback-forbidden-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	// Assert the PREMISE before any denial is asserted. Without this the test
	// degrades into a tautology whenever the environment moves the directory.
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	for _, allowed := range []string{"/tmp", "/private/tmp", "/var/folders", "/private/var/folders"} {
		allowedResolved, evalErr := filepath.EvalSymlinks(allowed)
		if evalErr != nil {
			continue // not present on this host
		}
		rel, relErr := filepath.Rel(allowedResolved, resolved)
		inside := relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
		require.False(t, inside,
			"the forbidden fixture %q is INSIDE the allowed root %q, so any denial asserted against it would be testing the wrong thing",
			resolved, allowedResolved)
	}

	secretBody = "-----BEGIN OPENSSH PRIVATE KEY-----\nATCR-FALLBACK-CANARY\n"
	secretPath = filepath.Join(dir, "id_ed25519")
	require.NoError(t, os.WriteFile(secretPath, []byte(secretBody), 0o600))
	return secretPath, secretBody
}

// runUnsandboxed runs a probe OUTSIDE the sandbox, as the control half of a
// denial assertion. Without it, "the probe failed" is not evidence of
// containment — it is equally consistent with a broken probe, a missing binary,
// or a host with no network at all.
func runUnsandboxed(t *testing.T, script string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if err != nil && !errors.As(err, &ee) {
		require.NoError(t, err, "the unsandboxed control probe could not be started; output: %s", out)
	}
	return cmd.ProcessState.ExitCode(), string(out)
}

// loopbackListener opens a real listener the sandboxed probe can dial, so a
// blocked connection is attributable to the sandbox rather than to the absence
// of anything listening.
func loopbackListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	return port
}

// TestIntegration_AutoFixFallback_WritableValidationCompletes is the end-to-end
// bar: configure the fallback, fail Docker's preflight for real, and let the
// resolver hand back a backend the auto-fix validation adapter then actually
// runs — with the Writable: true spec that adapter always builds.
func TestIntegration_AutoFixFallback_WritableValidationCompletes(t *testing.T) {
	requireResolvableSandboxTool(t)

	snapshot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(snapshot, "source.txt"), []byte("original\n"), 0o644))

	backend := resolveFallbackBackend(t, fallbackConfig(t))

	// A validate_command that WRITES inside its working directory — the shape
	// that distinguishes Writable: true from the read-only --exec path, and the
	// shape every non-Go builder has (npm -> dist/, cargo -> target/).
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
	// which is the entire justification for permitting writes at all.
	_, statErr := os.Stat(filepath.Join(snapshot, "artifact.txt"))
	assert.True(t, os.IsNotExist(statErr),
		"the writable overlay must not mutate the caller's snapshot directory")
}

// TestIntegration_AutoFixFallback_ContainmentHoldsOnTheResolvedBackend re-runs
// the denials through the RESOLVER-SELECTED backend rather than a directly
// constructed one, so a resolver that built the backend with a widened config
// is caught here rather than in review.
func TestIntegration_AutoFixFallback_ContainmentHoldsOnTheResolvedBackend(t *testing.T) {
	requireResolvableSandboxTool(t)

	snapshot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(snapshot, "keep.txt"), []byte("x\n"), 0o644))
	secret, secretBody := forbiddenFixture(t)

	backend := resolveFallbackBackend(t, fallbackConfig(t))

	// Positive control: the read genuinely succeeds outside the sandbox, so the
	// denial below cannot be satisfied by a broken probe or a missing fixture.
	hostCode, hostOut := runUnsandboxed(t, "cat "+secret)
	require.Equal(t, 0, hostCode, "the fixture must be readable on the host, or the denial proves nothing")
	require.Contains(t, hostOut, "ATCR-FALLBACK-CANARY")

	res, runErr := RunSandboxedValidation(
		context.Background(),
		backend,
		[]string{"/bin/sh", "-c", "cat " + secret},
		snapshot,
		60*time.Second,
	)
	require.NoError(t, runErr, "a denied read is a workload result, never a backend fault")
	assert.NotEqual(t, 0, res.ExitCode, "reading outside the allowed roots must fail")
	assert.False(t, res.TimedOut, "a timeout is not a denial")
	assert.NotContains(t, res.Stdout+res.Stderr, "ATCR-FALLBACK-CANARY",
		"the secret's contents must never reach the caller")
	if runtime.GOOS == "darwin" {
		assert.Contains(t, res.Stdout+res.Stderr, "Operation not permitted",
			"the failure must carry the sandbox's own denial diagnostic, not merely a non-zero exit: %s", res.Stdout+res.Stderr)
	} else if runtime.GOOS == "linux" {
		output := res.Stdout + res.Stderr
		found := false
		for _, want := range []string{"No such file or directory", "Directory nonexistent"} {
			if strings.Contains(output, want) {
				found = true
				break
			}
		}
		assert.True(t, found,
			"the failure must carry the sandbox's own ENOENT-class denial diagnostic, not merely a non-zero exit: %s", output)
		assert.Contains(t, output, secret,
			"the ENOENT must name the forbidden path under test")
	}
	assert.NotContains(t, res.Stdout+res.Stderr, secretBody)
}

// TestIntegration_AutoFixFallback_NetworkEgressIsBlocked dials a listener this
// test itself opened. Dialing a public address instead would be vacuous: on an
// offline laptop, behind a firewall that RSTs, or in a CI runner without
// outbound access, the dial fails identically with containment fully removed.
func TestIntegration_AutoFixFallback_NetworkEgressIsBlocked(t *testing.T) {
	requireResolvableSandboxTool(t)

	snapshot := t.TempDir()
	port := loopbackListener(t)
	// nc is used rather than /bin/sh's /dev/tcp because dash — /bin/sh on Debian
	// and Ubuntu, the other supported platform — has no /dev/tcp redirection at
	// all, which would make this leg vacuous by construction there.
	probe := "nc -z -w 5 127.0.0.1 " + port + " && echo CONNECTED || echo REFUSED"

	// Positive control: the identical probe MUST succeed unsandboxed.
	hostCode, hostOut := runUnsandboxed(t, probe)
	if hostCode != 0 || !strings.Contains(hostOut, "CONNECTED") {
		skipOrFailFallbackProof(t, "the unsandboxed control probe could not reach the test's own listener (nc missing or unusable), so a blocked dial would prove nothing: rc=%d out=%s", hostCode, hostOut)
	}

	backend := resolveFallbackBackend(t, fallbackConfig(t))
	res, runErr := RunSandboxedValidation(
		context.Background(),
		backend,
		[]string{"/bin/sh", "-c", probe},
		snapshot,
		60*time.Second,
	)
	require.NoError(t, runErr)
	assert.False(t, res.TimedOut, "a timeout is not evidence of a blocked connection")
	assert.NotContains(t, res.Stdout+res.Stderr, "CONNECTED",
		"outbound egress to a live listener must be blocked under the fallback")
}

// TestIntegration_AutoFixFallback_HostTmpIsReadableAndWritable records a
// containment REDUCTION relative to the Docker backend, deliberately, as a
// passing assertion rather than a comment.
//
// Docker's /tmp is a container tmpfs; the os-level profile grants the host's
// real /tmp read and write, as the invoking user. That is a documented property
// of the platform profile (Phase 2), not a defect introduced here — but it is a
// difference an operator opting into the fallback inherits, and the 4.8.A review
// found it invisible: named in no warning and probed by no test, so a future
// widening could not be told apart from it. Pinning it means a change to the
// carve-out breaks a test instead of passing silently. See TD-025.
func TestIntegration_AutoFixFallback_HostTmpIsReadableAndWritable(t *testing.T) {
	requireResolvableSandboxTool(t)

	snapshot := t.TempDir()
	// os.CreateTemp, not a fixed name: os.WriteFile follows symlinks, so a
	// pre-existing symlink at a predictable path in world-writable /tmp would be
	// overwritten as the invoking user, and two concurrent runs would collide and
	// delete each other's marker in cleanup.
	f, err := os.CreateTemp("/tmp", "atcr-fallback-tmp-probe-*.txt")
	require.NoError(t, err)
	marker := f.Name()
	_, err = f.WriteString("HOSTTMP\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	t.Cleanup(func() { _ = os.Remove(marker) })

	backend := resolveFallbackBackend(t, fallbackConfig(t))
	res, runErr := RunSandboxedValidation(
		context.Background(),
		backend,
		[]string{"/bin/sh", "-c", "cat " + marker},
		snapshot,
		60*time.Second,
	)
	require.NoError(t, runErr)
	assert.Equal(t, 0, res.ExitCode,
		"host /tmp is readable under the os-level fallback — if this now FAILS the carve-out changed, which is a behavior change to document, not a test to delete")
	assert.Contains(t, res.Stdout, "HOSTTMP")
}
