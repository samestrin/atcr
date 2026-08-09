//go:build integration && darwin

package sandbox

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC 02-03 — the macOS half of this sprint's ONLY kernel-level containment
// proof. Every other test of this backend asserts the SHAPE of a generated
// profile string; nothing but a real `sandbox-exec` run can show that the
// kernel actually enforces it, and a profile can be perfectly well-formed and
// contain nothing.
//
// Two design rules, both from AC 02-03's normative preamble:
//
//  1. Drive osLevelBackend.Run, NOT sandboxExecProfile plus a hand-rolled
//     exec.Command. A test that execs the binary itself would prove the PROFILE
//     contains while the backend stayed inert — which is exactly the state this
//     sprint was in until task 3.0 wired osLevelContainmentArgs. Driving Run
//     means the wiring, the argv assembly, the env allowlist, the scratch
//     partition, and the profile are all under test together.
//  2. Every denial is paired with a positive control. A profile that denied
//     everything — including exec — would pass every negative assertion
//     vacuously and look like perfect containment.
//
// File-split note: Go allows exactly one //go:build line per file, so the
// darwin leg (`&& darwin`) and the Linux leg (`&& linux`) cannot share a file.
// The name is deliberate: stripping _test.go leaves `..._darwin_integration`,
// whose last underscore element is `integration` rather than a GOOS, so Go adds
// no implicit constraint and the explicit build line is the sole gate.

// darwinIntegrationBackend returns a preflighted backend, or skips.
//
// Preflight is the skip guard rather than a bare exec.LookPath: it also covers
// a host where the binary exists but cannot actually contain (Apple relocating
// or neutering sandbox-exec, an unexpected profile rejection). Skipping there is
// correct — the run degrades coverage — but it must be a skip, never a failure,
// per Edge Case 1.
func darwinIntegrationBackend(t *testing.T) *osLevelBackend {
	t.Helper()
	b := NewOSLevelBackend(DefaultOSLevelConfig())
	if err := b.Preflight(context.Background()); err != nil {
		t.Skipf("os-level sandbox not usable on this host, skipping containment proof: %v", err)
	}
	return b
}

// forbiddenDir creates a stand-in ~/.ssh outside every allowed root, pre-seeded
// with a known secret.
//
// It is a stand-in and never the operator's real $HOME — a hard requirement of
// AC 02-03's security section, not a stylistic one. The entire premise of this
// file is "run code that tries to escape", so a profile bug must not be able to
// damage the machine running the test.
//
// It lives under os.MkdirTemp rather than t.TempDir() because t.TempDir() is
// removed by the test framework and this directory's continued ABSENCE of a
// file is the assertion.
func forbiddenDir(t *testing.T) (dir, secretPath, secretBody string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "atcr-forbidden-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	secretBody = "-----BEGIN OPENSSH PRIVATE KEY-----\nATCR-INTEGRATION-CANARY\n"
	secretPath = filepath.Join(dir, "id_ed25519")
	require.NoError(t, os.WriteFile(secretPath, []byte(secretBody), 0o600))
	return dir, secretPath, secretBody
}

// loopbackListener opens a listener on 127.0.0.1 and returns its port. It is
// closed when the test ends.
func loopbackListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	// Keep accepting so a connect completes rather than racing the backlog.
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

// runUnsandboxed runs a shell probe OUTSIDE the sandbox, as the control half of
// a network assertion. Without it, "the dial failed" is not evidence of
// containment — it is equally consistent with a host that has no network.
func runUnsandboxed(t *testing.T, script string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/bin/sh", "-c", script).CombinedOutput()
	require.NoError(t, err, "the unsandboxed control probe must run; output: %s", out)
	return string(out)
}

