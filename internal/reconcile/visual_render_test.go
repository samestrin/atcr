package reconcile

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// visualFinding is an out-of-scope browser finding carrying screenshots and a
// console excerpt, the shape stampVisualEvidence produces (Epic 36.0.3).
func visualFinding() JSONFinding {
	return JSONFinding{
		Severity: SevLow, File: "web/index.html", Line: 1, Category: CategoryOutOfScope,
		Problem: "uncaught TypeError on load", Evidence: "ui-console: visual/console.log, visual/home.png",
		Reviewers: []string{"felix"}, Confidence: ConfLow,
		VisualEvidence: &VisualEvidence{
			Screenshots:    []string{"visual/home.png", "visual/home-mobile.png"},
			ConsoleLog:     "visual/console.log",
			ConsoleExcerpt: "TypeError: x is undefined\n\n    at main.js:3",
		},
	}
}

// The reconciled report.md embeds each screenshot as an image link and shows
// the console excerpt in a fenced block indented under the finding, so it stays
// part of the finding's list item (Epic 36.0.3 T4).
func TestRenderMarkdown_VisualEvidence(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, renderMarkdown(&b, Summary{TotalFindings: 1}, []JSONFinding{visualFinding()}, DisagreementsFile{}, 0, nil))
	want := "" +
		"- `web/index.html:1` — confidence LOW, reviewers: felix\n" +
		"  - Problem: uncaught TypeError on load\n" +
		"  - Evidence: ui-console: visual/console.log, visual/home.png\n" +
		"  - Screenshot: ![screenshot](visual/home.png)\n" +
		"  - Screenshot: ![screenshot](visual/home-mobile.png)\n" +
		"  - Console: `visual/console.log`\n" +
		"    ```text\n" +
		"    TypeError: x is undefined\n" +
		"\n" +
		"        at main.js:3\n" +
		"    ```\n"
	assert.True(t, strings.HasSuffix(b.String(), want), "visual evidence block drifted; got:\n%s", b.String())
}

// A finding with no visual evidence renders no visual lines at all, keeping
// report.md byte-identical for a review with no visual run.
func TestRenderMarkdown_NoVisualEvidenceUnchanged(t *testing.T) {
	f := visualFinding()
	f.VisualEvidence = nil
	var b bytes.Buffer
	require.NoError(t, renderMarkdown(&b, Summary{TotalFindings: 1}, []JSONFinding{f}, DisagreementsFile{}, 0, nil))
	assert.NotContains(t, b.String(), "Screenshot")
	assert.NotContains(t, b.String(), "Console")
	assert.True(t, strings.HasSuffix(b.String(), "  - Evidence: ui-console: visual/console.log, visual/home.png\n"))
}

// findings.json can be hand-edited before `atcr report` reads it, so the shared
// writer must not trust VisualEvidence: a reference outside visual/ or one that
// would break out of the link is dropped, a duplicate is rendered once, a
// reserved URL character is percent-encoded, and a backtick run in the excerpt
// cannot close the fence.
func TestWriteVisualEvidence_Hostile(t *testing.T) {
	var b bytes.Buffer
	WriteVisualEvidence(&b, &VisualEvidence{
		Screenshots: []string{
			"visual/ok.png", "visual/ok.png", "../secret.png", "visual/a/b.png",
			"visual/x.png) <script>", "visual/x\n# h.png", "visual/frag#1.png", "javascript:alert(1)",
		},
		ConsoleLog:     "visual/`evil`.log",
		ConsoleExcerpt: "a\r\nb ```` c\x1b[31m\rd",
	})
	want := "" +
		"  - Screenshot: ![screenshot](visual/ok.png)\n" +
		"  - Screenshot: ![screenshot](visual/frag%231.png)\n" +
		"  - Console:\n" +
		"    `````text\n" +
		"    a\n" +
		"    b ```` c[31m\n" +
		"    d\n" +
		"    `````\n"
	assert.Equal(t, want, b.String())
}

func TestWriteVisualEvidence_NilAndEmpty(t *testing.T) {
	var b bytes.Buffer
	WriteVisualEvidence(&b, nil)
	WriteVisualEvidence(&b, &VisualEvidence{})
	assert.Empty(t, b.String())

	// A console log with no readable excerpt still names the file, with no block.
	WriteVisualEvidence(&b, &VisualEvidence{ConsoleLog: "visual/c.log", ConsoleExcerpt: " \n"})
	assert.Equal(t, "  - Console: `visual/c.log`\n", b.String())
}

// A hand-edited excerpt over the stamp-time budget is cut to it on render too.
func TestWriteVisualEvidence_ExcerptBounded(t *testing.T) {
	var b bytes.Buffer
	WriteVisualEvidence(&b, &VisualEvidence{ConsoleExcerpt: strings.Repeat("line\n", 100)})
	assert.Equal(t, visualExcerptMaxLines, strings.Count(b.String(), "    line\n"))
	b.Reset()
	WriteVisualEvidence(&b, &VisualEvidence{ConsoleExcerpt: strings.Repeat("é", visualExcerptMaxBytes)})
	assert.LessOrEqual(t, len(b.String()), visualExcerptMaxBytes+64)
}

func TestScreenshotURIs(t *testing.T) {
	assert.Nil(t, ScreenshotURIs(nil))
	assert.Equal(t, []string{"visual/a.png", "visual/q%3F.png"},
		ScreenshotURIs(&VisualEvidence{Screenshots: []string{"visual/a.png", "/abs/a.png", "visual/q?.png", "visual/a.png"}}))
}
