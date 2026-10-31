package reconcile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/stream"
	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.17 T3: the panel-wide-instruction discount reaches findings.json as
// confidence_discount, on the derived path, through the cluster merge and
// through the fallback recompute, and an undiscounted finding carries no key.

const discountQuote = "case b: return a.ID == b.ID && a.Kind == b.Kind"

var fiveReviewers = []string{"bruce", "greta", "kai", "mira", "otto"}

func predicateSources(file, evidence string, reviewers ...string) []Source {
	var out []Source
	for _, r := range reviewers {
		out = append(out, Source{Name: r, Findings: []stream.Finding{{
			Severity: "MEDIUM", File: file, Line: 42,
			Problem: "equal() compares Name in the edited branch but the sibling branch omits it",
			Fix:     "compare Name in both branches", Category: "invariant", EstMinutes: 15,
			Evidence: evidence, Reviewer: r,
		}}})
	}
	return out
}

func predicateRecord(reviewer, file, evidence string) JSONFinding {
	return JSONFinding{
		Severity: "MEDIUM", File: file, Line: 42, Problem: "equal() omits Name in the sibling branch",
		Category: "invariant", Evidence: evidence, Reviewers: []string{reviewer}, Confidence: ConfMedium,
	}
}

func TestJSONFindings_CarryTheConfidenceDiscount(t *testing.T) {
	res := Reconcile(predicateSources("key.go", discountQuote, fiveReviewers...), Options{})
	jf := res.JSONFindings()
	require.Len(t, jf, 1)
	assert.Equal(t, ConfMedium, jf[0].Confidence)
	assert.Equal(t, PanelWideReason, jf[0].ConfidenceDiscount)

	dir := t.TempDir()
	require.NoError(t, Emit(dir, res))
	data, err := os.ReadFile(filepath.Join(dir, FindingsJSON))
	require.NoError(t, err)
	var back []JSONFinding
	require.NoError(t, json.Unmarshal(data, &back))
	require.Len(t, back, 1)
	assert.Equal(t, PanelWideReason, back[0].ConfidenceDiscount)
	assert.Contains(t, string(data), `"confidence_discount": `,
		"the reason rides findings.json under the confidence_discount key")
}

// An undiscounted finding serializes with no confidence_discount key, so its
// findings.json is byte-identical to the output before the field existed.
func TestJSONFinding_ConfidenceDiscountOmittedWhenEmpty(t *testing.T) {
	res := Reconcile(predicateSources("key.go", discountQuote, "bruce"), Options{})
	res.Findings = append(res.Findings, Reconcile(append(
		predicateSources("a.go", discountQuote, "bruce"),
		predicateSources("b.go", discountQuote, "kai")...), Options{}).Findings...)
	dir := t.TempDir()
	require.NoError(t, Emit(dir, res))
	data, err := os.ReadFile(filepath.Join(dir, FindingsJSON))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "confidence_discount")

	f := JSONFinding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Reviewers: []string{"greta"}, Confidence: "MEDIUM"}
	b, err := json.Marshal(f)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "confidence_discount")
}

func TestMergeJSONFindings_FiveCorrelatedPredicateFindingsAreDiscounted(t *testing.T) {
	var group []JSONFinding
	for _, r := range fiveReviewers {
		group = append(group, predicateRecord(r, "key.go", discountQuote))
	}
	m := MergeJSONFindings(group)
	assert.Equal(t, fiveReviewers, m.Reviewers)
	assert.Equal(t, ConfMedium, m.Confidence, "reviewer count alone does not make the class HIGH")
	assert.Equal(t, PanelWideReason, m.ConfidenceDiscount)
}

func TestMergeJSONFindings_IndependentInvariantAgreementStaysHigh(t *testing.T) {
	cases := map[string][]JSONFinding{
		"distinct files": {
			predicateRecord("bruce", "key.go", discountQuote),
			predicateRecord("kai", "hash.go", discountQuote),
		},
		"distinct evidence": {
			predicateRecord("bruce", "key.go", discountQuote),
			predicateRecord("kai", "key.go", "func (k Key) Hash() uint64 { return fnv(k.ID) }"),
		},
	}
	for name, g := range cases {
		m := MergeJSONFindings(g)
		assert.Equal(t, ConfHigh, m.Confidence, name)
		assert.Empty(t, m.ConfidenceDiscount, name)
	}
}

