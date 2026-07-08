package personas

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionOf_ValidYAML(t *testing.T) {
	data := []byte("version: \"1.2.3\"\n")
	v, err := versionOf(data)
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", v)
}

func TestVersionOf_MissingVersion(t *testing.T) {
	data := []byte("provider: anthropic\n")
	v, err := versionOf(data)
	require.NoError(t, err)
	assert.Equal(t, "-", v)
}

func TestVersionOf_CorruptYAMLReturnsError(t *testing.T) {
	data := []byte("version: [unclosed\n")
	_, err := versionOf(data)
	require.Error(t, err, "corrupt local YAML must surface a parse error")
	assert.Contains(t, err.Error(), "parse")
}

func TestIsNewer_MixedValidityTreatsAsUpToDate(t *testing.T) {
	cases := []struct {
		local, remote string
	}{
		{"v1.0.0", "latest"},
		{"latest", "v1.0.0"},
		{"v2.0.0", "v1.0.0"},
	}
	for _, c := range cases {
		assert.False(t, isNewer(c.local, c.remote), "isNewer(%q, %q) should treat mixed/ambiguous validity as up-to-date", c.local, c.remote)
	}
}

// --- Upgrade paired-write behavior (TD-007) ---------------------------------

func TestUpgrade_WritesMarkdownWhenRemoteHasOne(t *testing.T) {
	remoteYAML := `provider: anthropic
model: claude-sonnet-4-6
role: reviewer
version: "1.1.0"
`
	remoteMD := "# Upgraded OWASP reviewer\n"
	srv := testServer(t, map[string]string{
		"/security/owasp.yaml": remoteYAML,
		"/security/owasp.md":   remoteMD,
	})
	dir := t.TempDir()
	installFixture(t, dir, "security/owasp", validPersonaYAML) // v1.0.0, no .md

	res, err := Upgrade(srv.Client(), srv.URL, dir, "security/owasp", false)
	require.NoError(t, err)
	assert.True(t, res.Upgraded)

	gotMD, err := os.ReadFile(filepath.Join(dir, "security", "owasp.md"))
	require.NoError(t, err)
	assert.Equal(t, remoteMD, string(gotMD))
}

func TestUpgrade_RemovesStaleMarkdownWhenBindingOnly(t *testing.T) {
	remoteYAML := `provider: anthropic
model: claude-sonnet-4-6
role: reviewer
version: "1.1.0"
`
	// Remote no longer ships a co-located .md; a stale local .md must be removed.
	srv := testServer(t, map[string]string{"/security/owasp.yaml": remoteYAML})
	dir := t.TempDir()
	installFixture(t, dir, "security/owasp", validPersonaYAML)
	staleMD := filepath.Join(dir, "security", "owasp.md")
	require.NoError(t, os.WriteFile(staleMD, []byte("# stale"), 0o644))

	res, err := Upgrade(srv.Client(), srv.URL, dir, "security/owasp", false)
	require.NoError(t, err)
	assert.True(t, res.Upgraded)
	assert.NoFileExists(t, staleMD, "stale .md must be removed when remote is binding-only")
}

func TestUpgrade_StrictDecodeRejectsUnknownField(t *testing.T) {
	badYAML := validPersonaYAML + "unknown_strict_field: value\n"
	srv := testServer(t, map[string]string{"/security/owasp.yaml": badYAML})
	dir := t.TempDir()
	installFixture(t, dir, "security/owasp", validPersonaYAML)

	_, err := Upgrade(srv.Client(), srv.URL, dir, "security/owasp", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in type")

	// Local unit must remain untouched.
	got, _ := os.ReadFile(filepath.Join(dir, "security", "owasp.yaml"))
	assert.Equal(t, validPersonaYAML, string(got))
	assert.NoFileExists(t, filepath.Join(dir, "security", "owasp.md"))
}
