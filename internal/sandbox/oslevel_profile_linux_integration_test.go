//go:build integration && linux

package sandbox

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
// The guard is SPLIT, and the split is the whole point. A blanket "skip on any
// Preflight failure" makes this file self-disable on exactly the class of bug it
// exists to catch: a review emptied the bind allow-list so nothing executable
// remained inside the namespace, and all eight tests printed SKIP while the
// binary printed PASS — including the one whose comment says it exists "to catch
// a bind set that contains perfectly and is useless". A skipped proof and a
// passing proof are indistinguishable in `go test` output.
//
// So only HOST-CAPABILITY causes skip, and each is identified specifically:
//
//   - bwrap absent (resolveToolPath fails),
//   - the userns sysctls say namespaces are off (checkHostPrerequisite fails),
//   - bwrap is present but the kernel/AppArmor refuses it the namespace, which
//     no sysctl reports and which only shows up when bwrap actually runs. That
//     is matched on bwrap's OWN diagnostics, listed below.
//
// Anything else — a rejected flag, an unbindable path, a workload that cannot
// exec — is OUR bug, and fails.
func linuxIntegrationBackend(t *testing.T) *OSLevelBackend {
	t.Helper()
	b := NewOSLevelBackend(DefaultOSLevelConfig())
	if _, err := b.resolveToolPath(); err != nil {
		skipOrFail(t, "bwrap is not usable on this host: %v", err)
		return nil
	}
	if err := checkHostPrerequisite(context.Background(), "linux", ""); err != nil {
		skipOrFail(t, "unprivileged user namespaces are unavailable on this host: %v", err)
		return nil
	}
	if err := b.Preflight(context.Background()); err != nil {
		if isUsernsDenial(err) {
			skipOrFail(t, "the kernel or AppArmor refused bwrap a user namespace on this host "+
				"(no sysctl reports this; run as root or install a bwrap AppArmor profile): %v", err)
			return nil
		}
		require.NoError(t, err,
			"bwrap resolved and the host prerequisites hold, so this Preflight failure is a containment "+
				"regression in this repo, not a host limitation")
	}
	return b
}

// usernsDenialMarkers are bubblewrap's own messages for "this host will not give
// me a user namespace" — the one failure mode that is a host limitation rather
// than a bug in the generated argv.
//
// Measured on orchestrator.lan (Ubuntu 24.04,
// kernel.apparmor_restrict_unprivileged_userns=1, no bwrap AppArmor profile):
// an unprivileged run prints "bwrap: setting up uid map: Permission denied",
// while the same argv under sudo succeeds. Matching text is unavoidable here —
// bwrap exits 1 for every one of its own failures, so the exit code cannot
// separate this from a rejected flag.
var usernsDenialMarkers = []string{
	"setting up uid map",
	"No permissions to create new namespace",
	"Creating new namespace failed",
	"loopback: Failed RTM_NEWADDR",
}

