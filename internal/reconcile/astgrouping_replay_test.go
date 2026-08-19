package reconcile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildPostMortemReplay writes the reviewed source tree (a Python file whose
// load_contracts function spans the three cited lines) and the three reviewer
// sources from the external-repo post-mortem (2026-08-15, nolan-gmail-to-zoho-csv
// epic 7.4.1): greta, mira and dax each flagged the SAME defect in
// tests/test_contracts_jsonl.py at lines 172/175/178, worded so independently
// that pairwise token-set Jaccard fell below even the 0.4 gray band.
func buildPostMortemReplay(t *testing.T) (root, reviewDir string) {
	t.Helper()

	root = t.TempDir()
	const target = "tests/test_contracts_jsonl.py"
	var src strings.Builder
	for line := 1; line <= 185; line++ {
		switch {
		case line == 170:
			src.WriteString("def load_contracts(path):\n")
		case line == 172:
			src.WriteString("    raw = path.read_text()\n")
		case line == 175:
			src.WriteString("    return json.loads(raw)\n")
		case line > 170 && line <= 180:
			src.WriteString("    pass\n")
		default:
			fmt.Fprintf(&src, "# padding line %d\n", line)
		}
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tests"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, target), []byte(src.String()), 0o644))

	reviewDir = t.TempDir()
	sources := filepath.Join(reviewDir, "sources")
	writeFindings(t, sources, "pool/raw/agent/greta/findings.txt",
		"MEDIUM|"+target+":172|json.loads on the raw file text raises JSONDecodeError on any blank or malformed line|fix|correctness|10|ev|greta\n")
	writeFindings(t, sources, "pool/raw/agent/mira/findings.txt",
		"MEDIUM|"+target+":175|unbounded memory growth when the contracts payload is streamed without a size cap|fix|correctness|10|ev|mira\n")
	writeFindings(t, sources, "pool/raw/agent/dax/findings.txt",
		"MEDIUM|"+target+":178|missing timeout leaves the sync loop hanging forever on a stalled upstream|fix|correctness|10|ev|dax\n")
	return root, reviewDir
}

// The post-mortem's net effect was that three-reviewer agreement surfaced as zero
// findings: nothing merged (Jaccard ~0), each finding stayed a MEDIUM singleton,
// and the strict consensus filter (panel of 3 >= consensusMinReviewers) sidecarred
// all three. MergeThreshold is a fixed v1 constant and stays untouched — the merge
// must come from the AST-isomorphism grouper (epic 13.1, ON by default), which
// assigns all three lines the same enclosing-block GroupKey and matches them at
// distance 0 regardless of wording (dedupe.go's mergeable). Driving the real
// RunReconcile path means a regression that disengages AST grouping — a dropped
// .py parser, a wrong root, an env-opt-out leak — turns the HIGH-consensus finding
// back into sidecarred singletons and fails here.
func TestRunReconcile_ASTMergesIndependentlyWordedSameBlockFindings(t *testing.T) {
	root, reviewDir := buildPostMortemReplay(t)

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		Root:         root,
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
	})
	require.NoError(t, err)

	require.Len(t, res.Findings, 1,
		"the three same-block findings must merge into ONE corroborated finding, not sidecar as singletons")
	m := res.Findings[0]
	assert.Equal(t, ConfHigh, m.Confidence,
		"three distinct reviewers agreeing on one defect is HIGH consensus")
	assert.ElementsMatch(t, []string{"greta", "mira", "dax"}, m.Reviewers)
	assert.Empty(t, res.Ambiguous,
		"a shared AST key is authoritative duplicate evidence — no gray pair may be recorded")
}

// The differential arm: with AST grouping opted out, the SAME inputs reproduce the
// post-mortem exactly — no merge, three MEDIUM singletons, all consensus-filtered
// into the ambiguous sidecar. This pins that the merge above comes from the AST
// signal and not from text similarity, so the test cannot be satisfied by lowering
// MergeThreshold or loosening the gray band.
func TestRunReconcile_ProximityOnlyReplaysThePostMortem(t *testing.T) {
	t.Setenv(astGroupingDisabledEnv, "1")
	root, reviewDir := buildPostMortemReplay(t)

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		Root:         root,
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
	})
	require.NoError(t, err)

	assert.Empty(t, res.Findings,
		"without AST keys these independently-worded findings never merge — the post-mortem outcome")
	assert.Len(t, res.Ambiguous, 3,
		"all three singletons are sidecarred by the strict consensus filter")
}