// A member reconcile already discounted is read through its per-reviewer quotes
// (SourceQuotes, Epic 99 T4; before that, a first quote cut from its evidence),
// so unioning it with another reviewer's matching quote stays discounted, and a
// quote that itself holds " / " is not cut at the separator. The short quote is
// the case that needs the per-reviewer reading: "[bruce] x == y / [greta] x == y"
// shares only half its words with "x == y", below the near-identical threshold,
// so read whole it would make the union look independent and HIGH.
func TestMergeJSONFindings_DiscountedMemberIsReadThroughItsFirstQuote(t *testing.T) {
	for name, quote := range map[string]string{
		"short quote":                 "x == y",
		"quote holding the separator": "return a.Size / b.Size == 1 && a.ID == b.ID",
	} {
		pair := Reconcile(predicateSources("key.go", quote, "bruce", "greta"), Options{}).JSONFindings()
		require.Len(t, pair, 1, name)
		require.Equal(t, PanelWideReason, pair[0].ConfidenceDiscount, name)

		m := MergeJSONFindings([]JSONFinding{pair[0], predicateRecord("kai", "key.go", quote)})
		assert.Equal(t, ConfMedium, m.Confidence, name)
		assert.Equal(t, PanelWideReason, m.ConfidenceDiscount, name)

		for _, q := range memberQuotes(pair[0]) {
			assert.Equal(t, quote, q.Evidence, "%s: %s's quote is whole", name, q.Reviewer)
		}
		for _, q := range memberQuotes(predicateRecord("kai", "key.go", quote)) {
			assert.Equal(t, quote, q.Evidence, "%s: an unprefixed quote passes through whole", name)
		}
	}
}

// A member with no reviewers (a hand-built or old findings.json record) still
// stands in the independence test as one entry: it adds no voice, but its file
// and evidence are compared, so a reviewer-less member quoting other code in
// another file keeps the union out of the class (TD merge.go:113).
func TestMergeJSONFindings_ReviewerlessMemberCountsInTheIndependenceTest(t *testing.T) {
	reviewerless := predicateRecord("", "hash.go", "func (k Key) Hash() uint64 { return fnv(k.ID) }")
	reviewerless.Reviewers = nil

	members := memberQuotes(reviewerless)
	require.Len(t, members, 1)
	assert.Equal(t, reclib.SourceQuote{File: "hash.go", Category: "invariant", Evidence: reviewerless.Evidence}, members[0])

	m := MergeJSONFindings([]JSONFinding{
		predicateRecord("bruce", "key.go", discountQuote),
		predicateRecord("kai", "key.go", discountQuote),
		reviewerless,
	})
	assert.Equal(t, []string{"bruce", "kai"}, m.Reviewers)
	assert.Equal(t, ConfHigh, m.Confidence)
	assert.Empty(t, m.ConfidenceDiscount)

	same := predicateRecord("", "key.go", discountQuote)
	same.Reviewers = nil
	m = MergeJSONFindings([]JSONFinding{
		predicateRecord("bruce", "key.go", discountQuote),
		predicateRecord("kai", "key.go", discountQuote),
		same,
	})
	assert.Equal(t, ConfMedium, m.Confidence, "a reviewer-less member quoting the same code keeps the discount")
	assert.Equal(t, PanelWideReason, m.ConfidenceDiscount)
}

func TestRecomputeFallbackConfidence_KeepsTheDiscountWhileTwoVoicesRemain(t *testing.T) {
	discounted := func(file string, fb map[string]string) JSONFinding {
		f := predicateRecord("bruce", file, discountQuote)
		f.Reviewers = []string{"bruce", "greta", "kai"}
		f.FallbackReviewers = fb
		f.ConfidenceDiscount = PanelWideReason
		return f
	}
	twoVoices := discounted("a.go", map[string]string{"greta": "net", "kai": "net"})
	oneVoice := discounted("b.go", map[string]string{"bruce": "net", "greta": "net", "kai": "net"})
	findings := []JSONFinding{twoVoices, oneVoice}
	recomputeFallbackConfidence(findings)

	assert.Equal(t, ConfMedium, findings[0].Confidence, "a collapse to two voices does not re-earn HIGH")
	assert.Equal(t, PanelWideReason, findings[0].ConfidenceDiscount)
	assert.Equal(t, ConfMedium, findings[1].Confidence)
	assert.Empty(t, findings[1].ConfidenceDiscount, "one voice is MEDIUM on count alone; the discount no longer applies")
}

// Guard on the reason's wire shape: it is prose, so it must survive a JSON
// round-trip unchanged for the TD writer that reads it back.
func TestConfidenceDiscount_RoundTrips(t *testing.T) {
	in := JSONFinding{Severity: "MEDIUM", File: "a.go", Line: 1, Reviewers: []string{"a", "b"}, Confidence: ConfMedium, ConfidenceDiscount: PanelWideReason}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var out JSONFinding
	require.NoError(t, json.NewDecoder(strings.NewReader(string(b))).Decode(&out))
	assert.Equal(t, PanelWideReason, out.ConfidenceDiscount)
}
