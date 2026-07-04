package reconcile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
	require.Equal(t, 0, anchorTier("mentions x/y.go with no line", "x/y.go", 42)) // below minAnchorTier
	require.Equal(t, 0, anchorTier("x/y.go:10 far other line", "x/y.go", 42))     // far → below tier 2
	require.Equal(t, 2, anchorTier("x/y.go:44 near line", "x/y.go", 42))          // within ±3 → tier 2
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

// TestStampJustifications_SetsSourceReport verifies the back-reference (Epic 18.2
// T3): path (review-dir-relative), 1-based anchor line, and nearest heading.
func TestStampJustifications_SetsSourceReport(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", "# Host review\n\n"+ // line 1
		"## Findings\n"+ // line 3
		"1. **`internal/auth/token.go:42` — JWT not verified.** narrative body.\n") // line 4
	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"host"}}}
	stampJustifications(jf, reviewDir)

	require.NotNil(t, jf[0].SourceReport, "expected a back-reference")
	require.Equal(t, "sources/host/review.md", jf[0].SourceReport.Path)
	require.Equal(t, 4, jf[0].SourceReport.Line, "1-based line of the anchor")
	require.Equal(t, "Findings", jf[0].SourceReport.Section)

	// And it round-trips through findings.json under the source_report key.
	b, err := json.Marshal(jf[0])
	require.NoError(t, err)
	require.Contains(t, string(b), `"source_report"`)
	require.Contains(t, string(b), `"path":"sources/host/review.md"`)
}

// TestStampJustifications_SuffixPathNoFalseMatch: a finding for "y.go" must NOT
// match a line that only references a longer path ending in y.go (Epic 18.2
// cumulative-review fix).
func TestStampJustifications_SuffixPathNoFalseMatch(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", "# H\n\n## Findings\n1. **`internal/x/y.go:42` — a different file entirely.** wrong narrative.\n")
	jf := []JSONFinding{{File: "y.go", Line: 42}}
	stampJustifications(jf, reviewDir)
	require.Empty(t, jf[0].Justification, "y.go must not match internal/x/y.go:42")

	// The full path still matches its own anchor at the covering tier; the suffix
	// "y.go" never reaches a matchable tier — its `y.go:` occurrence is a suffix of
	// the longer path token, so it scores below minAnchorTier.
	require.Equal(t, 3, anchorTier("`internal/x/y.go:42`", "internal/x/y.go", 42))
	require.Less(t, anchorTier("`internal/x/y.go:42`", "y.go", 42), minAnchorTier)
}

// TestStampJustifications_FarSameFileLine_NoMatch: a same-file reference far from
// the finding's line (beyond anchorLineProximity) is a different finding and must
// not attach its narrative (independent-review MEDIUM #1).
func TestStampJustifications_FarSameFileLine_NoMatch(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", "# H\n\n## Findings\n1. **`svc/a.go:200` — a totally different bug far away.** unrelated narrative.\n")
	jf := []JSONFinding{{File: "svc/a.go", Line: 42}}
	stampJustifications(jf, reviewDir)
	require.Empty(t, jf[0].Justification, "line 200 is far from finding line 42 — different finding")

	// A near line (within ±3, cluster-merge divergence) still matches at tier 2;
	// a far same-file reference stays below the match threshold.
	require.Equal(t, 2, anchorTier("`svc/a.go:44`", "svc/a.go", 42)) // off by 2 → near
	require.Less(t, anchorTier("`svc/a.go:200`", "svc/a.go", 42), minAnchorTier)
}

