package verify

import (
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckOSLevelSnapshotUsable_ThreadsOperatorConfigIntoTheCheck pins the
// mechanism that makes the sc parameter load-bearing DESPITE no field of it
// changing the verdict today.
//
// The parameter reads as inert — osLevelFallbackConfig(sc)'s only
// operator-derived field is Timeout, and sandbox.CheckSnapshotUsable never reads
// Timeout — which invited "delete the dead parameter". Deleting it is the wrong
// move: the check would then validate against DefaultOSLevelConfig forever, so
// the first config-sourced field added to OSLevelConfig (a ToolPath or ScratchDir
// from the operator's sandbox block) would silently stop being checked, while the
// gate kept reporting success. That is a fail-OPEN drift with no test to catch it.
//
// Keeping sc and reconstructing the config in-function means such a field is
// picked up with ZERO resolver-signature change — which matters because AC 03-03
// pins both resolvers' return shapes, so threading the resolver's already-built
// config through instead would force exactly the edits that constraint forbids.
//
// This test is the mechanical proof of that claim: it asserts the operator's
// value actually reaches the checker, so the wiring cannot rot unobserved.
func TestCheckOSLevelSnapshotUsable_ThreadsOperatorConfigIntoTheCheck(t *testing.T) {
	var gotCfg sandbox.OSLevelConfig
	var called int
	prev := checkSnapshotUsableFn
	checkSnapshotUsableFn = func(cfg sandbox.OSLevelConfig, snapshotDir string, writable bool) error {
		called++
		gotCfg = cfg
		return nil
	}
	t.Cleanup(func() { checkSnapshotUsableFn = prev })

	secs := 137
	sc := &registry.SandboxConfig{TimeoutSecs: &secs}

	require.NoError(t, CheckOSLevelSnapshotUsable(&fakeOSLevel{}, sc, t.TempDir(), true))

	require.Equal(t, 1, called, "the gate must actually reach the checker")
	assert.Equal(t, 137*time.Second, gotCfg.Timeout,
		"the operator's sandbox block must reach the containment check; if this breaks, sc has become genuinely inert and the gate is validating defaults")
}

// TestCheckOSLevelSnapshotUsable_NilConfigStillChecks is the paired control. sc is
// nil at every current call site's test, and a nil-guard regression that turned
// the whole gate into a no-op for nil would be invisible without this.
func TestCheckOSLevelSnapshotUsable_NilConfigStillChecks(t *testing.T) {
	var called int
	prev := checkSnapshotUsableFn
	checkSnapshotUsableFn = func(cfg sandbox.OSLevelConfig, snapshotDir string, writable bool) error {
		called++
		assert.Positive(t, cfg.Timeout,
			"a nil sc must still yield the hardened defaults, never a zero config")
		return nil
	}
	t.Cleanup(func() { checkSnapshotUsableFn = prev })

	require.NoError(t, CheckOSLevelSnapshotUsable(&fakeOSLevel{}, nil, t.TempDir(), false))
	assert.Equal(t, 1, called)
}
