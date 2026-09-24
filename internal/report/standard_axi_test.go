package report

import (
	"bytes"
	"io"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	toon "github.com/toon-format/toon-go"

	reclib "github.com/samestrin/atcr/reconcile"

	"github.com/samestrin/atcr/internal/reconcile"
)

// TestRenderAXI_SanitizesViaGoAxi pins that the standard TOON path cleans text
// with go-axi rather than the legacy hand-rolled stripper: go-axi drops an
// invalid UTF-8 byte, where the legacy path writes U+FFFD.
func TestRenderAXI_SanitizesViaGoAxi(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Confidence: "MEDIUM", Problem: "bad \xff byte"},
	}
	var b bytes.Buffer
	require.NoError(t, Render(&b, findings, FormatAXI))
	assert.Contains(t, b.String(), goaxi.SanitizeString("bad \xff byte"))
	assert.NotContains(t, b.String(), "�", "the standard path must not use the legacy U+FFFD replacement")
}

// decodeAXI decodes a standard TOON payload with go-axi's stock decoder — no
// custom delimiter option — failing the test if it does not parse or does not
// use the default comma delimiter.
func decodeAXI(t *testing.T, payload []byte) *goaxi.Document {
	t.Helper()
	doc, err := goaxi.DecodeTabular(bytes.NewReader(payload))
	require.NoErrorf(t, err, "standard TOON payload must decode:\n%s", payload)
	_, err = goaxi.Decode(bytes.NewReader(payload))
	require.NoErrorf(t, err, "standard TOON payload must decode with the generic decoder:\n%s", payload)
	assert.Equal(t, ',', doc.Delimiter, "standard TOON uses the default comma delimiter")
	return doc
}

// TestRenderAXI_StandardTOONRoundTrip is AC1/AC2: the default --axi findings
// payload is standard comma-delimited TOON, and code strings carrying pipes,
// quotes and newlines survive a stock decode verbatim.
func TestRenderAXI_StandardTOONRoundTrip(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "cmd/a|b.go", Line: 3,
			Problem: "a || b and x | y: \"quoted\"\nnext", Fix: "use regexp `foo|bar`, [x]{y}",
			Category: "true", EstMinutes: 30, Reviewers: []string{"greta", "otto"}, Confidence: "42"},
		{Severity: "LOW", File: "u.go", Line: 7, Problem: "-1", Category: "style", Confidence: "LOW"},
	}
	var b bytes.Buffer
	require.NoError(t, Render(&b, findings, FormatAXI))
	doc := decodeAXI(t, b.Bytes())
	assert.Equal(t, "findings", doc.Name)
	assert.NotContains(t, strings.SplitN(b.String(), "\n", 2)[0], "|", "the header carries no pipe delimiter marker")
	require.Len(t, doc.Rows, 2)
	assert.Equal(t, findings[0].Problem, doc.Rows[0]["problem"])
	assert.Equal(t, findings[0].Fix, doc.Rows[0]["fix"])
	assert.Equal(t, "cmd/a|b.go:3", doc.Rows[0]["file:line"])
	assert.Equal(t, "true", doc.Rows[0]["category"])
	assert.Equal(t, "42", doc.Rows[0]["confidence"])
	assert.Equal(t, "greta,otto", doc.Rows[0]["reviewers"])
	assert.Equal(t, "-1", doc.Rows[1]["problem"])
}

