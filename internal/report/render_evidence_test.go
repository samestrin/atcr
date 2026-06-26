package report

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRender_ReproducedBadge(t *testing.T) {
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "calc.go", Line: 10, Problem: "off-by-one", Confidence: "VERIFIED",
		Reviewers:    []string{"greta"},
		Verification: &reclib.Verification{Verdict: "confirmed", Skeptic: "repro"},
		EvidenceExec: &reconcile.EvidenceExec{Command: "go test ./calc", ExitCode: 1, OutputExcerpt: "FAIL: want 4 got 5"},
	}}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "Reproduced", "a finding with evidence_exec must show a Reproduced badge")
	assert.Contains(t, out, "go test ./calc", "the reproduced command must be rendered")
	assert.Contains(t, out, "exit 1", "the exit code must be rendered")
	assert.Contains(t, out, "FAIL: want 4 got 5", "the output excerpt must be rendered")
}

func TestRender_NoBadgeWithoutEvidenceExec(t *testing.T) {
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: "HIGH", Reviewers: []string{"r"},
	}}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	assert.NotContains(t, b.String(), "Reproduced", "a finding without evidence_exec must not show the badge")
}

func TestRender_ReproducedBadge_EmptyOutputOmitsLine(t *testing.T) {
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: "VERIFIED", Reviewers: []string{"r"},
		EvidenceExec: &reconcile.EvidenceExec{Command: "true", ExitCode: 0, OutputExcerpt: ""},
	}}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "Reproduced")
	assert.NotContains(t, out, "Output:", "an empty output excerpt must not render an Output line")
}
