package reconcile

import (
	"strings"
	"testing"
)

// A DANGLING fence opener releases its tail as prose so the block walk does not
// swallow the rest of the document. The excerpt that reaches a consumer must still
// SAY a quote was opened — `docs/findings-format.md` promises "its opening ``` marker
// is kept", and cli/debt_resolve.go's isRecordedRationale scans for exactly that
// marker before it will accept a justification as a permanent dismissal's whole audit
// trail.
//
// The marker only survives on its own when the block walk-up happens to reach it. It
// does not when the quoted body begins with a list item or a heading — the shape
// reviewers actually write — because itemAt/headingAt read the RELEASED view, so that
// line is a genuine block start and the walk stops there. These pin the excerpt
// carrying the marker in both shapes.
func TestExtractSection_DanglingFenceOverAListKeepsAFenceMarker(t *testing.T) {
	lines := []string{
		"## Findings",
		"",
		"For reference, findings look like this:",
		"",
		"```markdown",
		"- internal/auth/token.go:120 HIGH the refresh token is never rotated",
		"  (illustrative only — this is the format, not a real finding)",
	}

	text, section := extractSection(lines, 5)

	if section != "Findings" {
		t.Errorf("section = %q, want %q", section, "Findings")
	}
	if text == "" {
		t.Fatal("excerpt was suppressed entirely; want the released tail behind a fence marker")
	}
	first := strings.SplitN(text, "\n", 2)[0]
	if !isFenceMarker(first) {
		t.Errorf("excerpt does not open with a fence marker, so a reader cannot tell the\n"+
			"tail is a released quote:\nfirst line = %q\nfull excerpt = %q", first, text)
	}
	if !strings.Contains(text, "the refresh token is never rotated") {
		t.Errorf("excerpt dropped the released tail it exists to carry: %q", text)
	}
}

// A heading inside the dangling tail stops the walk-up the same way a list item does.
func TestExtractSection_DanglingFenceOverAHeadingKeepsAFenceMarker(t *testing.T) {
	lines := []string{
		"## Findings",
		"",
		"```",
		"### Example finding",
		"internal/auth/token.go:120 HIGH the refresh token is never rotated",
	}

	text, _ := extractSection(lines, 4)

	if text == "" {
		t.Fatal("excerpt was suppressed entirely; want the released tail behind a fence marker")
	}
	first := strings.SplitN(text, "\n", 2)[0]
	if !isFenceMarker(first) {
		t.Errorf("excerpt does not open with a fence marker: first line = %q, full = %q", first, text)
	}
}

// The marker must not be prepended when the block already opens with one — the walk-up
// reaching the opener is the case that already worked, and doubling it would render two
// bare ``` lines where the document has one.
func TestExtractSection_DanglingFenceOpenerIsNotDoubled(t *testing.T) {
	lines := []string{
		"## Findings",
		"",
		"```",
		"the reviewer never closed this fence",
	}

	text, _ := extractSection(lines, 3)

	if got := strings.Count(text, "```"); got != 1 {
		t.Errorf("fence marker appears %d time(s), want exactly 1: %q", got, text)
	}
}

// Prose OUTSIDE any fence keeps its excerpt marker-free, so an ordinary reviewer
// narrative is still accepted as a recorded rationale.
func TestExtractSection_UnfencedProseGetsNoFenceMarker(t *testing.T) {
	lines := []string{
		"## Findings",
		"",
		"- internal/auth/token.go:120 HIGH the refresh token is never rotated",
		"  because refreshTokens() reuses the existing jti.",
	}

	text, _ := extractSection(lines, 2)

	if strings.Contains(text, "```") {
		t.Errorf("unfenced prose gained a fence marker it has no business carrying: %q", text)
	}
}

// The synthetic marker is for a DANGLING opener only. Inside a TERMINATED fence the
// region elides to a placeholder with its markers included, and prepending a ``` there
// would put back the very marker the elision just removed.
//
// Reachable because a blank line INSIDE a fence stops the walk-up: the block then
// begins on fenced content rather than above the opener.
func TestExtractSection_BalancedFenceStartGetsNoSyntheticMarker(t *testing.T) {
	lines := []string{
		"## Review",
		"",
		"```",
		"quoted",
		"",
		"more quoted",
		"```",
		"The handler at internal/x.go:42 skips verification.",
	}

	text, _ := extractSection(lines, 5)

	if strings.Contains(text, "```") {
		t.Errorf("a terminated fence's region gained a marker the elision removes: %q", text)
	}
	if !strings.HasPrefix(text, ElidedQuotePlaceholder) {
		t.Errorf("expected the fenced region to elide to a placeholder, got %q", text)
	}
}
