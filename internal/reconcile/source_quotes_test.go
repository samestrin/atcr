package reconcile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 99 T4: the discount reason has one source (reclib.DiscountedConfidence),
// and the independence rule has one predicate (reclib.IsPanelWideQuotes) over
// per-reviewer quotes carried as provenance, used by reclib.Merge, the cluster
// merge and the fallback recompute alike.

// The check behind row lib.go:114: no non-test file infers the reason by
// comparing a confidence to ConfidenceFor, and none but the library's one
// source names PanelWideReason in code (the re-export in lib.go and comments
// aside). A second discount class would be mislabeled by either.
func TestDiscountReason_HasOneSource(t *testing.T) {
	library := filepath.Join("..", "..", "reconcile", "merge.go")
	compare := regexp.MustCompile(`[!=]= *(reclib\.)?ConfidenceFor\(`)
	for _, path := range []string{"lib.go", "merge.go", "fallback.go", library} {
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		if loc := compare.FindIndex(src); loc != nil {
			t.Errorf("%s infers the discount by comparing to ConfidenceFor: %q", path, src[loc[0]:loc[1]])
		}
		if path == library {
			continue
		}
		for i, line := range strings.Split(string(src), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if strings.Contains(code, "PanelWideReason") && code != "const PanelWideReason = reclib.PanelWideReason" {
				t.Errorf("%s:%d names PanelWideReason; take the reason from DiscountedConfidence: %s", path, i+1, strings.TrimSpace(code))
			}
		}
	}
}

// A merged record carries its quotes, in memory and on the findings.json wire,
// so the cluster merge (which reads findings.json back) tests independence on
// the quotes reclib.Merge tested. A one-finding record carries none: its
// evidence is its only quote.
func TestJSONFindings_CarryTheSourceQuotes(t *testing.T) {
	res := Reconcile(predicateSources("key.go", discountQuote, "bruce", "greta"), Options{})
	res.Findings = append(res.Findings, Reconcile(predicateSources("solo.go", discountQuote, "kai"), Options{}).Findings...)
	jf := res.JSONFindings()
	require.Len(t, jf, 2)
	want := []reclib.SourceQuote{
		{Reviewer: "bruce", File: "key.go", Category: "invariant", Evidence: discountQuote},
		{Reviewer: "greta", File: "key.go", Category: "invariant", Evidence: discountQuote},
	}
	assert.Equal(t, want, jf[0].SourceQuotes)
	assert.Nil(t, jf[1].SourceQuotes)

	dir := t.TempDir()
	require.NoError(t, Emit(dir, res))
	data, err := os.ReadFile(filepath.Join(dir, FindingsJSON))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(data), `"source_quotes": `), "only the merged record carries the key")
	var back []JSONFinding
	require.NoError(t, json.Unmarshal(data, &back))
	require.Len(t, back, 2)
	assert.Equal(t, want, back[0].SourceQuotes, "the quotes survive the findings.json round trip")
	assert.Nil(t, back[1].SourceQuotes)
}

// Row merge.go:63: the cluster merge tests the union of its members' own
// quotes, each with its reviewer's own category, rather than restating a
// member from its merged evidence and its record-level category. Here bruce
// filed invariant and greta correctness, quoting the same sibling branch; their
// record reads correctness (the alphabetical tie-break) and is undiscounted,
// and its evidence is the prefixed "[bruce] q / [greta] q". Unioned with two
// more invariant filings of the same quote, four reviewers quote one branch
// and three of them filed invariant: the class the discount exists for.
func TestMergeJSONFindings_TestsTheUnionOfMemberQuotes(t *testing.T) {
	src := predicateSources("key.go", "x == y", "bruce", "greta")
	src[1].Findings[0].Category = "correctness"
	pair := Reconcile(src, Options{}).JSONFindings()
	require.Len(t, pair, 1)
	require.Equal(t, "correctness", pair[0].Category)
	require.Empty(t, pair[0].ConfidenceDiscount)

	// Read the member back from findings.json, as the debate apply path does.
	b, err := json.Marshal(pair[0])
	require.NoError(t, err)
	var member JSONFinding
	require.NoError(t, json.Unmarshal(b, &member))

	m := MergeJSONFindings([]JSONFinding{member, predicateRecord("kai", "key.go", "x == y"), predicateRecord("mira", "key.go", "x == y")})
	assert.Equal(t, "invariant", m.Category)
	assert.Equal(t, ConfMedium, m.Confidence)
	assert.Equal(t, PanelWideReason, m.ConfidenceDiscount)
	assert.Equal(t, []reclib.SourceQuote{
		{Reviewer: "bruce", File: "key.go", Category: "invariant", Evidence: "x == y"},
		{Reviewer: "greta", File: "key.go", Category: "correctness", Evidence: "x == y"},
		{Reviewer: "kai", File: "key.go", Category: "invariant", Evidence: "x == y"},
		{Reviewer: "mira", File: "key.go", Category: "invariant", Evidence: "x == y"},
	}, m.SourceQuotes, "the union carries every member's quotes, so a later union reads them too")
}

// Row merge.go:63, the fallback leg: the recompute tests the record's quotes,
// not whether a reason happens to be recorded on it.
func TestRecomputeFallbackConfidence_TestsTheQuotes(t *testing.T) {
	collapsed := func(file string, quotes ...string) JSONFinding {
		f := predicateRecord("bruce", file, strings.Join(quotes, ClusterEvidenceSep))
		f.Reviewers = []string{"bruce", "greta", "kai"}
		f.FallbackReviewers = map[string]string{"greta": "net", "kai": "net"}
		f.Confidence = ConfHigh
		for i, r := range f.Reviewers {
			f.SourceQuotes = append(f.SourceQuotes, reclib.SourceQuote{Reviewer: r, File: file, Category: "invariant", Evidence: quotes[i]})
		}
		return f
	}
	panelWide := collapsed("a.go", discountQuote, discountQuote, discountQuote)
	independent := collapsed("b.go", discountQuote, discountQuote, "func (k Key) Hash() uint64 { return fnv(k.ID) }")
	independent.ConfidenceDiscount = PanelWideReason // a stale reason the quotes do not bear out
	findings := []JSONFinding{panelWide, independent}
	recomputeFallbackConfidence(findings)

	assert.Equal(t, ConfMedium, findings[0].Confidence, "two voices quoting one branch are the panel-wide class")
	assert.Equal(t, PanelWideReason, findings[0].ConfidenceDiscount)
	assert.Equal(t, ConfHigh, findings[1].Confidence, "two voices with distinct quotes corroborate")
	assert.Empty(t, findings[1].ConfidenceDiscount)
}
