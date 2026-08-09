//go:build integration && darwin

package sandbox

import (
	"context"
	"errors"
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
//  1. Drive OSLevelBackend.Run, NOT sandboxExecProfile plus a hand-rolled
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
// The guard is SPLIT, and the split is the point. Skipping on any Preflight
// failure would make this file self-disable on exactly the class of bug it
// exists to catch: a containment-argv or profile regression fails Preflight's
// trivial run, so a blanket skip would turn "the sandbox is broken" into a green
// PASS. (The Linux sibling was measured doing precisely that — emptying the bind
// allow-list made all 8 tests skip and the binary report PASS.)
//
// So only the HOST-CAPABILITY cause skips: sandbox-exec absent, relocated or
// removed by a future macOS. If the binary resolves and Preflight still fails,
// that is our bug and the test fails.
func darwinIntegrationBackend(t *testing.T) *OSLevelBackend {
	t.Helper()
	b := NewOSLevelBackend(DefaultOSLevelConfig())
	if _, err := b.resolveToolPath(); err != nil {
		skipOrFail(t, "sandbox-exec is not usable on this host: %v", err)
		return nil
	}
	require.NoError(t, b.Preflight(context.Background()),
		"sandbox-exec resolved, so a Preflight failure is a containment regression in this repo, not a host limitation")
	return b
}

// skipOrFail skips a genuinely unrunnable case, unless
// ATCR_REQUIRE_OSLEVEL_SANDBOX_PROOF is set, in which case it fails.
//
// A skip and a proof are indistinguishable in `go test` output — both render as
// a green package — and these tests are the sprint's only kernel-level
// containment evidence. The variable is set when the run is being taken as
// sign-off for AC 02-03/02-04, so "the proof did not actually run" cannot be
// mistaken for "the proof passed".
func skipOrFail(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("ATCR_REQUIRE_OSLEVEL_SANDBOX_PROOF") != "" {
		t.Fatalf("ATCR_REQUIRE_OSLEVEL_SANDBOX_PROOF is set, so this containment proof may not be skipped: "+format, args...)
	}
	t.Skipf(format, args...)
}

// forbiddenDir creates a stand-in ~/.ssh outside every allowed root, pre-seeded
// with a known secret.
//
// It is a stand-in and never the operator's real $HOME — a hard requirement of
// AC 02-03's security section, not a stylistic one. The entire premise of this
// file is "run code that tries to escape", so a profile bug must not be able to
// damage the machine running the test.
//
// It is created under /var/tmp EXPLICITLY, never os.MkdirTemp's default, and
// its placement is then asserted. That is what makes every denial in this file
// mean anything.
//
// os.MkdirTemp("", …) resolves through os.TempDir(), i.e. $TMPDIR else /tmp.
// When /tmp was still an unconditionally allowed WRITABLE root of this profile,
// that was an active hazard: running this suite with `env -u TMPDIR` put the
// stand-in in /tmp and three tests FAILED as false alarms, reporting a
// "containment breach" for writes the sandbox was correct to permit — including
// the secret being readable. TMPDIR-unset is exactly the launchd/cron/CI shape,
// and the inverse is worse: a test whose premise is invalid in the other
// direction passes while proving nothing.
//
// The /tmp write rule has since been removed (the per-run scratch dir is now the
// only writable subtree), so that specific false-alarm shape is closed. The
// explicit /var/tmp placement stays anyway: the temp roots retain METADATA scope
// (darwinTmpDirs, oslevel_profile.go) and the read tier is unchanged, so a
// stand-in under /tmp would still not be the clean outside-every-allow-rule
// fixture this file's denials need. Pinning the location is cheaper than
// re-deriving that argument each time the allow set moves.
//
// /var/tmp is world-writable on macOS and appears in no allow rule — not in
// darwinSystemReadDirs, not in darwinTmpDirs — so absence there is the profile
// denying it rather than an ambient accident.
func forbiddenDir(t *testing.T) (dir, secretPath, secretBody string) {
	t.Helper()
	root := "/var/tmp"
	if _, err := os.Stat(root); err != nil {
		skipOrFail(t, "%s is unavailable, so no directory outside the allowed roots can be created: %v", root, err)
	}
	dir, err := os.MkdirTemp(root, "atcr-forbidden-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	assertOutsideAllowedRoots(t, dir)
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

// probeResult is an unsandboxed control probe's outcome. The exit code is
// returned rather than swallowed: it is the whole point of the control.
type probeResult struct {
	exitCode int
	output   string
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

// assertOutsideAllowedRoots fails if dir sits inside a root the profile allows.
//
// This asserts the test's PREMISE before any denial is asserted. Without it a
// denial test degrades silently into a tautology when the environment moves the
// directory: the sandbox permits the write, the assertion reports a
// "containment breach", and whichever way it lands the result is not evidence.
func assertOutsideAllowedRoots(t *testing.T, dir string) {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	for _, allowed := range append(append([]string{}, darwinTmpDirs...), darwinSystemReadDirs...) {
		allowedResolved, evalErr := filepath.EvalSymlinks(allowed)
		if evalErr != nil {
			continue // not present on this host
		}
		require.False(t, pathContainsFold(allowedResolved, resolved, true),
			"the forbidden-path fixture %q is INSIDE the allowed root %q, so any denial asserted against it "+
				"would be testing the wrong thing", resolved, allowedResolved)
	}
}

// assertDeniedByProfile checks for the kernel sandbox's own denial diagnostic.
//
// A bare "non-zero exit" cannot tell a containment denial from a typo'd path, a
// missing binary, or bad shell syntax — all of which also exit non-zero and also
// leave the target file absent. docker_integration_test.go, the file this one
// mirrors, asserts the EROFS text for exactly this reason; this is the macOS
// equivalent.
func assertDeniedByProfile(t *testing.T, output, path string) {
	t.Helper()
	assert.Contains(t, output, "Operation not permitted",
		"the failure must carry the sandbox's own denial diagnostic, not merely a non-zero exit; output: %s", output)
	assert.Contains(t, output, filepath.Base(path),
		"the denial must name the path under test, so an unrelated failure cannot satisfy it; output: %s", output)
}

func TestIntegration_OSLevelDarwin_ForbiddenWriteOutsideAllowedRootsFails(t *testing.T) {
	// AC 02-03 Scenario 1.
	b := darwinIntegrationBackend(t)
	forbidden, _, _ := forbiddenDir(t)
	target := filepath.Join(forbidden, "id_rsa")

	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"/bin/sh", "-c", "echo pwned > " + target + " 2>&1"},
		SnapshotDir: t.TempDir(),
		Timeout:     10 * time.Second,
	})
	require.NoError(t, err, "the run itself must complete; output: %s", res.Output)
	assert.NotEqual(t, 0, res.ExitCode,
		"sandbox-exec profile failed to block write to %s — containment breach; output: %s", target, res.Output)
	assertDeniedByProfile(t, res.Output, target)

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
		Command:     []string{"/bin/sh", "-c", "cat " + secretPath + " 2>&1"},
		SnapshotDir: t.TempDir(),
		Timeout:     10 * time.Second,
	})
	require.NoError(t, err, "the run itself must complete; output: %s", res.Output)
	assert.NotEqual(t, 0, res.ExitCode, "reading outside the allowed roots must fail; output: %s", res.Output)
	assertDeniedByProfile(t, res.Output, secretPath)
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
		// assert, not require: the leak check below must run even when the
		// positive control fails, or a regression that redirects the run at the
		// host snapshot is reported as "the positive control broke" while the
		// assertion written to catch it never executes.
		assert.Equal(t, 0, res.ExitCode, "a write into the writable copy must succeed; output: %s", res.Output)
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
		{"python3", "python3 -c 'print(\"ok\")'"},
	} {
		t.Run(tool.name, func(t *testing.T) {
			// /usr/bin/python3 exists only with the Command Line Tools installed;
			// without them there is no shim for the profile to mishandle.
			if tool.name == "python3" {
				if _, err := exec.LookPath("python3"); err != nil {
					skipOrFail(t, "python3 is not installed on this host: %v", err)
				}
			}
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

	// The probe deliberately does NOT end in `|| echo BLOCKED`. A shell-authored
	// token like that is produced by ANY failure — a missing nc, a denied exec, a
	// dead listener — so asserting on it cannot distinguish "the kernel denied
	// the connect" from "the probe never ran". Measured on this host: a
	// nonexistent binary and a connect to a closed port both produced the
	// byte-identical `BLOCKED`, satisfying the old assertions completely.
	//
	// Instead nc's own exit status is the signal, with a distinct code reserved
	// for "the probe itself was unavailable" so that case is never counted as
	// containment.
	const probeMissingCode = 97
	probe := "command -v nc >/dev/null 2>&1 || exit 97; exec nc -z -w 2 127.0.0.1 " + port

	if control := runUnsandboxed(t, probe); control.exitCode != 0 {
		skipOrFail(t, "unsandboxed control could not reach the loopback listener (exit %d, %q); "+
			"cannot distinguish sandbox denial from host policy", control.exitCode, strings.TrimSpace(control.output))
	}

	res, runErr := b.Run(context.Background(), RunSpec{
		Command:     []string{"/bin/sh", "-c", probe},
		SnapshotDir: t.TempDir(),
		Timeout:     15 * time.Second,
	})
	require.NoError(t, runErr, "output: %s", res.Output)
	require.NotEqual(t, probeMissingCode, res.ExitCode,
		"the network probe binary was unreachable inside the sandbox, so this run proves nothing about egress; output: %s",
		res.Output)
	assert.NotEqual(t, 0, res.ExitCode,
		"outbound network egress was NOT blocked — containment breach; output: %s", res.Output)

	// Re-probe unsandboxed AFTER the sandboxed attempt. The pre-check alone
	// cannot rule out the listener having died in between, which would make the
	// denial above indistinguishable from an unreachable target.
	after := runUnsandboxed(t, probe)
	require.Equal(t, 0, after.exitCode,
		"the listener stopped accepting during the test, so the sandboxed failure is not attributable to containment; output: %s",
		after.output)
}

func TestIntegration_OSLevelDarwin_SnapshotIsReadOnly(t *testing.T) {
	// The trailing `(deny file-write* (subpath <snapshot>))` rule is, by
	// oslevel_profile.go:180-189's own account, "the one that holds": when the
	// snapshot lives under /tmp — the os.MkdirTemp shape whenever TMPDIR is
	// unset — the unconditional /tmp carve-out would otherwise make the tree
	// under review writable, and a review reproduced exactly that, overwriting a
	// snapshot file from inside the sandbox.
	//
	// It had NO coverage here: a reviewer deleted both that rule and the
	// (deny network*) line and all six tests in this file stayed green. The
	// writes below therefore use the snapshot's ABSOLUTE path, which is the only
	// way to attempt what the rule forbids — the writable-copy control writes a
	// RELATIVE path into a cwd that is already the ephemeral copy, so it cannot
	// reach the snapshot even with containment removed.
	b := darwinIntegrationBackend(t)

	for _, writable := range []bool{false, true} {
		name := "read-only spec"
		if writable {
			name = "writable spec"
		}
		t.Run(name, func(t *testing.T) {
			// The snapshot is placed under /tmp DELIBERATELY, not in t.TempDir().
			// t.TempDir() resolves through $TMPDIR to /var/folders/... on a
			// developer's Mac, which no allow rule covers — so the write is
			// refused by the deny-default posture and the trailing rule is never
			// consulted. Measured: with the trailing rule deleted, a t.TempDir()
			// snapshot still passed this test, proving nothing.
			//
			// Under /tmp the unconditional /tmp carve-out DOES grant write, so
			// only the trailing `(deny file-write* (subpath <snapshot>))` can
			// refuse it — which is precisely the configuration
			// oslevel_profile.go:180-189 says a review once used to overwrite a
			// snapshot file from inside the sandbox, and the shape os.MkdirTemp
			// produces under launchd, cron and CI.
			snapshot, err := os.MkdirTemp("/tmp", "atcr-snap-*")
			require.NoError(t, err)
			t.Cleanup(func() { _ = os.RemoveAll(snapshot) })

			existing := filepath.Join(snapshot, "existing.txt")
			require.NoError(t, os.WriteFile(existing, []byte("orig"), 0o644))
			fresh := filepath.Join(snapshot, "victim.txt")

			res, runErr := b.Run(context.Background(), RunSpec{
				Command: []string{"/bin/sh", "-c",
					"echo tampered > " + existing + " 2>&1; echo tampered > " + fresh + " 2>&1; true"},
				SnapshotDir: snapshot,
				Writable:    writable,
				Timeout:     10 * time.Second,
			})
			require.NoError(t, runErr, "output: %s", res.Output)

			assert.Contains(t, res.Output, "Operation not permitted",
				"a write to the snapshot's absolute path must be denied by the kernel; output: %s", res.Output)
			assert.NoFileExists(t, fresh,
				"the snapshot must be read-only — a new file was created in the tree under review")

			body, readErr := os.ReadFile(existing)
			require.NoError(t, readErr)
			assert.Equal(t, "orig", string(body),
				"the snapshot must be read-only — an existing file in the tree under review was overwritten")
		})
	}
}

func TestIntegration_OSLevelDarwin_ScriptModeIsContainedToo(t *testing.T) {
	// RunSpec.Script takes a different path through the backend: osLevelRunArgs
	// appends `/bin/sh -s` and the body is streamed over stdin rather than
	// appearing in argv (which is what keeps the injection-safety guarantee).
	// docker_integration_test.go covers both modes; without this, the Script
	// branch has no real-binary coverage at all.
	b := darwinIntegrationBackend(t)
	_, secretPath, _ := forbiddenDir(t)
	snapshot := t.TempDir()

	res, err := b.Run(context.Background(), RunSpec{
		Script: "echo script-ran\n" +
			"cat " + secretPath + " 2>&1 || true\n",
		SnapshotDir: snapshot,
		Writable:    true,
		Timeout:     10 * time.Second,
	})
	require.NoError(t, err, "output: %s", res.Output)
	assert.Contains(t, res.Output, "script-ran",
		"the script body must reach the shell over stdin; output: %s", res.Output)
	assert.NotContains(t, res.Output, "ATCR-INTEGRATION-CANARY",
		"Script mode must be contained exactly as Command mode is — the secret leaked; output: %s", res.Output)
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