func TestIntegration_OSLevelDarwin_ForbiddenWriteOutsideAllowedRootsFails(t *testing.T) {
	// AC 02-03 Scenario 1.
	b := darwinIntegrationBackend(t)
	forbidden, _, _ := forbiddenDir(t)
	target := filepath.Join(forbidden, "id_rsa")

	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"/bin/sh", "-c", "echo pwned > " + target},
		SnapshotDir: t.TempDir(),
		Timeout:     10 * time.Second,
	})
	require.NoError(t, err, "the run itself must complete; output: %s", res.Output)
	assert.NotEqual(t, 0, res.ExitCode,
		"sandbox-exec profile failed to block write to %s — containment breach; output: %s", target, res.Output)

	_, statErr := os.Stat(target)
	require.True(t, os.IsNotExist(statErr),
		"sandbox-exec profile failed to block write to %s — containment breach: the file exists on the host", target)
}

func TestIntegration_OSLevelDarwin_ReadOutsideAllowedRootsFailsWithoutLeaking(t *testing.T) {
	// AC 02-03 Scenario 5 — the exfiltration-via-read vector, which a
	// write-only proof would miss entirely.
	b := darwinIntegrationBackend(t)
	_, secretPath, secretBody := forbiddenDir(t)

	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"/bin/sh", "-c", "cat " + secretPath},
		SnapshotDir: t.TempDir(),
		Timeout:     10 * time.Second,
	})
	require.NoError(t, err, "the run itself must complete; output: %s", res.Output)
	assert.NotEqual(t, 0, res.ExitCode, "reading outside the allowed roots must fail; output: %s", res.Output)
	assert.NotContains(t, res.Output, "ATCR-INTEGRATION-CANARY",
		"secret content leaked into captured output — containment breach")
	assert.NotContains(t, res.Output, strings.TrimSpace(secretBody))
}

func TestIntegration_OSLevelDarwin_WriteInsideAllowedRootsSucceeds(t *testing.T) {
	// AC 02-03 Scenarios 2 and 4 — the positive controls. Without these, a
	// profile that denied literally everything would pass every other test in
	// this file and read as flawless containment.
	b := darwinIntegrationBackend(t)

	t.Run("writable snapshot copy", func(t *testing.T) {
		// Writable:true is the ONLY shape --auto-fix ever uses
		// (internal/verify/sandboxvalidate.go:72 hardcodes it), so the permitted
		// write is proven on the path that actually ships.
		snapshot := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(snapshot, "seed.txt"), []byte("seeded"), 0o644))

		res, err := b.Run(context.Background(), RunSpec{
			Command:     []string{"/bin/sh", "-c", "cat seed.txt && echo ok > proof.txt && cat proof.txt"},
			SnapshotDir: snapshot,
			Writable:    true,
			Timeout:     10 * time.Second,
		})
		require.NoError(t, err, "output: %s", res.Output)
		require.Equal(t, 0, res.ExitCode, "a write into the writable copy must succeed; output: %s", res.Output)
		assert.Contains(t, res.Output, "seeded", "the snapshot must be readable through the copy")
		assert.Contains(t, res.Output, "ok", "the copy must be genuinely writable")

		// AC 02-03's preamble rule 2: a writable overlay that leaks back to the
		// snapshot is a containment failure that no unit test can detect.
		assert.NoFileExists(t, filepath.Join(snapshot, "proof.txt"),
			"a write into the ephemeral copy must never reach the host snapshot")
	})

	t.Run("tmp", func(t *testing.T) {
		// The second permitted root. /tmp is a symlink to /private/tmp on macOS,
		// and a previous review round found a profile that silently matched
		// nothing because only one spelling was granted — so this control is
		// specifically load-bearing, not decorative.
		marker := filepath.Join("/tmp", "atcr-sbx-proof-"+randHex(8)+".txt")
		t.Cleanup(func() { _ = os.Remove(marker) })

		res, err := b.Run(context.Background(), RunSpec{
			Command:     []string{"/bin/sh", "-c", "echo ok > " + marker + " && cat " + marker},
			SnapshotDir: t.TempDir(),
			Timeout:     10 * time.Second,
		})
		require.NoError(t, err, "output: %s", res.Output)
		require.Equal(t, 0, res.ExitCode, "a write into /tmp must succeed; output: %s", res.Output)
		assert.Contains(t, res.Output, "ok")
		assert.FileExists(t, marker)
	})
}

