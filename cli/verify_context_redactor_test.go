package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/sandbox"
	"github.com/samestrin/atcr/internal/verify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunVerify_InstallsAContextLevelRedactor closes the gap that made the
// standalone `atcr verify` path the only one with no review-scoped redaction.
//
// newRedactor(absRoot, …) was wired ONLY into verify.Options.Redactor, which
// scrubs evidence text before it is persisted into findings.json. log.NewContext
// appeared nowhere in cli/verify.go, so every LOG line on this path — including
// the os-level fallback-engaged notice, which carries the docker preflight cause
// with absolute host paths — ran under the base log.NewRedactor("") from
// setupLogger for the whole process lifetime. That redactor has no review root,
// so it cannot relativize a path.
//
// `atcr review` differs: correlateAndRedact (cli/review.go) installs a path-aware
// redactor on the context. This is that wiring, for verify.
func TestRunVerify_InstallsAContextLevelRedactor(t *testing.T) {
	isolate(t)
	writeVerifyRegistry(t)
	require.NoError(t, os.Mkdir("redactrepo", 0o755))
	verifyFixture(t, "r", []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "x",
	}})
	t.Setenv("ATCR_LOG_LEVEL", "info")

	absRepo, err := filepath.Abs("redactrepo")
	require.NoError(t, err)

	// Log an absolute path under the reviewed-repo root THROUGH the context the
	// pipeline is handed. If the context carries the review-scoped redactor, it
	// renders relative; under the base redactor it renders absolute.
	orig := verifyRun
	verifyRun = func(ctx context.Context, repoRoot, reviewDir string, reg *registry.Registry, opts verify.Options) (verify.Result, error) {
		log.FromContext(ctx).Info("probe", "path", filepath.Join(absRepo, "internal", "secret.go"))
		return orig(ctx, repoRoot, reviewDir, reg, opts)
	}
	t.Cleanup(func() { verifyRun = orig })

	code, out := execCmdCapture(t, "verify", "r", "--repo", "redactrepo")
	require.Equal(t, 0, code)

	require.Contains(t, out, "probe", "the probe line must have been emitted at all")
	assert.NotContains(t, out, absRepo,
		"an absolute reviewed-repo path reached the log: the standalone verify path has no context-level redactor")
	assert.Contains(t, out, "internal/secret.go",
		"relativization must strip the root, not swallow the path")
}

// TestRunVerify_ContextRedactorAndEvidenceRedactorAreTheSame pins that one
// redactor serves both sinks.
//
// Two separately-constructed redactors could silently diverge — different root,
// different secret set — leaving log lines and persisted findings.json scrubbed
// to different standards, with nothing to catch it. Building once and sharing
// makes the two provably identical.
func TestRunVerify_ContextRedactorAndEvidenceRedactorAreTheSame(t *testing.T) {
	isolate(t)
	writeVerifyRegistry(t)
	require.NoError(t, os.Mkdir("samerepo", 0o755))
	verifyFixture(t, "r", []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "x",
	}})

	var built int
	origNew := newRedactor
	newRedactor = func(reviewRoot string, secrets ...string) *log.Redactor {
		built++
		return origNew(reviewRoot, secrets...)
	}
	t.Cleanup(func() { newRedactor = origNew })

	var gotOpts verify.Options
	orig := verifyRun
	verifyRun = func(ctx context.Context, repoRoot, reviewDir string, reg *registry.Registry, opts verify.Options) (verify.Result, error) {
		gotOpts = opts
		return orig(ctx, repoRoot, reviewDir, reg, opts)
	}
	t.Cleanup(func() { verifyRun = orig })

	code, _ := execCmdCapture(t, "verify", "r", "--repo", "samerepo")
	require.Equal(t, 0, code)

	assert.Equal(t, 1, built,
		"exactly one redactor must be constructed and shared, or the log and findings.json sinks can drift apart")
	assert.NotNil(t, gotOpts.Redactor, "the evidence redactor must still be wired")
}

// TestRunVerify_RedactorIsInstalledBeforeExecResolution pins the ORDERING, which
// is the half a wiring test alone would miss.
//
// The os-level fallback-engaged notice is emitted from inside resolveExec, and
// resolveExec ran BEFORE the repo root was even resolved. Installing the redactor
// after that call would leave the one log line this row was filed about still
// unredacted — a fix that looks complete and changes nothing where it matters.
//
// The probe logs THROUGH the ctx resolveExec hands the emit seam, from inside the
// run, so the assertion is against what the real notice would have rendered.
func TestRunVerify_RedactorIsInstalledBeforeExecResolution(t *testing.T) {
	isolate(t)
	writeVerifyExecRegistry(t)
	require.NoError(t, os.Mkdir("orderrepo", 0o755))
	verifyFixture(t, "r", []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "x",
	}})
	t.Setenv("ATCR_LOG_LEVEL", "info")

	absRepo, err := filepath.Abs("orderrepo")
	require.NoError(t, err)

	origResolve := resolveExecBackendFn
	resolveExecBackendFn = func(context.Context, bool, *registry.SandboxConfig) (sandbox.Backend, []string, time.Duration, error) {
		return &nameOnlyBackend{name: registry.SandboxFallbackOSLevel}, []string{"go", "test"}, time.Minute, nil
	}
	t.Cleanup(func() { resolveExecBackendFn = origResolve })
	captureSnapshotGate(t, nil)
	captureToolchainGate(t, nil)

	var emitted bool
	prevEmit := emitPendingFallbackWarningFn
	emitPendingFallbackWarningFn = func(ctx context.Context, b sandbox.Backend) {
		emitted = true
		log.FromContext(ctx).Info("fallback-probe", "cause", filepath.Join(absRepo, "deep", "path.go"))
	}
	t.Cleanup(func() { emitPendingFallbackWarningFn = prevEmit })

	code, out := execCmdCapture(t, "verify", "r", "--repo", "orderrepo", "--exec")
	require.Equal(t, 0, code, "output: %s", out)

	require.True(t, emitted, "resolveExec must have reached the pending-warning emit")
	require.Contains(t, out, "fallback-probe", "the probe line must have been emitted at all")
	assert.NotContains(t, out, absRepo,
		"the fallback notice renders through a context with no review root — the redactor is installed too late")
	assert.Contains(t, out, "deep/path.go")
}

// writeVerifyExecRegistry is writeVerifyRegistry plus a sandbox block, so --exec
// passes config validation and resolveExec proceeds to the gates.
func writeVerifyExecRegistry(t *testing.T) {
	t.Helper()
	writeVerifyRegistry(t)
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"),
		[]byte("agents:\n  - bruce\nsandbox:\n  backend: docker\n  image: alpine:3.20\n  test_command: [go, test]\n  fallback: os-level\n"), 0o644))
}
