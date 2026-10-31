package reconcile

import (
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/history"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TD internal/reconcile/gate.go:355: routeUnresolved splices Tier 4 records out
// BEFORE stampSymbolAnchors and stampFindingIDs run, so a record in
// reconciled/unresolved.json keeps the id the derived path stamped over its
// un-anchored problem. That id is self-consistent — it hashes the very problem
// text unresolved.json emits — and it is never persisted, because
// PersistForReconcile reads only the primary stream. The format doc's "What the
// ID identifies" passage describes findings.json alone (hashed AFTER the anchor),
// so a reader of unresolved.json had no published rule for its id.
//
// The behaviour is pinned through a real RunReconcile so the doc sentence
// cannot outlive it, and the doc is pinned so the sentence cannot be dropped.
func TestUnresolvedSidecar_IDHashesTheUnanchoredProblem(t *testing.T) {
	root := gitRepoWithFiles(t, "internal/auth/session.go")
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/ghost/phantom.go:9|`quantumFlux` leaks a handle|close it|security|10|ev|greta\n"+
			"LOW|internal/auth/session.go:3|`RefreshToken` is slow|cache it|perf|5|ev|greta\n")

	runReconcileWithFakeTier4(t, root, reviewDir, &fakeTier4{})

	sidecar, err := ReadUnresolvedFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, sidecar, 1)
	rec := sidecar[0]
	require.NotEmpty(t, rec.ID, "a routed record still carries an id")
	assert.Equal(t, history.FindingID(rec.File, rec.Line, rec.Problem), rec.ID,
		"the id hashes the problem unresolved.json emits, which no symbol anchor was stamped onto")

	doc := readRepoFile(t, "../../docs/findings-format.md")
	line := docLineContaining(t, doc, "**Routed records (`unresolved.json`).**")
	assert.Contains(t, line, "un-anchored problem",
		"the doc must say which problem text an unresolved.json id hashes")
	assert.Contains(t, line, "never persisted",
		"and that no .atcr/debt/ row carries it")
}
