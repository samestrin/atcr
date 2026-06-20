package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateFindingPaths_SkipsWhenRootEmpty: with no base dir configured,
// validation is a no-op so existing reconcile tests (synthetic paths) are never
// falsely flagged.
func TestValidateFindingPaths_SkipsWhenRootEmpty(t *testing.T) {
	findings := []Merged{{Finding: stream.Finding{File: "does/not/exist.go"}}}
	validateFindingPaths(findings, "")

	assert.False(t, findings[0].PathValid)
	assert.Empty(t, findings[0].PathWarning)
}

// TestValidateFindingPaths_StampsWhenRootSet: a configured root validates each
// merged finding against the filesystem.
func TestValidateFindingPaths_StampsWhenRootSet(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "exists.go"), []byte("package x\n"), 0o644))

	findings := []Merged{
		{Finding: stream.Finding{File: "exists.go"}},
		{Finding: stream.Finding{File: "missing.go"}},
	}
	validateFindingPaths(findings, root)

	assert.True(t, findings[0].PathValid)
	assert.Empty(t, findings[0].PathWarning)
	assert.False(t, findings[1].PathValid)
	assert.Equal(t, stream.PathNotFoundWarning, findings[1].PathWarning)
}

// TestJSONFindings_CarriesPathValidation: path validity flows into the
// findings.json record so the report command (which reads findings.json) can
// surface the warning.
func TestJSONFindings_CarriesPathValidation(t *testing.T) {
	r := Result{Findings: []Merged{
		{Finding: stream.Finding{
			Severity: "HIGH", File: "missing.go", Line: 7,
			PathValid: false, PathWarning: stream.PathNotFoundWarning,
		}},
		{Finding: stream.Finding{
			Severity: "LOW", File: "real.go", Line: 1, PathValid: true,
		}},
	}}

	js := r.JSONFindings()
	require.Len(t, js, 2)
	assert.False(t, js[0].PathValid)
	assert.Equal(t, stream.PathNotFoundWarning, js[0].PathWarning)
	assert.True(t, js[1].PathValid)
	assert.Empty(t, js[1].PathWarning)
}
