package reconcile

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// metricsDocPath is the published metric catalog an operator reads to interpret
// a counter. It lives outside any Go package, so this test reads it directly —
// the pattern justification_record_boundary_test.go established and CLAUDE.md
// requires for a doc-vs-code drift test (it belongs in internal/reconcile/,
// never in docs/ and never in the published top-level reconcile/ module, which
// must not assume this repo's file layout).
const metricsDocPath = "../../docs/metrics.md"

// metricsDocRow returns the catalog row for one counter name, and whether the
// catalog documents it at all.
//
// The row is matched on the leading `| ` + backticked name so a MENTION of the
// counter inside a sibling row's prose can never be mistaken for its own entry.
// That distinction is the whole point: three of the FIX rows reference each
// other by name, so a substring search would report every counter as documented
// the moment any one row named it.
func metricsDocRow(t *testing.T, name string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(metricsDocPath)
	require.NoError(t, err, "the published metric catalog must be readable from this package")
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "| `"+name+"` |") {
			return line, true
		}
	}
	return "", false
}

// TestMetricsDocDocumentsEveryTier4SetLevelCounter is the drift guard between
// docs/metrics.md and the counter constants in this package.
//
// Every constant below names a counter an operator is expected to SUM with its
// siblings to estimate suggestions lost to anchor fidelity. A counter missing
// from the catalog is worse than an undocumented one: the sum silently omits it,
// and nothing in the tool says so.
func TestMetricsDocDocumentsEveryTier4SetLevelCounter(t *testing.T) {
	for _, name := range []string{
		tier4FixSetCappedMetric,
		tier4FixSetUnaccountedMetric,
		tier4FixAnchorDroppedMetric,
		tier4FixSetAllDroppedMetric,
		tier4ProblemSetUnaccountedMetric,
		tier4FixSetContradictedMetric,
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := metricsDocRow(t, name)
			assert.True(t, ok,
				"%s has no row of its own in %s — an operator summing the Tier 4 "+
					"set-level counters would omit it without being told",
				name, metricsDocPath)
		})
	}
}

// TestMetricsDocProblemSetUnaccountedRowMatchesTheCode pins the two claims the
// row got wrong, which are the two an operator acts on.
//
// The row described the arm as "the PROBLEM anchor set resolved to exactly one
// file". validate.go reads `outcome == tier4Resolved`, and resolve produces that
// from locate(primary) AND from locate(secondary) under a matched primary — so
// the counter also fires when the PROBLEM set localized nothing and the FIX set
// produced the file. The row also omitted that it is a LOWER BOUND, which its
// sibling FIX rows disclose: when the FIX is unaccounted too, resolve yields
// tier4Inconclusive, the arm is never reached, and the counter stays flat
// although a suggestion was equally lost.
func TestMetricsDocProblemSetUnaccountedRowMatchesTheCode(t *testing.T) {
	row, ok := metricsDocRow(t, tier4ProblemSetUnaccountedMetric)
	require.True(t, ok, "precondition: the row exists")

	assert.Contains(t, row, "secondary",
		"the row must name BOTH tier4Resolved producers: a reader told only about "+
			"the PROBLEM set will misread every firing sourced from the FIX set")
	assert.Contains(t, row, "lower bound",
		"the row must disclose that it undercounts — when the FIX is unaccounted too "+
			"the arm is never reached and the counter stays flat on an equal loss")
}

// TestMetricsDocContradictedRowLeadsWithItsNotClause pins the wording discipline
// the PROBLEM counter's own const doc established, on the counter added beside it.
//
// A set-level counter that documents only what it counts is how the row this
// epic exists to correct came to over-claim. The veto counter's failure mode is
// specifically being read as implied by atcr_tier4_fix_anchor_dropped_total — a
// non-empty dropped set is the veto's PRECONDITION, never evidence it fired — so
// that denial has to appear in the row, not only in the code.
func TestMetricsDocContradictedRowLeadsWithItsNotClause(t *testing.T) {
	row, ok := metricsDocRow(t, tier4FixSetContradictedMetric)
	require.True(t, ok, "precondition: the row exists")

	assert.Contains(t, row, "NOT",
		"the row must carry an explicit NOT-clause, as the PROBLEM counter's const "+
			"doc does — that is the discipline that keeps a new counter from "+
			"repeating the over-claim this epic was filed to fix")
	assert.Contains(t, row, tier4FixAnchorDroppedMetric,
		"the NOT-clause must name the counter this one is most likely to be "+
			"conflated with, since a dropped anchor is the veto's precondition")
}
