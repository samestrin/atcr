//go:build integration && linux

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

// AC 02-04 — the Linux half of this sprint's ONLY kernel-level containment
// proof, and per the AC the story's sole accepted enforcement evidence on this
// platform. Everything else about `bwrapArgs` is an assertion about the SHAPE of
// an argument list; only a real `bwrap` run shows the kernel enforcing it.
//
// This file is a SEPARATE file from the darwin leg, not a section of it: Go
// permits exactly one //go:build line per file and the two constraints
// (`&& linux` / `&& darwin`) are mutually exclusive. Stripping _test.go leaves
// `..._linux_integration`, whose last underscore element is `integration` rather
// than a GOOS, so Go adds no implicit constraint and the explicit build line is
// the sole gate.
//
// The containment model differs from macOS in a way the assertions have to
// respect, so these are not the darwin tests with the names changed:
//
//   - Forbidden paths are INVISIBLE rather than permission-denied. bwrap builds
//     a fresh mount namespace containing only what was bound, so an unbound path
//     produces "No such file or directory", not "Operation not permitted".
//   - /tmp is an ephemeral tmpfs (`--tmpfs /tmp`), not a bind of the host's. A
//     file written to /tmp inside the sandbox therefore must NOT appear on the
//     host — the opposite of the darwin expectation — so that control is read
//     back from inside the sandbox.
//   - Network egress fails structurally: `--unshare-net` leaves a namespace with
//     only loopback and no routes.

// linuxIntegrationBackend returns a preflighted backend, or skips.
//
// Preflight is the skip guard because it covers BOTH failure modes AC 02-04
// Edge Case 1 requires: a missing `bwrap` binary, and a binary that is present
// but cannot isolate because unprivileged user namespaces are disabled or
// AppArmor-restricted (common on Ubuntu 24.04+ and in nested CI containers). A
// bare exec.LookPath would catch only the first and would turn the second into
// a hard failure of a machine that simply cannot run this test.
func linuxIntegrationBackend(t *testing.T) *osLevelBackend {
	t.Helper()
	b := NewOSLevelBackend(DefaultOSLevelConfig())
	if err := b.Preflight(context.Background()); err != nil {
		t.Skipf("os-level sandbox not usable on this host (bwrap missing, or user namespaces "+
			"disabled/restricted), skipping containment proof: %v", err)
	}
	return b
}

// forbiddenDir creates a stand-in ~/.ssh outside every bound root, pre-seeded
// with a known secret.
//
// A stand-in and never the real $HOME — a hard requirement of AC 02-04's
// security section rather than a stylistic one, since the premise of this file
// is running code that tries to escape. It uses os.MkdirTemp rather than
// t.TempDir() because the framework removes the latter, and this directory's
// continued ABSENCE of a file is itself the assertion.
//
// It is placed under /var/tmp rather than the default /tmp, and that placement
// is what makes the denial MEAN anything on Linux. bwrap mounts an ephemeral
// tmpfs over /tmp, so a stand-in under /tmp would be invisible merely because
// its parent was replaced — the test would pass with the entire bind allow-list
// removed. /var is bound nowhere (linuxRequiredRoots is /usr; linuxOptionalRoots
// is /bin,/sbin,/lib,/lib64), so a path under /var/tmp is absent specifically
// because the allow-list model did not admit it, which is the property under
// test. Measured: a stand-in under /tmp produced "Directory nonexistent" from
// the tmpfs, indistinguishable from a real containment result.
func forbiddenDir(t *testing.T) (dir, secretPath, secretBody string) {
	t.Helper()
	root := "/var/tmp"
	if _, err := os.Stat(root); err != nil {
		root = "" // fall back to the default temp dir on an unusual host
	}
	dir, err := os.MkdirTemp(root, "atcr-forbidden-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	secretBody = "-----BEGIN OPENSSH PRIVATE KEY-----\nATCR-INTEGRATION-CANARY\n"
	secretPath = filepath.Join(dir, "id_ed25519")
	require.NoError(t, os.WriteFile(secretPath, []byte(secretBody), 0o600))
	return dir, secretPath, secretBody
}

func TestIntegration_OSLevelLinux_ForbiddenWriteOutsideBoundRootsFails(t *testing.T) {
	// AC 02-04 Scenario 1.
	b := linuxIntegrationBackend(t)
	forbidden, _, _ := forbiddenDir(t)
	target := filepath.Join(forbidden, "id_rsa")

	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"/bin/sh", "-c", "echo pwned > " + target + " 2>&1"},
		SnapshotDir: t.TempDir(),
		Timeout:     10 * time.Second,
	})
	require.NoError(t, err, "the run itself must complete; output: %s", res.Output)
	assert.NotEqual(t, 0, res.ExitCode,
		"bwrap argv failed to block write to %s — containment breach; output: %s", target, res.Output)
	// The mount-namespace signature, not merely a non-zero exit: an unbound path
	// is absent from the namespace entirely. Asserting this distinguishes real
	// containment from the command having failed for some unrelated reason.
	assertPathInvisible(t, res.Output)

	_, statErr := os.Stat(target)
	require.True(t, os.IsNotExist(statErr),
		"bwrap argv failed to block write to %s — containment breach: the file exists on the host", target)
}

