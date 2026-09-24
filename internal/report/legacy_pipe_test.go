package report

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/reconcile"
)

// legacyPipeEdgeFindings exercises every legacy must-quote and escape rule in one
// payload: the pipe delimiter, TOON specials, all five escapes, reserved tokens,
// number-like strings, a leading '-', surrounding whitespace, raw ANSI and
// U+2028/U+2029 (stripped), an invalid UTF-8 byte (rendered as U+FFFD), unicode,
// and a mixed present/absent verification/evidence block with every additive
// column declared.
func legacyPipeEdgeFindings() []reconcile.JSONFinding {
	return []reconcile.JSONFinding{
		{Severity: "HIGH", File: "cmd/a|b.go", Line: 3,
			Problem:  "a || b and x | y: \"quoted\" \\ back\nnext\ttab\rcr",
			Fix:      "use regexp `foo|bar` [x]{y}",
			Category: "true", EstMinutes: 30, Evidence: "\x1b[31mred\x1b[0m\u2028sep\u2029",
			Reviewers: []string{"greta", "otto"}, Confidence: "42",
			Disagreement: "LOW vs HIGH",
			Verification: &reclib.Verification{Verdict: "confirmed", Skeptic: "null", Notes: " padded ", ChallengeSurvived: true},
			EvidenceExec: &reconcile.EvidenceExec{Command: "grep -E 'a|b' x.go", ExitCode: 1, OutputExcerpt: "-1.5e3"},
			FixWarning:   "invalid_syntax: 1:2", FixReview: "NEEDS_REVIEW"},
		{Severity: "LOW", File: "ユニコード.go", Line: 0,
			Problem: "bad \xff byte", Category: "style", Reviewers: nil, Confidence: "false"},
	}
}

// legacyPipeTextCapFindings builds findings whose cells sit exactly at and one
// rune over the shared maxTextLen cap (500), including a multibyte over-cap
// cell. Freezing these bytes pins the legacy per-cell cap: a change to the
// shared maxTextLen (or truncate) silently rewrites these cells and fails the
// golden, where no existing golden had a cell long enough to notice.
func legacyPipeTextCapFindings() []reconcile.JSONFinding {
	exact500 := strings.Repeat("a", 500)
	over501 := strings.Repeat("b", 500) + "!" // 501 runes
	multi500 := strings.Repeat("\u00e9", 500) // 500 runes, 1000 bytes
	return []reconcile.JSONFinding{
		{Severity: "HIGH", File: "cap.go", Line: 1,
			Problem: exact500, Fix: over501, Category: "cap", EstMinutes: 1,
			Reviewers: []string{"greta"}, Confidence: "HIGH"},
		{Severity: "LOW", File: "cap.go", Line: 2,
			Problem: multi500, Category: "cap", EstMinutes: 1,
			Reviewers: []string{"otto"}, Confidence: "LOW"},
	}
}

