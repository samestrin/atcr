package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmit_FindingsJSONPublishedLast pins the publish order: findings.json is
// the marker the fan-out status reads as "reconciled", so every sibling must
// already exist when it appears. Each sibling is blocked in turn by a
// non-empty directory at its path (the atomic rename onto it fails); Emit must
// then error without having published findings.json.
func TestEmit_FindingsJSONPublishedLast(t *testing.T) {
	siblings := []string{
		FindingsTxt, ReportMD, SummaryJSON, AmbiguousJSON,
		DisagreementsJSON, UnresolvedJSON, SuppressedJSON,
	}
	for _, name := range siblings {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "reconciled")
			blocker := filepath.Join(dir, name)
			require.NoError(t, os.MkdirAll(filepath.Join(blocker, "keep"), 0o755))

			require.Error(t, Emit(dir, Result{}))
			_, err := os.Stat(filepath.Join(dir, FindingsJSON))
			assert.True(t, os.IsNotExist(err), "findings.json must not exist when %s failed to publish", name)
		})
	}
}
