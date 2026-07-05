package history

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSince(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"30d", 30 * 24 * time.Hour},
		{"2w", 2 * 7 * 24 * time.Hour},
		{"48h", 48 * time.Hour},
		{"90m", 90 * time.Minute},
		{"1h30m", 90 * time.Minute}, // native Go composite still works
		{"1d", 24 * time.Hour},
	}
	for _, c := range cases {
		got, err := ParseSince(c.in)
		require.NoError(t, err, "input %q", c.in)
		assert.Equal(t, c.want, got, "input %q", c.in)
	}
}

func TestParseSince_Invalid(t *testing.T) {
	for _, in := range []string{"", "  ", "abc", "d", "-5d", "0d", "0", "5x", "w"} {
		_, err := ParseSince(in)
		assert.Error(t, err, "input %q should be rejected", in)
	}
}

func recAt(pkg string, days int, now time.Time) Record {
	return Record{
		Timestamp: now.Add(-time.Duration(days) * 24 * time.Hour),
		Package:   pkg,
		Severity:  "HIGH",
		ID:        "x",
	}
}

func TestFilter_Since(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	recs := []Record{
		recAt("internal/registry", 5, now),
		recAt("internal/registry", 40, now), // outside a 30d window
		recAt("internal/registry", 29, now),
	}
	out := Filter(recs, 30*24*time.Hour, "", now)
	require.Len(t, out, 2)
	for _, r := range out {
		assert.NotEqual(t, 40, int(now.Sub(r.Timestamp).Hours()/24))
	}
}

func TestFilter_PackageSeparatorAware(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	recs := []Record{
		recAt("internal/registry", 1, now),     // exact match
		recAt("internal/registry/sub", 1, now), // nested match
		recAt("internal/registry2", 1, now),    // sibling — must NOT match
		recAt("internal/report", 1, now),       // unrelated
	}
	out := Filter(recs, 30*24*time.Hour, "internal/registry", now)
	require.Len(t, out, 2)
	pkgs := []string{out[0].Package, out[1].Package}
	assert.Contains(t, pkgs, "internal/registry")
	assert.Contains(t, pkgs, "internal/registry/sub")
	assert.NotContains(t, pkgs, "internal/registry2")
}

func TestFilter_PackageTrailingSlashAndEmpty(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	recs := []Record{
		recAt("internal/registry", 1, now),
		recAt("cmd/atcr", 1, now),
	}
	// Trailing slash on the query is normalized away.
	assert.Len(t, Filter(recs, 30*24*time.Hour, "internal/registry/", now), 1)
	// Empty package filter keeps everything (within the window).
	assert.Len(t, Filter(recs, 30*24*time.Hour, "", now), 2)
}