// TestRenderAXIPaginated_HeaderNEqualsEmittedRows is AC8 (amending Sprint 31.0
// AC 03-02 Edge Case 1 for the standard path): a truncated payload declares
// header N equal to the rows emitted and carries the true count as `total: N`
// beside `truncated: true`. Uncut payloads carry both keys too.
func TestRenderAXIPaginated_HeaderNEqualsEmittedRows(t *testing.T) {
	many := make([]reconcile.JSONFinding, 1200)
	for i := range many {
		many[i] = reconcile.JSONFinding{Severity: "LOW", File: "a.go", Line: i, Problem: "p", Confidence: "LOW"}
	}
	var b bytes.Buffer
	require.NoError(t, RenderAXIPaginated(&b, many, AXIMaxLinesDefault))
	doc := decodeAXI(t, b.Bytes())
	assert.Equal(t, AXIMaxLinesDefault-1, doc.Declared, "header N counts the emitted rows, not the true total")
	assert.Len(t, doc.Rows, doc.Declared)
	assert.Equal(t, "1200", doc.Meta["total"])
	assert.Equal(t, "true", doc.Meta["truncated"])

	var u bytes.Buffer
	require.NoError(t, RenderAXIPaginated(&u, sample(), AXIMaxLinesDefault))
	ud := decodeAXI(t, u.Bytes())
	assert.Equal(t, 2, ud.Declared)
	assert.Equal(t, "2", ud.Meta["total"])
	assert.Equal(t, "false", ud.Meta["truncated"])
}

// TestRenderSingleRowAXI_StandardTOON covers the review-summary and home
// payloads: single-row standard TOON arrays a stock decoder reads.
func TestRenderSingleRowAXI_StandardTOON(t *testing.T) {
	var rs bytes.Buffer
	require.NoError(t, RenderReviewSummaryAXI(&rs, ReviewSummaryAXI{ID: "2026-06-10_x", Dir: "/tmp/a|b", AgentsTotal: 4, FindingsHigh: 2}))
	doc := decodeAXI(t, rs.Bytes())
	assert.Equal(t, "review_summary", doc.Name)
	assert.Equal(t, reviewSummaryAXIHeader, doc.Fields)
	require.Len(t, doc.Rows, 1)
	assert.Equal(t, "/tmp/a|b", doc.Rows[0]["dir"])
	assert.Equal(t, "4", doc.Rows[0]["agents_total"])

	var h bytes.Buffer
	require.NoError(t, RenderHomeViewAXI(&h, HomeViewAXI{ExecPath: "~/go/bin/atcr", Description: "d, e", ReviewStatus: "none"}))
	hd := decodeAXI(t, h.Bytes())
	assert.Equal(t, "home", hd.Name)
	assert.Equal(t, homeViewAXIHeader, hd.Fields)
	require.Len(t, hd.Rows, 1)
	assert.Equal(t, "", hd.Rows[0]["review_id"])
	assert.Equal(t, "d, e", hd.Rows[0]["description"])
}

// TestEncodeAXI_SanitizeErrorsSurfaceViaErrorsAs pins that go-axi's typed
// sanitize failures (key collision, cycle) reach the caller unwrapped enough for
// errors.As.
func TestEncodeAXI_SanitizeErrorsSurfaceViaErrorsAs(t *testing.T) {
	err := encodeAXI(io.Discard, map[string]any{"a": 1, "a\x1b": 2})
	var kc *goaxi.KeyCollisionError
	require.ErrorAs(t, err, &kc)

	type node struct{ Next *node }
	n := &node{}
	n.Next = n
	err = encodeAXI(io.Discard, n)
	var ce *goaxi.CycleError
	require.ErrorAs(t, err, &ce)
}

// TestRenderAXIPaginated_NonPositiveMaxLinesClampsToDefault mirrors PaginateAXI's
// clamp on the standard path: a non-positive cap behaves as AXIMaxLinesDefault,
// so the header line is never the only thing emitted.
func TestRenderAXIPaginated_NonPositiveMaxLinesClampsToDefault(t *testing.T) {
	for _, maxLines := range []int{0, -3} {
		doc := axiPaginatedDoc(sample(), maxLines)
		assert.Len(t, doc.Findings, 2, "maxLines=%d must clamp to the default cap", maxLines)
		assert.False(t, doc.Truncated)
		assert.Equal(t, 2, doc.Total)
	}
}

