package reconcile

import (
	"os"
	"regexp"
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

// tier4MetricDeclRe extracts a Tier-4 counter constant's declaration from the
// package's non-test sources. Deriving the set from source is the whole point:
// a hand-typed literal list drifts the moment a counter is added and the list
// is not updated, so the guard would silently stop guarding the very next
// counter — the duplicated-literal failure findings_format_taxonomy_test.go was
// written to avoid on category.go.
var tier4MetricDeclRe = regexp.MustCompile(`(?m)^const\s+(tier4\w+Metric)\s*=\s*"(atcr_tier4_[a-z0-9_]+)"`)

// tier4MetricConstants returns const-name -> metric-string for every Tier-4
// counter this package declares, read from the package sources themselves.
func tier4MetricConstants(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	out := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		require.NoError(t, err)
		for _, m := range tier4MetricDeclRe.FindAllStringSubmatch(string(data), -1) {
			out[m[1]] = m[2]
		}
	}
	require.NotEmpty(t, out,
		"the package must declare Tier-4 counter constants — an empty result means "+
			"the declaration shape drifted from the regex and the guard is pinned "+
			"against nothing, which is the silent-drift failure it exists to close")
	return out
}

// TestMetricsDocDocumentsEveryTier4SetLevelCounter is the drift guard between
// docs/metrics.md and the counter constants in this package.
//
// Every constant names a counter an operator is expected to SUM with its
// siblings to estimate suggestions lost to anchor fidelity. A counter missing
// from the catalog is worse than an undocumented one: the sum silently omits it,
// and nothing in the tool says so.
func TestMetricsDocDocumentsEveryTier4SetLevelCounter(t *testing.T) {
	for constName, metric := range tier4MetricConstants(t) {
		t.Run(constName, func(t *testing.T) {
			_, ok := metricsDocRow(t, metric)
			assert.True(t, ok,
				"%s (= %s) has no row of its own in %s — an operator summing the Tier 4 "+
					"set-level counters would omit it without being told",
				constName, metric, metricsDocPath)
		})
	}
}

// tier4DocRowRe matches a catalog table row leading with a backticked Tier-4
// counter name, the reverse direction of the guard above.
var tier4DocRowRe = regexp.MustCompile("^\\| `(atcr_tier4_[a-z0-9_]+)` \\|")

// TestMetricsDocEveryTier4RowMapsToALiveConstant is the reverse direction of
// the drift guard: a catalog row left behind by a DELETED or renamed constant
// is the same doc-vs-code drift from the other side, and the forward guard
// cannot see it because it only ever asks about live constants.
func TestMetricsDocEveryTier4RowMapsToALiveConstant(t *testing.T) {
	b, err := os.ReadFile(metricsDocPath)
	require.NoError(t, err, "precondition: the published metric catalog must be readable")
	live := map[string]bool{}
	for _, metric := range tier4MetricConstants(t) {
		live[metric] = true
	}
	documented := 0
	for _, line := range strings.Split(string(b), "\n") {
		m := tier4DocRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		documented++
		assert.True(t, live[m[1]],
			"%s rows %s but no live constant declares it — a deleted or renamed "+
				"counter left its catalog row behind",
			metricsDocPath, m[1])
	}
	require.Positive(t, documented,
		"precondition: the catalog documents at least one atcr_tier4_ counter — "+
			"zero matches means the row regex drifted from the table shape")
}

