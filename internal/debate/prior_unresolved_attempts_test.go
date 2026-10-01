package debate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeDebateFileFixture writes df to dir's reconciled/debate.json.
func writeDebateFileFixture(t *testing.T, dir string, df DebateFile) {
	t.Helper()
	recon := filepath.Join(dir, reconciledSubdir)
	require.NoError(t, os.MkdirAll(recon, 0o755))
	raw, err := json.Marshal(df)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(recon, DebateJSON), raw, 0o644))
}

// TestPriorUnresolvedAttempts_CarriesTheWithheldOverflowRecord pins the half of
// priorUnresolvedAttempts that survived mutation: the df.Overflow loop.
//
// The run that WITHHOLDS an item writes no item entry for it — only an overflow
// record carrying OverflowAttemptsExhausted and the count. Reading df.Items alone
// therefore loses the history on the very next run, the item re-enters selection,
// and the three-strike ceiling holds for exactly one run. Deleting the loop leaves
// the rest of the suite green, so this is the only thing standing between that
// loop and a silent removal.
func TestPriorUnresolvedAttempts_CarriesTheWithheldOverflowRecord(t *testing.T) {
	dir := t.TempDir()
	writeDebateFileFixture(t, dir, DebateFile{
		SchemaVersion: DebateSchemaVersion,
		Overflow: []OverflowItem{{
			File: "a.go", Line: 7, Problem: "withheld finding",
			Reason: OverflowAttemptsExhausted, UnresolvedAttempts: maxUnresolvedAttempts,
		}},
	})

	got := priorUnresolvedAttempts(dir)

	key := FindingKey{File: "a.go", Line: 7, Problem: "withheld finding"}
	require.Contains(t, got, key, "a withheld record is the only carrier of its own history")
	assert.GreaterOrEqual(t, got[key], maxUnresolvedAttempts,
		"the ceiling must stay reached, or the item is re-debated forever")
}

// TestPriorUnresolvedAttempts_FloorsAPreCountWithheldRecord covers the floor
// inside the same loop: a record written before UnresolvedAttempts existed
// unmarshals to 0, and reading that literally would re-open an item the ceiling
// already closed.
func TestPriorUnresolvedAttempts_FloorsAPreCountWithheldRecord(t *testing.T) {
	dir := t.TempDir()
	writeDebateFileFixture(t, dir, DebateFile{
		SchemaVersion: DebateSchemaVersion,
		Overflow: []OverflowItem{{
			File: "b.go", Line: 11, Problem: "legacy withheld finding",
			Reason: OverflowAttemptsExhausted, // UnresolvedAttempts absent -> 0
		}},
	})

	got := priorUnresolvedAttempts(dir)

	key := FindingKey{File: "b.go", Line: 11, Problem: "legacy withheld finding"}
	assert.Equal(t, maxUnresolvedAttempts, got[key],
		"a withheld record proves the ceiling was reached even when it carries no count")
}

// TestPriorUnresolvedAttempts_IgnoresACapOverflowRecord is the complement that
// keeps the loop's guard honest: an ordinary max_items cap overflow is not
// evidence anyone tried to debate the item, so it must contribute no count.
func TestPriorUnresolvedAttempts_IgnoresACapOverflowRecord(t *testing.T) {
	dir := t.TempDir()
	writeDebateFileFixture(t, dir, DebateFile{
		SchemaVersion: DebateSchemaVersion,
		Overflow: []OverflowItem{{
			File: "c.go", Line: 3, Problem: "never debated, just over the cap",
		}},
	})

	got := priorUnresolvedAttempts(dir)

	assert.NotContains(t, got, FindingKey{File: "c.go", Line: 3, Problem: "never debated, just over the cap"},
		"a cap overflow records no attempt — counting it would withhold an item nobody tried")
}
