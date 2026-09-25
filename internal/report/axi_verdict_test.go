package report

import (
	"testing"

	goaxi "github.com/samestrin/go-axi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	toon "github.com/toon-format/toon-go"

	"github.com/samestrin/atcr/internal/reconcile"
)

// plainAXI projects an AXI document onto plain maps and slices. goaxi.Check
// measures efficiency against json.Marshal of the value, and toon.Object has no
// JSON form of its own — it marshals as {"Fields":[{"Key":…,"Value":…}]}, which
// inflates the JSON side and makes Efficient trivially true. The plain projection
// carries the same keys and values, so its JSON size is the honest baseline.
func plainAXI(v any) any {
	switch x := v.(type) {
	case toon.Object:
		m := make(map[string]any, len(x.Fields))
		for _, f := range x.Fields {
			m[f.Key] = plainAXI(f.Value)
		}
		return m
	case []toon.Object:
		out := make([]any, len(x))
		for i, o := range x {
			out[i] = plainAXI(o)
		}
		return out
	case axiFindingsPayload:
		return map[string]any{"findings": plainAXI(x.Findings)}
	case axiPaginatedPayload:
		return map[string]any{"findings": plainAXI(x.Findings), "total": x.Total, "truncated": x.Truncated}
	default:
		return v
	}
}

// assertAXIVerdict runs goaxi.Check over an AXI document. Every payload must be
// OK (lossless and non-empty output for non-empty input). A payload with rows
// must also be tabular and no larger than its JSON form; an empty table cannot
// be tabular (go-axi tiers findings[0]: as nested), so it asserts OK only.
func assertAXIVerdict(t *testing.T, name string, doc any, empty bool) {
	t.Helper()
	v := goaxi.Check(doc)
	require.Truef(t, v.OK, "%s: goaxi.Check must pass: %s", name, v.Reason)
	if empty {
		return
	}
	assert.Equalf(t, goaxi.TierTabular, v.Tier, "%s: payload must encode as a tabular array", name)
	honest := goaxi.Check(plainAXI(doc))
	require.Truef(t, honest.OK, "%s: plain projection must pass: %s", name, honest.Reason)
	require.Truef(t, honest.SizeCompared, "%s: efficiency must be measured", name)
	assert.Truef(t, honest.Efficient, "%s: TOON must not be larger than JSON: %s", name, honest.Reason)
	assert.Equalf(t, goaxi.TierTabular, honest.Tier, "%s: plain projection must stay tabular", name)
}

// TestAXIPayloads_GoaxiCheck is AC5: every AXI payload the standard path emits
// passes goaxi.Check — non-empty and lossless — and every non-empty one is a
// token-efficient tabular array.
func TestAXIPayloads_GoaxiCheck(t *testing.T) {
	many := make([]reconcile.JSONFinding, 1200)
	for i := range many {
		many[i] = reconcile.JSONFinding{Severity: "LOW", File: "a.go", Line: i, Problem: "p", Confidence: "LOW"}
	}
	summary, err := reviewSummaryAXIDoc(ReviewSummaryAXI{ID: "2026-06-10_x", Dir: "/tmp/r", AgentsSucceeded: 3, AgentsTotal: 4, FindingsTotal: 7, FindingsHigh: 2})
	require.NoError(t, err)
	home, err := homeViewAXIDoc(HomeViewAXI{ExecPath: "~/go/bin/atcr", Description: "Agent Team Code Review — a review panel, not a reviewer", ReviewStatus: "none"})
	require.NoError(t, err)

	cases := []struct {
		name  string
		doc   any
		empty bool
	}{
		{"findings_plain", axiFindingsDoc(sample()), false},
		{"findings_edge", axiFindingsDoc(legacyPipeEdgeFindings()), false},
		{"findings_empty", axiFindingsDoc(nil), true},
		{"paginated_under", axiPaginatedDoc(sample(), AXIMaxLinesDefault), false},
		{"paginated_truncated", axiPaginatedDoc(many, AXIMaxLinesDefault), false},
		{"paginated_empty", axiPaginatedDoc(nil, AXIMaxLinesDefault), true},
		{"review_summary", summary, false},
		{"home", home, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { assertAXIVerdict(t, c.name, c.doc, c.empty) })
	}
}
