package payload

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMode(t *testing.T) {
	for _, s := range []string{"diff", "blocks", "files"} {
		m, err := ParseMode(s)
		require.NoErrorf(t, err, "mode %q", s)
		assert.Equal(t, PayloadMode(s), m)
	}
	// empty resolves to the default, not an error.
	m, err := ParseMode("")
	require.NoError(t, err)
	assert.Equal(t, ModeBlocks, m)

	for _, bad := range []string{"DIFF", "Blocks", "invalid", "unified"} {
		_, err := ParseMode(bad)
		require.Errorf(t, err, "mode %q should be rejected", bad)
		assert.Contains(t, err.Error(), "must be one of diff, blocks, files")
	}
}

func TestValidMode(t *testing.T) {
	assert.True(t, ValidMode("diff"))
	assert.True(t, ValidMode("blocks"))
	assert.True(t, ValidMode("files"))
	assert.False(t, ValidMode("DIFF"))
	assert.False(t, ValidMode(""))
	assert.False(t, ValidMode("nope"))
}
