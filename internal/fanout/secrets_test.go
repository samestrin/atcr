package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/require"
)

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
	got := prep.SecretValues()

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
	got := prep.SecretValues()

	require.Equal(t, []string{"AIzaSySharedKeyValue1234567"}, got,
		"identical resolved values must be deduped")
}

// TestSecretValues_SkipsUnsetEnv verifies an env var that is not set contributes
// no entry (no empty string passed to the Redactor).
func TestSecretValues_SkipsUnsetEnv(t *testing.T) {
	t.Setenv("ATCR_SV_SET", "AIzaSyOnlyOneIsSet123456789")
	// ATCR_SV_UNSET is intentionally never set.

	prep := &PreparedReview{Slots: []Slot{slotForKeys("ATCR_SV_SET", "ATCR_SV_UNSET")}}
	got := prep.SecretValues()

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
	got := prep.SecretValues()

	require.ElementsMatch(t, []string{"abcd1234", "AIzaSyLongEnoughKeyValue123"}, got,
		"values shorter than 8 chars must be skipped; 8-char and longer values kept")
}

// TestSecretValues_EmptySlots verifies no slots yields no secrets (nil/empty),
// never a panic.
func TestSecretValues_EmptySlots(t *testing.T) {
	prep := &PreparedReview{}
	require.Empty(t, prep.SecretValues(), "no slots must yield no secrets")
}