// TestAXIRowKeysMatchColumnHeader pins that the standard encoder's column set —
// the literal keys axiRow builds — equals axiColumns.header() for EVERY
// combination of the optional signal flags. The two are independent literals
// (header() serves the legacy pipe encoder; axiRow serves the standard path), so
// only this equality keeps a rename in one place from silently desyncing
// --format axi and --format pipe.
func TestAXIRowKeysMatchColumnHeader(t *testing.T) {
	blocks := []struct {
		name string
		on   func(f *reconcile.JSONFinding)
	}{
		{"disagreement", func(f *reconcile.JSONFinding) { f.Disagreement = "d" }},
		{"verification", func(f *reconcile.JSONFinding) { f.Verification = &reclib.Verification{Verdict: "v"} }},
		{"evidence", func(f *reconcile.JSONFinding) { f.EvidenceExec = &reconcile.EvidenceExec{Command: "c"} }},
		{"fix_warning", func(f *reconcile.JSONFinding) { f.FixWarning = "w" }},
		{"fix_review", func(f *reconcile.JSONFinding) { f.FixReview = "r" }},
	}
	base := reconcile.JSONFinding{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Fix: "f",
		Category: "c", EstMinutes: 1, Reviewers: []string{"greta"}, Confidence: "HIGH",
	}
	for mask := 0; mask < 1<<len(blocks); mask++ {
		f := base
		for i, b := range blocks {
			if mask&(1<<i) != 0 {
				b.on(&f)
			}
		}
		findings := []reconcile.JSONFinding{f}
		var out bytes.Buffer
		require.NoError(t, renderAXI(&out, findings))
		header := strings.SplitN(out.String(), "\n", 2)[0]
		open := strings.Index(header, "{")
		close_ := strings.LastIndex(header, "}")
		require.Greaterf(t, open, -1, "mask=%d: encoded header %q has no column block", mask, header)
		require.Greaterf(t, close_, open, "mask=%d: encoded header %q malformed", mask, header)
		var got []string
		for _, col := range strings.Split(header[open+1:close_], ",") {
			got = append(got, strings.Trim(strings.TrimSpace(col), `"`))
		}
		assert.Equalf(t, axiColumnsFor(findings).header(), got,
			"mask=%d: standard row keys diverge from axiColumns.header()", mask)
	}
}

// TestRenderAXI_SanitizationScope pins the EXACT sanitization guarantee go-axi
// v0.3 provides, so the encodeAXI doc comment cannot overstate it and a go-axi
// bump that tightens or loosens the behavior is a visible diff: single control
// bytes (ESC, C1), U+2028/U+2029 and invalid UTF-8 are stripped, but Unicode Cf
// format characters (U+202E bidi override, U+200B zero-width space) pass through
// raw and a stripped escape leaves its CSI parameter text ("[31m") as residue.
func TestRenderAXI_SanitizationScope(t *testing.T) {
	in := "\x1b[31mred\x1b[0m \u202Ebidi\u202C \u200bzw"
	var b bytes.Buffer
	require.NoError(t, renderAXI(&b, []reconcile.JSONFinding{
		{Severity: "LOW", File: "a.go", Line: 1, Problem: in, Category: "c", Confidence: "LOW"},
	}))
	out := b.String()
	assert.NotContains(t, out, "\x1b", "raw ESC bytes are stripped")
	assert.Contains(t, out, "[31mred[0m", "CSI parameter text survives as residue (sequences are not consumed as units)")
	assert.Contains(t, out, "\u202E", "U+202E bidi-override format char passes through")
	assert.Contains(t, out, "\u200b", "U+200B zero-width space passes through")
}

// TestEncodeAXI_WritesNothingOnFailure pins the "writes nothing on failure"
// claim the encodeAXI doc makes about go-axi: a failing encode must leave the
// caller's writer untouched, so an orchestrator reading stdout before branching
// on the exit code gets nothing rather than a partial, unparseable payload.
// goaxi.Encode is Sanitize → full toon.Marshal → one writeLine, so marshal
// faults happen before any byte is written; this test makes that structural
// guarantee visible if a go-axi bump ever changes it.
func TestEncodeAXI_WritesNothingOnFailure(t *testing.T) {
	// Two map keys that sanitize to the same string force goaxi's
	// KeyCollisionError — a post-sanitize encode fault.
	dirty := map[string]any{"na\x1bme": 1, "name": 2}
	var b bytes.Buffer
	err := encodeAXI(&b, dirty)
	require.Error(t, err)
	assert.Empty(t, b.String(), "a failed encode must leave the writer untouched")
	var kce *goaxi.KeyCollisionError
	assert.ErrorAs(t, err, &kce)
}