func TestIntegration_OSLevelLinux_ReadOutsideBoundRootsFailsWithoutLeaking(t *testing.T) {
	// AC 02-04 Scenario 5 — the exfiltration-via-read vector.
	b := linuxIntegrationBackend(t)
	_, secretPath, _ := forbiddenDir(t)

	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"/bin/sh", "-c", "cat " + secretPath + " 2>&1"},
		SnapshotDir: t.TempDir(),
		Timeout:     10 * time.Second,
	})
	require.NoError(t, err, "the run itself must complete; output: %s", res.Output)
	assert.NotEqual(t, 0, res.ExitCode, "reading outside the bound roots must fail; output: %s", res.Output)
	assert.NotContains(t, res.Output, "ATCR-INTEGRATION-CANARY",
		"secret content leaked into captured output — containment breach")
	assertPathInvisible(t, res.Output)
}

// assertPathInvisible checks for the mount-namespace ENOENT signature in any of
// the wordings the sandbox's shells and utilities produce.
//
// The variants are not cosmetic. Ubuntu's /bin/sh is dash, whose redirection
// failure reads "Directory nonexistent" where coreutils and bash both say "No
// such file or directory" — measured on orchestrator.lan, where pinning only
// the coreutils wording failed a run whose containment was in fact correct.
func assertPathInvisible(t *testing.T, output string) {
	t.Helper()
	for _, want := range []string{"No such file or directory", "Directory nonexistent", "nonexistent"} {
		if strings.Contains(output, want) {
			return
		}
	}
	t.Errorf("an unbound path must be INVISIBLE inside the mount namespace (an ENOENT-class failure), "+
		"not merely unwritable; output: %s", output)
}

func TestIntegration_OSLevelLinux_SensitiveHostPathsAreInvisible(t *testing.T) {
	// Not a stand-in this time: the REAL paths an escape would target. They are
	// never written to — only their absence from the namespace is asserted —
	// which is what makes this safe to run while still proving the allow-list
	// mount model rather than a denial of one synthetic directory.
	b := linuxIntegrationBackend(t)

	for _, path := range []string{"/root/.ssh", "/etc/shadow", "/home"} {
		t.Run(path, func(t *testing.T) {
			res, err := b.Run(context.Background(), RunSpec{
				Command:     []string{"/bin/sh", "-c", "ls -la " + path + " 2>&1"},
				SnapshotDir: t.TempDir(),
				Timeout:     10 * time.Second,
			})
			require.NoError(t, err, "output: %s", res.Output)
			assert.NotEqual(t, 0, res.ExitCode, "%s must not be listable; output: %s", path, res.Output)
			assertPathInvisible(t, res.Output)
		})
	}
}

