package reconcile

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/require"
)

// goldenCorpusSources is the representative pre-extraction reconcile input whose
// emitted findings.json / ambiguous.json / disagreements.json are committed under
// testdata/golden as the byte-identical oracle for the Epic 8.0 extraction
// (sprint 8.0 task 2.1). It exercises every determinism-critical path the port
// must preserve:
//   - a clean two-reviewer merge (HIGH confidence, no disagreement)   — auth.go
//   - a severity-disagreement merge (LOW vs CRITICAL, 2 reviewers)    — db.go
//   - a gray-zone ambiguous pair left unmerged (Jaccard in [0.4,0.7)) — pay.go
//   - a solo finding (MEDIUM confidence)                              — util.go
//   - an out-of-scope finding (annotated, excluded from the radar)    — legacy.go
//
// The fixed ReconciledAt and the deterministic sort/Jaccard/hash paths make the
// emitted bytes stable, so a post-extraction re-run must reproduce them exactly.
func goldenCorpusSources() []Source {
	return []Source{
		{Name: "pool", Findings: []stream.Finding{
			mf("HIGH", "auth.go", 42, "token never expires here", "guard it", "security", 15, "pool saw it", "greta"),
			mf("CRITICAL", "db.go", 100, "sql injection in query builder", "parametrize input", "security", 30, "pool repro", "greta"),
			mf("MEDIUM", "pay.go", 10, "session token expires without refresh check", "add refresh", "security", 20, "pool note", "greta"),
			mf("LOW", "util.go", 5, "unused import lingers in file", "remove it", "style", 2, "pool lint", "greta"),
			mf("HIGH", "legacy.go", 7, "preexisting smell outside the diff", "n/a", "out-of-scope", 0, "pool oos", "greta"),
		}},
		{Name: "host", Findings: []stream.Finding{
			mf("HIGH", "auth.go", 43, "token never expires here", "guard it", "security", 15, "host also", "host"),
			mf("LOW", "db.go", 101, "sql injection in query builder", "parametrize input", "security", 30, "host low", "host"),
			mf("MEDIUM", "pay.go", 12, "session token expires without bound", "cap it", "security", 20, "host note", "host"),
		}},
	}
}

// goldenCorpusResult runs the deterministic pipeline over the corpus and stamps a
// single Verification block (an unverifiable tie with two skeptics) onto the
// db.go finding — the realistic post-reconcile verify-stage mutation — so the
// golden artifacts also lock down *Verification omitempty serialization and the
// verification_disagreement radar path.
func goldenCorpusResult() Result {
	res := Reconcile(goldenCorpusSources(), Options{ReconciledAt: time.Unix(1700000000, 0).UTC()})
	for i := range res.Findings {
		if res.Findings[i].File == "db.go" {
			res.Findings[i].Verification = &Verification{
				Verdict: VerdictUnverifiable,
				Skeptic: "skeptic-a, skeptic-b",
				Notes:   "skeptics split on exploitability",
			}
		}
	}
	return res
}

// TestGoldenCorpus_ByteIdentical emits the corpus artifacts and asserts they are
// byte-identical to the committed testdata/golden snapshot. Regenerate the golden
// files (only when a deliberate, reviewed behavior change is intended) with:
//
//	UPDATE_GOLDEN=1 go test ./internal/reconcile/ -run TestGoldenCorpus_ByteIdentical
//
// This is the Phase-3 oracle: after the consumer import-flip, the same emit must
// reproduce these exact bytes (sprint 8.0 AC 01-05).
func TestGoldenCorpus_ByteIdentical(t *testing.T) {
	res := goldenCorpusResult()

	dir := t.TempDir()
	require.NoError(t, Emit(dir, res))

	goldenDir := filepath.Join("testdata", "golden")
	for _, name := range []string{FindingsJSON, AmbiguousJSON, DisagreementsJSON} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err, "read emitted %s", name)

		goldenPath := filepath.Join(goldenDir, name)
		if os.Getenv("UPDATE_GOLDEN") == "1" {
			require.NoError(t, os.MkdirAll(goldenDir, 0o755))
			require.NoError(t, os.WriteFile(goldenPath, got, 0o644), "write golden %s", name)
			continue
		}

		want, err := os.ReadFile(goldenPath)
		require.NoError(t, err, "read golden %s (run with UPDATE_GOLDEN=1 to create)", name)
		require.Equal(t, string(want), string(got),
			"%s drifted from the pre-extraction byte-identical baseline", name)
	}
}
