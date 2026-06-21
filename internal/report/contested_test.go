package report

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/reconcile"
)

func TestWriteContestedSection_EmptyIsByteIdentical(t *testing.T) {
	findings := []reconcile.JSONFinding{{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "HIGH"}}

	var plain, withEmpty bytes.Buffer
	require.NoError(t, RenderMarkdownWithDisagreements(&plain, findings, reconcile.DisagreementsFile{}))
	require.NoError(t, RenderMarkdownWithContested(&withEmpty, findings, reconcile.DisagreementsFile{}, ContestedReport{}))

	assert.Equal(t, plain.String(), withEmpty.String(), "empty contested report must not change the output")
	assert.NotContains(t, withEmpty.String(), "Contested findings")
}

func TestWriteContestedSection_RendersRulings(t *testing.T) {
	cr := ContestedReport{
		Items: []Contested{
			{File: "a.go", Line: 10, Outcome: "uphold", OriginalSeverity: "HIGH", Judge: "carol", Reasoning: "evidence holds", ChallengeSurvived: true},
			{File: "b.go", Line: 20, Outcome: "split", OriginalSeverity: "HIGH", SettledSeverity: "MEDIUM", Judge: "carol", Reasoning: "real but minor"},
			{File: "c.go", Line: 30, Outcome: "overturn", OriginalSeverity: "LOW", Judge: "carol", Reasoning: "false positive"},
			{File: "d.go", Line: 40, Outcome: "unresolved", OriginalSeverity: "HIGH", Reason: "insufficient_distinct_models"},
		},
		Overflow: 2,
	}
	var b bytes.Buffer
	writeContestedSection(&b, cr)
	out := b.String()

	assert.Contains(t, out, "## Contested findings")
	assert.Contains(t, out, "Debated 4 finding(s)")
	assert.Contains(t, out, "uphold")
	assert.Contains(t, out, "survived hostile challenge")
	assert.Contains(t, out, "(HIGH → MEDIUM)") // split severity transition
	assert.Contains(t, out, "Overturned")
	assert.Contains(t, out, "insufficient_distinct_models")
	assert.Contains(t, out, "Rationale: evidence holds")
	assert.Contains(t, out, "exceeded the debate cap")
}

func TestWriteContestedSection_SingleModelDisclosed(t *testing.T) {
	cr := ContestedReport{Items: []Contested{
		{File: "a.go", Line: 1, Outcome: "uphold", OriginalSeverity: "HIGH", Judge: "alice", SingleModel: true},
	}}
	var b bytes.Buffer
	writeContestedSection(&b, cr)
	assert.Contains(t, b.String(), "single-model fallback")
}

func TestWriteContestedSection_OverflowOnlyStillRenders(t *testing.T) {
	var b bytes.Buffer
	writeContestedSection(&b, ContestedReport{Overflow: 3})
	out := b.String()
	assert.Contains(t, out, "## Contested findings")
	assert.Contains(t, out, "3 disputed item(s) exceeded")
}