func TestIntegration_OSLevelLinux_WriteInsideBoundRootsSucceeds(t *testing.T) {
	// AC 02-04 Scenarios 2 and 4 — the positive controls. Without them a
	// completely broken argv that made every path unreachable would pass every
	// denial in this file and read as flawless containment.
	b := linuxIntegrationBackend(t)

	t.Run("writable work tree", func(t *testing.T) {
		// Writable:true is the ONLY shape --auto-fix ever uses
		// (internal/verify/sandboxvalidate.go:72 hardcodes it). bwrapArgs mounts
		// the snapshot read-only at /src and the ephemeral copy at /work, and
		// chdirs into /work.
		snapshot := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(snapshot, "seed.txt"), []byte("seeded"), 0o644))

		res, err := b.Run(context.Background(), RunSpec{
			Command: []string{"/bin/sh", "-c",
				"pwd && cat seed.txt && echo ok > proof.txt && cat proof.txt"},
			SnapshotDir: snapshot,
			Writable:    true,
			Timeout:     10 * time.Second,
		})
		require.NoError(t, err, "output: %s", res.Output)
		require.Equal(t, 0, res.ExitCode, "a write into /work must succeed; output: %s", res.Output)
		assert.Contains(t, res.Output, bwrapWorkDir, "the run must start in the writable work tree")
		assert.Contains(t, res.Output, "seeded", "the snapshot must be readable through the copy")
		assert.Contains(t, res.Output, "ok", "the copy must be genuinely writable")

		// AC 02-04 preamble rule 2: a writable overlay leaking back to the
		// snapshot is a containment failure no unit test can detect.
		assert.NoFileExists(t, filepath.Join(snapshot, "proof.txt"),
			"a write into the ephemeral copy must never reach the host snapshot")
	})

	t.Run("snapshot is read-only at src", func(t *testing.T) {
		// The other half of the writable split: /src must reject writes even
		// while /work accepts them.
		snapshot := t.TempDir()
		res, err := b.Run(context.Background(), RunSpec{
			Command:     []string{"/bin/sh", "-c", "touch " + bwrapSrcDir + "/cant-write-here 2>&1"},
			SnapshotDir: snapshot,
			Writable:    true,
			Timeout:     10 * time.Second,
		})
		require.NoError(t, err, "output: %s", res.Output)
		assert.NotEqual(t, 0, res.ExitCode, "%s must be read-only; output: %s", bwrapSrcDir, res.Output)
		assert.Contains(t, res.Output, "Read-only file system", "output: %s", res.Output)
		assert.NoFileExists(t, filepath.Join(snapshot, "cant-write-here"))
	})

	t.Run("read-only snapshot at work", func(t *testing.T) {
		// Writable:false is what --exec uses. The snapshot is bound at /work
		// read-only, so it must be readable and unwritable.
		snapshot := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(snapshot, "seed.txt"), []byte("seeded"), 0o644))

		res, err := b.Run(context.Background(), RunSpec{
			Command:     []string{"/bin/sh", "-c", "cat seed.txt && (touch nope 2>&1 || true)"},
			SnapshotDir: snapshot,
			Timeout:     10 * time.Second,
		})
		require.NoError(t, err, "output: %s", res.Output)
		assert.Contains(t, res.Output, "seeded", "the snapshot must stay readable; output: %s", res.Output)
		assert.Contains(t, res.Output, "Read-only file system",
			"the non-writable snapshot must reject writes; output: %s", res.Output)
		assert.NoFileExists(t, filepath.Join(snapshot, "nope"))
	})

	t.Run("tmp", func(t *testing.T) {
		// The second permitted root. Unlike macOS, /tmp here is an ephemeral
		// tmpfs, so the proof is a read-back from INSIDE the sandbox plus the
		// assertion that nothing reached the host — the ephemerality is the
		// feature (it closes the symlink-race and predictable-path
		// preconditions a shared host /tmp leaves open).
		name := "atcr-sbx-proof-" + randHex(8) + ".txt"
		marker := filepath.Join("/tmp", name)
		t.Cleanup(func() { _ = os.Remove(marker) })

		res, err := b.Run(context.Background(), RunSpec{
			Command:     []string{"/bin/sh", "-c", "echo ok > " + marker + " && cat " + marker},
			SnapshotDir: t.TempDir(),
			Timeout:     10 * time.Second,
		})
		require.NoError(t, err, "output: %s", res.Output)
		require.Equal(t, 0, res.ExitCode, "a write into /tmp must succeed; output: %s", res.Output)
		assert.Contains(t, res.Output, "ok")
		assert.NoFileExists(t, marker,
			"/tmp is an ephemeral tmpfs — the write must not reach the host's /tmp")
	})
}

