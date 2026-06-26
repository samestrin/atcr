package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/require"
)

// TestRegistrySecretValues resolves API key values from a registry's providers
// for the verify-only entry points (atcr verify / atcr_verify) that hold a
// resolved registry but never built a PreparedReview. Unset and below-floor
// values are skipped; identical resolved values dedupe.
func TestRegistrySecretValues(t *testing.T) {
	t.Setenv("ATCR_RSV_A", "sk-registrykeyvalue-aaaa")
	t.Setenv("ATCR_RSV_B", "sk-registrykeyvalue-bbbb")
	t.Setenv("ATCR_RSV_SHORT", "tiny")

	reg := &registry.Registry{Providers: map[string]registry.Provider{
		"a":     {APIKeyEnv: "ATCR_RSV_A"},
		"b":     {APIKeyEnv: "ATCR_RSV_B"},
		"short": {APIKeyEnv: "ATCR_RSV_SHORT"},
		"unset": {APIKeyEnv: "ATCR_RSV_UNSET"},
	}}
	got := RegistrySecretValues(reg)
	require.ElementsMatch(t, []string{"sk-registrykeyvalue-aaaa", "sk-registrykeyvalue-bbbb"}, got,
		"only set, at-or-above-floor provider key values are enumerated")

	require.Nil(t, RegistrySecretValues(nil), "a nil registry yields no secrets")
}

// slotForKeys builds a one-slot chain whose primary + fallbacks read the given
// env vars, mirroring cmd/atcr's slotWithKeys test helper.
func slotForKeys(envs ...string) Slot {
	s := Slot{Primary: Agent{Invocation: llmclient.Invocation{APIKeyEnv: envs[0]}}}
	for _, e := range envs[1:] {
		s.Fallbacks = append(s.Fallbacks, Agent{Invocation: llmclient.Invocation{APIKeyEnv: e}})
	}
	return s
}

// TestSecretValues_ResolvesPrimaryAndFallbacks verifies the helper resolves the
// set env values from both the primary agent and its fallback chain.
func TestSecretValues_ResolvesPrimaryAndFallbacks(t *testing.T) {
	t.Setenv("ATCR_SV_PRIMARY", "AIzaSyPrimaryKeyValue123456")
	t.Setenv("ATCR_SV_FALLBACK", "sk-fallbackkeyvalue7890")

	prep := &PreparedReview{Slots: []Slot{slotForKeys("ATCR_SV_PRIMARY", "ATCR_SV_FALLBACK")}}
	got, _ := prep.SecretValues()

	require.ElementsMatch(t, []string{"AIzaSyPrimaryKeyValue123456", "sk-fallbackkeyvalue7890"}, got,
		"both the primary and fallback resolved key values must be enumerated")
}

// TestSecretValues_DedupesIdenticalResolvedValues verifies two agents sharing a
// resolved key value contribute only one entry (the Redactor dedupes too, but
// the helper should not emit redundant work).
func TestSecretValues_DedupesIdenticalResolvedValues(t *testing.T) {
	t.Setenv("ATCR_SV_A", "AIzaSySharedKeyValue1234567")
	t.Setenv("ATCR_SV_B", "AIzaSySharedKeyValue1234567")

	prep := &PreparedReview{Slots: []Slot{
		slotForKeys("ATCR_SV_A"),
		slotForKeys("ATCR_SV_B"),
	}}
	got, _ := prep.SecretValues()

	require.Equal(t, []string{"AIzaSySharedKeyValue1234567"}, got,
		"identical resolved values must be deduped")
}

// TestSecretValues_SkipsUnsetEnv verifies an env var that is not set contributes
// no entry (no empty string passed to the Redactor).
func TestSecretValues_SkipsUnsetEnv(t *testing.T) {
	t.Setenv("ATCR_SV_SET", "AIzaSyOnlyOneIsSet123456789")
	// ATCR_SV_UNSET is intentionally never set.

	prep := &PreparedReview{Slots: []Slot{slotForKeys("ATCR_SV_SET", "ATCR_SV_UNSET")}}
	got, _ := prep.SecretValues()

	require.Equal(t, []string{"AIzaSyOnlyOneIsSet123456789"}, got,
		"an unset env var must not contribute an empty secret")
}

// TestSecretValues_SkipsShortValues locks the min-length guard (clarification
// 2026-06-20: len(s) < 8 is skipped, symmetric with the Redactor's empty guard)
// so a coincidentally-short or misconfigured env value can't over-redact logs.
func TestSecretValues_SkipsShortValues(t *testing.T) {
	t.Setenv("ATCR_SV_SHORT", "abc123")    // 6 chars < 8 → skipped
	t.Setenv("ATCR_SV_EXACT8", "abcd1234") // exactly 8 → kept
	t.Setenv("ATCR_SV_LONG", "AIzaSyLongEnoughKeyValue123")

	prep := &PreparedReview{Slots: []Slot{slotForKeys("ATCR_SV_SHORT", "ATCR_SV_EXACT8", "ATCR_SV_LONG")}}
	got, _ := prep.SecretValues()

	require.ElementsMatch(t, []string{"abcd1234", "AIzaSyLongEnoughKeyValue123"}, got,
		"values shorter than 8 chars must be skipped; 8-char and longer values kept")
}

// TestSecretValues_EmptySlots verifies no slots yields no secrets (nil/empty),
// never a panic.
func TestSecretValues_EmptySlots(t *testing.T) {
	prep := &PreparedReview{}
	secrets, warnings := prep.SecretValues()
	require.Empty(t, secrets, "no slots must yield no secrets")
	require.Empty(t, warnings, "no slots must yield no warnings")
}

// TestSecretValues_WarnsOnConfiguredButUnusableSlot verifies a named APIKeyEnv
// that resolves empty or below the floor yields a warning (never the value),
// while a slot with no APIKeyEnv configured stays silent.
func TestSecretValues_WarnsOnConfiguredButUnusableSlot(t *testing.T) {
	t.Setenv("ATCR_SV_SHORTKEY", "abc") // configured but below the floor
	// ATCR_SV_MISSINGKEY intentionally unset.

	prep := &PreparedReview{Slots: []Slot{
		slotForKeys("ATCR_SV_SHORTKEY", "ATCR_SV_MISSINGKEY"),
		{Primary: Agent{Invocation: llmclient.Invocation{APIKeyEnv: ""}}}, // no key env → silent
	}}
	secrets, warnings := prep.SecretValues()

	require.Empty(t, secrets, "no usable key values")
	require.Len(t, warnings, 2, "one warning per configured-but-unusable env; the unconfigured slot is silent")
	for _, w := range warnings {
		require.NotContains(t, w, "abc", "a warning must never contain the resolved value")
	}
	require.Contains(t, warnings[0], "ATCR_SV_SHORTKEY")
	require.Contains(t, warnings[1], "ATCR_SV_MISSINGKEY")
}

// TestSecretValues_WarnsOncePerEnv verifies the same misconfigured env across
// two slots warns only once.
func TestSecretValues_WarnsOncePerEnv(t *testing.T) {
	// ATCR_SV_DUP intentionally unset, referenced by two slots.
	prep := &PreparedReview{Slots: []Slot{
		slotForKeys("ATCR_SV_DUP"),
		slotForKeys("ATCR_SV_DUP"),
	}}
	_, warnings := prep.SecretValues()
	require.Len(t, warnings, 1, "a repeated misconfigured env warns once")
}
