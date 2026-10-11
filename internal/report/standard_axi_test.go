package report

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	toon "github.com/toon-format/toon-go"

	reclib "github.com/samestrin/atcr/reconcile"

	"github.com/samestrin/atcr/internal/history"
	"github.com/samestrin/atcr/internal/reconcile"
)

// withFindingIDs returns a copy of findings with each ID stamped the way the
// reconcile emitter stamps it (history.FindingID over file, line and problem),
// so a golden shows a filled id column rather than an empty one.
func withFindingIDs(findings []reconcile.JSONFinding) []reconcile.JSONFinding {
	out := append([]reconcile.JSONFinding(nil), findings...)
	for i := range out {
		out[i].ID = history.FindingID(out[i].File, out[i].Line, out[i].Problem)
	}
	return out
}

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
// the literal keys axiRow builds — equals axiColumns.header() plus the id column
// after confidence, for EVERY combination of the optional signal flags. The two
// are independent literals (header() serves the legacy pipe encoder; axiRow
// serves the standard path), so only this equality keeps a rename in one place
// from silently desyncing --format axi and --format pipe. id is the one column
// the standard path adds: the legacy pipe bytes are frozen, so header() never
// carries it.
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
		legacy := axiColumnsFor(findings).header()
		require.Equal(t, "confidence", legacy[8], "the id column follows the nine base columns")
		want := append(append(append([]string{}, legacy[:9]...), "id"), legacy[9:]...)
		assert.Equalf(t, want, got,
			"mask=%d: standard row keys diverge from axiColumns.header() plus id", mask)
	}
}

// TestRenderAXI_SanitizationScope pins the EXACT sanitization guarantee go-axi
// v0.4 provides, so the encodeAXI doc comment cannot overstate it and a go-axi
// bump that tightens or loosens the behavior is a visible diff: single control
// bytes (ESC, C1), U+2028/U+2029, invalid UTF-8 and Unicode Cf format characters
// (U+202E bidi override, U+200B zero-width space) are stripped, but a stripped
// escape leaves its CSI parameter text ("[31m") as residue.
func TestRenderAXI_SanitizationScope(t *testing.T) {
	in := "\x1b[31mred\x1b[0m \u202Ebidi\u202C \u200bzw"
	var b bytes.Buffer
	require.NoError(t, renderAXI(&b, []reconcile.JSONFinding{
		{Severity: "LOW", File: "a.go", Line: 1, Problem: in, Category: "c", Confidence: "LOW"},
	}))
	out := b.String()
	assert.NotContains(t, out, "\x1b", "raw ESC bytes are stripped")
	assert.Contains(t, out, "[31mred[0m", "CSI parameter text survives as residue (sequences are not consumed as units)")
	assert.NotContains(t, out, "\u202E", "U+202E bidi-override format char is stripped")
	assert.NotContains(t, out, "\u200b", "U+200B zero-width space is stripped")
	assert.Contains(t, out, "bidi zw", "text around stripped format chars survives")
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
	assert.Equal(t, strings.Repeat("a", 497)+"...", got, "cap applies to the sanitized text (497 runes + ellipsis = 500)")

	// 450 clean runes padded with 60 ESC bytes: sanitized length fits, no cut.
	padded := strings.Repeat("b", 450) + strings.Repeat("\x1b", 60)
	got2 := axiText(padded)
	assert.Equal(t, strings.Repeat("b", 450), got2, "control bytes must not count toward the cap")
	assert.NotContains(t, got2, "...", "a field whose sanitized text fits must not be truncated")
}

// standardAXIGoldenCases mirrors the legacy_pipe golden set for the standard
// TOON path: only findings_plain (report.axi) and home had byte-exact goldens,
// so a go-axi/toon-go bump could shift edge, paginated and review_summary bytes
// unnoticed as long as they still decoded.
var standardAXIGoldenCases = []struct {
	name   string
	golden string
	render func(w io.Writer) error
}{
	{"findings_edge", "findings_edge.axi", func(w io.Writer) error { return renderAXI(w, withFindingIDs(legacyPipeEdgeFindings())) }},
	{"findings_empty", "findings_empty.axi", func(w io.Writer) error { return renderAXI(w, nil) }},
	{"paginated_under", "paginated_under.axi", func(w io.Writer) error {
		return RenderAXIPaginated(w, withFindingIDs(sample()), AXIMaxLinesDefault)
	}},
	{"paginated_truncated", "paginated_truncated.axi", func(w io.Writer) error {
		many := make([]reconcile.JSONFinding, 5)
		for i := range many {
			many[i] = reconcile.JSONFinding{Severity: "LOW", File: "a.go", Line: i, Problem: "p", Confidence: "LOW"}
		}
		return RenderAXIPaginated(w, withFindingIDs(many), 3)
	}},
	{"paginated_empty", "paginated_empty.axi", func(w io.Writer) error {
		return RenderAXIPaginated(w, nil, AXIMaxLinesDefault)
	}},
	{"review_summary", "review_summary.axi", func(w io.Writer) error {
		return RenderReviewSummaryAXI(w, ReviewSummaryAXI{
			ID: "2026-06-10_x", Dir: "/tmp/a|b \x1b[1m", AgentsSucceeded: 3, AgentsTotal: 4,
			AgentsFailed: 1, APICalls: 12, FindingsTotal: 7, FindingsCritical: 1, FindingsHigh: 2,
			FindingsMedium: 3, FindingsLow: 1,
		})
	}},
}