// TestRenderAXI_MixedPayloadDecodesWithTypedPointers pins AC1 for the common
// mixed case: when only some findings carry a verification/evidence block, the
// absent cells must encode as null (not ""), so a stock typed decoder with
// pointer fields accepts the whole document. "" in a bool/int column makes
// toon.Unmarshal reject the payload outright ("cannot assign string to int").
func TestRenderAXI_MixedPayloadDecodesWithTypedPointers(t *testing.T) {
	carrier := reconcile.JSONFinding{
		Severity: "LOW", File: "carrier.go", Line: 2, Problem: "p", Fix: "f",
		Category: "c", EstMinutes: 1, Confidence: "LOW",
		Verification: &reclib.Verification{Verdict: "confirmed", ChallengeSurvived: true},
		EvidenceExec: &reconcile.EvidenceExec{Command: "true", ExitCode: 0},
	}
	plain := reconcile.JSONFinding{
		Severity: "HIGH", File: "plain.go", Line: 1, Problem: "p", Fix: "f",
		Category: "c", EstMinutes: 2, Confidence: "HIGH",
	}
	var b bytes.Buffer
	require.NoError(t, RenderAXIPaginated(&b, []reconcile.JSONFinding{plain, carrier}, AXIMaxLinesDefault))

	type row struct {
		ChallengeSurvived *bool `toon:"verification.challenge_survived"`
		ExitCode          *int  `toon:"evidence_exec.exit_code"`
	}
	var doc struct {
		Findings  []row `toon:"findings"`
		Total     int   `toon:"total"`
		Truncated bool  `toon:"truncated"`
	}
	require.NoErrorf(t, toon.Unmarshal(b.Bytes(), &doc), "stock typed decoder must accept a mixed payload")
	require.Len(t, doc.Findings, 2)
	assert.Nil(t, doc.Findings[0].ChallengeSurvived, "absent block decodes as null, not a value")
	assert.Nil(t, doc.Findings[0].ExitCode, "absent block decodes as null, not a value")
	require.NotNil(t, doc.Findings[1].ChallengeSurvived)
	assert.True(t, *doc.Findings[1].ChallengeSurvived)
	require.NotNil(t, doc.Findings[1].ExitCode)
	assert.Equal(t, 0, *doc.Findings[1].ExitCode)
}

// TestAXIText_SanitizesBeforeTruncating pins the cell-preparation order for the
// standard path: go-axi's sanitizer runs BEFORE the 500-rune cap. Truncating
// first turns invalid UTF-8 into U+FFFD on over-cap fields only (short fields
// have it stripped — inconsistent with AC4), and lets control bytes eat the
// cap so a 450-rune field padded with ANSI bytes is cut with "..." even though
// the cleaned text fits (contradicts AC2).
func TestAXIText_SanitizesBeforeTruncating(t *testing.T) {
	// Over-cap field with an invalid byte: the byte is stripped, no U+FFFD.
	over := strings.Repeat("a", 501) + "\xff"
	got := axiText(over)
	assert.NotContains(t, got, "\uFFFD", "invalid UTF-8 must be stripped, not replaced, on over-cap fields too")
	assert.Equal(t, strings.Repeat("a", 499)+"...", got, "cap applies to the sanitized text")

	// 450 clean runes padded with 60 ESC bytes: sanitized length fits, no cut.
	padded := strings.Repeat("b", 450) + strings.Repeat("\x1b", 60)
	got2 := axiText(padded)
	assert.Equal(t, strings.Repeat("b", 450), got2, "control bytes must not count toward the cap")
	assert.NotContains(t, got2, "...", "a field whose sanitized text fits must not be truncated")
}
