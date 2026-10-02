package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/reconcile"
)

// TestWriteContestedSection_ChallengeSurvivedBadge: the ChallengeSurvived field was
// dead plumbing (loadContested mapped it, the renderer never read it). It must render
// a marker on uphold AND split survivors — distinguishing a split survivor from a
// bare split — and never on a refuted (overturn) finding.
func TestWriteContestedSection_ChallengeSurvivedBadge(t *testing.T) {
	cr := ContestedReport{Items: []Contested{
		{File: "a.go", Line: 10, Outcome: "uphold", OriginalSeverity: "HIGH", Judge: "carol", ChallengeSurvived: true},
		{File: "b.go", Line: 20, Outcome: "split", OriginalSeverity: "HIGH", SettledSeverity: "MEDIUM", Judge: "carol", ChallengeSurvived: true},
		{File: "c.go", Line: 30, Outcome: "overturn", OriginalSeverity: "LOW", Judge: "carol", ChallengeSurvived: false},
	}}
	var b bytes.Buffer
	writeContestedSection(&b, cr)
	out := b.String()

	assert.Equal(t, 2, strings.Count(out, "challenge-survived"), "uphold and split survivors are both badged from the structured field")
}

// TestWriteContestedSection_UnresolvedReasonTruncated: the unresolved Reason is
// untrusted free text from debate.json and must obey the same truncation contract
// (escTrunc) as Reasoning, not render unbounded.
func TestWriteContestedSection_UnresolvedReasonTruncated(t *testing.T) {
	longReason := strings.Repeat("z", maxTextLen+50)
	cr := ContestedReport{Items: []Contested{
		{File: "a.go", Line: 1, Outcome: "unresolved", Reason: longReason},
	}}
	var b bytes.Buffer
	writeContestedSection(&b, cr)
	out := b.String()
	assert.NotContains(t, out, longReason, "unbounded reason must be truncated")
	assert.Contains(t, out, "...", "truncation ellipsis present")
}

// TestSeverityTransition_OverturnMarkedExcluded: an overturned (refuted) finding is
// excluded from the gate, so its severity tag must not render bare — identical to a
// live, gating severity — but be annotated excluded.
func TestSeverityTransition_OverturnMarkedExcluded(t *testing.T) {
	assert.Equal(t, " (HIGH, excluded)", severityTransition(Contested{Outcome: "overturn", OriginalSeverity: "HIGH"}),
		"an overturned finding's severity must be marked excluded, not rendered as a live gating tag")
	assert.Equal(t, " (HIGH)", severityTransition(Contested{Outcome: "uphold", OriginalSeverity: "HIGH"}),
		"uphold still renders the bare live severity")
	assert.Equal(t, " (HIGH → MEDIUM)", severityTransition(Contested{Outcome: "split", OriginalSeverity: "HIGH", SettledSeverity: "MEDIUM"}),
		"split still renders the severity transition")
}

// TestWriteContestedSection_SingleModelSectionAndUnresolvedDisclosure: the same-model
// persona fallback must be disclosed at the section level (aggregate count) and on an
// unresolved insufficient_distinct_models item that has no Judge line to carry the
// per-item note.
func TestWriteContestedSection_SingleModelSectionAndUnresolvedDisclosure(t *testing.T) {
	cr := ContestedReport{Items: []Contested{
		{File: "a.go", Line: 1, Outcome: "uphold", OriginalSeverity: "HIGH", Judge: "alice", SingleModel: true},
		{File: "b.go", Line: 2, Outcome: "split", OriginalSeverity: "HIGH", SettledSeverity: "LOW", Judge: "alice", SingleModel: true},
		{File: "c.go", Line: 3, Outcome: "unresolved", Reason: "insufficient_distinct_models"},
	}}
	var b bytes.Buffer
	writeContestedSection(&b, cr)
	out := b.String()
	assert.Contains(t, out, "2 ruling(s) used the same-model persona fallback", "section-level aggregate disclosure of fallback rulings")
	assert.Contains(t, out, "distinct models were unavailable", "unresolved insufficient-models item discloses weakened independence")
}

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
	assert.Contains(t, out, "2 disputed item(s) were not debated: the debate cap was reached")
	// The two causes have OPPOSITE remedies and opposite permanence, so they are
	// rendered separately rather than collapsed into one integer: raising
	// debate.max_items recovers a cap overflow and recovers a withheld item at NO
	// cap value (TD internal/report/contested.go:98).
	assert.NotContains(t, out, "withheld",
		"a cap-only report must not mention withholding")
}

func TestWriteContestedSection_RendersTheWithheldCauseSeparately(t *testing.T) {
	var b bytes.Buffer
	writeContestedSection(&b, ContestedReport{Overflow: 2, Withheld: 5})
	out := b.String()

	assert.Contains(t, out, "2 disputed item(s) were not debated: the debate cap was reached")
	assert.Contains(t, out, "5 disputed item(s) were withheld")
	assert.Contains(t, out, "Raising the cap will NOT recover them",
		"the withheld half needs its own remedy: an operator who reads '7 not debated' and raises "+
			"max_items concludes all seven will now be debated, which is false for five of them")
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
	assert.Contains(t, out, "3 disputed item(s) were not debated")
}

// TestWriteContestedSection_WithheldItemsAreListedNotJustCounted: on the run that
// withholds an item it produces no ItemResult, so it has no Contested ruling — its
// diagnosis vanishes and only an integer grows. A withheld item must therefore be
// LISTED with its location, reason, and attempt countdown, so the transition from
// fully-described to withheld is visible rather than silent (TD cli/report.go:284).
func TestWriteContestedSection_WithheldItemsAreListedNotJustCounted(t *testing.T) {
	cr := ContestedReport{
		Withheld:                  1,
		UnresolvedAttemptsCeiling: 3,
		WithheldItems: []Withheld{{
			File: "a.go", Line: 42, Severity: "HIGH", Problem: "leaks the token",
			Reason: "unresolved_attempts_exhausted", UnresolvedAttempts: 3,
		}},
	}
	var b bytes.Buffer
	writeContestedSection(&b, cr)
	out := b.String()

	assert.Contains(t, out, "Withheld items", "withheld items get their own listed section")
	assert.Contains(t, out, "a.go:42", "the withheld item's location is listed")
	assert.Contains(t, out, "leaks the token", "the withheld item's problem survives")
	assert.Contains(t, out, "attempt 3 of 3", "the countdown is shown, not just the fact of withholding")
}

// TestWriteContestedSection_RendersTheAttemptCountdownOnARuling: a debated item
// carried forward across runs records UnresolvedAttempts; surfacing it lets an
// operator see "attempt 2 of 3" before the run that finally withholds it, rather
// than only learning of the ceiling after the item has already vanished.
func TestWriteContestedSection_RendersTheAttemptCountdownOnARuling(t *testing.T) {
	cr := ContestedReport{
		UnresolvedAttemptsCeiling: 3,
		Items: []Contested{{
			File: "b.go", Line: 7, Outcome: "unresolved", OriginalSeverity: "HIGH",
			Reason: "insufficient_distinct_models", UnresolvedAttempts: 2,
		}},
	}
	var b bytes.Buffer
	writeContestedSection(&b, cr)
	assert.Contains(t, b.String(), "attempt 2 of 3",
		"an unresolved ruling shows its attempt countdown against the ceiling")
}
