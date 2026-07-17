package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTelemetryReport_NoReconcileImport locks AC 04-02's content-free guarantee at
// the import-graph level: the maintainer report sources EXCLUSIVELY from Story 1's
// content-free localdebt.AggregateQualitySignal, never from internal/reconcile or
// the raw reconciled findings loader. A future edit that reached into
// internal/reconcile (whose JSONFinding carries file paths and finding text) would
// reopen the exact leak channel the privacy line forbids, so it is blocked
// structurally rather than left to review.
func TestTelemetryReport_NoReconcileImport(t *testing.T) {
	dir, err := os.Getwd()
	require.NoError(t, err)

	var srcPath string
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			srcPath = filepath.Join(dir, "cmd", "atcr", "telemetry_report.go")
			break
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "go.mod not found above working directory")
		dir = parent
	}

	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, srcPath, nil, parser.ImportsOnly)
	require.NoError(t, perr, "telemetry_report.go must parse")

	for _, imp := range f.Imports {
		p, uerr := strconv.Unquote(imp.Path.Value)
		require.NoError(t, uerr)
		assert.NotContains(t, p, "internal/reconcile",
			"telemetry_report.go must not import internal/reconcile (content-free source only)")
	}

	data, rerr := os.ReadFile(srcPath)
	require.NoError(t, rerr)
	src := string(data)
	// Check the quoted import literal (not the bare substring) so a doc comment that
	// merely names the forbidden package to explain WHY is not a false positive.
	assert.NotContains(t, src, `"github.com/samestrin/atcr/internal/reconcile"`,
		"telemetry_report.go must not import internal/reconcile")
	assert.NotContains(t, src, "readReconciledFindings",
		"telemetry_report.go must not call the reconciled-findings loader (raw finding content)")
}