// TestStandardAXI_Goldens freezes the standard TOON payloads byte-for-byte,
// mirroring TestLegacyPipe_Goldens. Unlike the legacy goldens these MAY be
// regenerated with the package-wide -update flag — they pin current renderer
// output, not a reference captured from a frozen fallback.
func TestStandardAXI_Goldens(t *testing.T) {
	for _, tc := range standardAXIGoldenCases {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			require.NoError(t, tc.render(&b))
			path := filepath.Join("testdata", "standard", tc.golden)
			if *update {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, b.Bytes(), 0o644))
				return
			}
			want, err := os.ReadFile(path)
			require.NoErrorf(t, err, "missing golden %s — run: go test ./internal/report -update", path)
			assert.Equalf(t, string(want), b.String(), "standard AXI output drifted from golden %s", path)
		})
	}
}

// axiLossyCell is a TextMarshaler type: toon-go ignores it, silently dropping
// the VALUE while still emitting the KEY — the exact silent-loss shape the
// runtime guard exists to catch (ratified 2026-09-24: switch encodeAXI to
// goaxi.EncodeChecked; ~14% encode cost accepted on a cold admin path).
type axiLossyCell struct{ V string }

func (c axiLossyCell) MarshalText() ([]byte, error) { return []byte(c.V), nil }

// TestEncodeAXI_FailsLoudlyOnLossyValue pins the runtime guard: a value whose
// encoding silently loses data (a TextMarshaler field toon-go drops) must make
// encodeAXI fail loudly and write NOTHING, not emit a lossy payload that parses.
func TestEncodeAXI_FailsLoudlyOnLossyValue(t *testing.T) {
	dirty := map[string]any{"note": axiLossyCell{V: "dropped"}}
	var b bytes.Buffer
	err := encodeAXI(&b, dirty)
	require.Error(t, err, "a lossy value must fail the encode at runtime, not silently drop the cell")
	assert.Empty(t, b.String(), "a failed encode must leave the writer untouched")
}

// TestRenderAXI_IDColumnDeclaredAndFilled pins the id column on both standard
// payloads: the header declares it by name, every row carries the finding's
// JSONFinding.ID, and a finding with no ID (a findings.json written before the
// field existed) decodes to an empty cell rather than shifting the row.
func TestRenderAXI_IDColumnDeclaredAndFilled(t *testing.T) {
	findings := withFindingIDs(sample())
	findings = append(findings, reconcile.JSONFinding{Severity: "LOW", File: "old.go", Line: 1, Problem: "p", Confidence: "LOW"})

	var plain, paged bytes.Buffer
	require.NoError(t, renderAXI(&plain, findings))
	require.NoError(t, RenderAXIPaginated(&paged, findings, AXIMaxLinesDefault))
	for name, payload := range map[string][]byte{"renderAXI": plain.Bytes(), "RenderAXIPaginated": paged.Bytes()} {
		doc := decodeAXI(t, payload)
		assert.Containsf(t, doc.Fields, "id", "%s must declare the id column", name)
		require.Lenf(t, doc.Rows, 3, "%s", name)
		for i, f := range findings {
			assert.Equalf(t, f.ID, doc.Rows[i]["id"], "%s row %d", name, i)
		}
		assert.Equalf(t, "", doc.Rows[2]["id"], "%s: a finding without an ID gets an empty cell", name)
	}
}

