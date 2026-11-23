package localdebt

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compact reports the malformed lines its rewrite drops (EST-8260): only those
// in shards it rewrites. A shard of nothing but malformed lines is frozen and
// kept byte-for-byte, so its lines are not counted.
func TestCompact_CountsDroppedMalformedLines(t *testing.T) {
	dir := t.TempDir()
	writeShard(t, dir, "2026-04", "{not json", "also not json")
	good := detection("a real finding", "2026-05-01T00:00:00Z")
	writeShard(t, dir, "2026-05", recordLine(t, good), "{bad", "[1]", `{"id":`)

	plan, err := CompactPlan(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.Equal(t, 3, plan.Malformed, "the dry run counts what the real run drops")

	res, err := Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.Equal(t, 3, res.Malformed)

	again, err := Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.Equal(t, 0, again.Malformed, "the rewrite removed them")
}

func TestCompact_NoStoreCreatesNothing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no", "debt")
	for _, compact := range []func(string, ReadOpts) (CompactResult, error){Compact, CompactPlan} {
		res, err := compact(missing, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		assert.False(t, res.StoreFound)
		_, statErr := os.Lstat(filepath.Dir(missing))
		assert.True(t, os.IsNotExist(statErr), "no directory may be created for a missing store")
	}
}
