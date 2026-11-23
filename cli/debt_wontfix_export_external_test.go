package cli_test

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/cli"
)

// TestExportWontfixRecords_CallableFromAnotherModule calls the seam the way an
// embedder must: from outside the package, with no internal/ import.
func TestExportWontfixRecords_CallableFromAnotherModule(t *testing.T) {
	var got []cli.WontfixRecord
	got, err := cli.ExportWontfixRecords(cli.WontfixExportOptions{
		Store:       filepath.Join(t.TempDir(), "absent"),
		Diagnostics: io.Discard,
	})
	require.NoError(t, err)
	require.Empty(t, got)
}
