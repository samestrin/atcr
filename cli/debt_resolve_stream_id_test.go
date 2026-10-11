package cli

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/reconcile"
	reclib "github.com/samestrin/atcr/reconcile"
)

// TestDebtResolve_StreamIDClosesRowEndToEnd proves AC4 of Epic 35.16.11.3: an id
// read off the reconciled stream closes the row it names in the local debt store.
// Every hop is the real one — `atcr reconcile` over fixture source findings (only
// the LLM fan-out is replaced, as in every reconcile test), the PersistForReconcile
// bridge writing ./.atcr/debt, ReadReconciledFindings loading findings.json, and
// `atcr debt resolve <id>` — so no fixture row is written by hand. The second half
// pins the no-row case: an out-of-scope finding carries a stream id but has no
// stored row, and resolving it is the not-found error.
func TestDebtResolve_StreamIDClosesRowEndToEnd(t *testing.T) {
	isolate(t)
	touchFiles(t, "a.go", "b.go")
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|leaks a file handle|close it|resource|10|ev|host\n" +
			"HIGH|b.go:2|unrelated legacy smell|n/a|" + reclib.CategoryOutOfScope + "|5|ev|host\n",
	})
	require.Equal(t, 0, execCmd(t, "reconcile", "r"))

	findings, err := reconcile.ReadReconciledFindings(filepath.Join(".atcr", "reviews", "r"))
	require.NoError(t, err)
	var inScope, outOfScope reconcile.JSONFinding
	for _, f := range findings {
		switch f.File {
		case "a.go":
			inScope = f
		case "b.go":
			outOfScope = f
		}
	}
	require.NotEmpty(t, inScope.ID, "the in-scope finding carries an id on findings.json")
	require.NotEmpty(t, outOfScope.ID, "an out-of-scope finding still carries an id on findings.json")
	require.Equal(t, reclib.CategoryOutOfScope, outOfScope.Category)
	require.NotEqual(t, inScope.ID, outOfScope.ID)

	// The bridge stored exactly the in-scope row, open, under the stream id.
	dir := localdebt.DefaultDir(".")
	before, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	require.Len(t, before, 1, "PersistForReconcile stores the in-scope finding only")
	require.Equal(t, inScope.ID, before[0].ID, "stored id equals the stream id")
	require.False(t, localdebt.IsClosedStatus(before[0].Status), "the stored row starts open")

	out, err := execute(t, "debt", "resolve", inScope.ID)
	require.NoError(t, err, out)
	assert.Contains(t, out, inScope.ID)

	after, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	folded := localdebt.FoldRecords(after)
	require.Len(t, folded, 1)
	assert.Equal(t, inScope.ID, folded[0].ID)
	assert.True(t, localdebt.IsClosedStatus(folded[0].Status),
		"the row named by the stream id folds closed, got status %q", folded[0].Status)

	// An id on the stream with no stored row is the not-found error, exit non-zero.
	out, err = execute(t, "debt", "resolve", outOfScope.ID)
	require.Error(t, err, out)
	assert.NotEqual(t, 0, exitCode(err))
	assert.Contains(t, err.Error(), "no open technical-debt item with id")
	assert.Contains(t, err.Error(), outOfScope.ID)
}
