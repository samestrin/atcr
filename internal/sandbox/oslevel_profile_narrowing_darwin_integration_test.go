//go:build integration && darwin

package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.23.1 — kernel-level proof for the measured profile narrowings. The
// shape of the narrowed rules is pinned in oslevel_profile_test.go; only a real
// sandbox-exec run shows the kernel refuses what the rule leaves out. Reuses
// darwinIntegrationBackend and skipOrFail from
// oslevel_profile_darwin_integration_test.go, which this epic does not edit.

func TestIntegration_OSLevelDarwin_FingerprintSysctlsAreUnreadable(t *testing.T) {
	// TD-013 / T1. hw.model and kern.uuid were readable under the unfiltered
	// `(allow sysctl-read)` and are not in darwinSysctlReadNames. The host-side
	// value is the control: its absence from the sandboxed output discriminates
	// "denied" from "read but happened to print nothing useful".
	b := darwinIntegrationBackend(t)

	for _, name := range []string{"hw.model", "kern.uuid"} {
		t.Run(name, func(t *testing.T) {
			hostOut, err := exec.Command("/usr/sbin/sysctl", "-n", name).Output()
			host := strings.TrimSpace(string(hostOut))
			if err != nil || host == "" {
				skipOrFail(t, "%s is not readable on the host, so its absence inside the sandbox proves nothing: %v", name, err)
				return
			}

			res, err := b.Run(context.Background(), RunSpec{
				Command:     []string{"/bin/sh", "-c", "/usr/sbin/sysctl -n " + name + " 2>&1"},
				SnapshotDir: t.TempDir(),
				Timeout:     10 * time.Second,
			})
			require.NoError(t, err, "output: %s", res.Output)
			assert.NotContains(t, res.Output, host,
				"%s leaked into the sandbox — the sysctl-read narrowing is not enforced; output: %s", name, res.Output)
		})
	}
}

func TestIntegration_OSLevelDarwin_MeasuredSysctlsStillResolve(t *testing.T) {
	// The positive control for the test above: a profile denying every sysctl
	// would pass it vacuously and break every shell (the measurement's deny pass
	// failed 14 of 16 proofs). uname(3) reads kern.ostype, kern.osrelease,
	// kern.version and hw.machine, all in darwinSysctlReadNames.
	b := darwinIntegrationBackend(t)

	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"/bin/sh", "-c", "uname -s -r -m 2>&1"},
		SnapshotDir: t.TempDir(),
		Timeout:     10 * time.Second,
	})
	require.NoError(t, err, "output: %s", res.Output)
	require.Equal(t, 0, res.ExitCode, "uname must run under the narrowed profile; output: %s", res.Output)
	assert.Contains(t, res.Output, "Darwin", "kern.ostype must resolve inside the sandbox; output: %s", res.Output)
}

// writeFixture writes files (relative path -> body) into a fresh snapshot dir.
func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	return dir
}

// runToolchainLeg runs script in a Writable copy of snapshot and requires a
// clean exit carrying want. Shared by the exec-tier legs below, which are what
// the TD-012 narrowing (T2) is measured against and then proven by.
func runToolchainLeg(t *testing.T, b *OSLevelBackend, snapshot, script, want string) {
	t.Helper()
	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"/bin/sh", "-c", script + " 2>&1"},
		SnapshotDir: snapshot,
		Writable:    true,
		Timeout:     2 * time.Minute,
	})
	require.NoError(t, err, "output: %s", res.Output)
	require.Equal(t, 0, res.ExitCode,
		"%q must run under the profile — the exec narrowing denied something it needs; output: %s", script, res.Output)
	assert.NotContains(t, res.Output, "Operation not permitted",
		"the profile denied something %q needs; output: %s", script, res.Output)
	assert.Contains(t, res.Output, want, "output: %s", res.Output)
}

func TestIntegration_OSLevelDarwin_GoTestRuns(t *testing.T) {
	// TD-012 / T2. RealToolchainRuns stops at `go version`, which execs one
	// binary. A real `go test` execs the compiler and linker from GOROOT's
	// pkg/tool and then the test binary it just linked under GOTMPDIR, which
	// sandboxEnv points into the per-run scratch tree. That last exec is the one
	// a subpath filter naming only toolchain prefixes would break.
	b := darwinIntegrationBackend(t)
	snapshot := writeFixture(t, map[string]string{
		"go.mod": "module example.com/sbxproof\n\ngo 1.21\n",
		"proof_test.go": `package sbxproof

import "testing"

func TestSandboxedGoTest(t *testing.T) { t.Log("atcr-go-test-ran") }
`,
	})
	runToolchainLeg(t, b, snapshot, "go test -count=1 -v ./...", "atcr-go-test-ran")
}

