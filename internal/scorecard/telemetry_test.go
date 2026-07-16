package scorecard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var hexDigestRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// TestHashPersonaID_Deterministic asserts a stable, stdlib-exact hex SHA-256
// digest across repeated calls — there is no salt or seeded RNG, so a repeat call
// stands in for a process restart (AC 03-01, 03-04).
func TestHashPersonaID_Deterministic(t *testing.T) {
	h1 := HashPersonaID("bruce")
	h2 := HashPersonaID("bruce")
	assert.Equal(t, h1, h2)
	assert.True(t, hexDigestRe.MatchString(h1), "want 64-char lowercase hex, got %q", h1)

	sum := sha256.Sum256([]byte("bruce"))
	assert.Equal(t, hex.EncodeToString(sum[:]), h1, "must be the stdlib SHA-256 hex digest")
}

// TestHashPersonaID_EmptyPathAndUnicode covers the AC 03-01 edge cases: the empty
// string hashes to the well-known SHA-256 constant; a path-like value is hashed
// raw (no scrubbing); Unicode input yields a valid digest without panic.
func TestHashPersonaID_EmptyPathAndUnicode(t *testing.T) {
	assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", HashPersonaID(""))

	p := HashPersonaID("/Users/sam/reviewer")
	assert.True(t, hexDigestRe.MatchString(p))
	assert.NotContains(t, p, "Users", "raw path value must not survive into the digest")

	u := HashPersonaID("审阅者-42")
	assert.True(t, hexDigestRe.MatchString(u), "unicode input must produce a valid 64-hex digest")
}

// TestHashPersonaID_UniquenessAcrossDifferentInputs hashes 20+ distinct Persona
// IDs (including near-identical whitespace/case variants) and asserts pairwise
// distinctness plus per-input reproducibility (AC 03-04).
func TestHashPersonaID_UniquenessAcrossDifferentInputs(t *testing.T) {
	ids := []string{
		"bruce", "alice", "carol", "dave", "eve", "frank", "grace", "heidi",
		"ivan", "judy", "mallory", "niaj", "olivia", "peggy", "rupert", "sybil",
		"trent", "victor", "walter", "yvonne", "zoe", "bruce ", "Bruce", "审阅者-42",
	}
	require.GreaterOrEqual(t, len(ids), 20)
	seen := map[string]string{}
	for _, id := range ids {
		h := HashPersonaID(id)
		if prev, ok := seen[h]; ok {
			t.Fatalf("hash collision: %q and %q both hash to %s", prev, id, h)
		}
		seen[h] = id
		assert.Equal(t, h, HashPersonaID(id), "hash must reproduce for %q", id)
	}
}

// TestHashPersonaID_NonReversible asserts the raw input never appears in the
// digest. The function signature carries no error return and no io.Writer, so
// there is no logging/error path that could leak the raw value (AC 03-04).
func TestHashPersonaID_NonReversible(t *testing.T) {
	raw := "correct-horse-battery-staple-42"
	h := HashPersonaID(raw)
	assert.NotContains(t, h, raw)
	assert.NotContains(t, h, "correct")
}

// TestTelemetryPersonaSchema_SeparateFromPublicRecord asserts the new
// TelemetryPersonaRecord is a distinct schema: it hashes the Reviewer, passes
// Model through unhashed, and its JSON leaks no raw persona id, reviewer, or
// run_id, and none of the PublicRecord allowlist field names (AC 03-02).
func TestTelemetryPersonaSchema_SeparateFromPublicRecord(t *testing.T) {
	rec := Record{Reviewer: "bruce", Model: "claude-sonnet-4-6", RunID: "2026-06-15-bruce"}
	tr := NewTelemetryPersonaRecord(rec)

	assert.Equal(t, HashPersonaID("bruce"), tr.PersonaIDHash, "persona id is hashed")
	assert.Equal(t, "claude-sonnet-4-6", tr.Model, "model passes through unhashed (non-PII)")

	raw, err := json.Marshal(tr)
	require.NoError(t, err)
	s := string(raw)
	assert.NotContains(t, s, "bruce", "raw persona id must not appear")
	assert.NotContains(t, s, "2026-06-15-bruce", "run_id must not appear")
	assert.NotContains(t, strings.ToLower(s), "reviewer")
	assert.NotContains(t, s, "run_id")
	assert.NotContains(t, s, "persona\"", "must not reuse the PublicRecord 'persona' key")

	// The JSON key set is a deliberate allowlist of its own.
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	for k := range m {
		assert.Contains(t, []string{"persona_id_hash", "model"}, k, "unexpected key %q in telemetry schema", k)
	}
	assert.Contains(t, m, "persona_id_hash")

	// Zero-value Record does not panic and hashes the empty string.
	trZero := NewTelemetryPersonaRecord(Record{})
	assert.Equal(t, HashPersonaID(""), trZero.PersonaIDHash)
}