func isUsernsDenial(err error) bool {
	msg := err.Error()
	for _, marker := range usernsDenialMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// skipOrFail skips a genuinely unrunnable case, unless
// ATCR_REQUIRE_OSLEVEL_SANDBOX_PROOF is set, in which case it fails.
//
// A skip and a proof are indistinguishable in `go test` output — both render as
// a green package — and this file is the sprint's only kernel-level containment
// evidence on Linux. The variable is set when a run is being taken as sign-off
// for AC 02-04, so "the proof did not actually run" cannot be mistaken for "the
// proof passed".
func skipOrFail(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("ATCR_REQUIRE_OSLEVEL_SANDBOX_PROOF") != "" {
		t.Fatalf("ATCR_REQUIRE_OSLEVEL_SANDBOX_PROOF is set, so this containment proof may not be skipped: "+format, args...)
	}
	t.Skipf(format, args...)
}

// forbiddenDir creates a stand-in ~/.ssh outside every bound root, pre-seeded
// with a known secret.
//
// A stand-in and never the real $HOME — a hard requirement of AC 02-04's
// security section rather than a stylistic one, since the premise of this file
// is running code that tries to escape.
//
// It is created under /var/tmp EXPLICITLY and the placement is then asserted;
// there is deliberately NO fallback to os.MkdirTemp's default. That default
// resolves through $TMPDIR to /tmp, and bwrap mounts an ephemeral tmpfs over
// /tmp — so a stand-in there would be invisible because its parent was
// replaced, not because the allow-list refused it, and the denial tests would
// pass with the entire bind allow-list removed. Measured: forcing that fallback
// while ALSO widening the allow-list with `--bind /var/tmp /var/tmp` kept both
// exfiltration tests green, where the correct /var/tmp placement failed them.
//
// /var is bound nowhere (linuxRequiredRoots is /usr; linuxOptionalRoots is
// /bin,/sbin,/lib,/lib64), so absence under /var/tmp is the allow-list model
// doing the work — which is the property under test.
func forbiddenDir(t *testing.T) (dir, secretPath, secretBody string) {
	t.Helper()
	const root = "/var/tmp"
	if _, err := os.Stat(root); err != nil {
		skipOrFail(t, "%s is unavailable, so no directory outside the bound roots can be created: %v", root, err)
	}
	dir, err := os.MkdirTemp(root, "atcr-forbidden-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	assertOutsideBoundRoots(t, dir)
	secretBody = "-----BEGIN OPENSSH PRIVATE KEY-----\nATCR-INTEGRATION-CANARY\n"
	secretPath = filepath.Join(dir, "id_ed25519")
	require.NoError(t, os.WriteFile(secretPath, []byte(secretBody), 0o600))
	return dir, secretPath, secretBody
}

// assertOutsideBoundRoots fails if dir sits inside anything bwrapArgs binds, or
// inside a mount bwrap establishes itself (notably the /tmp tmpfs).
//
// This asserts the test's PREMISE before any denial is asserted against it.
// Without it a denial test degrades silently into a tautology when the
// environment moves the directory.
func assertOutsideBoundRoots(t *testing.T, dir string) {
	t.Helper()
	clean := filepath.Clean(dir)
	bound := append([]string{}, linuxRequiredRoots...)
	bound = append(bound, linuxOptionalRoots...)
	bound = append(bound, linuxPseudoMounts...)
	for _, root := range bound {
		require.False(t, pathContainsFold(root, clean, false),
			"the forbidden-path fixture %q is INSIDE %q, which bwrap binds or mounts, so any denial "+
				"asserted against it would be testing the wrong thing", clean, root)
	}
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
	assertPathInvisible(t, res.Output, target)

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
	assertPathInvisible(t, res.Output, secretPath)
}

// assertPathInvisible checks for the mount-namespace ENOENT signature AND that
// the message names the path under test.
//
// The wording variants are not cosmetic: Ubuntu's /bin/sh is dash, whose
// redirection failure reads "Directory nonexistent" where coreutils and bash say
// "No such file or directory" — measured on orchestrator.lan, where pinning only
// the coreutils wording failed a run whose containment was in fact correct.
//
// Requiring the PATH in the same output is what stops the helper being a rubber
// stamp. Without it any ENOENT from anywhere in the sandbox satisfies it, so a
// mistyped probe path or a missing helper binary reads as containment — a
// reviewer confirmed a nonexistent path passed the earlier version. The bare
// substring "nonexistent" was also accepted before, which is a strict superset
// of the dash wording and only widened the match.
func assertPathInvisible(t *testing.T, output, path string) {
	t.Helper()
	found := false
	for _, want := range []string{"No such file or directory", "Directory nonexistent"} {
		if strings.Contains(output, want) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("an unbound path must be INVISIBLE inside the mount namespace (an ENOENT-class failure), "+
			"not merely unwritable; output: %s", output)
		return
	}
	assert.Contains(t, output, path,
		"the ENOENT must name the path under test, so an unrelated failure elsewhere in the sandbox "+
			"cannot satisfy this assertion; output: %s", output)
}

func TestIntegration_OSLevelLinux_SensitiveHostPathsAreInvisible(t *testing.T) {
	// Not a stand-in this time: the REAL paths an escape would target. They are
	// never written to — only their absence from the namespace is asserted —
	// which is what makes this safe to run while still proving the allow-list
	// mount model rather than a denial of one synthetic directory.
	b := linuxIntegrationBackend(t)

	for _, path := range []string{"/root/.ssh", "/etc/shadow", "/home"} {
		t.Run(path, func(t *testing.T) {
			// The host-side existence control. Without it this test passes
			// identically for a path that simply is not on the host — verified by
			// a reviewer, who saw "/definitely-not-here-xyz" reported as
			// contained. That vacuity is the same one runUnsandboxed guards
			// against for the network probe.
			if _, err := os.Stat(path); err != nil {
				skipOrFail(t, "%s does not exist on this host, so its invisibility inside the "+
					"namespace proves nothing: %v", path, err)
				return
			}
			res, err := b.Run(context.Background(), RunSpec{
				Command:     []string{"/bin/sh", "-c", "ls -la " + path + " 2>&1"},
				SnapshotDir: t.TempDir(),
				Timeout:     10 * time.Second,
			})
			require.NoError(t, err, "output: %s", res.Output)
			assert.NotEqual(t, 0, res.ExitCode, "%s must not be listable; output: %s", path, res.Output)
			assertPathInvisible(t, res.Output, path)
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
		// Narrowly: THIS path did not reach the host, because /tmp inside the
		// sandbox is a fresh tmpfs. It is not a claim that nothing under /tmp is
		// host-reachable — the per-run scratch dir is bound at its own host path
		// and normally lives under /tmp, which the scratch-bind test below covers.
		assert.NoFileExists(t, marker,
			"this /tmp write must land in the ephemeral tmpfs, not the host's /tmp")
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

	// The probe deliberately does NOT end in `|| echo BLOCKED`. A shell-authored
	// token like that is produced by ANY failure — a missing nc, an unbound
	// /usr/bin, a dash quirk — so asserting on it cannot distinguish "the
	// namespace blocked the connect" from "the probe never ran". Verified: with
	// only the sandboxed leg pointed at a nonexistent binary, the old form still
	// passed. nc's own exit status is the signal now, with a reserved code for
	// "the probe itself was unavailable" so that case is never counted as
	// containment.
	const probeMissingCode = 97
	probe := "command -v nc >/dev/null 2>&1 || exit 97; exec nc -z -w 2 127.0.0.1 " + port

	if control := runUnsandboxed(t, probe); control.exitCode != 0 {
		skipOrFail(t, "unsandboxed control could not reach the loopback listener (exit %d, %q); "+
			"cannot distinguish namespace denial from host policy", control.exitCode, strings.TrimSpace(control.output))
	}

	res, runErr := b.Run(context.Background(), RunSpec{
		Command:     []string{"/bin/sh", "-c", probe},
		SnapshotDir: t.TempDir(),
		Timeout:     15 * time.Second,
	})
	require.NoError(t, runErr, "output: %s", res.Output)
	require.NotEqual(t, probeMissingCode, res.ExitCode,
		"the network probe binary was unreachable inside the namespace, so this run proves nothing about egress; output: %s",
		res.Output)
	assert.NotEqual(t, 0, res.ExitCode,
		"outbound network egress was NOT blocked — containment breach; output: %s", res.Output)

	// Re-probe unsandboxed AFTER the sandboxed attempt: the pre-check alone
	// cannot rule out the listener having died in between.
	after := runUnsandboxed(t, probe)
	require.Equal(t, 0, after.exitCode,
		"the listener stopped accepting during the test, so the sandboxed failure is not attributable "+
			"to containment; output: %s", after.output)
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

	// Parsed, not measured by string length. The earlier `len(count) < 3` was a
	// check on the number of DIGITS, so it accepted up to 99 visible host PIDs
	// (and any 1-2 character garbage) while its message claimed "a handful".
	// Measured inside the namespace: 4.
	count := strings.TrimSpace(res.Output)
	n, convErr := strconv.Atoi(count)
	require.NoError(t, convErr, "the probe must report a PID count; output: %q", res.Output)
	assert.LessOrEqual(t, n, 5,
		"a private PID namespace must show a handful of processes, not the host's table (got %d)", n)
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
	// NOT an assertion on "Unknown option": both hostile tokens are VALID
	// bubblewrap options, so bwrap would accept them rather than complain if the
	// terminator were dropped — the old assertion was true either way. The
	// discriminating check is that the rebind the tokens ask for did not happen.
	probe, probeErr := b.Run(context.Background(), RunSpec{
		Command:     []string{"/bin/sh", "-c", "ls /host 2>&1"},
		SnapshotDir: t.TempDir(),
		Timeout:     10 * time.Second,
	})
	require.NoError(t, probeErr, "output: %s", probe.Output)
	assert.NotEqual(t, 0, probe.ExitCode,
		"/host exists inside the namespace, so a --bind from a workload argv took effect — the terminator did not hold; output: %s",
		probe.Output)
	assertPathInvisible(t, probe.Output, "/host")
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
func runUnsandboxed(t *testing.T, script string) probeResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
	out, err := cmd.CombinedOutput()
	code := cmd.ProcessState.ExitCode()
	var ee *exec.ExitError
	if err != nil && !errors.As(err, &ee) {
		require.NoError(t, err, "the unsandboxed control probe could not be started; output: %s", out)
	}
	return probeResult{exitCode: code, output: string(out)}
}

// probeResult is an unsandboxed control probe's outcome. The exit code is
// returned rather than swallowed: it is the whole point of the control. The
// earlier form returned only the output and carried a require.NoError that
// could never fire, because the probe script ended in `|| echo`, which forces
// exit 0.
type probeResult struct {
	exitCode int
	output   string
}

func TestIntegration_OSLevelLinux_WritableOverlayDoesNotWidenTheBoundary(t *testing.T) {
	// AC 02-04 Edge Case 2, and the asymmetry a reviewer caught: every denial in
	// this file used the zero value of Writable, so the ONLY shape --auto-fix
	// ever uses (internal/verify/sandboxvalidate.go:72 hardcodes Writable:true)
	// had no denial coverage at all on Linux, while the macOS sibling had it.
	//
	// The writable split adds two mounts — the copy at /work and the scratch dir
	// at its own host path — which makes it the most plausible place for an
	// over-broad bind to hide.
	b := linuxIntegrationBackend(t)
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
		"the writable split widened the boundary: %s was created — containment breach", target)
	assert.NotContains(t, res.Output, "ATCR-INTEGRATION-CANARY",
		"the writable split widened the boundary: the secret was readable — containment breach")
	assertPathInvisible(t, res.Output, forbidden)

	// The network denial must survive the writable split too.
	port := loopbackListener(t)
	probe := "command -v nc >/dev/null 2>&1 || exit 97; exec nc -z -w 2 127.0.0.1 " + port
	if control := runUnsandboxed(t, probe); control.exitCode != 0 {
		skipOrFail(t, "unsandboxed control could not reach the loopback listener (exit %d)", control.exitCode)
		return
	}
	netRes, netErr := b.Run(context.Background(), RunSpec{
		Command:     []string{"/bin/sh", "-c", probe},
		SnapshotDir: t.TempDir(),
		Writable:    true,
		Timeout:     15 * time.Second,
	})
	require.NoError(t, netErr, "output: %s", netRes.Output)
	require.NotEqual(t, 97, netRes.ExitCode, "the probe was unreachable; output: %s", netRes.Output)
	assert.NotEqual(t, 0, netRes.ExitCode,
		"egress was NOT blocked under the writable split — containment breach; output: %s", netRes.Output)
}

func TestIntegration_OSLevelLinux_ScratchBindIsNotAnEscapeHatch(t *testing.T) {
	// The scratch directory is bound at its own HOST path so the toolchain's
	// HOME/TMPDIR/GOCACHE resolve to a real writable location. That is a genuine
	// writable window onto the host filesystem, normally under /tmp, and nothing
	// else in this file measures its blast radius.
	//
	// What must hold: the window is exactly the scratch tree. Its parent must not
	// be reachable through it, so a workload cannot walk out via `..` into the
	// host's /tmp and read another run's — or another process's — files.
	b := linuxIntegrationBackend(t)

	// The canary must sit in the scratch dir's REAL parent: the backend takes
	// its scratch from os.MkdirTemp("") (oslevel.go's runWith), whose parent is
	// $TMPDIR when set. The previous revision hardcoded /tmp, so under a
	// developer shell's TMPDIR the canary was never in the scratch parent and
	// both probes were vacuous — and with TMPDIR unset the walk-out resolved to
	// the tmpfs bwrap mounts at /tmp, so the canary's absence was explained by
	// the tmpfs rather than by the bind being narrow. Derive the parent the
	// same MkdirTemp call would use and assert the placement before trusting
	// the probes.
	probeDir, err := os.MkdirTemp("", "atcr-scratch-parent-probe-*")
	require.NoError(t, err)
	scratchParent := filepath.Dir(probeDir)
	require.NoError(t, os.Remove(probeDir))

	canary, err := os.CreateTemp(scratchParent, "atcr-scratch-canary-*")
	require.NoError(t, err)
	canaryPath := canary.Name()
	t.Cleanup(func() { _ = os.Remove(canaryPath) })
	_, err = canary.WriteString("SCRATCH-PARENT-CANARY\n")
	require.NoError(t, err)
	require.NoError(t, canary.Close())

	// Positive control: the canary must be readable from the HOST, or its
	// absence inside the sandbox proves nothing about the bind.
	hostBody, err := os.ReadFile(canaryPath)
	require.NoError(t, err)
	require.Contains(t, string(hostBody), "SCRATCH-PARENT-CANARY",
		"the canary is not host-readable; the in-sandbox assertions would be vacuous")

	// Two probes, each discriminating bind WIDTH rather than the /tmp tmpfs:
	// the canary's absolute host path, and a walk out of the scratch tree into
	// its parent ($HOME/.. — wherever TMPDIR put it, NOT a path through /tmp).
	// `|| echo PROBE-n-MISS` proves the cat ran and the file was genuinely
	// absent; a bare NotContains would also pass if the probe never executed.
	res, runErr := b.Run(context.Background(), RunSpec{
		Command: []string{"/bin/sh", "-c",
			"echo HOME_WRITABLE=$(touch \"$HOME/probe\" 2>&1 && echo yes || echo no); " +
				"cat " + canaryPath + " 2>/dev/null || echo PROBE-1-MISS; " +
				"cat \"$HOME/../\"$(basename " + canaryPath + ") 2>/dev/null || echo PROBE-2-MISS; true"},
		SnapshotDir: t.TempDir(),
		Timeout:     10 * time.Second,
	})
	require.NoError(t, runErr, "output: %s", res.Output)

	assert.Contains(t, res.Output, "HOME_WRITABLE=yes",
		"the scratch tree must be writable, or no toolchain can run; output: %s", res.Output)
	assert.NotContains(t, res.Output, "SCRATCH-PARENT-CANARY",
		"a file in the scratch dir's HOST parent was readable from inside the sandbox — the scratch "+
			"bind is an escape hatch onto the host's temp root; output: %s", res.Output)
	assert.Contains(t, res.Output, "PROBE-1-MISS",
		"the absolute-path probe did not run to a genuine miss; output: %s", res.Output)
	assert.Contains(t, res.Output, "PROBE-2-MISS",
		"the parent walk-out probe did not run to a genuine miss; output: %s", res.Output)
}
