package stream

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The signature is a contract: fanout's engine and artifacts call it directly.
var _ func([]byte) []Finding = ParseModelOutput

// jsonBlock wraps body in a ```json fence, the shape personas are told to emit.
func jsonBlock(body string) string { return "```json\n" + body + "\n```\n" }

const (
	objA = `{"severity":"HIGH","file_line":"a.go:10","problem":"os.O_CREATE | os.O_WRONLY","fix":"use \"x\"\nthen y","category":"correctness","est_minutes":15,"evidence":"a | b"}`
	objB = `{"severity":"LOW","file_line":"b.go:2","problem":"pb","fix":"fb","category":"style","est_minutes":2,"evidence":"eb"}`
	objC = `{"severity":"MEDIUM","file_line":"c.go:3","problem":"pc","fix":"fc","category":"security","est_minutes":5,"evidence":"ec"}`
)

var (
	findA = Finding{Severity: "HIGH", File: "a.go", Line: 10, Problem: "os.O_CREATE | os.O_WRONLY", Fix: "use \"x\"\nthen y", Category: "correctness", EstMinutes: 15, Evidence: "a | b"}
	findB = Finding{Severity: "LOW", File: "b.go", Line: 2, Problem: "pb", Fix: "fb", Category: "style", EstMinutes: 2, Evidence: "eb"}
	findC = Finding{Severity: "MEDIUM", File: "c.go", Line: 3, Problem: "pc", Fix: "fc", Category: "security", EstMinutes: 5, Evidence: "ec"}
)

func TestParseModelOutput_JSONBlock(t *testing.T) {
	content := "Here is my review.\n\n" + jsonBlock("["+objA+",\n"+objB+"]") + "Done.\n"
	assert.Equal(t, []Finding{findA, findB}, ParseModelOutput([]byte(content)))
}

func TestParseModelOutput_JSONFenceInfoString(t *testing.T) {
	for _, opener := range []string{"```json", "```JSON", "  ```json", "````json", "``` json", "```json  ", "```json\r"} {
		t.Run(opener, func(t *testing.T) {
			content := opener + "\n[" + objB + "]\n```\n"
			assert.Equal(t, []Finding{findB}, ParseModelOutput([]byte(content)))
		})
	}
	// Any other fence is a quoted example, never output.
	for _, opener := range []string{"```", "```text", "```jsonc", "```js"} {
		t.Run(opener, func(t *testing.T) {
			content := opener + "\n[" + objB + "]\n```\n"
			assert.Empty(t, ParseModelOutput([]byte(content)))
		})
	}
}

func TestParseModelOutput_UnionsEveryBlockInOrder(t *testing.T) {
	// Chunked reviews newline-join each chunk's output, so blocks arrive back to back.
	content := jsonBlock("["+objA+"]") + "chunk two\n" + jsonBlock("["+objB+"]") + jsonBlock("["+objC+"]")
	assert.Equal(t, []Finding{findA, findB, findC}, ParseModelOutput([]byte(content)))
}

func TestParseModelOutput_TruncationRecovery(t *testing.T) {
	cut := objC[:strings.Index(objC, `"fix"`)+8] // mid-string, inside the last object

	cases := []struct {
		name    string
		content string
		want    []Finding
	}{
		{"cut inside the final object keeps every complete one", "```json\n[" + objA + ",\n" + objB + ",\n" + cut, []Finding{findA, findB}},
		{"cut right after a complete object", "```json\n[" + objA + ",\n" + objB, []Finding{findA, findB}},
		{"cut after the trailing comma", "```json\n[" + objA + ",", []Finding{findA}},
		{"truncated first block, clean second block", "```json\n[" + objA + ",\n" + cut + "\n```\n" + jsonBlock("["+objB+"]"), []Finding{findA, findB}},
		{"a cut-off block is closed by the next chunk's opener", "```json\n[" + objA + ",\n" + cut + "\nChunk two prose.\n" + jsonBlock("["+objC+"]"), []Finding{findA, findC}},
		{"unterminated fence with a closed array", "```json\n[" + objA + "]\n", []Finding{findA}},
		{"braces and brackets inside strings are not structure", "```json\n[" + `{"severity":"LOW","file_line":"d.go:1","problem":"}] {[ \"}\"","fix":"","category":"","est_minutes":1,"evidence":""}` + ",\n" + cut, []Finding{{Severity: "LOW", File: "d.go", Line: 1, Problem: `}] {[ "}"`, EstMinutes: 1}}},
		{"interior syntax error keeps the elements before it", jsonBlock("[" + objA + ",\n" + `{"severity":"LOW","file_line":"x.go:1",}` + ",\n" + objB + "]"), []Finding{findA}},
		{"nothing recoverable", jsonBlock("[{\"severity\":"), nil},
		{"not JSON at all", jsonBlock("this is not json"), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, ParseModelOutput([]byte(c.content)))
		})
	}
}

