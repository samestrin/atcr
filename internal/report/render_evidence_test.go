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

func TestRender_NoBadgeWhenNotConfirmed(t *testing.T) {
	// A finding whose repro run did NOT confirm — an unverifiable verdict (e.g.
	// both runs exited 0, a timeout, or disagreeing exit codes) — must not show
	// the green Reproduced badge even though an EvidenceExec block is attached.
	// The badge asserts a demonstrated failure; rendering it here would lie to the
	// operator that the finding reproduced.
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "calc.go", Line: 10, Problem: "off-by-one", Confidence: "HIGH",
		Reviewers:    []string{"greta"},
		Verification: &reclib.Verification{Verdict: "unverifiable", Skeptic: "repro"},
		EvidenceExec: &reconcile.EvidenceExec{Command: "go test ./calc", ExitCode: 0, OutputExcerpt: "ok"},
	}}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	assert.NotContains(t, b.String(), "Reproduced",
		"an unverifiable repro (exit 0) must not render the Reproduced badge")
}

func TestRender_NoBadgeWhenConfirmedButExitZero(t *testing.T) {
	// Even a confirmed verdict must not show the badge when the reproduced command
	// exited 0: a green badge reading "Reproduced: cmd (exit 0)" is a contradiction.
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "calc.go", Line: 10, Problem: "off-by-one", Confidence: "VERIFIED",
		Reviewers:    []string{"greta"},
		Verification: &reclib.Verification{Verdict: "confirmed", Skeptic: "repro"},
		EvidenceExec: &reconcile.EvidenceExec{Command: "true", ExitCode: 0, OutputExcerpt: ""},
	}}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	assert.NotContains(t, b.String(), "Reproduced",
		"a confirmed verdict with exit 0 must not render the Reproduced badge")
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
		Verification: &reclib.Verification{Verdict: "confirmed", Skeptic: "repro"},
		EvidenceExec: &reconcile.EvidenceExec{Command: "false", ExitCode: 1, OutputExcerpt: ""},
	}}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "Reproduced")
	assert.NotContains(t, out, "Output:", "an empty output excerpt must not render an Output line")
}

func TestRender_ReproducedBadge_CommandNotEntityMangled(t *testing.T) {
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: "VERIFIED", Reviewers: []string{"r"},
		Verification: &reclib.Verification{Verdict: "confirmed", Skeptic: "repro"},
		EvidenceExec: &reconcile.EvidenceExec{Command: `echo "<foo>" && bar`, ExitCode: 1, OutputExcerpt: "FAIL"},
	}}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "`echo \"<foo>\" && bar`", "command inside code span must stay raw")
	assert.NotContains(t, out, "&lt;", "HTML entity encoding must not leak into code span")
	assert.NotContains(t, out, "&quot;")
}
