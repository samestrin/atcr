package fanout

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// UNSUPPORTED is the one verdict the claim ledger exists to produce, and the
// section instructs reviewers to emit it with NO line number when no changed
// line settles it (internal/payload/claims.go, claimLedgerSection). That
// instruction is only safe if a line-less finding actually survives the
// grounding gate and the 7-column parser — and the justification for believing
// it does lived only in a comment. Nothing produced an UNSUPPORTED-shaped
// finding and ran it through internal/fanout/grounding.go, so if the parser or
// the gate rejected a line-less FILE column every such finding would be
// discarded before a human read it and the feature would ship inert.
//
// This drives the real chain — ParseModelOutput inside ExecuteReview, then
// groundFindings, then reconcile — and asserts the verdict reaches report.md.

// unsupportedClaimCompleter returns the exact finding shape the ledger contract
// asks for: a line-less citation on a file the diff DOES change. The second row
// is the control — an UNSUPPORTED verdict pinned to a file the diff never
// touched, which the gate must still drop, so a green result cannot come from a
// gate that simply keeps everything.
type unsupportedClaimCompleter struct{}

func (unsupportedClaimCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "Adjudicating the claims to verify.\n\n" +
		"HIGH|auth.go|UNSUPPORTED: the branch claims b() validates the token, but the diff adds no validation|Implement the claimed validation or withdraw the claim|correctness|30|no changed line settles this claim\n" +
		"MEDIUM|never/touched.go:99|UNSUPPORTED: a claim about a file this diff never changes|withdraw the claim|correctness|10|cited on an unchanged file\n", nil
}

func TestClaimLedger_LinelessUnsupportedFindingSurvivesGroundingAndReachesTheReport(t *testing.T) {
	repo, base, head := initRepo(t)
	cfg := twoAgentConfig("http://unused") // both agents emit the same verdict
	req := reviewReq(repo, repo, base, head)
	req.OutputDir = filepath.Join(t.TempDir(), "review")

	prep, err := PrepareReview(context.Background(), cfg, req)
	require.NoError(t, err)
	require.NotNil(t, prep.Changed,
		"precondition: grounding must be ACTIVE, or the gate this test exists for never runs")

	_, err = ExecuteReview(context.Background(), unsupportedClaimCompleter{}, prep)
	require.NoError(t, err)

	_, err = reconcile.RunReconcile(context.Background(), prep.Dir, nil,
		reconcile.Options{Root: repo, ReconciledAt: time.Unix(1000, 0).UTC()})
	require.NoError(t, err)

	findings, err := reconcile.ReadReconciledFindings(prep.Dir)
	require.NoError(t, err)

	var kept *reconcile.JSONFinding
	for i, f := range findings {
		if f.File == "auth.go" {
			kept = &findings[i]
		}
		assert.NotEqual(t, "never/touched.go", f.File,
			"a verdict cited on a file the diff never changed must be dropped by the grounding gate")
	}
	require.NotNil(t, kept,
		"the line-less UNSUPPORTED verdict was discarded before reconcile — the ledger's driving verdict never reaches a human")
	assert.Zero(t, kept.Line,
		"the finding must survive WITH no line number; a fabricated line would defeat the contract's own instruction")
	assert.Contains(t, kept.Problem, "UNSUPPORTED")

	report, err := os.ReadFile(filepath.Join(prep.Dir, "reconciled", "report.md"))
	require.NoError(t, err)
	assert.Contains(t, string(report), "UNSUPPORTED",
		"the verdict must reach the artifact a human actually reads")
	assert.NotContains(t, string(report), "never/touched.go",
		"the ungrounded control verdict must not reach report.md")
}