func TestIntegration_OSLevelLinux_RealToolchainRuns(t *testing.T) {
	// The strongest positive control: an actual tool, not just `sh`. On the
	// macOS side an earlier profile was measured to be "secure" only because
	// `git` could not run under it at all, so this case exists to catch a
	// bind set that contains perfectly and is useless.
	b := linuxIntegrationBackend(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed on this host; skipping the real-tool positive control")
	}

	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"/bin/sh", "-c", "git --version 2>&1"},
		SnapshotDir: t.TempDir(),
		Writable:    true,
		Timeout:     20 * time.Second,
	})
	require.NoError(t, err, "output: %s", res.Output)
	assert.Equal(t, 0, res.ExitCode,
		"git must be runnable under the argv — a sandbox nothing can run in is not containment; output: %s",
		res.Output)
	assert.Contains(t, res.Output, "git version", "output: %s", res.Output)
}

func TestIntegration_OSLevelLinux_NetworkEgressIsBlocked(t *testing.T) {
	// AC 02-04 Scenario 3, including its Error Scenario 2: the probe must
	// distinguish "--unshare-net blocked it" from "this host has no outbound
	// network anyway", so the identical probe runs UNSANDBOXED first as a
	// control and the test skips if that control cannot reach the target either.
	//
	// The target is a loopback listener on the host: it depends on no external
	// service and cannot exfiltrate anything. It is also the strictest form of
	// the assertion — the sandbox gets a FRESH loopback, so the host's listener
	// is unreachable from inside even though 127.0.0.1 itself exists there.
	b := linuxIntegrationBackend(t)

	port := loopbackListener(t)
	probe := "nc -z -w 2 127.0.0.1 " + port + " && echo CONNECTED || echo BLOCKED"

	control := runUnsandboxed(t, probe)
	if !strings.Contains(control, "CONNECTED") {
		t.Skipf("unsandboxed control could not reach the loopback listener (%q); "+
			"cannot distinguish namespace denial from host policy", strings.TrimSpace(control))
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

func TestIntegration_OSLevelLinux_HostProcessTableIsInvisible(t *testing.T) {
	// --unshare-pid plus the private --proc mount. A workload that can see the
	// host process table can also signal it, and `ps` output is itself a
	// disclosure. The assertion is a small PID count rather than an exact one,
	// since the sandbox legitimately contains the shell and its children.
	b := linuxIntegrationBackend(t)

	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"/bin/sh", "-c", "ls /proc | grep -c '^[0-9]*$'"},
		SnapshotDir: t.TempDir(),
		Timeout:     10 * time.Second,
	})
	require.NoError(t, err, "output: %s", res.Output)
	require.Equal(t, 0, res.ExitCode, "output: %s", res.Output)

	count := strings.TrimSpace(res.Output)
	assert.NotEmpty(t, count)
	assert.Less(t, len(count), 3,
		"a private PID namespace must show a handful of processes, not the host's table (got %q)", count)
}

func TestIntegration_OSLevelLinux_ArgvTerminatorHoldsAgainstAHostileCommand(t *testing.T) {
	// The model-authored workload is appended immediately after bwrap's own
	// options. Without the terminator, bwrap parses those tokens as its own
	// flags — a review used exactly that to rebind the host root and re-share
	// the network from a RunSpec.Command. This asserts the terminator holds
	// against that attack rather than merely being present in the argv.
	b := linuxIntegrationBackend(t)

	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"--bind", "/", "/host", "--share-net", "/bin/sh"},
		SnapshotDir: t.TempDir(),
		Timeout:     10 * time.Second,
	})
	// The tokens must be treated as a command to execute (which fails), never
	// as bwrap options.
	if err == nil {
		assert.NotEqual(t, 0, res.ExitCode, "output: %s", res.Output)
	}
	combined := res.Output
	if err != nil {
		combined += " " + err.Error()
	}
	assert.Contains(t, combined, "--bind",
		"bwrap must report the hostile token as an un-executable COMMAND, proving it was not parsed as an option; output: %s",
		combined)
	assert.NotContains(t, combined, "Unknown option",
		"the hostile token reached bwrap's option parser — the terminator did not hold; output: %s", combined)
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
