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

// TestHashPersonaID_CanonicalizesCaseAndWhitespace pins epic 35.16.1's contract:
// the digest is the SOLE persona identity the telemetry backend ever holds, and
// atcr.dev peppers it irreversibly on arrival (HMAC-SHA256 under a never-rotated
// secret, pre-pepper value discarded). A casing or padding variant therefore does
// not merely look untidy — it mints a SECOND permanent, unmergeable backend
// identity for one persona, and there is no backfill that can repair it later.
//
// So every variant must collapse to one digest BEFORE it leaves the process.
// cloudsync.go:104 already trimmed at its own call site for exactly this reason
// (see TestNewCloudSyncRecord_TrimsAgentNameBeforeHashing, "fragments the Persona
// Leaderboard into two buckets for one identity"); canonicalizing inside the hash
// generalizes that one-site fix to every producer, present and future.
func TestHashPersonaID_CanonicalizesCaseAndWhitespace(t *testing.T) {
	want := HashPersonaID("bruce")
	for _, variant := range []string{"Bruce", "BRUCE", " bruce ", "\tBruce\n", "bRuCe"} {
		assert.Equal(t, want, HashPersonaID(variant),
			"variant %q must canonicalize to the same digest as %q — a split here is permanent once atcr.dev peppers it", variant, "bruce")
	}

	// Whitespace-only canonicalizes to the empty string, so it takes the same path
	// as a genuinely empty persona rather than minting its own bucket.
	assert.Equal(t, HashPersonaID(""), HashPersonaID("   "),
		"whitespace-only must canonicalize to the empty-string digest")
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
// IDs and asserts pairwise distinctness plus per-input reproducibility (AC 03-04).
//
// SUPERSEDED IN PART by epic 35.16.1. This list used to also carry "bruce " and
// "Bruce" and assert they hashed DISTINCTLY from "bruce" — the opposite of what
// HashPersonaID now guarantees. Those two entries are removed deliberately, and must
// NOT be restored as a "regression fix": collapsing casing and padding variants onto
// one digest is the intended contract, pinned by
// TestHashPersonaID_CanonicalizesCaseAndWhitespace above. Distinctness across
// genuinely DIFFERENT personas is the property this test still exists to protect,
// and the 22 remaining ids (still ≥ 20, per the require below) all differ by more
// than case or whitespace.
func TestHashPersonaID_UniquenessAcrossDifferentInputs(t *testing.T) {
	ids := []string{
		"bruce", "alice", "carol", "dave", "eve", "frank", "grace", "heidi",
		"ivan", "judy", "mallory", "niaj", "olivia", "peggy", "rupert", "sybil",
		"trent", "victor", "walter", "yvonne", "zoe", "审阅者-42",
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

// TestHashPersonaID_PinnedPublishedPersonaDigests is the CROSS-LANGUAGE contract
// (epic 35.16.1 AC5). atcr.dev classifies a stored digest as a community persona by
// pre-computing HMAC(pepper, SHA256(canonical_name)) over the published roster and
// matching it — which only works if its JS reproduces this canonicalization exactly.
//
// These expected values were NOT generated from this Go code. They were computed
// independently with node:
//
//	crypto.createHash("sha256").update(name.trim().toLowerCase()).digest("hex")
//
// so this test proves Go and the server agree rather than merely proving Go is
// self-consistent. If it fails, the two implementations have diverged and atcr.dev's
// persona dictionary will silently match nothing — do NOT "fix" it by regenerating
// the constants from Go; re-derive them from the JS above and find out which side moved.
//
// Names are real entries in personas/community/.
func TestHashPersonaID_PinnedPublishedPersonaDigests(t *testing.T) {
	pinned := map[string]string{
		"sonny":  "57f0e30b29126a4866ff1ba8da6f62d104007d322e40ddbdeee93c8a4a771f78",
		"glenna": "d3e7fc7752f78729498a143d6f9547ce74ab291639ea6d2ffb74a0d7264382e9",
		"milo":   "13adffea089b50908ce4d41d4ee483ea99cc375350fd664461ee979263fae45b",
	}
	for name, want := range pinned {
		assert.Equal(t, want, HashPersonaID(name),
			"digest for published persona %q must match the value atcr.dev's dictionary computes in JS", name)
	}

	// The same digests must be reached from non-canonical spellings — that is the
	// whole reason the server can key its dictionary on one entry per persona.
	assert.Equal(t, pinned["sonny"], HashPersonaID("Sonny"))
	assert.Equal(t, pinned["glenna"], HashPersonaID("  GLENNA  "))
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
