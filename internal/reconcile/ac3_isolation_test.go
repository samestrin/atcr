package reconcile

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAC3_HallucinationIsolatedToAmbiguousSidecar proves the Epic 13.2 AC3
// end-to-end contract at the emit boundary: a single-model hallucination
// reported amid a corroborated consensus is removed from findings.json and
// isolated into ambiguous.json as a single-finding cluster. This is what lets
// the sidecar be trusted as strictly uncorroborated noise.
func TestAC3_HallucinationIsolatedToAmbiguousSidecar(t *testing.T) {
	sources := []Source{
		{Name: "pool", Findings: []stream.Finding{
			mf("HIGH", "auth.go", 42, "token never expires unchecked", "guard it", "security", 15, "pool saw it", "greta"),
			mf("LOW", "auth.go", 42, "totally unrelated hallucinated nonsense", "n/a", "style", 5, "pool only", "greta"),
		}},
		{Name: "host", Findings: []stream.Finding{
			mf("HIGH", "auth.go", 43, "token never expires unchecked", "guard it", "security", 15, "host also", "host"),
		}},
	}
	res := Reconcile(sources, Options{ReconciledAt: time.Unix(1700000000, 0).UTC()})

	dir := t.TempDir()
	require.NoError(t, Emit(dir, res))

	findings, err := os.ReadFile(filepath.Join(dir, FindingsJSON))
	require.NoError(t, err)
	amb, err := os.ReadFile(filepath.Join(dir, AmbiguousJSON))
	require.NoError(t, err)

	assert.NotContains(t, string(findings), "hallucinated nonsense",
		"the uncorroborated hallucination must be removed from the consensus findings")
	assert.Contains(t, string(amb), "hallucinated nonsense",
		"the hallucination must be isolated into the ambiguous.json sidecar")
	assert.Contains(t, string(findings), "token never expires",
		"the corroborated consensus finding stays in findings.json")

	// The sidecar entry is a single-finding noise cluster (debate skips < 2).
	require.Len(t, res.Ambiguous, 1, "exactly one isolated cluster")
	assert.Len(t, res.Ambiguous[0].Findings, 1, "noise cluster carries one finding")
	assert.Equal(t, "totally unrelated hallucinated nonsense", res.Ambiguous[0].Findings[0].Problem)
}