var legacyPipeGoldenCases = []struct {
	name   string
	golden string
	render func(w io.Writer) error
}{
	{"findings_plain", "findings_plain.axi", func(w io.Writer) error { return renderPipeAXI(w, sample()) }},
	{"findings_edge", "findings_edge.axi", func(w io.Writer) error { return renderPipeAXI(w, legacyPipeEdgeFindings()) }},
	{"findings_textcap", "findings_textcap.axi", func(w io.Writer) error { return renderPipeAXI(w, legacyPipeTextCapFindings()) }},
	{"findings_empty", "findings_empty.axi", func(w io.Writer) error { return renderPipeAXI(w, nil) }},
	{"paginated_under", "paginated_under.axi", func(w io.Writer) error {
		return RenderPipeAXIPaginated(w, sample(), AXIMaxLinesDefault)
	}},
	{"paginated_truncated", "paginated_truncated.axi", func(w io.Writer) error {
		many := make([]reconcile.JSONFinding, 5)
		for i := range many {
			many[i] = reconcile.JSONFinding{Severity: "LOW", File: "a.go", Line: i, Problem: "p", Confidence: "LOW"}
		}
		return RenderPipeAXIPaginated(w, many, 3)
	}},
	{"paginated_empty", "paginated_empty.axi", func(w io.Writer) error {
		return RenderPipeAXIPaginated(w, nil, AXIMaxLinesDefault)
	}},
	{"review_summary", "review_summary.axi", func(w io.Writer) error {
		return RenderReviewSummaryPipe(w, ReviewSummaryAXI{
			ID: "2026-06-10_x", Dir: "/tmp/a|b \x1b[1m", AgentsSucceeded: 3, AgentsTotal: 4,
			AgentsFailed: 1, APICalls: 12, FindingsTotal: 7, FindingsCritical: 1, FindingsHigh: 2,
			FindingsMedium: 3, FindingsLow: 1,
		})
	}},
	{"home", "home.axi", func(w io.Writer) error {
		return RenderHomeViewPipe(w, HomeViewAXI{
			ExecPath: "~/go/bin/atcr", Description: "Agent Team Code Review — a review panel, not a reviewer",
			ReviewID: "", ReviewStatus: "none",
		})
	}},
}

// TestLegacyPipe_Goldens freezes the legacy pipe-delimited AXI output byte-for-byte
// across findings, pagination, review-summary and home payloads. The legacy path is
// the deprecated fallback for consumers not yet on standard TOON, so its bytes must
// never change. Regenerate with `-update` only for a deliberate legacy change.
func TestLegacyPipe_Goldens(t *testing.T) {
	for _, tc := range legacyPipeGoldenCases {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			require.NoError(t, tc.render(&b))
			path := filepath.Join("testdata", "legacy_pipe", tc.golden)
			if *update {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, b.Bytes(), 0o644))
				return
			}
			want, err := os.ReadFile(path)
			require.NoErrorf(t, err, "missing golden %s — run: go test ./internal/report -update", path)
			assert.Equalf(t, string(want), b.String(), "legacy pipe output drifted from golden %s", path)
		})
	}
}

// TestLegacyPipe_InvalidUTF8RendersReplacementChar pins that the legacy path
// replaces an invalid UTF-8 byte with U+FFFD rather than dropping it — the
// behavior go-axi's sanitizer does NOT share, so it must stay in the legacy
// helper.
func TestLegacyPipe_InvalidUTF8RendersReplacementChar(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, renderPipeAXI(&b, legacyPipeEdgeFindings()))
	assert.Contains(t, b.String(), "\"bad � byte\"")
	assert.NotContains(t, b.String(), "\xff")
}

// TestLegacyPipe_HeadersArePipeDelimited pins the legacy header shape on every
// payload: the `|` delimiter marker inside the declared count.
func TestLegacyPipe_HeadersArePipeDelimited(t *testing.T) {
	for _, tc := range legacyPipeGoldenCases {
		if tc.name == "findings_empty" || tc.name == "paginated_empty" {
			continue
		}
		var b bytes.Buffer
		require.NoError(t, tc.render(&b))
		first := strings.SplitN(b.String(), "\n", 2)[0]
		assert.Regexpf(t, `^[a-z_]+\[\d+\|\]\{`, first, "%s: legacy header must declare the pipe delimiter", tc.name)
	}
}

// TestFormatPipe_IsValidAndRendersLegacy pins `pipe` as a CLI format whose base
// Render output equals the frozen legacy findings encoder.
func TestFormatPipe_IsValidAndRendersLegacy(t *testing.T) {
	assert.True(t, ValidFormat(FormatPipe))
	assert.Contains(t, FormatList(), FormatPipe)
	var got, want bytes.Buffer
	require.NoError(t, Render(&got, sample(), FormatPipe))
	require.NoError(t, renderPipeAXI(&want, sample()))
	assert.Equal(t, want.String(), got.String())
}
