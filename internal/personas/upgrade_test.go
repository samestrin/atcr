package personas

import (
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
