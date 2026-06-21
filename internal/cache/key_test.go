package cache

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashText_FormatAndDeterminism(t *testing.T) {
	h := HashText("hello world")
	require.True(t, strings.HasPrefix(h, "sha256:"), "digest must carry the canonical sha256: prefix")
	hex := strings.TrimPrefix(h, "sha256:")
	assert.Len(t, hex, 64, "sha256 hex digest is 64 chars")

	// Deterministic: same input -> same digest.
	assert.Equal(t, h, HashText("hello world"))
	// Distinct inputs -> distinct digests.
	assert.NotEqual(t, h, HashText("hello worlD"))
	// Empty input still produces a well-formed digest.
	assert.True(t, strings.HasPrefix(HashText(""), "sha256:"))
}

func TestKey_DeterministicAndFormat(t *testing.T) {
	ph := HashText("payload bytes")
	sh := HashText("persona text")

	k1 := Key(ph, "gpt-4o", sh)
	k2 := Key(ph, "gpt-4o", sh)
	assert.Equal(t, k1, k2, "same triple must yield the same key")
	assert.True(t, strings.HasPrefix(k1, "sha256:"), "key must be a sha256: digest")
	assert.Len(t, strings.TrimPrefix(k1, "sha256:"), 64)
}

func TestKey_VariesWithEveryComponent(t *testing.T) {
	ph := HashText("payload bytes")
	sh := HashText("persona text")
	base := Key(ph, "gpt-4o", sh)

	// Payload hash changes -> key changes.
	assert.NotEqual(t, base, Key(HashText("other payload"), "gpt-4o", sh))
	// Model changes -> key changes.
	assert.NotEqual(t, base, Key(ph, "claude-opus", sh))
	// Persona hash changes -> key changes.
	assert.NotEqual(t, base, Key(ph, "gpt-4o", HashText("other persona")))
}

// TestKey_DomainSeparation guards against boundary ambiguity: two distinct
// triples whose naive concatenation would coincide must still produce distinct
// keys (NUL-separated fields).
func TestKey_DomainSeparation(t *testing.T) {
	a := Key("aa", "bb", "cc")
	b := Key("a", "abb", "cc")
	c := Key("aa", "b", "bcc")
	assert.NotEqual(t, a, b)
	assert.NotEqual(t, a, c)
	assert.NotEqual(t, b, c)
}
