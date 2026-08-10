package verify

import (
	"context"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFallbackWarning_IsDeferredUntilTheCallerCommits covers the ordering defect.
//
// warnOSLevelFallbackEngaged fired the moment the OS-level preflight passed — but
// the caller's snapshot and toolchain gates can still REFUSE immediately
// afterwards. The operator then read "os-level sandbox fallback engaged ... runs_as
// invoking user" describing a run that never executed anything.
//
// That is the same confusion TestFallbackWarning_SilentWhenDockerSucceedsOrFallbackFails
// already forbids for the sibling case ("nothing was engaged, so the refusal is
// the whole signal") — the two paths were simply decided inconsistently.
func TestFallbackWarning_IsDeferredUntilTheCallerCommits(t *testing.T) {
	ctx, buf := captureWarnings(t)
	stubOSLevel(t, nil)

	b, _, _, err := ResolveExecBackend(ctx, true, daemonDownShim(t))
	require.NoError(t, err)
	require.NotNil(t, b)

	assert.NotContains(t, buf.String(), "os-level sandbox fallback engaged",
		"the resolver must not warn before the caller's gates have accepted the run")

	// The caller commits: now, and only now, the operator learns the isolation
	// model changed.
	EmitPendingFallbackWarning(ctx, b)
	assert.Contains(t, buf.String(), "os-level sandbox fallback engaged")
	assert.Contains(t, buf.String(), "uid 65534",
		"the deferred warning must carry the same content, not a reduced one")
}

// TestFallbackWarning_NeverFiresWhenTheCallerRefuses is the negative test the TD
// row explicitly asks for: gate refuses -> no engagement warning at all. It holds
// by construction here, because a refusing caller simply never reaches the emit.
func TestFallbackWarning_NeverFiresWhenTheCallerRefuses(t *testing.T) {
	ctx, buf := captureWarnings(t)
	stubOSLevel(t, nil)

	b, _, _, err := ResolveExecBackend(ctx, true, daemonDownShim(t))
	require.NoError(t, err)
	require.NotNil(t, b)

	// Caller's gate refuses; EmitPendingFallbackWarning is never called.
	assert.NotContains(t, buf.String(), "os-level sandbox fallback engaged",
		"a run refused after resolution must produce no engagement notice")
}

// TestFallbackWarning_EmitsAtMostOnce guards the decorator's own hazard: the emit
// is called from two CLI sites and a future third would double-log the same
// engagement, which reads as two fallbacks.
func TestFallbackWarning_EmitsAtMostOnce(t *testing.T) {
	ctx, buf := captureWarnings(t)
	stubOSLevel(t, nil)

	b, _, _, err := ResolveExecBackend(ctx, true, daemonDownShim(t))
	require.NoError(t, err)

	EmitPendingFallbackWarning(ctx, b)
	EmitPendingFallbackWarning(ctx, b)
	EmitPendingFallbackWarning(ctx, b)

	assert.Equal(t, 1, strings.Count(buf.String(), "os-level sandbox fallback engaged"),
		"one engagement is one notice, however many call sites ask")
}

// TestFallbackWarning_EmitIsSafeOnAnUndecoratedBackend keeps the helper total.
// The docker path returns a bare backend and the CLI calls the emit
// unconditionally, so a type assertion that panicked (or a nil deref) would crash
// every successful docker run.
func TestFallbackWarning_EmitIsSafeOnAnUndecoratedBackend(t *testing.T) {
	ctx, buf := captureWarnings(t)

	assert.NotPanics(t, func() { EmitPendingFallbackWarning(ctx, nil) })
	assert.NotPanics(t, func() { EmitPendingFallbackWarning(ctx, &fakeOSLevel{}) })
	assert.Empty(t, buf.String(), "an undecorated backend carries no pending warning")
}

// TestFallbackDecorator_ForwardsNameToTheWrappedBackend is the hazard test, and
// the reason this decorator needs one at all.
//
// CheckOSLevelSnapshotUsable and CheckOSLevelToolchainReachable both REFUSE any
// backend they cannot positively identify, and their godoc names the exact shape:
// "a decorating wrapper, which reports its own Name()". A decorator that reported
// its own name would turn both gates into a hard refusal for every fallback run —
// the fix for a cosmetic log-ordering bug would break the feature outright.
func TestFallbackDecorator_ForwardsNameToTheWrappedBackend(t *testing.T) {
	ctx, _ := captureWarnings(t)
	stubOSLevel(t, nil)

	b, _, _, err := ResolveExecBackend(ctx, true, daemonDownShim(t))
	require.NoError(t, err)

	assert.Equal(t, registry.SandboxFallbackOSLevel, b.Name(),
		"the decorator must report the WRAPPED backend's name, or both gates fail-closed on it")

	// The end-to-end consequence, asserted rather than inferred: the gates must
	// still recognize the decorated backend.
	assert.NoError(t, CheckOSLevelSnapshotUsable(b, nil, t.TempDir(), false),
		"the snapshot gate must still recognize a decorated backend")
	assert.NoError(t, CheckOSLevelToolchainReachable(b, []string{"go"}),
		"the toolchain gate must still recognize a decorated backend")
}

// TestFallbackDecorator_ForwardsRunAndPreflight pins the rest of the interface.
// A decorator that swallowed Run would silently execute nothing while every
// assertion above still passed.
func TestFallbackDecorator_ForwardsRunAndPreflight(t *testing.T) {
	ctx, _ := captureWarnings(t)
	fake, _, _ := stubOSLevel(t, nil)

	b, _, _, err := ResolveExecBackend(ctx, true, daemonDownShim(t))
	require.NoError(t, err)

	before := fake.preflights
	require.NoError(t, b.Preflight(context.Background()))
	assert.Equal(t, before+1, fake.preflights, "Preflight must reach the wrapped backend")

	_, runErr := b.Run(context.Background(), sandbox.RunSpec{})
	assert.ErrorContains(t, runErr, "fakeOSLevel does not execute",
		"Run must reach the wrapped backend, not be absorbed by the decorator")
}
