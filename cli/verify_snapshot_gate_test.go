package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/sandbox"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// execResolveFixture stages an --exec resolution whose backend resolution always
// succeeds, so the only thing under test is WHICH directories the snapshot gate
// is asked about.
func execResolveFixture(t *testing.T) (*cobra.Command, *registry.ProjectConfig) {
	t.Helper()
	prev := resolveExecBackendFn
	resolveExecBackendFn = func(context.Context, bool, *registry.SandboxConfig) (sandbox.Backend, []string, time.Duration, error) {
		return &stubOSLevelBackend{}, []string{"go", "test"}, time.Minute, nil
	}
	t.Cleanup(func() { resolveExecBackendFn = prev })

	cmd := newVerifyCmd()
	require.NoError(t, cmd.Flags().Set("exec", "true"))
	proj := &registry.ProjectConfig{Sandbox: &registry.SandboxConfig{
		Image: "alpine:3.20", TestCommand: []string{"go", "test"},
	}}
	return cmd, proj
}

// stubOSLevelBackend is a minimal os-level-named backend; the gate seam is stubbed
// separately, so nothing here is ever executed.
type stubOSLevelBackend struct{}

func (stubOSLevelBackend) Name() string                    { return registry.SandboxFallbackOSLevel }
func (stubOSLevelBackend) Preflight(context.Context) error { return nil }
func (stubOSLevelBackend) Run(context.Context, sandbox.RunSpec) (sandbox.RunResult, error) {
	return sandbox.RunResult{}, errors.New("stub does not execute")
}

// gateCall records one checkOSLevelSnapshotFn invocation.
type gateCall struct {
	dir      string
	writable bool
}

// captureSnapshotGate swaps the shared pre-check seam and records every directory
// it is asked about. Both gate call sites share the package var, so the cleanup
// is mandatory (see the seam's own doc comment).
func captureSnapshotGate(t *testing.T, ret error) *[]gateCall {
	t.Helper()
	var calls []gateCall
	prev := checkOSLevelSnapshotFn
	checkOSLevelSnapshotFn = func(b sandbox.Backend, sc *registry.SandboxConfig, dir string, writable bool) error {
		calls = append(calls, gateCall{dir: dir, writable: writable})
		return ret
	}
	t.Cleanup(func() { checkOSLevelSnapshotFn = prev })
	return &calls
}

// TestResolveExec_GatesTheTempSnapshotRootNotOnlyTheRepoRoot covers the
// wrong-directory defect.
//
// resolveExec gates absRoot, but --exec does not necessarily RUN against
// absRoot: buildDispatcher takes the run's root from
// tools.NewSnapshotManager(repoRoot).SnapshotFor(head), which returns the live
// worktree ONLY on the clean-tree fast path. Any dirty worktree gets a detached
// copy under os.MkdirTemp("", "atcr-snapshot-") — a path unrelated to absRoot. So
// on the dirty path the gate validated a directory the run never uses: it could
// false-refuse (a repo under $HOME rejected even though the sandboxed dir is an
// OS temp path) and false-pass (a TMPDIR the generators would reject, never
// inspected).
//
// The fix validates os.TempDir() ITSELF alongside absRoot rather than predicting
// the leaf: checkSnapshotUsableFor calls filepath.EvalSymlinks, which errors on a
// path that does not exist yet, and os.MkdirTemp always nests under os.TempDir()
// with a metacharacter-free random suffix — so a safe os.TempDir() implies every
// snapshot leaf under it is safe for these pure path checks.
func TestResolveExec_GatesTheTempSnapshotRootNotOnlyTheRepoRoot(t *testing.T) {
	calls := captureSnapshotGate(t, nil)

	cmd, proj := execResolveFixture(t)
	_, _, _, err := resolveExec(cmd, proj)
	require.NoError(t, err)

	require.NotEmpty(t, *calls, "the gate must run at all")

	tmp := filepath.Clean(os.TempDir())
	var sawTemp bool
	for _, c := range *calls {
		if filepath.Clean(c.dir) == tmp {
			sawTemp = true
		}
		assert.False(t, c.writable, "--exec call sites leave RunSpec.Writable false")
	}
	assert.True(t, sawTemp,
		"the temp-snapshot root the dirty-worktree path actually runs against must be gated too; gated dirs were %v", *calls)
}

// TestResolveExec_TempRootRefusalIsAUsageError pins that the added check keeps the
// existing failure CLASS. The whole reason the pre-check lives in resolveExec
// rather than in buildDispatcher is that a refusal here is a fail-fast usage
// error (exit 2) before the review starts, instead of a pipeline error that
// degrades findings to unverifiable. A new check that refused with a different
// class would trade one defect for another.
func TestResolveExec_TempRootRefusalIsAUsageError(t *testing.T) {
	captureSnapshotGate(t, errors.New("os-level sandbox cannot contain this directory: staged refusal"))

	cmd, proj := execResolveFixture(t)
	_, _, _, err := resolveExec(cmd, proj)

	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err),
		"a snapshot-gate refusal must stay a fail-fast usage error, not become a pipeline degradation")
}