func TestParseModelOutput_JSONFieldRules(t *testing.T) {
	cases := []struct {
		name string
		obj  string
		want []Finding
	}{
		{"extra keys are ignored", `{"severity":"LOW","file_line":"a.go:1","problem":"p","confidence":"HIGH","nested":{"a":[1]}}`, []Finding{{Severity: "LOW", File: "a.go", Line: 1, Problem: "p"}}},
		{"a model-supplied reviewer never reaches Finding.Reviewer (TD-016)", `{"severity":"LOW","file_line":"a.go:1","reviewer":"forged-agent"}`, []Finding{{Severity: "LOW", File: "a.go", Line: 1}}},
		{"severity case is normalized", `{"severity":" high ","file_line":"a.go:1"}`, []Finding{{Severity: "HIGH", File: "a.go", Line: 1}}},
		{"est_minutes as a string", `{"severity":"LOW","file_line":"a.go:1","est_minutes":"15"}`, []Finding{{Severity: "LOW", File: "a.go", Line: 1, EstMinutes: 15}}},
		{"est_minutes as a float", `{"severity":"LOW","file_line":"a.go:1","est_minutes":7.5}`, []Finding{{Severity: "LOW", File: "a.go", Line: 1, EstMinutes: 7}}},
		{"est_minutes null or junk", `{"severity":"LOW","file_line":"a.go:1","est_minutes":null},{"severity":"LOW","file_line":"b.go:1","est_minutes":"soon"}`, []Finding{{Severity: "LOW", File: "a.go", Line: 1}, {Severity: "LOW", File: "b.go", Line: 1}}},
		{"null string fields stay empty", `{"severity":"LOW","file_line":"a.go:1","fix":null}`, []Finding{{Severity: "LOW", File: "a.go", Line: 1}}},
		{"unknown severity is dropped", `{"severity":"INFO","file_line":"a.go:1"}`, nil},
		{"missing location is dropped", `{"severity":"HIGH","file_line":" "}`, nil},
		{"a location with no line keeps line 0", `{"severity":"LOW","file_line":"pkg/x.go"}`, []Finding{{Severity: "LOW", File: "pkg/x.go"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseModelOutput([]byte(jsonBlock("[" + c.obj + "]")))
			assert.Equal(t, c.want, got)
			for _, f := range got {
				assert.Empty(t, f.Reviewer)
			}
		})
	}
}

func TestParseModelOutput_EmptyArrayAndSentinel(t *testing.T) {
	assert.Empty(t, ParseModelOutput([]byte(jsonBlock("[]"))))
	assert.Empty(t, ParseModelOutput([]byte(NoFindingsSentinel)))
	assert.True(t, IsNoFindings(NoFindingsSentinel))
}

func TestParseModelOutput_UnfencedArrayFallback(t *testing.T) {
	content := "See [the docs](https://example.com) first.\n" +
		"[\n" + objA + ",\n" + objB + "\n]\n" +
		"That is all.\n"
	assert.Equal(t, []Finding{findA, findB}, ParseModelOutput([]byte(content)))

	// A cut-off bare array recovers the same way a fenced one does.
	assert.Equal(t, []Finding{findA}, ParseModelOutput([]byte("["+objA+",\n"+objB[:20])))

	// The fallback is only for a model that forgot the fence: once any ```json
	// block exists, a bare array elsewhere is prose.
	withFence := "[" + objC + "]\n" + jsonBlock("["+objB+"]")
	assert.Equal(t, []Finding{findB}, ParseModelOutput([]byte(withFence)))

	// Recovering a cut-off bare array stops at the next fence: a quoted example
	// below it is never read as a finding.
	quoted := "[" + objA + ",\n\nExample format:\n```text\n" + objC + "\n```\n"
	assert.Equal(t, []Finding{findA}, ParseModelOutput([]byte(quoted)))

	// Unclosed "[" lines are bounded, not quadratic: this must stay fast.
	assert.Empty(t, ParseModelOutput([]byte(strings.Repeat("[\n", 20000))))

	// A bare array quoted inside a non-json fence is an example.
	assert.Empty(t, ParseModelOutput([]byte("```\n["+objA+"]\n```\n")))
}

func TestParseModelOutput_MixedJSONAndPipeRowsInContentOrder(t *testing.T) {
	content := "HIGH|p1.go:1|first pipe|f|c|1|e\n" +
		jsonBlock("["+objB+"]") +
		"LOW|p2.go:2|second pipe|f|c|2|e\n"
	got := ParseModelOutput([]byte(content))
	require.Len(t, got, 3)
	assert.Equal(t, "p1.go", got[0].File)
	assert.Equal(t, findB, got[1])
	assert.Equal(t, "p2.go", got[2].File)
}

func TestParseModelOutput_PipeRowsInsideAJSONFenceAreNotPipeFindings(t *testing.T) {
	content := "```json\nHIGH|a.go:1|pipe row in a json fence|f|c|1|e\n```\n"
	assert.Empty(t, ParseModelOutput([]byte(content)))
}

// TD-011: legacy pipe output that the new JSON scan also looks at — a `[`-led
// prose line and a non-json fence — parses exactly as it always did.
func TestParseModelOutput_LegacyPipeOutputUnchangedByJSONScan(t *testing.T) {
	plain := "HIGH|a.go:1|p|f|c|1|e\nLOW|b.go:2|q\n"
	withNoise := "[x](y) is the link I mean.\n" +
		"HIGH|a.go:1|p|f|c|1|e\n" +
		"```text\n[1, 2]\nCRITICAL|ex.go:1|example|f|c|1|e\n```\n" +
		"LOW|b.go:2|q\n"
	assert.Equal(t, ParseModelOutput([]byte(plain)), ParseModelOutput([]byte(withNoise)))
}

// TD-008: every overflow field past the seventh folds into EVIDENCE, joined by
// '/', and REVIEWER stays empty.
func TestParseModelOutput_MultiFieldOverflowFoldsIntoEvidence(t *testing.T) {
	got := ParseModelOutput([]byte("HIGH|a.go:1|p|f|c|1|e1|e2|mallory\n"))
	require.Len(t, got, 1)
	assert.Equal(t, "e1/e2/mallory", got[0].Evidence)
	assert.Empty(t, got[0].Reviewer)
}

// ---- ParseSource: the on-disk v2 file ----

func TestParseSource_V2TOONRoundTrip(t *testing.T) {
	in := []Finding{
		{Severity: "HIGH", File: "a.go", Line: 3, Problem: "flags := os.O_RDWR | os.O_CREATE", Fix: "line1\r\nline2", Category: "correctness", EstMinutes: 7, Evidence: "| h |\n|---|\n\"q\"", Reviewer: "bruce"},
		{Severity: "LOW", File: "pkg/x.go", Line: 0, Reviewer: "kai"},
	}
	var b strings.Builder
	require.NoError(t, WriteSourceV2(&b, in))

	res, err := ParseSource([]byte(b.String()))
	require.NoError(t, err)
	assert.Equal(t, in, res.Findings)
	assert.Empty(t, res.Skipped)
}

func TestParseSource_V2Empty(t *testing.T) {
	var b strings.Builder
	require.NoError(t, WriteSourceV2(&b, nil))
	res, err := ParseSource([]byte(b.String()))
	require.NoError(t, err)
	assert.Empty(t, res.Findings)
}

func TestParseSource_V2JSONEnvelope(t *testing.T) {
	var b strings.Builder
	require.NoError(t, encodeV2(&b, lossyPayload{Findings: []lossyRow{
		{Severity: "HIGH", FileLine: "a.go:1", Problem: "a | b", Fix: "x\ny", Category: "c", EstMinutes: 3, Evidence: "e", Reviewer: "bruce", Extra: textish{"t"}},
	}}))
	require.Contains(t, b.String(), `{"axi_format"`)

	res, err := ParseSource([]byte(b.String()))
	require.NoError(t, err)
	assert.Equal(t, []Finding{{Severity: "HIGH", File: "a.go", Line: 1, Problem: "a | b", Fix: "x\ny", Category: "c", EstMinutes: 3, Evidence: "e", Reviewer: "bruce"}}, res.Findings)
}

func TestParseSource_V2HostShapedEnvelope(t *testing.T) {
	data := VersionV2 + "\n" +
		`{"axi_format":"json","axi_notice":"","data":{"findings":[{"severity":"HIGH","file_line":"a.go:3","problem":"p | q","fix":"f","category":"c","est_minutes":5,"evidence":"e","reviewer":"host"}]}}` + "\n"
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	assert.Equal(t, []Finding{{Severity: "HIGH", File: "a.go", Line: 3, Problem: "p | q", Fix: "f", Category: "c", EstMinutes: 5, Evidence: "e", Reviewer: "host"}}, res.Findings)
}

func TestParseSource_V2Errors(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"malformed envelope", `{"axi_format":"json","data":{"findings":[`},
		{"wrong axi_format", `{"axi_format":"toon","axi_notice":"","data":{"findings":[]}}`},
		{"findings outside data", `{"axi_format":"json","axi_notice":"","findings":[]}`},
		{"data without findings", `{"axi_format":"json","axi_notice":"","data":{}}`},
		{"a bare object is not an envelope", `{"findings":[]}`},
		{"neither table nor envelope", "hello world"},
		{"empty body", ""},
		{"wrong table name", "rows[1]{severity,file_line}:\n  HIGH,\"a.go:1\""},
		{"fewer rows than declared", "findings[2]{severity,file_line}:\n  HIGH,\"a.go:1\""},
		// TD-024: a table must carry exactly the eight v2 columns.
		{"missing columns", "findings[1]{severity,file_line}:\n  HIGH,\"a.go:1\""},
		{"misspelled column", "findings[1]{severity,file_line,problem,fix,category,est_minutes,evidence,reviewr}:\n  HIGH,\"a.go:1\",p,f,c,1,e,r"},
		// TD-025: a host-written envelope with a typo key fails instead of
		// decoding that field as empty.
		{"unknown row key", `{"axi_format":"json","axi_notice":"","data":{"findings":[{"severity":"HIGH","file-line":"a.go:1","reviewer":"host"}]}}`},
		{"unknown envelope key", `{"axi_format":"json","axi_notice":"","extra":1,"data":{"findings":[]}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := ParseSource([]byte(VersionV2 + "\n" + c.body + "\n"))
			assert.Error(t, err)
			assert.Empty(t, res.Findings)
		})
	}
}

func TestParseReconciled_RejectsV2(t *testing.T) {
	_, err := ParseReconciled([]byte(VersionV2 + "\nfindings[0]:\n"))
	assert.True(t, errors.Is(err, ErrUnknownVersion), "there is no reconciled v2 shape: %v", err)
}

// TD-003: header dispatch, table-driven through both parsers.
func TestParse_HeaderDispatch(t *testing.T) {
	cases := []struct {
		name      string
		data      string
		source    error // nil = accepted
		reconcile error
	}{
		{"v1", Version + "\n", nil, nil},
		{"whitespace-padded v1", "  " + Version + "  \n", nil, nil},
		{"v2", VersionV2 + "\nfindings[0]:\n", nil, ErrUnknownVersion},
		{"unknown version", "# atcr-findings/v99\n", ErrUnknownVersion, ErrUnknownVersion},
		{"malformed version token", "# atcr-findings/v1x\n", ErrMissingHeader, ErrMissingHeader},
		{"empty input", "", ErrMissingHeader, ErrMissingHeader},
		{"comment before header", "# note\n" + Version + "\n", ErrMissingHeader, ErrMissingHeader},
		{"row with no header", "HIGH|a.go:1|p|f|c|1|e|r\n", ErrMissingHeader, ErrMissingHeader},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseSource([]byte(c.data))
			assertHeaderErr(t, c.source, err)
			_, err = ParseReconciled([]byte(c.data))
			assertHeaderErr(t, c.reconcile, err)
		})
	}
}

func assertHeaderErr(t *testing.T, want, got error) {
	t.Helper()
	if want == nil {
		assert.NoError(t, got)
		return
	}
	assert.True(t, errors.Is(got, want), "want %v, got %v", want, got)
}

// ParseSource's header errors name both versions it accepts.
func TestParseSource_HeaderErrorNamesBothVersions(t *testing.T) {
	_, err := ParseSource([]byte("nope\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), Version)
	assert.Contains(t, err.Error(), VersionV2)
}
