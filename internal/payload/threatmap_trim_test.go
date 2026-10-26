package payload

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTrimToPaths(t *testing.T) {
	ledger := newClaimLedgerEntry("CLAIMS BLOCK")
	prefetch := newPrefetchEntry("CONTEXT BLOCK")
	// A repository file literally named after the ledger sentinel is
	// reviewable: the exemption keys on shedExempt, never on Path.
	forged := FileEntry{Path: ClaimLedgerPath, Size: 5}

	tests := []struct {
		name        string
		in          []FileEntry
		keep        map[string]bool
		wantKept    []string
		wantDropped []string
	}{
		{
			name:        "entries outside keep are dropped and listed sorted",
			in:          entries("z.go", 1, "a.go", 2, "m.go", 3, "b.go", 4),
			keep:        map[string]bool{"m.go": true},
			wantKept:    []string{"m.go"},
			wantDropped: []string{"a.go", "b.go", "z.go"},
		},
		{
			name:        "a false value in keep does not keep the path",
			in:          entries("a.go", 1, "b.go", 2),
			keep:        map[string]bool{"a.go": true, "b.go": false},
			wantKept:    []string{"a.go"},
			wantDropped: []string{"b.go"},
		},
		{
			name:        "shed-exempt sections survive a keep that does not name them",
			in:          append([]FileEntry{prefetch, ledger}, entries("a.go", 1, "b.go", 2)...),
			keep:        map[string]bool{"b.go": true},
			wantKept:    []string{PrefetchContextPath, ClaimLedgerPath, "b.go"},
			wantDropped: []string{"a.go"},
		},
		{
			name:        "shed-exempt sections survive when every file is dropped",
			in:          append([]FileEntry{ledger}, entries("a.go", 1)...),
			keep:        map[string]bool{"other.go": true},
			wantKept:    []string{ClaimLedgerPath},
			wantDropped: []string{"a.go"},
		},
		{
			name:        "a repo file at the sentinel path is trimmed like any file",
			in:          []FileEntry{forged, {Path: "a.go"}},
			keep:        map[string]bool{"a.go": true},
			wantKept:    []string{"a.go"},
			wantDropped: []string{ClaimLedgerPath},
		},
		{
			name:     "every entry kept drops nothing",
			in:       entries("a.go", 1, "b.go", 2),
			keep:     map[string]bool{"a.go": true, "b.go": true, "extra.go": true},
			wantKept: []string{"a.go", "b.go"},
		},
		{
			name:     "empty keep returns every entry unchanged",
			in:       append(entries("b.go", 1, "a.go", 2), ledger),
			keep:     map[string]bool{},
			wantKept: []string{"b.go", "a.go", ClaimLedgerPath},
		},
		{
			name:     "nil keep returns every entry unchanged",
			in:       entries("b.go", 1, "a.go", 2),
			keep:     nil,
			wantKept: []string{"b.go", "a.go"},
		},
		{
			name:     "no entries",
			in:       nil,
			keep:     map[string]bool{"a.go": true},
			wantKept: []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]FileEntry(nil), tc.in...)
			kept, dropped := TrimToPaths(tc.in, tc.keep)
			assert.Equal(t, tc.wantKept, keptPaths(kept))
			assert.Equal(t, tc.wantDropped, dropped)
			assert.Equal(t, before, tc.in, "TrimToPaths must not modify its input")
		})
	}
}

// An empty keep hands back the very entries it was given, bodies, sizes,
// modes and sentinels included.
func TestTrimToPathsEmptyKeepIsIdentity(t *testing.T) {
	in := []FileEntry{
		{Path: "a.go", Size: 3, Body: "abc", Mode: PayloadMode("full")},
		newClaimLedgerEntry("CLAIMS"),
	}
	kept, dropped := TrimToPaths(in, map[string]bool{})
	assert.Equal(t, in, kept)
	assert.Nil(t, dropped)
}

// Kept entries are copied whole, so a kept shed-exempt section is still
// exempt from the byte budget afterwards.
func TestTrimToPathsKeepsExemptionSentinel(t *testing.T) {
	kept, _ := TrimToPaths([]FileEntry{newClaimLedgerEntry("CLAIMS"), {Path: "a.go"}},
		map[string]bool{"a.go": true})
	assert.Equal(t, 1, ReviewableCount(kept))
	assert.True(t, kept[0].shedExempt)
}
