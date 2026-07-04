package reconcile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeReview is a test helper: writes review.md at reviewDir/sources/<leaf>/review.md.
func writeReview(t *testing.T, reviewDir, leaf, content string) {
	t.Helper()
	dir := filepath.Join(reviewDir, "sources", leaf)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "review.md"), []byte(content), 0o644))
}

// TestJSONFinding_JustificationField verifies the additive justification field
// (Epic 18.2): present under the "justification" key when set, and omitted
// entirely when empty so findings.json stays byte-identical to pre-18.2 output.
func TestJSONFinding_JustificationField(t *testing.T) {
	b, err := json.Marshal(JSONFinding{Justification: "narrative context from review.md"})
	require.NoError(t, err)
	require.Contains(t, string(b), `"justification":"narrative context from review.md"`)

	b2, err := json.Marshal(JSONFinding{})
	require.NoError(t, err)
	require.NotContains(t, string(b2), "justification")
}

// TestStampJustifications_MatchesFileLine is the core happy path (Epic 18.2 T2):
// a finding whose file:line is anchored in a source review.md gets that section's
// narrative extracted into Justification.
func TestStampJustifications_MatchesFileLine(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", "# Host review\n\n"+
		"## Findings\n"+
		"1. **`internal/auth/token.go:42` — JWT signature not verified.** The handler\n"+
		"calls jwt.Parse without jwt.Verify, so a forged token is accepted.\n\n"+
		"## Areas examined — no issues found\n"+
		"- Cache eviction: fine.\n")

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Problem: "JWT sig", Reviewers: []string{"host"}}}
	stampJustifications(jf, reviewDir)

	require.NotEmpty(t, jf[0].Justification, "expected a matched narrative")
	require.Contains(t, jf[0].Justification, "jwt.Verify")
	require.Contains(t, jf[0].Justification, "forged token")
	require.NotContains(t, jf[0].Justification, "Cache eviction", "must not bleed into the next section")
}

// TestStampJustifications_NoMatch_LeavesEmpty: a finding whose file is not
// referenced in any review.md gets no Justification (byte-identical output).
func TestStampJustifications_NoMatch_LeavesEmpty(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", "# Host\n\n## Findings\n1. **`other/file.go:9` — bug.** narrative.\n")
	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42}}
	stampJustifications(jf, reviewDir)
	require.Empty(t, jf[0].Justification)
	require.Nil(t, jf[0].SourceReport)
}

// TestStampJustifications_BareFileMention_NoMatch: a file named without a line
// reference (e.g. in an "Areas examined — no issues" section) must NOT attach a
// misleading narrative (minAnchorTier guard).
func TestStampJustifications_BareFileMention_NoMatch(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", "# Host\n\n## Areas examined — no issues found\n- token.go: internal/auth/token.go looked fine, no problems.\n")
	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42}}
	stampJustifications(jf, reviewDir)
	require.Empty(t, jf[0].Justification, "bare file mention (no line) must not match")
}

// TestStampJustifications_RangeAnchor: a `file:A-B` range covering the finding's
// line matches.
func TestStampJustifications_RangeAnchor(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "claude", "# Review\n\n"+
		"### Criterion: role separation.\n"+
		"- **Evidence:** `internal/debate/cast.go:65-102` enforces three distinct models across the cast.\n")
	jf := []JSONFinding{{File: "internal/debate/cast.go", Line: 80, Reviewers: []string{"claude"}}}
	stampJustifications(jf, reviewDir)
	require.Contains(t, jf[0].Justification, "three distinct models")
}

// TestStampJustifications_MissingSourcesDir_NoOp: no sources/ dir → clean no-op.
func TestStampJustifications_MissingSourcesDir_NoOp(t *testing.T) {
	reviewDir := t.TempDir() // no sources/ subtree
	jf := []JSONFinding{{File: "a.go", Line: 1}}
	stampJustifications(jf, reviewDir)
	require.Empty(t, jf[0].Justification)
	// Empty reviewDir and empty jf are also no-ops.
	stampJustifications(jf, "")
	stampJustifications(nil, reviewDir)
	require.Empty(t, jf[0].Justification)
}

// TestStampJustifications_PrefersCoveringLine: given a same-file-other-line
// reference and a covering reference across two narratives, the covering one
// (tier 3) wins regardless of narrative order.
func TestStampJustifications_PrefersCoveringLine(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "aaa", "# A\n\n## Findings\n1. **`x/y.go:10` — unrelated other-line note about y.go.** other.\n")
	writeReview(t, reviewDir, "zzz", "# Z\n\n## Findings\n1. **`x/y.go:42` — the exact-line finding.** the covering narrative here.\n")
	jf := []JSONFinding{{File: "x/y.go", Line: 42}}
	stampJustifications(jf, reviewDir)
	require.Contains(t, jf[0].Justification, "covering narrative")
	require.NotContains(t, jf[0].Justification, "unrelated other-line")
}

// TestAnchorTier covers the scoring tiers directly.
func TestAnchorTier(t *testing.T) {
	require.Equal(t, 0, anchorTier("no file here", "x/y.go", 42))
	require.Equal(t, 1, anchorTier("mentions x/y.go with no line", "x/y.go", 42))
	require.Equal(t, 2, anchorTier("x/y.go:10 other line", "x/y.go", 42))
	require.Equal(t, 3, anchorTier("`x/y.go:42` exact", "x/y.go", 42))
	require.Equal(t, 3, anchorTier("`x/y.go:40-50` range", "x/y.go", 42))
	require.Equal(t, 2, anchorTier("`x/y.go:43` off by one", "x/y.go", 42))
}

// TestStampJustifications_ReviewerTiebreak: two narratives reference the finding's
// exact line at equal tier; the one whose leaf dir is a finding reviewer wins.
func TestStampJustifications_ReviewerTiebreak(t *testing.T) {
	reviewDir := t.TempDir()
	// "aaa" sorts first but is not a reviewer; "greta" is the reviewer.
	writeReview(t, reviewDir, "aaa", "# A\n\n## Findings\n1. **`x/y.go:42` — note.** narrative from the non-reviewer aaa.\n")
	writeReview(t, reviewDir, "greta", "# G\n\n## Findings\n1. **`x/y.go:42` — note.** narrative from reviewer greta.\n")
	jf := []JSONFinding{{File: "x/y.go", Line: 42, Reviewers: []string{"greta"}}}
	stampJustifications(jf, reviewDir)
	require.Contains(t, jf[0].Justification, "reviewer greta")
	require.NotContains(t, jf[0].Justification, "non-reviewer aaa")
}

// TestStampJustifications_Truncates: an over-long narrative section is truncated
// to justificationMaxRunes with an ellipsis.
func TestStampJustifications_Truncates(t *testing.T) {
	reviewDir := t.TempDir()
	long := strings.Repeat("verylongword ", 300) // ~3900 chars, one paragraph
	writeReview(t, reviewDir, "host", "# H\n\n## Findings\n1. **`x/y.go:42`** "+long+"\n")
	jf := []JSONFinding{{File: "x/y.go", Line: 42}}
	stampJustifications(jf, reviewDir)
	require.NotEmpty(t, jf[0].Justification)
	require.LessOrEqual(t, len([]rune(jf[0].Justification)), justificationMaxRunes+1) // +1 for the ellipsis
	require.True(t, strings.HasSuffix(jf[0].Justification, "…"))
}