// TestMetricsDocProblemSetUnaccountedRowMatchesTheCode pins the claims the
// row got wrong, which are the ones an operator acts on.
//
// The row described the arm as "the PROBLEM anchor set resolved to exactly one
// file". validate.go reads `outcome == tier4Resolved`, and resolve produces that
// from locate(primary) AND from locate(secondary) under a matched primary — so
// the counter also fires when the PROBLEM set localized nothing and the FIX set
// produced the file. The row also omitted that it is a LOWER BOUND, and once
// that was added it overstated it: the suppression holds only when the PROBLEM
// set does not itself localize, because resolve returns at locate(primary)
// before the secondary branch is ever consulted.
//
// The assertions name the affirmative phrasings, not bare keywords: a bare
// Contains(row, "secondary") or Contains(row, "lower bound") passes on a row
// that merely mentions the words inside a negation — "not sourced from the
// secondary set" — which is the over-claim shape this guard was written to
// remove, not to re-admit through the back door.
func TestMetricsDocProblemSetUnaccountedRowMatchesTheCode(t *testing.T) {
	row, ok := metricsDocRow(t, tier4ProblemSetUnaccountedMetric)
	require.True(t, ok, "precondition: the row exists")

	assert.Contains(t, row, "locate(primary)",
		"the row must name the PRIMARY producer of tier4Resolved: a reader told "+
			"only about the PROBLEM set will misread every firing sourced from the "+
			"FIX set")
	assert.Contains(t, row, "locate(secondary)",
		"the row must name the SECONDARY producer under a matched primary, for "+
			"the same reason")

	assert.Regexp(t, regexp.MustCompile(`(?i)it is a \*?\*?lower bound`), row,
		"the row must disclose affirmatively that it undercounts — 'this is not "+
			"a lower bound' satisfies a bare keyword check and is exactly the "+
			"wrong claim")
	assert.Contains(t, row, "does not itself localize",
		"the lower-bound sentence must carry its scope: resolve returns at "+
			"locate(primary) when the PROBLEM set localizes, so an unconditional "+
			"flatness claim describes behavior resolve does not have")
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

	deny := strings.Index(row, "NOT")
	require.GreaterOrEqual(t, deny, 0,
		"the row must carry an explicit NOT-clause, as the PROBLEM counter's const "+
			"doc does — that is the discipline that keeps a new counter from "+
			"repeating the over-claim this epic was filed to fix")
	assert.Contains(t, row, tier4FixAnchorDroppedMetric,
		"the NOT-clause must name the counter this one is most likely to be "+
			"conflated with, since a dropped anchor is the veto's precondition")

	// AC5 asks for NOT-clause FIRST, not merely present. A denial appended after
	// the affirmative clause reads as a footnote, and the failure mode being
	// guarded against is a reader who stops at the first sentence.
	affirm := strings.Index(row, "What it counts")
	require.GreaterOrEqual(t, affirm, 0,
		"the row must state what it counts in a locatable clause, so the ordering "+
			"below is measurable rather than assumed")
	assert.Less(t, deny, affirm,
		"the denial must come BEFORE the affirmative clause — 'contains NOT' would "+
			"pass on a row whose denial was appended last, which is exactly the "+
			"shape AC5 rules out")

	// The affirmative content is checked against the CODE, not against keyword
	// presence: a row that kept the NOT-clause tokens while describing the arm
	// wrongly in every substantive claim ('counts every finding whose anchor
	// set was capped, incremented once per dropped anchor — divide by two
	// before summing') passed the keyword greps above.
	assert.Contains(t, row, "locate(secondary)",
		"the veto fires only when the secondary set produced a file under a "+
			"matched primary — the row must say so, not merely gesture at losses")
	assert.Contains(t, row, "contradicts",
		"the veto is the contradicts() veto on the narrowed-out anchors: the row "+
			"must name the mechanism an operator would grep for")
	assert.Contains(t, row, "tier4Inconclusive",
		"the row must name the outcome the veto falls through to, which is what "+
			"makes this counter the arm's only signal")
	assert.NotContains(t, row, "capped",
		"the veto row must not borrow the cap's vocabulary: a dropped-anchor veto "+
			"described in cap terms is the conflation the counter exists to separate")

	// The FORWARD implication the NOT-clauses deny only in reverse: every veto
	// increment is accompanied by a fix_anchor_dropped increment for the same
	// finding, in different units. A row that names the sibling counter only in
	// the denial still invites the double-counted, mismatched-unit sum.
	assert.Contains(t, row, "accompanied",
		"the row must disclose that every increment of this counter is ACCOMPANIED "+
			"by an atcr_tier4_fix_anchor_dropped_total increment for the same "+
			"finding, so the two are read alongside, never summed")
}

// TestMetricsDocFixSetUnaccountedRowDisclosesItsEmptinessGuard pins the exclusion
// the emptiness guard introduced.
//
// The guard narrowed what the counter counts: a FIX whose scan collected no
// anchor at all is now silent, because you cannot abandon a set that never
// existed. A row that still promised to count every set "abandoned whole because
// a call-scan fidelity loss left NO member behind" would be the same doc-vs-code
// over-claim this epic was filed to remove — reintroduced by the epic itself, on
// the row next to the one it came to fix.
func TestMetricsDocFixSetUnaccountedRowDisclosesItsEmptinessGuard(t *testing.T) {
	row, ok := metricsDocRow(t, tier4FixSetUnaccountedMetric)
	require.True(t, ok, "precondition: the row exists")

	assert.Contains(t, row, "NOT count",
		"the row must disclose that a FIX whose scan collected no anchor at all is "+
			"not counted here — otherwise an operator reads the counter as a "+
			"complete census of member-less fidelity losses, which it is not")
	assert.Contains(t, row, "len(fixScan.anchors)",
		"the row must NAME the guard validate.go carries on the arm — a row that "+
			"says 'not counted' without the mechanism passes on prose that disagrees "+
			"with the code in every other claim")
}

// TestMetricsDocFixSetUnaccountedRowDisclosesTheReconciliation pins the row's
// second-scope disclosure, mirroring the PROBLEM row's pin above.
//
// After reconcileSilenced, a silenced span whose destroyed name was cited
// cleanly in the same text AND survives the anchor cap does NOT abandon the
// set and does NOT increment this counter. A row that still describes every
// member-less loss as an abandonment overstates the counter — and it disagrees
// with the PROBLEM-side row, which already names the same reconciliation, so
// the two sibling rows would describe one mechanism by two different rules.
func TestMetricsDocFixSetUnaccountedRowDisclosesTheReconciliation(t *testing.T) {
	row, ok := metricsDocRow(t, tier4FixSetUnaccountedMetric)
	require.True(t, ok, "precondition: the row exists")

	assert.Contains(t, row, "cited cleanly",
		"the row must disclose the reconcileSilenced retraction — a loss whose "+
			"destroyed name was cited cleanly does not fire this counter — the way "+
			"the PROBLEM-side row already does")
	assert.Contains(t, row, "survives",
		"the retraction also requires the name to survive the anchor cap into "+
			"the set locate is given; a row naming only the clean citation omits "+
			"the conjunct the cap half of the predicate enforces")
}