func TestIntegration_OSLevelDarwin_MakeRuns(t *testing.T) {
	// TD-012 / T2. make execs its recipe through /bin/sh, so this covers a
	// toolchain that starts a second binary of its own.
	//
	// It runs a real make binary, not the /usr/bin/make shim. The shim asks
	// xcode-select for the developer dir by reading /var/db/xcode_select_link,
	// which the profile does not grant, and on the macOS runner that link points
	// into /Applications/Xcode*.app, which the read tier excludes anyway. Measured
	// in CI (run 39806011885): "unable to read data link at
	// '/var/db/xcode_select_link' (Operation not permitted)" under the unchanged,
	// unscoped exec rule, so the shim failure is a read-tier gap, not something
	// the exec narrowing can prove or break. The CLT make binary consults
	// xcode-select too (same error, run 39806821087), so it runs with
	// DEVELOPER_DIR naming the Command Line Tools, which xcode-select honours
	// instead of reading the link. Homebrew gmake needs neither and is preferred.
	// A host with no make under a readable prefix skips, as NpmRuns does;
	// GoTestRuns and NpmRuns carry the task's proof there.
	var mk string
	for _, p := range []string{
		"/opt/homebrew/bin/gmake",
		"/usr/local/bin/gmake",
		"/Library/Developer/CommandLineTools/usr/bin/make",
	} {
		if _, err := os.Stat(p); err == nil {
			mk = p
			break
		}
	}
	if strings.HasPrefix(mk, "/Library/Developer/CommandLineTools/") {
		mk = "DEVELOPER_DIR=/Library/Developer/CommandLineTools " + mk
	}
	if mk == "" {
		t.Skip("no make binary under a prefix the profile reads (Command Line Tools or Homebrew gmake)")
	}
	b := darwinIntegrationBackend(t)
	snapshot := writeFixture(t, map[string]string{
		"Makefile": "proof:\n\t@echo atcr-make-ran\n",
	})
	runToolchainLeg(t, b, snapshot, mk+" --no-print-directory proof", "atcr-make-ran")
}

func TestIntegration_OSLevelDarwin_NpmRuns(t *testing.T) {
	// TD-012 / T2. npm runs node, which lives only under /opt/homebrew or
	// /usr/local on a normal Mac (vettedToolchainPrefixes). A node installed
	// anywhere else is invisible inside the sandbox by design (docs/auto-fix.md),
	// so that host proves nothing here and the leg skips rather than fails, even
	// under ATCR_REQUIRE_OSLEVEL_SANDBOX_PROOF. MakeRuns is the required non-Go
	// leg; this one runs wherever node is installed where the profile reads.
	npm, err := exec.LookPath("npm")
	if err != nil {
		t.Skipf("npm is not installed on this host: %v", err)
	}
	if resolved, rerr := filepath.EvalSymlinks(npm); rerr != nil || !underVettedToolchainPrefix(resolved, vettedToolchainPrefixes) {
		t.Skipf("npm resolves to %q (%v), outside vettedToolchainPrefixes, so the sandbox cannot read it by design", resolved, rerr)
	}
	b := darwinIntegrationBackend(t)
	snapshot := writeFixture(t, map[string]string{
		"package.json": `{"name":"sbxproof","version":"0.0.0","private":true,"scripts":{"proof":"echo atcr-npm-ran"}}` + "\n",
	})
	runToolchainLeg(t, b, snapshot, "npm run --silent proof", "atcr-npm-ran")
}

func TestIntegration_OSLevelDarwin_ExecOutsideTheMeasuredDirsIsRefused(t *testing.T) {
	// TD-012 / T2, the negative leg. A copy of /bin/echo is placed in the
	// module cache dir: a tree the profile lets the workload READ but is not in
	// darwinExecDirs. Reading it inside the sandbox is the control that the
	// refusal is the exec rule, not the read tier; running it unsandboxed is the
	// control that the copy is a working binary at all.
	cache := t.TempDir()
	bin := filepath.Join(cache, "atcr-echo")
	src, err := os.ReadFile("/bin/echo")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(bin, src, 0o755))

	if control := runUnsandboxed(t, bin+" atcr-exec-ran"); control.exitCode != 0 || !strings.Contains(control.output, "atcr-exec-ran") {
		skipOrFail(t, "the copied echo does not run even unsandboxed (exit %d, %q), so a sandboxed refusal proves nothing",
			control.exitCode, strings.TrimSpace(control.output))
		return
	}

	cfg := DefaultOSLevelConfig()
	cfg.ModuleCacheDir = cache
	b := NewOSLevelBackend(cfg)
	if _, err := b.resolveToolPath(); err != nil {
		skipOrFail(t, "sandbox-exec is not usable on this host: %v", err)
		return
	}
	require.NoError(t, b.Preflight(context.Background()))

	res, err := b.Run(context.Background(), RunSpec{
		Command: []string{"/bin/sh", "-c",
			`/bin/cat "$0" >/dev/null && echo atcr-readable; "$0" atcr-exec-ran; echo "rc=$?"`, bin},
		SnapshotDir: t.TempDir(),
		Timeout:     10 * time.Second,
	})
	require.NoError(t, err, "output: %s", res.Output)
	require.Contains(t, res.Output, "atcr-readable",
		"the module cache must be readable, or the refusal below is the read tier and not the exec rule; output: %s", res.Output)
	assert.NotContains(t, res.Output, "atcr-exec-ran",
		"a binary outside darwinExecDirs ran — the exec narrowing is not enforced; output: %s", res.Output)
	assert.Contains(t, res.Output, "rc=126", "the shell must report the exec as refused; output: %s", res.Output)
}

func TestIntegration_OSLevelDarwin_ReadOnlyRunExecsItsSnapshot(t *testing.T) {
	// T2. A read-only run (the agent exec tool) executes files in the snapshot
	// in place, where a Writable run executes its scratch copy. execRule grants
	// the snapshot so `./script.sh` does not depend on RunSpec.Writable.
	b := darwinIntegrationBackend(t)
	snapshot := writeFixture(t, map[string]string{
		"proof.sh": "#!/bin/sh\necho atcr-snapshot-exec-ran\n",
	})
	require.NoError(t, os.Chmod(filepath.Join(snapshot, "proof.sh"), 0o755))

	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"/bin/sh", "-c", "./proof.sh 2>&1"},
		SnapshotDir: snapshot,
		Timeout:     10 * time.Second,
	})
	require.NoError(t, err, "output: %s", res.Output)
	require.Equal(t, 0, res.ExitCode, "a read-only run must exec its own snapshot; output: %s", res.Output)
	assert.Contains(t, res.Output, "atcr-snapshot-exec-ran", "output: %s", res.Output)
}