// TestStampJustifications_TightListNoBleed: a blank-line-free numbered list must
// not bleed sibling items into one finding's Justification (independent-review
// MEDIUM #2).
func TestStampJustifications_TightListNoBleed(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", "# H\n\n## Findings\n"+
		"1. **`a/x.go:10` — first issue.** first narrative alpha.\n"+
		"2. **`a/x.go:42` — second issue.** second narrative bravo.\n"+
		"3. **`a/x.go:99` — third issue.** third narrative charlie.\n")
	jf := []JSONFinding{{File: "a/x.go", Line: 42}}
	stampJustifications(jf, reviewDir)
	require.Contains(t, jf[0].Justification, "second narrative bravo")
	require.NotContains(t, jf[0].Justification, "first narrative alpha")
	require.NotContains(t, jf[0].Justification, "third narrative charlie")
	require.NotNil(t, jf[0].SourceReport)
	require.Equal(t, "Findings", jf[0].SourceReport.Section)
}

// TestStampJustifications_LineZero_NoProximityMatch verifies that a file-level
// finding with Line == 0 does not inherit the narrative of an early-line finding
// via proximity (Epic 18.2 TD #5).
func TestStampJustifications_LineZero_NoProximityMatch(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", "# H\n\n## Findings\n1. **`a/x.go:2` — early line issue.** narrative.\n")
	jf := []JSONFinding{{File: "a/x.go", Line: 0}}
	stampJustifications(jf, reviewDir)
	require.Empty(t, jf[0].Justification, "file-level finding (line 0) must not match via proximity")
}

// TestIsItemStart covers the list-item boundary detector.
func TestIsItemStart(t *testing.T) {
	for _, s := range []string{"- bullet", "* star", "+ plus", "1. ordered", "12) paren", "  - indented", "3."} {
		require.Truef(t, isItemStart(s), "%q should be an item start", s)
	}
	for _, s := range []string{"", "not a list", "-nospace", "word - dash", "#. heading-ish", "1x. not"} {
		require.Falsef(t, isItemStart(s), "%q should NOT be an item start", s)
	}
}

// TestReviewFileName_MatchesFanout guards against silent drift between the
// review.md filename used by internal/reconcile (reviewFileName) and the one
// used by internal/fanout (reviewFile). The two packages deliberately do not
// import each other to avoid a cycle, so the contract is enforced by this
// cross-package source test instead (Epic 18.2 TD #1).
func TestReviewFileName_MatchesFanout(t *testing.T) {
	reconcileSrc, err := os.ReadFile("justification.go")
	require.NoError(t, err)
	fanoutSrc, err := os.ReadFile("../fanout/artifacts.go")
	require.NoError(t, err)

	extract := func(src []byte, name string) string {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*=\s*"([^"]+)"`)
		m := re.FindSubmatch(src)
		if m == nil {
			return ""
		}
		return string(m[1])
	}

	reconcileName := extract(reconcileSrc, "reviewFileName")
	fanoutName := extract(fanoutSrc, "reviewFile")
	require.NotEmpty(t, reconcileName, "could not find reviewFileName constant in justification.go")
	require.NotEmpty(t, fanoutName, "could not find reviewFile constant in fanout/artifacts.go")
	require.Equal(t, fanoutName, reconcileName,
		"internal/reconcile reviewFileName must stay in sync with internal/fanout reviewFile")
}

// TestCollectReviewNarratives_SkipsOversizedFiles verifies that a review.md
// larger than maxReviewBytes is skipped rather than fully loaded into memory
// (Epic 18.2 TD #2). The file contains a matching anchor; if it were read it
// would produce a justification, so an empty result proves the skip.
func TestCollectReviewNarratives_SkipsOversizedFiles(t *testing.T) {
	reviewDir := t.TempDir()
	// A file one byte over the cap must be ignored.
	anchor := "1. **`a.go:1`** would match if read.\n"
	padding := strings.Repeat("x\n", maxReviewBytes)
	writeReview(t, reviewDir, "host", anchor+padding)
	jf := []JSONFinding{{File: "a.go", Line: 1}}
	stampJustifications(jf, reviewDir)
	require.Empty(t, jf[0].Justification, "oversized review.md must be skipped")
}