func TestIntegration_OSLevelDarwin_RealToolchainRuns(t *testing.T) {
	// The strongest positive control: an actual tool, not just `sh`. A previous
	// review round measured `git --version` FAILING under this profile
	// (xcode-select could not read /var/select/developer_dir, an alias-grant
	// bug), while every containment assertion still passed — the profile was
	// "secure" only because it was unusable. AC 02-03's preamble requires this
	// case for exactly that reason.
	b := darwinIntegrationBackend(t)

	for _, tool := range []struct{ name, argv string }{
		{"git", "git --version"},
		{"go", "go version"},
	} {
		t.Run(tool.name, func(t *testing.T) {
			res, err := b.Run(context.Background(), RunSpec{
				Command:     []string{"/bin/sh", "-c", tool.argv + " 2>&1"},
				SnapshotDir: t.TempDir(),
				Writable:    true,
				Timeout:     20 * time.Second,
			})
			require.NoError(t, err, "output: %s", res.Output)
			assert.Equal(t, 0, res.ExitCode,
				"%s must be runnable under the profile — a sandbox nothing can run in is not containment; output: %s",
				tool.name, res.Output)
			assert.NotContains(t, res.Output, "Operation not permitted",
				"the profile denied something %s needs; output: %s", tool.name, res.Output)
		})
	}
}

func TestIntegration_OSLevelDarwin_NetworkEgressIsBlocked(t *testing.T) {
	// AC 02-03 Scenario 3. The probe must distinguish "the sandbox blocked it"
	// from "this host has no network anyway", so it runs UNSANDBOXED first as a
	// control and skips if that control cannot reach the target either.
	//
	// The target is a loopback listener rather than a public address: it depends
	// on no external service, cannot exfiltrate anything, and is the strictest
	// possible test of the deny — a profile that blocked only off-host traffic
	// would pass against an internet target and fail here.
	b := darwinIntegrationBackend(t)

	port := loopbackListener(t)
	probe := "nc -z -w 2 127.0.0.1 " + port + " && echo CONNECTED || echo BLOCKED"

	control := runUnsandboxed(t, probe)
	if !strings.Contains(control, "CONNECTED") {
		t.Skipf("unsandboxed control could not reach the loopback listener (%q); "+
			"cannot distinguish sandbox denial from host policy", strings.TrimSpace(control))
	}

	res, runErr := b.Run(context.Background(), RunSpec{
		Command:     []string{"/bin/sh", "-c", probe},
		SnapshotDir: t.TempDir(),
		Timeout:     15 * time.Second,
	})
	require.NoError(t, runErr, "output: %s", res.Output)
	assert.NotContains(t, res.Output, "CONNECTED",
		"outbound network egress was NOT blocked — containment breach; output: %s", res.Output)
	assert.Contains(t, res.Output, "BLOCKED", "output: %s", res.Output)
}

func TestIntegration_OSLevelDarwin_WritableOverlayDoesNotWidenTheBoundary(t *testing.T) {
	// AC 02-03 Edge Case 2. The writable carve-out is the one part of the
	// profile that grants write access, so it is the most plausible place for an
	// over-broad rule to hide. Re-run the two denials with Writable:true and
	// require identical outcomes to the read-only case.
	b := darwinIntegrationBackend(t)
	forbidden, secretPath, _ := forbiddenDir(t)
	target := filepath.Join(forbidden, "id_rsa")

	res, err := b.Run(context.Background(), RunSpec{
		Command: []string{"/bin/sh", "-c",
			"echo pwned > " + target + " 2>&1; cat " + secretPath + " 2>&1; true"},
		SnapshotDir: t.TempDir(),
		Writable:    true,
		Timeout:     10 * time.Second,
	})
	require.NoError(t, err, "output: %s", res.Output)

	_, statErr := os.Stat(target)
	assert.True(t, os.IsNotExist(statErr),
		"the writable carve-out widened the boundary: %s was created — containment breach", target)
	assert.NotContains(t, res.Output, "ATCR-INTEGRATION-CANARY",
		"the writable carve-out widened the boundary: the secret was readable — containment breach")
}
