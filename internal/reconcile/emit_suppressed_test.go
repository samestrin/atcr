package reconcile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readSuppressed reads reconciled/suppressed.json raw and decoded, failing the
// test when the file is absent.
func readSuppressed(t *testing.T, reviewDir string) ([]byte, []JSONFinding) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(reviewDir, "reconciled", SuppressedJSON))
	require.NoError(t, err, "suppressed.json must be written on every run")
	var got []JSONFinding
	require.NoError(t, json.Unmarshal(raw, &got))
	return raw, got
}

func readReport(t *testing.T, reviewDir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(reviewDir, "reconciled", ReportMD))
	require.NoError(t, err)
	return string(raw)
}

// TestEmit_SuppressedJSON is the T4 success criterion: a run with no rules
// writes suppressed.json as `[]` and a report.md with no footer; a run with
// one rule lists the finding and its rule in suppressed.json, omits it from
// findings.json and report.md, and carries the footer.
func TestEmit_SuppressedJSON(t *testing.T) {
	const reason = "this package needs unsafe for the mmap view"

	t.Run("no rules", func(t *testing.T) {
		root, reviewDir := ignoreReview(t)
		_, err := runIgnoreReconcile(t, root, reviewDir)
		require.NoError(t, err)

		raw, got := readSuppressed(t, reviewDir)
		assert.Equal(t, "[]\n", string(raw), "an empty run renders `[]`, never `null`")
		assert.Empty(t, got)

		report := readReport(t, reviewDir)
		assert.Contains(t, report, "internal/auth/session.go:3")
		assert.NotContains(t, report, "Suppressed")
	})

	t.Run("one rule", func(t *testing.T) {
		root, reviewDir := ignoreReview(t)
		writeIgnoreYAML(t, root, "rules:\n"+
			"  - file: internal/auth/session.go\n"+
			"    category: security\n"+
			"    match: unsafe\\.Pointer\n"+
			"    reason: "+reason+"\n")
		_, err := runIgnoreReconcile(t, root, reviewDir)
		require.NoError(t, err)

		_, got := readSuppressed(t, reviewDir)
		require.Len(t, got, 1)
		assert.Equal(t, "internal/auth/session.go", got[0].File)
		assert.Equal(t, 3, got[0].Line)
		assert.Contains(t, got[0].Problem, "unsafe.Pointer")
		require.NotNil(t, got[0].SuppressedBy, "the record names the rule that suppressed it")
		assert.Equal(t, IgnoreRule{
			File:     "internal/auth/session.go",
			Category: "security",
			Match:    `unsafe\.Pointer`,
			Reason:   reason,
		}, *got[0].SuppressedBy)

		js, err := ReadReconciledFindings(reviewDir)
		require.NoError(t, err)
		require.Len(t, js, 1, "findings.json omits the suppressed finding")
		assert.Equal(t, "internal/store/db.go", js[0].File)
		assert.Nil(t, js[0].SuppressedBy)

		report := readReport(t, reviewDir)
		assert.NotContains(t, report, "internal/auth/session.go")
		assert.NotContains(t, report, "unsafe.Pointer")
		assert.Contains(t, report, "internal/store/db.go:7")
		assert.Contains(t, report, "## Suppressed Findings")
		assert.Contains(t, report, "- Suppressed: 1 (matched .atcr/ignore.yaml rules; recorded in suppressed.json)")
		assert.Contains(t, report, "- "+reason+" (1)")
	})
}

// TestRenderSuppressedFooter pins the footer's shape: absent when nothing was
// suppressed, reasons listed once each in first-seen order with their count,
// and a reason (PR-controlled text) escaped like every other free-text field.
func TestRenderSuppressedFooter(t *testing.T) {
	var empty bytes.Buffer
	require.NoError(t, writeSuppressedFooter(&empty, nil))
	assert.Empty(t, empty.String())

	rule := func(reason string) *IgnoreRule { return &IgnoreRule{Package: "p", Reason: reason} }
	var b bytes.Buffer
	require.NoError(t, writeSuppressedFooter(&b, []JSONFinding{
		{File: "p/a.go", SuppressedBy: rule("generated")},
		{File: "p/b.go", SuppressedBy: rule("<b>vendored</b>\n## forged")},
		{File: "p/c.go", SuppressedBy: rule("generated")},
		{File: "p/d.go"},
	}))
	assert.Equal(t, "\n## Suppressed Findings\n\n"+
		"- Suppressed: 4 (matched .atcr/ignore.yaml rules; recorded in suppressed.json)\n"+
		"- generated (2)\n"+
		"- &lt;b&gt;vendored&lt;/b&gt; ## forged (1)\n"+
		"- (no reason recorded) (1)\n", b.String())
}

// TestRenderMarkdown_SuppressedFooter: the exported renderer carries the same
// footer Emit writes, after the "No findings." line when every finding was
// suppressed.
func TestRenderMarkdown_SuppressedFooter(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, RenderMarkdown(&b, Result{Suppressed: []JSONFinding{
		{File: "p/a.go", SuppressedBy: &IgnoreRule{Package: "p", Reason: "generated"}},
	}}))
	assert.Contains(t, b.String(), "\nNo findings.\n\n## Suppressed Findings\n")
	assert.Contains(t, b.String(), "- generated (1)\n")
}
