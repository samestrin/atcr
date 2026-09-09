package fanout

import (
	"context"
	"encoding/json"
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

// The manifest is the artifact an operator reads AFTER the fact, and it is where
// the claim ledger's outcome has to be recoverable: a run that lost its ledger to
// a transient git failure was previously byte-for-byte indistinguishable there
// from a branch whose commits asserted nothing. This drives a real review and
// reads the persisted manifest, so the whole wire — RangeBuilder → buildPayloads
// → manifest → disk — is under test, not just the accessor.
func TestManifest_ClaimLedgerStatusDistinguishesFailureFromAClaimFreeBranch(t *testing.T) {
	readManifest := func(t *testing.T, dir string) map[string]any {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(b, &m))
		return m
	}

	t.Run("a claiming branch records present with a claim count", func(t *testing.T) {
		// fanoutRepo's head commit carries a subject and two bullets.
		repo, base, head := fanoutRepo(t)
		req := reviewReq(repo, repo, base, head)
		req.OutputDir = filepath.Join(t.TempDir(), "review")
		prep, err := PrepareReview(context.Background(), twoAgentConfig("http://unused"), req)
		require.NoError(t, err)

		cl, ok := readManifest(t, prep.Dir)["claim_ledger"].(map[string]any)
		require.True(t, ok, "a git-range review must record claim_ledger in its manifest")
		assert.Equal(t, true, cl["present"])
		assert.Greater(t, cl["claims"], float64(0), "the fixture's commits assert something")
		assert.Nil(t, cl["failed"], "a successful read must not be recorded as a failure")
		assert.Nil(t, cl["disabled"])
	})

	// The distinction the field exists for, at the artifact layer: a branch that
	// asserted nothing is recorded as absent WITHOUT failed, so a reader can tell it
	// apart from a run whose ledger read broke. initRepo's head commit subject is a
	// single word, which isClaimBearing drops, so the range genuinely yields none.
	t.Run("a claim-free branch is absent but not failed", func(t *testing.T) {
		repo, base, head := initRepo(t)
		req := reviewReq(repo, repo, base, head)
		req.OutputDir = filepath.Join(t.TempDir(), "review")
		prep, err := PrepareReview(context.Background(), twoAgentConfig("http://unused"), req)
		require.NoError(t, err)

		cl, ok := readManifest(t, prep.Dir)["claim_ledger"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, false, cl["present"])
		assert.Nil(t, cl["failed"],
			"the read succeeded and found no assertion — recording that as a failure would be the same conflation, inverted")
	})

	// max_claim_bytes: 0 is an operator choice and must be legible as one in the
	// artifact, never as an absent or failed ledger.
	t.Run("a disabled ledger is recorded as disabled", func(t *testing.T) {
		repo, base, head := fanoutRepo(t)
		cfg := twoAgentConfig("http://unused")
		zero := int64(0)
		cfg.Settings.MaxClaimBytes = &zero
		req := reviewReq(repo, repo, base, head)
		req.OutputDir = filepath.Join(t.TempDir(), "review")
		prep, err := PrepareReview(context.Background(), cfg, req)
		require.NoError(t, err)

		cl, ok := readManifest(t, prep.Dir)["claim_ledger"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, cl["disabled"])
		assert.Equal(t, false, cl["present"])
		assert.Nil(t, cl["failed"], "'you told us not to' must never read as 'we could not'")
	})
}
