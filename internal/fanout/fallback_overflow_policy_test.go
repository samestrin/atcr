package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.5.1 TD (resolved by clarification): buildFallbackAgent already
// DETECTS that the inherited prompt overflows the fallback's own budget — it
// warns and stamps the honest overflow degradation — but it shipped the doomed
// prompt regardless, whatever the operator's on_overflow policy said.
//
// The primary path routes its own overflow through applyOverflowPolicy so
// fail/fallback propagate a typed error and hard-fail PRE-DISPATCH. The fallback
// path now mirrors exactly that gate, and only that gate: chunk/truncate keep
// today's warn-and-ship, because both would require re-packing the slot's
// FileEntry list — which would make the fallback review a different file set
// than its primary and falsify the slot-level chunk accounting and baseline
// coverage tag it inherits unconditionally. That remains out of scope.

// oversizedFallbackCfg pairs a large declared primary (greta) with an undeclared
// backup (kai) on a model absent from the static table, so the fallback's budget
// is genuinely smaller than the payload the primary was sized for.
func oversizedFallbackCfg(t *testing.T, onOverflow string) *ReviewConfig {
	t.Helper()
	cfg := declaredWindowRoster(t, 512000)
	kai := cfg.Registry.Agents["kai"]
	kai.Model = "unlisted-backup-model" // undeclared → 32768 default
	cfg.Registry.Agents["kai"] = kai
	cfg.Settings.OnOverflow = onOverflow
	return cfg
}

func TestBuildFallbackAgent_OnOverflowFailHardFailsPreDispatch(t *testing.T) {
	cfg := oversizedFallbackCfg(t, OverflowFail)
	primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)

	_, _, err = buildFallbackAgent(cfg, primary, "kai", true, fallbackRefit{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOverflowPolicyFail,
		"on_overflow=fail must hard-fail the lane rather than dispatch a prompt the fallback cannot hold")
}

func TestBuildFallbackAgent_OnOverflowFallbackHardFailsPreDispatch(t *testing.T) {
	cfg := oversizedFallbackCfg(t, OverflowFallback)
	primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)

	_, _, err = buildFallbackAgent(cfg, primary, "kai", true, fallbackRefit{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFallbackUnavailable,
		"on_overflow=fallback must surface its typed error pre-dispatch, matching the primary path's arm")
}

func TestBuildFallbackAgent_OnOverflowChunkStillWarnsAndShips(t *testing.T) {
	// The default arm is deliberately unchanged: chunk/truncate need FileEntry
	// re-packing, so warn-and-ship remains the honest degradation there.
	for _, policy := range []string{OverflowChunk, OverflowTruncate, ""} {
		t.Run("policy="+policy, func(t *testing.T) {
			cfg := oversizedFallbackCfg(t, policy)
			primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
			require.NoError(t, err)

			var fb Agent
			out := captureStderr(t, func() {
				fb, _, err = buildFallbackAgent(cfg, primary, "kai", true, fallbackRefit{})
			})
			require.NoError(t, err, "a non-fail/fallback policy must still build the fallback")
			assert.Contains(t, out, "may overflow")
			assert.Equal(t, degradationOverflow, fb.DegradationAction)
		})
	}
}

func TestBuildFallbackAgent_OnOverflowFailDoesNotFireWhenPayloadFits(t *testing.T) {
	// The gate hangs off the SAME condition as the warning, so a fallback whose
	// budget comfortably holds the inherited payload must not be hard-failed just
	// because on_overflow=fail is configured. Without this, enabling the policy
	// would break every ordinary small-diff run that happens to have a backup.
	cfg := oversizedFallbackCfg(t, OverflowFail)
	primary, _, err := buildOneAgent(cfg, "greta", measurablePayload(2, 3000), ReviewRange{Base: "a", Head: "b"}, string(payload.ModeFiles), "")
	require.NoError(t, err)
	require.NotEmpty(t, primary.CodeContext, "precondition: the shipped payload is measurable")
	require.True(t, inheritedPayloadFits(primary, 32768),
		"precondition: a 6 KB payload must be measurably within the fallback's own budget")

	fb, _, err := buildFallbackAgent(cfg, primary, "kai", true, fallbackRefit{})
	require.NoError(t, err, "a fitting payload must not trip the overflow policy")
	assert.NotEqual(t, degradationOverflow, fb.DegradationAction,
		"and must not be stamped as an overflow either")
}

// Sprint 35.16.11.2.1 AC 03-01 Scenario 3 / Edge Case 3: the re-fit arm
// re-renders the fallback's prompt through renderAgent with the PRIMARY's
// config, so a re-rendered Agent carries the primary's response_format. The
// fallback must still send its own — on the re-fit arm and on the arm where the
// re-fit declines and the inherited payload ships.
func TestBuildFallbackAgent_RefitKeepsFallbackOwnResponseFormat(t *testing.T) {
	cases := []struct {
		name       string
		primaryRF  string
		fallbackRF string
	}{
		{"declared fallback, undeclared primary", "", registry.ResponseFormatJSONObject},
		{"declared primary, undeclared fallback", registry.ResponseFormatJSONObject, ""},
	}
	for _, tc := range cases {
		t.Run("refit/"+tc.name, func(t *testing.T) {
			cfg := refitRoster(t, 128000, OverflowTruncate)
			g := cfg.Registry.Agents["greta"]
			g.ResponseFormat = tc.primaryRF
			cfg.Registry.Agents["greta"] = g
			k := cfg.Registry.Agents["kai"]
			k.ResponseFormat = tc.fallbackRF
			cfg.Registry.Agents["kai"] = k

			slot := buildRefitSlot(t, cfg)
			fb := slot.Fallbacks[0]
			require.True(t, fb.rePacked, "precondition: this fixture's fallback takes the re-fit arm")
			assert.Equal(t, tc.primaryRF, slot.Primary.Invocation.ResponseFormat)
			assert.Equal(t, tc.fallbackRF, fb.Invocation.ResponseFormat,
				"a re-fit fallback still sends its own response_format, not the re-rendered primary's")
		})
		t.Run("refit declines/"+tc.name, func(t *testing.T) {
			cfg := oversizedFallbackCfg(t, OverflowTruncate)
			g := cfg.Registry.Agents["greta"]
			g.ResponseFormat = tc.primaryRF
			cfg.Registry.Agents["greta"] = g
			k := cfg.Registry.Agents["kai"]
			k.ResponseFormat = tc.fallbackRF
			cfg.Registry.Agents["kai"] = k

			primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
			require.NoError(t, err)
			var fb Agent
			captureStderr(t, func() {
				fb, _, err = buildFallbackAgent(cfg, primary, "kai", true, fallbackRefit{})
			})
			require.NoError(t, err)
			require.False(t, fb.rePacked, "precondition: no re-pack source, so the inherited payload ships")
			assert.Equal(t, tc.fallbackRF, fb.Invocation.ResponseFormat)
		})
	}
}
