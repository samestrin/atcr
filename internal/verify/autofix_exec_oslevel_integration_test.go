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
		// The only legitimate skip is a host capability the product cannot
		// create: Linux unprivileged user namespaces (the case Phase 3 measured
		// on orchestrator.lan). Classify it by the bubblewrap-specific
		// diagnostics the Linux leg curates (internal/sandbox's
		// usernsDenialMarkers, matched case-sensitively as there), on Linux
		// only. The bare "Permission denied" / "operation not permitted"
		// substrings this list used to carry are the MOST LIKELY text of a
		// genuine regression on this path — verifyExecutable rejecting a mode,
		// an unwritable scratch dir, sandbox-exec refusing the generated
		// profile — so matching them converted exactly the containment failures
		// this file exists to catch into skips, upstream of
		// ATCR_REQUIRE_OSLEVEL_SANDBOX_PROOF. They now fail, as regressions.
		if runtime.GOOS == "linux" {
			msg := err.Error()
			for _, hostLimit := range []string{"setting up uid map", "No permissions to create new namespace", "Creating new namespace failed", "loopback: Failed RTM_NEWADDR"} {
				if strings.Contains(msg, hostLimit) {
					skipOrFailFallbackProof(t, "the host cannot create the namespaces the backend requires: %v", err)
					return nil
				}
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
	//
	// The probe's exit code IS the signal, mirroring both Phase 3 sandbox legs:
	// exit 97 reserves "nc missing/unexecutable inside the sandbox" (a broken
	// probe, not containment), `exec` makes nc's own code the workload's (0 =
	// connected, non-zero = refused). A shell-authored token (&& echo CONNECTED
	// || echo REFUSED) is produced by ANY failure and always exits 0, which is
	// the anti-pattern those legs document — it passes with containment fully
	// removed.
	probe := "command -v nc >/dev/null 2>&1 || exit 97; exec nc -z -w 2 127.0.0.1 " + port

	// Positive control: the identical probe MUST succeed unsandboxed.
	hostCode, hostOut := runUnsandboxed(t, probe)
	if hostCode != 0 {
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
	require.NotEqual(t, 97, res.ExitCode,
		"nc was missing or unexecutable INSIDE the sandbox, so a blocked dial proves nothing")
	assert.NotEqual(t, 0, res.ExitCode,
		"outbound egress to a live listener must be blocked under the fallback")

	// Post-run control: the listener must still be reachable unsandboxed, or the
	// blocked dial above is attributable to a listener that died mid-test.
	postCode, postOut := runUnsandboxed(t, probe)
	require.Equal(t, 0, postCode,
		"the test's own listener died mid-test, so the blocked dial proves nothing: %s", postOut)
}

// TestIntegration_AutoFixFallback_HostTmpIsNotReachable pins the host /tmp
// boundary relative to the Docker backend, as a passing assertion rather than a
// comment, so a future widening breaks a test instead of passing silently.
//
// Docker's /tmp is a container tmpfs. Under the os-level fallback the host's
// /tmp is out of reach on BOTH platforms, by two different mechanisms:
//
//   - Linux: bwrapArgs mounts --tmpfs /tmp unconditionally, so the host's /tmp is
//     invisible and a sandboxed /tmp write lands in the ephemeral tmpfs.
//   - darwin: darwinTmpDirs grants the host temp roots METADATA scope only —
//     enough for path resolution, not read-data and not write. The per-run
//     scratch tree is the only writable subtree, and TMPDIR points into it.
//
// HISTORY, kept because it is the reason this test exists in this shape: darwin
// originally granted the host's real /tmp read AND write, and this test asserted
// that. The unconditional write grant was removed on 2026-08-09 (5f6a952), and
// internal/sandbox's sibling subtest was aligned with it (3ee6b12) — but this one
// was not, so the two packages disagreed about the containment boundary while
// both suites stayed green, because no CI job built the integration tag. The
// original comment's own instruction for that situation was "a behavior change to
// document, not a test to delete", which is what the darwin branch below now
// does. See TD-025.
func TestIntegration_AutoFixFallback_HostTmpIsNotReachable(t *testing.T) {
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

	// The write half of the name: a probe is only AndWritable if a write is
	// actually issued and its landing place asserted on the host.
	writeRes, writeRunErr := RunSandboxedValidation(
		context.Background(),
		backend,
		[]string{"/bin/sh", "-c", "printf 'SANDBOXED-WRITE\n' >> " + marker},
		snapshot,
		60*time.Second,
	)
	require.NoError(t, writeRunErr)
	hostBody, readErr := os.ReadFile(marker)
	require.NoError(t, readErr)

	if runtime.GOOS == "darwin" {
		// The carve-out this test was written against is GONE, and its own
		// instruction for that case was "a behavior change to document, not a test
		// to delete" — so it is documented here rather than deleted.
		//
		// darwinTmpDirs (oslevel_profile.go) now grants the host temp roots
		// METADATA scope only: enough for path resolution, not read-data and not
		// write. The per-run scratch tree is the only writable subtree. Measured
		// against the real sandbox-exec: `cat /tmp/<marker>` returns
		// "Operation not permitted".
		//
		// internal/sandbox's sibling subtest was aligned when the grant was dropped
		// (3ee6b12); this one was missed, so the two packages disagreed about the
		// containment boundary until a CI job ran both.
		assert.NotEqual(t, 0, res.ExitCode,
			"host /tmp carries metadata scope only — a read of its CONTENT must fail closed; output: %s", res.Stdout)
		assert.NotContains(t, res.Stdout, "HOSTTMP")
		assert.NotEqual(t, 0, writeRes.ExitCode,
			"host /tmp has no write rule — a write must fail closed; output: %s", writeRes.Stdout)
		assert.NotContains(t, string(hostBody), "SANDBOXED-WRITE",
			"a sandboxed write reached the host's real /tmp — containment breach")

		// Paired positive control: without it, a backend that denied EVERYTHING
		// (including exec) would satisfy every assertion above vacuously. A
		// workload following the environment must still be able to write.
		envRes, envErr := RunSandboxedValidation(
			context.Background(),
			backend,
			[]string{"/bin/sh", "-c", `printf 'SCRATCH-OK\n' > "$TMPDIR/atcr-fallback-scratch.txt" && cat "$TMPDIR/atcr-fallback-scratch.txt"`},
			snapshot,
			60*time.Second,
		)
		require.NoError(t, envErr)
		assert.Equal(t, 0, envRes.ExitCode,
			"a write through TMPDIR (the scratch tree) must succeed; output: %s", envRes.Stdout)
		assert.Contains(t, envRes.Stdout, "SCRATCH-OK")
	} else if runtime.GOOS == "linux" {
		assert.NotEqual(t, 0, res.ExitCode,
			"on Linux the host /tmp hides behind an ephemeral tmpfs — the marker must NOT be readable")
		assert.NotContains(t, res.Stdout, "HOSTTMP")
		assert.NotContains(t, string(hostBody), "SANDBOXED-WRITE",
			"this /tmp write must land in the ephemeral tmpfs, not the host's /tmp")
	}
}