// TestRenderAXI_NameMappedConsumerWithoutIDField is AC3 for the axi surface: a
// consumer that reads the payload by NAME into a struct that has no id field
// still reads every other column correctly, base and additive alike. Adding the
// column is therefore not a breaking change for a name-mapped consumer.
func TestRenderAXI_NameMappedConsumerWithoutIDField(t *testing.T) {
	f := reconcile.JSONFinding{
		Severity: "HIGH", File: "a.go", Line: 12, Problem: "token never expires", Fix: "check expiry",
		Category: "security", EstMinutes: 15, Evidence: "expiresAt unread",
		Reviewers: []string{"greta", "otto"}, Confidence: "HIGH", Disagreement: "LOW vs HIGH",
		Verification: &reclib.Verification{Verdict: "confirmed", Skeptic: "sk", Notes: "n", ChallengeSurvived: true},
		EvidenceExec: &reconcile.EvidenceExec{Command: "go test", ExitCode: 1, OutputExcerpt: "FAIL"},
		FixWarning:   "w", FixReview: "r",
	}
	findings := withFindingIDs([]reconcile.JSONFinding{f})
	require.NotEmpty(t, findings[0].ID)

	type row struct {
		Severity          string `toon:"severity"`
		FileLine          string `toon:"file:line"`
		Problem           string `toon:"problem"`
		Fix               string `toon:"fix"`
		Category          string `toon:"category"`
		EstMinutes        int    `toon:"est_minutes"`
		Evidence          string `toon:"evidence"`
		Reviewers         string `toon:"reviewers"`
		Confidence        string `toon:"confidence"`
		Disagreement      string `toon:"disagreement"`
		Verdict           string `toon:"verification.verdict"`
		Skeptic           string `toon:"verification.skeptic"`
		Notes             string `toon:"verification.notes"`
		ChallengeSurvived *bool  `toon:"verification.challenge_survived"`
		Command           string `toon:"evidence_exec.command"`
		ExitCode          *int   `toon:"evidence_exec.exit_code"`
		OutputExcerpt     string `toon:"evidence_exec.output_excerpt"`
		FixWarning        string `toon:"fix_warning"`
		FixReview         string `toon:"fix_review"`
	}
	var b bytes.Buffer
	require.NoError(t, RenderAXIPaginated(&b, findings, AXIMaxLinesDefault))
	require.Contains(t, strings.SplitN(b.String(), "\n", 2)[0], ",id,", "the payload under test must carry the id column")
	var doc struct {
		Findings  []row `toon:"findings"`
		Total     int   `toon:"total"`
		Truncated bool  `toon:"truncated"`
	}
	require.NoErrorf(t, toon.Unmarshal(b.Bytes(), &doc), "a consumer without an id field must decode the payload:\n%s", b.String())
	require.Len(t, doc.Findings, 1)
	got := doc.Findings[0]
	require.NotNil(t, got.ChallengeSurvived)
	require.NotNil(t, got.ExitCode)
	assert.Equal(t, row{
		Severity: "HIGH", FileLine: "a.go:12", Problem: f.Problem, Fix: f.Fix, Category: f.Category,
		EstMinutes: 15, Evidence: f.Evidence, Reviewers: "greta,otto", Confidence: "HIGH",
		Disagreement: f.Disagreement, Verdict: "confirmed", Skeptic: "sk", Notes: "n",
		ChallengeSurvived: got.ChallengeSurvived, Command: "go test", ExitCode: got.ExitCode,
		OutputExcerpt: "FAIL", FixWarning: "w", FixReview: "r",
	}, got)
	assert.True(t, *got.ChallengeSurvived)
	assert.Equal(t, 1, *got.ExitCode)
	assert.Equal(t, 1, doc.Total)
}

// TestLegacyPipe_IgnoresFindingID pins that the frozen --format pipe /
// --legacy-pipe bytes do not change when findings carry an ID: the id column is
// a standard-path addition only.
func TestLegacyPipe_IgnoresFindingID(t *testing.T) {
	without := legacyPipeEdgeFindings()
	with := withFindingIDs(without)
	var a, b bytes.Buffer
	require.NoError(t, Render(&a, without, FormatPipe))
	require.NoError(t, Render(&b, with, FormatPipe))
	assert.Equal(t, a.String(), b.String(), "--format pipe bytes must not change with an ID")
	a.Reset()
	b.Reset()
	require.NoError(t, RenderPipeAXIPaginated(&a, without, AXIMaxLinesDefault))
	require.NoError(t, RenderPipeAXIPaginated(&b, with, AXIMaxLinesDefault))
	assert.Equal(t, a.String(), b.String(), "paginated legacy pipe bytes must not change with an ID")
	assert.NotContains(t, b.String(), with[0].ID)
}
